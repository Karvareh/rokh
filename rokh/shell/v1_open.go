package shell

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/lineage"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// The sentence surface opens a v1 carrier the way the command line does:
// files through medium, the writer's turn through turn, and the key through
// the key layer. The hooks let the key layer's command wiring give the shell
// a keyring-aware unlock and sealer; without them the shell uses the owner's
// own slot and seals to the owner's reader alone.
var (
	// UnlockV1 turns a passphrase into the vessel's unlock (key.Unlock).
	UnlockV1 func(pass string) vessel.Unlock
	// SealerV1 is the session sealer of an opened carrier (key.Session).
	SealerV1 func(c *carrier.Carrier, pass string) (carrier.Sealer, error)
	// JudgeV1 gives the key layer's judge and owner check for an opened
	// carrier, as cmd/rokh's v1Judge does. While it is nil, uniting with a
	// berth is refused with ancestry_unproven.
	JudgeV1 func(c *carrier.Carrier, pass string) (judge func(frame.ID) string, covers func(id frame.ID, env []byte) bool, err error)
)

// judgedSide is one side of a union with the key layer's judge and owner
// check.
func judgedSide(c *carrier.Carrier, own vessel.Owner, pass string) (lineage.Side, error) {
	if JudgeV1 == nil {
		return lineage.Side{}, fmt.Errorf("%w: the key layer gives no judge in this build", lineage.ErrAncestryUnproven)
	}
	judge, covers, err := JudgeV1(c, pass)
	if err != nil {
		return lineage.Side{}, err
	}
	if judge == nil || covers == nil {
		return lineage.Side{}, lineage.ErrAncestryUnproven
	}
	// The owner check travels with the side and is applied by the sender to
	// every envelope it offers, against the ledger that holds the record
	// (lineage.offer). The receiving carrier is not given it for union: an
	// arriving event is not yet in its ledger, so it cannot place the point.
	return lineage.Side{C: c, Own: own, Judge: judge, Covers: covers}, nil
}

// iterations is the owner passphrase's work factor for a new rokh. Tests
// lower it through setIterations; real use never does.
var iterations = vessel.DefaultIter

// ownerSealer seals every record to the owner's reader (suite 0x01) and
// opens with it. It is the sealer of a session that holds the owner's key
// and no keyring-aware sealer.
type ownerSealer struct{ r key.Reader }

func (o ownerSealer) SealAt(kind byte, id frame.ID, address string, at, plain []byte) ([]byte, error) {
	return key.SealReaders(kind, id, at, [][]byte{o.r.Public()}, plain, rand.Reader)
}

func (o ownerSealer) OpenAt(kind byte, id frame.ID, at, env []byte) ([]byte, error) {
	return key.OpenReaders(kind, id, at, env, o.r)
}

func (o ownerSealer) Seal(kind byte, id frame.ID, address string, plain []byte) ([]byte, error) {
	return o.SealAt(kind, id, address, nil, plain)
}

func (o ownerSealer) Open(kind byte, id frame.ID, env []byte) ([]byte, error) {
	return o.OpenAt(kind, id, nil, env)
}

var errNoKeyLayer = errors.New("the key layer is not wired into this build; nothing was recorded")

// mediumFor is the files of the carrier in dir. It is a variable only so
// that a test can put a failing disk under a real session and see what the
// surface says and how it ends; nothing else changes it.
var mediumFor = func(dir string) vessel.Medium { return medium.Dir{Root: dir} }

// isCarrier reports whether dir holds a v1 vessel. It reads no content.
func isCarrier(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(turn.HeadFile)))
	return err == nil
}

func unlockFor(pass string) vessel.Unlock {
	if UnlockV1 != nil {
		return UnlockV1(pass)
	}
	return key.Unlock(pass)
}

// openCarrier opens the v1 carrier in dir with a passphrase and returns the
// key the passphrase holds there.
func (s *session) openCarrier(dir, pass string) (*carrier.Carrier, key.Secret, error) {
	c, rep, err := carrier.Open(mediumFor(dir), unlockFor(pass), rand.Reader, nil)
	if err != nil {
		return nil, key.Secret{}, err
	}
	for _, f := range rep.FellBack {
		if s.notes != nil {
			fmt.Fprintf(s.notes, "(generation %d of %s did not verify; opened %d)\n", f.From, dir, rep.Generation)
		}
	}
	info := c.Vessel().Info()
	sec, _, err := key.Try(pass, c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		return nil, key.Secret{}, err
	}
	if SealerV1 != nil {
		sl, err := SealerV1(c, pass)
		if err != nil {
			return nil, key.Secret{}, err
		}
		c.SetSealer(sl)
	} else {
		r, err := key.ReaderFrom(sec.Reader[:])
		if err != nil {
			return nil, key.Secret{}, err
		}
		c.SetSealer(ownerSealer{r: r})
	}
	return c, sec, nil
}

// openLayered is openCarrier with the key layer the passphrase opened: its
// session is the carrier's sealer, and the layer judges each record at its
// own point and gives the session a record is sealed with (T2). The layer is
// read from the secret openCarrier already opened; the passphrase is not
// derived again.
func (s *session) openLayered(dir, pass string) (*carrier.Carrier, key.Secret, *keyLayer, error) {
	c, sec, err := s.openCarrier(dir, pass)
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	sess, k, err := layerFor(c, sec)
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	c.SetSealer(sess)
	return c, sec, k, nil
}

// createCarrier makes a new v1 carrier in dir whose owner is the passphrase:
// its slot holds the root signing seed and the owner's reader. The genesis
// is recorded by the caller.
func createCarrier(dir, pass string, anchor frame.ID, root ed25519.PrivateKey) (*carrier.Carrier, key.Secret, error) {
	return createCarrierWith(dir, pass, anchor, root, nil)
}

// createCarrierWith is createCarrier for a vessel of an existing ledger (a
// berth): its owner slot keeps the ledger's own owner reader, so what either
// side seals names the same owner and passes the owner check (E3). A nil
// reader makes a new one, for a new ledger.
func createCarrierWith(dir, pass string, anchor frame.ID, root ed25519.PrivateKey, owner *key.Reader) (*carrier.Carrier, key.Secret, error) {
	var reader key.Reader
	if owner != nil {
		reader = *owner
	} else {
		r, err := key.NewReader(rand.Reader)
		if err != nil {
			return nil, key.Secret{}, err
		}
		reader = r
	}
	vk := make([]byte, 32)
	salt := make([]byte, 32)
	if _, err := rand.Read(vk); err != nil {
		return nil, key.Secret{}, err
	}
	if _, err := rand.Read(salt); err != nil {
		return nil, key.Secret{}, err
	}
	kk, err := key.PassKey(pass, salt, iterations)
	if err != nil {
		return nil, key.Secret{}, err
	}
	sec := key.Secret{Gen: 1}
	copy(sec.SignerSeed[:], root.Seed())
	copy(sec.Reader[:], reader.Bytes())
	cell, err := key.Cell(kk, sec, vk, rand.Reader)
	if err != nil {
		return nil, key.Secret{}, err
	}
	p := vessel.Params{Iter: iterations, Salt: salt, Rand: rand.Reader}
	// The size the person chose at the gate, when this carrier is the one
	// the gate is making; otherwise the vessel's own default.
	if room, ok := roomFor(dir); ok {
		p.SlabLog2, p.Slabs, p.Growth = room.SlabLog2, room.Slabs, room.Growth
	}
	c, err := carrier.Create(mediumFor(dir), p,
		vk, [][]byte{cell}, anchor, frame.Zero, nil, ownerSealer{r: reader})
	if err != nil {
		return nil, key.Secret{}, err
	}
	return c, sec, nil
}

// record commits one signed event and moves the branch onto it, in one vessel
// commit, under the writer's turn the caller holds.
func record(c *carrier.Carrier, lock *turn.Lock, e event.Signed, branch string) (vessel.Outcome, error) {
	return recordWith(c, lock, e, branch, nil)
}

// recordWith is record with more records put into the same commit first.
func recordWith(c *carrier.Carrier, lock *turn.Lock, e event.Signed, branch string, extra func(*carrier.Recording) error) (vessel.Outcome, error) {
	return recordSealed(c, lock, e, branch, extra, nil)
}

// recordSealed is recordWith sealed by sealer, a session at the event's own
// point, when one is given; by the carrier's sealer otherwise.
func recordSealed(c *carrier.Carrier, lock *turn.Lock, e event.Signed, branch string, extra func(*carrier.Recording) error, sealer carrier.Sealer) (vessel.Outcome, error) {
	r, err := c.Begin(lock)
	if err != nil {
		return vessel.NotRecorded, err
	}
	if sealer != nil {
		r.SealWith(sealer)
	}
	if extra != nil {
		if err := extra(r); err != nil {
			r.Abandon()
			return vessel.NotRecorded, err
		}
	}
	if err := r.Event(e.ID, e.Head, e.Body, e.Event.Address); err != nil {
		r.Abandon()
		return vessel.NotRecorded, err
	}
	if err := r.SetRef(branch, e.ID); err != nil {
		r.Abandon()
		return vessel.NotRecorded, err
	}
	return r.Commit()
}

// OpenCarrier opens a v1 carrier with a passphrase for another program of
// Rokh (the courier) and returns the key the passphrase holds there.
func OpenCarrier(dir, pass string) (*carrier.Carrier, key.Secret, error) {
	return (&session{}).openCarrier(dir, pass)
}
