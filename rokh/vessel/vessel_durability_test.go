package vessel

// Probes of durability, fake commit outcomes, authentication of vessel and
// index, and G1.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// landsThenFails is a medium whose write of chosen files lands every byte and
// then reports an error, as a write does when the bytes reached the cache and
// the flush failed.
type landsThenFails struct {
	*Memory
	match func(name string) bool
	hits  int
}

var errFlush = errors.New("the flush failed after the bytes landed")

func (m *landsThenFails) Write(name string, b []byte) error {
	if err := m.Memory.Write(name, b); err != nil {
		return err
	}
	if m.match != nil && m.match(name) {
		m.hits++
		return errFlush
	}
	return nil
}

// Probe: the head lands and its flush fails. Contract U4: "recorded" means
// written, flushed and read back. What does Commit answer?
func TestAFailedFlushOfTheHeadIsNotRecorded(t *testing.T) {
	mem := NewMemory()
	m := &landsThenFails{Memory: mem}
	v, err := Create(m, Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(4)}, testVK, nil, Root{})
	if err != nil {
		t.Fatal(err)
	}
	own := &holder{}
	mustRecord(t, v, own, 1)
	m.match = func(name string) bool { return strings.Contains(name, "head") }
	out, err := record(t, v, own, 2)
	t.Logf("head landed, flush failed: outcome=%s err=%v (failing writes: %d)", out, err, m.hits)
	if out == Recorded {
		t.Errorf("a head whose flush failed is answered %q; by U4 it was not flushed", out)
	}
}

// Probe: a pack slab's flush fails. Nothing may be recorded.
func TestAFailedFlushOfASlabRecordsNothing(t *testing.T) {
	mem := NewMemory()
	m := &landsThenFails{Memory: mem}
	v, err := Create(m, Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(4)}, testVK, nil, Root{})
	if err != nil {
		t.Fatal(err)
	}
	own := &holder{}
	mustRecord(t, v, own, 1)
	before := v.Info().Generation
	m.match = func(name string) bool { return !strings.Contains(name, "head") }
	out, err := record(t, v, own, 2)
	t.Logf("slab landed, flush failed: outcome=%s err=%v", out, err)
	if out == Recorded {
		t.Errorf("recorded although a slab's flush failed")
	}
	m.match = nil
	w, _, err := Open(mem, unlockWith(testVK), stream(9))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if w.Info().Generation != before {
		t.Errorf("generation moved from %d to %d after a refused commit", before, w.Info().Generation)
	}
}

// Probe: two live packs of one vessel change places; a live pack is replaced
// by an older whole version of itself; a head file is planted in another
// head slot. Nothing of the altered generation may be served.
func TestIndexAndVersionAreAuthenticated(t *testing.T) {
	mem := NewMemory()
	v := newVessel(t, mem, 5)
	own := &holder{}
	for i := 0; i < 3; i++ {
		bigRecord(t, v, own, i)
	}
	var packs []uint32
	for i, e := range v.st.inv {
		if e.state == stateLive && e.kind == KindPack {
			packs = append(packs, uint32(i))
		}
	}
	if len(packs) < 2 {
		t.Skipf("only %d live packs", len(packs))
	}
	gen := v.Info().Generation

	// 1. Swap two live packs.
	swapped := mem.Clone()
	a, b := swapped.Get(SlabName(packs[0])), swapped.Get(SlabName(packs[1]))
	swapped.Put(SlabName(packs[0]), b)
	swapped.Put(SlabName(packs[1]), a)
	w, rep, err := Open(swapped, unlockWith(testVK), stream(9))
	if err == nil {
		serr := w.Scan(func(h Header, r Ref) error { _, e := w.Body(r); return e })
		t.Logf("swapped packs: opened generation %d of %d, report %+v, scan %v", w.Info().Generation, gen, rep.FellBack, serr)
		if w.Info().Generation == gen && serr == nil {
			t.Errorf("two packs changed places and the generation still reads")
		}
	} else {
		t.Logf("swapped packs: refused: %v", err)
	}

	// 2. Replay an older version of a slab at its own index.
	old := mem.Clone()
	for i := 3; i < 9; i++ {
		mustRecord(t, v, own, i)
	}
	replay := mem.Clone()
	n := 0
	for _, name := range replay.FileNames() {
		if len(name) == len("rokh/head0.rkh") {
			continue
		}
		if string(old.Get(name)) != string(replay.Get(name)) {
			replay.Put(name, old.Get(name))
			n++
		}
	}
	w, rep, err = Open(replay, unlockWith(testVK), stream(9))
	if err == nil {
		serr := w.Scan(func(h Header, r Ref) error { _, e := w.Body(r); return e })
		t.Logf("%d slabs replaced by their older versions: opened generation %d of %d, fell back %v, scan %v",
			n, w.Info().Generation, v.Info().Generation, rep.FellBack, serr)
		if w.Info().Generation == v.Info().Generation && serr == nil {
			t.Errorf("older slab versions were taken for the newest generation")
		}
	} else {
		t.Logf("%d slabs replaced by older versions: refused: %v", n, err)
	}

	// 3. A head file planted in another head slot.
	planted := mem.Clone()
	top := int(v.Info().Generation % HeadSlots)
	other := (top + 1) % HeadSlots
	planted.Put(HeadName(other), planted.Get(HeadName(top)))
	w, rep, err = Open(planted, unlockWith(testVK), stream(9))
	if err != nil {
		t.Fatalf("a planted head made the vessel unreadable: %v", err)
	}
	t.Logf("planted head%d into head%d: opened generation %d, torn %v", top, other, w.Info().Generation, rep.Torn)
	found := false
	for _, x := range rep.Torn {
		if x == other {
			found = true
		}
	}
	if !found {
		t.Errorf("a head file copied into another slot was not refused as a head")
	}
}

// Probe: the counterexample of G1, made on purpose and kept. Heads are copied
// at generation a, then R+1 commits land, each rewriting the last pack, then
// the slabs are copied.
func TestG1Counterexample(t *testing.T) {
	for _, extra := range []int{Retention, Retention + 1, Retention + 2, 8} {
		mem := NewMemory()
		v := newVessel(t, mem, 6)
		own := &holder{}
		for i := 0; i < 6; i++ {
			mustRecord(t, v, own, i)
		}
		a := v.Info().Generation
		cp := NewMemory()
		heads, slabs := splitFiles(mem)
		for _, n := range heads {
			cp.CopyFile(mem, n)
		}
		for j := 0; j < extra; j++ {
			mustRecord(t, v, own, 100+j)
		}
		for _, n := range slabs {
			cp.CopyFile(mem, n)
		}
		w, rep, err := Open(cp, unlockWith(testVK), stream(7))
		switch {
		case err != nil:
			t.Logf("G1: heads at generation %d, then %d commits, then slabs: THE COPY DOES NOT OPEN: %v", a, extra, err)
		default:
			serr := w.Scan(func(h Header, r Ref) error { _, e := w.Body(r); return e })
			verdict := "opens whole"
			if serr != nil {
				verdict = fmt.Sprintf("OPENS AND THEN FAILS TO READ: %v", serr)
			}
			t.Logf("G1: heads at generation %d, then %d commits, then slabs: generation %d, fell back %d times, %s",
				a, extra, w.Info().Generation, len(rep.FellBack), verdict)
		}
	}
}

// Probe: how early does G1 bite? Heads copied first, then k commits, then
// slabs, over many random streams.
func TestG1HowEarly(t *testing.T) {
	for _, k := range []int{3, 4, 5, 6, 8} {
		fails, first := 0, -1
		for seed := 1; seed <= 200; seed++ {
			mem := NewMemory()
			v, err := Create(mem, Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(byte(seed))}, testVK, nil, Root{})
			if err != nil {
				t.Fatal(err)
			}
			own := &holder{}
			for i := 0; i < 6; i++ {
				mustRecord(t, v, own, i)
			}
			cp := NewMemory()
			heads, slabs := splitFiles(mem)
			for _, n := range heads {
				cp.CopyFile(mem, n)
			}
			for j := 0; j < k; j++ {
				mustRecord(t, v, own, 100+j)
			}
			for _, n := range slabs {
				cp.CopyFile(mem, n)
			}
			w, _, err := Open(cp, unlockWith(testVK), stream(7))
			bad := err != nil
			if !bad {
				bad = w.Scan(func(h Header, r Ref) error { _, e := w.Body(r); return e }) != nil
			}
			if bad {
				fails++
				if first < 0 {
					first = seed
				}
			}
		}
		t.Logf("G1: %d commits during the copy: %d of 200 copies do not open or do not read (first failing stream %d)", k, fails, first)
		if k <= Retention && fails > 0 {
			t.Errorf("V2 broken inside its bound: %d failures with %d commits", fails, k)
		}
	}
}
