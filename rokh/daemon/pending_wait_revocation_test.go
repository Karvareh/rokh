package daemon

// A waiting, already authenticated reader must be
// reauthorized when the answer is produced after its key has been revoked.
import (
	"encoding/hex"
	"rokh/booth"
	"rokh/event"
	"rokh/frame"
	"testing"
	"time"
)

func TestRevocationAlsoCutsAnAlreadyWaitingRequest(t *testing.T) {
	f, srv := newBoothFixture(t)
	reader := boothClient(t, srv)
	if ans, err := reader.ProveReader(f.readerKid, f.reader); err != nil || ans["ok"] != true {
		t.Fatalf("reader proof: %v %v", ans, err)
	}
	owner := boothClient(t, srv)
	if ans, err := owner.ProveKey([32]byte{}, f.rootPriv); err != nil || ans["owner"] != true {
		t.Fatalf("owner proof: %v %v", ans, err)
	}
	waiting := make(chan map[string]any, 1)
	go func() {
		a, e := reader.Call("wait", map[string]any{"after": f.outside.String(), "address": "journal", "timeout": 10})
		if e != nil {
			a = map[string]any{"transport_error": e.Error()}
		}
		waiting <- a
	}()
	select {
	case a := <-waiting:
		t.Fatalf("fixture: wait returned before a new event: %v", a)
	case <-time.After(350 * time.Millisecond):
	}
	srv.mu.RLock()
	adds, _ := srv.led.Keyring()
	anchor := srv.led.Genesis()
	srv.mu.RUnlock()
	if len(adds) != 1 {
		t.Fatalf("fixture keyring count: %d", len(adds))
	}
	payload, err := (event.Keyring{Op: event.KeyringRevoke, Key: f.readerKid, Gen: 1, Target: adds[0]}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	revoke, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{f.outside}, Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: payload}, f.rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	if a, e := owner.Call("append", map[string]any{"raw": hex.EncodeToString(revoke.Raw)}); e != nil || a["record"] != Recorded {
		t.Fatalf("revoke: %v %v", a, e)
	}
	fresh, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{revoke.ID}, Address: "journal/after-revocation", Verb: "note", Payload: []byte("synthetic bytes written only after revocation")}, f.rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	if a, e := owner.Call("append", map[string]any{"raw": hex.EncodeToString(fresh.Raw)}); e != nil || a["record"] != Recorded {
		t.Fatalf("fresh write: %v %v", a, e)
	}
	select {
	case a := <-waiting:
		leak := shown(a, map[string]string{"new body": "synthetic bytes written only after revocation"})
		t.Logf("waiting answer: ok=%v code=%v leaked=%v", a["ok"], a["code"], leak)
		if a["code"] != booth.CodeKeyRevoked || len(leak) != 0 {
			t.Fatalf("a request started before revocation was served after it: %v", a)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("waiting request did not end")
	}
}
