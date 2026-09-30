package lineage

import (
	"testing"

	"rokh/frame"
)

// S1: a seed's scopes lie inside the giver's own vessel scopes, and the whole
// rokh lies inside no slice. A source that holds a slice gives no whole seed:
// it is refused before the give, and nothing is written on either side.
func TestAWholeSeedOfASliceIsRefused(t *testing.T) {
	src, sm := newSide(t, 1, []string{"work"}, true)
	dst, m := target(43)
	before := sm.Writes()
	res, err := Seed(src, dst, plan(t, src, frame.Hash([]byte("whole of a slice")), nil))
	if Code(err) != "seed_refused" || res.OK() {
		t.Fatalf("a whole seed of a slice: %v %+v", err, res)
	}
	if sm.Writes() != before || len(m.FileNames()) != 0 {
		t.Fatal("a refused seed of a slice wrote")
	}
	// A seed inside the slice is given.
	dst, _ = target(44)
	if res, err := Seed(src, dst, plan(t, src, frame.Hash([]byte("inside the slice")), []string{"work/plan"})); err != nil || !res.OK() {
		t.Fatalf("a seed inside the slice: %v %+v", err, res)
	}
}
