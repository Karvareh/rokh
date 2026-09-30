package key

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"

	"rokh/event"
	"rokh/frame"
)

// The owner's key (contract 4.6): key id zero, whose slot holds the root's
// signing seed and the owner's reader, and which is never placed in an event.

// NewOwner makes the owner's secret — the root's signing seed and a fresh
// reader — and the slot cell the owner's passphrase opens. salt and iter are
// the vessel's; vk is the vessel's key.
func NewOwner(pass string, salt []byte, iter int, rootSeed [32]byte, vk []byte, rnd io.Reader) ([]byte, Secret, error) {
	r, err := NewReader(rnd)
	if err != nil {
		return nil, Secret{}, err
	}
	s := Secret{Gen: 1, SignerSeed: rootSeed}
	copy(s.Reader[:], r.Bytes())
	kk, err := PassKey(pass, salt, iter)
	if err != nil {
		return nil, Secret{}, err
	}
	cell, err := Cell(kk, s, vk, rnd)
	if err != nil {
		return nil, Secret{}, err
	}
	return cell, s, nil
}

// ErrKeyNotLive refuses a session for a key generation the given ring does
// not hold live: revoked, superseded, or never added.
var ErrKeyNotLive = errors.New("key: this key generation is not live in the keyring given; it opens no session")

// KeySession is the session of a key that is not the owner, opened by the
// key's own passphrase (contract 3.3). Its ring, the owner generations at
// each record's point and the system reader's private half are the caller's
// to give, read from the ledger; this package stands nothing in for them.
// It refuses a ring that names no owner, a missing fold of the point, and a
// generation that is not live in the ring, and it never takes the root.
//
// It opens what is sealed to its own reader, or to the system reader when
// given, and only where the envelope names the owner of its point (E3). It
// seals to the readers the ring names for an address, the owner always.
func KeySession(s Secret, ring Ring, ownerAt func(frame.ID) ([][]byte, bool), system *Reader, rnd io.Reader) (*Session, error) {
	if s.Key == ([32]byte{}) {
		return nil, errors.New("key: the owner's secret opens the owner's session, SessionFor")
	}
	if len(ring.Owner()) == 0 {
		return nil, fmt.Errorf("%w: the ring given names no owner generation", ErrOwnerUnknown)
	}
	if ownerAt == nil {
		return nil, fmt.Errorf("%w: a key's session judges each record at its own point, and no fold of it was given", ErrOwnerUnknown)
	}
	r, err := ReaderFrom(s.Reader[:])
	if err != nil {
		return nil, err
	}
	live := false
	for _, g := range ring.Of(s.Key) {
		if g.Gen == s.Gen && bytes.Equal(g.Reader, r.Public()) {
			live = true
		}
	}
	if !live {
		return nil, ErrKeyNotLive
	}
	sess := &Session{Ring: ring, Mine: []Reader{r}, Rand: rnd, OwnerAt: ownerAt}
	if system != nil {
		sess.System = system.Public()
		sess.Mine = append(sess.Mine, *system)
	}
	return sess, nil
}

// SessionFor is the session of one opened secret: it seals every record to
// the owner's reader (E3) and opens with its own. The ring names the owner's
// generation; the keyring's other generations join it through Fold.
func SessionFor(s Secret, rnd io.Reader) (*Session, error) {
	r, err := ReaderFrom(s.Reader[:])
	if err != nil {
		return nil, err
	}
	if s.Key != ([32]byte{}) {
		return nil, errors.New("key: this session is not the owner's; a key's session needs the keyring's owner reader")
	}
	ring := Ring{Live: []Gen{{Keyring: event.Keyring{Op: event.KeyringAdd, Key: s.Key, Gen: s.Gen,
		Name: "owner", Reader: r.Public(), Reads: []string{""}}}}}
	return &Session{Ring: ring, Mine: []Reader{r}, Rand: rnd}, nil
}

// OwnerSession opens the owner's secret from a vessel's cells and returns its
// session.
func OwnerSession(pass string, slots [][]byte, salt []byte, iter int, rnd io.Reader) (*Session, Secret, error) {
	s, _, err := Try(pass, slots, salt, iter)
	if err != nil {
		return nil, Secret{}, err
	}
	sess, err := SessionFor(s, rnd)
	if err != nil {
		return nil, Secret{}, err
	}
	return sess, s, nil
}

// Root is the owner's signing key, or nil in cold custody.
func (s Secret) Root() ed25519.PrivateKey { return s.Signer() }

// VesselSession opens the owner's session for an opened vessel: its cells,
// salt and cost come from the vessel, and pointers are sealed under its
// shared key (E7).
func VesselSession(pass string, slots [][]byte, salt []byte, iter int, ptk []byte, rnd io.Reader) (*Session, Secret, error) {
	sess, sec, err := OwnerSession(pass, slots, salt, iter, rnd)
	if err != nil {
		return nil, Secret{}, err
	}
	sess.PTK = append([]byte(nil), ptk...)
	return sess, sec, nil
}
