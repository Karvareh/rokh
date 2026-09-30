package ledger

import (
	"errors"
	"testing"

	"rokh/frame"
)

// A replayed event — the very same bytes — is a valid no-op that returns the
// verdict already reached. This is what synchronising two copies of a ledger
// does all day, and it must never look like an error.
//
//	— T3.6, T5.4
func TestTheSameEventAgainReturnsItsVerdict(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, _ := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)

	first, err := l.Add(gr.Raw)
	if err != nil {
		t.Fatal(err)
	}
	before := l.Len()
	second, err := l.Add(gr.Raw)
	if err != nil {
		t.Fatalf("the same event a second time was refused: %v", err)
	}
	if first != second {
		t.Fatalf("a replay changed the verdict: %s then %s", first, second)
	}
	if l.Len() != before {
		t.Fatal("a replay grew the ledger")
	}
}

// And the case that looks the same and is not. Two *different* byte strings
// under one full name cannot be produced by hashing, so the seam is made
// inside the package: a different event is planted under an existing name,
// and the ledger is asked what it does with the real one.
//
// It compares the bytes, sees they differ, and refuses. Seeing the collision
// is settled; what should follow is not.
//
//	— T3.5, T3.6, T13.8
func TestTwoDifferentEventsUnderOneNameAreRefused(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, _ := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	other := w.grant([]frame.ID{w.gen.ID}, kPub, "elsewhere", nil, false)
	if string(gr.Raw) == string(other.Raw) {
		t.Fatal("the two fixtures are the same event; the seam proves nothing")
	}

	// The seam: the other event, held under this one's name.
	planted := other
	planted.ID = gr.ID
	l.events[gr.ID] = planted
	l.state[gr.ID] = Accepted

	st, err := l.Add(gr.Raw)
	if !errors.Is(err, ErrNameCollision) {
		t.Fatalf("the ledger accepted a second byte string under one name: %s, %v", st, err)
	}
	if st != Rejected {
		t.Fatalf("a collision must not be accepted; got %s", st)
	}
}
