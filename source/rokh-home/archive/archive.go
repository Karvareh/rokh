// Package archive brings files, folders and projects into a home and hands them
// out again, with nothing lost and nothing overwritten.
//
// Bringing in has three steps that are kept apart:
//
//  1. Preview: the source is walked without following a link. Every file,
//     folder, empty folder and symbolic link is listed with its permission
//     bits; anything that cannot be kept as a file — a device, a socket, a
//     pipe, a name that is not UTF-8 — is refused and named before a byte is
//     copied. The free space the copy needs is measured.
//  2. Copy: each file is opened without following links, checked against what
//     the preview saw, streamed into sealed storage while it is hashed, and
//     checked again. A file that changed on the way stops the copy and says so.
//     Progress is kept in a journal, so a copy that is stopped — or whose
//     process died — resumes where its journal left it.
//  3. The manifest: only a complete copy yields one. What it names, and only
//     that, is what a recording refers to.
//
// Handing out writes into a destination that does not exist or is an empty
// folder, and nowhere else. It never follows a link in the destination, never
// replaces a file, recreates a link only when it stays inside what is handed
// out, restores permission bits and empty folders, and checks every file's hash
// as it writes it. If anything could not be written, the handing-out is not
// called complete: by default everything it wrote is taken back and the
// destination is left as it was; asked explicitly, it leaves what it wrote and
// lists what is missing.
package archive

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// TreeFormat names the manifest's encoding.
const TreeFormat = "rokh-home.tree/2"

// JournalFormat names the journal's encoding.
const JournalFormat = "rokh-home.import/1"

// MaxEntries bounds one import.
const MaxEntries = 200000

var (
	ErrRefused = errors.New("archive: the source holds what cannot be kept; nothing was copied")
	ErrChanged = errors.New("archive: the source changed after its preview; the copy stopped")
	ErrStopped = errors.New("archive: the copy was stopped; its journal can resume it")
	ErrNoSpace = errors.New("archive: the home does not have the space this copy needs")
)

// Entry is one thing in a manifest.
type Entry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Blob   string `json:"blob,omitempty"`
	Link   string `json:"link,omitempty"`
	MTime  int64  `json:"mtime_ns,omitempty"`
}

// Manifest is what was brought in, completely.
type Manifest struct {
	Format  string  `json:"format"`
	Name    string  `json:"name"`
	Single  bool    `json:"single"`
	Mode    uint32  `json:"mode"`
	Entries []Entry `json:"entries"`
}

// Refusal is something that could not be kept or written, and why.
type Refusal struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

// Stat is what the preview saw of a source file, to know it again.
type Stat struct {
	Dev   uint64 `json:"dev"`
	Ino   uint64 `json:"ino"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime_ns"`
}

// Preview is what an import would bring in.
type Preview struct {
	Source    string    `json:"source"`
	Name      string    `json:"name"`
	Single    bool      `json:"single"`
	Mode      uint32    `json:"mode"`
	Entries   []Entry   `json:"entries"`
	Stats     []Stat    `json:"stats"`
	Files     int       `json:"files"`
	Dirs      int       `json:"dirs"`
	Links     int       `json:"links"`
	Bytes     int64     `json:"bytes"`
	Refused   []Refusal `json:"refused,omitempty"`
	FreeBytes uint64    `json:"free_bytes"`
	NeedBytes uint64    `json:"need_bytes"`
	Fits      bool      `json:"fits"`
}

func statOf(fi os.FileInfo) Stat {
	s := Stat{Size: fi.Size(), MTime: fi.ModTime().UnixNano()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		s.Dev, s.Ino = uint64(st.Dev), uint64(st.Ino)
	}
	return s
}

// FreeBytes is the space available to an unprivileged writer at a path.
func FreeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// Need is the space a copy of so many bytes in so many files takes in sealed
// storage, with a margin: every 64 KiB segment carries a 16-byte tag, every
// object a header and a directory entry.
func Need(bytes int64, files int) uint64 {
	return uint64(bytes) + uint64(bytes)/4096 + uint64(files)*8192 + 64<<20
}

// MakePreview walks a source without following links.
func MakePreview(source, home string) (*Preview, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	p := &Preview{Source: abs, Name: filepath.Base(abs), Mode: uint32(fi.Mode().Perm())}
	if !utf8.ValidString(p.Name) {
		p.Refused = append(p.Refused, Refusal{Path: p.Name, Why: "the name is not UTF-8"})
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		p.Refused = append(p.Refused, Refusal{Path: p.Name, Why: "the source itself is a symbolic link; name what it points at"})
	case fi.Mode().IsRegular():
		p.Single = true
		p.Entries = []Entry{{Path: p.Name, Kind: "file", Mode: uint32(fi.Mode().Perm()), Size: fi.Size(), MTime: fi.ModTime().UnixNano()}}
		p.Stats = []Stat{statOf(fi)}
		p.Files, p.Bytes = 1, fi.Size()
	case fi.IsDir():
		if err := p.walk(abs, ""); err != nil {
			return nil, err
		}
	default:
		p.Refused = append(p.Refused, Refusal{Path: p.Name, Why: "the source is neither a file nor a folder"})
	}
	if len(p.Entries) > MaxEntries {
		p.Refused = append(p.Refused, Refusal{Path: p.Name, Why: fmt.Sprintf("more than %d entries", MaxEntries)})
	}
	if free, err := FreeBytes(home); err == nil {
		p.FreeBytes = free
	}
	p.NeedBytes = Need(p.Bytes, p.Files)
	p.Fits = p.FreeBytes >= p.NeedBytes
	return p, nil
}

func (p *Preview) walk(dir, rel string) error {
	list, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, de := range list {
		name := de.Name()
		path := name
		if rel != "" {
			path = rel + "/" + name
		}
		if !utf8.ValidString(name) {
			p.Refused = append(p.Refused, Refusal{Path: strings.ToValidUTF8(path, "�"), Why: "the name is not UTF-8"})
			continue
		}
		full := filepath.Join(dir, name)
		fi, err := os.Lstat(full)
		if err != nil {
			return err
		}
		e := Entry{Path: path, Mode: uint32(fi.Mode().Perm()), MTime: fi.ModTime().UnixNano()}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(full)
			if err != nil {
				return err
			}
			e.Kind, e.Link, e.Mode = "symlink", target, 0o777
			p.Links++
		case fi.Mode().IsRegular():
			e.Kind, e.Size = "file", fi.Size()
			p.Files++
			p.Bytes += fi.Size()
		case fi.IsDir():
			e.Kind = "dir"
			p.Dirs++
		default:
			p.Refused = append(p.Refused, Refusal{Path: path, Why: "a device, socket or pipe cannot be kept as a file"})
			continue
		}
		p.Entries = append(p.Entries, e)
		p.Stats = append(p.Stats, statOf(fi))
		if e.Kind == "dir" {
			if err := p.walk(full, path); err != nil {
				return err
			}
		}
	}
	return nil
}

// Sink is sealed storage for the bytes of an import.
type Sink interface {
	PutBlob(id string, r io.Reader) (size int64, sum [32]byte, err error)
}

// Done is one copied file.
type Done struct {
	Blob   string `json:"blob"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Journal is an import in progress.
type Journal struct {
	Format  string       `json:"format"`
	ID      string       `json:"id"`
	Runs    int          `json:"runs"`
	State   string       `json:"state"` // previewed | running | stopped | changed | failed | complete
	Note    string       `json:"note,omitempty"`
	Preview Preview      `json:"preview"`
	Done    map[int]Done `json:"done"`
}

// NewJournal starts a journal for a preview with no refusals.
func NewJournal(p *Preview) (*Journal, error) {
	if len(p.Refused) > 0 {
		return nil, ErrRefused
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Journal{Format: JournalFormat, ID: hex.EncodeToString(b), State: "previewed",
		Preview: *p, Done: map[int]Done{}}, nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(b)
}

// Run copies what is not yet copied. It saves the journal as it goes and
// before it returns, whatever it returns.
func (j *Journal) Run(ctx context.Context, sink Sink, save func(*Journal) error) error {
	if j.State == "complete" {
		return nil
	}
	j.Runs++
	j.State, j.Note = "running", ""
	if err := save(j); err != nil {
		return err
	}
	finish := func(state, note string, err error) error {
		j.State, j.Note = state, note
		if serr := save(j); serr != nil && err == nil {
			return serr
		}
		return err
	}
	sinceSave := 0
	for i, e := range j.Preview.Entries {
		if e.Kind != "file" {
			continue
		}
		if _, done := j.Done[i]; done {
			continue
		}
		if ctx.Err() != nil {
			return finish("stopped", "stopped before "+e.Path, ErrStopped)
		}
		full := j.Preview.Source
		if !j.Preview.Single {
			full = filepath.Join(j.Preview.Source, filepath.FromSlash(e.Path))
		}
		d, err := copyOne(ctx, sink, full, j.Preview.Stats[i], fmt.Sprintf("%s/%d/%d", j.ID, j.Runs, i))
		switch {
		case errors.Is(err, ErrChanged):
			return finish("changed", e.Path+" changed after the preview", ErrChanged)
		case ctx.Err() != nil:
			return finish("stopped", "stopped while copying "+e.Path, ErrStopped)
		case err != nil:
			return finish("failed", e.Path+": "+err.Error(), err)
		}
		j.Done[i] = d
		sinceSave++
		if sinceSave >= 64 {
			if err := save(j); err != nil {
				return err
			}
			sinceSave = 0
		}
	}
	return finish("complete", "", nil)
}

func copyOne(ctx context.Context, sink Sink, path string, seen Stat, blob string) (Done, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ELOOP) {
			return Done{}, ErrChanged
		}
		return Done{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Done{}, err
	}
	if !before.Mode().IsRegular() || statOf(before) != seen {
		return Done{}, ErrChanged
	}
	size, sum, err := sink.PutBlob(blob, ctxReader{ctx: ctx, r: f})
	if err != nil {
		return Done{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return Done{}, err
	}
	if statOf(after) != seen || size != seen.Size {
		return Done{}, ErrChanged
	}
	if now, err := os.Lstat(path); err != nil || statOf(now) != seen {
		return Done{}, ErrChanged
	}
	return Done{Blob: blob, SHA256: hex.EncodeToString(sum[:]), Size: size}, nil
}

// Manifest is the complete copy's manifest.
func (j *Journal) Manifest() (Manifest, error) {
	if j.State != "complete" {
		return Manifest{}, fmt.Errorf("archive: the import is %s, not complete", j.State)
	}
	m := Manifest{Format: TreeFormat, Name: j.Preview.Name, Single: j.Preview.Single, Mode: j.Preview.Mode}
	for i, e := range j.Preview.Entries {
		if e.Kind == "file" {
			d, ok := j.Done[i]
			if !ok {
				return Manifest{}, fmt.Errorf("archive: %s was never copied", e.Path)
			}
			e.Blob, e.SHA256, e.Size = d.Blob, d.SHA256, d.Size
		}
		m.Entries = append(m.Entries, e)
	}
	sort.Slice(m.Entries, func(a, b int) bool { return m.Entries[a].Path < m.Entries[b].Path })
	return m, nil
}

// Encode is the manifest's one encoding, whose SHA-256 is its name.
func (m Manifest) Encode() ([]byte, [32]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, [32]byte{}, err
	}
	out := []byte(b.String())
	return out, sha256.Sum256(out), nil
}

// DecodeManifest reads a manifest and checks its shape.
func DecodeManifest(b []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Format != TreeFormat || m.Name == "" || strings.ContainsAny(m.Name, "/\x00") || m.Name == "." || m.Name == ".." {
		return m, errors.New("archive: not a tree manifest")
	}
	for _, e := range m.Entries {
		if err := validRel(e.Path); err != nil {
			return m, err
		}
		switch e.Kind {
		case "file", "dir", "symlink":
		default:
			return m, fmt.Errorf("archive: unknown entry kind %q", e.Kind)
		}
	}
	return m, nil
}

func validRel(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\x00") || !utf8.ValidString(p) {
		return fmt.Errorf("archive: bad entry path %q", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("archive: bad entry path %q", p)
		}
	}
	return nil
}

// Source gives back the bytes of a stored blob.
type Source interface {
	OpenBlob(id string) (io.ReadCloser, error)
}

// ExportOptions are the explicit choices a handing-out may be given.
type ExportOptions struct {
	// AllowIncomplete leaves what was written when something could not be,
	// and reports the handing-out as incomplete. Without it, nothing is left.
	AllowIncomplete bool
	// AllowOutsideLinks recreates a link that points outside what is handed
	// out. Without it such a link is not written, and is listed as missing.
	AllowOutsideLinks bool
	// KeepTimes restores modification times.
	KeepTimes bool
}

// ExportReport is what a handing-out did.
type ExportReport struct {
	Destination string    `json:"destination"`
	Root        string    `json:"root"`
	State       string    `json:"state"` // complete | incomplete | failed | refused
	Written     int       `json:"written"`
	Missing     []Refusal `json:"missing,omitempty"`
}

// Export hands out a manifest's tree under dst. dst must not exist, or be an
// empty folder; its parent must exist.
func Export(m Manifest, dst string, src Source, opts ExportOptions) (ExportReport, error) {
	rep := ExportReport{Destination: dst}
	if _, err := DecodeManifestCheck(m); err != nil {
		rep.State = "refused"
		return rep, err
	}
	abs, err := filepath.Abs(dst)
	if err != nil {
		rep.State = "refused"
		return rep, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		rep.State = "refused"
		return rep, fmt.Errorf("archive: the destination's folder does not exist: %w", err)
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	rep.Destination = abs
	var created []string // in order of creation
	madeDst := false
	switch fi, err := os.Lstat(abs); {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		rep.State = "refused"
		return rep, errors.New("archive: the destination is a symbolic link; it is not followed")
	case err == nil && fi.IsDir():
		list, err := os.ReadDir(abs)
		if err != nil {
			rep.State = "refused"
			return rep, err
		}
		if len(list) > 0 {
			rep.State = "refused"
			return rep, errors.New("archive: the destination is a folder that is not empty; nothing is written into it")
		}
	case err == nil:
		rep.State = "refused"
		return rep, errors.New("archive: something that is not a folder is at the destination; it is not replaced")
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(abs, 0o700); err != nil {
			rep.State = "refused"
			return rep, err
		}
		madeDst = true
	default:
		rep.State = "refused"
		return rep, err
	}
	root := filepath.Join(abs, m.Name)
	rep.Root = root
	missing := func(path, why string) { rep.Missing = append(rep.Missing, Refusal{Path: path, Why: why}) }
	target := func(rel string) string {
		if m.Single {
			return root
		}
		return filepath.Join(root, filepath.FromSlash(rel))
	}
	if !m.Single {
		if err := os.Mkdir(root, 0o700); err != nil {
			rep.State = "failed"
			return rep, err
		}
		created = append(created, root)
	}
	entries := append([]Entry(nil), m.Entries...)
	sort.Slice(entries, func(a, b int) bool { return entries[a].Path < entries[b].Path })
	var dirs []Entry
	for _, e := range entries {
		if e.Kind != "dir" {
			continue
		}
		if err := os.Mkdir(target(e.Path), 0o700); err != nil {
			missing(e.Path, "the folder could not be made: "+err.Error())
			continue
		}
		created = append(created, target(e.Path))
		dirs = append(dirs, e)
	}
	for _, e := range entries {
		if e.Kind != "file" {
			continue
		}
		t := target(e.Path)
		if err := writeFile(t, e, src, opts); err != nil {
			missing(e.Path, err.Error())
			continue
		}
		created = append(created, t)
		rep.Written++
	}
	for _, e := range entries {
		if e.Kind != "symlink" {
			continue
		}
		inside := !filepath.IsAbs(e.Link)
		if inside {
			resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(e.Path)), filepath.FromSlash(e.Link))))
			inside = resolved != ".." && !strings.HasPrefix(resolved, "../")
		}
		if !inside && !opts.AllowOutsideLinks {
			missing(e.Path, "the link points outside what is handed out: "+e.Link)
			continue
		}
		if err := os.Symlink(e.Link, target(e.Path)); err != nil {
			missing(e.Path, "the link could not be made: "+err.Error())
			continue
		}
		created = append(created, target(e.Path))
		rep.Written++
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Chmod(target(dirs[i].Path), fs.FileMode(dirs[i].Mode)&fs.ModePerm)
		if opts.KeepTimes && dirs[i].MTime != 0 {
			setTime(target(dirs[i].Path), dirs[i].MTime)
		}
	}
	if !m.Single {
		os.Chmod(root, fs.FileMode(m.Mode)&fs.ModePerm|0o700)
	}
	if len(rep.Missing) == 0 {
		rep.State = "complete"
		return rep, nil
	}
	if opts.AllowIncomplete {
		rep.State = "incomplete"
		return rep, fmt.Errorf("archive: %d entries were not written; the handing-out is incomplete", len(rep.Missing))
	}
	for i := len(created) - 1; i >= 0; i-- {
		os.Chmod(created[i], 0o700)
		os.Remove(created[i])
	}
	if madeDst {
		os.Remove(abs)
	}
	rep.State, rep.Written = "failed", 0
	return rep, fmt.Errorf("archive: %d entries could not be written; nothing was left at the destination", len(rep.Missing))
}

// DecodeManifestCheck checks a manifest already in hand.
func DecodeManifestCheck(m Manifest) (Manifest, error) {
	b, _, err := m.Encode()
	if err != nil {
		return m, err
	}
	return DecodeManifest(b)
}

func writeFile(t string, e Entry, src Source, opts ExportOptions) error {
	rc, err := src.OpenBlob(e.Blob)
	if err != nil {
		return fmt.Errorf("its bytes are not in the home: %v", err)
	}
	defer rc.Close()
	nonce := make([]byte, 6)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(t), ".rokh-partial-"+hex.EncodeToString(nonce))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), rc)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && (n != e.Size || hex.EncodeToString(h.Sum(nil)) != e.SHA256) {
		err = errors.New("its bytes do not match the hash they were recorded with")
	}
	if err == nil {
		err = os.Chmod(tmp, fs.FileMode(e.Mode)&fs.ModePerm)
	}
	if err == nil && opts.KeepTimes && e.MTime != 0 {
		setTime(tmp, e.MTime)
	}
	if err == nil {
		// A hard link to the new name fails if anything already answers to
		// that name on this filesystem, which is the one test that catches
		// names the filesystem folds together.
		if lerr := os.Link(tmp, t); lerr != nil {
			if errors.Is(lerr, os.ErrExist) {
				err = errors.New("a file of a name this filesystem cannot tell apart is already there; it is not replaced")
			} else {
				err = lerr
			}
		}
	}
	os.Remove(tmp)
	return err
}

func setTime(path string, ns int64) {
	t := time.Unix(0, ns)
	_ = os.Chtimes(path, t, t)
}
