package key

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"rokh/event"
	"rokh/frame"
)

// stream is a fixed random stream: SHA-256 of a counter. The same stream
// gives the same bytes on every platform (C5).
type stream struct {
	seed byte
	n    uint64
	buf  []byte
}

func (s *stream) Read(p []byte) (int, error) {
	for i := range p {
		if len(s.buf) == 0 {
			var b [9]byte
			b[0] = s.seed
			binary.BigEndian.PutUint64(b[1:], s.n)
			s.n++
			h := sha256.Sum256(b[:])
			s.buf = h[:]
		}
		p[i] = s.buf[0]
		s.buf = s.buf[1:]
	}
	return len(p), nil
}

func reader(t *testing.T, seed byte) Reader {
	t.Helper()
	r, err := NewReader(&stream{seed: seed})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func gen(key [32]byte, g uint32, r Reader, reads []string) Gen {
	return Gen{Keyring: event.Keyring{Op: event.KeyringAdd, Key: key, Gen: g, Name: "k", Reader: r.Public(), Reads: reads}}
}

func TestEnvelopeOpensForEachNamedReaderAndNoOther(t *testing.T) {
	a, b, c := reader(t, 1), reader(t, 2), reader(t, 3)
	id := frame.Hash([]byte("an event"))
	env, err := SealReaders(TypeEvent, id, nil, [][]byte{a.Public(), b.Public()}, []byte("body"), &stream{seed: 9})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []Reader{a, b} {
		got, err := OpenReaders(TypeEvent, id, nil, env, r)
		if err != nil || string(got) != "body" {
			t.Fatalf("a named reader did not open: %v", err)
		}
	}
	if _, err := OpenReaders(TypeEvent, id, nil, env, c); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("an unnamed reader: %v", err)
	}
	if _, err := OpenReaders(TypeEvent, frame.Hash([]byte("another id")), nil, env, a); !errors.Is(err, ErrForged) {
		t.Fatalf("an envelope moved to another id opened: %v", err)
	}
}

func TestTheSameInputsAndStreamGiveTheSameBytes(t *testing.T) {
	a, b := reader(t, 1), reader(t, 2)
	id := frame.Hash([]byte("x"))
	e1, _ := SealReaders(TypeContent, id, At(0, 1, 4, 4), [][]byte{b.Public(), a.Public()}, []byte("data"), &stream{seed: 7})
	e2, _ := SealReaders(TypeContent, id, At(0, 1, 4, 4), [][]byte{a.Public(), b.Public(), a.Public()}, []byte("data"), &stream{seed: 7})
	if !bytes.Equal(e1, e2) {
		t.Fatal("reader order or repetition changed the envelope")
	}
	sum := sha256.Sum256(e1)
	const want = "vector: two readers, content chunk 0 of 1"
	t.Logf("%s: sha256 %s", want, hex.EncodeToString(sum[:]))
}

func TestAForgedEnvelopeAndAForgedRecipientDoNotOpen(t *testing.T) {
	a, b := reader(t, 1), reader(t, 2)
	id := frame.Hash([]byte("x"))
	env, _ := SealReaders(TypeEvent, id, nil, [][]byte{a.Public(), b.Public()}, []byte("body"), &stream{seed: 5})
	flipped := append([]byte(nil), env...)
	flipped[len(flipped)-1] ^= 1
	if _, err := OpenReaders(TypeEvent, id, nil, flipped, a); !errors.Is(err, ErrForged) {
		t.Fatalf("a flipped tag: %v", err)
	}
	// A forged recipient entry: the wrapped key of the first entry is replaced.
	forged := append([]byte(nil), env...)
	off := 4 + 32 + KidSize + 12
	for i := 0; i < 48; i++ {
		forged[off+i] ^= 0x5A
	}
	kids, _ := Kids(env)
	first := a
	if kids[0] != a.Kid() {
		first = b
	}
	if _, err := OpenReaders(TypeEvent, id, nil, forged, first); !errors.Is(err, ErrForged) {
		t.Fatalf("a forged recipient entry: %v", err)
	}
	// An unknown version or suite is unread, not invalid.
	unknown := append([]byte(nil), env...)
	unknown[1] = 0x7F
	if _, err := OpenReaders(TypeEvent, id, nil, unknown, a); !errors.Is(err, ErrUnread) {
		t.Fatalf("an unknown suite: %v", err)
	}
}

func TestAChunkIsBoundToItsCoordinates(t *testing.T) {
	a := reader(t, 1)
	id := frame.Hash([]byte("whole content"))
	env, _ := SealReaders(TypeContent, id, At(1, 3, 10, 3), [][]byte{a.Public()}, []byte("abc"), &stream{seed: 3})
	if _, err := OpenReaders(TypeContent, id, At(2, 3, 10, 3), env, a); !errors.Is(err, ErrForged) {
		t.Fatalf("a chunk moved to another index opened: %v", err)
	}
	if _, err := OpenReaders(TypeContent, id, At(1, 3, 10, 3), env, a); err != nil {
		t.Fatal(err)
	}
}

func TestSharedEnvelope(t *testing.T) {
	k := bytes.Repeat([]byte{7}, 32)
	id := frame.Hash([]byte("main"))
	env, err := SealShared(TypePointer, id, nil, k, []byte("head id"), &stream{seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenShared(TypePointer, id, nil, env, k); err != nil || string(got) != "head id" {
		t.Fatalf("shared envelope: %v", err)
	}
	if _, err := OpenShared(TypePointer, id, nil, env, bytes.Repeat([]byte{8}, 32)); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("another shared key: %v", err)
	}
}

// Read-only, write-only and no-right keys; two overlapping views; the owner
// named in every envelope; a revoked generation left out of what is written
// after the revoke.
func TestTheRingSaysWhoReadsWhere(t *testing.T) {
	owner, ro, wo, none, other := reader(t, 1), reader(t, 2), reader(t, 3), reader(t, 4), reader(t, 5)
	r := Ring{Live: []Gen{
		gen([32]byte{}, 1, owner, []string{""}),
		gen([32]byte{1}, 1, ro, []string{"journal"}),
		gen([32]byte{2}, 1, wo, nil), // write-only: reads nothing
		gen([32]byte{3}, 1, none, nil),
		gen([32]byte{4}, 1, other, []string{"journal/work", "mail"}),
	}}
	has := func(rs [][]byte, x Reader) bool {
		for _, p := range rs {
			if bytes.Equal(p, x.Public()) {
				return true
			}
		}
		return false
	}
	j := r.Readers("journal/work/today", nil, false)
	if !has(j, owner) || !has(j, ro) || !has(j, other) || has(j, wo) || has(j, none) {
		t.Fatalf("overlapping views at journal/work: %d readers", len(j))
	}
	m := r.Readers("mail", nil, false)
	if !has(m, owner) || has(m, ro) || !has(m, other) {
		t.Fatal("independent view at mail")
	}
	s := &Session{Ring: r, Mine: []Reader{ro}, Rand: &stream{seed: 11}}
	id := frame.Hash([]byte("e"))
	env, err := s.Seal(TypeEvent, id, "journal/x", []byte("dear diary"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Open(TypeEvent, id, env); err != nil || string(got) != "dear diary" {
		t.Fatalf("the read-only key could not read its own view: %v", err)
	}
	wos := &Session{Ring: r, Mine: []Reader{wo}}
	if _, err := wos.Open(TypeEvent, id, env); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("a write-only key read: %v", err)
	}
	// E3: an envelope that does not name the owner is refused at opening.
	// Judged by the present ring alone, the point it was sealed at is what is
	// missing; at a given point, the owner is.
	noOwner, _ := SealReaders(TypeEvent, id, nil, [][]byte{ro.Public()}, []byte("x"), &stream{seed: 12})
	if _, err := s.Open(TypeEvent, id, noOwner); !errors.Is(err, ErrOwnerUnknown) {
		t.Fatalf("an envelope without the owner, no point given: %v", err)
	}
	at := &Session{Ring: r, Mine: []Reader{ro}, OwnerAt: func(frame.ID) ([][]byte, bool) { return r.Owner(), true }}
	if _, err := at.Open(TypeEvent, id, noOwner); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("an envelope without the owner of its point: %v", err)
	}
	// After the revoke of ro, the fold no longer names it: what is written
	// after the revoke does not open to it.
	after := Ring{Live: []Gen{r.Live[0], r.Live[2], r.Live[3], r.Live[4]}}
	s2 := &Session{Ring: after, Mine: []Reader{ro}, Rand: &stream{seed: 13}}
	env2, _ := s2.Seal(TypeEvent, id, "journal/x", []byte("after the revoke"))
	if _, err := s2.Open(TypeEvent, id, env2); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("a revoked generation opened content written after its revoke: %v", err)
	}
	if _, err := s.Open(TypeEvent, id, env); err != nil {
		t.Fatal("what was sealed to it before stays open to it (K4)")
	}
	// One id in several envelopes opens to the same bytes (E2).
	envOwner, _ := SealReaders(TypeEvent, id, nil, [][]byte{owner.Public()}, []byte("dear diary"), &stream{seed: 14})
	os := &Session{Mine: []Reader{owner}}
	a, _ := os.Open(TypeEvent, id, env)
	b, _ := os.Open(TypeEvent, id, envOwner)
	if !bytes.Equal(a, b) {
		t.Fatal("one id in two envelopes opened to different bytes")
	}
}

func TestConcurrentGenerationsAreMarked(t *testing.T) {
	a, b := reader(t, 1), reader(t, 2)
	r := Ring{Live: []Gen{gen([32]byte{9}, 1, a, nil), gen([32]byte{9}, 2, b, nil)}}
	if c := r.Concurrent(); len(c) != 1 || c[0] != [32]byte{9} {
		t.Fatalf("concurrent: %v", c)
	}
}

func TestACellOpensOnlyWithItsPassphrase(t *testing.T) {
	salt := bytes.Repeat([]byte{1}, 32)
	kk, err := PassKey("synthetic passphrase", salt, 1000)
	if err != nil {
		t.Fatal(err)
	}
	r := reader(t, 1)
	s := Secret{Key: [32]byte{5}, Gen: 3, SignerSeed: [32]byte{6}}
	copy(s.Reader[:], r.Bytes())
	vk := bytes.Repeat([]byte{0xAB}, 32)
	cell, err := Cell(kk, s, vk, &stream{seed: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(cell) != CellSize {
		t.Fatalf("cell is %d bytes", len(cell))
	}
	noise := make([]byte, CellSize)
	(&stream{seed: 3}).Read(noise)
	got, gotVK, err := Try("synthetic passphrase", [][]byte{noise, cell}, salt, 1000)
	if err != nil || got != s || !bytes.Equal(gotVK, vk) || got.Signer() == nil {
		t.Fatalf("the cell did not open to its secret: %v", err)
	}
	if _, _, err := Try("another passphrase", [][]byte{noise, cell}, salt, 1000); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	unlock := Unlock("synthetic passphrase")
	if v, err := unlock([][]byte{cell}, salt, 1000); err != nil || !bytes.Equal(v, vk) {
		t.Fatalf("unlock: %v", err)
	}
}

func TestHeirAndSystemSeals(t *testing.T) {
	old, next := reader(t, 1), reader(t, 2)
	heir, err := SealTo(next.Public(), old.Bytes(), InfoHeir, &stream{seed: 4})
	if err != nil || len(heir) != event.SealedKeySize {
		t.Fatalf("heir: %v %d", err, len(heir))
	}
	got, err := OpenFrom(next, heir, InfoHeir)
	if err != nil || !bytes.Equal(got, old.Bytes()) {
		t.Fatalf("the heir did not open to the previous reader: %v", err)
	}
	if _, err := OpenFrom(old, heir, InfoHeir); !errors.Is(err, ErrForged) {
		t.Fatal("the heir opened to a reader it was not sealed to")
	}
	if _, err := OpenFrom(next, heir, InfoSystem); !errors.Is(err, ErrForged) {
		t.Fatal("a seal opened under another info")
	}
}

// E3 is judged at the record's own point. With the owner
// generations at that point supplied, an envelope sealed before the owner
// rotated opens afterwards to a session holding the inherited reader; an
// unknown point is refused, never guessed.
func TestOldEnvelopesOpenAtTheirOwnPointAfterTheOwnerRotates(t *testing.T) {
	o1, o2 := reader(t, 21), reader(t, 22)
	before := &Session{Ring: Ring{Live: []Gen{gen([32]byte{}, 1, o1, []string{""})}}, Mine: []Reader{o1}, Rand: &stream{seed: 23}}
	id := frame.Hash([]byte("written before the rotation"))
	env, err := before.Seal(TypeEvent, id, "journal", []byte("the body"))
	if err != nil {
		t.Fatal(err)
	}
	points := map[frame.ID][][]byte{id: {o1.Public()}}
	after := &Session{Ring: Ring{Live: []Gen{gen([32]byte{}, 2, o2, []string{""})}}, Mine: []Reader{o2, o1},
		OwnerAt: func(id frame.ID) ([][]byte, bool) { o, ok := points[id]; return o, ok }}
	if p, err := after.Open(TypeEvent, id, env); err != nil || string(p) != "the body" {
		t.Fatalf("an old envelope at its own point: %q %v", p, err)
	}
	other := frame.Hash([]byte("a record whose point is not known"))
	env2, _ := before.Seal(TypeEvent, other, "journal", []byte("x"))
	if _, err := after.Open(TypeEvent, other, env2); !errors.Is(err, ErrOwnerUnknown) {
		t.Fatalf("an unknown point: %v", err)
	}
}

// Pointers are sealed under suite 0x02 with the vessel's
// shared key, not to readers.
func TestPointersAreSealedUnderTheSharedKey(t *testing.T) {
	o := reader(t, 31)
	ptk := bytes.Repeat([]byte{9}, 32)
	s := &Session{Ring: Ring{Live: []Gen{gen([32]byte{}, 1, o, []string{""})}}, Mine: []Reader{o}, Rand: &stream{seed: 32}, PTK: ptk}
	id := frame.Hash([]byte("main"))
	env, err := s.Seal(TypePointer, id, "rokh", []byte("tip"))
	if err != nil || env[1] != SuiteShared {
		t.Fatalf("a pointer was not sealed under the shared key: %v", err)
	}
	if p, err := s.Open(TypePointer, id, env); err != nil || string(p) != "tip" {
		t.Fatalf("pointer: %q %v", p, err)
	}
	if _, err := (&Session{Mine: []Reader{o}}).Open(TypePointer, id, env); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("a session without the shared key opened a pointer: %v", err)
	}
}
