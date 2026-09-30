package lineage

// Probes: a second envelope travels; a repeated seed; a slice and its heads.

import (
	"bytes"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

var otherK = bytes.Repeat([]byte{9}, 32)

func eventRecords(t *testing.T, c *carrier.Carrier, id frame.ID) (n int) {
	t.Helper()
	c.Vessel().Scan(func(h vessel.Header, r vessel.Ref) error {
		if h.Type == vessel.RecEvent && h.ID == id {
			n++
		}
		return nil
	})
	return
}

// Contract E2, and the goal "content and rights become equal": a second
// envelope for an id the other side already holds must travel.
func TestASecondEnvelopeTravels(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	e := write(t, a, 1, "work/plan", "shared later with another reader")
	b, _ := newSide(t, 2, nil, false)
	if res, err := Reconcile(a, b); err != nil || !res.OK() {
		t.Fatalf("first meeting: %v %s", err, res)
	}
	// On a, the owner seals the same event for a second reader.
	var hd []byte
	var bd []byte
	a.C.Events(func(r carrier.EventRecord) error {
		if r.ID == e {
			hd = r.Head
			bd, _ = a.C.Body(r)
		}
		return nil
	}, nil)
	env, _ := kSealer{k: otherK, rnd: stream(77)}.Seal(vessel.RecEvent, e, "work/plan", bd)
	r, _ := a.C.Begin(a.Own)
	if err := r.EventEnvelope(e, hd, env); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	if n := eventRecords(t, a.C, e); n != 2 {
		t.Fatalf("a holds %d records of the event, want 2", n)
	}
	res, err := Reconcile(a, b)
	t.Logf("second meeting: %v %s", err, res)
	if n := eventRecords(t, b.C, e); n != 2 {
		t.Errorf("after reconcile b holds %d record(s) of the event; the second envelope did not travel", n)
	}
	// The second reader opens b.
	other := carrier.Wrap(b.C.Vessel(), kSealer{k: otherK, rnd: stream(78)})
	raw, err := other.Get(e)
	if err != nil || !bytes.Contains(raw, []byte("shared later")) {
		t.Errorf("the second reader cannot open the event on b (err %v, %d bytes)", err, len(raw))
	}
}

// Contract S5 says repeating a seed whose target already TOOK the give
// changes nothing. A target that exists and took nothing is not that.
func TestARepeatedSeedOnAnUnfinishedTarget(t *testing.T) {
	src, _ := newSide(t, 1, nil, true)
	write(t, src, 1, "work/plan", "in scope")
	dst, mem := target(40)
	seedID := frame.Hash([]byte("seed unfinished"))
	p := plan(t, src, seedID, nil)
	// First attempt: the source's owner is gone, so the give fails after the
	// target was created.
	broken := Side{C: src.C, Own: &holder{no: true}}
	res, err := Seed(broken, dst, p)
	t.Logf("first attempt: err=%v give=%s copy=%q take=%q", err, res.Give.Outcome, res.Copy.Outcome, res.Take.Outcome)
	if res.OK() {
		t.Fatal("the first attempt was meant to fail")
	}
	// Second attempt with a good owner.
	res, err = Seed(src, dst, p)
	t.Logf("second attempt: err=%v repeated=%v give=%s copy=%s take=%s", err, res.Repeated, res.Give.Outcome, res.Copy.Outcome, res.Take.Outcome)
	d, _, oerr := carrier.Open(mem, func([][]byte, []byte, int) ([]byte, error) { return dst.VK, nil }, stream(50), dst.Sealer)
	if oerr != nil {
		t.Fatalf("target does not open: %v", oerr)
	}
	n := 0
	d.Events(func(carrier.EventRecord) error { n++; return nil }, nil)
	t.Logf("the target holds %d events", n)
	if res.OK() && n == 0 {
		t.Errorf("the seed is answered recorded three times over and the target holds nothing")
	}
}

// Contract S3, acceptance B6: a slice holds its scopes and the signed
// head of every ANCESTOR of what it opens, nothing else.
func TestASliceHoldsNoHeadOfADescendant(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	w := write(t, a, 1, "work/plan", "in scope")
	later := write(t, a, 1, "home/diary", "written after, out of scope")
	latest := write(t, a, 1, "home/letters", "written last, out of scope")
	b, _ := newSide(t, 2, []string{"work"}, false)
	if res, err := Reconcile(a, b); err != nil || !res.OK() {
		t.Fatalf("%v %s", err, res)
	}
	got := map[frame.ID]string{}
	b.C.Events(func(e carrier.EventRecord) error {
		if carrier.IsHeadOnly(e.Ref) {
			got[e.ID] = "head"
		} else {
			got[e.ID] = "whole"
		}
		return nil
	}, nil)
	refs, _ := b.C.Refs()
	t.Logf("slice holds: genesis=%s work=%s later=%q latest=%q refs=%d", got[anchor], got[w], got[later], got[latest], len(refs))
	for name, id := range refs {
		t.Logf("  branch %q names %s", name, id.Short())
	}
	if got[later] != "" || got[latest] != "" {
		t.Errorf("the slice holds the heads of events written after what it opens (%q, %q); they are no ancestor of anything in its scope", got[later], got[latest])
	}
}
