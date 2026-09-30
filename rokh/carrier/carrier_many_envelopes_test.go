package carrier

// Probes: several envelopes of one id (E2).

import (
	"bytes"
	"errors"
	"testing"

	"rokh/frame"
	"rokh/vessel"
)

func putEnvelope(t *testing.T, c *Carrier, id frame.ID, hd, env []byte) {
	t.Helper()
	r, err := c.Begin(holder{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.EventEnvelope(id, hd, env); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
}

// Every envelope of an id is kept, in any order of arrival, also after the
// vessel is opened again, and each reader is given its own.
func TestManyEnvelopesOfOneID(t *testing.T) {
	m := vessel.NewMemory()
	c := newCarrier(t, m)
	bd := bodyAt("work/plan", "for many readers")
	hd := head(3, bd)
	id := frame.Hash(hd)
	// The head alone arrives first.
	putEnvelope(t, c, id, hd, HeadOnly)
	if raw, err := c.Get(id); err != nil || !bytes.Equal(raw, hd) {
		t.Fatalf("a head alone: %d bytes, %v", len(raw), err)
	}
	var readers []ptkSealer
	for i := 0; i < 5; i++ {
		s := ptkSealer{k: bytes.Repeat([]byte{byte(0x60 + i)}, 32), rnd: stream(byte(70 + i))}
		readers = append(readers, s)
		env, _ := s.Seal(vessel.RecEvent, id, "work/plan", bd)
		putEnvelope(t, c, id, hd, env)
	}
	n, whole := 0, 0
	c.Events(func(e EventRecord) error {
		if e.ID == id {
			n++
			whole = len(e.Refs)
		}
		return nil
	}, nil)
	if n != 1 || whole != 5 {
		t.Errorf("the index names the id %d times and keeps %d whole envelopes, want 1 and 5", n, whole)
	}
	for i, s := range readers {
		raw, err := Wrap(c.Vessel(), s).Get(id)
		if err != nil || !bytes.Contains(raw, []byte("for many readers")) {
			t.Errorf("reader %d is not given its envelope: %d bytes, %v", i, len(raw), err)
		}
	}
	stranger := ptkSealer{k: bytes.Repeat([]byte{0x7f}, 32), rnd: stream(90)}
	if raw, err := Wrap(c.Vessel(), stranger).Get(id); err != nil || !bytes.Equal(raw, hd) {
		t.Errorf("a reader named in no envelope: %d bytes, %v; want the head alone", len(raw), err)
	}
}

// An envelope that opens for a reader and holds other bytes than the head
// names. It is never shown. Whether a true envelope beside it is still
// served is what this probe records.
func TestAForgedEnvelopeBesideATrueOne(t *testing.T) {
	for _, order := range []string{"forged first", "true first"} {
		m := vessel.NewMemory()
		c := newCarrier(t, m)
		bd := bodyAt("work/plan", "the true body")
		hd := head(4, bd)
		id := frame.Hash(hd)
		s := ptkSealer{k: bytes.Repeat([]byte{0x31}, 32), rnd: stream(91)}
		good, _ := s.Seal(vessel.RecEvent, id, "work/plan", bd)
		forged, _ := s.Seal(vessel.RecEvent, id, "work/plan", bodyAt("work/plan", "another body"))
		if order == "forged first" {
			putEnvelope(t, c, id, hd, forged)
			putEnvelope(t, c, id, hd, good)
		} else {
			putEnvelope(t, c, id, hd, good)
			putEnvelope(t, c, id, hd, forged)
		}
		raw, err := Wrap(c.Vessel(), s).Get(id)
		t.Logf("%s: %d bytes, err=%v, forged=%v", order, len(raw), err, errors.Is(err, ErrForged))
		if bytes.Contains(raw, []byte("another body")) {
			t.Errorf("DEFECT: a body that does not hash to the head was served (%s)", order)
		}
		if err != nil && order == "forged first" {
			t.Logf("one planted envelope ahead of the true one closes the event to its reader; it fails closed")
		}
	}
}
