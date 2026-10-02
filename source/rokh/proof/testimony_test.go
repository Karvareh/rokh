package proof

import (
	"encoding/base64"
	"testing"

	"rokh/daemon"
	"rokh/frame"
	"rokh/ledger"
)

// Three questions, three fields, and none of them stands in for another: the
// author is the key that signed, the authority is the grant it signed under,
// and the door is only the registrar's mark — an attestation, which is the
// signer saying where it signed and nothing more. Change the door and the
// verdict does not move, because a verdict is about bytes, lineage and
// authority and never about testimony. That is also the whole of what a
// signature proves: who signed which bytes. Not that it was true, and not
// that anybody consented.
//
//	— T12.5, T10.2, T12, T12.2
func TestTheDoorsMarkIsTestimonyAndNeverAVerdict(t *testing.T) {
	w := newFastWorld(t)
	led := w.load()

	home := daemon.New(w.car, led, w.at(daemon.Options{AllowSign: true, Door: "rokh-home/program"}))
	plain := daemon.New(w.car, led, w.at(daemon.Options{AllowSign: true}))

	marked := recorded(t, ask(t, home, map[string]any{"op": "write", "address": "home/journal",
		"verb": "note", "message": "written at a named door"}), "the marked recording")
	bare := recorded(t, ask(t, plain, map[string]any{"op": "write", "address": "home/journal",
		"verb": "tag", "message": "written at a door with no name"}), "the unmarked recording")

	// Both are accepted; the mark bought nothing and cost nothing.
	for _, r := range []map[string]any{marked, bare} {
		id, err := frame.ParseID(r["id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if led.State(id) != ledger.Accepted {
			t.Fatalf("%v is not accepted", r["id"])
		}
	}

	rows := mustOK(t, ask(t, plain, map[string]any{"op": "log"}), "log")["events"].([]map[string]any)
	byID := map[string]map[string]any{}
	for _, row := range rows {
		byID[row["id"].(string)] = row
	}
	markedRow, bareRow := byID[marked["id"].(string)], byID[bare["id"].(string)]
	if markedRow == nil || bareRow == nil {
		t.Fatal("the log does not hold both recordings")
	}
	if markedRow["door"] != "rokh-home/program" {
		t.Fatalf("the door's mark is %v, want %q", markedRow["door"], "rokh-home/program")
	}
	if _, said := bareRow["door"]; said {
		t.Fatalf("a door with no name left a mark anyway: %v", bareRow["door"])
	}
	// The mark lives in the attestations, inside the bytes the signature
	// covers, and it is the signer's testimony — not a field of its own.
	att, _ := markedRow["attest"].([]map[string]string)
	found := false
	for _, a := range att {
		if a["oracle"] != "door" {
			continue
		}
		claim, err := base64.StdEncoding.DecodeString(a["claim"])
		if err != nil {
			t.Fatal(err)
		}
		if string(claim) != "rokh-home/program" {
			t.Fatalf("the door attested %q", claim)
		}
		found = true
	}
	if !found {
		t.Fatalf("the door's mark is not an attestation: %v", att)
	}
	// The author is the key and the door is not it.
	if markedRow["author"] != bareRow["author"] {
		t.Fatal("two recordings by one key report two authors")
	}

	// And the log's aperture is address and verb, which are what an event
	// says about itself — nothing here narrows by who recorded it.
	//   — T10.6
	only := mustOK(t, ask(t, plain, map[string]any{"op": "log", "verb": "tag"}), "log by verb")["events"].([]map[string]any)
	if len(only) != 1 || only[0]["id"] != bare["id"] {
		t.Fatalf("narrowing by verb gave %d rows", len(only))
	}
	elsewhere := mustOK(t, ask(t, plain, map[string]any{"op": "log", "address": "work"}), "log by address")["events"].([]map[string]any)
	if len(elsewhere) != 0 {
		t.Fatalf("narrowing to an address nobody wrote at gave %d rows", len(elsewhere))
	}
	after := mustOK(t, ask(t, plain, map[string]any{"op": "log", "after": marked["id"], "limit": 10}), "log after")["events"].([]map[string]any)
	if len(after) != 1 || after[0]["id"] != bare["id"] {
		t.Fatalf("the cursor handed back %d rows", len(after))
	}
}
