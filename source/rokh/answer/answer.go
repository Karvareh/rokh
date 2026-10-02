// Package answer is what a harness owes when it answers.
//
// Every answer and every act stands on Rokh's three pillars. This package is
// where the three stop being a description and become a check something can
// fail:
//
//   - Sound memory — what is recorded can be cited, and a quotation without
//     an address is not accepted.
//
//   - Source of authority — every act traces back to a named, delegated
//     authority and leaves a receipt. An act with neither had no guard.
//
//   - Personal ground — the answer is built on this person's ground, not on
//     the average of the world.
//
//     — T2, T2.1, T2.2, T2.3
package answer

import (
	"errors"
	"fmt"
	"strings"

	"rokh/frame"
)

var (
	// ErrUncited is returned for something asserted with no address behind
	// it. A quotation without an address is not accepted — not because it is
	// false, but because it cannot be checked.
	//
	//	— T2.1
	ErrUncited = errors.New("answer: a quotation with no address is not accepted")
	// ErrNotInThisLedger is returned for a citation this ledger does not
	// hold. Citing outward is citing nothing.
	//
	//	— T2.1, T2.3
	ErrNotInThisLedger = errors.New("answer: cited something this ledger does not hold")
	// ErrGuardAbsent is returned for an act with no named authority or no
	// receipt. The wording is the specification's: if an act was done without
	// either, the guard was absent.
	//
	//	— T2.2
	ErrGuardAbsent = errors.New("answer: the guard was absent")
	// ErrNotThisPerson is returned for an answer built on someone else's
	// ground, or on none.
	//
	//	— T2.3
	ErrNotThisPerson = errors.New("answer: built on other ground than this person's")
)

// Claim is one thing an answer asserts, together with where it came from.
type Claim struct {
	Saying string
	// From names the events the claim rests on. Empty is not allowed: it is
	// the shape of a quotation with no address.
	//
	//	— T2.1
	From []frame.ID
}

// Act is anything the answer did rather than said.
type Act struct {
	Doing string
	// Authority is the named, delegated grant it was done under.
	Authority frame.ID
	// Receipt is the intent it opened. Both are required; either one missing
	// means there was no guard.
	//
	//	— T2.2
	Receipt frame.ID
}

// Answer is what a harness hands back, built on one person's ledger.
type Answer struct {
	// Anchor is whose ledger this was built on. An answer is for a person,
	// not for the average of the world.
	//
	//	— T2.3
	Anchor frame.ID
	Claims []Claim
	Acts   []Act
}

// Ground is the ledger an answer is checked against.
//
// It is an interface so this package needs no ledger of its own — the pillars
// are a shape, and the shape is checkable wherever the events actually live.
// But it is an interface with four questions, not one predicate, because the
// three pillars ask four different things and a single "do you have this id"
// cannot answer them. Whether a ledger holds an id says nothing about whether
// that id is a grant, and an act that names an ordinary event as its authority
// has named nothing.
//
//	— T2.1, T2.2, T2.3
type Ground interface {
	// Anchor is whose ledger this is.
	Anchor() frame.ID
	// Holds reports whether an event is *accepted* here. Present is not
	// enough: a pending event has incomplete ancestry, and a rejected one
	// never was.
	//   — T2.1, T3.3
	Holds(frame.ID) bool
	// Grants reports whether the id is a grant of authority still standing in
	// the causal past — granted and not revoked.
	//   — T2.2
	Grants(frame.ID) bool
	// Opened reports whether the id is a receipt's intent recorded here.
	//   — T2.2
	Opened(frame.ID) bool
}

// ErrNoGround is returned when there is no ledger to check against.
//
// It is not a technicality. The pillars are all three about a particular
// ledger — what it holds, what it granted, whose it is — so an answer with no
// ledger behind it has not passed the check leniently; it has not been checked.
// The old shape of this package allowed a nil predicate and then skipped the
// membership tests, which meant the easiest way to satisfy the pillars was to
// bring nothing to satisfy them against.
//
//	— T2.1
var ErrNoGround = errors.New("answer: there is no ledger to check this against")

// Check reports every way this answer falls short of the three pillars. It
// returns all of them rather than the first, because a harness ought to be
// told everything it owes at once.
//
//	— T2, T2.1, T2.2, T2.3
func (a Answer) Check(g Ground) []error {
	if g == nil {
		return []error{ErrNoGround}
	}
	anchor := g.Anchor()
	var errs []error

	// Personal ground.
	if a.Anchor.IsZero() {
		errs = append(errs, fmt.Errorf("%w: no ground named", ErrNotThisPerson))
	} else if a.Anchor != anchor {
		errs = append(errs, fmt.Errorf("%w: built on %s, asked of %s",
			ErrNotThisPerson, a.Anchor.Short(), anchor.Short()))
	}

	// Sound memory.
	for i, c := range a.Claims {
		if strings.TrimSpace(c.Saying) == "" {
			continue
		}
		if len(c.From) == 0 {
			errs = append(errs, fmt.Errorf("%w: claim %d, %q", ErrUncited, i, short(c.Saying)))
			continue
		}
		for _, id := range c.From {
			if id.IsZero() {
				errs = append(errs, fmt.Errorf("%w: claim %d cites nothing", ErrUncited, i))
				continue
			}
			if !g.Holds(id) {
				errs = append(errs, fmt.Errorf("%w: claim %d cites %s",
					ErrNotInThisLedger, i, id.Short()))
			}
		}
	}

	// Source of authority. Naming an id is not the same as naming an
	// authority: the id has to be a grant that still stands, and the receipt
	// has to be an intent that was really opened.
	for i, act := range a.Acts {
		switch {
		case act.Authority.IsZero() && act.Receipt.IsZero():
			errs = append(errs, fmt.Errorf("%w: act %d, %q, had neither authority nor receipt",
				ErrGuardAbsent, i, short(act.Doing)))
			continue
		case act.Authority.IsZero():
			errs = append(errs, fmt.Errorf("%w: act %d, %q, names no authority",
				ErrGuardAbsent, i, short(act.Doing)))
			continue
		case act.Receipt.IsZero():
			errs = append(errs, fmt.Errorf("%w: act %d, %q, left no receipt",
				ErrGuardAbsent, i, short(act.Doing)))
			continue
		}
		if !g.Grants(act.Authority) {
			errs = append(errs, fmt.Errorf("%w: act %d rests on %s, which is not a "+
				"grant standing in this ledger", ErrGuardAbsent, i, act.Authority.Short()))
		}
		if !g.Opened(act.Receipt) {
			errs = append(errs, fmt.Errorf("%w: act %d shows %s as its receipt, which "+
				"is not an intent opened here", ErrGuardAbsent, i, act.Receipt.Short()))
		}
	}
	return errs
}

// Sound is Check reduced to a yes or no, for callers that only need the gate.
func (a Answer) Sound(g Ground) bool { return len(a.Check(g)) == 0 }

func short(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}
