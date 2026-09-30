package receipt_test

import (
	"testing"

	"rokh/receipt"
)

// Two receipts open at once, and closing one leaves the other open.
//
// This is the case the receipt exists for, and it was the case that broke it.
// A keeper builds on the branch head, so a result recorded while another
// receipt is open carries that open intent among its parents — not because it
// answers it, but because the branch happened to be sitting there. Reading the
// parents as the pairing made the result answer both, and the receipt nobody
// had closed disappeared from the open list. A reader that says "nothing is
// open" about an unanswered receipt is worse than no reader at all.
//
// The sixth witness is what the two halves agree on, and it names one intent.
//
//	— T10.1, T10.2, T10.4
func TestAnOpenReceiptSurvivesAnotherReceiptClosing(t *testing.T) {
	k := newKeeper(t)
	r := receipt.New("home/print", k)

	first, err := r.Open(receipt.Intent{Doing: "charge the card", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Open(receipt.Intent{Doing: "ship the box", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}

	open, err := r.Unanswered()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("two were opened and %d are open", len(open))
	}

	// The second intent is now the branch head, so the result below takes it
	// as a parent. It answers the first and nothing else.
	if _, err := r.Close(first, receipt.Result{
		Outcome: receipt.Done, Saying: "card charged", Witness: witness(),
	}); err != nil {
		t.Fatal(err)
	}

	open, err = r.Unanswered()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0] != second {
		t.Fatalf("the unanswered receipt is %s; got %v", second.Short(), open)
	}
	if got, yes, err := r.Answered(second); err != nil {
		t.Fatal(err)
	} else if yes {
		t.Fatalf("nobody answered %s and %s is reported as its result",
			second.Short(), got.Short())
	}
	if got, yes, err := r.Answered(first); err != nil {
		t.Fatal(err)
	} else if !yes {
		t.Fatal("the answered receipt reports no result")
	} else if _, found := k.l.Get(got); !found {
		t.Fatal("the result named is not in the ledger")
	}

	// And closing the second one closes it, so nothing has been made
	// permanently unanswerable in the course of protecting it.
	if _, err := r.Close(second, receipt.Result{
		Outcome: receipt.Failed, Saying: "the box was lost", Witness: witness(),
	}); err != nil {
		t.Fatal(err)
	}
	open, err = r.Unanswered()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("both were answered and %d is open", len(open))
	}
}

// A result is written where its intent stands, so every aperture that sees one
// half sees the other.
//
// The address is the aperture. A ritual opened at a place reaches the intents
// written below it, so a ritual at "home" can close an intent at
// "home/print" — and writing the result at "home" left a reader at
// "home/print" seeing the intent and never the result. It reported a closed
// receipt as open for as long as the receipt existed, and a harness closing
// its books on that reading wrote a second result for a receipt that already
// had one.
//
//	— T10, T10.1, T10.6
func TestAResultIsWrittenWhereItsIntentStands(t *testing.T) {
	k := newKeeper(t)
	below := receipt.New("home/print", k)
	above := receipt.New("home", k)

	intent, err := below.Open(receipt.Intent{Doing: "print the page", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}
	// Closed from the wider aperture, which is allowed to reach it.
	result, err := above.Close(intent, receipt.Result{
		Outcome: receipt.Done, Saying: "printed", Witness: witness(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s, found := k.l.Get(result)
	if !found {
		t.Fatal("the result is not in the ledger")
	}
	if s.Event.Address != "home/print" {
		t.Fatalf("the result stands at %q and its intent at %q", s.Event.Address, "home/print")
	}
	// Both readers now agree, which is the whole point of the placement.
	for _, r := range []*receipt.Ritual{below, above} {
		open, err := r.Unanswered()
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != 0 {
			t.Fatalf("at %q the answered receipt is still open: %v", r.Address(), open)
		}
		if _, yes, err := r.Answered(intent); err != nil {
			t.Fatal(err)
		} else if !yes {
			t.Fatalf("at %q the answered receipt reports no result", r.Address())
		}
	}
}
