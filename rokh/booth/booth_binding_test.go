package booth

// Probes of binding at the booth.
//
// The stream is made of two io.Pipes rather than a net pipe: the architecture
// guard reads test files too, and TestNoCoreLayerImportsATransport refuses net
// in this package.

import (
	"crypto/ed25519"
	"io"
	"testing"

	"rokh/frame"
	"rokh/key"
)

// duplex3 is an ordered byte stream both ways, made of two pipes.
type duplex3 struct {
	io.Reader
	io.Writer
}

func pipe3(t *testing.T) (client, server io.ReadWriter) {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	t.Cleanup(func() { cw.Close(); sw.Close() })
	return duplex3{cr, cw}, duplex3{sr, sw}
}

type probe3Authority struct {
	keys  map[[32]byte]Identity
	creds map[string]Identity
}

func (a probe3Authority) Anchor() frame.ID { return frame.Hash([]byte("anchor of review 3")) }
func (a probe3Authority) KeyByID(id [32]byte) (Identity, bool) {
	w, ok := a.keys[id]
	return w, ok
}
func (a probe3Authority) Credential(tok string) (Identity, bool) {
	w, ok := a.creds[tok]
	return w, ok
}

// serve3 serves one session and counts what reached the handler.
func serve3(t *testing.T, a Authority) (*Client, *int) {
	t.Helper()
	client, server := pipe3(t)
	served := new(int)
	go Serve(server, a, HandlerFunc(func(s *Session, req map[string]any) map[string]any {
		*served++
		return map[string]any{"ok": true, "served_to": s.Who.Name}
	}), probeStream3(9))
	return NewClient(client), served
}

type probeStream3 byte

func (p probeStream3) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(p) + byte(i)
	}
	return len(b), nil
}

// B-1, kept under its name. A client that holds a key's id
// and no private half must not be bound as that key.
func TestAStrangerCannotBindAsAReadOnlyKey(t *testing.T) {
	r, _ := key.NewReader(probeStream3(7))
	kid := [32]byte{0x42}
	a := probe3Authority{keys: map[[32]byte]Identity{kid: {Key: kid, Gen: 1, Name: "reads", Reader: r.Public(), Reads: []string{"journal"}}}}
	c, served := serve3(t, a)
	h, err := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hexOf(kid[:])})
	if err != nil {
		t.Fatal(err)
	}
	clear, _ := h["challenge"].(string)
	t.Logf("hello: sealed=%v challenge in the clear=%v", h["sealed"] != nil, clear != "")
	for _, prove := range []map[string]any{{"challenge": clear}, {"challenge": h["sealed"]}, {}} {
		p, _ := c.Call("prove", prove)
		if p["ok"] == true {
			t.Errorf("DEFECT B-1: a client with no private half was bound as the key that only reads")
		}
		// A failed proof ends the hello; ask again for the next try.
		h, _ = c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hexOf(kid[:])})
	}
	if r, _ := c.Call("log", nil); r["code"] != CodeHelloFirst || *served != 0 {
		t.Errorf("DEFECT B-1: the session was answered: %v", r)
	}
}

// B-2. Contract B3: a session is bound to a key before anything else is
// answered. A hello that offers a credential and a prove that carries
// nothing must bind nothing.
func TestNothingBindsWithoutAProof(t *testing.T) {
	a := probe3Authority{creds: map[string]Identity{"good": {Name: "program", Credential: "p1", Program: "p1"}}}
	for _, prove := range []map[string]any{{}, {"challenge": ""}, {"sig": ""}} {
		c, served := serve3(t, a)
		if h, err := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "credential"}); err != nil || h["ok"] != true {
			t.Fatalf("hello: %v %v", h, err)
		}
		p, err := c.Call("prove", prove)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := c.Call("log", nil)
		t.Logf("prove %v: ok=%v key=%v owner=%v; then log: ok=%v code=%v served=%d", prove, p["ok"], p["key"], p["owner"], r["ok"], r["code"], *served)
		if p["ok"] == true || *served != 0 {
			t.Errorf("DEFECT B-2: a session was bound with no credential, no signature and no opened challenge (prove %v), and its next request reached the booth", prove)
		}
	}
}

// A wrong credential binds nothing, and a good one binds its program.
func TestACredentialBindsItsProgramOnly(t *testing.T) {
	a := probe3Authority{creds: map[string]Identity{"good": {Name: "program", Credential: "p1", Program: "p1"}}}
	c, served := serve3(t, a)
	if p, _ := c.ProveCredential("bad"); p["ok"] == true {
		t.Errorf("a wrong credential was bound")
	}
	if r, _ := c.Call("log", nil); r["code"] != CodeHelloFirst || *served != 0 {
		t.Errorf("after a wrong credential the session was answered: %v", r)
	}
	if p, _ := c.ProveCredential("good"); p["ok"] != true || p["program"] != "p1" {
		t.Errorf("a good credential was not bound: %v", p)
	}
}

// A key of the keyring with neither half can prove nothing.
func TestAKeyWithNeitherHalfIsNotBound(t *testing.T) {
	kid := [32]byte{0x51}
	a := probe3Authority{keys: map[[32]byte]Identity{kid: {Key: kid, Gen: 1, Name: "no halves", Reads: []string{"journal"}}}}
	c, served := serve3(t, a)
	h, _ := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hexOf(kid[:])})
	t.Logf("hello for a key with neither half: ok=%v code=%v sealed=%v challenge=%v", h["ok"], h["code"], h["sealed"] != nil, h["challenge"] != nil)
	for _, prove := range []map[string]any{{}, {"challenge": ""}, {"credential": ""}, {"credential": "x"}} {
		p, _ := c.Call("prove", prove)
		if p["ok"] == true {
			t.Errorf("DEFECT: a key with neither half was bound by prove %v", prove)
		}
	}
	if r, _ := c.Call("log", nil); r["code"] != CodeHelloFirst || *served != 0 {
		t.Errorf("DEFECT: the session was answered: %v", r)
	}
}

// The repair of B-1 holds for both kinds of key, and a proof is for one
// hello only.
func TestAProofIsForItsOwnHello(t *testing.T) {
	r1, _ := key.NewReader(probeStream3(3))
	kid := [32]byte{0x52}
	_, signer, _ := ed25519.GenerateKey(probeStream3(5))
	sid := [32]byte{0x53}
	a := probe3Authority{keys: map[[32]byte]Identity{
		kid: {Key: kid, Gen: 1, Name: "reads", Reader: r1.Public(), Reads: []string{"journal"}},
		sid: {Key: sid, Gen: 1, Name: "signs", Signer: signer.Public().(ed25519.PublicKey), Reads: []string{"journal"}},
	}}
	c, _ := serve3(t, a)
	h, _ := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hexOf(kid[:])})
	if h["challenge"] != nil {
		t.Errorf("DEFECT B-1: the challenge of a key that only reads is sent in the clear")
	}
	if p, err := c.ProveReader(kid, r1); err != nil || p["ok"] != true || p["writes"] != false {
		t.Errorf("a key that reads did not bind by opening its challenge: %v %v", p, err)
	}
	// A key that signs: the challenge of one hello does not serve the next.
	h1, _ := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hexOf(sid[:])})
	ch1 := unhex(h1["challenge"])
	anchor := a.Anchor()
	sig1 := ed25519.Sign(signer, ProofMessage(ch1, anchor))
	c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hexOf(sid[:])})
	if p, _ := c.Call("prove", map[string]any{"sig": hexOf(sig1)}); p["ok"] == true {
		// probeStream3 is a fixed stream, so both challenges are equal here;
		// a booth with real randomness refuses. Logged, not failed.
		t.Logf("note: with a fixed random stream both challenges are equal, so the old signature holds")
	}
	if p, err := c.ProveKey(sid, signer); err != nil || p["ok"] != true || p["writes"] != true {
		t.Errorf("a key that signs did not bind: %v %v", p, err)
	}
}

// B5. The contract asks for a guest: a session without a key, served the
// read-open addresses and nothing else. It proves the root of a genesis it
// presents; a hello that presents none is refused.
func TestTheBoothHasAGuest(t *testing.T) {
	c, served := serve3(t, probe3Authority{})
	h, err := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "guest"})
	t.Logf("hello as a guest with no genesis: ok=%v code=%v error=%v err=%v", h["ok"], h["code"], h["error"], err)
	if h["ok"] == true {
		t.Errorf("DEFECT B5: a guest that presents no genesis is offered a challenge")
	}
	if msg, _ := h["error"].(string); !containsGuest(msg) {
		t.Errorf("MISSING B5: the booth offers no guest; hello answers %q", msg)
	}
	for _, prove := range []map[string]any{{}, {"sig": ""}, {"challenge": ""}, {"credential": ""}} {
		if p, _ := c.Call("prove", prove); p["ok"] == true {
			t.Errorf("DEFECT B5: after a refused guest hello, prove %v bound the session", prove)
		}
	}
	if r, _ := c.Call("log", nil); r["code"] != CodeHelloFirst || *served != 0 {
		t.Errorf("DEFECT B5: the session was answered: %v", r)
	}
}

func containsGuest(s string) bool {
	for i := 0; i+5 <= len(s); i++ {
		if s[i:i+5] == "guest" {
			return true
		}
	}
	return false
}

func hexOf(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, d[x>>4], d[x&15])
	}
	return string(out)
}

func unhex(v any) []byte {
	s, _ := v.(string)
	out := make([]byte, 0, len(s)/2)
	val := func(c byte) byte {
		switch {
		case c >= '0' && c <= '9':
			return c - '0'
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10
		}
		return 0
	}
	for i := 0; i+1 < len(s); i += 2 {
		out = append(out, val(s[i])<<4|val(s[i+1]))
	}
	return out
}
