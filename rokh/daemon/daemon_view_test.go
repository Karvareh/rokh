package daemon

// Probes that run the daemon's booth on a real v1 carrier in a
// folder of the test, with the fixture of booth_test.go.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net"
	"sort"
	"testing"

	"rokh/booth"
	"rokh/event"
	"rokh/frame"
)

func boothClient(t *testing.T, srv *Server) *booth.Client {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	go srv.ServeBooth(b)
	return booth.NewClient(a)
}

// shown lists which of the needles an answer shows, as text or inside any
// string of it that is hex or base64.
func shown(ans any, needles map[string]string) []string {
	found := map[string]bool{}
	var walk func(v any)
	look := func(b []byte) {
		for name, n := range needles {
			if bytes.Contains(b, []byte(n)) {
				found[name] = true
			}
		}
	}
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				look([]byte(k))
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case string:
			look([]byte(x))
			if b, err := hex.DecodeString(x); err == nil {
				look(b)
			}
			for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
				if b, err := enc.DecodeString(x); err == nil {
					look(b)
				}
			}
		}
	}
	walk(ans)
	out := make([]string, 0, len(found))
	for n := range found {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type ask3 struct {
	op     string
	fields map[string]any
}

func asks3(f *boothFixture, after frame.ID) []ask3 {
	return []ask3{
		{"log", nil},
		{"get", map[string]any{"event": f.outside.String()}},
		{"get", map[string]any{"event": f.inside.String()}},
		{"wait", map[string]any{"after": after.String(), "timeout": 1}},
		{"status", nil},
		{"view", nil},
		{"seed", nil},
		{"capabilities", nil},
		{"bound", nil},
		{"receipts", nil},
		{"attempt", map[string]any{"attempt": "none"}},
		{"whoami", nil},
	}
}

// B-2 on the daemon: what a session that proved nothing is served.
func TestAStrangerIsServedNothingByTheDaemon(t *testing.T) {
	_, srv := newBoothFixture(t)
	c := boothClient(t, srv)
	c.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "credential"})
	p, _ := c.Call("prove", map[string]any{})
	t.Logf("prove with nothing: ok=%v key=%v", p["ok"], p["key"])
	for _, op := range []string{"status", "view", "log", "capabilities", "bound", "seed", "receipts", "whoami"} {
		r, err := c.Call(op, nil)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		t.Logf("%-12s ok=%v code=%v", op, r["ok"], r["code"])
		if r["code"] != booth.CodeHelloFirst {
			t.Errorf("DEFECT B-2: %q was answered to a session that proved nothing (ok=%v)", op, r["ok"])
		}
	}
}

// B-3. Contract B4: a key sees its own view and nothing else. Every reading
// op is asked by the key that reads "journal"; the event at private/diary is
// outside its view.
func TestEveryReadingOpStaysInsideTheView(t *testing.T) {
	f, srv := newBoothFixture(t)
	c := boothClient(t, srv)
	if who, err := c.ProveReader(f.readerKid, f.reader); err != nil || who["ok"] != true {
		t.Fatalf("the reading key did not bind: %v %v", who, err)
	}
	outside := map[string]string{"the address outside": "private/diary", "the payload outside": "outside the view"}
	for _, a := range asks3(f, f.inside) {
		r, err := c.Call(a.op, a.fields)
		if err != nil {
			t.Fatalf("%s: %v", a.op, err)
		}
		got := shown(r, outside)
		t.Logf("%-12s ok=%v code=%v shows: %v", a.op, r["ok"], r["code"], got)
		if len(got) > 0 {
			t.Errorf("DEFECT B-3: %q shows a key what lies outside its view: %v", a.op, got)
		}
	}
}

// B-4. A key that the owner revokes is served nothing more, also on a
// session it bound before.
func TestARevokedKeyIsServedNothingMore(t *testing.T) {
	f, srv := newBoothFixture(t)
	c := boothClient(t, srv)
	if who, err := c.ProveReader(f.readerKid, f.reader); err != nil || who["ok"] != true {
		t.Fatalf("the reading key did not bind: %v %v", who, err)
	}
	srv.mu.RLock()
	adds, _ := srv.led.Keyring()
	anchor := srv.led.Genesis()
	srv.mu.RUnlock()
	if len(adds) != 1 {
		t.Fatalf("the fixture's keyring holds %d adds", len(adds))
	}
	kr, err := event.Keyring{Op: event.KeyringRevoke, Key: f.readerKid, Gen: 1, Target: adds[0]}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	rv, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{f.outside}, Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: kr}, f.rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	oc := boothClient(t, srv)
	if r, err := oc.ProveKey([32]byte{}, f.rootPriv); err != nil || r["owner"] != true {
		t.Fatalf("the owner did not bind: %v %v", r, err)
	}
	if r, _ := oc.Call("append", map[string]any{"raw": hex.EncodeToString(rv.Raw)}); r["record"] != Recorded {
		t.Fatalf("the revoke was not recorded: %v", r)
	}
	inside := map[string]string{"the address inside": "journal/today", "the payload inside": "inside the view"}
	for _, a := range []ask3{{"log", nil}, {"get", map[string]any{"event": f.inside.String()}}, {"view", nil}} {
		r, _ := c.Call(a.op, a.fields)
		got := shown(r, inside)
		t.Logf("after the revoke, %-6s ok=%v code=%v shows: %v", a.op, r["ok"], r["code"], got)
		if r["ok"] == true || len(got) > 0 {
			t.Errorf("DEFECT B-4: a key the owner revoked is still served %q on the session it bound before (code %v, shows %v)", a.op, r["code"], got)
		}
	}
	c2 := boothClient(t, srv)
	if h, _ := c2.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "key", "key": hex.EncodeToString(f.readerKid[:])}); h["ok"] == true {
		t.Errorf("DEFECT B-4: a revoked key is offered a challenge")
	}
}

// B5. A guest is served the read-open addresses and nothing else: no event
// of another address, and no system event of this rokh, which names its keys.
func TestAGuestIsServedTheReadOpenAddressesOnly(t *testing.T) {
	f, srv := newBoothFixture(t)
	anchor := srv.led.Genesis()
	oc := boothClient(t, srv)
	if r, err := oc.ProveKey([32]byte{}, f.rootPriv); err != nil || r["owner"] != true {
		t.Fatalf("the owner did not bind: %v %v", r, err)
	}
	put := func(e event.Signed) {
		t.Helper()
		if r, _ := oc.Call("append", map[string]any{"raw": hex.EncodeToString(e.Raw)}); r["record"] != Recorded {
			t.Fatalf("not recorded: %v", r)
		}
	}
	op, _ := event.Grant{Open: true, Read: true, Scope: "public"}.Encode()
	og, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{f.outside}, Address: event.AddressRoot, Verb: event.VerbGrant, Payload: op}, f.rootPriv)
	put(og)
	pub, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{og.ID}, Address: "public/board", Verb: "note", Payload: []byte("for everyone")}, f.rootPriv)
	put(pub)

	_, groot, _ := ed25519.GenerateKey(rand.Reader)
	ggen, _ := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("a guest's own rokh")}, groot)
	c := boothClient(t, srv)
	h, err := c.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "guest", "genesis": hex.EncodeToString(ggen.Raw)})
	if err != nil || h["ok"] != true {
		t.Fatalf("MISSING B5: the booth offers no guest: %v %v", h, err)
	}
	ch, _ := hex.DecodeString(h["challenge"].(string))
	who, _ := c.Call("prove", map[string]any{"sig": hex.EncodeToString(ed25519.Sign(groot, booth.ProofMessage(ch, anchor)))})
	if who["ok"] != true || who["guest"] != true {
		t.Fatalf("the guest did not bind: %v", who)
	}
	t.Logf("the guest: reads=%v writes=%v", who["reads"], who["writes"])
	closed := map[string]string{
		"an address outside (private/diary)": "private/diary", "its payload": "outside the view",
		"an address not open (journal/today)": "journal/today", "its payload ": "inside the view",
		"the name of a key of this rokh": "journal-reader",
	}
	open := map[string]string{"the open address": "public/board", "its payload": "for everyone"}
	sawOpen := false
	for _, a := range asks3(f, anchor) {
		r, err := c.Call(a.op, a.fields)
		if err != nil {
			t.Fatalf("%s: %v", a.op, err)
		}
		got := shown(r, closed)
		if len(shown(r, open)) == 2 {
			sawOpen = true
		}
		t.Logf("%-12s ok=%v code=%v shows of what is closed: %v", a.op, r["ok"], r["code"], got)
		if len(got) > 0 {
			t.Errorf("DEFECT B5: %q shows a guest %v", a.op, got)
		}
	}
	if !sawOpen {
		t.Errorf("no reading op showed the guest the event at the read-open address")
	}
	// A wrong root proves nothing.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	c2 := boothClient(t, srv)
	h2, _ := c2.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "guest", "genesis": hex.EncodeToString(ggen.Raw)})
	ch2, _ := hex.DecodeString(h2["challenge"].(string))
	if p, _ := c2.Call("prove", map[string]any{"sig": hex.EncodeToString(ed25519.Sign(other, booth.ProofMessage(ch2, anchor)))}); p["ok"] == true {
		t.Errorf("DEFECT B5: a guest was bound by a key that is not the root of the genesis it presented")
	}
	// This rokh's own genesis, presented by a stranger, binds nothing.
	srv.mu.RLock()
	own, _ := srv.led.Get(anchor)
	srv.mu.RUnlock()
	c3 := boothClient(t, srv)
	h3, _ := c3.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "guest", "genesis": hex.EncodeToString(own.Raw)})
	t.Logf("a guest that presents this rokh's own genesis: hello ok=%v code=%v", h3["ok"], h3["code"])
	if h3["ok"] == true {
		ch3, _ := hex.DecodeString(h3["challenge"].(string))
		if p, _ := c3.Call("prove", map[string]any{"sig": hex.EncodeToString(ed25519.Sign(groot, booth.ProofMessage(ch3, anchor)))}); p["ok"] == true {
			t.Errorf("DEFECT B5: a stranger was bound with this rokh's own genesis")
		}
	}
}
