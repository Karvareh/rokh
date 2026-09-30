package bench

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// The host gives the core its randomness (event.Entropy).
func init() {
	if event.Entropy == nil {
		event.Entropy = rand.Reader
	}
}

const v1Iter = 1000

// createV1 makes a v1 carrier in dir whose owner cell opens with pass and
// holds root, and records the given events on "main" in one commit. root may
// be nil for a cold carrier whose owner cell holds no signing seed.
func createV1(t *testing.T, dir, pass string, root ed25519.PrivateKey, anchor frame.ID, events ...event.Signed) *carrier.Carrier {
	t.Helper()
	salt := make([]byte, 32)
	vk := make([]byte, 32)
	io.ReadFull(rand.Reader, salt)
	io.ReadFull(rand.Reader, vk)
	var seed [32]byte
	if root != nil {
		copy(seed[:], root.Seed())
	}
	cell, sec, err := key.NewOwner(pass, salt, v1Iter, seed, vk, rand.Reader)
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
	p := vessel.Params{SlabLog2: 20, Slabs: 64, Iter: v1Iter, Rand: rand.Reader, Salt: salt,
		Growth: vessel.Growth{Auto: true, Step: 64, Max: 1 << 16}}
	c, err := carrier.Create(medium.Dir{Root: dir}, p, vk, [][]byte{cell}, anchor, frame.Zero, nil, sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) > 0 {
		recordV1(t, dir, c, events...)
	}
	return c
}

// recordV1 records events and points "main" at the last, in one commit.
func recordV1(t *testing.T, dir string, c *carrier.Carrier, events ...event.Signed) {
	t.Helper()
	lock, err := turn.Acquire(dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	rec, err := c.Begin(lock)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if err := rec.Event(e.ID, e.Head, e.Body, e.Event.Address); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.SetRef("main", events[len(events)-1].ID); err != nil {
		t.Fatal(err)
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		t.Fatalf("commit: %s %v", out, err)
	}
}

// keys is a daemon.KeySource holding one delegated key.
type keys struct {
	name      string
	priv      ed25519.PrivateKey
	authority frame.ID
}

// Key hands out a copy: the door owns what it is given and wipes it after
// use, so the fixture's own key must never be the slice it receives.
func (k keys) Key(name string) (ed25519.PrivateKey, *frame.ID, bool) {
	if name != k.name {
		return nil, nil, false
	}
	a := k.authority
	return append(ed25519.PrivateKey(nil), k.priv...), &a, true
}

// own gives the door its own copy of a key.
func own(k ed25519.PrivateKey) ed25519.PrivateKey { return append(ed25519.PrivateKey(nil), k...) }

func (k keys) Names() []string { return []string{k.name} }
