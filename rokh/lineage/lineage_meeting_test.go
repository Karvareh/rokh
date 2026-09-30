package lineage

// Probes: several envelopes of one id through union, and what a
// session moves that it cannot open.

import (
	"bytes"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

func whole3(c *carrier.Carrier, id frame.ID) (n int) {
	c.Events(func(e carrier.EventRecord) error {
		if e.ID == id {
			n = len(e.Refs)
		}
		return nil
	}, nil)
	return
}

func seal3(t *testing.T, s Side, k []byte, seed byte, id frame.ID, addr string) {
	t.Helper()
	var hd, bd []byte
	s.C.Events(func(r carrier.EventRecord) error {
		if r.ID == id {
			hd = r.Head
			bd, _ = s.C.Body(r)
		}
		return nil
	}, nil)
	if bd == nil {
		t.Fatalf("the event does not open on this side")
	}
	env, _ := kSealer{k: k, rnd: stream(seed)}.Seal(vessel.RecEvent, id, addr, bd)
	r, _ := s.C.Begin(s.Own)
	if err := r.EventEnvelope(id, hd, env); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
}

// Envelopes added on both sides, for different readers, all meet; a second
// meeting moves nothing and writes nothing.
func TestEnvelopesFromBothSidesMeetOnce(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	e := write(t, a, 1, "work/plan", "read by more and more")
	b, mb := newSide(t, 2, nil, false)
	if res, err := Reconcile(a, b); err != nil || !res.OK() {
		t.Fatalf("first meeting: %v %s", err, res)
	}
	k1, k2 := bytes.Repeat([]byte{0x21}, 32), bytes.Repeat([]byte{0x22}, 32)
	seal3(t, a, k1, 81, e, "work/plan")
	seal3(t, b, k2, 82, e, "work/plan")
	res, err := Reconcile(a, b)
	t.Logf("second meeting: %v %s", err, res)
	if na, nb := whole3(a.C, e), whole3(b.C, e); na != 3 || nb != 3 {
		t.Errorf("after the meeting a holds %d envelopes of the event and b holds %d, want 3 and 3", na, nb)
	}
	for name, k := range map[string][]byte{"first": k1, "second": k2} {
		for side, s := range map[string]Side{"a": a, "b": b} {
			raw, err := carrier.Wrap(s.C.Vessel(), kSealer{k: k, rnd: stream(83)}).Get(e)
			if err != nil || !bytes.Contains(raw, []byte("read by more and more")) {
				t.Errorf("the %s added reader cannot open the event on %s: %v", name, side, err)
			}
		}
	}
	before := mb.Writes()
	res, err = Reconcile(a, b)
	t.Logf("third meeting: %v %s; writes on b: %d", err, res, mb.Writes()-before)
	if res.Local.Added != 0 || res.Remote.Added != 0 || mb.Writes() != before {
		t.Errorf("a meeting of two equal sides moved %d and %d records and wrote %d times on b", res.Local.Added, res.Remote.Added, mb.Writes()-before)
	}
}

// Contract R1: union moves records sealed and unopened. A session that
// cannot open a body still carries its envelope to a side that holds the
// whole rokh; the address is needed only to cut a slice.
func TestASessionCarriesWhatItCannotOpen(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	mine := write(t, a, 1, "work/plan", "opened by the session")
	// An event on a, sealed for another reader only.
	heads, _ := a.C.Heads()
	bd := body("home/diary", "note", "sealed for another reader")
	hd := head(1, heads, bd, false)
	id := frame.Hash(hd)
	env, _ := kSealer{k: otherK, rnd: stream(84)}.Seal(vessel.RecEvent, id, "home/diary", bd)
	r, _ := a.C.Begin(a.Own)
	if err := r.EventEnvelope(id, hd, env); err != nil {
		t.Fatal(err)
	}
	r.SetRef("main", id)
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	b, _ := newSide(t, 2, nil, false)
	res, err := Reconcile(a, b)
	t.Logf("meeting: %v %s", err, res)
	got := map[frame.ID]string{}
	b.C.Events(func(e carrier.EventRecord) error {
		if carrier.IsHeadOnly(e.Ref) {
			got[e.ID] = "head"
		} else {
			got[e.ID] = "whole"
		}
		return nil
	}, nil)
	t.Logf("b holds: the opened event %q, the unopened event %q", got[mine], got[id])
	if got[id] != "whole" {
		t.Errorf("b holds the whole rokh and was given %q of an event the session could not open; its envelope stayed behind and the answer is %s", got[id], res)
	}
	other := carrier.Wrap(b.C.Vessel(), kSealer{k: otherK, rnd: stream(85)})
	if raw, err := other.Get(id); err != nil || !bytes.Contains(raw, []byte("sealed for another reader")) {
		t.Errorf("the reader of that event cannot open it on b (err %v)", err)
	}
}

// With no judge and no check of the owner given, a side moves everything.
// This is the state of every caller in the product today.
func TestASideWithoutAJudgeMovesEverything(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	e := write(t, a, 1, "work/plan", "never judged")
	b, _ := newSide(t, 2, nil, false)
	res, err := Reconcile(Side{C: a.C, Own: a.Own}, Side{C: b.C, Own: b.Own})
	t.Logf("no judge, no owner check: %v %s", err, res)
	if whole3(b.C, e) != 0 {
		t.Logf("a side with Judge and Covers nil moves a record nobody judged; the checks of R6 act only where a caller gives them")
	}
	refuse := Side{C: a.C, Own: a.Own, Judge: func(frame.ID) string { return "pending" }, Covers: func(frame.ID, []byte) bool { return false }}
	c, _ := newSide(t, 3, nil, false)
	res, err = Reconcile(refuse, Side{C: c.C, Own: c.Own})
	t.Logf("a judge that answers pending: %v %s", err, res)
	n := 0
	c.C.Events(func(carrier.EventRecord) error { n++; return nil }, nil)
	if n != 0 {
		t.Errorf("DEFECT R6: %d events moved from a side whose judge answers pending", n)
	}
}
