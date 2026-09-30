// Package store is a Rokh home's sealed storage, format rokh-home/2: the home
// keeps no store of its own beside its ledger's vessel (contract 2.10). What
// the 0.9 home kept in home.json, objects/ and pointers/ is now object and
// pointer records of the home's spaces (0x10..0x1F) in the same vessel as the
// home's ledger, sealed under suite 0x02 with keys the vessel derives, every
// chunk bound to its coordinates, every write one vessel commit under the
// writer's turn (package vesselstore).
//
// The owner's proof key, with which the owner's channel proves the passphrase
// to a running gate, is derived from the passphrase with the vessel's own
// salt and cost; the passphrase never crosses the socket.
package store

import (
	"bytes"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"path/filepath"
	"sync"

	"rokh-home/vesselstore"
	"rokh/key"
	"rokh/medium"
	"rokh/vessel"
)

// Layout and costs.
const (
	Format      = "rokh-home/2"
	LedgerDir   = "ledger"
	DefaultIter = 600000
)

var (
	ErrExists     = errors.New("store: a home is already here")
	ErrNoHome     = errors.New("store: not a Rokh home")
	ErrPass       = errors.New("store: wrong passphrase")
	ErrCorrupt    = errors.New("store: sealed data does not open")
	ErrTruncated  = errors.New("store: the stream ends before its last segment")
	ErrNotFound   = errors.New("store: not found")
	ErrMismatch   = errors.New("store: the bytes do not hash to the name they were given")
	ErrDurability = errors.New("store: publication durability is unknown; close and reopen before continuing")
)

// Info describes a stored object without its bytes.
type Info struct {
	Size   int64
	SHA256 [32]byte
}

// Home is an opened home's storage.
type Home struct {
	mu       sync.Mutex
	root     string
	v        *vessel.Vessel
	vs       *vesselstore.Store
	vk       []byte
	ownerKey []byte
}

func ownerKeyOf(pass string, info vessel.Info) ([]byte, error) {
	kk, err := key.PassKey(pass, info.Salt, info.Iter)
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, kk, nil, "rokh-home/1/owner", 32)
}

// On opens the home's storage over its ledger vessel, already opened with the
// owner's passphrase.
func On(root string, v *vessel.Vessel, pass string) (*Home, error) {
	info := v.Info()
	owner, err := ownerKeyOf(pass, info)
	if err != nil {
		return nil, err
	}
	k := bits.TrailingZeros(uint(info.SlabSize))
	vs, err := vesselstore.New(vesselstore.Vessel{V: v, Dir: filepath.Join(root, LedgerDir)}, v.SharedKey(),
		vessel.MaxChunk(k)-256, rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Home{root: root, v: v, vs: vs, vk: v.VK(), ownerKey: owner}, nil
}

// OwnerKey derives the owner's proof key from the passphrase and the home's
// ledger vessel: the vessel is opened with the passphrase, which also proves
// it, and nothing is written.
func OwnerKey(root, pass string) ([]byte, error) {
	v, _, err := vessel.Open(medium.Dir{Root: filepath.Join(root, LedgerDir)}, vessel.Unlock(key.Unlock(pass)), rand.Reader)
	if err != nil {
		if errors.Is(err, key.ErrPassphrase) {
			return nil, fmt.Errorf("%w: %v", ErrPass, err)
		}
		return nil, err
	}
	return ownerKeyOf(pass, v.Info())
}

// Root is the home's folder.
func (h *Home) Root() string { return h.root }

// SubKey derives a named key from the vessel's key for a layer above the
// store, the registry's credential verifier for one.
func (h *Home) SubKey(name string) ([]byte, error) {
	if err := h.Health(); err != nil {
		return nil, err
	}
	if name == "" || len(name) > 512 {
		return nil, errors.New("store: bad key name")
	}
	return hkdf.Key(sha256.New, h.vk, nil, "rokh-home/2/sub/"+name, 32)
}

// OwnerProofKey is the key the owner's proofs are checked with.
func (h *Home) OwnerProofKey() []byte { return append([]byte(nil), h.ownerKey...) }

// Secret returns a named secret the home keeps sealed.
func (h *Home) Secret(name string) ([]byte, bool) {
	v, ok, err := h.Pointer("secret/" + name)
	if err != nil || !ok {
		return nil, false
	}
	return v, true
}

// SetSecrets stores named secrets; the passphrase is asked again and must be
// the owner's.
func (h *Home) SetSecrets(pass string, kv map[string][]byte) error {
	owner, err := ownerKeyOf(pass, h.v.Info())
	if err != nil {
		return err
	}
	if !bytes.Equal(owner, h.ownerKey) {
		return ErrPass
	}
	for k, v := range kv {
		if err := h.SetPointer("secret/"+k, v); err != nil {
			return err
		}
	}
	return nil
}

// Close zeroes the keys held in memory.
func (h *Home) Close() {
	for _, b := range [][]byte{h.vk, h.ownerKey} {
		for i := range b {
			b[i] = 0
		}
	}
}

// Health is non-nil once a commit's ending was not learned: the home must be
// opened again before it serves anything.
func (h *Home) Health() error {
	if err := h.vs.Health(); err != nil {
		return fmt.Errorf("%w: %v", ErrDurability, err)
	}
	return nil
}

func typed(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, vesselstore.ErrUnknown):
		return fmt.Errorf("%w: %v", ErrDurability, err)
	case errors.Is(err, vesselstore.ErrMismatch):
		return ErrMismatch
	case errors.Is(err, vesselstore.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, vesselstore.ErrCorrupt), errors.Is(err, vesselstore.ErrIncomplete), errors.Is(err, vesselstore.ErrConflict):
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return err
}

// Has says whether an object exists.
func (h *Home) Has(kind, id string) (bool, error) {
	if err := h.Health(); err != nil {
		return false, err
	}
	ok, err := h.vs.Has(kind, id)
	return ok, typed(err)
}

// Put stores an object, immutable once stored; with wantSHA256 the bytes must
// hash to it.
func (h *Home) Put(kind, id string, r io.Reader, wantSHA256 []byte) (Info, error) {
	if err := h.Health(); err != nil {
		return Info{}, err
	}
	n, sum, err := h.vs.Put(kind, id, r, wantSHA256)
	if err != nil {
		return Info{}, typed(err)
	}
	return Info{Size: n, SHA256: sum}, nil
}

// Get opens an object; it is served only after all of it opened.
func (h *Home) Get(kind, id string) (io.ReadCloser, error) {
	if err := h.Health(); err != nil {
		return nil, err
	}
	b, err := h.vs.Get(kind, id)
	if err != nil {
		return nil, typed(err)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// SetPointer replaces a small sealed record.
func (h *Home) SetPointer(name string, value []byte) error {
	if err := h.Health(); err != nil {
		return err
	}
	return typed(h.vs.SetPointer(name, value))
}

// Pointer reads a small sealed record.
func (h *Home) Pointer(name string) ([]byte, bool, error) {
	if err := h.Health(); err != nil {
		return nil, false, err
	}
	v, ok, err := h.vs.Pointer(name)
	return v, ok, typed(err)
}

// Create makes a home's storage on its own: a ledger vessel at root/ledger
// whose owner cell opens with pass and holds no signing seed, and the store
// over it. A home made with the home package creates its vessel with its
// genesis and uses On; this is for a store with no ledger of its own (the
// tests of the layers above it). The iteration count may only be lowered by
// tests.
func Create(root, pass string, iter ...int) (*Home, error) {
	if pass == "" {
		return nil, errors.New("store: an empty passphrase is not allowed")
	}
	dir := filepath.Join(root, LedgerDir)
	m := medium.Dir{Root: dir}
	if err := mkdir(dir); err != nil {
		return nil, err
	}
	p := vessel.Params{SlabLog2: 20, Slabs: 64, Iter: DefaultIter, Rand: rand.Reader,
		Growth: vessel.Growth{Auto: true, Step: 64, Max: 4096}}
	if len(iter) == 1 && iter[0] > 0 {
		p.SlabLog2, p.Slabs, p.Iter = 18, 16, iter[0]
		p.Growth = vessel.Growth{Auto: true, Step: 16, Max: 1024}
	}
	salt := make([]byte, 32)
	vk := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, vk); err != nil {
		return nil, err
	}
	p.Salt = salt
	cell, _, err := key.NewOwner(pass, salt, p.Iter, [32]byte{}, vk, rand.Reader)
	if err != nil {
		return nil, err
	}
	v, err := vessel.Create(m, p, vk, [][]byte{cell}, vessel.Root{})
	if err != nil {
		return nil, err
	}
	return On(root, v, pass)
}

// Open opens a home's storage on its own, with the owner's passphrase.
func Open(root, pass string) (*Home, error) {
	v, _, err := vessel.Open(medium.Dir{Root: filepath.Join(root, LedgerDir)}, vessel.Unlock(key.Unlock(pass)), rand.Reader)
	if err != nil {
		if errors.Is(err, key.ErrPassphrase) {
			return nil, fmt.Errorf("%w: %v", ErrPass, err)
		}
		return nil, err
	}
	return On(root, v, pass)
}
