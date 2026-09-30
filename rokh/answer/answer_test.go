package answer

import (
	"errors"
	"testing"

	"rokh/frame"
)

func id(s string) frame.ID { return frame.Hash([]byte(s)) }

// fake is a ledger stated as three sets, so a test can say exactly what a
// ledger holds, what it granted, and which intents were opened on it. The
// three are separate on purpose: holding an id says nothing about whether it
// is a grant, and that gap is what the second pillar is about.
type fake struct {
	anchor frame.ID
	held   map[frame.ID]bool
	grants map[frame.ID]bool
	opened map[frame.ID]bool
}

func (f fake) Anchor() frame.ID       { return f.anchor }
func (f fake) Holds(i frame.ID) bool  { return f.held[i] }
func (f fake) Grants(i frame.ID) bool { return f.grants[i] }
func (f fake) Opened(i frame.ID) bool { return f.opened[i] }

func set(ids ...frame.ID) map[frame.ID]bool {
	m := map[frame.ID]bool{}
	for _, i := range ids {
		m[i] = true
	}
	return m
}

// ledgerOf is a ground that holds these ids, granted whichever of them an act
// names as an authority, and opened whichever it names as a receipt. It is the
// permissive shape, for tests that are about something other than the second
// pillar.
func ledgerOf(anchor frame.ID, ids ...frame.ID) fake {
	m := set(ids...)
	return fake{anchor: anchor, held: m, grants: m, opened: m}
}

func has(errs []error, target error) bool {
	for _, e := range errs {
		if errors.Is(e, target) {
			return true
		}
	}
	return false
}

// Sound memory: what is recorded can be cited, and a quotation with no address
// is not accepted. Not because it is false — because it cannot be checked.
//
//	— T2.1
func TestAQuotationWithNoAddressIsNotAccepted(t *testing.T) {
	me := id("my anchor")
	e := id("the event I am quoting")
	known := ledgerOf(me, e)

	good := Answer{Anchor: me, Claims: []Claim{{Saying: "you wrote this on Tuesday", From: []frame.ID{e}}}}
	if errs := good.Check(known); len(errs) != 0 {
		t.Fatalf("a cited claim was refused: %v", errs)
	}

	bare := Answer{Anchor: me, Claims: []Claim{{Saying: "you wrote this on Tuesday"}}}
	if errs := bare.Check(known); !has(errs, ErrUncited) {
		t.Fatalf("a quotation with no address was accepted: %v", errs)
	}

	outward := Answer{Anchor: me, Claims: []Claim{
		{Saying: "somebody somewhere said so", From: []frame.ID{id("an event elsewhere")}},
	}}
	if errs := outward.Check(known); !has(errs, ErrNotInThisLedger) {
		t.Fatalf("a citation this ledger does not hold was accepted: %v", errs)
	}
}

// Source of authority: every act traces back to a named, delegated authority
// and leaves a receipt. An act done with neither had no guard, and the
// specification says so in those words.
//
//	— T2.2
func TestAnActWithoutAuthorityOrReceiptHadNoGuard(t *testing.T) {
	me := id("my anchor")
	grant := id("the grant")
	receipt := id("the receipt")
	known := ledgerOf(me, grant, receipt)

	good := Answer{Anchor: me, Acts: []Act{{Doing: "copied the archive", Authority: grant, Receipt: receipt}}}
	if errs := good.Check(known); len(errs) != 0 {
		t.Fatalf("a guarded act was refused: %v", errs)
	}

	for name, act := range map[string]Act{
		"no authority": {Doing: "copied the archive", Receipt: receipt},
		"no receipt":   {Doing: "copied the archive", Authority: grant},
		"neither":      {Doing: "copied the archive"},
	} {
		a := Answer{Anchor: me, Acts: []Act{act}}
		if errs := a.Check(known); !has(errs, ErrGuardAbsent) {
			t.Errorf("%s: the act was accepted anyway: %v", name, errs)
		}
	}

	// An authority this ledger does not hold is no authority.
	stranger := Answer{Anchor: me, Acts: []Act{
		{Doing: "copied the archive", Authority: id("someone else's grant"), Receipt: receipt},
	}}
	if errs := stranger.Check(known); !has(errs, ErrGuardAbsent) {
		t.Fatalf("an act rested on a grant from outside this ledger: %v", errs)
	}
}

// Personal ground: the answer is built on this person's ground, not on the
// average of the world and not on somebody else's ledger.
//
//	— T2.3
func TestAnAnswerIsBuiltOnThisPersonsGround(t *testing.T) {
	me, you := id("my anchor"), id("your anchor")
	e := id("an event of mine")
	known := ledgerOf(me, e)

	mine := Answer{Anchor: me, Claims: []Claim{{Saying: "so it stands", From: []frame.ID{e}}}}
	if errs := mine.Check(known); len(errs) != 0 {
		t.Fatalf("my own ground was refused: %v", errs)
	}
	theirs := Answer{Anchor: you, Claims: []Claim{{Saying: "so it stands", From: []frame.ID{e}}}}
	if errs := theirs.Check(known); !has(errs, ErrNotThisPerson) {
		t.Fatalf("an answer built on someone else's ground was accepted: %v", errs)
	}
	nowhere := Answer{Claims: []Claim{{Saying: "so it stands", From: []frame.ID{e}}}}
	if errs := nowhere.Check(known); !has(errs, ErrNotThisPerson) {
		t.Fatalf("an answer built on no ground at all was accepted: %v", errs)
	}
}

// Every answer and every act stands on all three. A harness is told everything
// it owes at once, not the first thing it got wrong.
//
//	— T2
func TestAllThreePillarsAreReportedTogether(t *testing.T) {
	me, you := id("my anchor"), id("your anchor")
	known := ledgerOf(me)

	bad := Answer{
		Anchor: you,
		Claims: []Claim{{Saying: "no address behind this"}},
		Acts:   []Act{{Doing: "did a thing"}},
	}
	errs := bad.Check(known)
	for _, want := range []error{ErrNotThisPerson, ErrUncited, ErrGuardAbsent} {
		if !has(errs, want) {
			t.Errorf("the check stayed silent about %v", want)
		}
	}
	if bad.Sound(known) {
		t.Fatal("an answer failing all three was called sound")
	}

	e, grant, receipt := id("e"), id("g"), id("r")
	good := Answer{
		Anchor: me,
		Claims: []Claim{{Saying: "you wrote this", From: []frame.ID{e}}},
		Acts:   []Act{{Doing: "filed it", Authority: grant, Receipt: receipt}},
	}
	if !good.Sound(ledgerOf(me, e, grant, receipt)) {
		t.Fatalf("an answer standing on all three was refused: %v",
			good.Check(ledgerOf(me, e, grant, receipt)))
	}
}

// An answer with no ledger behind it is not an answer that passed the check.
//
// All three pillars are about a particular ledger: what it holds, what it
// granted, whose it is. The old shape of this package took a predicate that
// might be nil and then quietly skipped every membership test, which made the
// cheapest way to satisfy the pillars bringing nothing to satisfy them
// against. Now there is no such door.
//
//	— T2, T2.1
func TestAnAnswerWithNoLedgerBehindItIsNotChecked(t *testing.T) {
	me := id("my anchor")
	a := Answer{
		Anchor: me,
		Claims: []Claim{{Saying: "so it stands", From: []frame.ID{id("whatever")}}},
		Acts:   []Act{{Doing: "did it", Authority: id("g"), Receipt: id("r")}},
	}
	errs := a.Check(nil)
	if !has(errs, ErrNoGround) {
		t.Fatalf("an answer with no ledger was checked anyway: %v", errs)
	}
	if a.Sound(nil) {
		t.Fatal("an answer with no ledger was called sound")
	}
	// And the same answer against a ledger that does not hold what it cites
	// still fails, so the previous line is not passing for want of content.
	empty := fake{anchor: me, held: set(), grants: set(), opened: set()}
	if a.Sound(empty) {
		t.Fatal("an answer citing what the ledger lacks was called sound")
	}
}

// Naming an id is not naming an authority.
//
// The second pillar says an act traces back to an authority that is *named and
// delegated*, and leaves a receipt. An id the ledger merely holds satisfies
// neither: an ordinary event pointed at as "the grant" is a guard in name
// only, which is precisely the absent guard the ruling is about.
//
//	— T2.2
func TestAnOrdinaryEventNamedAsAuthorityIsNotOne(t *testing.T) {
	me := id("my anchor")
	grant, intent, ordinary := id("a real grant"), id("a real intent"), id("just an event")

	held := set(grant, intent, ordinary)
	g := fake{anchor: me, held: held, grants: set(grant), opened: set(intent)}

	good := Answer{Anchor: me, Acts: []Act{
		{Doing: "filed it", Authority: grant, Receipt: intent}}}
	if errs := good.Check(g); len(errs) != 0 {
		t.Fatalf("a properly guarded act was refused: %v", errs)
	}

	// Held, but not a grant.
	passedOff := Answer{Anchor: me, Acts: []Act{
		{Doing: "filed it", Authority: ordinary, Receipt: intent}}}
	if errs := passedOff.Check(g); !has(errs, ErrGuardAbsent) {
		t.Fatalf("an ordinary event was accepted as an authority: %v", errs)
	}

	// Held, but not an intent anyone opened.
	invented := Answer{Anchor: me, Acts: []Act{
		{Doing: "filed it", Authority: grant, Receipt: ordinary}}}
	if errs := invented.Check(g); !has(errs, ErrGuardAbsent) {
		t.Fatalf("an ordinary event was accepted as a receipt: %v", errs)
	}

	// Both wrong is reported as both, not as the first one found.
	neither := Answer{Anchor: me, Acts: []Act{
		{Doing: "filed it", Authority: ordinary, Receipt: ordinary}}}
	if n := len(neither.Check(g)); n != 2 {
		t.Fatalf("two absent guards were reported as %d complaints", n)
	}
}
