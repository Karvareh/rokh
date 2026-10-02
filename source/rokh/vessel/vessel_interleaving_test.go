package vessel

// Probes of the vessel under adverse interleavings: a commit after fallback,
// copies during recordings, a stale slot cell, no create and no remove while
// recording.

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

func splitFiles(m *Memory) (heads, slabs []string) {
	for _, n := range m.FileNames() {
		if len(n) == len("rokh/head0.rkh") {
			heads = append(heads, n)
		} else {
			slabs = append(slabs, n)
		}
	}
	return
}

// Probe 1: a vessel that opened by falling back must still be writable, or
// must refuse with a code that names the reason. Contract 2.6 steps 1 and 4
// speak of the highest VALID generation.
func TestCommitAfterFallback(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 6)
	own := &holder{}
	for i := 0; i < 6; i++ {
		mustRecord(t, v, own, i)
	}
	cp := NewMemory()
	heads, slabs := splitFiles(m)
	for _, n := range slabs {
		cp.CopyFile(m, n)
	}
	mustRecord(t, v, own, 100) // one commit lands between slabs and heads
	for _, n := range heads {
		cp.CopyFile(m, n)
	}
	w, rep, err := Open(cp, unlockWith(testVK), stream(7))
	if err != nil {
		t.Fatalf("the copy did not open: %v", err)
	}
	t.Logf("opened generation %d, report %+v", w.Info().Generation, rep)
	if len(rep.FellBack) == 0 {
		t.Skip("no fallback happened in this arrangement; the probe proves nothing")
	}
	out, err := record(t, w, &holder{}, 500)
	t.Logf("first recording after the fallback: outcome=%s code=%q err=%v", out, Code(err), err)
	if out != Recorded {
		t.Errorf("DEFECT: a vessel that fell back cannot record: %s, code %q", out, Code(err))
	}
	// And it must stay so after a reopen.
	w2, rep2, err := Open(cp, unlockWith(testVK), stream(8))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	out, err = record(t, w2, &holder{}, 501)
	t.Logf("after reopen: generation %d, report %+v, outcome=%s code=%q", w2.Info().Generation, rep2, out, Code(err))
}

// Probe 2: V2 inside its own bound, under adversarial interleavings. Every
// file is copied at an arbitrary moment among at most R commits.
func TestCopyInterleavings(t *testing.T) {
	bad := 0
	for seed := uint64(1); seed <= 400; seed++ {
		r := rand.New(rand.NewPCG(seed, seed*7919))
		k := 1 + r.IntN(Retention) // 1..R commits start during the copy
		m := NewMemory()
		v, cerr := Create(m, Params{SlabLog2: 18, Slabs: 96, Iter: 1, Rand: stream(byte(seed))}, testVK, nil, Root{})
		if cerr != nil {
			t.Fatal(cerr)
		}
		own := &holder{}
		pre := 3 + r.IntN(9)
		for i := 0; i < pre; i++ {
			if r.IntN(4) == 0 {
				bigRecord(t, v, own, i)
			} else {
				mustRecord(t, v, own, i)
			}
		}
		began := v.Info().Generation
		names := m.FileNames()
		when := map[string]int{}
		for _, n := range names {
			when[n] = r.IntN(k + 1)
		}
		cp := NewMemory()
		for c := 0; c <= k; c++ {
			for _, n := range names {
				if when[n] == c {
					cp.CopyFile(m, n)
				}
			}
			if c < k {
				if r.IntN(3) == 0 {
					bigRecord(t, v, own, 1000+c)
				} else {
					mustRecord(t, v, own, 1000+c)
				}
			}
		}
		w, rep, err := Open(cp, unlockWith(testVK), stream(7))
		if err != nil {
			bad++
			t.Errorf("seed %d, k=%d: the copy did not open: %v", seed, k, err)
			continue
		}
		if g := w.Info().Generation; g < began {
			bad++
			t.Errorf("seed %d, k=%d: opened %d, older than %d when the copy began (report %+v)", seed, k, g, began, rep)
			continue
		}
		serr := w.Scan(func(h Header, ref Ref) error { _, e := w.Body(ref); return e })
		if serr != nil {
			bad++
			t.Errorf("seed %d, k=%d: opened generation %d but a record does not read: %v", seed, k, w.Info().Generation, serr)
		}
	}
	t.Logf("400 schedules, %d failures", bad)
}

// Probe 3: which slot cells does Open hand to Unlock? A cell that was
// replaced in the newest generation must not keep opening the vessel through
// an older head file.
func TestStaleSlotCell(t *testing.T) {
	m := NewMemory()
	old := bytes.Repeat([]byte{0xAA}, CellSize)
	v, err := Create(m, Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(3)}, testVK, [][]byte{old},
		Root{})
	if err != nil {
		t.Fatal(err)
	}
	own := &holder{}
	mustRecord(t, v, own, 1)
	// The cell is replaced, as a removed passphrase would be.
	tx, err := v.Begin(own)
	if err != nil {
		t.Fatal(err)
	}
	cells := v.Slots()
	cells[0] = bytes.Repeat([]byte{0xBB}, CellSize)
	tx.SetSlots(cells)
	if out, err := tx.Commit(); err != nil || out != Recorded {
		t.Fatalf("slot commit: %s %v", out, err)
	}
	for extra := 0; extra <= 4; extra++ {
		seen := 0
		onlyOld := func(slots [][]byte, salt []byte, iter int) ([]byte, error) {
			seen++
			if len(slots) > 0 && bytes.Equal(slots[0], old) {
				return testVK, nil
			}
			return nil, fmt.Errorf("no cell opens")
		}
		w, _, err := Open(m, onlyOld, stream(9))
		if err == nil {
			cur := w.Slots()
			t.Logf("after %d further commits: the OLD cell still opens the vessel at generation %d (unlock calls %d); the opened generation holds the old cell: %v",
				extra, w.Info().Generation, seen, bytes.Equal(cur[0], old))
			if !bytes.Equal(cur[0], old) {
				t.Errorf("after %d further commits a replaced cell opens a generation that no longer holds it", extra)
			}
		} else {
			t.Logf("after %d further commits: the old cell no longer opens: %v", extra, err)
		}
		mustRecord(t, v, own, 50+extra)
	}
}

// Probe 4: the log of Medium calls during ordinary recordings: only reads of
// anything, and writes of existing names with unchanged length.
func TestNoCreateNoRemoveDuringRecordings(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 9)
	own := &holder{}
	before := m.Sizes()
	m.Record(true)
	for i := 0; i < 200; i++ {
		mustRecord(t, v, own, i)
	}
	for _, c := range m.Log() {
		switch c.Op {
		case "mkdir", "remove":
			t.Errorf("a recording called %s %s", c.Op, c.Name)
		case "write":
			if n, ok := before[c.Name]; !ok || n != c.Len {
				t.Errorf("a recording wrote %s with %d bytes; before: %d (known %v)", c.Name, c.Len, n, ok)
			}
		}
	}
}
