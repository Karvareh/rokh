package harness

import (
	"errors"
	"testing"
)

// Rokh alone does not promise that an effect outside it happens exactly once.
// What it does is make every harness say, before it is bound, what its
// destination actually does — and a harness that has not said all four has not
// declared its effects at all. There is no default here, because a default
// would be Rokh guessing what somewhere else does.
//
//	— T10.7
func TestAHarnessDeclaresAllFourEffectsOrNone(t *testing.T) {
	full := Effects{Repeat: Idempotent, Retry: MayRetry,
		Compensate: Compensable, Ending: AskAPerson}
	if _, err := full.Declare(); err != nil {
		t.Fatalf("a complete declaration was refused: %v", err)
	}

	for name, hole := range map[string]Effects{
		"no repeat":     {Retry: MayRetry, Compensate: Compensable, Ending: AskAPerson},
		"no retry":      {Repeat: Idempotent, Compensate: Compensable, Ending: AskAPerson},
		"no compensate": {Repeat: Idempotent, Retry: MayRetry, Ending: AskAPerson},
		"no ending":     {Repeat: Idempotent, Retry: MayRetry, Compensate: Compensable},
	} {
		if _, err := hole.Declare(); !errors.Is(err, ErrUndeclared) {
			t.Errorf("%s: declared anyway (%v)", name, err)
		}
	}

	// It refuses rather than repairs: an invented answer is not an answer.
	made_up := full
	made_up.Repeat = "probably fine"
	if _, err := made_up.Declare(); !errors.Is(err, ErrUndeclared) {
		t.Fatal("an invented answer was accepted")
	}
}

// There is no ending that says "assume it failed". Both honest answers close
// the receipt as unknown; they differ only in whether a person is shown it.
// Assuming a failure is a guess, and a guess recorded as a fact cannot be told
// apart from knowledge afterwards.
//
//	— T10.4, T10.7
func TestThereIsNoEndingThatGuessesFailure(t *testing.T) {
	for _, e := range []Ending{CloseUnknown, AskAPerson} {
		if !e.valid() {
			t.Fatalf("%q is not among the endings", e)
		}
	}
	for _, guess := range []Ending{"failed", "assume-failed", "done", "probably-worked"} {
		if guess.valid() {
			t.Fatalf("%q was accepted as an ending", guess)
		}
	}
}

// Whether a resend is safe is the arithmetic of the four declarations, not a
// fifth rule. A harness that may retry into a destination that duplicates has
// said, in its own words, that its effect will land twice. Nothing forbids the
// combination — that would be Rokh ruling on somewhere else — but nothing calls
// it safe either.
//
//	— T10.7
func TestWhetherAResendIsSafeIsReadOffTheDeclarationsNotAssumed(t *testing.T) {
	cases := []struct {
		repeat Repeat
		retry  Retry
		safe   bool
	}{
		{Idempotent, MayRetry, true},
		{Idempotent, NeverRetry, false},
		{Duplicates, MayRetry, false},
		{Duplicates, NeverRetry, false},
	}
	for _, c := range cases {
		e := Effects{Repeat: c.repeat, Retry: c.retry,
			Compensate: Irreversible, Ending: CloseUnknown}
		if _, err := e.Declare(); err != nil {
			t.Fatalf("%s/%s: %v", c.repeat, c.retry, err)
		}
		if got := e.MayResend(); got != c.safe {
			t.Errorf("%s with %s: resend safe = %v, want %v", c.repeat, c.retry, got, c.safe)
		}
	}
	// The combination the ledger will not call safe is still allowed to exist.
	risky := Effects{Repeat: Duplicates, Retry: MayRetry,
		Compensate: Irreversible, Ending: CloseUnknown}
	if _, err := risky.Declare(); err != nil {
		t.Fatalf("a harness was forbidden from declaring an awkward truth: %v", err)
	}
}

// Binding is both together. The covenant says what a verb means; the effects
// say what happens somewhere else when it is acted on. Neither answers the
// other's question, so neither alone binds a harness.
//
//	— T11.10, T10.7
func TestBindingTakesTheCovenantAndTheEffectsTogether(t *testing.T) {
	r := NewRegister()
	c := Covenant{Namespace: "post", Version: "1",
		Can: []string{"post.send"}, Unknown: Refuse}

	if err := r.Bind(c, Effects{}); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("a harness bound without declaring its effects: %v", err)
	}
	if err := r.Bind(Covenant{Namespace: "post"}, plainEffects()); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("a harness bound without a whole covenant: %v", err)
	}
	if len(r.All()) != 0 {
		t.Fatal("a refused binding was kept")
	}

	eff := Effects{Repeat: Duplicates, Retry: NeverRetry,
		Compensate: Irreversible, Ending: AskAPerson}
	if err := r.Bind(c, eff); err != nil {
		t.Fatal(err)
	}
	// And what it declared is readable afterwards, alongside what it means.
	bound := r.All()
	if len(bound) != 1 {
		t.Fatalf("%d harnesses bound", len(bound))
	}
	if bound[0].Effects != eff {
		t.Fatalf("the declarations came back as %+v", bound[0].Effects)
	}
	if bound[0].Namespace != "post" {
		t.Fatal("the covenant did not come back with it")
	}
}
