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
	"rokh/keyview"
	"rokh/ledger"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// Fixtures of T2 for doors that serve keys their own views: a carrier made
// as a v1 carrier is, with every record sealed by hand to the readers of its
// own point, and a door that reads through the owner's key layer and seals
// at each record's point.

// createBare makes a v1 vessel carrier in dir whose owner cell opens with
// pass and holds root, and records nothing: the caller records the genesis.
// It returns the owner's secret.
func createBare(t *testing.T, dir, pass string, root ed25519.PrivateKey, anchor frame.ID) (*carrier.Carrier, key.Secret) {
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
	p := vessel.Params{SlabLog2: 18, Slabs: 16, Iter: v1Iter, Rand: rand.Reader, Salt: salt}
	c, err := carrier.Create(medium.Dir{Root: dir}, p, vk, [][]byte{cell}, anchor, frame.Zero, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, sec
}

// putSealed records one event sealed to exactly the readers given, under the
// writer's turn, and moves branch onto it.
func putSealed(t *testing.T, dir string, c *carrier.Carrier, e event.Signed, branch string, readers ...[]byte) {
	t.Helper()
	env, err := key.SealReaders(key.TypeEvent, e.ID, nil, readers, e.Body, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := turn.Acquire(dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err := c.Refresh(); err != nil {
		t.Fatal(err)
	}
	rec, err := c.Begin(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.EventEnvelope(e.ID, e.Head, env); err != nil {
		t.Fatal(err)
	}
	if branch != "" {
		if err := rec.SetRef(branch, e.ID); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		t.Fatalf("commit: %s %v", out, err)
	}
}

// ownerDoor opens the owner's key layer over c with the owner's secret,
// reads the door's own view through the owner's dressed session, and makes a
// door that seals every record at its own point.
func ownerDoor(t *testing.T, c *carrier.Carrier, sec key.Secret, opts Options) *Server {
	t.Helper()
	_, _, layer, err := keyview.OwnerSessionOf(c, sec)
	if err != nil {
		t.Fatal(err)
	}
	ptk := c.Vessel().SharedKey()
	opts.Layer = layer
	opts.SealerAt = func(l *ledger.Ledger, parents []frame.ID) (carrier.Sealer, error) {
		sess, err := layer.OwnerSessionAt(l, sec, ptk, parents)
		if err != nil {
			return nil, err
		}
		return sess, nil
	}
	return New(c, loadV1(t, c), opts)
}
