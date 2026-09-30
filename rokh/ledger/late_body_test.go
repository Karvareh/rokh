// Scenarios: a body that arrives late cannot launder a write out of its
// scope, and every single-bit mutation is refused.
package ledger_test

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// Independent host scenario: arrival order cannot authorize an otherwise
// forbidden body. A lineage-only header must never launder its later body.
func TestLateBodyCannotLaunderOutOfScopeWrite(t *testing.T) {
	root := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	writer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32))
	sign := func(e event.Event, key ed25519.PrivateKey, salt byte) event.Signed {
		t.Helper()
		s, err := event.SignFrom(e, key, bytes.NewReader(bytes.Repeat([]byte{salt}, 64)))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	genesis := sign(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, 3)
	p, err := (event.Grant{Subject: writer.Public().(ed25519.PublicKey), Scope: "allowed", Verbs: []string{"note"}}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	grant := sign(event.Event{Carrier: &genesis.ID, Parents: []frame.ID{genesis.ID}, Address: event.AddressRoot, Verb: event.VerbGrant, Payload: p}, root, 4)
	denied := sign(event.Event{Carrier: &genesis.ID, Authority: &grant.ID, Parents: []frame.ID{grant.ID}, Address: "forbidden/private", Verb: "note", Payload: []byte("unauthorized body")}, writer, 5)
	newLedger := func() *ledger.Ledger {
		t.Helper()
		l, err := ledger.New(genesis.Raw)
		if err != nil {
			t.Fatal(err)
		}
		if s, err := l.Add(grant.Raw); err != nil || s != ledger.Accepted {
			t.Fatalf("grant %v %v", s, err)
		}
		return l
	}
	direct := newLedger()
	if s, _ := direct.Add(denied.Raw); s != ledger.Rejected {
		t.Fatalf("control: full unauthorized event is %v", s)
	}
	split := newLedger()
	head := denied.WithoutBody()
	if _, err := split.Add(head.Raw); err != nil {
		t.Fatal(err)
	}
	state, err := split.Add(denied.Raw)
	got, _ := split.Get(denied.ID)
	if state == ledger.Accepted && !got.HeadOnly {
		t.Fatalf("out-of-scope full body accepted after head-only arrival: state=%v level=%v err=%v; full-first control was rejected", state, split.Judged(denied.ID), err)
	}
}

func TestEverySingleBitMutationIsRefused(t *testing.T) {
	root := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
	s, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("synthetic authenticity test")}, root, bytes.NewReader(bytes.Repeat([]byte{9}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Raw {
		for bit := uint(0); bit < 8; bit++ {
			changed := append([]byte(nil), s.Raw...)
			changed[i] ^= 1 << bit
			if _, err := event.Parse(changed); err == nil {
				t.Fatalf("mutation accepted at byte %d bit %d", i, bit)
			}
		}
	}
}
