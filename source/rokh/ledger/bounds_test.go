package ledger

import (
	"testing"

	"rokh/frame"
)

// Two events neither of which is reachable from the other are concurrent —
// even if their clocks agree. Concurrency is causal incomparability, and no
// timestamp makes it go away.
//
//	— T5.2
func TestConcurrentEventsAreCausallyIncomparable(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	// Two siblings, both parented on the grant, neither on the other.
	a := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/left", "note", []byte("left"))
	b := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/right", "note", []byte("right"))
	must(t, l, a, Accepted, "left")
	must(t, l, b, Accepted, "right")

	past := func(at frame.ID) map[frame.ID]bool {
		m := map[frame.ID]bool{}
		for _, id := range l.CausalPast(at) {
			m[id] = true
		}
		return m
	}
	if past(a.ID)[b.ID] {
		t.Error("the right event stands in the left one's past; they are not concurrent")
	}
	if past(b.ID)[a.ID] {
		t.Error("the left event stands in the right one's past; they are not concurrent")
	}
}

// Two concurrent acts can contradict each other out in the world. Resolving
// that is the application layer's work, not the ledger's: the ledger keeps
// both, leaves both as heads, and names no winner.
//
//	— T5.5, T5.4
func TestTheLedgerNamesNoWinnerBetweenConcurrentActs(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	// One address, written twice, concurrently, with opposite payloads.
	a := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/seat", "note", []byte("yes"))
	b := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/seat", "note", []byte("no"))
	must(t, l, a, Accepted, "yes")
	must(t, l, b, Accepted, "no")

	heads := map[frame.ID]bool{}
	for _, h := range l.Heads() {
		heads[h] = true
	}
	if !heads[a.ID] || !heads[b.ID] {
		t.Error("both concurrent acts must remain heads; choosing between them " +
			"is the application's judgement, never the ledger's")
	}
}

// A grant carries a subject, a scope, a verb list and whether it may delegate.
// It carries no clock: authority is bounded by events — the end of the work, a
// revocation, or a terminal event named in advance — and testimony about the
// hour never closes it.
//
//	— T5.3
func TestAGrantCarriesNoClock(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	// A write far "later" by any clock is still accepted, because no clock
	// bounds the grant; only an event can.
	late := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("much later"))
	must(t, l, late, Accepted, "a grant does not expire by the hour")

	rev := w.revoke(w.rootPriv, nil, []frame.ID{late.ID}, gr.ID)
	must(t, l, rev, Accepted, "revocation")
	after := w.write(kPriv, &gr.ID, []frame.ID{rev.ID}, "home/journal", "note", []byte("after"))
	must(t, l, after, Rejected, "an event closes the grant where a clock cannot")
}
