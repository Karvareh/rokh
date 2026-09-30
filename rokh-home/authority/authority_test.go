package authority

import (
	"testing"
)

var key = []byte("synthetic registry key, thirty-two")

func TestEachActionIsItsOwnRight(t *testing.T) {
	r := New()
	c, cred, err := r.Add("synthetic-reader", "cli", key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Grant(c.ID, Read, "آزمون/نوشته‌ها"); err != nil {
		t.Fatal(err)
	}
	got, ok := r.Authenticate(key, cred)
	if !ok || got.ID != c.ID {
		t.Fatal("the credential did not authenticate its program")
	}
	v := c.Version
	if d := r.Decide(c.ID, v, Read, "آزمون/نوشته‌ها/نمونه.md"); !d.Allowed || d.Grant == "" || d.Reason != Allowed {
		t.Fatalf("read inside scope: %+v", d)
	}
	for _, a := range []Action{Search, Bytes, Draft, Record, Export, Share, Ledger} {
		if d := r.Decide(c.ID, v, a, "آزمون/نوشته‌ها/نمونه.md"); d.Allowed || d.Reason != NoGrant {
			t.Fatalf("read implied %s: %+v", a, d)
		}
	}
}

func TestAScopeCoversSegmentsNotPrefixes(t *testing.T) {
	r := New()
	c, _, _ := r.Add("synthetic", "cli", key)
	r.Grant(c.ID, Bytes, "projects/alpha")
	for path, want := range map[string]bool{
		"projects/alpha":          true,
		"projects/alpha/readme":   true,
		"projects/alphabet":       false,
		"projects/alphabet/x":     false,
		"projects":                false,
		"projects/alpha/../omega": false,
		WholeHome:                 false,
	} {
		if d := r.Decide(c.ID, c.Version, Bytes, path); d.Allowed != want {
			t.Errorf("%q: allowed=%v reason=%s", path, d.Allowed, d.Reason)
		}
	}
}

func TestRevocationAndStaleSessions(t *testing.T) {
	r := New()
	c, cred, _ := r.Add("synthetic", "ai", key)
	g, _ := r.Grant(c.ID, Search, "research")
	session := c.Version
	if d := r.Decide(c.ID, session, Search, "research/q1"); !d.Allowed {
		t.Fatal("search denied before any change")
	}
	r.Grant(c.ID, Bytes, "research")
	if d := r.Decide(c.ID, session, Search, "research/q1"); d.Allowed || d.Reason != StaleSession {
		t.Fatalf("a session from before the change was honoured: %+v", d)
	}
	session = c.Version
	if err := r.Ungrant(c.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	if d := r.Decide(c.ID, c.Version, Search, "research/q1"); d.Allowed || d.Reason != NoGrant {
		t.Fatalf("an ungranted action: %+v", d)
	}
	if d := r.Decide(c.ID, session, Bytes, "research/q1"); d.Reason != StaleSession {
		t.Fatalf("an old session after ungrant: %+v", d)
	}
	if err := r.Revoke(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Authenticate(key, cred); ok {
		t.Fatal("a revoked program's credential still authenticates")
	}
	if d := r.Decide(c.ID, c.Version, Bytes, "research/q1"); d.Allowed || d.Reason != ConsumerRevoked {
		t.Fatalf("a revoked program: %+v", d)
	}
}

func TestAClaimedNameIsNotAnIdentity(t *testing.T) {
	r := New()
	a, credA, _ := r.Add("clerk", "clerk", key)
	b, _, _ := r.Add("catware", "catware", key)
	r.Grant(a.ID, Bytes, "clerk")
	r.Grant(b.ID, Bytes, "catware")
	got, ok := r.Authenticate(key, credA)
	if !ok || got.ID != a.ID {
		t.Fatal("credential A")
	}
	if d := r.Decide(b.ID, b.Version, Bytes, "clerk/notes"); d.Allowed {
		t.Fatal("a program reached another's scope by its identifier")
	}
	if _, ok := r.Authenticate(key, []byte("clerk")); ok {
		t.Fatal("a name authenticated as a credential")
	}
	if d := r.Decide("not-registered", 1, Bytes, "clerk"); d.Allowed || d.Reason != UnknownConsumer {
		t.Fatalf("an unknown program: %+v", d)
	}
	if _, _, err := r.Add("clerk", "impostor", key); err == nil {
		t.Fatal("a second program registered under a standing name")
	}
}

func TestTheRegistryChecksWhatItReadsBack(t *testing.T) {
	r := New()
	c, _, _ := r.Add("synthetic", "cli", key)
	r.Grant(c.ID, Read, WholeHome)
	if err := r.Check(); err != nil {
		t.Fatal(err)
	}
	c.Grants = append(c.Grants, Grant{ID: "x", Action: "rule-everything", Scope: "a"})
	if err := r.Check(); err == nil {
		t.Fatal("an unknown action passed the check")
	}
}
