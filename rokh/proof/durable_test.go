package proof

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"rokh/carrier"
	"rokh/daemon"
	"rokh/frame"
	"rokh/ledger"
	"rokh/vessel"
)

func refHeads(t *testing.T, w *world) []frame.ID {
	t.Helper()
	refs, err := w.car.Refs()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out
}

// The carrier is the truth and the live view is a view of it, so after two
// hundred recordings the two must be the same ledger — the same order, the
// same heads, the same tally — with nothing kept in memory that the carrier
// does not hold. And a view left behind fifty recordings ago is brought up by
// reading only what it is missing: Extend names exactly those fifty and ends
// in the state a whole reopen would have reached.
//
//	— T8.5, T4.4, T3.3, T1.1
func TestReopeningEqualsTheLiveViewAndExtendCatchesUp(t *testing.T) {
	w := newFastWorld(t)
	live := w.load()
	s := daemon.New(w.car, live, w.at(daemon.Options{AllowSign: true}))

	const total, lastRun = 200, 50
	ids := make([]string, 0, total)
	var stale *ledger.Ledger
	for i := 0; i < total; i++ {
		if i == total-lastRun {
			// A view of the carrier as it stands right now; the rest of the
			// recordings happen behind its back.
			stale = w.load()
		}
		r := recorded(t, ask(t, s, map[string]any{"op": "write",
			"address": fmt.Sprintf("home/journal/%03d", i), "verb": "note",
			"message": fmt.Sprintf("entry %d", i)}), "write")
		ids = append(ids, r["id"].(string))
	}

	// The carrier opened again, from its bytes alone.
	fresh := loadFrom(t, w.reopen())
	if !sameIDs(live.Order(), fresh.Order()) {
		t.Fatal("the live order and the carrier's order are not the same ledger")
	}
	if !sameIDs(live.Heads(), fresh.Heads()) {
		t.Fatalf("heads differ: live %v, carrier %v", live.Heads(), fresh.Heads())
	}
	la, lr, lp := live.Tally()
	fa, fr, fp := fresh.Tally()
	if la != fa || lr != fr || lp != fp {
		t.Fatalf("tally differs: live %d/%d/%d, carrier %d/%d/%d", la, lr, lp, fa, fr, fp)
	}
	if fa != total+1 {
		t.Fatalf("accepted = %d, want %d", fa, total+1)
	}

	// The stale view is brought up by reading only what it does not hold.
	if got := stale.Len(); got != total-lastRun+1 {
		t.Fatalf("the stale view holds %d events, want %d", got, total-lastRun+1)
	}
	added, err := stale.Extend(w.car.Get, refHeads(t, w))
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != lastRun {
		t.Fatalf("Extend reported %d new events, want %d", len(added), lastRun)
	}
	want := map[string]bool{}
	for _, id := range ids[total-lastRun:] {
		want[id] = true
	}
	for _, id := range added {
		if !want[id.String()] {
			t.Fatalf("Extend named %s, which is not one of the last %d recordings", id.Short(), lastRun)
		}
		delete(want, id.String())
	}
	if len(want) != 0 {
		t.Fatalf("Extend missed %d of the recordings it should have found", len(want))
	}
	if !sameIDs(stale.Order(), fresh.Order()) {
		t.Fatal("an extended view is not the same ledger as a reopened one")
	}
	if !sameIDs(stale.Heads(), fresh.Heads()) {
		t.Fatal("an extended view has different heads from a reopened one")
	}

	// Asking again, with nothing new to find, finds nothing and changes
	// nothing.
	again, err := stale.Extend(w.car.Get, refHeads(t, w))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("a second Extend invented %d events", len(again))
	}
}

// A recording is one commit, and its point is the head file: before the head
// is written nothing, after it everything (contract 2.6). Cut the recording
// anywhere before that point — the medium refusing every write from the pack
// on, from the segment on, from the head on, or tearing the head half way —
// and what it wrote lies in slabs no generation names: it is not an event,
// and the next open does not find it. What was recorded before is intact,
// the next recording on that branch proceeds as if nothing had happened, and
// the abandoned bytes are not poison — offered again through the door that
// takes signed bytes, the same event is accepted and held once: what the cut
// left behind never becomes a second record of it. A third offer is a no-op
// rather than a second event, and recording identical bytes again names the
// same event.
//
// The 0.9 carrier wrote the event and then the reference, and a cut between
// the two left a whole object on disk that the re-offer found and did not
// write again. A v1 cut leaves no object: its bytes are overwritten when the
// slabs they lie in are chosen again. So "no second object" is asked of the
// one v1 has: the carrier keeps one record of the event, not two.
//
//	— T8.5, T3.3, T4.4, N2.8
func TestBytesLeftBehindByACutAreNotAnEventAndNotPoison(t *testing.T) {
	for _, c := range []struct {
		where string
		at    int // the write of the recording from which the medium refuses: 1 pack, 2 segment, 3 head
		torn  bool
	}{
		{"before its pack", 1, false},
		{"before its segment", 2, false},
		{"before its head", 3, false},
		{"half way through its head", 3, true},
	} {
		t.Run(c.where, func(t *testing.T) { cutAndOfferAgain(t, c.at, c.torn) })
	}
}

func cutAndOfferAgain(t *testing.T, at int, torn bool) {
	w := newFastWorld(t)
	live := w.load()
	s := daemon.New(w.car, live, w.at(daemon.Options{AllowSign: true}))
	recorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/a",
		"verb": "note", "message": "before the cut"}), "the recording before the cut")

	head := refHeadOf(t, w, "main")
	orphan := w.sign(w.root, nil, []frame.ID{head}, "home/b", "note", []byte("cut in the middle of its recording"))
	sizes := w.sizes(t)
	_, rep := w.open(t)
	generation := rep.Generation

	// The recording is made; the medium stops taking writes part way through,
	// as it does when the power goes.
	m := w.mem
	start := m.Writes()
	m.Fail = func(name string, n int) error {
		if n > start+at || (n == start+at && !torn) {
			return errCut
		}
		return nil
	}
	if torn {
		m.Torn = func(name string, n int) int {
			if n == start+at {
				return vessel.HeadSize / 2
			}
			return 0
		}
	}
	own, release := w.writer(t)
	rec, err := w.car.Begin(own)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Event(orphan.ID, orphan.Head, orphan.Body, orphan.Event.Address); err != nil {
		t.Fatal(err)
	}
	if err := rec.SetRef("main", orphan.ID); err != nil {
		t.Fatal(err)
	}
	out, err := rec.Commit()
	release()
	m.Fail, m.Torn = nil, nil
	if out == vessel.Recorded {
		t.Fatalf("a recording cut before its commit point was reported recorded (%v)", err)
	}
	if written := m.Writes() - start; written != at {
		t.Fatalf("the cut recording made %d writes, want %d: the cut is not where this case puts it", written, at)
	}

	// The cut changed the bytes of slabs no generation names, and nothing
	// else: the file set and every length are as they were (contract V6),
	// and the carrier opens to the generation it had.
	if got := w.sizes(t); !reflect.DeepEqual(got, sizes) {
		t.Fatal("the cut changed the carrier's file set or a length")
	}
	opened, rep := w.open(t)
	if rep.Generation != generation {
		t.Fatalf("after the cut the carrier opens at generation %d, want %d", rep.Generation, generation)
	}
	if torn && len(rep.Torn) == 0 {
		t.Fatalf("a torn head is not reported: %+v", rep)
	}
	after := loadFrom(t, opened)
	if after.Has(orphan.ID) {
		t.Fatal("bytes no commit reached were read back as an event")
	}
	if got, want := after.Len(), 2; got != want {
		t.Fatalf("the reopened ledger holds %d events, want %d", got, want)
	}
	if got := branchOf(t, opened, "main"); got != head {
		t.Fatal("the cut moved the branch")
	}
	if w.holds(t, orphan.ID) {
		t.Fatal("the carrier keeps a record of an event whose recording was cut")
	}

	// The next recording on that branch proceeds from where the reference is.
	next := recorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/c",
		"verb": "note", "message": "after the cut"}), "the recording after the cut")
	nextID, err := frame.ParseID(next["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if got := loadFrom(t, w.reopen()); !got.Has(nextID) || got.Has(orphan.ID) {
		t.Fatal("the recording after the cut did not land, or the orphan came back with it")
	}

	// The same bytes offered again are an ordinary event with an ordinary
	// ancestry, held once: the cut's leftovers are not a record of it.
	offer := daemon.New(w.car, w.load(), w.at(daemon.Options{}))
	recorded(t, ask(t, offer, map[string]any{"op": "append", "raw": hexOf(orphan.Raw)}), "the orphan, offered again")
	back := loadFrom(t, w.reopen())
	if !back.Has(orphan.ID) || back.State(orphan.ID) != ledger.Accepted {
		t.Fatal("the re-offered event is not recorded")
	}
	if got, want := back.Len(), 4; got != want {
		t.Fatalf("the ledger holds %d events, want %d", got, want)
	}
	if got := w.envelopes(t, orphan.ID); got != 1 {
		t.Fatalf("the carrier keeps %d records of the re-offered event, want 1", got)
	}

	// And a third offer is a no-op rather than a second event.
	recorded(t, ask(t, offer, map[string]any{"op": "append", "raw": hexOf(orphan.Raw)}), "a third offer")
	if got, want := loadFrom(t, w.reopen()).Len(), 4; got != want {
		t.Fatalf("after a third offer the ledger holds %d events, want %d", got, want)
	}

	// Recording identical bytes again is not an error, and it names the same
	// event: the carrier serves the same bytes under the same name.
	w.commit(func(r *carrier.Recording) error {
		return r.Event(orphan.ID, orphan.Head, orphan.Body, orphan.Event.Address)
	})
	raw, err := w.reopen().Get(orphan.ID)
	if err != nil || !bytes.Equal(raw, orphan.Raw) {
		t.Fatalf("recording identical bytes again: %v", err)
	}
	if got, want := loadFrom(t, w.reopen()).Len(), 4; got != want {
		t.Fatalf("recording identical bytes again made %d events, want %d", got, want)
	}
}

func refHeadOf(t *testing.T, w *world, branch string) frame.ID {
	t.Helper()
	return branchOf(t, w.car, branch)
}

func branchOf(t *testing.T, c *carrier.Carrier, branch string) frame.ID {
	t.Helper()
	id, found, err := c.Ref(branch)
	if err != nil || !found {
		t.Fatalf("branch %q: found=%v err=%v", branch, found, err)
	}
	return id
}
