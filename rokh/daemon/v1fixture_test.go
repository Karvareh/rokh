package daemon

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

const v1Iter = 1000

// createV1 makes a v1 vessel carrier in dir whose owner cell opens with pass
// and holds root, and records the genesis on "main" in one commit.
func createV1(t *testing.T, dir, pass string, root ed25519.PrivateKey, gen event.Signed) *carrier.Carrier {
	t.Helper()
	salt := make([]byte, 32)
	vk := make([]byte, 32)
	io.ReadFull(rand.Reader, salt)
	io.ReadFull(rand.Reader, vk)
	var seed [32]byte
	copy(seed[:], root.Seed())
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
	p := vessel.Params{SlabLog2: 18, Slabs: 16, Iter: v1Iter, Rand: rand.Reader, Salt: salt}
	c, err := carrier.Create(medium.Dir{Root: dir}, p, vk, [][]byte{cell}, gen.ID, frame.Zero, nil, sess)
	if err != nil {
		t.Fatal(err)
	}
	recordV1(t, dir, c, []event.Signed{gen}, "main")
	return c
}

// recordV1 records events (and points branch at the last) in one commit.
func recordV1(t *testing.T, dir string, c *carrier.Carrier, events []event.Signed, branch string) {
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
		if e.HeadOnly {
			if err := rec.EventEnvelope(e.ID, e.Head, carrier.HeadOnly); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := rec.Event(e.ID, e.Head, e.Body, e.Event.Address); err != nil {
			t.Fatal(err)
		}
	}
	if branch != "" && len(events) > 0 {
		if err := rec.SetRef(branch, events[len(events)-1].ID); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		t.Fatalf("commit: %s %v", out, err)
	}
}

// openV1 opens a v1 carrier with the owner's passphrase and its session.
func openV1(t *testing.T, dir, pass string) (*carrier.Carrier, key.Secret) {
	t.Helper()
	c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock(pass)), rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := c.Vessel()
	info := v.Info()
	sess, sec, err := key.VesselSession(pass, v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c.SetSealer(sess)
	return c, sec
}
