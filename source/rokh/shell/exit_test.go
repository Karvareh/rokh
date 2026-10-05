package shell

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"rokh/medium"
	"rokh/tui"
	"rokh/vessel"
)

// failingDisk is a disk that will not write the vessel's slabs, or that
// writes its head files and then says the write failed, so the ending of a
// recording is unknown. It stands under a real session.
type failingDisk struct {
	vessel.Medium
	slabs, heads bool
}

func (d failingDisk) Write(name string, b []byte) error {
	isHead := strings.Contains(name, "/head")
	if d.slabs && !isHead {
		return &fs.PathError{Op: "write", Path: name, Err: errors.New("file too large")}
	}
	err := d.Medium.Write(name, b)
	if d.heads && isHead && err == nil {
		return errors.New("the flush was not confirmed")
	}
	return err
}

// underFailingDisk makes a Rokh on a good disk and opens a session on it
// through a failing one.
func underFailingDisk(t *testing.T, d failingDisk) *session {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	s, err := makeRokh(vault, "", "", "pass", "home", minRoom)
	if err != nil {
		t.Fatal(err)
	}
	s.closeAll()
	old := mediumFor
	mediumFor = func(dir string) vessel.Medium {
		return failingDisk{Medium: medium.Dir{Root: dir}, slabs: d.slabs, heads: d.heads}
	}
	t.Cleanup(func() { mediumFor = old })
	return newSession(vault, "", "", "pass")
}

// The line surface ends the way it went, and prints nothing unasked into a
// pipe: 0 when every sentence was answered, 1 when any was refused, 4 when
// the ending of a recording is unknown; the worst stands. Every refusal
// carries its code.
func TestTheLineSurfaceEndsTheWayItWent(t *testing.T) {
	good := func(t *testing.T) *session {
		vault := filepath.Join(t.TempDir(), "vault")
		s, err := makeRokh(vault, "", "", "pass", "home", minRoom)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	coded := regexp.MustCompile(`^no — .+ \[[a-z_]+(; (not recorded|unknown))?\]$`)
	for _, c := range []struct {
		name    string
		session func(*testing.T) *session
		in      string
		code    int
		said    string
	}{
		{"a clean session", good, "see the ledger\nsee the ledgers\nleave\n", 0, ""},
		{"one refused line", good, "see the ledger\nopen the ledger nosuch\nsee the ledger\nleave\n", 1, "no — there is no ledger named nosuch"},
		{"a line that is no sentence, then the end of input", good, "sing a song\n", 1, "[unknown_sentence]"},
		{"a disk that will not write", func(t *testing.T) *session { return underFailingDisk(t, failingDisk{slabs: true}) },
			"write at home/a: one\nwrite\nleave\n", 1, "Nothing was recorded. [storage_failed; not recorded]"},
		{"an ending nobody knows", func(t *testing.T) *session { return underFailingDisk(t, failingDisk{heads: true}) },
			"write at home/a: one\nwrite\nsee the ledger\nleave\n", 4, "whether it was recorded is unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := c.session(t)
			defer s.closeAll()
			var out, notes strings.Builder
			code := runLines(s, strings.NewReader(c.in), &out, &notes, false)
			if code != c.code {
				t.Errorf("exit %d, want %d; notes %q", code, c.code, notes.String())
			}
			if strings.Contains(notes.String(), "? ") {
				t.Errorf("a prompt was printed into a pipe: %q", notes.String())
			}
			if !strings.Contains(notes.String(), c.said) {
				t.Errorf("notes %q do not say %q", notes.String(), c.said)
			}
			if c.code == 4 && (strings.Contains(strings.ToLower(notes.String()), "nothing was recorded") ||
				!strings.Contains(notes.String(), "read the ledger to see whether it holds that sentence")) {
				t.Errorf("an unknown ending was said to have recorded nothing, or leaving did not say it is unknown: %q", notes.String())
			}
			for _, ln := range strings.Split(notes.String(), "\n") {
				if strings.HasPrefix(ln, "no — ") && !coded.MatchString(ln) {
					t.Errorf("a refusal without its code: %q", ln)
				}
			}
		})
	}
}

// stderrOf runs f with standard error caught.
func stderrOf(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	os.Stderr = old
	w.Close()
	return <-done
}

// Asking for the flags ends 0 and lists every sentence of the one list; the
// first-run text names only the program that printed it, in straight
// columns.
func TestHelpAndTheFirstRunTextSpeakForTheirOwnProgram(t *testing.T) {
	var code int
	help := stderrOf(t, func() { code = RunWithHome("rokh-shell", []string{"-h"}, nil) })
	if code != 0 {
		t.Fatalf("rokh-shell -h exited %d", code)
	}
	for _, sn := range tui.Sentences {
		if !strings.Contains(help, " "+sn.Say+"\n") {
			t.Errorf("-h does not list %q", sn.Say)
		}
	}
	for _, name := range []string{"rokh", "rokh-shell"} {
		var b strings.Builder
		firstRun(&b, name)
		text := b.String()
		if (name == "rokh-shell") == strings.Contains(text, "rokh help") {
			t.Errorf("%s's first-run text and rokh help: %q", name, text)
		}
		col := -1
		for _, ln := range strings.Split(text, "\n") {
			if !strings.HasPrefix(ln, "  "+name) && !strings.HasPrefix(ln, "  rokh help") {
				continue
			}
			i := strings.Index(ln[2:], "  ") + 2
			for i < len(ln) && ln[i] == ' ' {
				i++
			}
			if col >= 0 && i != col {
				t.Errorf("%s: the first-run text is not in columns:\n%s", name, text)
			}
			col = i
		}
	}
}

// A snapshot file is drawn and nothing else happens: named with a folder,
// a sentence or a gate it is refused, and a file that is not a snapshot is
// said plainly, with nothing opened.
func TestAPictureOpensNoRokh(t *testing.T) {
	var code int
	said := stderrOf(t, func() {
		code = RunWithHome("rokh-shell", []string{"/path/to/carrier", "-state", "../tui/sample.json"}, nil)
	})
	if code != 2 || !strings.Contains(said, "opens no Rokh") {
		t.Fatalf("a snapshot with a folder: exit %d, %q", code, said)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"ledgr": "typo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	said = stderrOf(t, func() { code = RunWithHome("rokh-shell", []string{"-state", bad}, nil) })
	if code != 1 || !strings.Contains(said, "the snapshot could not be read") || !strings.Contains(said, "[refused]") {
		t.Fatalf("a snapshot that is not one: exit %d, %q", code, said)
	}
}
