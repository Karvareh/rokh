// Package medium is the host adapter that keeps a vessel in files. It is
// outside the portable core (contract 2.9, C11): the core sees only the
// vessel.Medium interface.
//
// Every call is a portable os call, the same code on darwin, linux, android
// and windows. A write opens the file in place, writes every byte, cuts any
// excess length and flushes the file. There is no temporary file and no
// rename: the vessel's format makes a half-written file harmless, and a
// rename is the one step FAT, exFAT and cloud folders do not keep atomic.
package medium

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rokh/vessel"
)

// Dir is a vessel medium rooted at a carrier folder.
//
// Every operation goes through os.Root opened on the carrier folder: a path
// that climbs out of it, whether by "..", an absolute name or a symlink that
// points outside, is refused by the kernel-level walk the standard library
// does, not by a check made before the open. The carrier folder
// itself is the one the person named.
type Dir struct{ Root string }

var _ vessel.Medium = Dir{}

// name checks a vessel name lexically and gives it in the host's form. The
// root confinement below is what binds it; this only refuses nonsense early.
func (d Dir) name(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("medium: bad name %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("medium: bad name %q", name)
		}
	}
	return filepath.FromSlash(name), nil
}

// root opens the carrier folder as a root that no operation escapes.
func (d Dir) root() (*os.Root, error) {
	if d.Root == "" {
		return nil, errors.New("medium: no carrier folder")
	}
	return os.OpenRoot(d.Root)
}

// plain walks a path through the root with Lstat and refuses a link at any
// component, even one that stays inside the carrier, and an intermediate that
// is not a directory; the last must be a directory when dir is set, else a
// regular file. absent allows a last component that does not exist yet
// (a write that creates). A vessel holds no link.
func plain(r *os.Root, p string, dir, absent bool) (fs.FileInfo, error) {
	parts := strings.Split(filepath.ToSlash(p), "/")
	cur := ""
	for i, part := range parts {
		if cur == "" {
			cur = part
		} else {
			cur += "/" + part
		}
		last := i == len(parts)-1
		fi, err := r.Lstat(filepath.FromSlash(cur))
		if err != nil {
			if last && absent && errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("medium: %s is a link, and a vessel holds none", cur)
		}
		switch {
		case !last && !fi.IsDir():
			return nil, fmt.Errorf("medium: %s is not a directory", cur)
		case last && dir && !fi.IsDir():
			return nil, fmt.Errorf("medium: %s is not a directory", cur)
		case last && !dir && !fi.Mode().IsRegular():
			return nil, fmt.Errorf("medium: %s is not a regular file", cur)
		}
		if last {
			return fi, nil
		}
	}
	return nil, nil
}

// same refuses an opened file that is not the one plain looked at: a swap
// between the look and the open.
func same(before fs.FileInfo, f *os.File, name string) error {
	after, err := f.Stat()
	if err != nil {
		return err
	}
	if before != nil && !os.SameFile(before, after) {
		return fmt.Errorf("medium: %s changed between the look and the open", name)
	}
	if !after.Mode().IsRegular() {
		return fmt.Errorf("medium: %s is not a regular file", name)
	}
	return nil
}

func notFound(err error, name string) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", vessel.ErrNotFound, name)
	}
	return err
}

// Read reads a whole file, refusing one longer than max.
func (d Dir) Read(name string, max int) ([]byte, error) {
	p, err := d.name(name)
	if err != nil {
		return nil, err
	}
	r, err := d.root()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	fi, err := plain(r, p, false, false)
	if err != nil {
		return nil, notFound(err, name)
	}
	f, err := r.Open(p)
	if err != nil {
		return nil, notFound(err, name)
	}
	defer f.Close()
	if err := same(fi, f, name); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, fmt.Errorf("%w: %s", vessel.ErrTooLarge, name)
	}
	return b, nil
}

// Write creates or overwrites a file in place, then flushes it.
func (d Dir) Write(name string, b []byte) error {
	p, err := d.name(name)
	if err != nil {
		return err
	}
	r, err := d.root()
	if err != nil {
		return err
	}
	defer r.Close()
	fi, err := plain(r, p, false, true)
	if err != nil {
		return notFound(err, name)
	}
	f, err := r.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return notFound(err, name)
	}
	if err := same(fi, f, name); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteAt(b, 0); err != nil {
		f.Close()
		return err
	}
	if st, err := f.Stat(); err == nil && st.Size() != int64(len(b)) {
		if err := f.Truncate(int64(len(b))); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Names lists one directory, ASCII-lowercased and sorted.
func (d Dir) Names(dir string) ([]string, error) {
	p, err := d.name(dir)
	if err != nil {
		return nil, err
	}
	r, err := d.root()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if _, err := plain(r, p, true, false); err != nil {
		return nil, notFound(err, dir)
	}
	f, err := r.Open(p)
	if err != nil {
		return nil, notFound(err, dir)
	}
	defer f.Close()
	es, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, lower(e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// Mkdir creates one directory; vessel.ErrExists when it is there.
func (d Dir) Mkdir(name string) error {
	p, err := d.name(name)
	if err != nil {
		return err
	}
	r, err := d.root()
	if err != nil {
		return err
	}
	defer r.Close()
	if parent := filepath.Dir(p); parent != "." {
		if _, err := plain(r, parent, true, false); err != nil {
			return notFound(err, name)
		}
	}
	if err := r.Mkdir(p, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return vessel.ErrExists
		}
		return err
	}
	return nil
}

// Remove removes one file. Only shrinking calls it.
func (d Dir) Remove(name string) error {
	p, err := d.name(name)
	if err != nil {
		return err
	}
	r, err := d.root()
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := plain(r, p, false, false); err != nil {
		return notFound(err, name)
	}
	if err := r.Remove(p); err != nil {
		return notFound(err, name)
	}
	return nil
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
