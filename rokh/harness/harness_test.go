package harness

import (
	"errors"
	"testing"

	"rokh/event"
)

func clerk() Covenant {
	return Covenant{
		Namespace: "clerk",
		Version:   "0.1",
		Can:       []string{"clerk.ask", "clerk.answer", "clerk.decline"},
		Unknown:   Refuse,
	}
}

// The covenant has four clauses and all four are required: a space of its own,
// a version, everything it can do, and what it does with a verb it has never
// heard. Three of four binds nothing.
//
//	— T11.10
func TestAllFourClausesAreRequired(t *testing.T) {
	for name, break_ := range map[string]func(*Covenant){
		"no namespace": func(c *Covenant) { c.Namespace = "" },
		"no version":   func(c *Covenant) { c.Version = "" },
		"no list":      func(c *Covenant) { c.Can = nil },
		"no answer":    func(c *Covenant) { c.Unknown = "" },
	} {
		c := clerk()
		break_(&c)
		if _, err := c.Bind(); !errors.Is(err, ErrIncomplete) {
			t.Errorf("%s: bound anyway (%v)", name, err)
		}
	}
	if _, err := clerk().Bind(); err != nil {
		t.Fatalf("a complete covenant was refused: %v", err)
	}
}

// There is no third answer. A harness either refuses an unknown verb or lets
// it pass — it never guesses, because a guessed meaning is a meaning nobody
// granted.
//
//	— T11.10, T12.3
func TestThereIsNoAnswerThatGuesses(t *testing.T) {
	for _, a := range []Answer{"guess", "infer", "best-effort", "assume"} {
		c := clerk()
		c.Unknown = a
		if _, err := c.Bind(); !errors.Is(err, ErrIncomplete) {
			t.Errorf("%q was accepted as an answer to an unknown verb", a)
		}
	}
	for _, a := range []Answer{Refuse, Ignore} {
		c := clerk()
		c.Unknown = a
		if _, err := c.Bind(); err != nil {
			t.Errorf("%q was refused as an answer: %v", a, err)
		}
	}
}

// A harness declares verbs in its own space and nowhere else, and the core's
// space is not on offer to anyone.
//
//	— T11.10
func TestAHarnessDeclaresOnlyItsOwnNames(t *testing.T) {
	c := clerk()
	c.Can = append(c.Can, "someone-else.write")
	if _, err := c.Bind(); !errors.Is(err, ErrOutsideNamespace) {
		t.Fatalf("a harness declared a verb outside its space: %v", err)
	}

	c = clerk()
	c.Can = append(c.Can, event.VerbGrant)
	if _, err := c.Bind(); err == nil {
		t.Fatal("a harness declared one of the core's own verbs")
	}

	for _, ns := range []string{event.AddressRoot, event.AddressRoot + "/inner"} {
		c := clerk()
		c.Namespace = ns
		c.Can = []string{ns + ".do"}
		if _, err := c.Bind(); !errors.Is(err, ErrReserved) {
			t.Errorf("a harness claimed %q, which belongs to the core: %v", ns, err)
		}
	}
}

// An unknown verb inside the harness's own space gets exactly the answer the
// covenant declared, and never becomes a known one.
//
//	— T11.10, T12.3
func TestAnUnknownVerbGetsTheDeclaredAnswer(t *testing.T) {
	refusing, _ := clerk().Bind()
	ignoring := clerk()
	ignoring.Unknown = Ignore
	ignoring, _ = ignoring.Bind()

	known := event.Event{Address: "clerk/desk", Verb: "clerk.ask"}
	strange := event.Event{Address: "clerk/desk", Verb: "clerk.rummage"}
	foreign := event.Event{Address: "home/journal", Verb: "note"}

	for _, tc := range []struct {
		c    Covenant
		e    event.Event
		want Reading
	}{
		{refusing, known, Known},
		{refusing, strange, Refused},
		{refusing, foreign, NotMine},
		{ignoring, strange, Ignored},
		{ignoring, known, Known},
	} {
		if got := tc.c.Read(tc.e); got != tc.want {
			t.Errorf("%s/%s: read as %s, want %s", tc.e.Address, tc.e.Verb, got, tc.want)
		}
	}
	// A near-miss address is not inside the space.
	if got := refusing.Read(event.Event{Address: "narikhaneh", Verb: "clerk.ask"}); got != NotMine {
		t.Errorf("narikhaneh was read as being inside clerk: %s", got)
	}
}

// Two harnesses cannot share a space. If they did, one verb would carry two
// meanings and the ledger would have to choose between them — which is the one
// thing it must never do.
//
//	— T11.10, T12
func TestTwoHarnessesCannotClaimOneSpace(t *testing.T) {
	r := NewRegister()
	if err := r.Bind(clerk(), plainEffects()); err != nil {
		t.Fatal(err)
	}
	same := clerk()
	same.Version = "0.2"
	if err := r.Bind(same, plainEffects()); !errors.Is(err, ErrNamespaceTaken) {
		t.Fatalf("a second harness took the same space: %v", err)
	}
	// Nor may one nest inside the other.
	inner := Covenant{Namespace: "clerk/inner", Version: "1", Can: []string{"clerk/inner.do"}, Unknown: Ignore}
	if err := r.Bind(inner, plainEffects()); err == nil {
		t.Fatal("a harness nested inside another's space")
	}
	// A separate space is fine.
	other := Covenant{Namespace: "archive", Version: "1", Can: []string{"archive.keep"}, Unknown: Refuse}
	if err := r.Bind(other, plainEffects()); err != nil {
		t.Fatalf("an unrelated space was refused: %v", err)
	}
	if c, ok := r.For(event.Event{Address: "archive/box", Verb: "archive.keep"}); !ok || c.Namespace != "archive" {
		t.Fatal("the register did not find the harness whose space the event is in")
	}
}

// The version is part of the covenant because meaning changes between
// versions. A reader must be able to say which version wrote a thing, and the
// register keeps that with the binding.
//
//	— T11.10
func TestTheVersionTravelsWithTheCovenant(t *testing.T) {
	r := NewRegister()
	c := clerk()
	c.Version = "0.7"
	if err := r.Bind(c, plainEffects()); err != nil {
		t.Fatal(err)
	}
	got, ok := r.For(event.Event{Address: "clerk/desk", Verb: "clerk.ask"})
	if !ok {
		t.Fatal("the harness was not found")
	}
	if got.Version != "0.7" {
		t.Fatalf("the version did not survive binding: %q", got.Version)
	}
}

// plainEffects is a harness whose destination cooperates and which does not
// resend on its own — the least interesting of the sixteen combinations, used
// where a test is about the covenant and not about the effects.
func plainEffects() Effects {
	return Effects{Repeat: Idempotent, Retry: NeverRetry,
		Compensate: Compensable, Ending: CloseUnknown}
}
