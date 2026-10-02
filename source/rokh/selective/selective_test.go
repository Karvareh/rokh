package selective

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"testing"
)

// A stand-in commitment scheme, and only a stand-in. The byte form is an open
// ruling, so nothing here may be read as the choice: it exists to show that
// the rule holds whatever the scheme turns out to be.
//
//	— T7.5, T13.1
type saltedHash struct{ salt []byte }

func (s saltedHash) Commit(field string, value []byte) ([]byte, error) {
	m := hmac.New(sha256.New, s.salt)
	m.Write([]byte(field))
	m.Write([]byte{0})
	m.Write(value)
	return m.Sum(nil), nil
}

func (s saltedHash) Opens(field string, value, commitment []byte) bool {
	want, _ := s.Commit(field, value)
	return hmac.Equal(want, commitment)
}

func whole() Whole {
	return Whole{
		Fields: map[string][]byte{
			"name":     []byte("the owner of this ledger"),
			"born":     []byte("1990"),
			"town":     []byte("somewhere"),
			"licence":  []byte("valid until the work ends"),
			"debtOwed": []byte("none"),
		},
		Binding: []byte("a signature over the commitments"),
	}
}

// One field is shown on its own, and the receiver checks it without seeing the
// rest. The least that is needed to check goes, and nothing more.
//
//	— T7.5
func TestOneFieldTravelsAndTheRestDoesNot(t *testing.T) {
	w, c := whole(), saltedHash{salt: []byte("a salt")}

	s, err := Least(w, c, "licence")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Shown) != 1 {
		t.Fatalf("more than the asked-for field travelled: %v", s.Shown)
	}
	if string(s.Shown["licence"]) != "valid until the work ends" {
		t.Fatal("the field asked for did not arrive")
	}
	// Everything else is present only as an opaque stand-in.
	for _, name := range s.Withheld() {
		cm := s.Commitments[name]
		if string(cm) == string(w.Fields[name]) {
			t.Fatalf("%q travelled in the clear", name)
		}
		if len(cm) == 0 {
			t.Fatalf("%q travelled as nothing at all", name)
		}
	}
	if len(s.Withheld()) != len(w.Fields)-1 {
		t.Fatalf("not every other field was accounted for: %v", s.Withheld())
	}
	// And the receiver can check what it was given.
	if err := Audit(s, c, w, "licence"); err != nil {
		t.Fatalf("a correct showing was refused: %v", err)
	}
}

// More than the least is not generosity; it is disclosure nobody authorised.
//
//	— T7.5, T7.3
func TestAShowingThatCarriesMoreIsRefused(t *testing.T) {
	w, c := whole(), saltedHash{salt: []byte("a salt")}
	s, _ := Least(w, c, "licence")

	// Somebody slips one more field in on the way.
	s.Shown["debtOwed"] = w.Fields["debtOwed"]
	delete(s.Commitments, "debtOwed")
	if err := Audit(s, c, w, "licence"); !errors.Is(err, ErrShowsMore) {
		t.Fatalf("an unasked-for field passed: %v", err)
	}
}

// A withheld field must travel as a commitment and as nothing else — not in
// the clear, not as a stand-in that does not stand for it, and not missing
// altogether.
//
//	— T7.5
func TestAWithheldFieldTravelsAsACommitmentOnly(t *testing.T) {
	w, c := whole(), saltedHash{salt: []byte("a salt")}

	bare, _ := Least(w, c, "licence")
	bare.Commitments["town"] = w.Fields["town"]
	if err := Audit(bare, c, w, "licence"); !errors.Is(err, ErrNotCommitted) {
		t.Fatalf("a withheld field in the clear passed: %v", err)
	}

	wrong, _ := Least(w, c, "licence")
	wrong.Commitments["town"] = []byte("a commitment to something else entirely")
	if err := Audit(wrong, c, w, "licence"); !errors.Is(err, ErrNotCommitted) {
		t.Fatalf("a commitment that stands for nothing passed: %v", err)
	}

	gone, _ := Least(w, c, "licence")
	delete(gone.Commitments, "town")
	if err := Audit(gone, c, w, "licence"); !errors.Is(err, ErrShowsLess) {
		t.Fatalf("a field that vanished entirely passed: %v", err)
	}
}

// Fields without the thing that ties them to the event are fields with no
// reason to be believed.
//
//	— T7.5
func TestWithoutTheBindingThereIsNothingToCheckAgainst(t *testing.T) {
	w, c := whole(), saltedHash{salt: []byte("a salt")}
	s, _ := Least(w, c, "licence")
	s.Binding = nil
	if err := Audit(s, c, w, "licence"); !errors.Is(err, ErrShowsLess) {
		t.Fatalf("a showing with nothing tying it to the event passed: %v", err)
	}
}

// The rule does not depend on the scheme. Swap the commitment for another and
// every check above still holds — which is what lets the byte form stay open
// while the rule is closed.
//
//	— T7.5, T13.1
func TestTheRuleHoldsWhateverTheSchemeTurnsOutToBe(t *testing.T) {
	w := whole()
	for _, c := range []Commit{
		saltedHash{salt: []byte("one salt")},
		saltedHash{salt: []byte("an entirely different salt")},
	} {
		s, err := Least(w, c, "born", "town")
		if err != nil {
			t.Fatal(err)
		}
		if err := Audit(s, c, w, "born", "town"); err != nil {
			t.Fatalf("the rule failed under a different scheme: %v", err)
		}
		if len(s.Shown) != 2 {
			t.Fatalf("asking for two fields showed %d", len(s.Shown))
		}
	}
	if _, err := Least(w, saltedHash{}, "no such field"); !errors.Is(err, ErrUnknownField) {
		t.Fatal("a field the event does not have was accepted")
	}
}

// The receiver checks holding only what travelled.
//
// This is the requirement itself, and it is easy to satisfy in a way that
// means nothing: a check that takes the whole event alongside the showing
// verifies beautifully and discloses everything, because the receiver already
// has what it was supposed to be spared. So the receiver's function is given
// the showing and nothing else, and the compiler enforces it — there is no
// parameter through which the rest could arrive.
//
//	— T7.5, T7.3
func TestTheReceiverChecksHoldingOnlyWhatTravelled(t *testing.T) {
	w, c := whole(), saltedHash{salt: []byte("a salt")}
	s, err := Least(w, c, "licence")
	if err != nil {
		t.Fatal(err)
	}

	// Everything the receiver has. Nothing else is in scope from here on.
	if err := Check(s, c, "licence"); err != nil {
		t.Fatalf("a correct showing was refused: %v", err)
	}

	// It really is checking, not merely accepting. Substitute the value and
	// the commitment no longer opens to it.
	tampered := Showing{
		Shown:       map[string][]byte{"licence": []byte("valid forever")},
		Commitments: s.Commitments,
		Binding:     s.Binding,
	}
	if err := Check(tampered, c, "licence"); !errors.Is(err, ErrNotCommitted) {
		t.Fatalf("a substituted value passed: %v", err)
	}

	// A shown field with nothing tying it to the event is not checkable.
	loose := Showing{
		Shown:       map[string][]byte{"licence": w.Fields["licence"]},
		Commitments: map[string][]byte{},
		Binding:     s.Binding,
	}
	if err := Check(loose, c, "licence"); !errors.Is(err, ErrShowsLess) {
		t.Fatalf("a field with no commitment behind it passed: %v", err)
	}

	// And what the receiver holds does not contain the withheld values in any
	// form it can read. This is the property the whole package exists for, so
	// it is checked rather than assumed.
	for name, v := range w.Fields {
		if name == "licence" {
			continue
		}
		if _, leaked := s.Shown[name]; leaked {
			t.Fatalf("%q travelled in the clear", name)
		}
		if string(s.Commitments[name]) == string(v) {
			t.Fatalf("%q is readable from its commitment", name)
		}
	}
}

// The owner's audit asks what the receiver cannot: does each commitment really
// stand for the field it was made from. The two are separate functions because
// they are separate questions, and only one of them may hold the whole event.
//
//	— T7.5
func TestTheOwnersAuditAsksWhatTheReceiverCannot(t *testing.T) {
	w, c := whole(), saltedHash{salt: []byte("a salt")}

	// A commitment for a withheld field, swapped for one over other bytes.
	// The receiver cannot tell — it has nothing to compare against, and that
	// is not a flaw in the check but the shape of not being shown a thing.
	swapped, _ := Least(w, c, "licence")
	swapped.Commitments["town"], _ = c.Commit("town", []byte("somewhere else"))
	if err := Check(swapped, c, "licence"); err != nil {
		t.Fatalf("the receiver claimed to know something it cannot: %v", err)
	}
	// The owner can, because the owner has the field.
	if err := Audit(swapped, c, w, "licence"); !errors.Is(err, ErrNotCommitted) {
		t.Fatalf("the owner's audit missed a swapped commitment: %v", err)
	}

	// And a commitment for a field the event does not have.
	extra, _ := Least(w, c, "licence")
	extra.Commitments["invented"], _ = c.Commit("invented", []byte("nothing"))
	if err := Audit(extra, c, w, "licence"); !errors.Is(err, ErrShowsMore) {
		t.Fatalf("the owner's audit missed an invented field: %v", err)
	}
}
