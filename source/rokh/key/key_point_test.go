package key

// Probes: E3 is judged with the
// owner generations at the record's own point; a point that is not known is
// refused; owner coverage is never relaxed.

import (
	"bytes"
	"errors"
	"testing"

	"rokh/frame"
)

// The three answers of a point.
func TestE3IsJudgedAtTheRecordsOwnPoint(t *testing.T) {
	rnd := probeStream(11)
	o1, _ := NewReader(rnd)
	o2, _ := NewReader(rnd)
	before := &Session{Ring: Ring{Live: []Gen{ownerGen(1, o1)}}, Mine: []Reader{o1}, Rand: rnd}
	id := frame.Hash([]byte("written before the rotation"))
	env, err := before.Seal(TypeEvent, id, "home/journal", []byte("the body"))
	if err != nil {
		t.Fatal(err)
	}
	points := map[frame.ID][][]byte{id: {o1.Public()}}
	after := &Session{Ring: Ring{Live: []Gen{ownerGen(2, o2)}}, Mine: []Reader{o2, o1}, Rand: rnd,
		OwnerAt: func(id frame.ID) ([][]byte, bool) { o, ok := points[id]; return o, ok }}
	// 1. The point is known: the envelope opens.
	if p, err := after.Open(TypeEvent, id, env); err != nil || !bytes.Equal(p, []byte("the body")) {
		t.Errorf("DEFECT K1: with its own point given, an envelope sealed before the rotation does not open: %v", err)
	}
	// 2. The point is not known: a typed refusal.
	other := frame.Hash([]byte("a record whose point is not known"))
	env2, _ := before.Seal(TypeEvent, other, "home/journal", []byte("x"))
	if _, err := after.Open(TypeEvent, other, env2); !errors.Is(err, ErrOwnerUnknown) {
		t.Errorf("DEFECT K1: an unknown point answered %v, not ErrOwnerUnknown", err)
	}
	// 3. The point is known and the envelope names the owner of another day
	// only: refused.
	points[other] = [][]byte{o2.Public()}
	if _, err := after.Open(TypeEvent, other, env2); !errors.Is(err, ErrNoOwner) {
		t.Errorf("DEFECT K1: an envelope that does not name the owner of its point answered %v", err)
	}
}

// A point that is known and names no owner must not switch E3 off: there
// is no point of a rokh at which the owner has no live reader.
func TestAPointWithNoOwnerDoesNotSwitchE3Off(t *testing.T) {
	rnd := probeStream(12)
	o, _ := NewReader(rnd)
	x, _ := NewReader(rnd)
	id := frame.Hash([]byte("sealed for a reader, the owner is not named"))
	env, err := SealReaders(TypeEvent, id, nil, [][]byte{x.Public()}, []byte("hidden from the owner"), rnd)
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]*Session{
		"the point answers known with no owner": {Ring: Ring{Live: []Gen{ownerGen(1, o)}}, Mine: []Reader{x},
			OwnerAt: func(frame.ID) ([][]byte, bool) { return nil, true }},
		"a session whose ring names no owner": {Mine: []Reader{x}},
	} {
		p, err := s.Open(TypeEvent, id, env)
		t.Logf("%s: %q err=%v", name, p, err)
		if err == nil {
			t.Errorf("%s, and an envelope that does not name the owner opens; E3 was not judged", name)
		}
	}
}

// Without a point given, the session judges by its present ring. That is
// a refusal, and it is the state of the product while no caller gives a
// point: after the owner rotates, nothing written before opens.
func TestWithoutAPointEarlierEnvelopesStayShut(t *testing.T) {
	rnd := probeStream(13)
	o1, _ := NewReader(rnd)
	o2, _ := NewReader(rnd)
	before := &Session{Ring: Ring{Live: []Gen{ownerGen(1, o1)}}, Mine: []Reader{o1}, Rand: rnd}
	id := frame.Hash([]byte("written before the rotation"))
	env, _ := before.Seal(TypeEvent, id, "home/journal", []byte("the body"))
	after := &Session{Ring: Ring{Live: []Gen{ownerGen(2, o2)}}, Mine: []Reader{o2, o1}, Rand: rnd}
	_, err := after.Open(TypeEvent, id, env)
	t.Logf("no point given: err=%v", err)
	if err == nil {
		t.Errorf("owner coverage was relaxed: the envelope opened with no point and another owner in the ring")
	}
	if !errors.Is(err, ErrOwnerUnknown) {
		t.Logf("the refusal is %q; the envelope is sound and the point is what is missing, so the reason a person reads is the wrong one", err)
	}
}

// A shared-key envelope is for pointers (E7). An event or content sealed
// under suite 0x02 opens for every holder of the vessel's key, whatever the
// address and the keyring say.
func TestASharedKeyEnvelopeIsForPointersOnly(t *testing.T) {
	rnd := probeStream(14)
	o, _ := NewReader(rnd)
	ptk := bytes.Repeat([]byte{7}, 32)
	id := frame.Hash([]byte("an event body under the shared key"))
	env, err := SealShared(TypeEvent, id, nil, ptk, []byte("read by every holder of the vessel"), rnd)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Ring: Ring{Live: []Gen{ownerGen(1, o)}}, Mine: []Reader{o}, Rand: rnd, PTK: ptk}
	p, err := s.Open(TypeEvent, id, env)
	t.Logf("an event under suite 0x02: %q err=%v", p, err)
	if err == nil {
		t.Errorf("an event sealed under the shared key opens; E3 and the readers of its address were not asked")
	}
}
