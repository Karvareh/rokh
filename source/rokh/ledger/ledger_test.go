package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	mrand "math/rand"
	"testing"

	"rokh/event"
	"rokh/frame"
)

// ---------- scaffolding ----------

type world struct {
	t        *testing.T
	rootPriv ed25519.PrivateKey
	rootPub  ed25519.PublicKey
	gen      event.Signed
	anchor   frame.ID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g, err := event.Sign(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte("genesis")}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return &world{t: t, rootPriv: priv, rootPub: pub, gen: g, anchor: g.ID}
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func (w *world) write(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID,
	addr, verb string, payload []byte) event.Signed {
	w.t.Helper()
	a := w.anchor
	s, err := event.Sign(event.Event{
		Carrier: &a, Authority: auth, Parents: parents,
		Address: addr, Verb: verb, Payload: payload,
	}, priv)
	if err != nil {
		w.t.Fatalf("sign (%s/%s): %v", addr, verb, err)
	}
	return s
}

func (w *world) grant(parents []frame.ID, subject ed25519.PublicKey,
	scope string, verbs []string, canDeleg bool) event.Signed {
	w.t.Helper()
	p, err := event.Grant{Subject: subject, Scope: scope, Verbs: verbs, CanDelegate: canDeleg}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.write(w.rootPriv, nil, parents, event.AddressRoot, event.VerbGrant, p)
}

func (w *world) revoke(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID, target frame.ID) event.Signed {
	w.t.Helper()
	p, err := event.Revoke{Target: target}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.write(priv, auth, parents, event.AddressRoot, event.VerbRevoke, p)
}

func (w *world) ledger() *Ledger {
	w.t.Helper()
	l, err := New(w.gen.Raw)
	if err != nil {
		w.t.Fatal(err)
	}
	return l
}

func must(t *testing.T, l *Ledger, s event.Signed, want State, why string) {
	t.Helper()
	got, err := l.Add(s.Raw)
	if err != nil {
		t.Fatalf("%s: add failed: %v", why, err)
	}
	if got != want {
		t.Fatalf("%s: verdict %s, want %s", why, got, want)
	}
}

func mustGrant(t *testing.T, sub ed25519.PublicKey, scope string, verbs []string, cd bool) []byte {
	t.Helper()
	b, err := event.Grant{Subject: sub, Scope: scope, Verbs: verbs, CanDelegate: cd}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ---------- basics ----------

// Granting is an event and so is revoking.
//
//	— T6
func TestGenesisAndDelegatedWrite(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	if l.State(w.gen.ID) != Accepted {
		t.Fatal("genesis not accepted")
	}

	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "home", nil, false)
	must(t, l, gr, Accepted, "grant by root")

	ok := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "home/journal", "note", []byte("hello"))
	must(t, l, ok, Accepted, "write inside scope")

	out := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "outside", "note", nil)
	must(t, l, out, Rejected, "write outside scope")

	naked := w.write(kPriv, nil, []frame.ID{gr.ID}, "home/journal", "note", nil)
	must(t, l, naked, Rejected, "write with no authority")

	open := w.grant([]frame.ID{gr.ID}, kPub, "", nil, false)
	must(t, l, open, Accepted, "open grant")
	esc := w.write(kPriv, &open.ID, []frame.ID{open.ID}, event.AddressRoot, event.VerbGrant,
		mustGrant(t, kPub, "", nil, false))
	must(t, l, esc, Rejected, "sub-delegation without permission")
}

// One ledger, one anchor, one owner. An event anchored elsewhere is not
// refused for being bad — it is refused for not belonging here.
//
//	— T8.4, N-Axiom1
func TestRootMustOmitAuthorityAndStrangersCannotForgeCarrier(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, _ := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	must(t, l, gr, Accepted, "grant")

	bad := w.write(w.rootPriv, &gr.ID, []frame.ID{gr.ID}, "a", "note", nil)
	must(t, l, bad, Rejected, "root with explicit authority")

	other := newWorld(t)
	foreign := other.write(other.rootPriv, nil, []frame.ID{other.gen.ID}, "a", "note", nil)
	must(t, l, foreign, Rejected, "event from another ledger")

	must(t, l, other.gen, Rejected, "second genesis")
}

// ---------- the central knot: causal revocation ----------
//
//	e1 -- rv -- e2   branch that saw the revocation: e2 rejected
//	 \--- e3         branch that did not:            e3 accepted
//	      m(rv,e3)   after the merge:                e4 rejected
//
// Authority is judged only in the event's own causal past. What was allowed
// at the moment of writing is not undone by a later revocation, and
// revocation does not unsee.
//
//	— T6.2, T6.3, N6.2, N-Axiom4
func TestRevocationIsCausalNotGlobal(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)

	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	must(t, l, gr, Accepted, "grant")

	e1 := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "a", "note", []byte("before the revocation"))
	must(t, l, e1, Accepted, "write before revocation")

	rv := w.revoke(w.rootPriv, nil, []frame.ID{e1.ID}, gr.ID)
	must(t, l, rv, Accepted, "revoke")

	// e1 must stay accepted; that is monotonicity itself.
	if l.State(e1.ID) != Accepted {
		t.Fatal("an earlier event was rejected when the revocation arrived; monotonicity broken")
	}

	e2 := w.write(kPriv, &gr.ID, []frame.ID{rv.ID}, "a", "note", []byte("after seeing it"))
	must(t, l, e2, Rejected, "write after revocation on the same branch")

	e3 := w.write(kPriv, &gr.ID, []frame.ID{e1.ID}, "a", "note", []byte("branch that did not see it"))
	must(t, l, e3, Accepted, "write on the unaware branch must be accepted")

	m := w.write(w.rootPriv, nil, []frame.ID{rv.ID, e3.ID}, event.AddressRoot, event.VerbMerge, nil)
	must(t, l, m, Accepted, "merge of both branches")

	e4 := w.write(kPriv, &gr.ID, []frame.ID{m.ID}, "a", "note", []byte("after the merge"))
	must(t, l, e4, Rejected, "write after the merge: the revocation is in the past")

	for _, g := range l.ActiveGrants(m.ID) {
		if g == gr.ID {
			t.Fatal("a revoked grant still counts as live after the merge")
		}
	}
	live := false
	for _, g := range l.ActiveGrants(e3.ID) {
		if g == gr.ID {
			live = true
		}
	}
	if !live {
		t.Fatal("the grant should still be live on the branch that never saw the revocation")
	}
}

func TestOnlyGranterOrRootMayRevoke(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	aPub, aPriv := newKey(t)
	bPub, bPriv := newKey(t)

	ga := w.grant([]frame.ID{w.gen.ID}, aPub, "", nil, true)
	gb := w.grant([]frame.ID{ga.ID}, bPub, "", nil, false)
	must(t, l, ga, Accepted, "grant to A")
	must(t, l, gb, Accepted, "grant to B")

	bad := w.revoke(bPriv, &gb.ID, []frame.ID{gb.ID}, ga.ID)
	must(t, l, bad, Rejected, "revoking somebody else's grant")

	cPub, _ := newKey(t)
	sub := w.write(aPriv, &ga.ID, []frame.ID{gb.ID}, event.AddressRoot, event.VerbGrant,
		mustGrant(t, cPub, "home", []string{"note"}, false))
	must(t, l, sub, Accepted, "sub-delegation")

	own := w.revoke(aPriv, &ga.ID, []frame.ID{sub.ID}, sub.ID)
	must(t, l, own, Accepted, "a granter withdrawing their own grant")
}

// A scope only narrows, never widens. A writing delegate cannot mint a
// wider delegate than the one it holds.
//
//	— T6.1, N4.4
func TestSubDelegationCannotWiden(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	aPub, aPriv := newKey(t)
	cPub, _ := newKey(t)

	ga := w.grant([]frame.ID{w.gen.ID}, aPub, "home", []string{"note"}, true)
	must(t, l, ga, Accepted, "grant to A with delegation")

	narrow := w.write(aPriv, &ga.ID, []frame.ID{ga.ID}, event.AddressRoot, event.VerbGrant,
		mustGrant(t, cPub, "home/journal", []string{"note"}, false))
	must(t, l, narrow, Accepted, "narrower sub-delegation")

	wideScope := w.write(aPriv, &ga.ID, []frame.ID{narrow.ID}, event.AddressRoot, event.VerbGrant,
		mustGrant(t, cPub, "", []string{"note"}, false))
	must(t, l, wideScope, Rejected, "sub-delegation with a wider scope")

	wideVerb := w.write(aPriv, &ga.ID, []frame.ID{narrow.ID}, event.AddressRoot, event.VerbGrant,
		mustGrant(t, cPub, "home", nil, false))
	must(t, l, wideVerb, Rejected, "sub-delegation with wider verbs")

	bPub, bPriv := newKey(t)
	gb := w.grant([]frame.ID{narrow.ID}, bPub, "home", nil, false)
	must(t, l, gb, Accepted, "grant to B without delegation")
	nope := w.write(bPriv, &gb.ID, []frame.ID{gb.ID}, event.AddressRoot, event.VerbGrant,
		mustGrant(t, cPub, "home", []string{"note"}, false))
	must(t, l, nope, Rejected, "sub-delegation without permission")
}

// ---------- property: monotonicity ----------

func scenario(t *testing.T) (*world, []event.Signed) {
	w := newWorld(t)
	kPub, kPriv := newKey(t)
	jPub, jPriv := newKey(t)

	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, true)
	gj := w.grant([]frame.ID{gr.ID}, jPub, "home", []string{"note"}, false)
	a := w.write(kPriv, &gr.ID, []frame.ID{gj.ID}, "a", "note", []byte("1"))
	b := w.write(jPriv, &gj.ID, []frame.ID{gj.ID}, "home/b", "note", []byte("2"))
	m := w.write(w.rootPriv, nil, []frame.ID{a.ID, b.ID}, event.AddressRoot, event.VerbMerge, nil)
	rv := w.revoke(w.rootPriv, nil, []frame.ID{m.ID}, gj.ID)
	after := w.write(jPriv, &gj.ID, []frame.ID{rv.ID}, "home/b", "note", []byte("3")) // rejected
	side := w.write(jPriv, &gj.ID, []frame.ID{b.ID}, "home/b", "note", []byte("4"))   // accepted
	bad := w.write(kPriv, &gr.ID, []frame.ID{rv.ID}, "a", "note", []byte("5"))        // accepted
	orphan := w.write(kPriv, &gr.ID, []frame.ID{frame.Hash([]byte("never arrives"))},
		"a", "note", []byte("6")) // pending forever

	return w, []event.Signed{gr, gj, a, b, m, rv, after, side, bad, orphan}
}

// The final verdict must not depend on arrival order.
// Merging two copies of a ledger commutes and rewrites nothing. Nobody has
// to say which branch won.
//
//	— T5.4, N-Axiom6
func TestOrderOfArrivalDoesNotChangeVerdicts(t *testing.T) {
	w, evs := scenario(t)

	ref := map[frame.ID]State{}
	l := w.ledger()
	for _, e := range evs {
		if _, err := l.Add(e.Raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range evs {
		ref[e.ID] = l.State(e.ID)
	}
	a, r, p := l.Tally()
	if a < 5 || r < 1 || p < 1 {
		t.Fatalf("scenario is not varied enough: %d accepted, %d rejected, %d pending", a, r, p)
	}

	for seed := int64(0); seed < 40; seed++ {
		rng := mrand.New(mrand.NewSource(seed))
		perm := append([]event.Signed(nil), evs...)
		rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })

		l2 := w.ledger()
		for _, e := range perm {
			if _, err := l2.Add(e.Raw); err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
		}
		for id, want := range ref {
			if got := l2.State(id); got != want {
				t.Fatalf("seed %d: verdict for %s changed with order: %s != %s",
					seed, id.Short(), got, want)
			}
		}
	}
}

// No settled verdict may reopen when a new event arrives.
// A settled verdict does not reopen. The ledger only adds.
//
//	— T6.4, N-Axiom2
func TestVerdictsAreFinal(t *testing.T) {
	w, evs := scenario(t)
	for seed := int64(0); seed < 25; seed++ {
		rng := mrand.New(mrand.NewSource(seed + 1000))
		perm := append([]event.Signed(nil), evs...)
		rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })

		l := w.ledger()
		seen := map[frame.ID]State{}
		for step, e := range perm {
			if _, err := l.Add(e.Raw); err != nil {
				t.Fatal(err)
			}
			for id, prev := range seen {
				now := l.State(id)
				if prev != Pending && now != prev {
					t.Fatalf("seed %d step %d: verdict for %s went from %s back to %s",
						seed, step, id.Short(), prev, now)
				}
			}
			for _, x := range perm[:step+1] {
				seen[x.ID] = l.State(x.ID)
			}
		}
	}
}

// Not arrived is not absent. An event that has not come is unknown, not
// false, and it decides when its past arrives.
//
//	— T12.3
func TestPendingBecomesDecidedWhenAncestorArrives(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	child := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "a", "note", []byte("child"))

	must(t, l, child, Pending, "child before parent")
	must(t, l, gr, Accepted, "parent arrives")
	if l.State(child.ID) != Accepted {
		t.Fatal("child not accepted after its parent arrived; not-arrived was treated as not-existing")
	}
}

func TestDeterministicOrder(t *testing.T) {
	w, evs := scenario(t)
	var ref []frame.ID
	for seed := int64(0); seed < 20; seed++ {
		rng := mrand.New(mrand.NewSource(seed + 7))
		perm := append([]event.Signed(nil), evs...)
		rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
		l := w.ledger()
		for _, e := range perm {
			l.Add(e.Raw)
		}
		got := l.Order()
		if ref == nil {
			ref = got
			pos := map[frame.ID]int{}
			for i, id := range got {
				pos[id] = i
			}
			for i, id := range got {
				s, _ := l.Get(id)
				for _, p := range s.Event.Parents {
					if j, ok := pos[p]; ok && j >= i {
						t.Fatal("order is not topological")
					}
				}
			}
			continue
		}
		if len(got) != len(ref) {
			t.Fatalf("seed %d: order length changed", seed)
		}
		for i := range got {
			if got[i] != ref[i] {
				t.Fatalf("seed %d: order changed at position %d", seed, i)
			}
		}
	}
}

func TestHeadsAndIdempotentAdd(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	l.Add(gr.Raw)
	a := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "a", "note", []byte("1"))
	b := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "a", "note", []byte("2"))
	l.Add(a.Raw)
	l.Add(b.Raw)

	if h := l.Heads(); len(h) != 2 {
		t.Fatalf("expected two heads, got %d", len(h))
	}
	n := l.Len()
	for i := 0; i < 3; i++ {
		if st, _ := l.Add(a.Raw); st != Accepted {
			t.Fatal("re-adding changed the verdict")
		}
	}
	if l.Len() != n {
		t.Fatal("re-adding grew the ledger; idempotence broken")
	}

	m := w.write(kPriv, &gr.ID, []frame.ID{a.ID, b.ID}, event.AddressRoot, event.VerbMerge, nil)
	must(t, l, m, Accepted, "merge by a delegated key")
	if h := l.Heads(); len(h) != 1 || h[0] != m.ID {
		t.Fatalf("one head expected after the merge, got %v", h)
	}
}

// The ledger believes nothing but bytes.
// Rokh trusts nothing but bytes. Bad input is refused and leaves no trace:
// the ledger does not grow, because a rejected event was never there.
//
//	— T3, T3.3
func TestLedgerTrustsOnlyBytes(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	for name, raw := range map[string][]byte{
		"empty":     {},
		"garbage":   []byte("this is not an event"),
		"bad magic": append([]byte("XXXX"), make([]byte, 100)...),
	} {
		if _, err := l.Add(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	kPub, _ := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	bad := append([]byte(nil), gr.Raw...)
	bad[len(bad)/2] ^= 0xFF
	if _, err := l.Add(bad); err == nil {
		t.Fatal("a tampered event was accepted")
	}
	if l.Len() != 1 {
		t.Fatalf("bad input grew the ledger: %d", l.Len())
	}
}

func TestRootIsACopy(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	r := l.Root()
	r[0] ^= 0xFF
	if l.Root()[0] == r[0] {
		t.Fatal("Root handed out the ledger's own state")
	}
}

// CausalPast is a graph query, not a policy. Layers above use it to evaluate
// their own questions over causal history.
// Rokh's time is not a clock; it is lineage. "Earlier" means reachable
// through parents.
//
//	— T5, T5.1
func TestCausalPast(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	gr := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	must(t, l, gr, Accepted, "grant")

	a := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "a", "note", []byte("1"))
	b := w.write(kPriv, &gr.ID, []frame.ID{gr.ID}, "a", "note", []byte("2"))
	must(t, l, a, Accepted, "a")
	must(t, l, b, Accepted, "b")
	m := w.write(kPriv, &gr.ID, []frame.ID{a.ID, b.ID}, event.AddressRoot, event.VerbMerge, nil)
	must(t, l, m, Accepted, "merge")

	in := func(ids []frame.ID, id frame.ID) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}

	// From a, the past is genesis, grant and a. Not b.
	pa := l.CausalPast(a.ID)
	if !in(pa, w.gen.ID) || !in(pa, gr.ID) || !in(pa, a.ID) {
		t.Fatalf("ancestors of a are incomplete: %d", len(pa))
	}
	if in(pa, b.ID) {
		t.Fatal("a concurrent event appeared in the causal past")
	}
	if len(pa) != 3 {
		t.Fatalf("expected exactly 3 ancestors of a, got %d", len(pa))
	}

	// From the merge, everything.
	pm := l.CausalPast(m.ID)
	if len(pm) != 5 {
		t.Fatalf("expected 5 ancestors of the merge, got %d", len(pm))
	}

	// Deterministic and topological.
	again := l.CausalPast(m.ID)
	for i := range pm {
		if pm[i] != again[i] {
			t.Fatal("CausalPast is not deterministic")
		}
	}
	pos := map[frame.ID]int{}
	for i, id := range pm {
		pos[id] = i
	}
	for i, id := range pm {
		s, _ := l.Get(id)
		for _, p := range s.Event.Parents {
			if j, ok := pos[p]; ok && j >= i {
				t.Fatal("CausalPast is not topological")
			}
		}
	}

	// An unknown or unaccepted point contributes nothing.
	if got := l.CausalPast(frame.Hash([]byte("nowhere"))); len(got) != 0 {
		t.Fatalf("an unknown point yielded %d ancestors", len(got))
	}
}
