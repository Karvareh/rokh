// Package turn is the writer's lock of a vessel: the host adapter that makes
// a vessel.Owner (contract 2.9). It is outside the portable core.
//
// The lock is the kernel's, on the vessel's first head file, rokh/head0.rkh.
// Nothing is created or written for it. It is exclusive, it is held until it
// is released or until the holder dies, and it is never a timer: a paused
// holder still holds, and nobody takes it from a living one. Readers may
// share it. On darwin, linux and android it is flock; on windows it is a
// byte-range lock beyond the end of the file. A host without such a lock
// offers no owner, and vessels then open for reading only.
//
// What it cannot do is said plainly: it is local. Two machines writing one
// synced folder are not kept apart (contract U5, G2).
package turn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HeadFile is the file whose kernel lock is the writer's turn.
const HeadFile = "rokh/head0.rkh"

// ErrBusy is returned when another holder kept the turn for longer than the
// caller was willing to wait. Nothing was written.
var ErrBusy = errors.New("turn: another writer holds this vessel")

// ErrUnavailable is returned where the host has no kernel lock to offer.
var ErrUnavailable = errors.New("turn: this host offers no writer's lock (turn_unavailable)")

// Lock is a held turn. It is the vessel.Owner a writer hands to the core.
type Lock struct {
	f        *os.File
	path     string
	released bool
}

// Holds answers from the live lock handle: nil while the handle is open, not
// released, and still the file at the vessel's path.
func (l *Lock) Holds() error {
	if l == nil || l.f == nil || l.released {
		return errors.New("turn: not held")
	}
	a, err := l.f.Stat()
	if err != nil {
		return fmt.Errorf("turn: the lock handle is gone: %w", err)
	}
	b, err := os.Stat(l.path)
	if err != nil || !os.SameFile(a, b) {
		return errors.New("turn: the head file was replaced; this lock no longer guards it")
	}
	return nil
}

// Release gives the turn back. It is idempotent.
func (l *Lock) Release() error {
	if l == nil || l.released {
		return nil
	}
	l.released = true
	uerr := unlock(l.f)
	cerr := l.f.Close()
	if uerr != nil {
		return uerr
	}
	return cerr
}

// Acquire waits up to patience for the exclusive turn on the vessel in the
// carrier folder dir. The waiting is the host's; the core never waits.
func Acquire(dir string, patience time.Duration) (*Lock, error) {
	return acquire(dir, true, patience)
}

// AcquireShared waits for a reading turn: many readers together, no writer
// while they hold it.
func AcquireShared(dir string, patience time.Duration) (*Lock, error) {
	return acquire(dir, false, patience)
}

func acquire(dir string, exclusive bool, patience time.Duration) (*Lock, error) {
	path := filepath.Join(dir, filepath.FromSlash(HeadFile))
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("turn: %s is not a vessel's head file: %w", path, err)
	}
	deadline := time.Now().Add(patience)
	for {
		ok, err := tryLock(f, exclusive)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			return &Lock{f: f, path: path}, nil
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrBusy
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Take is Acquire returning only the release, for callers that keep the
// shape of the earlier API.
func Take(dir string, patience time.Duration) (func() error, error) {
	l, err := Acquire(dir, patience)
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}

// Share is AcquireShared returning only the release.
func Share(dir string, patience time.Duration) (func() error, error) {
	l, err := AcquireShared(dir, patience)
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}
