package covenant

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

type world struct {
	t        *testing.T
	rootPriv ed25519.PrivateKey
	rootPub  ed25519.PublicKey
	gen      event.Signed
	anchor   frame.ID
	led      *ledger.Ledger
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
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
	return &world{t: t, rootPriv: priv, rootPub: pub, gen: g, anchor: g.ID, led: l}
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func (w *world) add(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID,
	addr, verb string, payload []byte) event.Signed {
	w.t.Helper()
	a := w.anchor
	s, err := event.Sign(event.Event{
		Carrier: &a, Authority: auth, Parents: parents,
		Address: addr, Verb: verb, Payload: payload,
	}, priv)
	if err != nil {
		w.t.Fatalf("sign %s/%s: %v", addr, verb, err)
	}
	st, err := w.led.Add(s.Raw)
	if err != nil {
		w.t.Fatalf("add %s/%s: %v", addr, verb, err)
	}
	if st != ledger.Accepted {
		w.t.Fatalf("%s/%s was %s, expected accepted", addr, verb, st)
	}
	return s
}

func (w *world) share(parents []frame.ID, subject ed25519.PublicKey, scope string) event.Signed {
	w.t.Helper()
	p, err := Share{Subject: subject, Scope: scope}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.add(w.rootPriv, nil, parents, Address, VerbShare, p)
}

func (w *world) unshare(parents []frame.ID, target frame.ID) event.Signed {
	w.t.Helper()
	p, err := Unshare{Target: target}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.add(w.rootPriv, nil, parents, Address, VerbUnshare, p)
}

// ---------- codec ----------

func TestShareCodec(t *testing.T) {
	sub, _ := newKey(t)
	seal := make([]byte, SealKeySize)
	seal[0] = 7
	s := Share{Subject: sub, Scope: "home/journal", SealTo: seal}
	b, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeShare(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Subject, sub) || back.Scope != s.Scope || !bytes.Equal(back.SealTo, seal) {
		t.Fatalf("share did not survive: %+v", back)
	}
	again, _ := s.Encode()
	if !bytes.Equal(b, again) {
		t.Fatal("share encoding is not deterministic")
	}
	// Absent scope means the whole ledger, and it must not be encoded as empty.
	whole, err := (Share{Subject: sub}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	w2, err := DecodeShare(whole)
	if err != nil || w2.Scope != "" {
		t.Fatal("whole-ledger scope did not round trip")
	}
	if _, err := DecodeShare(nil); err == nil {
		t.Fatal("empty share payload accepted")
	}
	if _, err := (Share{Subject: sub, SealTo: []byte{1, 2, 3}}).Encode(); err == nil {
		t.Fatal("bad seal key size accepted")
	}
}

func TestUnshareCodec(t *testing.T) {
	target := frame.Hash([]byte("some share"))
	b, err := Unshare{Target: target}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeUnshare(b)
	if err != nil || back.Target != target {
		t.Fatalf("unshare did not survive: %v %v", err, back)
	}
	if _, err := (Unshare{}).Encode(); err == nil {
		t.Fatal("unshare with no target accepted")
	}
}

// ---------- the rule that matters most ----------

// A write-delegate must not be able to disclose the ledger to itself.
//
// The address "peer" is ordinary, so a delegate with a broad write scope can
// author an event there and the ledger will accept it. The evaluator is what
// refuses to treat it as disclosure.
// Showing is an effect too, and it passes through the same gate: disclosure
// comes from the owner's authority, never from a right to write.
//
//	— T4.3, T7.3
func TestOnlyRootCanDisclose(t *testing.T) {
	w := newWorld(t)
	dPub, dPriv := newKey(t)
	peer, _ := newKey(t)

	// Root grants a wide write right, deliberately including the peer address.
	gp, err := event.Grant{Subject: dPub, Scope: ""}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	g := w.add(w.rootPriv, nil, []frame.ID{w.gen.ID}, event.AddressRoot, event.VerbGrant, gp)

	// The delegate writes a perfectly well-formed covenant naming itself.
	sp, err := Share{Subject: dPub}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	forged := w.add(dPriv, &g.ID, []frame.ID{g.ID}, Address, VerbShare, sp)

	// The ledger accepted the event: the delegate did have write authority.
	if w.led.State(forged.ID) != ledger.Accepted {
		t.Fatal("expected the ledger to accept the event itself")
	}
	// But it confers no disclosure.
	if live := ActiveAt(w.led); len(live) != 0 {
		t.Fatalf("a write-delegate's covenant was treated as disclosure: %+v", live)
	}
	if _, ok := Discloses(ActiveAt(w.led), dPub, "anything"); ok {
		t.Fatal("a delegate disclosed the ledger to itself")
	}

	// Root's own covenant does confer disclosure.
	real := w.share([]frame.ID{forged.ID}, peer, "home")
	live := ActiveAt(w.led)
	if len(live) != 1 || live[0].ID != real.ID {
		t.Fatalf("expected exactly root's covenant, got %+v", live)
	}
}

// ---------- causal evaluation ----------

// share -- e1 -- unshare -- e2   withdrawn on this branch
//
//	\--- e3              still live on the branch that never saw it
//	     merge           withdrawn from there on
func TestWithdrawalIsCausal(t *testing.T) {
	w := newWorld(t)
	peer, _ := newKey(t)

	sh := w.share([]frame.ID{w.gen.ID}, peer, "home")
	e1 := w.add(w.rootPriv, nil, []frame.ID{sh.ID}, "home/a", "note", []byte("1"))

	if live := ActiveAt(w.led, e1.ID); len(live) != 1 {
		t.Fatal("covenant not live before withdrawal")
	}

	un := w.unshare([]frame.ID{e1.ID}, sh.ID)
	e3 := w.add(w.rootPriv, nil, []frame.ID{e1.ID}, "home/b", "note", []byte("3"))

	if live := ActiveAt(w.led, un.ID); len(live) != 0 {
		t.Fatalf("covenant still live after withdrawal: %+v", live)
	}
	// The branch that never saw the withdrawal still has it, exactly as with
	// grants: a settled past does not change.
	if live := ActiveAt(w.led, e3.ID); len(live) != 1 {
		t.Fatal("covenant should still be live on the branch that never saw the withdrawal")
	}
	// e1 keeps its own answer forever.
	if live := ActiveAt(w.led, e1.ID); len(live) != 1 {
		t.Fatal("an earlier point changed its answer; monotonicity broken")
	}

	m := w.add(w.rootPriv, nil, []frame.ID{un.ID, e3.ID}, event.AddressRoot, event.VerbMerge, nil)
	if live := ActiveAt(w.led, m.ID); len(live) != 0 {
		t.Fatalf("covenant survived the merge: %+v", live)
	}
}

func TestScopeAndRecipient(t *testing.T) {
	w := newWorld(t)
	alice, _ := newKey(t)
	bob, _ := newKey(t)

	w.share([]frame.ID{w.gen.ID}, alice, "home/journal")
	live := ActiveAt(w.led)
	if len(live) != 1 {
		t.Fatal("expected one covenant")
	}

	cases := []struct {
		peer ed25519.PublicKey
		addr string
		want bool
	}{
		{alice, "home/journal", true},
		{alice, "home/journal/today", true},
		{alice, "home/journalism", false}, // component boundary, not string prefix
		{alice, "home", false},
		{alice, "work", false},
		{bob, "home/journal", false},
	}
	for _, c := range cases {
		if _, ok := Discloses(live, c.peer, c.addr); ok != c.want {
			t.Errorf("Discloses(%s) = %v, want %v", c.addr, ok, c.want)
		}
	}
}

func TestWholeLedgerScope(t *testing.T) {
	w := newWorld(t)
	peer, _ := newKey(t)
	w.share([]frame.ID{w.gen.ID}, peer, "")
	live := ActiveAt(w.led)
	for _, addr := range []string{"a", "home/journal/today", "anything/at/all"} {
		if _, ok := Discloses(live, peer, addr); !ok {
			t.Errorf("an empty scope should cover %q", addr)
		}
	}
}

// A covenant permits; it never transfers. Nothing in this package writes an
// event, opens anything, or runs on its own.
// "May write here" and "may read this" are two different things with two
// different instruments. A right to write never extends into a right to read.
//
//	— T7, T7.1, N4.8
func TestCovenantOnlyPermits(t *testing.T) {
	w := newWorld(t)
	peer, _ := newKey(t)
	before := w.led.Len()
	w.share([]frame.ID{w.gen.ID}, peer, "home")
	afterShare := w.led.Len()
	if afterShare != before+1 {
		t.Fatal("writing a covenant should add exactly one event")
	}
	// Evaluating it must add nothing at all.
	for i := 0; i < 5; i++ {
		ActiveAt(w.led)
		Discloses(ActiveAt(w.led), peer, "home/x")
	}
	if w.led.Len() != afterShare {
		t.Fatal("evaluating covenants changed the ledger")
	}
}

func TestMalformedCovenantGrantsNothing(t *testing.T) {
	w := newWorld(t)
	// Root writes a peer.share whose payload is not a share at all.
	w.add(w.rootPriv, nil, []frame.ID{w.gen.ID}, Address, VerbShare, []byte("not a payload"))
	if live := ActiveAt(w.led); len(live) != 0 {
		t.Fatal("a malformed covenant conferred disclosure")
	}
}
