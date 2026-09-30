package bond_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"rokh/bond"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// person is one owner: their own key, their own genesis, their own ledger.
// Nothing is shared between two of them, which is the point.
type person struct {
	name string
	priv ed25519.PrivateKey
	gen  event.Signed
	l    *ledger.Ledger
	head frame.ID
}

func newPerson(t *testing.T, name string) *person {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.Sign(event.Event{Address: event.AddressRoot,
		Verb: event.VerbGenesis, Payload: []byte(name)}, priv)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.New(gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return &person{name: name, priv: priv, gen: gen, l: l, head: gen.ID}
}

func (p *person) anchor() frame.ID { return p.gen.ID }

// write records an event in this person's own ledger and nobody else's.
func (p *person) write(t *testing.T, addr, verb string, payload []byte) frame.ID {
	t.Helper()
	a := p.gen.ID
	s, err := event.SignFresh(event.Event{Carrier: &a, Parents: []frame.ID{p.head},
		Address: addr, Verb: verb, Payload: payload}, p.priv)
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.l.Add(s.Raw)
	if err != nil || st != ledger.Accepted {
		t.Fatalf("%s: %v (%v)", p.name, err, st)
	}
	p.head = s.ID
	return s.ID
}

// world is the several ledgers, read one at a time. There is deliberately no
// method here that combines two.
type world struct{ by map[frame.ID]*person }

func (w world) Accepted(anchor, leaf frame.ID) (bond.Acceptance, bool) {
	p, ok := w.by[anchor]
	if !ok {
		return bond.Acceptance{}, false
	}
	for _, id := range p.l.Order() {
		s, ok := p.l.Get(id)
		if !ok || s.Event.Verb != bond.VerbAccept {
			continue
		}
		a, err := bond.ReadAcceptance(s.Event.Payload)
		if err != nil || a.Leaf != leaf {
			continue
		}
		return a, true
	}
	return bond.Acceptance{}, false
}

// A bond spans several ledgers without joining them.
//
// One leaf, whose name everyone computes for themselves from its bytes. Then
// each person writes, in their own ledger, that they accept that name and their
// own place in it. Nothing goes into anyone else's ledger and there is no
// ledger of the bond. The work is closed when everyone has done so, separately.
//
//	— T13.6, T11.6, T11.7
func TestABondSpansSeveralLedgersWithoutJoiningThem(t *testing.T) {
	sara := newPerson(t, "sara")
	navid := newPerson(t, "navid")
	w := world{by: map[frame.ID]*person{
		sara.anchor(): sara, navid.anchor(): navid,
	}}

	leaf := bond.Leaf{Kind: bond.Founding, Doing: "keep the archive together",
		Founders: []frame.ID{sara.anchor(), navid.anchor()}}

	// Each computes the same name from the same bytes, without asking anyone.
	b, err := leaf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	name := bond.Name(b)

	// Nobody has accepted yet.
	missing, closed, err := bond.Settled(leaf, w)
	if err != nil {
		t.Fatal(err)
	}
	if closed || len(missing) != 2 {
		t.Fatalf("an unaccepted leaf read as closed=%v, missing %v", closed, missing)
	}

	// Sara accepts, in her own ledger.
	p1, n1, err := bond.Accept(leaf, sara.anchor(), "I hold the copies")
	if err != nil {
		t.Fatal(err)
	}
	if n1 != name {
		t.Fatal("two people computed different names for one leaf")
	}
	sara.write(t, "bonds/archive", bond.VerbAccept, p1)

	// One of two is not closed, and the one still owed is named.
	missing, closed, _ = bond.Settled(leaf, w)
	if closed {
		t.Fatal("one acceptance closed a leaf naming two people")
	}
	if len(missing) != 1 || missing[0] != navid.anchor() {
		t.Fatalf("what was owed read as %v", missing)
	}

	// Navid accepts, in his.
	p2, _, err := bond.Accept(leaf, navid.anchor(), "I keep the index")
	if err != nil {
		t.Fatal(err)
	}
	navid.write(t, "bonds/archive", bond.VerbAccept, p2)

	missing, closed, _ = bond.Settled(leaf, w)
	if !closed || len(missing) != 0 {
		t.Fatalf("both accepted and it did not close: %v", missing)
	}

	// And the ledgers did not become one. Neither holds a single event of the
	// other's, including the acceptance that closed the leaf.
	for _, pair := range []struct{ mine, theirs *person }{{sara, navid}, {navid, sara}} {
		for _, id := range pair.theirs.l.Order() {
			if pair.mine.l.Has(id) {
				t.Fatalf("%s's ledger holds an event of %s's", pair.mine.name, pair.theirs.name)
			}
		}
		if pair.mine.l.Genesis() == pair.theirs.l.Genesis() {
			t.Fatal("two people share an anchor")
		}
	}

	// What closing established is that each said their own thing, separately.
	places, err := bond.Places(leaf, w)
	if err != nil {
		t.Fatal(err)
	}
	if places[sara.anchor()] != "I hold the copies" ||
		places[navid.anchor()] != "I keep the index" {
		t.Fatalf("the places read as %v", places)
	}
}

// Accepting a leaf means accepting a place in it, and only someone the leaf
// names can accept it. A near-identical leaf is a different leaf, because the
// name is the hash of the bytes and there are no near misses in a hash.
//
//	— T11.6, T11.7, T3.2
func TestAcceptanceIsOfThisLeafAndOfAPlaceInIt(t *testing.T) {
	sara := newPerson(t, "sara")
	navid := newPerson(t, "navid")
	stranger := newPerson(t, "stranger")
	w := world{by: map[frame.ID]*person{
		sara.anchor(): sara, navid.anchor(): navid, stranger.anchor(): stranger,
	}}

	leaf := bond.Leaf{Kind: bond.Founding, Doing: "keep the archive together",
		Founders: []frame.ID{sara.anchor(), navid.anchor()}}

	if _, _, err := bond.Accept(leaf, sara.anchor(), "   "); !errors.Is(err, bond.ErrNoPlace) {
		t.Fatalf("a leaf was accepted with no place taken: %v", err)
	}
	if _, _, err := bond.Accept(leaf, stranger.anchor(), "me too"); !errors.Is(err, bond.ErrNotAFounder) {
		t.Fatalf("somebody the leaf does not name accepted it: %v", err)
	}

	// Accepting a leaf that reads almost the same is not accepting this one.
	other := leaf
	other.Doing = "keep the archive together, mostly"
	p, _, err := bond.Accept(other, sara.anchor(), "I hold the copies")
	if err != nil {
		t.Fatal(err)
	}
	sara.write(t, "bonds/archive", bond.VerbAccept, p)

	pn, _, err := bond.Accept(leaf, navid.anchor(), "I keep the index")
	if err != nil {
		t.Fatal(err)
	}
	navid.write(t, "bonds/archive", bond.VerbAccept, pn)

	missing, closed, _ := bond.Settled(leaf, w)
	if closed {
		t.Fatal("a leaf closed on an acceptance of a different leaf")
	}
	if len(missing) != 1 || missing[0] != sara.anchor() {
		t.Fatalf("what was owed read as %v", missing)
	}

	// A payload that is not an acceptance is not read as one.
	if _, err := bond.ReadAcceptance([]byte(`{"leaf":"00"}`)); !errors.Is(err, bond.ErrNotAnAcceptance) {
		t.Fatalf("something else was read as an acceptance: %v", err)
	}
}
