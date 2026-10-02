package bond_test

import (
	"encoding/json"
	"strings"
	"testing"

	"rokh/bond"
	"rokh/canon"
	"rokh/frame"
	"rokh/receipt"
)

// The bug this package exists for, held down where it actually bit.
//
// A leaf's name is the hash of its bytes, and a receipt rides inside an event
// payload whose bytes are its name. Both went through encoding/json, which
// escapes < > and & — so a bond to "keep the books & the archive" had one name
// in Go and another in every other language, and bond.Parse would have refused
// the other language's bytes as non-canonical, which reads like corruption
// rather than like a disagreement about spelling.
func TestAnAmpersandDoesNotRenameThingsBetweenEngines(t *testing.T) {
	l := bond.Leaf{Kind: bond.Founding, Doing: "keep the books & the archive",
		Founders: []frame.ID{frame.Hash([]byte("a")), frame.Hash([]byte("b"))}}
	raw, err := l.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\\u0026") {
		t.Fatalf("the ampersand was escaped: %s", raw)
	}
	if !strings.Contains(string(raw), "&") {
		t.Fatalf("the ampersand did not survive as itself: %s", raw)
	}
	// And the bytes are the one encoding, so another engine writing the same
	// leaf writes the same bytes and gets the same name.
	if err := canon.Check(raw); err != nil {
		t.Fatalf("a leaf's own bytes are not canonical: %v", err)
	}
	if _, err := bond.Parse(raw); err != nil {
		t.Fatalf("a leaf refused its own encoding: %v", err)
	}

	// Same for a receipt, whose free text is the likeliest place for one.
	e, err := receipt.OpenIntent("home/work", receipt.Intent{
		Doing: "send the invoice & the note",
		Witness: receipt.Witness{Origin: "the shell", Authority: "g1",
			Audience: "the client", State: "sending", WayBack: "recall it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(e.Payload), "\\u0026") {
		t.Fatalf("the receipt escaped its ampersand: %s", e.Payload)
	}
	if err := canon.Check(e.Payload); err != nil {
		t.Fatalf("a receipt payload is not canonical: %v", err)
	}
}

// An id inside a payload is its hex, not a list of thirty-two numbers.
//
// encoding/json writes a byte *array* as numbers, so a name inside a leaf read
// [62,35,232,...] — not wrong so much as unshared, since every other spelling
// of an id in Rokh, in its documents and on screen, is the hex.
func TestAnIdInsideAPayloadIsItsHex(t *testing.T) {
	id := frame.Hash([]byte("an event"))
	raw, err := canon.Marshal(map[string]any{"parent": id})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"parent":"` + id.String() + `"}`
	if string(raw) != want {
		t.Fatalf("got  %s\nwant %s", raw, want)
	}
	// And it reads back to the same id.
	var back struct {
		Parent frame.ID `json:"parent"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Parent != id {
		t.Fatal("an id did not survive the round trip")
	}
	// Something that is not an id in hex is refused rather than guessed at.
	if err := json.Unmarshal([]byte(`{"parent":"nope"}`), &back); err == nil {
		t.Fatal("a bad id was accepted")
	}
}
