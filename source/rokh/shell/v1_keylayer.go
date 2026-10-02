package shell

// The sentence surface's key layer is the command line's (T2): package
// keyview, the one key layer of the command line, the surface and the
// booths. Over an opened carrier, read from its own ledger : the session's ring is the keyring's fold, an open address is
// read-open where a live open grant says so, and a record opens only if its
// envelope names the owner generations live at its own point. The
// passphrase's cell decides whose session it is, as on the command line:
// the owner's, or a key's own, never the owner's for a key.

import (
	"rokh/carrier"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
	"rokh/ledger"
)

// keyLayer is one reading of a carrier's ledger for its key layer.
type keyLayer struct {
	*keyview.Layer
	led *ledger.Ledger
}

// errOwnerEmptied refuses a point whose owner fold was emptied by
// revocation: it is known and empty, and nothing stands in for it (R4).
var errOwnerEmptied = keyview.ErrOwnerEmptied

func (k *keyLayer) judge(id frame.ID) string            { return k.Judge(id) }
func (k *keyLayer) covers(id frame.ID, env []byte) bool { return k.Covers(id, env) }

// keyLayerOf opens the session that the passphrase's cell holds in an
// opened carrier, as the command line's opener does: the owner's cell gives
// the owner's session, dressed from the ledger it reads; a key's cell gives
// that key's own session, never the owner's (keyview.Open).
func keyLayerOf(c *carrier.Carrier, pass string) (*key.Session, key.Secret, *keyLayer, error) {
	sess, sec, k, err := keyview.Open(c, pass)
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	return sess, sec, &keyLayer{Layer: k, led: k.Ledger()}, nil
}

// layerFor is the key layer of a carrier already opened with a secret: the
// owner's for the owner's, the key's own for a key's (keyview).
func layerFor(c *carrier.Carrier, sec key.Secret) (*key.Session, *keyLayer, error) {
	if sec.Key != ([32]byte{}) {
		sess, k, err := keyview.KeySessionOf(c, sec)
		if err != nil {
			return nil, nil, err
		}
		return sess, &keyLayer{Layer: k, led: k.Ledger()}, nil
	}
	sess, _, k, err := keyview.OwnerSessionOf(c, sec)
	if err != nil {
		return nil, nil, err
	}
	return sess, &keyLayer{Layer: k, led: k.Ledger()}, nil
}

// sealerAt is the session a record signed on parents is sealed with, judged
// in l, the ledger the record is about to join: the owner's at that point
// for the owner's passphrase, the key's own for a key's (contract E3, E4).
func (k *keyLayer) sealerAt(l *ledger.Ledger, sec key.Secret, ptk []byte, parents []frame.ID) (carrier.Sealer, error) {
	sess, err := k.SealerAt(l, sec, ptk, parents)
	if err != nil {
		return nil, err
	}
	return sess, nil
}
