package native

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is what one file of a runtime folder turned out to be.
type Finding struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Read  int64  `json:"read"`
	// WholeFileRead says whether the search reached the end of this file. A
	// search that stopped early has said nothing about the rest of it.
	WholeFileRead bool     `json:"whole_file_read"`
	Mode          string   `json:"mode"`
	Kind          string   `json:"kind"` // "sqlite" | "text" | "binary"
	Plaintext     []string `json:"plaintext,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// Probe is one attempt to open a database file with the host's own sqlite3,
// read-only. It is evidence about this build, not a proof of anything.
type Probe struct {
	Path           string `json:"path"`
	Tool           string `json:"tool"`
	ReadableSchema bool   `json:"readable_schema"`
	Error          string `json:"error,omitempty"`
}

// Assessment is what was actually searched for, and what was found.
//
// It makes no claim about encryption. Absence is not a proof: a string may be
// stored in pieces, compressed, or encoded, and none of that is encryption.
// What this reports is exactly what it did — which files, how many bytes of
// each, which strings were looked for, which were found — and the controls that
// say whether the search itself was working.
type Assessment struct {
	Format      string    `json:"format"`
	RuntimeDir  string    `json:"runtime_dir"`
	Files       int       `json:"files"`
	Bytes       int64     `json:"bytes"`
	BytesRead   int64     `json:"bytes_read"`
	SQLiteFiles int       `json:"sqlite_files"`
	Findings    []Finding `json:"findings"`

	// WholeFilesRead is true only when every file was searched to its end.
	WholeFilesRead bool `json:"whole_files_read"`

	// Needles are the app's own strings; Controls are strings the folder is
	// known to hold in the clear; Absent are strings known not to be there.
	// Finding the controls and not finding the absent ones is what makes a
	// negative result about the needles mean anything at all.
	Needles       []string `json:"needles"`
	Controls      []string `json:"controls"`
	Absent        []string `json:"absent_controls"`
	ControlsFound []string `json:"controls_found"`
	AbsentFound   []string `json:"absent_controls_found"`
	ExposedFiles  []string `json:"files_holding_app_content_in_the_clear"`

	// SearchTrustworthy is about the search, not about the engine: every
	// control was found, no absent control was, and every file was read whole.
	SearchTrustworthy bool `json:"search_trustworthy"`
	// ContentReadable is the one thing this measures: whether any of the app's
	// own strings appears verbatim anywhere it looked.
	ContentReadable bool    `json:"app_content_readable_verbatim"`
	Databases       []Probe `json:"database_open_probe,omitempty"`

	Conclusion string `json:"conclusion"`
	NotAClaim  string `json:"not_a_claim"`
	Protection string `json:"protection"`
}

const sqliteMagic = "SQLite format 3\x00"

// Assess walks a profile's runtime folder.
func (m *Manager) Assess(profileID string, needles, controls, absent []string, probe bool) (*Assessment, error) {
	p, err := m.loadProfile(profileID)
	if err != nil {
		return nil, err
	}
	if p.RuntimeDir == "" {
		return nil, errors.New("native: this profile has no runtime folder")
	}
	return AssessDir(p.RuntimeDir, needles, controls, absent, probe)
}

// AssessDir is Assess over a folder named directly, for a retired runtime.
//
// Every file is read to its end in windows that overlap by one needle's length,
// so a string lying across a window boundary is still found.
func AssessDir(dir string, needles, controls, absent []string, probe bool) (*Assessment, error) {
	a := &Assessment{Format: "rokh.native-assessment/2", RuntimeDir: dir,
		Needles: needles, Controls: controls, Absent: absent,
		Findings: []Finding{}, ControlsFound: []string{}, AbsentFound: []string{}, ExposedFiles: []string{}}
	kind := map[string]string{}
	var want [][]byte
	for _, group := range [][]string{needles, controls, absent} {
		for _, n := range group {
			if n == "" {
				continue
			}
			want = append(want, []byte(n))
		}
	}
	for _, n := range controls {
		kind[n] = "control"
	}
	for _, n := range absent {
		kind[n] = "absent"
	}
	a.WholeFilesRead = true
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		f := Finding{Path: rel, Bytes: fi.Size(), Mode: fi.Mode().Perm().String()}
		hits, head, read, whole, scanErr := scanFile(path, want)
		f.Read, f.WholeFileRead = read, whole
		if scanErr != nil {
			f.Error = scanErr.Error()
		}
		switch {
		case bytes.HasPrefix(head, []byte(sqliteMagic)):
			f.Kind = "sqlite"
			a.SQLiteFiles++
		case isMostlyText(head):
			f.Kind = "text"
		default:
			f.Kind = "binary"
		}
		for _, h := range hits {
			f.Plaintext = append(f.Plaintext, h)
		}
		sort.Strings(f.Plaintext)
		a.Files++
		a.Bytes += fi.Size()
		a.BytesRead += read
		if !whole {
			a.WholeFilesRead = false
		}
		a.Findings = append(a.Findings, f)
		return nil
	})
	if err != nil {
		return a, err
	}

	seen := map[string]bool{}
	for _, f := range a.Findings {
		exposedHere := false
		for _, n := range f.Plaintext {
			switch kind[n] {
			case "control":
				if !seen[n] {
					seen[n] = true
					a.ControlsFound = append(a.ControlsFound, n)
				}
			case "absent":
				if !seen[n] {
					seen[n] = true
					a.AbsentFound = append(a.AbsentFound, n)
				}
			default:
				exposedHere = true
			}
		}
		if exposedHere {
			a.ExposedFiles = append(a.ExposedFiles, f.Path)
		}
	}
	if probe {
		a.Databases = probeDatabases(dir)
	}
	a.ContentReadable = len(a.ExposedFiles) > 0
	a.SearchTrustworthy = len(a.ControlsFound) == len(nonEmpty(controls)) &&
		len(a.AbsentFound) == 0 && a.WholeFilesRead && len(nonEmpty(controls)) > 0
	switch {
	case len(nonEmpty(needles)) == 0:
		a.Conclusion = "no app strings were given, so nothing was measured"
	case !a.SearchTrustworthy:
		a.Conclusion = "this search cannot be relied on: a control was missed, an absent string was found, " +
			"or a file was not read to its end"
	case a.ContentReadable:
		a.Conclusion = "the app's own strings appear verbatim in the files named above"
	default:
		a.Conclusion = "every file was read to its end; the controls were found, the absent controls were not, " +
			"and none of the app's own strings appears verbatim anywhere in this folder"
	}
	a.NotAClaim = "this is not a claim that the engine encrypts anything. A string can be absent because it is " +
		"stored in pieces, compressed or encoded, none of which is encryption. What the database probe below " +
		"shows is that this build's files do not open as plain SQLite on this host — evidence about the build, " +
		"not a proof about the bytes."
	a.Protection = "the runtime folder is 0700 and ephemeral; the durable copy of this state is the sealed " +
		"checkpoint in the home, and the home's own catalog, index and content are sealed whatever the engine does"
	return a, nil
}

func nonEmpty(s []string) []string {
	out := []string{}
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// scanFile reads a file to its end in overlapping windows and reports which of
// the wanted strings it saw, the first bytes (for the kind), how much it read
// and whether it reached the end.
func scanFile(path string, want [][]byte) (hits []string, head []byte, read int64, whole bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, false, err
	}
	defer f.Close()
	longest := 0
	for _, w := range want {
		if len(w) > longest {
			longest = len(w)
		}
	}
	const window = 1 << 20
	overlap := longest
	if overlap > 0 {
		overlap--
	}
	buf := make([]byte, 0, window+overlap)
	chunk := make([]byte, window)
	found := map[string]bool{}
	for {
		n, readErr := f.Read(chunk)
		if n > 0 {
			read += int64(n)
			buf = append(buf, chunk[:n]...)
			if head == nil {
				cut := len(buf)
				if cut > 4096 {
					cut = 4096
				}
				head = append([]byte(nil), buf[:cut]...)
			}
			for _, w := range want {
				if !found[string(w)] && bytes.Contains(buf, w) {
					found[string(w)] = true
				}
			}
			if len(buf) > overlap {
				buf = append(buf[:0], buf[len(buf)-overlap:]...)
			}
		}
		if readErr == io.EOF {
			whole = true
			break
		}
		if readErr != nil {
			return keys(found), head, read, false, readErr
		}
	}
	return keys(found), head, read, whole, nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// probeDatabases asks the host's own sqlite3, read-only, whether these files
// open as plain SQLite. It changes nothing and is recorded as evidence.
func probeDatabases(dir string) []Probe {
	tool, err := exec.LookPath("sqlite3")
	if err != nil {
		return []Probe{{Tool: "sqlite3", Error: "not on this host"}}
	}
	out := []Probe{}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !strings.HasSuffix(path, ".db") && !strings.HasSuffix(path, ".sqlite3") {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		cmd := exec.Command(tool, "-readonly", path, ".schema")
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		stdout, runErr := cmd.CombinedOutput()
		p := Probe{Path: rel, Tool: tool}
		if runErr != nil {
			p.Error = strings.TrimSpace(trim(string(stdout), 200))
			if p.Error == "" {
				p.Error = runErr.Error()
			}
		} else {
			p.ReadableSchema = len(bytes.TrimSpace(stdout)) > 0
		}
		out = append(out, p)
		return nil
	})
	return out
}

func isMostlyText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	n := len(b)
	if n > 4096 {
		n = 4096
	}
	printable := 0
	for _, c := range b[:n] {
		if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c < 0x7f) || c >= 0x80 {
			printable++
		}
	}
	return printable*10 >= n*9 && !strings.Contains(string(b[:n]), "\x00\x00\x00\x00")
}
