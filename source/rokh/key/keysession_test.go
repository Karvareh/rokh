package key

import (
	"errors"
	"testing"

	"rokh/event"
	"rokh/frame"
)

func liveGen(k [32]byte, name string, r Reader, reads []string) Gen {
	return Gen{Keyring: event.Keyring{Op: event.KeyringAdd, Key: k, Gen: 1, Name: name, Reader: r.Public(), Reads: reads}}
}

func secretOf(k [32]byte, r Reader) Secret {
	s := Secret{Key: k, Gen: 1}
	copy(s.Reader[:], r.Bytes())
	return s
}

// A key's own session: given the ring, the fold of each
// record's point and, for system events, the system reader, it opens its
// view and nothing else; a key that reads nothing opens nothing; a record at
// an unknown point, or one that does not name its point's owner, is refused
// to every key; and a session is refused where the ring names no owner, no
// fold is given, or the generation is not live. The root is never taken.
func TestAKeysSessionOpensItsViewAtEachRecordsPointAndNothingElse(t *testing.T) {
	rnd := &stream{seed: 41}
	owner, _ := NewReader(rnd)
	readerR, _ := NewReader(rnd)
	writerR, _ := NewReader(rnd)
	noneR, _ := NewReader(rnd)
	sysR, _ := NewReader(rnd)
	kidR, kidW, kidN := [32]byte{1}, [32]byte{2}, [32]byte{3}
	ring := Ring{Live: []Gen{
		liveGen([32]byte{}, "owner", owner, []string{""}),
		liveGen(kidR, "reader", readerR, []string{"journal"}),
		liveGen(kidW, "writer", writerR, []string{"journal"}),
		liveGen(kidN, "none", noneR, nil),
	}}
	known := map[frame.ID]bool{}
	point := func(s string) frame.ID { id := frame.Hash([]byte(s)); known[id] = true; return id }
	at := func(id frame.ID) ([][]byte, bool) {
		if !known[id] {
			return nil, false
		}
		return ring.Owner(), true
	}
	ownerS := &Session{Ring: ring, Mine: []Reader{owner}, Rand: rnd, OwnerAt: at, System: sysR.Public()}
	session := func(k [32]byte, r Reader, sys *Reader) *Session {
		t.Helper()
		s, err := KeySession(secretOf(k, r), ring, at, sys, rnd)
		if err != nil {
			t.Fatalf("a live key's session was refused: %v", err)
		}
		return s
	}
	reader, writer, none := session(kidR, readerR, nil), session(kidW, writerR, nil), session(kidN, noneR, nil)

	inView, outView := point("journal/today"), point("private/diary")
	inEnv, _ := ownerS.Seal(TypeEvent, inView, "journal/today", []byte("in the view"))
	outEnv, _ := ownerS.Seal(TypeEvent, outView, "private/diary", []byte("outside the view"))
	if p, err := reader.Open(TypeEvent, inView, inEnv); err != nil || string(p) != "in the view" {
		t.Fatalf("a reader does not open its view: %q %v", p, err)
	}
	if _, err := reader.Open(TypeEvent, outView, outEnv); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("a reader opened outside its view: %v", err)
	}
	if _, err := none.Open(TypeEvent, inView, inEnv); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("a key that reads nothing opened a record: %v", err)
	}

	// The writer seals to the readers of the address, the owner always.
	byWriter := point("journal/by-writer")
	w, err := writer.Seal(TypeEvent, byWriter, "journal/by-writer", []byte("written by the writer"))
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]*Session{"the owner": ownerS, "the reader": reader, "the writer": writer} {
		if p, err := s.Open(TypeEvent, byWriter, w); err != nil || string(p) != "written by the writer" {
			t.Fatalf("%s does not open what the writer sealed: %v", name, err)
		}
	}
	if _, err := none.Open(TypeEvent, byWriter, w); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("a key that reads nothing opened the writer's record: %v", err)
	}

	// An unknown point is refused, and never judged by the present owner.
	nowhere := frame.Hash([]byte("a record at no point anyone knows"))
	lost, _ := ownerS.Seal(TypeEvent, nowhere, "journal/lost", []byte("no point"))
	if _, err := reader.Open(TypeEvent, nowhere, lost); !errors.Is(err, ErrOwnerUnknown) {
		t.Fatalf("a record at an unknown point: %v", err)
	}
	// An envelope that does not name its point's owner is refused (E3).
	hidden := point("journal/hidden")
	noOwner, _ := SealReaders(TypeEvent, hidden, nil, [][]byte{readerR.Public()}, []byte("hidden from the owner"), rnd)
	if _, err := reader.Open(TypeEvent, hidden, noOwner); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("an envelope without its point's owner: %v", err)
	}

	// System events open with the system reader, and without it they do not.
	sys := point("rokh")
	sysEnv, _ := ownerS.Seal(TypeEvent, sys, event.AddressRoot, []byte("a keyring event"))
	withSystem := session(kidN, noneR, &sysR)
	if p, err := withSystem.Open(TypeEvent, sys, sysEnv); err != nil || string(p) != "a keyring event" {
		t.Fatalf("the system reader does not open a system event: %v", err)
	}
	if _, err := none.Open(TypeEvent, sys, sysEnv); !errors.Is(err, ErrNotForMe) {
		t.Fatalf("a key without the system reader opened a system event: %v", err)
	}

	// Sessions that must not exist.
	other, _ := NewReader(rnd)
	noOwnerRing := Ring{Live: ring.Live[1:]}
	for name, try := range map[string]func() error{
		"the owner's own secret":              func() error { _, err := KeySession(secretOf([32]byte{}, owner), ring, at, nil, rnd); return err },
		"a ring that names no owner":          func() error { _, err := KeySession(secretOf(kidR, readerR), noOwnerRing, at, nil, rnd); return err },
		"no fold of the point":                func() error { _, err := KeySession(secretOf(kidR, readerR), ring, nil, nil, rnd); return err },
		"a generation the ring does not hold": func() error { _, err := KeySession(secretOf([32]byte{9}, other), ring, at, nil, rnd); return err },
		"the key's id with another reader":    func() error { _, err := KeySession(secretOf(kidR, other), ring, at, nil, rnd); return err },
	} {
		if err := try(); err == nil {
			t.Errorf("a session was made for %s", name)
		}
	}
	if _, err := KeySession(secretOf(kidR, readerR), Ring{Live: []Gen{ring.Live[0]}}, at, nil, rnd); !errors.Is(err, ErrKeyNotLive) {
		t.Errorf("a revoked generation: %v", err)
	}
}
