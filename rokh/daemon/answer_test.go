//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/receipt"
)

// bindNari binds a harness with a whole covenant and whole effect declarations.
func bindNari(t *testing.T, s *Server) {
	t.Helper()
	mustOK(t, ask(t, s, map[string]any{
		"op": "bind", "namespace": "clerk", "version": "0.1",
		"can": []any{"clerk.ask", "clerk.answer"}, "unknown": "refuse",
		"repeat": "idempotent", "retry": "never-retry",
		"compensate": "compensable", "ending": "close-unknown",
	}), "bind")
}

// put signs an event and appends it, returning the name the ledger gave it.
func put(t *testing.T, s *Server, f *fixture, e event.Event) frame.ID {
	t.Helper()
	anchor := f.gen.ID
	e.Carrier = &anchor
	if len(e.Parents) == 0 {
		e.Parents = []frame.ID{f.led.Heads()[0]}
	}
	signed, err := event.SignFresh(e, f.root)
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, ask(t, s, map[string]any{
		"op": "append", "raw": hex.EncodeToString(signed.Raw),
	}), "append")
	return signed.ID
}

// A harness's answer passes through the three pillars, and the ledger decides.
//
// This is the whole point of the gate: a harness hands over what it says, where
// each saying came from, what it did and under which authority, and it does not
// get to mark its own work. Every name is looked up in the ledger the answer
// claims to stand on.
//
//	— T2, T2.1, T2.2, T2.3
func TestAHarnessAnswerPassesThroughTheThreePillars(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	bindNari(t, s)

	// A real event to cite.
	cited := put(t, s, f, event.Event{Address: "clerk/desk", Verb: "clerk.ask",
		Payload: []byte("when did I write this?")})

	// A real grant, and a real intent opened under it.
	_, subject, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gp, err := event.Grant{Subject: subject.Public().(ed25519.PublicKey),
		Scope: "clerk", Verbs: []string{"clerk.answer"}}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	grant := put(t, s, f, event.Event{Address: event.AddressRoot,
		Verb: event.VerbGrant, Payload: gp})

	opening, err := receipt.OpenIntent("clerk/desk", receipt.Intent{
		Doing: "answer the question",
		Witness: receipt.Witness{Origin: "the shell", Authority: grant.String(),
			Audience: "the person", State: "answering", WayBack: "say so"},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent := put(t, s, f, opening)

	anchor := f.gen.ID.String()
	sound := mustOK(t, ask(t, s, map[string]any{
		"op": "answer", "namespace": "clerk", "anchor": anchor,
		"claims": []any{map[string]any{
			"saying": "you asked this on the desk", "from": []any{cited.String()},
		}},
		"acts": []any{map[string]any{
			"doing": "answered it", "authority": grant.String(), "receipt": intent.String(),
		}},
	}), "answer")
	if sound["sound"] != true {
		t.Fatalf("an answer standing on all three was refused: %v", sound["owed"])
	}
}

// Each pillar is a real refusal, and the ledger is what refuses.
//
// An unaddressed saying, a citation this ledger does not hold, an ordinary
// event passed off as a grant, an act with no receipt, someone else's ground:
// none of these is caught by trusting the harness, and each is named.
//
//	— T2.1, T2.2, T2.3
func TestTheLedgerRefusesEachPillarInTurn(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	bindNari(t, s)

	held := put(t, s, f, event.Event{Address: "clerk/desk", Verb: "clerk.ask",
		Payload: []byte("a real event, and only that")})
	anchor := f.gen.ID.String()
	stranger := frame.Hash([]byte("an event in someone else's ledger")).String()

	base := func() map[string]any {
		return map[string]any{"op": "answer", "namespace": "clerk", "anchor": anchor}
	}

	// Sound memory: a saying with no address behind it.
	req := base()
	req["claims"] = []any{map[string]any{"saying": "it simply is so"}}
	if r := mustOK(t, ask(t, s, req), "answer"); r["sound"] == true {
		t.Error("a quotation with no address passed")
	}

	// Sound memory: an address this ledger does not hold.
	req = base()
	req["claims"] = []any{map[string]any{"saying": "somewhere it says", "from": []any{stranger}}}
	if r := mustOK(t, ask(t, s, req), "answer"); r["sound"] == true {
		t.Error("a citation outside this ledger passed")
	}

	// Source of authority: an ordinary event passed off as the grant. This is
	// the one that the old check could not catch, because it only ever asked
	// whether the ledger had the id at all.
	req = base()
	req["acts"] = []any{map[string]any{"doing": "did it",
		"authority": held.String(), "receipt": held.String()}}
	r := mustOK(t, ask(t, s, req), "answer")
	if r["sound"] == true {
		t.Error("an ordinary event was accepted as an authority")
	}
	if owed, _ := r["owed"].([]string); len(owed) != 2 {
		t.Errorf("two absent guards were reported as %d: %v", len(owed), r["owed"])
	}

	// Source of authority: no receipt at all.
	req = base()
	req["acts"] = []any{map[string]any{"doing": "did it", "authority": held.String()}}
	if r := mustOK(t, ask(t, s, req), "answer"); r["sound"] == true {
		t.Error("an act with no receipt passed")
	}

	// Personal ground: built on someone else's anchor.
	req = base()
	req["anchor"] = stranger
	req["claims"] = []any{map[string]any{"saying": "so it is", "from": []any{held.String()}}}
	if r := mustOK(t, ask(t, s, req), "answer"); r["sound"] == true {
		t.Error("an answer built on someone else's ground passed")
	}

	// And none of this recorded anything: checking an answer is not performing
	// it.
	before := f.led.Len()
	mustOK(t, ask(t, s, base()), "answer")
	if f.led.Len() != before {
		t.Fatal("the gate wrote to the ledger")
	}
}

// A harness that never bound cannot put an answer through the gate under a
// name it does not hold.
//
//	— T2.3, T11.10
func TestOnlyABoundHarnessAnswersHere(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	r := ask(t, s, map[string]any{"op": "answer", "namespace": "clerk",
		"anchor": f.gen.ID.String()})
	if r["ok"] == true {
		t.Fatal("an unbound harness answered")
	}
	bindNari(t, s)
	mustOK(t, ask(t, s, map[string]any{"op": "answer", "namespace": "clerk",
		"anchor": f.gen.ID.String()}), "answer")
}
