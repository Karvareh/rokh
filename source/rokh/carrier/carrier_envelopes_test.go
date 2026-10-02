package carrier

// Probes: a second envelope of one id, and the address a body names.

import (
	"bytes"
	"testing"

	"rokh/frame"
	"rokh/vessel"
)

// bodyAt is an RKH3-shaped body with an address field.
func bodyAt(addr, payload string) []byte {
	b, _ := frame.EncodeFields(frame.Fields{
		{Tag: 0x0005, Value: []byte(addr)},
		{Tag: 0x0006, Value: []byte("note")},
		{Tag: 0x0007, Value: []byte(payload)},
		{Tag: 0x000C, Value: bytes.Repeat([]byte{7}, 32)},
	})
	return b
}

// Contract E2: one id may sit in several envelopes; a reader is given
// the one that opens for it.
func TestTheSecondEnvelopeOpensForItsReader(t *testing.T) {
	m := vessel.NewMemory()
	c := newCarrier(t, m)
	bd := bodyAt("work/plan", "for two readers")
	hd := head(1, bd)
	id := frame.Hash(hd)
	r, _ := c.Begin(holder{})
	if err := r.Event(id, hd, bd, "work/plan"); err != nil {
		t.Fatal(err)
	}
	r.SetRef("main", id)
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	second := ptkSealer{k: bytes.Repeat([]byte{0x42}, 32), rnd: stream(61)}
	env, err := second.Seal(vessel.RecEvent, id, "work/plan", bd)
	if err != nil {
		t.Fatal(err)
	}
	r, _ = c.Begin(holder{})
	if err := r.EventEnvelope(id, hd, env); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	view := Wrap(c.Vessel(), second)
	raw, err := view.Get(id)
	t.Logf("second reader: %d bytes, err %v, body present %v", len(raw), err, bytes.Contains(raw, []byte("for two readers")))
	if err != nil || !bytes.Contains(raw, []byte("for two readers")) {
		t.Errorf("the second envelope is in the vessel and its reader is given the head only")
	}
}

// The readers of an event follow from its address. The address is in the
// body; the caller's word must agree with it.
func TestTheAddressIsTheBodysOwn(t *testing.T) {
	m := vessel.NewMemory()
	c := newCarrier(t, m)
	bd := bodyAt("private/diary", "sealed for whom?")
	hd := head(2, bd)
	id := frame.Hash(hd)
	r, _ := c.Begin(holder{})
	err := r.Event(id, hd, bd, "public/board")
	t.Logf("Event with address %q for a body at %q: err=%v", "public/board", "private/diary", err)
	if err == nil {
		t.Errorf("the carrier seals a body for the readers of an address the body does not name")
	}
	r.Abandon()
}
