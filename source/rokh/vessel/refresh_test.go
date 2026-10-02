package vessel

import (
	"testing"
)

// An opened vessel keeps what it read in memory, and another opening of the
// same medium commits to the medium, not to that memory. Moved says the
// medium moved by reading one head file; Refresh reads the present
// generation. Neither writes a byte, and after a refresh the opening serves
// what the medium holds, however many commits it missed.
//
//	— T10, contract 2.6, 2.7
func TestAnOpeningReadsWhatAnotherOpeningCommitted(t *testing.T) {
	m := NewMemory()
	a := newVessel(t, m, 21)
	mustRecord(t, a, &holder{}, 0)
	b, _ := reopen(t, m)
	if b.Moved() {
		t.Fatal("a fresh opening says the medium moved")
	}
	if moved, err := b.Refresh(); moved || err != nil {
		t.Fatalf("a refresh of a fresh opening: %v %v", moved, err)
	}

	mustRecord(t, a, &holder{}, 1)
	if a.Moved() {
		t.Fatal("a vessel's own commit counts as another writer's")
	}
	if !b.Moved() {
		t.Fatal("another opening's commit went unnoticed")
	}
	// Until it refreshes, an opening serves what it read.
	if ev, ptr := contents(t, b); len(ev) != 1 || ptr != "main -> 0" {
		t.Fatalf("before the refresh: %d events, %q", len(ev), ptr)
	}
	m.Record(true)
	moved, err := b.Refresh()
	calls := m.Log()
	m.Record(false)
	if !moved || err != nil {
		t.Fatalf("the refresh after another's commit: %v %v", moved, err)
	}
	for _, c := range calls {
		if c.Op != "read" && c.Op != "names" {
			t.Fatalf("a refresh called %s %s", c.Op, c.Name)
		}
	}
	if ev, ptr := contents(t, b); len(ev) != 2 || ptr != "main -> 1" {
		t.Fatalf("after the refresh: %d events, %q", len(ev), ptr)
	}
	if b.Info().Generation != a.Info().Generation {
		t.Fatalf("b opened generation %d, the medium holds %d", b.Info().Generation, a.Info().Generation)
	}
	if b.Moved() {
		t.Fatal("still moved after reading the present generation")
	}
	if moved, err := b.Refresh(); moved || err != nil {
		t.Fatalf("a second refresh: %v %v", moved, err)
	}

	// b commits on what a committed; a reads b's commit the same way.
	mustRecord(t, b, &holder{}, 2)
	if b.Moved() {
		t.Fatal("b's own commit counts as another writer's")
	}
	if !a.Moved() {
		t.Fatal("b's commit went unnoticed by a")
	}
	if moved, err := a.Refresh(); !moved || err != nil {
		t.Fatalf("a's refresh: %v %v", moved, err)
	}
	if ev, ptr := contents(t, a); len(ev) != 3 || ptr != "main -> 2" {
		t.Fatalf("a after b's commit: %d events, %q", len(ev), ptr)
	}

	// Many commits missed, past every head file and past the retention: the
	// slabs b read may have been written over, and the refresh reads anew.
	for i := 3; i < 12; i++ {
		mustRecord(t, a, &holder{}, i)
	}
	if !b.Moved() {
		t.Fatal("nine commits went unnoticed")
	}
	if moved, err := b.Refresh(); !moved || err != nil {
		t.Fatalf("the refresh after nine commits: %v %v", moved, err)
	}
	if ev, ptr := contents(t, b); len(ev) != 12 || ptr != "main -> 11" {
		t.Fatalf("after nine missed commits: %d events, %q", len(ev), ptr)
	}
	w, _ := reopen(t, m)
	if ev, ptr := contents(t, w); len(ev) != 12 || ptr != "main -> 11" {
		t.Fatalf("a fresh opening: %d events, %q", len(ev), ptr)
	}
}

// A head torn by a writer that died changes the head file and is not a
// generation: Refresh keeps the generation it has, and Moved is quiet again
// afterwards rather than asking for a refresh at every question. The next
// whole commit is read.
//
//	— T10, contract 2.7, V1
func TestATornHeadIsNotANewGeneration(t *testing.T) {
	m := NewMemory()
	a := newVessel(t, m, 22)
	mustRecord(t, a, &holder{}, 0)
	b, _ := reopen(t, m)
	gen := b.Info().Generation
	m.Torn = func(name string, _ int) int {
		if len(name) == len("rokh/head0.rkh") {
			return 100
		}
		return 0
	}
	if out, _ := record(t, a, &holder{}, 1); out == Recorded {
		t.Fatal("a commit whose head was torn answered recorded")
	}
	m.Torn = nil
	if !b.Moved() {
		t.Fatal("the torn head file went unnoticed")
	}
	if moved, err := b.Refresh(); moved || err != nil || b.Info().Generation != gen {
		t.Fatalf("after a torn head: moved %v, %v, generation %d, want %d", moved, err, b.Info().Generation, gen)
	}
	if b.Moved() {
		t.Fatal("a torn head keeps saying moved")
	}
	if ev, ptr := contents(t, b); len(ev) != 1 || ptr != "main -> 0" {
		t.Fatalf("after a torn head: %d events, %q", len(ev), ptr)
	}
	c, _ := reopen(t, m)
	mustRecord(t, c, &holder{}, 2)
	if !b.Moved() {
		t.Fatal("the whole commit after the torn one went unnoticed")
	}
	if moved, err := b.Refresh(); !moved || err != nil {
		t.Fatalf("the refresh after the whole commit: %v %v", moved, err)
	}
	if ev, ptr := contents(t, b); len(ev) != 2 || ptr != "main -> 2" {
		t.Fatalf("after the whole commit: %d events, %q", len(ev), ptr)
	}
}
