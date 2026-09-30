package oracle

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"rokh/event"
	"rokh/frame"
)

// Byte budget: the guard on the narrow path.
//
// Rokh is meant to pass through the narrowest link we support. Today that
// means a LoRa frame: 51 bytes at SF12 up to 222 bytes at SF7 in the EU868
// band, and about 200 usable bytes in Meshtastic.
//
// These numbers are today's sizes plus a little headroom, not aspirations.
// Their job is to catch creep: any field or default attestation added later
// shows up right here.
//
// Today is RKH3 (contract 3.4). It gave every event its body's hash in the
// head and a 32-byte salt in the body, besides its kind, its length and its
// system mark; that contract, not creep, is why the budgets rose from the
// 0.9 ones of 240, 360 and 400 bytes, re-measured at 311, 431 and 436.
//
// And the fact underneath all of them: no Rokh event fits in a single LoRa
// frame. The floor for a self-contained delegated event is about 440 bytes
// on RKH3 and it does not compress away. See docs/04-bandwidth.md.
const (
	budgetGenesis = 330
	budgetGrant   = 450
	budgetWrite   = 460
)

func mustKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestEventSizeBudget(t *testing.T) {
	root := mustKey(t)
	dele := mustKey(t)
	delePub := dele.Public().(ed25519.PublicKey)

	att, errs := Observe(Default())
	for _, e := range errs {
		t.Fatalf("a default oracle failed: %v", e)
	}

	gen, err := event.Sign(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte("first page"), Attest: att,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	anchor := gen.ID

	grantPayload, err := event.Grant{Subject: delePub, Scope: "home/journal"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	grant, err := event.Sign(event.Event{
		Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: event.AddressRoot, Verb: event.VerbGrant,
		Payload: grantPayload, Attest: att,
	}, root)
	if err != nil {
		t.Fatal(err)
	}

	gid := grant.ID
	write, err := event.Sign(event.Event{
		Carrier: &anchor, Authority: &gid, Parents: []frame.ID{grant.ID},
		Address: "home/journal/today", Verb: "note",
		Payload: []byte("a short note for today"), Attest: att,
	}, dele)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		size   int
		budget int
	}{
		{"genesis", len(gen.Raw), budgetGenesis},
		{"grant", len(grant.Raw), budgetGrant},
		{"delegated write", len(write.Raw), budgetWrite},
	}
	for _, c := range cases {
		t.Logf("%-18s %4d bytes  (budget %d)  ~ %d SF7 frames, %d SF12 frames",
			c.name, c.size, c.budget, frames(c.size, 222), frames(c.size, 51))
		if c.size > c.budget {
			t.Errorf("%s over budget: %d > %d", c.name, c.size, c.budget)
		}
	}

	// Default testimony must not add heavy overhead.
	bare, err := event.Sign(event.Event{
		Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: "a", Verb: "note",
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	stamped, err := event.Sign(event.Event{
		Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: "a", Verb: "note", Attest: att,
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	overhead := len(stamped.Raw) - len(bare.Raw)
	t.Logf("%-18s %4d bytes", "default testimony", overhead)
	if overhead > 80 {
		t.Errorf("default testimony got heavy: %d bytes", overhead)
	}
}

func frames(size, mtu int) int { return (size + mtu - 1) / mtu }

// No default oracle may repeat something the event already carries, and none
// may add identifying data unasked.
// A clock in Rokh is testimony inside an event, not an ordering: a number
// somebody said, which can be weighed but never leaned on.
//
//	— T5.3
func TestDefaultOraclesAreMinimal(t *testing.T) {
	names := map[string]bool{}
	for _, o := range Default() {
		names[o.Name()] = true
	}
	if len(names) != 2 || !names["clock"] || !names["chance"] {
		t.Fatalf("the default set changed: %v", names)
	}
	if names["host"] {
		t.Fatal("host must not be a default; it is identifying data")
	}
	if names["carrier"] {
		t.Fatal("the anchor is already an event field; testifying to it is pure repetition")
	}
}
