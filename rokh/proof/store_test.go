package proof

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/daemon"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// place is where a vessel lives: a carrier folder on the drive, as the
// carrier ships, or a medium in memory, as the core's own tests keep one.
//
// The medium in memory takes the place the 0.9 quick store had here, and for
// the same reason. The flush is the medium's promise about a power cut, and
// that promise is measured in the vessel's and the medium's own suites, at
// every cut point. The tests that use a vessel in memory ask other questions:
// what the references reach, whether the live view says what the vessel
// holds, what a door answers. Their answers do not change with the drive's
// cache. Anything here that is about the writer's turn on a folder, which is
// the kernel's lock on a real file, uses a folder.
type place struct {
	dir string         // the carrier folder; empty for a vessel in memory
	m   vessel.Medium  // medium.Dir on dir, or mem
	mem *vessel.Memory // the medium in memory; nil on a folder
}

// onFolder is a place in a fresh carrier folder.
func onFolder(t *testing.T) place {
	t.Helper()
	dir := t.TempDir()
	return place{dir: dir, m: medium.Dir{Root: dir}}
}

// inMemory is a place in a fresh medium in memory.
func inMemory() place {
	m := vessel.NewMemory()
	return place{m: m, mem: m}
}

// processHold is the writer's hold of a process that has the only copy of a
// vessel in memory: nothing else can reach it, so it holds while the process
// lives (contract 2.9: the host makes the owner; the core never waits).
type processHold struct{}

func (processHold) Holds() error { return nil }

// writer takes the writer's hold for one recording: the folder's turn, which
// every writer on this machine takes, or the process's own hold in memory.
func (p place) writer(t *testing.T) (vessel.Owner, func()) {
	t.Helper()
	if p.dir == "" {
		return processHold{}, func() {}
	}
	lk, err := turn.Acquire(p.dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	return lk, func() { lk.Release() }
}

// door gives daemon options the writer's hold of this place: the folder, so
// that every recording takes the folder's turn, or the process's own hold.
func (p place) door(o daemon.Options) daemon.Options {
	if p.dir != "" {
		o.Dir = p.dir
	} else {
		o.Owner = processHold{}
	}
	return o
}

// create makes a new carrier here, anchored at anchor, whose owner cell
// opens with pass. root is the owner's signing key; nil makes a carrier in
// cold custody, whose cell holds no signing seed (contract 4.6). The owner's
// session seals every record to the owner's reader (E3), as `rokh init`
// makes it.
func (p place) create(t *testing.T, anchor frame.ID, root ed25519.PrivateKey) *carrier.Carrier {
	t.Helper()
	return p.createAs(t, anchor, root, pass)
}

// createAs is create with the owner's passphrase named.
func (p place) createAs(t *testing.T, anchor frame.ID, root ed25519.PrivateKey, passphrase string) *carrier.Carrier {
	t.Helper()
	salt := make([]byte, 32)
	vk := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(rand.Reader, vk); err != nil {
		t.Fatal(err)
	}
	var seed [32]byte
	if root != nil {
		copy(seed[:], root.Seed())
	}
	cell, sec, err := key.NewOwner(passphrase, salt, testIter, seed, vk, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if sess.PTK, err = vessel.SharedKey(vk); err != nil {
		t.Fatal(err)
	}
	params := vessel.Params{SlabLog2: slabLog2, Slabs: slabs, Iter: testIter, Rand: rand.Reader, Salt: salt}
	c, err := carrier.Create(p.m, params, vk, [][]byte{cell}, anchor, frame.Zero, nil, sess)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// commitOn makes one recording on a carrier at this place under the
// writer's hold, and insists that it was recorded.
func commitOn(t *testing.T, p place, c *carrier.Carrier, fill func(*carrier.Recording) error) {
	t.Helper()
	own, release := p.writer(t)
	defer release()
	rec, err := c.Begin(own)
	if err != nil {
		t.Fatal(err)
	}
	if err := fill(rec); err != nil {
		rec.Abandon()
		t.Fatal(err)
	}
	if out, err := rec.Commit(); err != nil || out != vessel.Recorded {
		t.Fatalf("the recording was %s: %v", out, err)
	}
}

// open opens the carrier here a second time with the owner's passphrase, as
// another process would: its own vessel state, its own session, nothing
// shared but the place. It returns what opening found (contract 2.7).
func (p place) open(t *testing.T) (*carrier.Carrier, vessel.Report) {
	t.Helper()
	c, _, rep, err := p.openAs(pass)
	if err != nil {
		t.Fatalf("opening the carrier again: %v", err)
	}
	return c, rep
}

// openAs opens the carrier here with a passphrase, and gives the secret that
// passphrase opened: the owner's, whose slot holds the root's signing seed.
func (p place) openAs(passphrase string) (*carrier.Carrier, key.Secret, vessel.Report, error) {
	c, rep, err := carrier.Open(p.m, vessel.Unlock(key.Unlock(passphrase)), rand.Reader, nil)
	if err != nil {
		return nil, key.Secret{}, rep, err
	}
	v := c.Vessel()
	info := v.Info()
	sess, sec, err := key.VesselSession(passphrase, v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
	if err != nil {
		return nil, key.Secret{}, rep, err
	}
	c.SetSealer(sess)
	return c, sec, rep, nil
}

// files is the name and digest of every file the vessel keeps here. The
// file set of a vessel is constant (contract V6), so "what was written" is
// "which files changed".
func (p place) files(t *testing.T) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	if p.mem != nil {
		for _, n := range p.mem.FileNames() {
			out[n] = sha256.Sum256(p.mem.Get(n))
		}
		return out
	}
	err := filepath.WalkDir(p.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(p.dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sha256.Sum256(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sizes is the name and length of every file the vessel keeps here.
func (p place) sizes(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	if p.mem != nil {
		return p.mem.Sizes()
	}
	err := filepath.WalkDir(p.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(p.dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = int(fi.Size())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// changed names the files whose bytes differ between two digests of one
// place.
func changed(before, after map[string][32]byte) []string {
	var out []string
	for n, d := range after {
		if before[n] != d {
			out = append(out, n)
		}
	}
	for n := range before {
		if _, ok := after[n]; !ok {
			out = append(out, n)
		}
	}
	return out
}

// holds reports whether the carrier here keeps any record of an event, its
// head with or without a body, read from the place itself rather than from a
// view kept in memory.
func (p place) holds(t *testing.T, id frame.ID) bool {
	t.Helper()
	c, _ := p.open(t)
	found := false
	if err := c.Events(func(e carrier.EventRecord) error {
		if e.ID == id {
			found = true
		}
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	return found
}

// envelopes counts the whole records the carrier here keeps of one event
// (contract E2: one id may sit in several envelopes).
func (p place) envelopes(t *testing.T, id frame.ID) int {
	t.Helper()
	c, _ := p.open(t)
	n := 0
	if err := c.Events(func(e carrier.EventRecord) error {
		if e.ID == id {
			n = len(e.Refs)
		}
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	return n
}

// The vessel's shape for a carrier that lives for one test: the smallest
// slab the format allows, and room enough that no proof here fills it.
const (
	slabLog2 = vessel.MinSlabLog2
	slabs    = 32
)

// turnPatience is how long a fixture waits for the writer's turn on a
// folder. The waiting is the host's; the core never waits.
const turnPatience = 5 * time.Second

// errCut is the medium refusing a write, as it does when the power goes.
var errCut = errors.New("proof: the power went")
