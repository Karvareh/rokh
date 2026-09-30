//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
)

// A daemon does not wedge because something else wrote to its carrier.
//
// The CLI works directly on the carrier by design and the lock file is human
// discipline, so a person jotting one line while a daemon is up is ordinary.
// It used to end the daemon: branchHead reads the reference from disk, so the
// next write named a parent the in-memory ledger had never held and came back
// "event pending; not stored" — for that write and for every write after it,
// because a verdict does not reopen. The ancestry was not missing. It was on
// the carrier, unread, and the message named the wrong cause.
//
// So the carrier is re-read when a reference names something the ledger does
// not hold. No event is created by that, and none could be: it is the walk
// that opening a carrier does.
//
//	— T8.5, T4.4, T12.3
func TestAWriteFromOutsideDoesNotWedgeTheDaemon(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	first := mustOK(t, ask(t, s, map[string]any{
		"op": "write", "address": "home/journal", "message": "from the daemon"}),
		"the daemon's first write")

	// Somebody else records an event on the same carrier and moves the
	// branch, exactly as `rokh write` does.
	head, err := frame.ParseID(first["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	outside := writeOutside(t, f, head, "typed by hand")

	// The daemon must pick it up rather than reporting the ancestry missing.
	second := ask(t, s, map[string]any{
		"op": "write", "address": "home/journal", "message": "after the other writer"})
	if ok, _ := second["ok"].(bool); !ok {
		t.Fatalf("the daemon wedged behind an outside write: %v", second["error"])
	}
	if state, _ := second["state"].(string); state != "accepted" {
		t.Fatalf("state is %q", state)
	}

	st := mustOK(t, ask(t, s, map[string]any{"op": "status"}), "status")
	if p := num(t, st["pending"]); p != 0 {
		t.Fatalf("%v events are pending", p)
	}
	if r := num(t, st["rejected"]); r != 0 {
		t.Fatalf("%v events are rejected", r)
	}
	// Five: genesis, the daemon's first, the outside one, the daemon's
	// second — and the outside write's own parent chain holds nothing else.
	if a := num(t, st["accepted"]); a != 4 {
		t.Fatalf("%v events are accepted; expected 4", a)
	}
	heads, _ := st["heads"].([]string)
	if len(heads) != 1 {
		t.Fatalf("the ledger has %d heads: %v", len(heads), heads)
	}

	// And the outside event is really in the daemon's view, not merely
	// stepped over.
	got := mustOK(t, ask(t, s, map[string]any{"op": "get", "id": outside.String()}),
		"the outside event")
	if state, _ := got["state"].(string); state != "accepted" {
		t.Fatalf("the outside event reads as %q", state)
	}

	// A third write still builds on the real head, so nothing was left in a
	// half-recovered state.
	mustOK(t, ask(t, s, map[string]any{
		"op": "write", "address": "home/journal", "message": "and again"}),
		"the daemon's third write")
}

// writeOutside records an event on the carrier without going through the
// server, the way another process holding the same directory would.
func writeOutside(t *testing.T, f *fixture, parent frame.ID, message string) frame.ID {
	t.Helper()
	anchor := f.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Parents: []frame.ID{parent},
		Address: "home/journal/cli", Verb: "note", Payload: []byte(message),
	}, f.root)
	if err != nil {
		t.Fatal(err)
	}
	c, err := carrier.Open(carrier.FS{Root: f.dir}, "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Put(e.Raw); err != nil {
		t.Fatal(err)
	}
	if err := c.SetRef("main", e.ID); err != nil {
		t.Fatal(err)
	}
	return e.ID
}
