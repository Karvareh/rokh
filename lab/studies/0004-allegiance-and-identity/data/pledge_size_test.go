// Measures a pledge: a bond of two founders, a person and the one they pledge
// to, accepted by each in their own ledger with the bond package as it is.
// Copy into source/rokh/oracle and run:
//
//	go test -count=1 -run TestPledgeSize -v ./oracle
//
// It changes nothing in the tree and asserts nothing; it reports.
package oracle

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"rokh/bond"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

func TestPledgeSize(t *testing.T) {
	type side struct {
		priv ed25519.PrivateKey
		gen  event.Signed
		l    *ledger.Ledger
	}
	born := func() side {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		gen, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, priv, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		l, err := ledger.New(gen.Raw)
		if err != nil {
			t.Fatal(err)
		}
		return side{priv, gen, l}
	}
	person, ruler := born(), born()
	t.Logf("%-40s %4d bytes", "a genesis, the anchor of an identity", len(person.gen.Raw))

	leaf := bond.Leaf{Kind: bond.Founding, Founders: []frame.ID{person.gen.ID, ruler.gen.ID}, Doing: "allegiance"}
	lb, err := leaf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%-40s %4d bytes", "the leaf of a pledge", len(lb))

	for _, s := range []struct {
		who   side
		place string
	}{{person, "I pledge"}, {ruler, "I answer for it"}} {
		p, name, err := bond.Accept(leaf, s.who.gen.ID, s.place)
		if err != nil {
			t.Fatal(err)
		}
		anchor := s.who.gen.ID
		ev, err := event.SignFrom(event.Event{Carrier: &anchor, Parents: []frame.ID{s.who.gen.ID},
			Address: "bond/" + name.String()[:16], Verb: bond.VerbAccept, Payload: p}, s.who.priv, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		st, err := s.who.l.Add(ev.Raw)
		t.Logf("%-40s %4d bytes, %s %v", "an acceptance, \""+s.place+"\"", len(ev.Raw), st, err)
	}
}
