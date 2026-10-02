package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net"
	"testing"

	"rokh/booth"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// B5: a guest without a key presents a genesis of its own,
// proves its root, is served the read-open addresses only, and appends only
// under an open grant.
func TestAGuestReadsTheReadOpenAddressAndWritesOnlyThere(t *testing.T) {
	dir := t.TempDir()
	_, root, _ := ed25519.GenerateKey(rand.Reader)
	gen, _ := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("host")}, root)
	car := createV1(t, dir, "synthetic passphrase", root, gen)
	a := gen.ID
	op, _ := event.Grant{Open: true, Read: true, Scope: "public"}.Encode()
	og, _ := event.SignFresh(event.Event{Carrier: &a, Parents: []frame.ID{gen.ID}, Address: event.AddressRoot, Verb: event.VerbGrant, Payload: op}, root)
	private, _ := event.SignFresh(event.Event{Carrier: &a, Parents: []frame.ID{og.ID}, Address: "private/diary", Verb: "note", Payload: []byte("not for guests")}, root)
	recordV1(t, dir, car, []event.Signed{og, private}, "main")
	heads, _ := car.Heads()
	led, err := ledger.Load(gen.Raw, car.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(car, led, Options{AllowSign: true, Dir: dir, Root: root})

	// The guest's own rokh: a genesis whose root it holds.
	_, groot, _ := ed25519.GenerateKey(rand.Reader)
	ggen, _ := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("guest")}, groot)
	c1, c2 := net.Pipe()
	defer c1.Close()
	go srv.ServeBooth(c2)
	c := booth.NewClient(c1)
	h, err := c.Call("hello", map[string]any{"auth": "guest", "genesis": hex.EncodeToString(ggen.Raw)})
	if err != nil || h["ok"] != true {
		t.Fatalf("guest hello: %v %v", h, err)
	}
	ch, _ := hex.DecodeString(h["challenge"].(string))
	anchor, _ := frame.ParseID(h["anchor"].(string))
	who, _ := c.Call("prove", map[string]any{"sig": hex.EncodeToString(ed25519.Sign(groot, booth.ProofMessage(ch, anchor)))})
	if who["ok"] != true || who["guest"] != true {
		t.Fatalf("the guest did not bind: %v", who)
	}
	view, _ := c.Call("view", nil)
	if planets, _ := view["planets"].([]any); len(planets) != 0 {
		t.Fatalf("a guest sees more than the read-open address: %v", planets)
	}
	inside, _ := event.SignFresh(event.Event{Carrier: &a, Authority: &og.ID, Parents: []frame.ID{private.ID}, Address: "public/board", Verb: "note", Payload: []byte("hello from a guest")}, groot)
	r, _ := c.Call("append", map[string]any{"raw": hex.EncodeToString(inside.Raw)})
	if r["record"] != Recorded {
		t.Fatalf("the guest's append inside the open address: %v", r)
	}
	outside, _ := event.SignFresh(event.Event{Carrier: &a, Authority: &og.ID, Parents: []frame.ID{inside.ID}, Address: "private/x", Verb: "note", Payload: []byte("trying")}, groot)
	if r, _ := c.Call("append", map[string]any{"raw": hex.EncodeToString(outside.Raw)}); r["record"] == Recorded {
		t.Fatalf("the guest wrote outside the open address: %v", r)
	}
	if r, _ := c.Call("write", map[string]any{"address": "public/board", "verb": "note", "message": "signed by the booth"}); r["record"] == Recorded {
		t.Fatalf("the booth signed for a guest: %v", r)
	}
	lg, _ := c.Call("log", nil)
	for _, row := range lg["events"].([]any) {
		m := row.(map[string]any)
		if m["id"] == private.ID.String() && m["address"] != nil {
			t.Fatalf("a guest read a private address: %v", m)
		}
	}
}
