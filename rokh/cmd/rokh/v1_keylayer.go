package main

// The key layer over an opened carrier is package keyview's (T2): the one
// layer of the command line, the sentence surface and the booths. The
// session's ring is the keyring's fold, an open address is read-open where a
// live open grant says so, and a record opens only if its envelope names the
// owner generations live at its own point. Here are
// the names this command's files use for it.

import (
	"rokh/carrier"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
	"rokh/ledger"
)

// keyLayer is one reading of a carrier's ledger for its key layer.
type keyLayer struct{ *keyview.Layer }

// errOwnerEmptied refuses a point whose owner fold was emptied by
// revocation (R4).
var errOwnerEmptied = keyview.ErrOwnerEmptied

// errNoEventYet is a vessel that holds no event at all (goal 7.41).
var errNoEventYet = keyview.ErrNoEventYet

// ownersOf is the one rule for the owner's reader generations at a point
// (keyview.OwnersOf).
func ownersOf(readers [][]byte, ever, known bool, first []byte) ([][]byte, error) {
	return keyview.OwnersOf(readers, ever, known, first)
}

// ownersAtEvent is ownersOf at an accepted event's own point: its parents.
func ownersAtEvent(l *ledger.Ledger, id frame.ID, first []byte) ([][]byte, error) {
	return keyview.OwnersAtEvent(l, id, first)
}

// readOpenAt is read-open as seen from given points of a ledger; no point is
// its heads.
func readOpenAt(l *ledger.Ledger, addr string, at ...frame.ID) bool {
	return keyview.ReadOpenAt(l, addr, at...)
}

// bootstrapOwner is the owner's first generation as the ledger holds it.
func bootstrapOwner(l *ledger.Ledger) (key.Gen, error) { return keyview.BootstrapOwner(l) }

// holdsEvents says whether a vessel holds any event record.
func holdsEvents(c *carrier.Carrier) (bool, error) { return keyview.HoldsEvents(c) }

// layerOf is the key layer of a ledger already read, answering from it alone.
func layerOf(c *carrier.Carrier, l *ledger.Ledger, cell key.Gen) *keyLayer {
	return &keyLayer{keyview.LayerOf(c, l, cell)}
}

func (k *keyLayer) ownersAt(id frame.ID) ([][]byte, bool) { return k.OwnersAt(id) }
func (k *keyLayer) judge(id frame.ID) string              { return k.Judge(id) }
func (k *keyLayer) covers(id frame.ID, env []byte) bool   { return k.Covers(id, env) }
func (k *keyLayer) readOpen(addr string) bool             { return k.ReadOpen(addr) }
func (k *keyLayer) dress(s *key.Session) error            { return k.Dress(s) }
func (k *keyLayer) ledgerRead() *ledger.Ledger            { return k.Ledger() }
func (k *keyLayer) sessionAt(l *ledger.Ledger, sec key.Secret, ptk []byte, parents []frame.ID) (*key.Session, error) {
	return k.SessionAt(l, sec, ptk, parents)
}

// keyLayerOf opens the owner's session of an opened carrier, or a key's own
// for a key's passphrase, and reads its ledger with it (keyview.Open).
func keyLayerOf(c *carrier.Carrier, pass string) (*key.Session, key.Secret, *keyLayer, error) {
	sess, sec, k, err := keyview.Open(c, pass)
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	return sess, sec, &keyLayer{k}, nil
}

// keySessionOf opens the session of a key that is not the owner, from the
// carrier itself (keyview.KeySessionOf).
func keySessionOf(c *carrier.Carrier, sec key.Secret) (*key.Session, *keyLayer, error) {
	sess, k, err := keyview.KeySessionOf(c, sec)
	if err != nil {
		return nil, nil, err
	}
	return sess, &keyLayer{k}, nil
}

// sealerAt is the session a record this session signs on parents is sealed
// with, judged in l: the owner's at that point for the owner's passphrase,
// the key's own for a key's (contract E3, E4, 3.3).
func (s *session) sealerAt(l *ledger.Ledger, parents []frame.ID) (carrier.Sealer, error) {
	sess, err := s.layer.SealerAt(l, s.sec, s.car.Vessel().SharedKey(), parents)
	if err != nil {
		return nil, err
	}
	return sess, nil
}
