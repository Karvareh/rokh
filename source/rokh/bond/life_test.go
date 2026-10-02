package bond

import (
	"errors"
	"testing"
)

// The cycle is eight stages, and a bond that cannot say what happens at each of
// them has not been thought through. Silence about incapacity is not a decision
// that incapacity will not happen; it is no decision, and the difference shows
// up exactly once.
//
//	— T11.8
func TestTheCycleIsEightStagesAndSilenceAboutOneIsNotADecision(t *testing.T) {
	if n := len(Stages()); n != 8 {
		t.Fatalf("the ruling names eight stages; the code has %d", n)
	}
	seen := map[Stage]bool{}
	for _, s := range Stages() {
		if seen[s] {
			t.Fatalf("%q is listed twice", s)
		}
		seen[s] = true
	}
	for _, want := range []Stage{Entry, Birth, Capable, Separated, Incapable,
		Death, Succeeded, Closed} {
		if !seen[want] {
			t.Fatalf("%q is not in the cycle", want)
		}
	}

	if missing, whole := Accounts(Stages()); !whole || len(missing) != 0 {
		t.Fatalf("an arrangement covering all eight read as incomplete: %v", missing)
	}
	// One left out is named, rather than passed over.
	partial := []Stage{Entry, Birth, Capable, Separated, Death, Succeeded, Closed}
	missing, whole := Accounts(partial)
	if whole {
		t.Fatal("an arrangement silent about incapacity read as whole")
	}
	if len(missing) != 1 || missing[0] != Incapable {
		t.Fatalf("what was missing read as %v", missing)
	}
	if _, whole := Accounts(nil); whole {
		t.Fatal("an arrangement covering nothing read as whole")
	}
}

// At every stage of the cycle, whoever acts for someone does so under a
// keeping over named work, bounded by an event, and never over the person or
// their ledger. That is as true at birth and at incapacity as anywhere else,
// which is precisely where it matters — those are the stages where a person
// cannot object.
//
//	— T11.8, T11.9
func TestAtEveryStageAKeepingIsOverWorkAndNeverOverThePerson(t *testing.T) {
	child, keeper := anchor("the child"), anchor("the keeper")

	for _, s := range Stages() {
		good := Passage{Who: child, From: s, To: s, Cover: &Keeping{
			Holder: keeper, Beneficiary: child, Work: "the schooling",
			Until: Bound{EndOfWork: true},
		}}
		if err := good.Check(); err != nil {
			t.Errorf("%s: a bounded keeping was refused: %v", s, err)
		}

		// Over the person rather than named work.
		owns := good
		owns.Cover = &Keeping{Holder: keeper, Beneficiary: child,
			Until: Bound{EndOfWork: true}}
		if err := owns.Check(); !errors.Is(err, ErrOwnsAPerson) {
			t.Errorf("%s: a keeping over the person passed: %v", s, err)
		}
		// Bounded by nothing.
		forever := good
		forever.Cover = &Keeping{Holder: keeper, Beneficiary: child,
			Work: "the schooling"}
		if err := forever.Check(); !errors.Is(err, ErrOwnsAPerson) {
			t.Errorf("%s: an unbounded keeping passed: %v", s, err)
		}
		// And a keeping for somebody else is not cover for this person.
		wrong := good
		wrong.Cover = &Keeping{Holder: keeper, Beneficiary: anchor("someone else"),
			Work: "the schooling", Until: Bound{EndOfWork: true}}
		if err := wrong.Check(); err == nil {
			t.Errorf("%s: somebody else's keeping covered this passage", s)
		}
	}

	// Nobody acting for them is a real answer, not a missing one.
	alone := Passage{Who: child, From: Capable, To: Separated}
	if err := alone.Check(); err != nil {
		t.Fatalf("a passage nobody covers was refused: %v", err)
	}
	// A stage that is not one of the eight is not a stage.
	if err := (Passage{Who: child, From: "grown up", To: Closed}).Check(); !errors.Is(err, ErrNoSuchStage) {
		t.Fatalf("an invented stage passed: %v", err)
	}
}

// Leaving closes the future and does not clear an open debt. Neither does
// death. The account is closed by settling, which is something somebody does.
//
//	— T11.7, T11.8
func TestAnOpenDebtIsNotClosedByReachingTheEnd(t *testing.T) {
	who := anchor("the person")

	owing := Passage{Who: who, From: Separated, To: Closed, Outstanding: true}
	if err := owing.Check(); err == nil {
		t.Fatal("an account with an open debt declared itself closed")
	}
	clear := owing
	clear.Outstanding = false
	if err := clear.Check(); err != nil {
		t.Fatalf("an account with nothing open could not be closed: %v", err)
	}

	// Passing through death with a debt open is not an error — it is the
	// ordinary case, and the debt is simply still there. What is refused is
	// only the claim that the account is closed.
	died := Passage{Who: who, From: Incapable, To: Death, Outstanding: true}
	if err := died.Check(); err != nil {
		t.Fatalf("dying with a debt open was refused: %v", err)
	}
	// Of the five acts, exactly one clears a debt, and leaving is not it.
	cleared := 0
	for _, a := range Acts() {
		if a.ClearsDebt() {
			cleared++
			if a != Settle {
				t.Fatalf("%q was said to clear a debt", a)
			}
		}
	}
	if cleared != 1 {
		t.Fatalf("%d of the five acts clear a debt", cleared)
	}
}

// Which stage may follow which, and who holds authority across the change, is
// an open ruling. So nothing here answers it — not for the obvious passages
// either, because answering the obvious ones would have decided the shape of
// the answer for the rest.
//
//	— T13.6, T11.8
func TestTheTransitionsAreLeftOpenIncludingTheObviousOnes(t *testing.T) {
	who := anchor("the person")
	n := 0
	for _, from := range Stages() {
		for _, to := range Stages() {
			p := Passage{Who: who, From: from, To: to}
			if err := p.Permitted(); !errors.Is(err, ErrCycleOpen) {
				t.Fatalf("%s → %s was answered: %v", from, to, err)
			}
			n++
		}
	}
	if n != 64 {
		t.Fatalf("%d passages were tried, expected 64", n)
	}
	// Including the ones anybody would call obvious.
	for _, obvious := range [][2]Stage{
		{Birth, Capable}, {Capable, Separated}, {Death, Succeeded},
		{Succeeded, Closed}, {Entry, Entry},
	} {
		p := Passage{Who: who, From: obvious[0], To: obvious[1]}
		if err := p.Permitted(); !errors.Is(err, ErrCycleOpen) {
			t.Fatalf("%s → %s slipped through as obvious", obvious[0], obvious[1])
		}
	}
	// And what is open is named, rather than left as a silence in the code.
	if Open == "" {
		t.Fatal("the open part of the cycle is not written down")
	}
}
