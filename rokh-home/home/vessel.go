package home

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// The home's ledger is a v1 vessel (contract 2.10): the owner's passphrase
// opens its key slot, which holds the root's signing seed and the owner's
// reader; every recording is one vessel commit made under the writer's turn.

// vesselParams are the home ledger's vessel parameters: the contract's
// defaults, or small ones for a synthetic test home (lowered iterations).
func vesselParams(iter []int) vessel.Params {
	// The home's content lives inside the vessel, so it grows with the data
	// (contract 1, growth auto), up to a ceiling a person can raise.
	p := vessel.Params{SlabLog2: 20, Slabs: 64, Iter: 600000, Rand: rand.Reader,
		Growth: vessel.Growth{Auto: true, Step: 64, Max: 4096}}
	if len(iter) == 1 && iter[0] > 0 {
		p.SlabLog2, p.Slabs, p.Iter = 18, 16, iter[0]
		p.Growth = vessel.Growth{Auto: true, Step: 16, Max: 1024}
	}
	return p
}

// createLedger makes the home's ledger vessel in dir with the genesis, the
// owner's cell under pass, and the given root signing seed (zero for a
// reading mirror, which holds no signing secret).
func createLedger(dir, pass string, gen event.Signed, rootSeed [32]byte, anchor frame.ID, iter []int) error {
	p := vesselParams(iter)
	salt := make([]byte, 32)
	vk := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	if _, err := io.ReadFull(rand.Reader, vk); err != nil {
		return err
	}
	p.Salt = salt
	cell, sec, err := key.NewOwner(pass, salt, p.Iter, rootSeed, vk, rand.Reader)
	if err != nil {
		return err
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		return err
	}
	c, err := carrier.Create(medium.Dir{Root: dir}, p, vk, [][]byte{cell}, anchor, frame.Zero, nil, sess)
	if err != nil {
		return err
	}
	if gen.ID.IsZero() {
		return nil
	}
	return recordOnce(dir, c, func(rec *carrier.Recording) error {
		if err := rec.Event(gen.ID, gen.Head, gen.Body, gen.Event.Address); err != nil {
			return err
		}
		return rec.SetRef(defaultBranch, gen.ID)
	})
}

// recordOnce makes one vessel commit under the writer's turn.
func recordOnce(dir string, c *carrier.Carrier, fill func(*carrier.Recording) error) error {
	lock, err := turn.Acquire(dir, 15*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	rec, err := c.Begin(lock)
	if err != nil {
		return err
	}
	if err := fill(rec); err != nil {
		rec.Abandon()
		return err
	}
	out, err := rec.Commit()
	if out != vessel.Recorded {
		return fmt.Errorf("home: the ledger's commit was %s: %v", out, err)
	}
	return nil
}

// openLedger opens the home's ledger vessel with the owner's passphrase and
// returns it with the owner's secret.
func openLedger(dir, pass string) (*carrier.Carrier, key.Secret, error) {
	var sec key.Secret
	unlock := func(slots [][]byte, salt []byte, iter int) ([]byte, error) {
		s, vk, err := key.Try(pass, slots, salt, iter)
		sec = s
		return vk, err
	}
	c, _, err := carrier.Open(medium.Dir{Root: dir}, unlock, rand.Reader, nil)
	if err != nil {
		return nil, key.Secret{}, err
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		return nil, key.Secret{}, err
	}
	c.SetSealer(sess)
	return c, sec, nil
}

func loadLedger(c *carrier.Carrier) (*ledger.Ledger, error) {
	heads, err := c.Heads()
	if err != nil {
		return nil, err
	}
	raw, err := c.Get(c.Anchor())
	if err != nil {
		return nil, err
	}
	return ledger.Load(raw, c.Get, heads)
}

// rootOf is the owner's signing key from an opened secret.
func rootOf(s key.Secret) ed25519.PrivateKey { return s.Signer() }
