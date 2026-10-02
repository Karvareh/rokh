package proof

import (
	"testing"

	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// A delegate's scope is a bound, and the bound is the component separator —
// not a string prefix. A grant at "home" reaches "home" and everything under
// it, and reaches neither "work" nor "homework". Every door is tried: the
// ledger itself, the daemon signing with the delegated key, and the daemon
// taking bytes that were signed elsewhere. All three give the same answer,
// and the two that can record say in one word that they recorded nothing.
//
//	— T6, T6.1, T6.3, T8.5, T12.3
func TestAScopeIsBoundedAtTheComponentAndEveryDoorAgrees(t *testing.T) {
	w := newWorld(t)
	dPub, dPriv := newKey(t)
	g := w.record(w.grant([]frame.ID{w.gen.ID}, dPub, "home", nil, false), "main")
	w.hold("delegate", dPriv, g.ID)

	cases := []struct {
		address string
		accept  bool
	}{
		{"home", true},
		{"home/journal", true},
		{"home/journal/today", true},
		{"work", false},
		{"homework", false},
		{"homestead/journal", false},
		{"hom", false},
		{"", false}, // not an address at all
	}

	// (a) The ledger, asked directly with bytes.
	l := w.load()
	for _, c := range cases {
		if c.address == "" {
			continue // event.Sign refuses to build it; door (b) covers the empty name
		}
		e := w.sign(dPriv, &g.ID, []frame.ID{g.ID}, c.address, "note", []byte("synthetic"))
		st, err := l.Add(e.Raw)
		if err != nil {
			t.Fatalf("ledger %q: %v", c.address, err)
		}
		want := ledger.Rejected
		if c.accept {
			want = ledger.Accepted
		}
		if st != want {
			t.Fatalf("ledger %q: verdict %s, want %s", c.address, st, want)
		}
		if !c.accept && l.Has(e.ID) {
			t.Fatalf("ledger %q: a rejected event was kept", c.address)
		}
	}

	// (b) The daemon, signing with the delegated key held for its door.
	s := daemon.New(w.car, w.load(), w.at(daemon.Options{AllowSign: true}))
	for _, c := range cases {
		r := ask(t, s, map[string]any{"op": "write", "address": c.address,
			"verb": "note", "message": "synthetic", "key": "delegate"})
		if c.accept {
			recorded(t, r, "write at "+c.address)
			continue
		}
		if c.address == "" {
			// An address that is not a name never reaches a verdict about
			// authority; it is refused for being unsayable.
			notRecorded(t, r, "bad_request")
			continue
		}
		notRecorded(t, r, "not_accepted")
	}

	// (c) The daemon, taking bytes that were signed elsewhere. No key of the
	// daemon's is involved, and the answer does not change.
	out := daemon.New(w.car, w.load(), w.at(daemon.Options{}))
	head := w.load().Heads()[0]
	for _, c := range cases {
		if c.address == "" || c.accept {
			continue
		}
		e := w.sign(dPriv, &g.ID, []frame.ID{head}, c.address, "note", []byte("elsewhere"))
		notRecorded(t, ask(t, out, map[string]any{"op": "append", "raw": hexOf(e.Raw)}), "not_accepted")
		if w.holds(t, e.ID) {
			t.Fatalf("append %q: a refused event reached the carrier", c.address)
		}
	}
}

// Sub-delegation does not exist until it is written, and what is written is
// never wider than what the writer holds. A delegate without CanDelegate
// mints nothing at all; one with it cannot widen the scope to the whole
// ledger, cannot move it sideways, and cannot hand on a verb it was not
// given. A narrower grant is accepted, and the key that receives it is bound
// by the narrower bound in turn.
//
//	— T6, T6.1, T6.2, N4.4
func TestADelegateNeverMintsSomethingWiderThanItself(t *testing.T) {
	w := newWorld(t)
	l := w.fresh()

	quietPub, quietPriv := newKey(t)
	quiet := w.grant([]frame.ID{w.gen.ID}, quietPub, "home", nil, false)
	mustVerdict(t, l, quiet, ledger.Accepted, "root's grant without delegation")

	thirdPub, _ := newKey(t)
	noRight := w.grantBy(quietPriv, &quiet.ID, []frame.ID{quiet.ID}, thirdPub, "home/x", nil, false)
	mustVerdict(t, l, noRight, ledger.Rejected, "a grant minted without CanDelegate")

	// A delegate that may delegate, bounded at "home" and at two verbs.
	dPub, dPriv := newKey(t)
	d := w.grant([]frame.ID{quiet.ID}, dPub, "home", []string{"note", "tag"}, true)
	mustVerdict(t, l, d, ledger.Accepted, "root's grant with delegation")

	subPub, subPriv := newKey(t)
	wider := []struct {
		why    string
		scope  string
		verbs  []string
		canDel bool
	}{
		{"the whole ledger", "", []string{"note"}, false},
		{"a scope beside its own", "work", []string{"note"}, false},
		{"a scope that only looks inside it", "homework", []string{"note"}, false},
		{"a verb it was never given", "home/inner", []string{"note", "publish"}, false},
		{"every verb, from a fixed list", "home/inner", nil, false},
	}
	for _, c := range wider {
		e := w.grantBy(dPriv, &d.ID, []frame.ID{d.ID}, subPub, c.scope, c.verbs, c.canDel)
		mustVerdict(t, l, e, ledger.Rejected, "sub-grant naming "+c.why)
	}

	// Narrower is the one direction that passes.
	sub := w.grantBy(dPriv, &d.ID, []frame.ID{d.ID}, subPub, "home/inner", []string{"note"}, false)
	mustVerdict(t, l, sub, ledger.Accepted, "a narrower sub-grant")

	inside := w.sign(subPriv, &sub.ID, []frame.ID{sub.ID}, "home/inner/leaf", "note", []byte("within"))
	mustVerdict(t, l, inside, ledger.Accepted, "the grand-delegate writing inside its bound")

	for _, c := range []struct {
		why  string
		addr string
		verb string
	}{
		{"outside the narrower scope but inside the wider one", "home/other", "note"},
		{"at the delegator's own root", "home", "note"},
		{"with a verb the narrower grant dropped", "home/inner/leaf", "tag"},
	} {
		e := w.sign(subPriv, &sub.ID, []frame.ID{sub.ID}, c.addr, c.verb, []byte("beyond"))
		mustVerdict(t, l, e, ledger.Rejected, "the grand-delegate writing "+c.why)
	}
}

// Revocation belongs to the root and to whoever granted, and to nobody else.
// It cannot reach a grant that is not in its own causal past. And what it
// closes is the future: a write on top of it is refused, a write made on a
// branch that never saw it stands, and that standing event is not unmade when
// the two branches meet. After the merge the revocation is in the past, and
// from there on the grant is dead.
//
//	— T6, T6.2, T6.5, T5.4, N6.2
func TestRevocationClosesTheFutureAndNeverUnseesThePast(t *testing.T) {
	w := newWorld(t)
	l := w.fresh()

	aPub, aPriv := newKey(t)
	bPub, bPriv := newKey(t)
	gA := w.grant([]frame.ID{w.gen.ID}, aPub, "home", nil, false)
	mustVerdict(t, l, gA, ledger.Accepted, "grant to A")
	gB := w.grant([]frame.ID{gA.ID}, bPub, "work", nil, false)
	mustVerdict(t, l, gB, ledger.Accepted, "grant to B")

	// B did not grant gA and is not the root, so B does not withdraw it.
	byB := w.revokeBy(bPriv, &gB.ID, []frame.ID{gB.ID}, gA.ID)
	mustVerdict(t, l, byB, ledger.Rejected, "a delegate revoking somebody else's grant")

	// A grant on a branch this revoke cannot see is not a grant it can close.
	cPub, _ := newKey(t)
	offPath := w.grant([]frame.ID{w.gen.ID}, cPub, "away", nil, false)
	blind := w.revokeBy(w.root, nil, []frame.ID{gB.ID}, offPath.ID)
	mustVerdict(t, l, blind, ledger.Rejected, "a revoke naming what is not in its past")

	n1 := w.sign(aPriv, &gA.ID, []frame.ID{gB.ID}, "home/journal", "note", []byte("before"))
	mustVerdict(t, l, n1, ledger.Accepted, "A writing while the grant stands")

	rev := w.revokeBy(w.root, nil, []frame.ID{n1.ID}, gA.ID)
	mustVerdict(t, l, rev, ledger.Accepted, "the root withdrawing its own grant")

	after := w.sign(aPriv, &gA.ID, []frame.ID{rev.ID}, "home/journal", "note", []byte("after"))
	mustVerdict(t, l, after, ledger.Rejected, "A writing on top of the revoke")

	// The other branch never saw it. Those events really were written before
	// their author knew, and they stand.
	fork := w.sign(aPriv, &gA.ID, []frame.ID{n1.ID}, "home/journal", "note", []byte("apart"))
	mustVerdict(t, l, fork, ledger.Accepted, "A writing on a branch that predates the revoke")

	merge := w.sign(w.root, nil, []frame.ID{rev.ID, fork.ID}, event.AddressRoot, event.VerbMerge, nil)
	mustVerdict(t, l, merge, ledger.Accepted, "the merge")

	if l.State(fork.ID) != ledger.Accepted {
		t.Fatal("the merge unmade an event that was validly written; revocation closed the past")
	}

	beyond := w.sign(aPriv, &gA.ID, []frame.ID{merge.ID}, "home/journal", "note", []byte("beyond"))
	mustVerdict(t, l, beyond, ledger.Rejected, "A writing after the merge reached the revoke")

	// And asked at each point, the ledger says the same thing.
	if live := l.ActiveGrants(fork.ID); !containsID(live, gA.ID) {
		t.Fatal("at the fork's own head the grant should still stand")
	}
	if live := l.ActiveGrants(merge.ID); containsID(live, gA.ID) {
		t.Fatal("at the merge the revocation is in the past and the grant is dead")
	}
}

// A grant carries no clock, and neither does a verdict. An event whose only
// oddity is a clock oracle reporting a century from now, or a century ago, is
// judged exactly as any other: by its lineage. Testimony is not verification,
// and there is no field here for time to be ordered by.
//
//	— T5, T5.2, T6, T12.5
func TestTimeIsTestimonyAndNeverAVerdict(t *testing.T) {
	w := newWorld(t)
	l := w.fresh()

	dPub, dPriv := newKey(t)
	g := w.grant([]frame.ID{w.gen.ID}, dPub, "home", nil, false)
	mustVerdict(t, l, g, ledger.Accepted, "grant")

	clocks := []string{
		"2999-12-31T23:59:59Z",
		"1970-01-01T00:00:00Z",
		"0001-01-01T00:00:00Z",
		"not a time at all",
	}
	parent := g.ID
	for _, claim := range clocks {
		e := w.sign(dPriv, &g.ID, []frame.ID{parent}, "home/journal", "note", []byte("in scope"),
			event.Attestation{Oracle: "clock", Claim: []byte(claim)})
		mustVerdict(t, l, e, ledger.Accepted, "an event whose clock says "+claim)
		parent = e.ID
	}

	// And the reverse: an impeccable clock buys nothing. Outside the scope is
	// outside the scope.
	bad := w.sign(dPriv, &g.ID, []frame.ID{parent}, "work", "note", []byte("out of scope"),
		event.Attestation{Oracle: "clock", Claim: []byte("2026-09-15T00:00:00Z")})
	mustVerdict(t, l, bad, ledger.Rejected, "a well-dated event outside the scope")

	// The order the ledger gives is lineage's, and the clocks played no part:
	// each event follows the one it names as parent.
	order := l.Order()
	at := map[frame.ID]int{}
	for i, id := range order {
		at[id] = i
	}
	for _, id := range order {
		e, _ := l.Get(id)
		for _, p := range e.Event.Parents {
			if at[p] >= at[id] {
				t.Fatal("an event was ordered before its own parent")
			}
		}
	}
}

// mustVerdict adds one event's bytes and insists on a verdict.
func mustVerdict(t *testing.T, l *ledger.Ledger, s event.Signed, want ledger.State, why string) {
	t.Helper()
	got, err := l.Add(s.Raw)
	if err != nil {
		t.Fatalf("%s: %v", why, err)
	}
	if got != want {
		t.Fatalf("%s: verdict %s, want %s", why, got, want)
	}
	if want == ledger.Rejected && l.Has(s.ID) {
		t.Fatalf("%s: a rejected event was kept", why)
	}
}

func containsID(ids []frame.ID, want frame.ID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
