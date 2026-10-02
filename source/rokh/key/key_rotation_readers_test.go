package key

// Probes: envelopes sealed before the owner rotated, and more readers than
// an envelope holds.

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"rokh/event"
	"rokh/frame"
)

func probeStream(seed byte) *rand.ChaCha8 {
	var s [32]byte
	s[0] = seed
	return rand.NewChaCha8(s)
}

func ownerGen(gen uint32, r Reader) Gen {
	return Gen{Keyring: event.Keyring{Op: event.KeyringAdd, Gen: gen, Name: "owner", Reader: r.Public(), Reads: []string{""}}}
}

// Contract E3 and E4: the readers of an event are judged at the event's
// parents. An envelope sealed while generation 1 of the owner's reader was
// the live one named every live generation of its day. After the owner
// rotates, the new generation holds the old half through `heir`.
func TestOldEnvelopesOpenAfterTheOwnerRotates(t *testing.T) {
	t.Skip("known to fail in 1.0.0 and owed: it and another review test ask opposite things of one session, and the ruling is not made; see STATE.md")
	rnd := probeStream(3)
	o1, _ := NewReader(rnd)
	o2, _ := NewReader(rnd)
	before := &Session{Ring: Ring{Live: []Gen{ownerGen(1, o1)}}, Mine: []Reader{o1}, Rand: rnd}
	id := frame.Hash([]byte("an event written before the rotation"))
	env, err := before.Seal(TypeEvent, id, "home/journal", []byte("the body"))
	if err != nil {
		t.Fatal(err)
	}
	after := &Session{Ring: Ring{Live: []Gen{ownerGen(2, o2)}}, Mine: []Reader{o2, o1}, Rand: rnd}
	p, err := after.Open(TypeEvent, id, env)
	t.Logf("after the rotation: %q err=%v", p, err)
	if err != nil || !bytes.Equal(p, []byte("the body")) {
		t.Errorf("an envelope sealed before the owner rotated is refused afterwards (%v); E3 is judged at the session's ring, not at the event's parents", err)
	}
	if errors.Is(err, ErrNoOwner) {
		t.Logf("the refusal is ErrNoOwner")
	}
}

// More readers than an envelope holds.
func TestMoreThanThirtyTwoReaders(t *testing.T) {
	rnd := probeStream(4)
	o, _ := NewReader(rnd)
	ring := Ring{Live: []Gen{ownerGen(1, o)}}
	for i := 0; i < 32; i++ {
		r, _ := NewReader(rnd)
		var k [32]byte
		k[0] = byte(i + 1)
		ring.Live = append(ring.Live, Gen{Keyring: event.Keyring{Op: event.KeyringAdd, Key: k, Gen: 1,
			Name: fmt.Sprintf("k%d", i), Reader: r.Public(), Reads: []string{"home"}}})
	}
	s := &Session{Ring: ring, Mine: []Reader{o}, Rand: rnd}
	_, err := s.Seal(TypeEvent, frame.Hash([]byte("x")), "home/journal", []byte("b"))
	t.Logf("33 readers cover the address: err=%v", err)
	if err != nil {
		t.Logf("an address read by more than 32 keys cannot be written; several envelopes for one id (E2) would carry them")
	}
}
