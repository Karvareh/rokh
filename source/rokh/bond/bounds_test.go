package bond

import (
	"errors"
	"testing"

	"rokh/frame"
)

// Reading, writing, acting in someone's name and keeping are four authorities,
// and none of them follows from another.
//
// The list is the easy half. The hard half is that holding one must not quietly
// confer a second — reading from writing, keeping from acting — because that is
// how somebody given the smallest thing ends up with all four. So every ordered
// pair is checked: twelve of them, and each one must fail.
//
//	— T11.8
func TestNoneOfTheFourAuthoritiesFollowsFromAnother(t *testing.T) {
	all := Powers()
	if len(all) != 4 {
		t.Fatalf("the ruling names four authorities; the code has %d", len(all))
	}
	for _, have := range all {
		h, err := Grant(have)
		if err != nil {
			t.Fatal(err)
		}
		if !h.Can(have) {
			t.Fatalf("%s was granted and is not held", have)
		}
		for _, other := range all {
			if other == have {
				continue
			}
			if h.Can(other) {
				t.Errorf("holding %s conferred %s", have, other)
			}
			if err := h.Must(other); !errors.Is(err, ErrNotGranted) {
				t.Errorf("holding %s let %s pass: %v", have, other, err)
			}
		}
		if got := h.All(); len(got) != 1 || got[0] != have {
			t.Errorf("granting %s yielded %v", have, got)
		}
	}

	// The way to hold two is to have been given two, and nothing else.
	both, err := Grant(Reading, Holding)
	if err != nil {
		t.Fatal(err)
	}
	if !both.Can(Reading) || !both.Can(Holding) {
		t.Fatal("two granted, not both held")
	}
	if both.Can(Writing) || both.Can(Acting) {
		t.Fatal("granting two conferred a third")
	}

	// Authority narrows and never widens: there is no Widen, and Narrow only
	// ever takes away.
	less := both.Narrow(Holding)
	if less.Can(Holding) {
		t.Fatal("narrowing did not narrow")
	}
	if !less.Can(Reading) {
		t.Fatal("narrowing took something it was not asked to take")
	}
	if len(less.Narrow(Reading).All()) != 0 {
		t.Fatal("narrowing to nothing left something")
	}
	// And the original is untouched: narrowing produces a new holding rather
	// than editing the one somebody was granted.
	if !both.Can(Holding) {
		t.Fatal("narrowing edited the holding it was derived from")
	}

	if _, err := Grant("everything"); !errors.Is(err, ErrNoSuchPower) {
		t.Fatalf("an invented authority was granted: %v", err)
	}
}

// A shared pen is the sharpest case of the two prohibitions, because a pen is
// exactly what a keeper would want to keep. Every holder's share is named, the
// keeper holds a share themselves, the keeping ends at an event rather than an
// hour, and holding it confers the power to write and nothing else.
//
//	— T11.8, T11.9
func TestTheSharedPenIsKeptWithAShareAndANamedEnd(t *testing.T) {
	a, b, c := anchor("a"), anchor("b"), anchor("c")
	knot := anchor("the bond")
	done := Bound{EndOfWork: true}

	good := Pen{Of: knot, Keeper: a, Shares: map[frame.ID]uint64{a: 1, b: 2}, Until: done}
	if err := good.Check(); err != nil {
		t.Fatalf("a properly kept pen was refused: %v", err)
	}
	if h := good.Holders(); len(h) != 2 {
		t.Fatalf("the holders read as %v", h)
	}

	for name, bad := range map[string]Pen{
		"no bond":        {Keeper: a, Shares: map[frame.ID]uint64{a: 1, b: 1}, Until: done},
		"nobody holds":   {Of: knot, Shares: map[frame.ID]uint64{a: 1, b: 1}, Until: done},
		"shared by one":  {Of: knot, Keeper: a, Shares: map[frame.ID]uint64{a: 1}, Until: done},
		"share of none":  {Of: knot, Keeper: a, Shares: map[frame.ID]uint64{a: 1, b: 0}, Until: done},
		"no named end":   {Of: knot, Keeper: a, Shares: map[frame.ID]uint64{a: 1, b: 1}},
		"keeper outside": {Of: knot, Keeper: c, Shares: map[frame.ID]uint64{a: 1, b: 1}, Until: done},
	} {
		if err := bad.Check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// Holding the pen is the power to write, and nothing else comes with it.
	w := good.Writes()
	if !w.Can(Writing) {
		t.Fatal("the pen does not confer writing")
	}
	for _, p := range []Power{Reading, Acting, Holding} {
		if w.Can(p) {
			t.Fatalf("holding the pen conferred %s", p)
		}
	}
}

// Handing the pen back is an act with a receipt, not a silence somebody later
// calls a return. And the shares do not move with the keeping: whoever held a
// share before holds the same share after.
//
//	— T11.8, T11.7, T11.9
func TestReturningThePenTakesAReceiptAndTheSharesDoNotMove(t *testing.T) {
	a, b := anchor("a"), anchor("b")
	knot := anchor("the bond")
	p := Pen{Of: knot, Keeper: a, Shares: map[frame.ID]uint64{a: 1, b: 3},
		Until: Bound{EndOfWork: true}}

	// No receipt, no return.
	if _, err := p.Return(Handback{Pen: knot, To: b}, Bound{TakenBack: true}); !errors.Is(err, ErrUnreturned) {
		t.Fatalf("the pen changed hands on nobody's word: %v", err)
	}
	// Handing it to whoever already holds it is not a return either.
	if _, err := p.Return(Handback{Pen: knot, To: a, Receipt: anchor("r")},
		Bound{TakenBack: true}); err == nil {
		t.Fatal("the keeper handed the pen to themselves")
	}

	next, err := p.Return(Handback{Pen: knot, To: b, Receipt: anchor("the receipt")},
		Bound{TakenBack: true})
	if err != nil {
		t.Fatal(err)
	}
	if next.Keeper != b {
		t.Fatal("the pen did not change hands")
	}
	for who, share := range p.Shares {
		if next.Shares[who] != share {
			t.Fatalf("%s's share changed with the keeping: %d became %d",
				who.Short(), share, next.Shares[who])
		}
	}
	// The keeper became a holder-of, not an owner-of: a still has their share.
	if next.Shares[a] != 1 {
		t.Fatal("the old keeper lost their share by handing the pen on")
	}
	// And the pen it was returned from is untouched.
	if p.Keeper != a {
		t.Fatal("returning edited the pen it came from")
	}
}
