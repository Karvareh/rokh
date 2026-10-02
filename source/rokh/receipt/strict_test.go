package receipt_test

import (
	"errors"
	"testing"

	"rokh/canon"
	"rokh/receipt"
)

// Go's encoding/json matches field names case-insensitively, and that is Go's
// leniency rather than JSON's.
//
// A payload spelled {"TYPE":…} is a different byte string from {"type":…} — a
// different event name — and Go reads them as the same value. So without a
// check both would be the same receipt under two names, which is the door
// "one content, one encoding" exists to shut. DisallowUnknownFields does not
// help: "TYPE" is not unknown to Go, it is a match.
//
//	— N4.1, T3.1
func TestACaseVariantKeyIsADifferentByteStringAndIsRefused(t *testing.T) {
	good, err := receipt.OpenIntent("home/work", receipt.Intent{
		Doing: "send the invoice",
		Witness: receipt.Witness{Origin: "the shell", Authority: "g1",
			Audience: "the client", State: "sending", WayBack: "recall it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receipt.Read[receipt.Intent](good.Payload); err != nil {
		t.Fatalf("a receipt refused its own encoding: %v", err)
	}

	// The same object with one key upper-cased. Go decodes it identically.
	bad := []byte(`{"DOING":"send the invoice","type":"rokh.receipt.intent.v1",` +
		`"witness":{"audience":"the client","authority":"g1","origin":"the shell",` +
		`"receipt":"","state":"sending","wayBack":"recall it"}}`)
	if _, err := receipt.Read[receipt.Intent](bad); !errors.Is(err, canon.ErrNotCanonical) {
		t.Fatalf("a case-variant key passed: %v", err)
	}
	// And the cheap peek is as exact as the full read, or "TYPE" would get
	// past the door "type" was checked at.
	for _, spelling := range []string{
		`{"TYPE":"rokh.receipt.intent.v1"}`,
		`{"TyPe":"rokh.receipt.intent.v1"}`,
		`{"Type":"rokh.receipt.intent.v1"}`,
	} {
		if got := receipt.PayloadType([]byte(spelling)); got != "" {
			t.Errorf("%s peeked as %q", spelling, got)
		}
	}
	if got := receipt.PayloadType([]byte(`{"type":"rokh.receipt.intent.v1"}`)); got !=
		receipt.TypeIntent {
		t.Fatalf("the exact key peeked as %q", got)
	}
}

// Duplicate keys: Go takes the last, silently. Another engine may take the
// first or refuse outright, so a payload with two "type" keys has no agreed
// meaning at all.
//
//	— N4.1
func TestDuplicateKeysAreRefusedRatherThanResolved(t *testing.T) {
	dup := []byte(`{"type":"other","type":"rokh.receipt.intent.v1"}`)
	if err := canon.Check(dup); !errors.Is(err, canon.ErrNotCanonical) {
		t.Fatalf("duplicate keys passed the canonical check: %v", err)
	}
	if _, err := receipt.Read[receipt.Intent](dup); err == nil {
		t.Fatal("a payload with duplicate keys was read as a receipt")
	}
	// The peek takes the first and does not silently prefer the last, so a
	// duplicate cannot smuggle a type past a reader that only peeks.
	if got := receipt.PayloadType(dup); got == receipt.TypeIntent {
		t.Fatal("a duplicate key smuggled a type past the peek")
	}
}
