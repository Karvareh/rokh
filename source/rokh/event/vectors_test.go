package event

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/frame"
)

var update = flag.Bool("update", false, "rewrite the conformance vectors")

// Conformance vectors.
//
// This file is a contract, not incidental output. Any other implementation of
// Rokh, in any language, must produce exactly these bytes and these ids from
// the same inputs, and must read the same fields back out of these bytes.
//
// Keys come from fixed seeds and Ed25519 signing is deterministic (RFC 8032),
// so the output is identical on every machine and every version.
type vector struct {
	Name    string `json:"name"`
	Raw     string `json:"bytes"`
	ID      string `json:"id"`
	Author  string `json:"author"`
	Address string `json:"address"`
	Verb    string `json:"verb"`
}

func seedKey(b byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = b
	}
	return ed25519.NewKeyFromSeed(seed)
}

func buildVectors(t *testing.T) []vector {
	t.Helper()
	root := seedKey(0x01)
	dele := seedKey(0x02)
	delePub := dele.Public().(ed25519.PublicKey)

	sign := func(name string, e Event, k ed25519.PrivateKey) (vector, Signed) {
		s, err := Sign(e, k)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return vector{
			Name:    name,
			Raw:     hex.EncodeToString(s.Raw),
			ID:      s.ID.String(),
			Author:  hex.EncodeToString(s.Event.Author),
			Address: s.Event.Address,
			Verb:    s.Event.Verb,
		}, s
	}

	var out []vector

	v, gen := sign("genesis", Event{
		Address: AddressRoot, Verb: VerbGenesis, Payload: []byte("first page"),
	}, root)
	out = append(out, v)
	anchor := gen.ID

	v, plain := sign("plain write", Event{
		Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: "home/journal/today", Verb: "note", Payload: []byte("hello"),
	}, root)
	out = append(out, v)

	v, withAtt := sign("with testimony", Event{
		Carrier: &anchor, Parents: []frame.ID{plain.ID},
		Address: "home/journal/tomorrow", Verb: "note", Payload: []byte("two witnesses"),
		Attest: []Attestation{
			{Oracle: "clock", Claim: []byte("2026-08-28T00:00:00Z")},
			{Oracle: "host", Claim: []byte("example linux/arm64")},
		},
	}, root)
	out = append(out, v)

	grantPayload, err := Grant{
		Subject: delePub, Scope: "home/journal", Verbs: []string{"note", "read"},
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	v, grant := sign("grant", Event{
		Carrier: &anchor, Parents: []frame.ID{withAtt.ID},
		Address: AddressRoot, Verb: VerbGrant, Payload: grantPayload,
	}, root)
	out = append(out, v)

	gid := grant.ID
	v, byDele := sign("delegated write", Event{
		Carrier: &anchor, Authority: &gid, Parents: []frame.ID{grant.ID},
		Address: "home/journal/third", Verb: "note", Payload: []byte("written by the delegate"),
	}, dele)
	out = append(out, v)

	revokePayload, err := Revoke{Target: grant.ID}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	v, revoke := sign("revoke", Event{
		Carrier: &anchor, Parents: []frame.ID{byDele.ID},
		Address: AddressRoot, Verb: VerbRevoke, Payload: revokePayload,
	}, root)
	out = append(out, v)

	v, _ = sign("merge", Event{
		Carrier: &anchor, Parents: []frame.ID{revoke.ID, plain.ID},
		Address: AddressRoot, Verb: VerbMerge,
	}, root)
	out = append(out, v)

	v, _ = sign("empty payload", Event{
		Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: "a", Verb: "mark",
	}, root)
	out = append(out, v)

	// Persian payload, with ZWNJ written as an escape so this file states
	// exactly what it is testing. A payload is prose: it is preserved byte for
	// byte and nothing normalizes or repairs it.
	v, _ = sign("persian payload", Event{
		Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: "home/journal/today", Verb: "note",
		Payload: []byte(persianSample),
	}, root)
	out = append(out, v)

	// The same text under an ASCII address, to show the payload rule is
	// independent of the identifier rule.
	v, _ = sign("persian payload ascii address", Event{
		Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: "notes/fa", Verb: "note",
		Payload: []byte(persianSample + "\n" + persianZWNJOnly),
	}, root)
	out = append(out, v)

	return out
}

// persianSample is ordinary Persian prose containing ZWNJ (U+200C) inside
// words, which is how Persian is actually written.
const persianSample = "\u0637\u0631\u062d\u0650 \u06a9\u0633\u0628\u200c\u0648\u06a9\u0627\u0631: " +
	"\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u06cc\u0645 \u0646\u06cc\u0645\u200c\u0641\u0627\u0635\u0644\u0647\u200c\u0647\u0627 " +
	"\u062f\u0633\u062a\u200c\u0646\u062e\u0648\u0631\u062f\u0647 \u0628\u0645\u0627\u0646\u0646\u062f."

// persianZWNJOnly is a short string whose only unusual character is ZWNJ.
const persianZWNJOnly = "\u0647\u0645\u200c\u06af\u0631\u0647"

func vectorPath() string { return filepath.Join("testdata", "vectors.json") }

func TestConformanceVectors(t *testing.T) {
	got := buildVectors(t)

	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		b, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorPath(), append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d vectors", len(got))
		return
	}

	raw, err := os.ReadFile(vectorPath())
	if err != nil {
		t.Fatalf("vectors unreadable (regenerate with -update): %v", err)
	}
	var want []vector
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got) {
		t.Fatalf("vector count changed: %d != %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("vector %q changed:\n  was: %s\n  now: %s",
				want[i].Name, want[i].ID, got[i].ID)
		}
	}

	// And every vector must also pass the parser and round-trip byte for byte.
	for _, w := range want {
		b, err := hex.DecodeString(w.Raw)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Parse(b)
		if err != nil {
			t.Errorf("vector %q did not parse: %v", w.Name, err)
			continue
		}
		if s.ID.String() != w.ID {
			t.Errorf("vector %q: id mismatch", w.Name)
		}
		if s.Event.Address != w.Address || s.Event.Verb != w.Verb {
			t.Errorf("vector %q: field mismatch", w.Name)
		}
	}
}

// The identifier rule and the payload rule are deliberately different, and
// this states the boundary rather than leaving it implied.
//
//	payload  any valid UTF-8, preserved byte for byte. Persian, ZWNJ, anything.
//	address  combining marks and format or bidirectional characters rejected.
//
// No claim of universal Persian-identifier support is made: precomposed
// letters pass, decomposed forms and ZWNJ do not. Changing that is a later
// ruling.
func TestPersianPayloadYesIdentifierNo(t *testing.T) {
	_, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID

	if !strings.Contains(persianSample, "\u200c") {
		t.Fatal("the sample was expected to contain ZWNJ")
	}

	// Payload: accepted, and preserved exactly.
	s, err := Sign(Event{
		Carrier: &anchor, Parents: []frame.ID{g.ID},
		Address: "notes/fa", Verb: "note", Payload: []byte(persianSample),
	}, priv)
	if err != nil {
		t.Fatalf("Persian payload was rejected: %v", err)
	}
	back, err := Parse(s.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(back.Event.Payload) != persianSample {
		t.Fatal("Persian payload was altered")
	}
	if !bytes.Equal(back.Event.Payload, []byte(persianSample)) {
		t.Fatal("Persian payload is not byte-identical")
	}

	// Address: ZWNJ rejected, because an identifier must mean one thing.
	if err := ValidAddress("\u0647\u0645\u200c\u06af\u0631\u0647"); err == nil {
		t.Fatal("ZWNJ accepted in an address")
	}
	// Precomposed Persian letters in an address are fine.
	if err := ValidAddress("\u0622\u0628/\u067e\u0631\u0648\u0646\u062f\u0647"); err != nil {
		t.Fatalf("precomposed Persian address rejected: %v", err)
	}
}
