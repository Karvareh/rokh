package ledger

import (
	"testing"

	"rokh/frame"
)

// Closed: authority comes from this ledger's own causal past and nowhere else.
//
// A key this ledger never granted anything to cannot write in it, and the
// answer is rejection rather than a queue or a lower confidence. There is
// nothing to point at that would make such a writer legitimate — no registry,
// no authority server, no set of peers — which is exactly why the ledger is
// entitled to say yes to the writers it does know.
//
//	— T1, T3.3
func TestTheLedgerIsClosedToAuthorityFromAnywhereElse(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	mine := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("mine"))
	must(t, l, mine, Accepted, "a granted writer")

	// A key with a perfectly valid signature and no grant here.
	_, strangerPriv := newKey(t)
	stranger := w.write(strangerPriv, nil, []frame.ID{mine.ID}, "home/journal", "note",
		[]byte("I signed this myself"))
	must(t, l, stranger, Rejected, "an ungranted writer")

	// Rejected means never was, and not only for input that failed to parse.
	// This event parsed perfectly; it was refused on authority, and nothing of
	// it is kept — not its bytes, not its author, not its payload.
	if l.Has(stranger.ID) {
		t.Fatal("a rejected event was kept")
	}
	if _, ok := l.Get(stranger.ID); ok {
		t.Fatal("a rejected event can still be read out of the ledger")
	}
	for _, id := range l.Order() {
		if id == stranger.ID {
			t.Fatal("a rejected event appears in the order")
		}
	}
	// The verdict against the name survives, because it must: a child of a
	// refused event has to be refusable, and that needs the name and the
	// verdict — one bit — not the event.
	if l.State(stranger.ID) != Rejected {
		t.Fatal("the ledger forgot that it had refused the name")
	}
	if _, rejected, _ := l.Tally(); rejected != 1 {
		t.Fatalf("the tally counted %d rejections", rejected)
	}
	// And the name stays refused. Offering it again is not a new question.
	if st, err := l.Add(stranger.Raw); st != Rejected || err == nil {
		t.Fatalf("a refused name got a second hearing: %v, %v", st, err)
	}
	// A child of it is refused too, which is what the surviving verdict is for.
	child := w.write(kPriv, &gr.ID, []frame.ID{stranger.ID}, "home/journal",
		"note", []byte("built on a refusal"))
	must(t, l, child, Rejected, "a child of a refused event")
	// And naming somebody else's grant does not help, because the authority
	// has to be in *this* ledger's past.
	elsewhere := frame.Hash([]byte("a grant in another ledger"))
	borrowed := w.write(strangerPriv, &elsewhere, []frame.ID{mine.ID}, "home/journal",
		"note", []byte("but they said I could"))
	must(t, l, borrowed, Rejected, "an authority from elsewhere")
}

// Alive: it keeps being written to, it grows, and opening it again resumes it.
//
// The settledness of the past is what makes that possible rather than what
// prevents it — an accepted event stays accepted and a rejected one stays
// rejected, so nothing already written has to be reconsidered in order to
// write the next thing.
//
//	— T1, T6.2
func TestTheLedgerIsAliveNotAnArchive(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant")

	// It grows, and each write leaves the earlier ones exactly as they were.
	parents := []frame.ID{gr.ID}
	var written []frame.ID
	for i := 0; i < 20; i++ {
		e := w.write(kPriv, &gr.ID, parents, "home/journal", "note", []byte{byte('a' + i)})
		must(t, l, e, Accepted, "note")
		parents = []frame.ID{e.ID}
		written = append(written, e.ID)
		for _, prior := range written {
			if l.State(prior) != Accepted {
				t.Fatalf("writing unsettled something already accepted: %s", prior.Short())
			}
		}
	}
	if l.Len() != len(written)+2 { // the genesis and the grant
		t.Fatalf("the ledger holds %d events after %d writes", l.Len(), len(written))
	}

	// Reopening resumes rather than starting over: the same bytes, read again,
	// give back the same ledger with the same past.
	again, err := New(w.gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	must(t, again, gr, Accepted, "grant on reopen")
	for i, id := range written {
		s, ok := l.Get(id)
		if !ok {
			t.Fatalf("event %d vanished", i)
		}
		if _, err := again.Add(s.Raw); err != nil {
			t.Fatalf("replaying event %d: %v", i, err)
		}
		if again.State(id) != Accepted {
			t.Fatalf("event %d did not come back accepted", i)
		}
	}
	if again.Len() != l.Len() {
		t.Fatalf("the reopened ledger holds %d, the original %d", again.Len(), l.Len())
	}
	// And it is still writable: reopening is not closing.
	more := w.write(kPriv, &gr.ID, again.Heads(), "home/journal", "note", []byte("after"))
	must(t, again, more, Accepted, "a write after reopening")
}
