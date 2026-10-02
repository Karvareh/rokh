//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
)

type heldKey struct {
	priv ed25519.PrivateKey
	auth frame.ID
}

// programKeys is a program's own store of the keys it holds for others.
type programKeys map[string]heldKey

func (k programKeys) Key(name string) (ed25519.PrivateKey, *frame.ID, bool) {
	v, ok := k[name]
	if !ok {
		return nil, nil, false
	}
	a := v.auth
	return append(ed25519.PrivateKey(nil), v.priv...), &a, true
}

func (k programKeys) Names() []string {
	var out []string
	for n := range k {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// A door embedded in a program signs with the keys that program holds for the
// programs it serves, by name, and with nothing else: not a keyring name, and
// never the root. The ledger still judges the grant each key signs under.
//
//	— T11.10, N4.8
func TestADoorCanTakeItsKeysFromTheProgramThatEmbedsIt(t *testing.T) {
	f := newFixture(t)
	pub, priv, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := event.Grant{Subject: pub, Scope: "home/items", Verbs: []string{"content.put"}}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	// The grant is written on the main branch, so what the program then writes
	// there has the grant in its causal past; authority is judged nowhere else.
	owner := New(f.car, f.led, Options{AllowSign: true, Dir: f.dir})
	g := mustOK(t, ask(t, owner, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbGrant,
		"payload": base64.StdEncoding.EncodeToString(payload)}), "grant")
	grantID, err := frame.ParseID(g["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	grant := struct{ ID frame.ID }{grantID}

	c, err := carrier.Open(carrier.FS{Root: f.dir}, "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	keys := programKeys{"consumer-a": heldKey{priv: priv, auth: grant.ID}}
	prog := New(c, auditReload(t, c), Options{AllowSign: true, NoRoot: true, Dir: f.dir, Keys: keys})

	r := mustOK(t, ask(t, prog, map[string]any{"op": "write", "address": "home/items/x", "verb": "content.put",
		"message": "synthetic", "key": "consumer-a"}), "write with a held key")
	if r["record"] != "recorded" {
		t.Fatalf("record: %v", r)
	}
	got := mustOK(t, ask(t, prog, map[string]any{"op": "get", "id": r["id"]}), "get")
	raw, _ := hex.DecodeString(got["raw"].(string))
	e, err := event.Parse(raw)
	if err != nil || hex.EncodeToString(e.Event.Author) != hex.EncodeToString(pub) {
		t.Fatalf("the event was not signed by the held key: %v", err)
	}

	// The carrier's keyring holds the root; this door does not reach for it,
	// and a name it does not hold is unknown here.
	expectNotRecorded(t, ask(t, prog, map[string]any{"op": "write", "address": "home/items/x", "message": "x"}), "root_refused")
	expectNotRecorded(t, ask(t, prog, map[string]any{"op": "write", "address": "home/items/x", "message": "x", "key": "someone-else"}), "key_unknown")
	// Outside the grant the ledger refuses, whatever the door holds.
	expectNotRecorded(t, ask(t, prog, map[string]any{"op": "write", "address": "elsewhere", "verb": "content.put",
		"message": "x", "key": "consumer-a"}), "not_accepted")

	caps := mustOK(t, ask(t, prog, map[string]any{"op": "capabilities"}), "capabilities")
	b, _ := json.Marshal(caps)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	signers := m["signers"].([]any)
	if len(signers) != 2 {
		t.Fatalf("signers: %v", signers)
	}
	held := signers[1].(map[string]any)
	if held["name"] != "consumer-a" || held["public"] != hex.EncodeToString(pub) ||
		held["authority"] != grant.ID.String() || held["standing"] != true {
		t.Fatalf("held signer: %v", held)
	}
	if strings.Contains(string(b), hex.EncodeToString(priv)) || strings.Contains(string(b), hex.EncodeToString(priv.Seed())) {
		t.Fatal("capabilities carries a held private key")
	}
}
