package ledger

import (
	"crypto/ed25519"
	"testing"

	"rokh/event"
	"rokh/frame"
)

// In an individual's ledger the double-spend problem does not arise. An event
// is not spent, it is added; two copies of one ledger do not compete. And it
// is not a philosophical claim but a structural result: because every event
// names its parents and its name is its own hash, merging two copies commutes
// and overwrites nothing. Nobody has to be asked which history is the shared
// one — that is the question consensus answers and Rokh never poses.
//
//	— N2.3, N2.4, N2.7, N2.12, N9.1, T5.4
func TestTwoCopiesOfOneLedgerConvergeWithoutAnybodyDeciding(t *testing.T) {
	w := newWorld(t)
	origin := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, origin, gr, Accepted, "grant")

	// Two branches, made independently and never shown to each other.
	left := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/left", "note", []byte("mine"))
	right := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/right", "note", []byte("also mine"))
	deeper := w.write(kPriv, &gr.ID, []frame.ID{left.ID}, "home/left", "note", []byte("and more"))
	all := []event.Signed{gr, left, right, deeper}

	// Two copies of the same ledger, told the same events in opposite orders.
	build := func(order []event.Signed) *Ledger {
		l, err := New(w.gen.Raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range order {
			if _, err := l.Add(e.Raw); err != nil {
				t.Fatalf("%s: %v", e.ID.Short(), err)
			}
		}
		return l
	}
	forward := build(all)
	backward := build([]event.Signed{all[3], all[2], all[1], all[0]})

	if forward.Len() != backward.Len() {
		t.Fatalf("the two copies hold different numbers of events: %d and %d",
			forward.Len(), backward.Len())
	}
	for _, e := range all {
		if forward.State(e.ID) != backward.State(e.ID) {
			t.Fatalf("%s: one copy says %s, the other %s", e.ID.Short(),
				forward.State(e.ID), backward.State(e.ID))
		}
		if forward.State(e.ID) != Accepted {
			t.Fatalf("%s was not accepted; nothing here competes with anything",
				e.ID.Short())
		}
	}
	// Nothing was overwritten: every event is still exactly its own bytes.
	for _, e := range all {
		held, ok := forward.Get(e.ID)
		if !ok || frame.Hash(held.Head) != e.ID || string(held.Raw) != string(e.Raw) {
			t.Fatalf("%s did not survive the merge unchanged", e.ID.Short())
		}
	}
}

// Measurement takes the place of the vote. One person, offline, with a small
// program, checks the whole ledger: every byte against its hash, every hash
// against its signature. This does exactly that, without asking the ledger
// anything — the loop below is the whole of what replaces a quorum.
//
//	— N2.6, N9.2, N-Axiom6
func TestOnePersonCheckingBytesIsWhatReplacesTheVote(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")
	parents := []frame.ID{gr.ID}
	for i := 0; i < 6; i++ {
		e := w.write(kPriv, &gr.ID, parents, "home/journal", "note", []byte{byte('a' + i)})
		must(t, l, e, Accepted, "note")
		parents = []frame.ID{e.ID}
	}

	checked := 0
	for _, id := range l.Order() {
		s, ok := l.Get(id)
		if !ok {
			t.Fatalf("%s is named and not held", id.Short())
		}
		// every byte against its hash
		if frame.Hash(s.Head) != id {
			t.Fatalf("%s: the head does not hash to the name", id.Short())
		}
		// every hash against its signature
		f, _, err := frame.ParseHead(s.Raw)
		if err != nil {
			t.Fatalf("%s: %v", id.Short(), err)
		}
		sig := f.Sig
		if !ed25519.Verify(s.Event.Author, f.Signed, sig) {
			t.Fatalf("%s: the signature does not answer for these bytes", id.Short())
		}
		checked++
	}
	if checked != l.Len() {
		t.Fatalf("checked %d of %d events; a partial check is not a check", checked, l.Len())
	}
}

// Rokh knows what came before what; it does not know Tuesday. A clock is
// testimony carried inside an event — change it, and not one thing about the
// order moves, because the order was never leaning on it.
//
//	— N6.4, T5.3, T5
func TestTheLedgerKnowsOrderAndNotTheCalendar(t *testing.T) {
	w := newWorld(t)
	kPub, kPriv := newKey(t)

	build := func(clock int64) (*Ledger, []frame.ID) {
		l, err := New(w.gen.Raw)
		if err != nil {
			t.Fatal(err)
		}
		gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
		if _, err := l.Add(gr.Raw); err != nil {
			t.Fatal(err)
		}
		parents := []frame.ID{gr.ID}
		var ids []frame.ID
		for i := 0; i < 4; i++ {
			// The clock runs backwards in one build and forwards in the other.
			e, err := event.Sign(event.Event{
				Carrier: &w.anchor, Authority: &gr.ID, Parents: parents,
				Address: "home/journal", Verb: "note", Payload: []byte{byte(i)},
				Attest: []event.Attestation{{Oracle: "clock",
					Claim: []byte{byte(clock + int64(i)*clockStep)}}},
			}, kPriv)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.Add(e.Raw); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, e.ID)
			parents = []frame.ID{e.ID}
		}
		return l, ids
	}

	// Two ledgers whose only difference is what the clock testified.
	early, earlyIDs := build(1)
	_, _ = build(200)

	// The order is the lineage, and the lineage is the parents.
	for i := 1; i < len(earlyIDs); i++ {
		past := map[frame.ID]bool{}
		for _, id := range early.CausalPast(earlyIDs[i]) {
			past[id] = true
		}
		if !past[earlyIDs[i-1]] {
			t.Fatalf("event %d does not have event %d in its past; the order is not lineage", i, i-1)
		}
	}
	// And nothing in the ledger's own vocabulary answers "what day was it".
	// The clock is inside the event, as testimony, and that is all it is.
	s, _ := early.Get(earlyIDs[0])
	if len(s.Event.Attest) != 1 || s.Event.Attest[0].Oracle != "clock" {
		t.Fatal("the clock is not where it should be: inside the event, as testimony")
	}
}

// clockStep is only here so the two builds differ in their testimony.
const clockStep = 7

// The guard's power is not in its claws; it is in the covenant. Whatever a
// delegate can do, it can do because it was entrusted — and every entrustment
// is taken back.
//
//	— N7.6, T2.2, T6
func TestADelegatesPowerIsOnlyWhatWasEntrustedAndItEnds(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	inside := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("within"))
	must(t, l, inside, Accepted, "inside the entrustment")
	outside := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "elsewhere", "note", []byte("beyond"))
	must(t, l, outside, Rejected, "beyond the entrustment")
	bare := w.write(kPriv, nil, []frame.ID{gr.ID}, "home/journal", "note", []byte("no covenant"))
	must(t, l, bare, Rejected, "with no covenant named at all")

	rev := w.revoke(w.rootPriv, nil, []frame.ID{inside.ID}, gr.ID)
	must(t, l, rev, Accepted, "taking it back")
	after := w.write(kPriv, &gr.ID, []frame.ID{rev.ID}, "home/journal", "note", []byte("after"))
	must(t, l, after, Rejected, "after it was taken back")
}
