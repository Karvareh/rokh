package gate

import (
	"testing"

	"rokh-home/home"
	"rokh-home/native"
)

// testNativeNamespace stands for the namespace an engine manifest names.
const testNativeNamespace = "native/holochain"

func TestNativeUnknownThenKnownKeepsBothLedgerWitnesses(t *testing.T) {
	f := newGate(t)
	witness := map[string]any{"origin": "synthetic item", "authority": "synthetic owner",
		"audience": "synthetic peer", "state": "unchanged disclosure", "wayBack": "future revocation"}
	in := f.h.OwnerLedger(map[string]any{"op": "intent", "address": testNativeNamespace,
		"doing": "synthetic native publication", "witness": witness, "attempt": "synthetic-native-intent"})
	if in["ok"] != true {
		t.Fatal(in)
	}
	intent := in["id"].(string)
	first := f.h.OwnerLedger(nativeOutcomeRequest(testNativeNamespace, intent, "same-publication", "unknown", "reply lost", witness))
	if first["ok"] != true || first["record"] != "recorded" {
		t.Fatal(first)
	}
	req := nativeOutcomeRequest(testNativeNamespace, intent, "same-publication", "done", "verified native action", witness)
	second := f.h.OwnerLedger(req)
	if second["ok"] != true || second["record"] != "recorded" || second["id"] == first["id"] {
		t.Fatalf("a newly known ending must not conflict with its preserved unknown witness: %v", second)
	}
	again := f.h.OwnerLedger(req)
	if again["ok"] != true || again["id"] != second["id"] || again["already"] != true {
		t.Fatalf("retry duplicated the known ending: %v", again)
	}
	for _, id := range []any{first["id"], second["id"]} {
		if r := f.h.OwnerLedger(map[string]any{"op": "get", "id": id}); r["ok"] != true {
			t.Fatalf("earlier witness was lost: %v", r)
		}
	}
}

func TestNativeEffectCannotHideARefusedOutcome(t *testing.T) {
	f := newGate(t)
	id, _ := f.program("synthetic publisher", map[string]string{"ledger": testNativeNamespace}, nil)
	c, _ := f.h.Consumer(id)
	a := home.Actor{Consumer: id, Session: c.Version}
	if err := f.h.RevokeConsumer(home.OwnerActor, id); err != nil {
		t.Fatal(err)
	}
	outcome := f.h.LedgerOp(a, map[string]any{"op": "outcome", "address": testNativeNamespace})
	if outcome["ok"] != false {
		t.Fatal("revoked actor unexpectedly recorded an outcome", outcome)
	}
	result := native.PublishResult{Record: "recorded", Manuscript: "synthetic native effect witness"}
	r := nativeCompletion(map[string]any{"record": result.Record, "native": result}, result, outcome)
	if r["ok"] != false || r["record"] != "unknown" || r["stage"] != "outcome" {
		t.Fatalf("native success hid the incomplete ledger receipt: %v", r)
	}
	if r["native"].(native.PublishResult).Record != "recorded" {
		t.Fatal("failure erased the already observed native effect")
	}
}
