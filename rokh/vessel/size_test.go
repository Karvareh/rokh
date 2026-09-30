package vessel

import (
	"errors"
	"testing"
)

// Compact keeps every event and the latest pointer, and frees what it
// replaced.
func TestCompactDropsSupersededPointers(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 20)
	own := &holder{}
	for i := 0; i < 30; i++ {
		mustRecord(t, v, own, i)
	}
	pointers := 0
	v.Scan(func(h Header, r Ref) error {
		if h.Type == RecPointer {
			pointers++
		}
		return nil
	})
	if pointers != 30 {
		t.Fatalf("%d pointer records before compaction", pointers)
	}
	if err := v.Compact(); err != nil {
		t.Fatal(err)
	}
	w, _ := reopen(t, m)
	ev, ptr := contents(t, w)
	pointers = 0
	w.Scan(func(h Header, r Ref) error {
		if h.Type == RecPointer {
			pointers++
		}
		return nil
	})
	if len(ev) != 30 || ptr != "main -> 29" || pointers != 1 {
		t.Fatalf("after compaction: %d events, %q, %d pointers", len(ev), ptr, pointers)
	}
}

// Shrink is refused unless the live slabs fit.
func TestShrinkIsRefusedWhenTheLiveSlabsDoNotFit(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 21)
	own := &holder{}
	if err := v.Grow(64); err != nil && errors.Is(err, ErrTurnUnavailable) {
		// Grow needs a Begin first.
	}
	tx, _ := v.Begin(own)
	tx.Abandon()
	if err := v.Grow(64); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		bigRecord(t, v, own, i)
	}
	if err := v.Shrink(16, false); Code(err) != "vessel_full" {
		t.Fatalf("a shrink below the live slabs: %v", err)
	}
	w, _ := reopen(t, m)
	if w.Info().Slabs != 64 {
		t.Fatal("a refused shrink changed the vessel")
	}
}

// A crash in the middle of grow leaves files beyond N: ignored and reported.
func TestACrashDuringGrowLeavesFilesBeyondN(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 22)
	own := &holder{}
	mustRecord(t, v, own, 0)
	m.Fail = func(name string, w int) error {
		if len(name) == len("rokh/head0.rkh") {
			return errors.New("power cut")
		}
		return nil
	}
	if err := v.Grow(24); err == nil {
		t.Fatal("grow recorded without its head")
	}
	m.Fail = nil
	w, rep := reopen(t, m)
	if w.Info().Slabs != 16 || len(rep.Beyond) != 8 {
		t.Fatalf("after a cut grow: %d slabs, %d beyond", w.Info().Slabs, len(rep.Beyond))
	}
	ev, _ := contents(t, w)
	if len(ev) != 1 {
		t.Fatal("the cut grow lost a record")
	}
	// Repeating the grow finishes it.
	tx, _ := w.Begin(own)
	tx.Abandon()
	if err := w.Grow(24); err != nil {
		t.Fatal(err)
	}
	if _, rep := reopen(t, m); len(rep.Beyond) != 0 {
		t.Fatalf("files beyond N after the repeated grow: %v", rep.Beyond)
	}
}

// A reviewed case: a commit on a fallen-back base is recorded, and the caller
// reads the fallback from the transaction; the head that did not verify is
// the one overwritten, and the next open no longer falls back.
func TestACommitOnAFallenBackBaseIsRecordedAndSaysSo(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 30)
	own := &holder{}
	for i := 0; i < 5; i++ {
		mustRecord(t, v, own, i)
	}
	g := v.Info().Generation
	seg := v.st.root.segments[0].index
	m.Flip(SlabName(seg), 8*4000+5) // the newest generation no longer verifies
	w, rep := reopen(t, m)
	if w.Info().Generation != g-1 || len(rep.FellBack) != 1 || rep.FellBack[0].From != g {
		t.Fatalf("fallback not reported: %+v", rep)
	}
	tx, err := w.Begin(own)
	if err != nil {
		t.Fatal(err)
	}
	tx.Put(eventHeader(99), []byte("after the fallback"))
	out, err := tx.Commit()
	if err != nil || out != Recorded {
		t.Fatalf("a commit on the verified base: %s %v", out, err)
	}
	if b := tx.Base(); b.Generation != g-1 || len(b.FellBack) != 1 || b.FellBack[0].From != g {
		t.Fatalf("the transaction does not say it was built on a fallback: %+v", b)
	}
	x, rep := reopen(t, m)
	if x.Info().Generation != g || len(rep.FellBack) != 0 {
		t.Fatalf("after the commit: generation %d, report %+v", x.Info().Generation, rep)
	}
	ev, _ := contents(t, x)
	if len(ev) != 5 || ev[len(ev)-1] != "after the fallback" {
		t.Fatalf("records after the fallback commit: %v", ev)
	}
}
