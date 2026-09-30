package event

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"rokh/frame"
)

func key(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func genesis(t *testing.T, priv ed25519.PrivateKey) Signed {
	t.Helper()
	s, err := Sign(Event{Address: AddressRoot, Verb: VerbGenesis, Payload: []byte("first page")}, priv)
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	return s
}

func TestSignParseRoundTrip(t *testing.T) {
	pub, priv := key(t)
	g := genesis(t, priv)
	if !g.IsGenesis() || !bytes.Equal(g.Event.Author, pub) {
		t.Fatal("genesis built wrong")
	}
	anchor := g.ID

	e := Event{
		Carrier: &anchor,
		Parents: []frame.ID{g.ID},
		Address: "home/journal/today",
		Verb:    "note",
		Payload: []byte("hello"),
		Attest:  []Attestation{{Oracle: "clock", Claim: []byte("2026-08-28T00:00:00Z")}},
	}
	s, err := Sign(e, priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	back, err := Parse(s.Raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.ID != s.ID || !bytes.Equal(back.Raw, s.Raw) {
		t.Fatal("round trip is not byte-identical")
	}
	if back.Event.Address != e.Address || back.Event.Verb != e.Verb {
		t.Fatal("fields did not survive the round trip")
	}
	again, err := Sign(e, priv)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != s.ID {
		t.Fatal("signing twice gave a different id; determinism broken")
	}
}

// Flip every byte of the frame in turn; none may pass.
// Change one byte and the name is a different name. That is the whole of
// what the hash promises: equality, and a test for a guess.
//
//	— T3.2, N4.2
func TestTamperIsAlwaysCaught(t *testing.T) {
	_, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID
	s, err := Sign(Event{
		Carrier: &anchor, Parents: []frame.ID{g.ID},
		Address: "a", Verb: "note", Payload: []byte("original text"),
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Raw {
		bad := append([]byte(nil), s.Raw...)
		bad[i] ^= 0x01
		if _, err := Parse(bad); err == nil {
			t.Fatalf("byte %d corrupted and the event was still accepted", i)
		}
	}
}

// Every line of the ledger is somebody's explicit act: the author is the key
// that signed, and no other key can stand in its place.
//
//	— T4.2
func TestForeignKeyCannotClaimAuthorship(t *testing.T) {
	_, priv := key(t)
	other, _ := key(t)
	g := genesis(t, priv)
	anchor := g.ID
	if _, err := Sign(Event{
		Carrier: &anchor, Parents: []frame.ID{g.ID},
		Author: other, Address: "a", Verb: "note",
	}, priv); err == nil {
		t.Fatal("signed with a key that does not match the author")
	}
}

// Invisible runes are written as ASCII escapes so this file does not itself
// contain the ambiguity it rejects.
//
// The one non-ASCII literal below is Unicode test data, not prose: U+0622 is
// a precomposed letter whose decomposed form must be refused, which is the
// whole point of the Arabic-block branch in badRune.
func TestNameRules(t *testing.T) {
	bad := map[string]string{
		"decomposed NFD":      "آlef",
		"zero width non join": "jour‌nal",
		"bidi override":       "a‮b",
		"ascii control":       "ab",
		"zero width space":    "a​b",
		"right to left mark":  "a‏b",
		"bidi isolate":        "a⁦b",
		"combining acute":     "é",
		"empty component":     "a//b",
		"dot component":       "a/./b",
		"edge space":          "a/ b",
		"empty":               "",
	}
	for name, v := range bad {
		if err := ValidAddress(v); err == nil {
			t.Errorf("%s: accepted, should have been rejected (%q)", name, v)
		}
	}
	// Precomposed forms must pass: U+0622, U+00E9.
	good := []string{"a", "آlef", "café", "home/journal/today", "star/planet/moon", "a-b_c.d"}
	for _, v := range good {
		if err := ValidAddress(v); err != nil {
			t.Errorf("valid address rejected %q: %v", v, err)
		}
	}
	if err := ValidVerb("rokh.unknown"); err == nil {
		t.Fatal("unknown reserved verb accepted")
	}
	if err := ValidVerb("note/thing"); err == nil {
		t.Fatal("\"/\" in verb accepted")
	}
	for _, v := range []string{VerbGenesis, VerbGrant, VerbRevoke, VerbMerge, "note"} {
		if err := ValidVerb(v); err != nil {
			t.Errorf("valid verb rejected %q: %v", v, err)
		}
	}
}

// An event is a recorded happening: what happened, by whose authority, and
// with what attestation.
//
//	— T1.1
func TestShapeRules(t *testing.T) {
	_, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID

	cases := map[string]Event{
		"second genesis":        {Carrier: &anchor, Parents: []frame.ID{g.ID}, Address: AddressRoot, Verb: VerbGenesis},
		"non genesis no parent": {Carrier: &anchor, Address: "a", Verb: "note"},
		"merge with one parent": {Carrier: &anchor, Parents: []frame.ID{g.ID}, Address: AddressRoot, Verb: VerbMerge},
		"reserved verb elsewhere": {Carrier: &anchor, Parents: []frame.ID{g.ID},
			Address: "a", Verb: VerbMerge},
		"genesis with parents":   {Parents: []frame.ID{g.ID}, Address: AddressRoot, Verb: VerbGenesis},
		"no carrier not genesis": {Address: "a", Verb: "note"},
	}
	for name, e := range cases {
		if _, err := Sign(e, priv); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The address "rokh" is core territory, in both directions.
func TestCoreAddressIsReserved(t *testing.T) {
	_, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID

	for _, addr := range []string{AddressRoot, AddressRoot + "/anything"} {
		if _, err := Sign(Event{Carrier: &anchor, Parents: []frame.ID{g.ID},
			Address: addr, Verb: "note"}, priv); err == nil {
			t.Errorf("user verb accepted at address %q", addr)
		}
	}
	if _, err := Sign(Event{Carrier: &anchor, Parents: []frame.ID{g.ID},
		Address: "some/where", Verb: VerbRevoke,
		Payload: mustRevoke(t, g.ID)}, priv); err == nil {
		t.Error("reserved verb accepted outside the core address")
	}
}

func mustRevoke(t *testing.T, id frame.ID) []byte {
	t.Helper()
	b, err := Revoke{Target: id}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The inline payload is deliberately small: bulk content does not live in an
// event.
// The payload ceiling is small so the ledger passes through the narrowest
// road. The number is a choice of the design, not a limit of the world.
//
//	— T3.4, N4.5
func TestInlinePayloadIsBounded(t *testing.T) {
	_, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID
	if _, err := Sign(Event{Carrier: &anchor, Parents: []frame.ID{g.ID},
		Address: "a", Verb: "note", Payload: make([]byte, MaxPayload+1)}, priv); err == nil {
		t.Fatal("payload over the limit accepted")
	}
}

// Every event names its parents, and names them in one canonical order.
//
//	— T5.1, N4.3
func TestParentsAreSortedOnTheWire(t *testing.T) {
	_, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID
	a, _ := Sign(Event{Carrier: &anchor, Parents: []frame.ID{g.ID}, Address: "a", Verb: "note", Payload: []byte("1")}, priv)
	b, _ := Sign(Event{Carrier: &anchor, Parents: []frame.ID{g.ID}, Address: "a", Verb: "note", Payload: []byte("2")}, priv)

	m, err := Sign(Event{Carrier: &anchor, Parents: []frame.ID{a.ID, b.ID}, Address: AddressRoot, Verb: VerbMerge}, priv)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := Sign(Event{Carrier: &anchor, Parents: []frame.ID{b.ID, a.ID}, Address: AddressRoot, Verb: VerbMerge}, priv)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != m2.ID {
		t.Fatal("parent input order changed the id")
	}
	f, _, _ := frame.ParseHead(m.Raw)
	v, _ := f.Fields.Get(TagParents)
	var p0, p1 frame.ID
	copy(p0[:], v[:32])
	copy(p1[:], v[32:])
	if p0.Compare(p1) >= 0 {
		t.Fatal("parents are not ascending on the wire")
	}
}

func TestGrantCodecAndScope(t *testing.T) {
	sub, _ := key(t)
	g := Grant{Subject: sub, Scope: "home/journal", Verbs: []string{"note", "read"}, CanDelegate: true}
	b, err := g.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeGrant(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.Scope != g.Scope || !back.CanDelegate || len(back.Verbs) != 2 {
		t.Fatalf("grant did not survive: %+v", back)
	}
	g2 := Grant{Subject: sub, Scope: "home/journal", Verbs: []string{"read", "note"}, CanDelegate: true}
	b2, _ := g2.Encode()
	if !bytes.Equal(b, b2) {
		t.Fatal("verb input order changed the grant bytes")
	}

	// Reserved verbs never appear in a verb list.
	if _, err := (Grant{Subject: sub, Verbs: []string{VerbGrant}}).Encode(); err == nil {
		t.Fatal("reserved verb accepted in a grant list")
	}

	if ScopeCovers("a", "ab") {
		t.Fatal("scope \"a\" must not cover \"ab\"")
	}
	if !ScopeCovers("a", "a/b") || !ScopeCovers("a", "a") || !ScopeCovers("", "anything") {
		t.Fatal("scope coverage is wrong")
	}
	if ScopeWithin("", "a") {
		t.Fatal("whole ledger must not count as inside a bounded scope")
	}
	if !ScopeWithin("a/b", "a") {
		t.Fatal("a real subset was rejected")
	}

	open := Grant{Subject: sub}
	if open.Allows("anything", VerbGrant) || open.Allows("anything", VerbRevoke) {
		t.Fatal("an open grant must not permit a reserved verb")
	}
	if !open.Allows("anything", "note") {
		t.Fatal("an open grant must permit an ordinary verb")
	}
	if VerbsWithin(nil, []string{"note"}) {
		t.Fatal("\"any verb\" must not count as inside a fixed list")
	}
	if !VerbsWithin([]string{"note"}, nil) {
		t.Fatal("an ordinary verb should be inside \"any non-reserved verb\"")
	}
}

func TestAttestationsAreASortedSet(t *testing.T) {
	_, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID
	mk := func(as []Attestation) Signed {
		s, err := Sign(Event{Carrier: &anchor, Parents: []frame.ID{g.ID},
			Address: "a", Verb: "note", Attest: as}, priv)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a := mk([]Attestation{{"b", []byte("2")}, {"a", []byte("1")}})
	b := mk([]Attestation{{"a", []byte("1")}, {"b", []byte("2")}})
	if a.ID != b.ID {
		t.Fatal("attestation order changed the id")
	}
	if _, err := Sign(Event{Carrier: &anchor, Parents: []frame.ID{g.ID}, Address: "a", Verb: "note",
		Attest: []Attestation{{"a", []byte("1")}, {"a", []byte("1")}}}, priv); err == nil {
		t.Fatal("duplicate attestation accepted")
	}
}
