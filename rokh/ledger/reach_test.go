package ledger

import (
	"testing"

	"rokh/frame"
)

// A history shown to you being correct does not prove it whole.
//
// The owner of the key writes two branches and shows one. Both verify: every
// byte against its hash, every hash against its signature, every signature
// against the authority live at that point. Nothing in the shown branch
// mentions the hidden one, and there is nothing a receiver could look at
// harder to find it. This is not a hole to be patched — it is the ceiling of
// what verification establishes.
//
//	— T5.6
func TestAVerifiedHistoryIsNotThereforeAWholeOne(t *testing.T) {
	w := newWorld(t)
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)

	// The owner writes two branches from the same point.
	shownA := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("A1"))
	shownB := w.write(kPriv, &gr.ID, []frame.ID{shownA.ID}, "home/journal", "note", []byte("A2"))
	hidden := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("B1"))

	// The receiver is handed one branch and only that.
	shown := w.ledger()
	must(t, shown, gr, Accepted, "grant")
	must(t, shown, shownA, Accepted, "A1")
	must(t, shown, shownB, Accepted, "A2")

	// It verifies completely. Every event in it is accepted.
	acc, rej, pend := shown.Tally()
	if rej != 0 || pend != 0 {
		t.Fatalf("the shown history did not verify cleanly: %d rejected, %d pending", rej, pend)
	}
	if acc != shown.Len() {
		t.Fatal("not every event in the shown history was accepted")
	}

	// And with no prior sighting, that is the whole of what the receiver
	// learns. Sound. Not whole — the word is not available to it.
	reach, missing := shown.Against(nil)
	if reach != Sound {
		t.Fatalf("a receiver with no prior witness concluded %q", reach)
	}
	if len(missing) != 0 {
		t.Fatal("it named something as missing that it could not have known about")
	}
	// The hidden branch leaves no trace in what was handed over.
	if shown.Has(hidden.ID) {
		t.Fatal("the hidden branch was in the shown history after all")
	}
	for _, id := range shown.Order() {
		s, _ := shown.Get(id)
		for _, p := range s.Event.Parents {
			if p == hidden.ID {
				t.Fatal("the shown history points at the hidden branch")
			}
		}
	}
}

// What breaks the concealment is evidence from before, and only that.
//
// A receipt someone kept, an anchor laid down earlier somewhere else: something
// that already existed and names an event. Held up against the history now
// shown, it either sits inside it or it does not, and if it does not the
// concealment is visible. This is not consensus and could not be — the
// signature is valid on both branches and the ledger is one person's, so there
// is no peer whose disagreement would mean anything.
//
//	— T5.6, N2.6
func TestOnlyAPriorWitnessRevealsTheBranchThatWasHidden(t *testing.T) {
	w := newWorld(t)
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	a1 := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("A1"))
	hidden := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("B1"))

	shown := w.ledger()
	must(t, shown, gr, Accepted, "grant")
	must(t, shown, a1, Accepted, "A1")

	// Someone kept a receipt naming the hidden event. Now the history shown
	// does not account for something that was seen.
	reach, missing := shown.Against([]frame.ID{hidden.ID})
	if reach != Diverges {
		t.Fatalf("a witness of the hidden branch concluded %q", reach)
	}
	if len(missing) != 1 || missing[0] != hidden.ID {
		t.Fatalf("the missing event was named as %v", missing)
	}

	// A witness of something the history does contain reads as growth, not
	// concealment.
	if reach, missing := shown.Against([]frame.ID{gr.ID, a1.ID}); reach != Extends || missing != nil {
		t.Fatalf("a witness the history accounts for read as %q, missing %v", reach, missing)
	}

	// And a witness naming an event that was never written anywhere also
	// diverges: the ledger does not have it, and the ledger is what answers.
	never := frame.Hash([]byte("an event nobody wrote"))
	if reach, _ := shown.Against([]frame.ID{never}); reach != Diverges {
		t.Fatalf("a witness of something never written read as %q", reach)
	}

	// A rejected event is not held either, whatever anyone witnessed. Rejection
	// means never was.
	stranger, strangerPriv := newKey(t)
	_ = stranger
	bad := w.write(strangerPriv, nil, []frame.ID{a1.ID}, "home/journal", "note", []byte("X"))
	must(t, shown, bad, Rejected, "unauthorised")
	if reach, _ := shown.Against([]frame.ID{bad.ID}); reach != Diverges {
		t.Fatal("a rejected event counted as held")
	}
}
