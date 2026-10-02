// Package seal is content closed for a named reader.
//
// Disclosure is decided when the bundle is sealed, not when it is sent. The
// courier holds no key and is not trusted: whatever it drops, delays,
// reorders or repeats, it changes nothing in what is accepted, and what must
// not travel never reaches it. Sealing is how that stops being a promise —
// the bytes handed to the courier are already closed to everyone but the one
// reader they were closed for.
//
// The key that reads is not the key that writes. Authorship is Ed25519;
// reading is X25519, and they are different keys with different instruments.
// A right to write has never extended into a right to read, and here it
// cannot: there is no way to open a seal with a signing key.
//
//	— T13.1, T7.4, T7.1, N4.8, N4.9
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	// KeySize is an X25519 public key.
	KeySize = 32
	// NonceSize is AES-GCM's nonce.
	NonceSize = 12
)

var (
	// ErrNotForYou is returned when the seal does not open with this key.
	// It is deliberately the same error whether the key is wrong or the
	// bytes were tampered with: a reader who cannot open a seal learns
	// nothing else from it.
	ErrNotForYou = errors.New("seal: this is not open to that key")
	// ErrBadKey is returned for something that is not an X25519 key.
	ErrBadKey = errors.New("seal: not a reading key")
	// ErrShape is returned for a sealed box that is not one.
	ErrShape = errors.New("seal: bad shape")
)

// Reader is a named recipient's reading key.
type Reader struct{ pub *ecdh.PublicKey }

// ReaderFrom takes the 32 bytes a covenant carries and makes a reader of them.
func ReaderFrom(b []byte) (Reader, error) {
	if len(b) != KeySize {
		return Reader{}, fmt.Errorf("%w: %d bytes, want %d", ErrBadKey, len(b), KeySize)
	}
	p, err := ecdh.X25519().NewPublicKey(b)
	if err != nil {
		return Reader{}, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	return Reader{pub: p}, nil
}

// Bytes is the reader's key as it travels in a covenant.
func (r Reader) Bytes() []byte { return r.pub.Bytes() }

// NewReading generates a reading key pair. It is separate from any signing
// key on purpose: entrusting someone to write has never entrusted them to
// read, and keeping the two in different keys is what makes that structural
// rather than a rule somebody has to remember.
//
//	— T7.1, N4.8
func NewReading() (*ecdh.PrivateKey, error) {
	return ecdh.X25519().GenerateKey(rand.Reader)
}

// Sealed is what travels. The ephemeral public key is in the clear — it has
// to be, for the reader to agree on the key — and it is one-time, so two
// seals of the same bytes to the same reader share nothing.
type Sealed struct {
	Ephemeral []byte // KeySize
	Box       []byte // nonce || ciphertext || tag
}

// To closes plain for exactly one reader.
//
// aad is bound into the seal without being hidden: it is what the reader is
// told about the context, and changing any of it makes the seal refuse to
// open. The reader's own key is bound in too, so a box cannot be lifted and
// re-offered as though it had been sealed to somebody else.
//
//	— T13.1, T7.4
func To(r Reader, plain, aad []byte) (Sealed, error) {
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Sealed{}, err
	}
	shared, err := eph.ECDH(r.pub)
	if err != nil {
		return Sealed{}, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	g, err := gcm(shared, eph.PublicKey().Bytes(), r.Bytes())
	if err != nil {
		return Sealed{}, err
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return Sealed{}, err
	}
	box := g.Seal(nonce, nonce, plain, aad)
	return Sealed{Ephemeral: eph.PublicKey().Bytes(), Box: box}, nil
}

// Open reads a seal with the reading key it was closed for. Any other key,
// and any change to the bytes or to the context, gets the same refusal.
func Open(priv *ecdh.PrivateKey, s Sealed, aad []byte) ([]byte, error) {
	if len(s.Ephemeral) != KeySize {
		return nil, fmt.Errorf("%w: ephemeral key is %d bytes", ErrShape, len(s.Ephemeral))
	}
	if len(s.Box) < NonceSize+16 {
		return nil, fmt.Errorf("%w: the box is too short to hold anything", ErrShape)
	}
	eph, err := ecdh.X25519().NewPublicKey(s.Ephemeral)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrShape, err)
	}
	shared, err := priv.ECDH(eph)
	if err != nil {
		return nil, ErrNotForYou
	}
	g, err := gcm(shared, s.Ephemeral, priv.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	nonce, ct := s.Box[:NonceSize], s.Box[NonceSize:]
	plain, err := g.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrNotForYou
	}
	return plain, nil
}

// gcm derives the sealing key from the agreed secret. The ephemeral key and
// the reader's key go into the derivation, so a secret agreed for one reader
// cannot produce the key for another.
func gcm(shared, ephPub, readerPub []byte) (cipher.AEAD, error) {
	info := append(append([]byte("rokh/seal/v1|"), ephPub...), readerPub...)
	key, err := hkdf.Key(sha256.New, shared, nil, string(info), 32)
	if err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// ReadingFrom reads a reading key back from its stored bytes.
//
// It exists so the private half can live where every other secret of a person
// lives — inside their carrier, under their passphrase — and be picked up
// again without this package knowing anything about carriers.
//
//	— T13.1, T8
func ReadingFrom(b []byte) (*ecdh.PrivateKey, error) {
	if len(b) != KeySize {
		return nil, fmt.Errorf("%w: a reading key is %d bytes, got %d",
			ErrBadKey, KeySize, len(b))
	}
	return ecdh.X25519().NewPrivateKey(b)
}
