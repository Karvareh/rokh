package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// R1: a body that overturns a head the door held. The offer is answered as
// not recorded with the overturned ids; the door's views drop them; a branch
// tip that names them is not served as a head; and the door opened again on
// the same carrier — closed and opened, not compared with memory — gives the
// same states.
func TestADoorAnswersAndRemembersABodyThatOverturnsItsHead(t *testing.T) {
	dir := t.TempDir()
	_, root, _ := ed25519.GenerateKey(rand.Reader)
	wPub, writer, _ := ed25519.GenerateKey(rand.Reader)
	gen, _ := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("r1")}, root)
	car := createV1(t, dir, "synthetic passphrase", root, gen)
	a := gen.ID
	gp, _ := event.Grant{Subject: wPub, Scope: "allowed", Verbs: []string{"note"}}.Encode()
	grant, _ := event.SignFresh(event.Event{Carrier: &a, Parents: []frame.ID{gen.ID}, Address: event.AddressRoot, Verb: event.VerbGrant, Payload: gp}, root)
	denied, _ := event.SignFresh(event.Event{Carrier: &a, Authority: &grant.ID, Parents: []frame.ID{grant.ID}, Address: "forbidden/private", Verb: "note", Payload: []byte("unauthorized")}, writer)
	child, _ := event.SignFresh(event.Event{Carrier: &a, Authority: &grant.ID, Parents: []frame.ID{denied.ID}, Address: "allowed/x", Verb: "note", Payload: []byte("rests on it")}, writer)
	// The head alone of the denied event: the door accepts it at lineage.
	recordV1(t, dir, car, []event.Signed{grant, denied.WithoutBody(), child}, "main")
	open := func(c *carrier.Carrier) *Server {
		heads, _ := c.Heads()
		g, _ := c.Get(c.Anchor())
		l, err := ledger.Load(g, c.Get, heads)
		if err != nil {
			t.Fatal(err)
		}
		return New(c, l, Options{AllowSign: true, Dir: dir, Root: root})
	}
	srv := open(car)
	if srv.led.State(child.ID) != ledger.Accepted || srv.led.Judged(child.ID) != ledger.Lineage {
		t.Fatalf("before the body: %s %s", srv.led.State(child.ID), srv.led.Judged(child.ID))
	}
	line, _ := json.Marshal(map[string]any{"op": "append", "raw": hex.EncodeToString(denied.Raw)})
	r := srv.Handle(line)
	over, _ := r["overturned"].([]string)
	if r["ok"] != false || r["record"] != NotRecorded || r["code"] != "not_accepted" || len(over) != 2 {
		t.Fatalf("1. the offer's answer: %v", r)
	}
	st := srv.Handle([]byte(`{"op":"status"}`))
	for _, h := range st["heads"].([]string) {
		if h == child.ID.String() || h == denied.ID.String() {
			t.Fatalf("2. an overturned id is still a head: %v", st["heads"])
		}
	}
	if b, _ := st["branches"].(map[string]string); b["main"] != "" {
		t.Fatalf("3. the branch tip naming an overturned event is served: %v", st)
	}
	lg := srv.Handle([]byte(`{"op":"log"}`))
	for _, row := range lg["events"].([]map[string]any) {
		if row["id"] == child.ID.String() || row["id"] == denied.ID.String() {
			t.Fatalf("2. log still shows an overturned event: %v", row)
		}
	}
	// 4. Opened again from the folder: the same refusals, not a lineage
	// acceptance of the head alone.
	car2, _ := openV1(t, dir, "synthetic passphrase")
	again := open(car2)
	for _, id := range []frame.ID{denied.ID, child.ID} {
		if s := again.led.State(id); s == ledger.Accepted {
			t.Fatalf("4. after reopening, %s is %s: the refusal did not survive", id.Short(), s)
		}
	}
	if again.led.State(grant.ID) != ledger.Accepted {
		t.Fatal("the grant itself stays accepted")
	}
}
