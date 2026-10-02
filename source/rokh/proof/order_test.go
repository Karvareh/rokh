package proof

import (
	"fmt"
	mrand "math/rand"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// tangle is a history with everything awkward in it: a chain, a sub-grant, a
// revocation, events refused for scope and for having no authority at all, a
// child of a refused event, a branch written by a key that never saw the
// revocation, a merge that reaches it, and a write after the merge.
func tangle(t *testing.T, w *world) []event.Signed {
	t.Helper()
	aPub, aPriv := newKey(t)
	bPub, bPriv := newKey(t)
	cPub, cPriv := newKey(t)
	_, strangerPriv := newKey(t)

	var all []event.Signed
	keep := func(s event.Signed) event.Signed {
		all = append(all, s)
		return s
	}

	gA := keep(w.grant([]frame.ID{w.gen.ID}, aPub, "home", []string{"note", "tag"}, true))
	gB := keep(w.grant([]frame.ID{gA.ID}, bPub, "work", nil, false))

	// A writes a chain at home.
	head := gB.ID
	for i := 0; i < 7; i++ {
		head = keep(w.sign(aPriv, &gA.ID, []frame.ID{head}, "home/journal",
			"note", []byte(fmt.Sprintf("note %d", i)))).ID
	}
	n5 := head

	// A sub-grants C a narrower right, and C uses it.
	sub := keep(w.grantBy(aPriv, &gA.ID, []frame.ID{n5}, cPub, "home/inner", []string{"note"}, false))
	head = sub.ID
	for i := 0; i < 4; i++ {
		head = keep(w.sign(cPriv, &sub.ID, []frame.ID{head}, "home/inner/leaf",
			"note", []byte(fmt.Sprintf("leaf %d", i)))).ID
	}
	c3 := head

	// Three that are refused, one of them because its parent was.
	bad1 := keep(w.sign(bPriv, &gB.ID, []frame.ID{c3}, "home/journal", "note", []byte("outside B's scope")))
	keep(w.sign(strangerPriv, nil, []frame.ID{c3}, "home/journal", "note", []byte("no authority at all")))
	keep(w.sign(aPriv, &gA.ID, []frame.ID{bad1.ID}, "home/journal", "note", []byte("child of a refusal")))
	keep(w.sign(cPriv, &sub.ID, []frame.ID{c3}, "home/inner/leaf", "tag", []byte("a verb C never had")))

	// B writes its own branch, which never meets the rest until the end.
	bHead := gB.ID
	for i := 0; i < 3; i++ {
		bHead = keep(w.sign(bPriv, &gB.ID, []frame.ID{bHead}, "work/desk",
			"note", []byte(fmt.Sprintf("work %d", i)))).ID
	}

	// The root withdraws A's grant, and A keeps writing on top of it.
	rev := keep(w.revokeBy(w.root, nil, []frame.ID{c3}, gA.ID))
	keep(w.sign(aPriv, &gA.ID, []frame.ID{rev.ID}, "home/journal", "note", []byte("after the revoke")))
	keep(w.sign(cPriv, &sub.ID, []frame.ID{rev.ID}, "home/inner/leaf", "note", []byte("C, whose own grant is untouched")))

	// A branch that predates the revocation: written before its author knew.
	fork := n5
	for i := 0; i < 4; i++ {
		fork = keep(w.sign(aPriv, &gA.ID, []frame.ID{fork}, "home/journal",
			"note", []byte(fmt.Sprintf("apart %d", i)))).ID
	}

	merge := keep(w.sign(w.root, nil, []frame.ID{rev.ID, fork}, event.AddressRoot, event.VerbMerge, nil))
	keep(w.sign(aPriv, &gA.ID, []frame.ID{merge.ID}, "home/journal", "note", []byte("after the merge")))
	keep(w.sign(w.root, nil, []frame.ID{merge.ID}, "home/journal", "note", []byte("the owner, still")))
	return all
}

// Arrival order is not evidence. The same thirty-odd events, poured into two
// fresh ledgers in two different random orders, settle to the same verdict for
// every single name, the same heads and the same order. Nothing here depends
// on who arrived first, because authority is judged in each event's own causal
// past and that is fixed the moment it is signed.
//
//	— T5.4, T6.4, T3.3, N-Axiom2
func TestArrivalOrderChangesNoVerdict(t *testing.T) {
	w := newWorld(t)
	all := tangle(t, w)
	if len(all) < 28 {
		t.Fatalf("the tangle is only %d events; it is meant to be about thirty", len(all))
	}

	type view struct {
		state map[frame.ID]ledger.State
		heads []frame.ID
		order []frame.ID
		tally [3]int
	}
	pour := func(seed int64) view {
		l := w.fresh()
		idx := mrand.New(mrand.NewSource(seed)).Perm(len(all))
		for _, i := range idx {
			if _, err := l.Add(all[i].Raw); err != nil {
				t.Fatalf("seed %d: adding %s: %v", seed, all[i].ID.Short(), err)
			}
		}
		v := view{state: map[frame.ID]ledger.State{}}
		for _, s := range all {
			v.state[s.ID] = l.State(s.ID)
		}
		v.state[w.gen.ID] = l.State(w.gen.ID)
		v.heads, v.order = l.Heads(), l.Order()
		a, r, p := l.Tally()
		v.tally = [3]int{a, r, p}
		return v
	}

	base := pour(1)
	if base.tally[2] != 0 {
		t.Fatalf("a whole history left %d events pending", base.tally[2])
	}
	if base.tally[1] == 0 {
		t.Fatal("the tangle is meant to contain refusals and contains none")
	}
	// Three bodies wrote while apart and nothing joined them: B's branch, the
	// sub-delegate writing past a revocation that was never about its own
	// grant, and the owner after the merge. Several heads is not a broken
	// ledger, and the ledger names no winner among them.
	//   — T5.5
	if len(base.heads) != 3 {
		t.Fatalf("heads = %d, want 3: %v", len(base.heads), base.heads)
	}

	for _, seed := range []int64{2, 3, 5, 8, 13, 21, 34, 55} {
		got := pour(seed)
		for id, want := range base.state {
			if got.state[id] != want {
				t.Fatalf("seed %d: %s is %s, was %s under seed 1",
					seed, id.Short(), got.state[id], want)
			}
		}
		if !sameIDs(got.heads, base.heads) {
			t.Fatalf("seed %d: heads differ\n got %v\nwant %v", seed, got.heads, base.heads)
		}
		if !sameIDs(got.order, base.order) {
			t.Fatalf("seed %d: the deterministic order is not deterministic", seed)
		}
		if got.tally != base.tally {
			t.Fatalf("seed %d: tally %v, want %v", seed, got.tally, base.tally)
		}
	}

	// And the same history read off a carrier, parents-first, agrees too: the
	// accepted events and a reference to every head, recorded together, and
	// read back by opening the carrier again.
	w.commit(func(r *carrier.Recording) error {
		for _, s := range all {
			if base.state[s.ID] != ledger.Accepted {
				continue
			}
			if err := r.Event(s.ID, s.Head, s.Body, s.Event.Address); err != nil {
				return err
			}
		}
		for i, h := range base.heads {
			if err := r.SetRef(fmt.Sprintf("head-%d", i), h); err != nil {
				return err
			}
		}
		return nil
	})
	loaded := loadFrom(t, w.reopen())
	if !sameIDs(loaded.Order(), base.order) {
		t.Fatal("the carrier read back in a different order from the live view")
	}
	if !sameIDs(loaded.Heads(), base.heads) {
		t.Fatal("the carrier read back different heads from the live view")
	}
}

func sameIDs(a, b []frame.ID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
