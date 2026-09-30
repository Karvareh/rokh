package bond

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"rokh/frame"
)

func anchor(s string) frame.ID { return frame.Hash([]byte(s)) }

// A bond is not several ledgers placed side by side; it is a knot between
// independent owners. One person alone cannot make one, because a two-sided
// relation is not something anyone originates by themselves.
//
//	— T11, T11.5
func TestOnePersonAloneCannotTieAKnot(t *testing.T) {
	alone := Leaf{Kind: Founding, Founders: []frame.ID{anchor("me")}, Doing: "keep the archive"}
	if _, err := alone.Encode(); !errors.Is(err, ErrAlone) {
		t.Fatalf("one owner founded a bond: %v", err)
	}
	none := Leaf{Kind: Founding, Doing: "keep the archive"}
	if _, err := none.Encode(); !errors.Is(err, ErrAlone) {
		t.Fatalf("nobody founded a bond: %v", err)
	}
	two := Leaf{Kind: Founding, Founders: []frame.ID{anchor("me"), anchor("you")},
		Doing: "keep the archive"}
	if _, err := two.Encode(); err != nil {
		t.Fatalf("two owners could not found a bond: %v", err)
	}
}

// Each person brings their own anchor. One anchor standing for two people is
// the shape of a shared root key, and that is the thing a bond is not.
//
//	— T11.5, T11.8
func TestEachPersonBringsTheirOwnAnchor(t *testing.T) {
	same := anchor("us")
	l := Leaf{Kind: Founding, Founders: []frame.ID{same, same}, Doing: "hold it together"}
	if _, err := l.Encode(); !errors.Is(err, ErrSharedRoot) {
		t.Fatalf("one anchor stood for two people: %v", err)
	}
}

// The founding leaf is a unique byte string, and its name is the hash of
// exactly those bytes. The same knot between the same people therefore has
// the same name no matter which of them writes it down.
//
//	— T11.6, T3.2
func TestTheLeafsNameIsTheHashOfItsOwnBytes(t *testing.T) {
	mine := Leaf{Kind: Founding, Founders: []frame.ID{anchor("a"), anchor("b")}, Doing: "the work"}
	yours := Leaf{Kind: Founding, Founders: []frame.ID{anchor("b"), anchor("a")}, Doing: "the work"}
	mb, err := mine.Encode()
	if err != nil {
		t.Fatal(err)
	}
	yb, err := yours.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if Name(mb) != Name(yb) {
		t.Fatal("the same knot written by two people got two names")
	}
	other := Leaf{Kind: Founding, Founders: []frame.ID{anchor("a"), anchor("b")}, Doing: "some other work"}
	ob, _ := other.Encode()
	if Name(ob) == Name(mb) {
		t.Fatal("two different undertakings share one name")
	}
	// And a second spelling of the same content is refused, not repaired.
	if _, err := Parse(append(mb, ' ')); err == nil {
		t.Fatal("a non-canonical leaf was accepted")
	}
	if _, err := Parse(mb); err != nil {
		t.Fatalf("the canonical leaf was refused: %v", err)
	}
}

// Each shared work has its own leaf and its own name, apart from the bond's.
// A work leaf names the bond it belongs to; a founding leaf belongs to none.
//
//	— T11.7
func TestEachSharedWorkHasItsOwnName(t *testing.T) {
	founders := []frame.ID{anchor("a"), anchor("b")}
	fb, _ := Leaf{Kind: Founding, Founders: founders, Doing: "the bond"}.Encode()
	bondName := Name(fb)

	w1, err := Leaf{Kind: Work, Founders: founders, Doing: "copy the archive", Of: &bondName}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	w2, err := Leaf{Kind: Work, Founders: founders, Doing: "index the archive", Of: &bondName}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	names := map[frame.ID]bool{bondName: true, Name(w1): true, Name(w2): true}
	if len(names) != 3 {
		t.Fatal("the bond and its two works do not have three separate names")
	}
	if _, err := (Leaf{Kind: Work, Founders: founders, Doing: "orphan"}).Encode(); err == nil {
		t.Fatal("a work leaf was accepted without naming its bond")
	}
	if _, err := (Leaf{Kind: Founding, Founders: founders, Doing: "x", Of: &bondName}).Encode(); err == nil {
		t.Fatal("a founding leaf was accepted as belonging to another")
	}
}

// A work is closed when everyone who deemed the leaf necessary has referred to
// that same name in their own ledger. That proves mutual acceptance and
// nothing more — and the ledgers never become one. There is no function in
// this package that merges two of them.
//
//	— T11.6, T11.7
func TestClosingIsMutualAcceptanceAndTheLedgersStaySeparate(t *testing.T) {
	a, b, c := anchor("a"), anchor("b"), anchor("c")
	l := Leaf{Kind: Founding, Founders: []frame.ID{a, b, c}, Doing: "the work"}

	if missing, closed := Standing(l, []frame.ID{a}); closed || len(missing) != 2 {
		t.Fatalf("one acceptance closed a three-way leaf: %v %v", missing, closed)
	}
	if missing, closed := Standing(l, []frame.ID{a, b}); closed || len(missing) != 1 || missing[0] != c {
		t.Fatalf("the missing party was not named: %v %v", missing, closed)
	}
	if _, closed := Standing(l, []frame.ID{a, b, c}); !closed {
		t.Fatal("everyone accepted and the leaf did not close")
	}
	// A stranger's acceptance is not one of theirs.
	if _, closed := Standing(l, []frame.ID{a, b, anchor("passer-by")}); closed {
		t.Fatal("a stranger closed a leaf on someone else's behalf")
	}

	// No merge exists, and none may appear.
	for _, m := range reflect.VisibleFields(reflect.TypeOf(Leaf{})) {
		if strings.Contains(strings.ToLower(m.Name), "merge") {
			t.Fatalf("a leaf gained a %q field; the ledgers never become one", m.Name)
		}
	}
}

// Joining, leaving, amending the leaf, returning the item and settling the
// account are five separate acts. Leaving closes the future; it does not clear
// an open debt, and that is why settling is its own act.
//
//	— T11.7
func TestTheFiveActsAreFiveAndLeavingIsNotSettling(t *testing.T) {
	acts := Acts()
	if len(acts) != 5 {
		t.Fatalf("there are %d acts, and the specification names five", len(acts))
	}
	seen := map[Act]bool{}
	for _, a := range acts {
		if seen[a] {
			t.Fatalf("%q appears twice", a)
		}
		seen[a] = true
	}
	if Leave.ClearsDebt() {
		t.Fatal("leaving cleared an open debt")
	}
	if !Settle.ClearsDebt() {
		t.Fatal("settling did not clear the account")
	}
	for _, a := range []Act{Join, Amend, Return} {
		if a.ClearsDebt() {
			t.Fatalf("%q cleared an open debt", a)
		}
	}
}

// Nobody comes to own another person. A keeper holds authority over named
// work, bounded by an event — never over the person or their ledger, and
// never by the hour.
//
//	— T11.9, T6.5, T5.3
func TestNobodyComesToOwnAnotherPerson(t *testing.T) {
	child, parent := anchor("the child"), anchor("the parent")

	unbounded := Keeping{Holder: parent, Beneficiary: child, Work: "school fees"}
	if err := unbounded.Check(); !errors.Is(err, ErrOwnsAPerson) {
		t.Fatalf("a keeping with no named end was accepted: %v", err)
	}
	nameless := Keeping{Holder: parent, Beneficiary: child, Until: Bound{EndOfWork: true}}
	if err := nameless.Check(); !errors.Is(err, ErrOwnsAPerson) {
		t.Fatalf("a keeping over no named work was accepted: %v", err)
	}
	// Each of the three bounds is an event, and each is enough on its own.
	terminal := anchor("the day the work is handed over")
	for name, b := range map[string]Bound{
		"end of work": {EndOfWork: true},
		"taken back":  {TakenBack: true},
		"terminal":    {Terminal: &terminal},
	} {
		k := Keeping{Holder: parent, Beneficiary: child, Work: "school fees", Until: b}
		if err := k.Check(); err != nil {
			t.Errorf("%s did not bound a keeping: %v", name, err)
		}
	}
	// And there is no field here that could hold a time.
	for _, f := range reflect.VisibleFields(reflect.TypeOf(Bound{})) {
		n := strings.ToLower(f.Name)
		if strings.Contains(n, "time") || strings.Contains(n, "deadline") ||
			strings.Contains(n, "expire") || f.Type.String() == "time.Time" {
			t.Fatalf("Bound gained %q; testimony about the hour does not close authority", f.Name)
		}
	}
}

// A keeper does not become the owner of what is kept, and the bond does not
// break when the keeper changes. That is exactly what carries it from one
// generation to the next.
//
//	— T11.9
func TestAKeeperDoesNotBecomeTheOwnerAndTheBondSurvivesTheHandover(t *testing.T) {
	child, first, second := anchor("the child"), anchor("first keeper"), anchor("second keeper")
	k := Keeping{Holder: first, Beneficiary: child, Work: "the inheritance",
		Share: 1000, Until: Bound{EndOfWork: true}}
	if err := k.Check(); err != nil {
		t.Fatal(err)
	}

	next, err := k.Handover(second, Bound{EndOfWork: true})
	if err != nil {
		t.Fatalf("the keeping could not be handed on: %v", err)
	}
	if next.Beneficiary != child {
		t.Fatal("the handover moved whose it is")
	}
	if next.Share != k.Share {
		t.Fatal("the share changed hands with the keeper")
	}
	if next.Work != k.Work {
		t.Fatal("the named work changed with the keeper")
	}
	if next.Holder != second {
		t.Fatal("the keeper did not change")
	}
	// A keeper cannot hand the keeping to the beneficiary as if it were a gift
	// of ownership through the back door.
	if _, err := k.Handover(child, Bound{EndOfWork: true}); err == nil {
		t.Fatal("a keeper handed the keeping to the person it is kept for, as a transfer of ownership")
	}
}
