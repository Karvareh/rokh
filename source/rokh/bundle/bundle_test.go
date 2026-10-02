package bundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"rokh/covenant"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

type world struct {
	t      *testing.T
	priv   ed25519.PrivateKey
	gen    event.Signed
	anchor frame.ID
	led    *ledger.Ledger
	head   frame.ID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g, err := event.Sign(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("genesis"),
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.New(g.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return &world{t: t, priv: priv, gen: g, anchor: g.ID, led: l, head: g.ID}
}

func newKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

// write appends onto the current head, so the graph is a simple chain.
func (w *world) write(addr, verb string, payload []byte) frame.ID {
	w.t.Helper()
	a := w.anchor
	s, err := event.Sign(event.Event{
		Carrier: &a, Parents: []frame.ID{w.head},
		Address: addr, Verb: verb, Payload: payload,
	}, w.priv)
	if err != nil {
		w.t.Fatalf("sign %s: %v", addr, err)
	}
	st, err := w.led.Add(s.Raw)
	if err != nil || st != ledger.Accepted {
		w.t.Fatalf("add %s: %v %s", addr, err, st)
	}
	w.head = s.ID
	return s.ID
}

func (w *world) share(peer ed25519.PublicKey, scope string) frame.ID {
	w.t.Helper()
	p, err := covenant.Share{Subject: peer, Scope: scope}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.write(covenant.Address, covenant.VerbShare, p)
}

func has(ids []frame.ID, id frame.ID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Nothing crosses without a covenant. This is the default, and it is the whole
// posture: disclosure is a recorded decision, never an assumption.
// What may be disclosed is decided when the bundle is sealed, not when it is
// sent. The courier holds no key, so what must not travel never reaches it.
//
//	— T7.4, N4.9
func TestNothingCrossesWithoutACovenant(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	w.write("home/journal/one", "note", []byte("x"))

	_, err := Disclosable(w.led, peer, frame.Zero, "", false)
	if !errors.Is(err, ErrNoCovenant) {
		t.Fatalf("expected ErrNoCovenant, got %v", err)
	}
}

func TestScopeLimitsWhatCrosses(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	one := w.write("home/journal/one", "note", []byte("1"))
	secret := w.write("work/secret", "note", []byte("private"))
	w.share(peer, "home/journal")
	two := w.write("home/journal/two", "note", []byte("2"))
	near := w.write("home/journalism", "note", []byte("not the same scope"))

	sel, err := Disclosable(w.led, peer, frame.Zero, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !has(sel.IDs, one) || !has(sel.IDs, two) {
		t.Fatal("in-scope events did not cross")
	}
	if has(sel.IDs, secret) {
		t.Fatal("an out-of-scope event crossed")
	}
	if has(sel.IDs, near) {
		t.Fatal("scope matched on a string prefix instead of a component boundary")
	}
	if has(sel.IDs, w.gen.ID) {
		t.Fatal("genesis crossed, but no covenant covers the core address")
	}
}

// Disclosure is outward and one-way. It says nothing about what the other
// side sends: arriving bytes are always weighed by the receiver's ledger.
//
//	— T7.2
func TestUnknownPeerGetsNothing(t *testing.T) {
	w := newWorld(t)
	alice := newKey(t)
	bob := newKey(t)
	w.write("home/journal/one", "note", []byte("1"))
	w.share(alice, "home/journal")

	if _, err := Disclosable(w.led, bob, frame.Zero, "", false); !errors.Is(err, ErrNoCovenant) {
		t.Fatalf("a peer with no covenant received something: %v", err)
	}
}

// A narrower scope than the covenant narrows further; it never widens.
func TestExplicitScopeOnlyNarrows(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	w.share(peer, "") // whole ledger
	a := w.write("home/a", "note", []byte("a"))
	b := w.write("work/b", "note", []byte("b"))

	all, err := Disclosable(w.led, peer, frame.Zero, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !has(all.IDs, a) || !has(all.IDs, b) {
		t.Fatal("a whole-ledger covenant did not cover everything")
	}

	narrow, err := Disclosable(w.led, peer, frame.Zero, "home", false)
	if err != nil {
		t.Fatal(err)
	}
	if !has(narrow.IDs, a) || has(narrow.IDs, b) {
		t.Fatal("an explicit scope did not narrow the selection")
	}
}

// "after" means the receiver already has that head's causal past.
func TestAfterExcludesWhatIsAlreadyHeld(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	w.share(peer, "home")
	first := w.write("home/one", "note", []byte("1"))
	second := w.write("home/two", "note", []byte("2"))

	sel, err := Disclosable(w.led, peer, first, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if has(sel.IDs, first) {
		t.Fatal("an event the receiver already has was resent")
	}
	if !has(sel.IDs, second) {
		t.Fatal("a newer event was not offered")
	}
}

// The unresolved tension, made visible rather than hidden: a scope narrower
// than the authority chain cannot be verified on its own.
// Withholding a past the receiver needs suspends the judgement — it does not
// reject it.
//
//	— T7.6
func TestMissingAncestorsAreReported(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	w.share(peer, "home/journal")
	one := w.write("home/journal/one", "note", []byte("1"))

	sel, err := Disclosable(w.led, peer, frame.Zero, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !has(sel.IDs, one) {
		t.Fatal("the in-scope event did not cross")
	}
	if len(sel.Missing) == 0 {
		t.Fatal("uncovered ancestors were not reported; the receiver would be left guessing")
	}
	t.Logf("%d event(s) may cross, %d ancestor(s) are not covered", len(sel.IDs), len(sel.Missing))

	// With ancestry, nothing is missing - but more is disclosed than the
	// covenant names, which is why it is off by default.
	full, err := Disclosable(w.led, peer, frame.Zero, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Missing) != 0 {
		t.Fatalf("with ancestry, nothing should be missing; %d were", len(full.Missing))
	}
	if len(full.IDs) <= len(sel.IDs) {
		t.Fatal("with ancestry the selection should be strictly larger")
	}
	if !has(full.IDs, w.gen.ID) {
		t.Fatal("ancestry should have pulled in genesis")
	}
}

// Withdrawing a covenant stops future disclosure.
func TestWithdrawalStopsDisclosure(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	sh := w.share(peer, "home")
	w.write("home/one", "note", []byte("1"))

	if _, err := Disclosable(w.led, peer, frame.Zero, "", false); err != nil {
		t.Fatalf("disclosure should work before withdrawal: %v", err)
	}
	p, err := covenant.Unshare{Target: sh}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	w.write(covenant.Address, covenant.VerbUnshare, p)

	if _, err := Disclosable(w.led, peer, frame.Zero, "", false); !errors.Is(err, ErrNoCovenant) {
		t.Fatalf("disclosure survived withdrawal: %v", err)
	}
}

// Selecting is a read. It permits; it does not transfer, and it changes
// nothing.
func TestSelectingChangesNothing(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	w.share(peer, "home")
	w.write("home/one", "note", []byte("1"))
	before := w.led.Len()
	for i := 0; i < 5; i++ {
		if _, err := Disclosable(w.led, peer, frame.Zero, "", false); err != nil {
			t.Fatal(err)
		}
	}
	if w.led.Len() != before {
		t.Fatal("selecting a bundle changed the ledger")
	}
}

func TestBadPeerKey(t *testing.T) {
	w := newWorld(t)
	if _, err := Disclosable(w.led, []byte{1, 2, 3}, frame.Zero, "", false); err == nil {
		t.Fatal("a malformed peer key was accepted")
	}
}
