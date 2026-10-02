package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"rokh/booth"
	"rokh/event"
	"rokh/frame"
)

// A guest waits on the read-open address; while it
// waits the owner withdraws the open grant and writes there. The waiting
// answer is cut by the view of when it is given, so the new body is not
// shown.
func TestAGuestsWaitIsCutByTheOpenGrantsOfWhenItAnswers(t *testing.T) {
	f, srv := newBoothFixture(t)
	anchor := srv.led.Genesis()
	owner := boothClient(t, srv)
	if r, err := owner.ProveKey([32]byte{}, f.rootPriv); err != nil || r["owner"] != true {
		t.Fatalf("the owner did not bind: %v %v", r, err)
	}
	put := func(e event.Signed) {
		t.Helper()
		if r, _ := owner.Call("append", map[string]any{"raw": hex.EncodeToString(e.Raw)}); r["record"] != Recorded {
			t.Fatalf("not recorded: %v", r)
		}
	}
	op, _ := event.Grant{Open: true, Read: true, Scope: "public"}.Encode()
	og, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{f.outside}, Address: event.AddressRoot, Verb: event.VerbGrant, Payload: op}, f.rootPriv)
	put(og)
	first, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{og.ID}, Address: "public/board", Verb: "note", Payload: []byte("open while it was open")}, f.rootPriv)
	put(first)

	_, groot, _ := ed25519.GenerateKey(rand.Reader)
	ggen, _ := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("a guest's own rokh")}, groot)
	guest := boothClient(t, srv)
	h, err := guest.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "guest", "genesis": hex.EncodeToString(ggen.Raw)})
	if err != nil || h["ok"] != true {
		t.Fatalf("guest hello: %v %v", h, err)
	}
	ch, _ := hex.DecodeString(h["challenge"].(string))
	if who, _ := guest.Call("prove", map[string]any{"sig": hex.EncodeToString(ed25519.Sign(groot, booth.ProofMessage(ch, anchor)))}); who["ok"] != true {
		t.Fatalf("the guest did not bind: %v", who)
	}
	waiting := make(chan map[string]any, 1)
	go func() {
		a, e := guest.Call("wait", map[string]any{"after": first.ID.String(), "address": "public", "timeout": 10})
		if e != nil {
			a = map[string]any{"transport_error": e.Error()}
		}
		waiting <- a
	}()
	select {
	case a := <-waiting:
		t.Fatalf("the wait returned before anything new: %v", a)
	case <-time.After(350 * time.Millisecond):
	}
	rv, _ := event.Revoke{Target: og.ID}.Encode()
	withdraw, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{first.ID}, Address: event.AddressRoot, Verb: event.VerbRevoke, Payload: rv}, f.rootPriv)
	put(withdraw)
	after, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{withdraw.ID}, Address: "public/board", Verb: "note", Payload: []byte("written after the grant was withdrawn")}, f.rootPriv)
	put(after)
	select {
	case a := <-waiting:
		leak := shown(a, map[string]string{"the new body": "written after the grant was withdrawn", "its address": "public/board"})
		t.Logf("the guest's waiting answer: ok=%v code=%v shows=%v", a["ok"], a["code"], leak)
		if len(leak) != 0 {
			t.Fatalf("a guest's wait showed what an open grant withdrawn during it no longer opens: %v", a)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("the waiting request did not end")
	}
}
