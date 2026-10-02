package ledger

import (
	"strings"
	"testing"

	"rokh/frame"
)

// The ledger says what happened; it does not say it was good. A measure may
// report "it reads seventy hundredths" — no threshold turns that into
// "accepted". Here the degree is put where the ledger would have to see it,
// and the verdict does not move: acceptance is about bytes, lineage and
// authority, and never about a number crossing a line.
//
//	— T12, T12.1, T12.2, N-Axiom7
func TestNoDegreeInThePayloadChangesTheVerdict(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	// The same event in every shade from certainly-false to certainly-true.
	for _, degree := range []string{
		`{"degree":0.0}`, `{"degree":0.3}`, `{"degree":0.5}`,
		`{"degree":0.7}`, `{"degree":1.0}`, `{"confidence":"none"}`,
	} {
		e := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/measure", "note", []byte(degree))
		if st, err := l.Add(e.Raw); st != Accepted || err != nil {
			t.Fatalf("%s: verdict %s (%v) — the ledger read the number", degree, st, err)
		}
	}
	// And the same event outside its scope is refused whatever it claims of
	// itself: the reason is authority, never the content.
	high := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "elsewhere", "note", []byte(`{"degree":1.0}`))
	must(t, l, high, Rejected, "a perfect degree does not buy authority")
}

// What the ledger proves is the author, the bytes, the lineage and the
// authority presented — not the outward truth of what was said. A plain lie,
// properly signed and properly authorised, is accepted, because the ledger
// witnesses the writer and not the world. Observation is written apart from
// interpretation, and this is where that line falls.
//
//	— T12.5, T12.1, T1.3, N6.3, N-Axiom7
func TestAPlainLieIsAcceptedBecauseTheLedgerWitnessesTheWriter(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	lie := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note",
		[]byte("two and two make five"))
	must(t, l, lie, Accepted, "the ledger judged the content")

	// What it will tell you is who wrote it and under what authority — and
	// nothing at all about whether it is so.
	held, ok := l.Get(lie.ID)
	if !ok {
		t.Fatal("the event is not there")
	}
	if string(held.Event.Author) != string(kPub) {
		t.Fatal("the author is not the key that signed")
	}
	if held.Event.Authority == nil || *held.Event.Authority != gr.ID {
		t.Fatal("the authority is not the grant it named")
	}
}

// Nothing decides on a person's behalf. A ledger left alone produces nothing:
// its state after any amount of reading is exactly its state before.
//
//	— T4, T4.1, T12.4, N7.7
func TestAnUntouchedLedgerProducesNothing(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")
	e := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("a note"))
	must(t, l, e, Accepted, "note")

	before := l.Len()
	beforeHeads := strings.Join(idStrings(l.Heads()), ",")
	a, r, p := l.Tally()

	// Every read there is, many times over.
	for i := 0; i < 50; i++ {
		l.Heads()
		l.Order()
		l.CausalPast()
		l.ActiveGrants()
		l.Tally()
		l.Get(e.ID)
		l.State(e.ID)
		l.Has(e.ID)
		l.Root()
		l.Genesis()
	}

	if l.Len() != before {
		t.Fatalf("reading grew the ledger: %d -> %d", before, l.Len())
	}
	if got := strings.Join(idStrings(l.Heads()), ","); got != beforeHeads {
		t.Fatal("reading moved the heads")
	}
	if a2, r2, p2 := l.Tally(); a2 != a || r2 != r || p2 != p {
		t.Fatalf("reading changed the tally: %d/%d/%d -> %d/%d/%d", a, r, p, a2, r2, p2)
	}
}

// Being right is not a vote. One person alone, offline, checks the whole
// ledger: there is no quorum to reach, no second party to ask, and no token
// to spend on a write. A ledger built and fully judged inside one process is
// the proof — nothing here could have consulted anybody.
//
//	— N-Axiom6, N2.5, N7.4, T1.4, N-Axiom5, N9.2
func TestOnePersonAloneJudgesTheWholeLedger(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	parents := []frame.ID{gr.ID}
	for i := 0; i < 20; i++ {
		e := w.write(kPriv, &gr.ID, parents, "home/journal", "note",
			[]byte(strings.Repeat("x", i+1)))
		must(t, l, e, Accepted, "note")
		parents = []frame.ID{e.ID}
	}
	accepted, rejected, pending := l.Tally()
	if pending != 0 {
		t.Fatalf("%d events are still undecided, yet there is nobody to ask", pending)
	}
	if accepted != l.Len() || rejected != 0 {
		t.Fatalf("a ledger judged alone did not settle: %d accepted, %d rejected, %d in all",
			accepted, rejected, l.Len())
	}
	// And a second, independent reader of the very same bytes reaches the
	// same verdicts without talking to the first.
	other, err := New(w.gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range l.Order() {
		s, _ := l.Get(id)
		if _, err := other.Add(s.Raw); err != nil {
			t.Fatalf("the second reader refused %s: %v", id.Short(), err)
		}
	}
	if a2, r2, p2 := other.Tally(); a2 != accepted || r2 != rejected || p2 != pending {
		t.Fatalf("two readers of one byte string disagreed: %d/%d/%d and %d/%d/%d",
			accepted, rejected, pending, a2, r2, p2)
	}
}

func idStrings(ids []frame.ID) []string {
	out := make([]string, 0, len(ids))
	for _, i := range ids {
		out = append(out, i.String())
	}
	return out
}
