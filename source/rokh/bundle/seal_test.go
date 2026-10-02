package bundle_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"rokh/bundle"
	"rokh/covenant"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
	"rokh/seal"
)

// A bundle sealed to a named reader opens for that reader and for nobody else.
//
// The whole point of sealing at bundling time is that the courier between the
// two people carries something it cannot read. The seal is only worth having
// if the wrong key is actually refused, so that is what this checks — not that
// the right key works, which is the easy half.
//
//	— T13.1, T7.4
func TestASealedBundleOpensForOneReaderAndNoOther(t *testing.T) {
	l, priv, anchor := newLedger(t)
	id := write(t, l, priv, anchor, "home/journal", "for their eyes")

	navid, err := seal.NewReading()
	if err != nil {
		t.Fatal(err)
	}
	mahtab, err := seal.NewReading()
	if err != nil {
		t.Fatal(err)
	}

	sel := bundle.Selection{IDs: []frame.ID{id}}
	boxes, err := bundle.SealFor(l, sel, navid.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 1 {
		t.Fatalf("%d boxes for one event", len(boxes))
	}

	// The named reader opens it, and what comes out is the event itself.
	got, err := seal.Open(navid, boxes[0], id[:])
	if err != nil {
		t.Fatalf("the named reader could not open it: %v", err)
	}
	// RKH3: the id is the hash of the signed head, and what opens is the
	// whole event, head and body.
	if s, err := event.Parse(got); err != nil || s.ID != id {
		t.Fatalf("what opened is not the event that was sealed: %v", err)
	}

	// Anybody else is refused, including somebody with a perfectly good
	// reading key of their own.
	if _, err := seal.Open(mahtab, boxes[0], id[:]); err == nil {
		t.Fatal("a different reading key opened it")
	}

	// And it cannot be lifted out and offered as a different event: the
	// event's own name is bound in as context.
	other := frame.Hash([]byte("some other event"))
	if _, err := seal.Open(navid, boxes[0], other[:]); err == nil {
		t.Fatal("a sealed event was opened under another event's name")
	}

	// The sealed bytes carry neither the payload nor the event's own name.
	if strings.Contains(string(boxes[0].Box), "for their eyes") {
		t.Fatal("the payload is readable in the sealed box")
	}
}

// Sealing takes a reading key, and refuses rather than quietly falling back to
// the clear when a covenant names none.
//
// The refusal matters more than it looks. A bundler that silently sent
// plaintext when it could not seal would give the sender every reason to
// believe their bundle was sealed when it was not, which is worse than never
// having offered sealing at all.
//
//	— T13.1
func TestSealingRefusesRatherThanFallingBackToTheClear(t *testing.T) {
	l, priv, anchor := newLedger(t)
	id := write(t, l, priv, anchor, "home/journal", "something")
	sel := bundle.Selection{IDs: []frame.ID{id}}

	if _, err := bundle.SealFor(l, sel, nil); err == nil {
		t.Fatal("sealing to no key succeeded")
	}
	if _, err := bundle.SealFor(l, sel, []byte("not a key")); err == nil {
		t.Fatal("sealing to a key of the wrong size succeeded")
	}
	// And an event the ledger does not hold cannot be sealed at all.
	absent := bundle.Selection{IDs: []frame.ID{frame.Hash([]byte("absent"))}}
	r, _ := seal.NewReading()
	if _, err := bundle.SealFor(l, absent, r.PublicKey().Bytes()); err == nil {
		t.Fatal("an event that is not held was sealed")
	}
}

// Whether a bundle is sealed is the covenant's decision, read off the covenant
// itself rather than passed in beside it.
//
//	— T13.1, T7.3
func TestTheCovenantDecidesWhetherThereIsASeal(t *testing.T) {
	r, err := seal.NewReading()
	if err != nil {
		t.Fatal(err)
	}
	key := r.PublicKey().Bytes()

	plain := covenant.Covenant{Scope: "home/journal"}
	withSeal := covenant.Covenant{Scope: "home/journal", SealTo: key}

	if _, ok := bundle.ReadingKeyFor([]covenant.Covenant{plain}, "home/journal"); ok {
		t.Fatal("a covenant naming no reading key was read as naming one")
	}
	got, ok := bundle.ReadingKeyFor([]covenant.Covenant{withSeal}, "home/journal")
	if !ok {
		t.Fatal("a covenant naming a reading key was not read as naming one")
	}
	if string(got) != string(key) {
		t.Fatal("the key that came back is not the one in the covenant")
	}
	// A covenant that does not cover the address does not lend its key to it.
	if _, ok := bundle.ReadingKeyFor([]covenant.Covenant{withSeal}, "home/private"); ok {
		t.Fatal("a covenant lent its reading key to an address it does not cover")
	}
	// A key of the wrong size is not a reading key, and is not treated as one.
	short := covenant.Covenant{Scope: "home/journal", SealTo: []byte("short")}
	if _, ok := bundle.ReadingKeyFor([]covenant.Covenant{short}, "home/journal"); ok {
		t.Fatal("a key of the wrong size was accepted as a reading key")
	}
}

// ---- helpers ----

func newLedger(t *testing.T) (*ledger.Ledger, ed25519.PrivateKey, frame.ID) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.Sign(event.Event{Address: event.AddressRoot,
		Verb: event.VerbGenesis, Payload: []byte("genesis")}, priv)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.New(gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return l, priv, gen.ID
}

func write(t *testing.T, l *ledger.Ledger, priv ed25519.PrivateKey,
	anchor frame.ID, addr, say string) frame.ID {
	t.Helper()
	a := anchor
	s, err := event.SignFresh(event.Event{Carrier: &a, Parents: l.Heads(),
		Address: addr, Verb: "note", Payload: []byte(say)}, priv)
	if err != nil {
		t.Fatal(err)
	}
	st, err := l.Add(s.Raw)
	if err != nil || st != ledger.Accepted {
		t.Fatalf("%v (%v)", err, st)
	}
	return s.ID
}
