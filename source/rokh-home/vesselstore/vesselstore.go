// Package vesselstore is the home's storage on the vessel (contract 2.10): no
// store of its own beside the vessel. What the 0.9 home kept as home.json,
// objects/ and pointers/ become records of spaces 0x10 to 0x1F in the same
// vessel as the home's ledger:
//
//	object  (0x04) space || name(32) || chunk || chunks || size || envelope   immutable
//	pointer (0x03) space || name(32) || envelope                            the latest commit wins
//
// Names are HMAC-SHA256 under the home's name key, so neither a kind nor an
// identifier can be read off a record. Every envelope is suite 0x02 under a key
// the vessel's shared key derives, and every chunk binds its coordinates
// (C10): a chunk moved, dropped, repeated or cut does not open, and an object
// is served only after all of it has hashed to what was stored.
//
// It is written against the Records interface. Memory is an in-memory
// implementation for tests; the vessel's Tx and Scan/Body replace
// it when pkg/vessel is merged.
package vesselstore

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"rokh/frame"
	"rokh/key"
)

// Spaces of the home (contract 2.5).
const (
	SpaceBlob    byte = 0x10
	SpaceTree    byte = 0x11
	SpaceSheet   byte = 0x12
	SpaceRecord  byte = 0x13
	SpaceObject  byte = 0x14 // any other kind
	SpacePointer byte = 0x18
	SpaceJournal byte = 0x19
	SpaceSecret  byte = 0x1F
)

var (
	ErrNotFound   = errors.New("vesselstore: not found")
	ErrMismatch   = errors.New("vesselstore: the bytes do not hash to the name they were given")
	ErrIncomplete = errors.New("vesselstore: content_incomplete: a chunk is missing")
	ErrConflict   = errors.New("vesselstore: content_conflict: two chunks claim one place with different bytes")
	ErrCorrupt    = errors.New("vesselstore: a record does not open")
)

// Chunk is one stored chunk of an object.
type Chunk struct {
	Chunk, Chunks uint32
	Size          uint64
	Envelope      []byte
}

// Records is the part of a vessel the home needs: immutable objects in chunks
// and mutable pointers, each in a space, under a 32-byte name.
type Records interface {
	// PutObject stores every chunk of one object in one commit.
	PutObject(space byte, name [32]byte, chunks []Chunk) error
	Object(space byte, name [32]byte) ([]Chunk, error)
	SetPointer(space byte, name [32]byte, envelope []byte) error
	Pointer(space byte, name [32]byte) ([]byte, bool, error)
}

// Store is the home's storage over a vessel's records.
type Store struct {
	rec       Records
	nameKey   []byte
	sealKey   []byte
	chunk     int
	rnd       io.Reader
	mu        sync.Mutex
	uncertain error
}

// New makes the store over records, with keys derived from the vessel's
// shared key, chunks of at most chunk bytes, and injected randomness.
func New(rec Records, sharedKey []byte, chunk int, rnd io.Reader) (*Store, error) {
	if chunk < 1 {
		return nil, errors.New("vesselstore: a chunk holds at least one byte")
	}
	nk, err := hkdf.Key(sha256.New, sharedKey, nil, "rokh-home/v1/names", 32)
	if err != nil {
		return nil, err
	}
	sk, err := hkdf.Key(sha256.New, sharedKey, nil, "rokh-home/v1/records", 32)
	if err != nil {
		return nil, err
	}
	return &Store{rec: rec, nameKey: nk, sealKey: sk, chunk: chunk, rnd: rnd}, nil
}

func (s *Store) name(parts ...string) [32]byte {
	m := hmac.New(sha256.New, s.nameKey)
	for _, p := range parts {
		m.Write([]byte(p))
		m.Write([]byte{0})
	}
	var n [32]byte
	copy(n[:], m.Sum(nil))
	return n
}

func spaceOf(kind string) byte {
	switch kind {
	case "blob":
		return SpaceBlob
	case "tree":
		return SpaceTree
	case "sheet":
		return SpaceSheet
	case "record":
		return SpaceRecord
	}
	return SpaceObject
}

// Health is non-nil once a publication's durability became unknown.
func (s *Store) Health() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uncertain
}

// Has says whether an object exists.
func (s *Store) Has(kind, id string) (bool, error) {
	cs, err := s.rec.Object(spaceOf(kind), s.name("object", kind, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	return len(cs) > 0, nil
}

// Put stores an object in chunks, each sealed with its coordinates bound in.
// When wantSHA256 is given, the bytes must hash to it.
func (s *Store) Put(kind, id string, r io.Reader, wantSHA256 []byte) (int64, [32]byte, error) {
	var sum [32]byte
	body, err := io.ReadAll(r)
	if err != nil {
		return 0, sum, err
	}
	sum = sha256.Sum256(body)
	if wantSHA256 != nil && !bytes.Equal(sum[:], wantSHA256) {
		return 0, sum, ErrMismatch
	}
	space, name := spaceOf(kind), s.name("object", kind, id)
	if cs, err := s.rec.Object(space, name); err == nil && len(cs) > 0 {
		// Immutable: an existing object is verified, never replaced.
		got, err := s.assemble(space, name, cs)
		if err != nil {
			return 0, sum, err
		}
		if !bytes.Equal(got, body) {
			return 0, sum, ErrMismatch
		}
		return int64(len(body)), sum, nil
	}
	n := (len(body) + s.chunk - 1) / s.chunk
	if n == 0 {
		n = 1
	}
	var chunks []Chunk
	for i := 0; i < n; i++ {
		lo, hi := i*s.chunk, (i+1)*s.chunk
		if hi > len(body) {
			hi = len(body)
		}
		part := body[lo:hi]
		at := key.At(uint32(i), uint32(n), uint64(len(body)), uint32(len(part)))
		env, err := key.SealShared(key.TypeObject, frame.ID(name), at, s.sealKey, part, s.rnd)
		if err != nil {
			return 0, sum, err
		}
		chunks = append(chunks, Chunk{Chunk: uint32(i), Chunks: uint32(n), Size: uint64(len(body)), Envelope: env})
	}
	if err := s.rec.PutObject(space, name, chunks); err != nil {
		s.note(err)
		return 0, sum, err
	}
	return int64(len(body)), sum, nil
}

// Get returns an object, served only after every chunk has opened at its own
// coordinates and the whole has been assembled (E8).
func (s *Store) Get(kind, id string) ([]byte, error) {
	space, name := spaceOf(kind), s.name("object", kind, id)
	cs, err := s.rec.Object(space, name)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, ErrNotFound
	}
	return s.assemble(space, name, cs)
}

func (s *Store) assemble(space byte, name [32]byte, cs []Chunk) ([]byte, error) {
	chunks, size := cs[0].Chunks, cs[0].Size
	if chunks == 0 {
		return nil, ErrCorrupt
	}
	want := uint64(s.chunk)
	if exp := (size + want - 1) / want; size > 0 && exp != uint64(chunks) || size == 0 && chunks != 1 {
		return nil, ErrCorrupt
	}
	parts := make([][]byte, chunks)
	for _, c := range cs {
		if c.Chunks != chunks || c.Size != size || c.Chunk >= chunks {
			return nil, ErrCorrupt
		}
		length := want
		if c.Chunk == chunks-1 {
			length = size - uint64(c.Chunk)*want
		}
		at := key.At(c.Chunk, c.Chunks, c.Size, uint32(length))
		plain, err := key.OpenShared(key.TypeObject, frame.ID(name), at, c.Envelope, s.sealKey)
		if err != nil {
			return nil, fmt.Errorf("%w: chunk %d: %v", ErrCorrupt, c.Chunk, err)
		}
		if parts[c.Chunk] != nil && !bytes.Equal(parts[c.Chunk], plain) {
			return nil, ErrConflict
		}
		parts[c.Chunk] = plain
	}
	var out []byte
	for i, p := range parts {
		if p == nil {
			return nil, fmt.Errorf("%w: chunk %d of %d", ErrIncomplete, i, chunks)
		}
		out = append(out, p...)
	}
	if uint64(len(out)) != size {
		return nil, ErrCorrupt
	}
	return out, nil
}

// SetPointer replaces a small sealed record; the latest commit wins.
func (s *Store) SetPointer(name string, value []byte) error {
	n := s.name("pointer", name)
	env, err := key.SealShared(key.TypePointer, frame.ID(n), nil, s.sealKey, value, s.rnd)
	if err != nil {
		return err
	}
	if err := s.rec.SetPointer(SpacePointer, n, env); err != nil {
		s.note(err)
		return err
	}
	return nil
}

// ErrUnknown is a commit whose ending was not learned: the store refuses
// everything after it until it is opened again (typed durability unknown).
var ErrUnknown = errors.New("vesselstore: durability_unknown: the commit's ending was not learned; open the home again")

func (s *Store) note(err error) {
	if errors.Is(err, ErrUnknown) {
		s.mu.Lock()
		if s.uncertain == nil {
			s.uncertain = err
		}
		s.mu.Unlock()
	}
}

// Pointer reads a small sealed record.
func (s *Store) Pointer(name string) ([]byte, bool, error) {
	n := s.name("pointer", name)
	env, ok, err := s.rec.Pointer(SpacePointer, n)
	if err != nil || !ok {
		return nil, ok, err
	}
	v, err := key.OpenShared(key.TypePointer, frame.ID(n), nil, env, s.sealKey)
	if err != nil {
		return nil, false, ErrCorrupt
	}
	return v, true, nil
}

// Memory is Records in memory: for tests, until the vessel is merged.
type Memory struct {
	mu       sync.Mutex
	objects  map[string][]Chunk
	pointers map[string][]byte
}

// NewMemory makes empty records.
func NewMemory() *Memory {
	return &Memory{objects: map[string][]Chunk{}, pointers: map[string][]byte{}}
}

func mk(space byte, name [32]byte) string { return string(append([]byte{space}, name[:]...)) }

func (m *Memory) PutObject(space byte, name [32]byte, chunks []Chunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := mk(space, name)
	for _, c := range chunks {
		m.objects[k] = append(m.objects[k], Chunk{Chunk: c.Chunk, Chunks: c.Chunks, Size: c.Size, Envelope: append([]byte(nil), c.Envelope...)})
	}
	return nil
}

func (m *Memory) Object(space byte, name [32]byte) ([]Chunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cs := m.objects[mk(space, name)]
	if len(cs) == 0 {
		return nil, ErrNotFound
	}
	out := append([]Chunk(nil), cs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Chunk < out[j].Chunk })
	return out, nil
}

func (m *Memory) SetPointer(space byte, name [32]byte, env []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pointers[mk(space, name)] = append([]byte(nil), env...)
	return nil
}

func (m *Memory) Pointer(space byte, name [32]byte) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.pointers[mk(space, name)]
	return append([]byte(nil), v...), ok, nil
}

// Tamper is for tests: it lets a test swap, drop or rewrite stored chunks.
func (m *Memory) Tamper(fn func(objects map[string][]Chunk)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.objects)
}
