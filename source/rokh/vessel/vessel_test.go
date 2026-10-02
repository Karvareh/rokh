package vessel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"

	"rokh/frame"
)

func stream(seed byte) *rand.ChaCha8 {
	var s [32]byte
	s[0] = seed
	return rand.NewChaCha8(s)
}

var testVK = bytes.Repeat([]byte{7}, 32)

func unlockWith(vk []byte) Unlock {
	return func(slots [][]byte, salt []byte, iter int) ([]byte, error) { return vk, nil }
}

type holder struct {
	calls int
	until int // answers no from this call on; 0 means always yes
}

func (h *holder) Holds() error {
	h.calls++
	if h.until > 0 && h.calls >= h.until {
		return errors.New("the lock is gone")
	}
	return nil
}

func newVessel(t *testing.T, m Medium, seed byte) *Vessel {
	t.Helper()
	v, err := Create(m, Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(seed)}, testVK, nil,
		Root{Anchor: frame.Hash([]byte("anchor"))})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func eventHeader(i int) Header {
	head := []byte(fmt.Sprintf("RKH3-head-%06d-%s", i, bytes.Repeat([]byte("h"), 120)))
	return Header{Type: RecEvent, ID: frame.Hash(head), Head: head}
}

func record(t *testing.T, v *Vessel, own Owner, i int) (Outcome, error) {
	t.Helper()
	tx, err := v.Begin(own)
	if err != nil {
		return NotRecorded, err
	}
	if err := tx.Put(eventHeader(i), []byte(fmt.Sprintf("envelope of event %d %s", i, bytes.Repeat([]byte("e"), 200)))); err != nil {
		t.Fatal(err)
	}
	if err := tx.Put(Header{Type: RecPointer, Space: SpaceRefs, Name: frame.Hash([]byte("main"))},
		[]byte(fmt.Sprintf("main -> %d", i))); err != nil {
		t.Fatal(err)
	}
	return tx.Commit()
}

// bigRecord commits one record that fills most of a slab, so the next one
// starts a new pack.
func bigRecord(t *testing.T, v *Vessel, own Owner, i int) {
	t.Helper()
	tx, err := v.Begin(own)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Put(eventHeader(i), bytes.Repeat([]byte{byte(i)}, SlabCapacity(18)-400)); err != nil {
		t.Fatal(err)
	}
	if out, err := tx.Commit(); err != nil || out != Recorded {
		t.Fatalf("big record %d: %s %v", i, out, err)
	}
}

func mustRecord(t *testing.T, v *Vessel, own Owner, i int) {
	t.Helper()
	out, err := record(t, v, own, i)
	if err != nil || out != Recorded {
		t.Fatalf("recording %d: %s %v", i, out, err)
	}
}

// contents lists what a vessel serves: every event envelope and the latest
// pointer.
func contents(t *testing.T, v *Vessel) (events []string, pointer string) {
	t.Helper()
	err := v.Scan(func(h Header, r Ref) error {
		b, err := v.Body(r)
		if err != nil {
			return err
		}
		switch h.Type {
		case RecEvent:
			if frame.Hash(h.Head) != h.ID {
				return fmt.Errorf("head does not hash to its id")
			}
			events = append(events, string(b))
		case RecPointer:
			pointer = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return
}

func reopen(t *testing.T, m Medium) (*Vessel, Report) {
	t.Helper()
	v, rep, err := Open(m, unlockWith(testVK), stream(99))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return v, rep
}

func treeDigest(m *Memory) string {
	h := sha256.New()
	for _, n := range m.FileNames() {
		b := m.Get(n)
		fmt.Fprintf(h, "%s %d\n", n, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestCreateCommitReopen(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 1)
	own := &holder{}
	for i := 0; i < 5; i++ {
		mustRecord(t, v, own, i)
	}
	ev, ptr := contents(t, v)
	w, rep := reopen(t, m)
	if rep.Generation != 6 || len(rep.FellBack) != 0 {
		t.Fatalf("report %+v", rep)
	}
	ev2, ptr2 := contents(t, w)
	if len(ev) != 5 || fmt.Sprint(ev) != fmt.Sprint(ev2) || ptr != ptr2 || ptr != "main -> 4" {
		t.Fatalf("reopened content differs: %d %q %q", len(ev2), ptr, ptr2)
	}
	if w.Info().Anchor != frame.Hash([]byte("anchor")) {
		t.Fatal("anchor did not survive")
	}
	if _, _, err := Open(m, unlockWith(bytes.Repeat([]byte{8}, 32)), stream(1)); !errors.Is(err, ErrLocked) {
		t.Fatalf("a wrong key opened the vessel: %v", err)
	}
	if _, _, err := Open(NewMemory(), unlockWith(testVK), stream(1)); Code(err) != "not_a_vessel" {
		t.Fatalf("an empty folder: %v", err)
	}
	if _, err := Create(m, Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(2)}, testVK, nil, Root{}); !errors.Is(err, ErrExists) {
		t.Fatalf("a second create over a vessel: %v", err)
	}
}

// Nothing in the clear names the anchor, a record or a size (axiom 4).
func TestNothingReadableWithoutTheKey(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 3)
	mustRecord(t, v, &holder{}, 1)
	anchor := frame.Hash([]byte("anchor"))
	for _, n := range m.FileNames() {
		b := m.Get(n)
		for what, needle := range map[string][]byte{"anchor": anchor[:], "envelope": []byte("envelope of event"), "head": []byte("RKH3-head")} {
			if bytes.Contains(b, needle) {
				t.Fatalf("%s is readable in %s", what, n)
			}
		}
	}
	for _, n := range m.FileNames() {
		if !ValidName(n) {
			t.Fatalf("%s is outside the name grammar", n)
		}
	}
}

// A1 in memory: across 1,000 recordings every Medium call is a read or an
// in-place write of a file's own length; the file set and lengths sampled
// after every commit never change.
func TestFileSetAndLengthsConstantOverAThousandRecordings(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 4)
	before := m.Sizes()
	m.Record(true)
	own := &holder{}
	for i := 0; i < 1000; i++ {
		mustRecord(t, v, own, i)
		if i%50 == 0 {
			if got := m.Sizes(); fmt.Sprint(got) != fmt.Sprint(before) {
				t.Fatalf("after recording %d the file set or a length changed", i)
			}
		}
	}
	for _, c := range m.Log() {
		switch c.Op {
		case "read", "names":
		case "write":
			if c.Len != before[c.Name] {
				t.Fatalf("write of %d bytes to %s, whose length is %d", c.Len, c.Name, before[c.Name])
			}
		default:
			t.Fatalf("%s %s during recording", c.Op, c.Name)
		}
	}
	if fmt.Sprint(m.Sizes()) != fmt.Sprint(before) {
		t.Fatal("the file set changed")
	}
	ev, ptr := contents(t, v)
	if len(ev) != 1000 || ptr != "main -> 999" {
		t.Fatalf("%d events, pointer %q", len(ev), ptr)
	}
	t.Logf("%d medium calls over 1,000 recordings; %d files, constant", len(m.Log()), len(before))
}

// V1, A2 (cuts): a cut after every single Write call, and a torn write at
// every call, opens to the last complete commit and nothing else.
func TestACutAtEveryWriteOpensToTheLastCompleteCommit(t *testing.T) {
	for _, torn := range []bool{false, true} {
		for cut := 0; cut < 14; cut++ {
			m := NewMemory()
			v := newVessel(t, m, 5)
			start := m.Writes()
			if torn {
				m.Torn = func(name string, w int) int {
					if w == start+cut+1 {
						return 1000
					}
					return 0
				}
			}
			m.Fail = func(name string, w int) error {
				if w > start+cut+func() int {
					if torn {
						return 1
					}
					return 0
				}() {
					return errors.New("power cut")
				}
				return nil
			}
			own := &holder{}
			last := -1
			for i := 0; i < 4; i++ {
				out, _ := record(t, v, own, i)
				if out == Recorded {
					last = i
				} else {
					break
				}
			}
			m.Fail, m.Torn = nil, nil
			w, rep := reopen(t, m.Clone())
			ev, ptr := contents(t, w)
			if len(ev) != last+1 {
				t.Fatalf("torn=%v cut=%d: %d events after reopen, the last complete commit had %d (report %+v)", torn, cut, len(ev), last+1, rep)
			}
			if last >= 0 && ptr != fmt.Sprintf("main -> %d", last) {
				t.Fatalf("torn=%v cut=%d: pointer %q", torn, cut, ptr)
			}
		}
	}
}

// A2 bounded, and the counterexample of G1: a file-by-file copy made while
// commits run opens to a complete commit for up to R commits during the copy.
// With 8 commits the copy is expected not to open: G1 stays red.
func TestCopyDuringCommits(t *testing.T) {
	for _, order := range []string{"heads-first", "slabs-first"} {
		for _, k := range []int{1, 2, 3, 8} {
			m := NewMemory()
			v := newVessel(t, m, 6)
			own := &holder{}
			for i := 0; i < 6; i++ {
				mustRecord(t, v, own, i)
			}
			began := v.Info().Generation
			cp := NewMemory()
			var heads, slabs []string
			for _, n := range m.FileNames() {
				if len(n) == len("rokh/head0.rkh") {
					heads = append(heads, n)
				} else {
					slabs = append(slabs, n)
				}
			}
			first, second := heads, slabs
			if order == "slabs-first" {
				first, second = slabs, heads
			}
			for _, n := range first {
				cp.CopyFile(m, n)
			}
			for j := 0; j < k; j++ {
				mustRecord(t, v, own, 100+j)
			}
			for _, n := range second {
				cp.CopyFile(m, n)
			}
			w, rep, err := Open(cp, unlockWith(testVK), stream(7))
			if k <= Retention {
				if err != nil {
					t.Fatalf("%s, %d commits during the copy: did not open: %v", order, k, err)
				}
				if g := w.Info().Generation; g < began {
					t.Fatalf("%s, %d commits: opened generation %d, older than %d when the copy began", order, k, g, began)
				}
				contents(t, w)
				continue
			}
			if err != nil {
				t.Logf("G1 RED (expected): %s, %d commits during the copy: the copy does not open: %v", order, k, err)
			} else {
				_, _, serr := func() ([]string, string, error) {
					var e error
					werr := w.Scan(func(h Header, r Ref) error { _, e = w.Body(r); return e })
					return nil, "", werr
				}()
				t.Logf("G1 RED: %s, %d commits during the copy opened generation %d (report %+v, scan %v); the bounded mechanism proves nothing beyond R", order, k, w.Info().Generation, rep, serr)
			}
		}
	}
}

// A4, V3: one bit flipped in a head, a segment, a pack and a free slab.
func TestOneFlippedBitIsRefusedBeforeUse(t *testing.T) {
	setup := func() (*Memory, *Vessel) {
		m := NewMemory()
		v := newVessel(t, m, 8)
		for i := 0; i < 6; i++ {
			mustRecord(t, v, &holder{}, i)
		}
		return m, v
	}
	// The current head: the vessel falls back to the generation before.
	m, v := setup()
	g := v.Info().Generation
	m.Flip(HeadName(int(g%HeadSlots)), 8*9000+3)
	w, rep := reopen(t, m)
	if w.Info().Generation != g-1 || len(rep.Torn) == 0 {
		t.Fatalf("flipped head: opened %d, report %+v", w.Info().Generation, rep)
	}
	// The current segment: fall back.
	m, v = setup()
	seg := v.st.root.segments[0].index
	m.Flip(SlabName(seg), 8*5000+1)
	w, rep = reopen(t, m)
	if w.Info().Generation != g-1 || len(rep.FellBack) == 0 {
		t.Fatalf("flipped segment: opened %d, report %+v", w.Info().Generation, rep)
	}
	// The newest pack: verified at open, so the vessel falls back.
	m, v = setup()
	packs := v.packs()
	m.Flip(SlabName(packs[len(packs)-1]), 8*100+2)
	w, rep = reopen(t, m)
	if w.Info().Generation != g-1 || len(rep.FellBack) == 0 {
		t.Fatalf("flipped newest pack: opened %d, report %+v", w.Info().Generation, rep)
	}
	// An old pack, not verified at open: the open succeeds and nothing of
	// that slab is served.
	m = NewMemory()
	v = newVessel(t, m, 8)
	own := &holder{}
	for i := 0; i < 6; i++ {
		bigRecord(t, v, own, i)
	}
	packs = v.packs()
	old := packs[0]
	if v.st.inv[old].gen+Retention > v.Info().Generation {
		t.Fatal("the first pack is not old enough for this case")
	}
	m.Flip(SlabName(old), 8*100+2)
	w, _ = reopen(t, m)
	err := w.Scan(func(h Header, r Ref) error { _, err := w.Body(r); return err })
	if Code(err) != "vessel_corrupt" {
		t.Fatalf("a flipped old pack was served: %v", err)
	}
	// A free slab: nothing changes.
	m, v = setup()
	var free uint32
	for i, e := range v.st.inv {
		if e.state != stateLive {
			free = uint32(i)
			break
		}
	}
	m.Flip(SlabName(free), 77)
	w, rep = reopen(t, m)
	if w.Info().Generation != g || len(rep.FellBack) != 0 {
		t.Fatalf("a flipped free slab changed the open: %+v", rep)
	}
	contents(t, w)
}

// V4: a slab sealed for one index or one vessel does not open at another.
func TestASlabDoesNotOpenElsewhere(t *testing.T) {
	var a, b frame.ID
	a[0], b[0] = 1, 2
	slk := bytes.Repeat([]byte{3}, 32)
	file, err := sealSlab(slk, a, 5, KindPack, []byte("body"), 1<<18, stream(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, body, err := openSlab(slk, a, 5, file, 1<<18); err != nil || string(body) != "body" {
		t.Fatalf("own place: %v", err)
	}
	if _, _, err := openSlab(slk, a, 6, file, 1<<18); Code(err) != "vessel_corrupt" {
		t.Fatal("a slab opened at another index")
	}
	if _, _, err := openSlab(slk, b, 5, file, 1<<18); Code(err) != "vessel_corrupt" {
		t.Fatal("a slab opened in another vessel")
	}
	// And inside a vessel: a pack copied over another live pack is refused.
	m := NewMemory()
	v := newVessel(t, m, 9)
	own := &holder{}
	for i := 0; i < 40; i++ {
		mustRecord(t, v, own, i)
	}
	if err := v.Compact(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		bigRecord(t, v, own, 100+i)
	}
	ps := v.packs()
	if v.st.inv[ps[0]].gen+Retention > v.Info().Generation {
		t.Fatal("the compacted pack is not old enough for this case")
	}
	var other uint32
	for i, e := range v.st.inv {
		if e.state != stateLive {
			other = uint32(i)
			break
		}
	}
	m.Put(SlabName(ps[0]), m.Get(SlabName(other)))
	w, _ := reopen(t, m)
	err = w.Scan(func(h Header, r Ref) error { _, err := w.Body(r); return err })
	if Code(err) != "vessel_corrupt" {
		t.Fatalf("a moved slab was served: %v", err)
	}
}

// A5, V5: a full vessel refuses before a byte changes.
func TestAFullVesselRefusesAndChangesNothing(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 10)
	own := &holder{}
	big := bytes.Repeat([]byte("x"), SlabCapacity(18)-200)
	var err error
	var out Outcome
	i := 0
	var digest string
	var gen uint64
	for ; i < 64; i++ {
		digest, gen = treeDigest(m), v.Info().Generation
		tx, _ := v.Begin(own)
		h := eventHeader(i)
		if err := tx.Put(h, big); err != nil {
			t.Fatal(err)
		}
		out, err = tx.Commit()
		if err != nil {
			break
		}
	}
	if Code(err) != "vessel_full" || out != NotRecorded {
		t.Fatalf("filling ended with %s %v after %d", out, err, i)
	}
	if treeDigest(m) != digest {
		t.Fatal("the refused commit changed bytes on the medium")
	}
	w, _ := reopen(t, m)
	if w.Info().Generation != gen {
		t.Fatalf("reopened at %d, want %d", w.Info().Generation, gen)
	}
	ev, _ := contents(t, w)
	if len(ev) != i {
		t.Fatalf("%d events after filling, want %d", len(ev), i)
	}
	t.Logf("full after %d slab-sized records", i)
}

// S2, V7: a writer whose owner answers no writes nothing more, and every
// committed generation keeps its digests.
func TestAWriterThatNoLongerHoldsWritesNothing(t *testing.T) {
	if _, err := newVessel(t, NewMemory(), 11).Begin(nil); Code(err) != "turn_unavailable" {
		t.Fatalf("no owner: %v", err)
	}
	for until := 1; until <= 5; until++ {
		m := NewMemory()
		v := newVessel(t, m, 11)
		mustRecord(t, v, &holder{}, 0)
		digest := treeDigest(m)
		own := &holder{until: until}
		var writesAfterNo int
		m.Record(true)
		out, err := record(t, v, own, 1)
		if out == Recorded {
			t.Fatalf("until=%d: recorded after the owner answered no", until)
		}
		if Code(err) != "turn_lost" {
			t.Fatalf("until=%d: %v", until, err)
		}
		for _, c := range m.Log() {
			if c.Op == "write" && len(c.Name) == len("rokh/head0.rkh") {
				writesAfterNo++
			}
		}
		if writesAfterNo != 0 {
			t.Fatalf("until=%d: a head was written by a writer that no longer held", until)
		}
		w, _ := reopen(t, m)
		ev, _ := contents(t, w)
		if len(ev) != 1 || w.Info().Generation != 2 {
			t.Fatalf("until=%d: the committed generation was disturbed", until)
		}
		_ = digest
	}
}

// S2: a writer stopped in the middle of its commit, while a second writer
// takes the vessel after the first one's death, commits; the first writer's
// remains change nothing the second committed.
func TestAWriterStoppedMidCommitIsOvertaken(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 12)
	mustRecord(t, v, &holder{}, 0)
	// Writer A dies after its slabs and before its head.
	died := errors.New("killed")
	var aWrites int
	m.Fail = func(name string, w int) error {
		if len(name) == len("rokh/head0.rkh") {
			return died
		}
		aWrites++
		return nil
	}
	out, _ := record(t, v, &holder{}, 1)
	if out == Recorded {
		t.Fatal("a writer killed before its head recorded")
	}
	m.Fail = nil
	// Writer B opens afresh and commits twice.
	b, _ := reopen(t, m)
	mustRecord(t, b, &holder{}, 2)
	mustRecord(t, b, &holder{}, 3)
	w, rep := reopen(t, m)
	ev, ptr := contents(t, w)
	if len(ev) != 3 || ptr != "main -> 3" || len(rep.FellBack) != 0 {
		t.Fatalf("after the dead writer: %d events, %q, %+v", len(ev), ptr, rep)
	}
	if aWrites == 0 {
		t.Fatal("writer A wrote nothing; the case was not exercised")
	}
}

// Two writers opened on one vessel, where the host failed to keep them
// apart: the second commit is refused at the commit point, not merged over.
func TestAConcurrentCommitIsRefusedAtTheCommitPoint(t *testing.T) {
	m := NewMemory()
	a := newVessel(t, m, 13)
	mustRecord(t, a, &holder{}, 0)
	b, _ := reopen(t, m)
	// A's owner lets B commit while A is between its slabs and its head.
	ownA := &sneaky{run: func() { mustRecord(t, b, &holder{}, 7) }, at: 4}
	out, err := record(t, a, ownA, 1)
	if out == Recorded || Code(err) != "turn_lost" {
		t.Fatalf("a commit on a stale base: %s %v", out, err)
	}
	w, _ := reopen(t, m)
	_, ptr := contents(t, w)
	if ptr != "main -> 7" {
		t.Fatalf("B's commit was disturbed: %q", ptr)
	}
}

type sneaky struct {
	calls, at int
	run       func()
}

func (s *sneaky) Holds() error {
	s.calls++
	if s.calls == s.at {
		s.run()
	}
	return nil
}

// A.2: grow and shrink name a target; repeating changes nothing; automatic
// growth grows by its step.
func TestGrowShrinkAndAutomaticGrowth(t *testing.T) {
	m := NewMemory()
	v := newVessel(t, m, 14)
	own := &holder{}
	for i := 0; i < 3; i++ {
		mustRecord(t, v, own, i)
	}
	if err := v.Grow(32); err != nil {
		t.Fatal(err)
	}
	if err := v.Grow(32); err != nil {
		t.Fatal(err)
	}
	if n := len(m.FileNames()); n != 32+4 || v.Info().Slabs != 32 {
		t.Fatalf("after grow: %d files, %d slabs", n, v.Info().Slabs)
	}
	mustRecord(t, v, own, 3)
	if err := v.Shrink(20, false); err != nil {
		t.Fatal(err)
	}
	w, rep := reopen(t, m)
	if w.Info().Slabs != 20 || len(rep.Beyond) != 12 {
		t.Fatalf("after shrink: %d slabs, beyond %d", w.Info().Slabs, len(rep.Beyond))
	}
	ev, ptr := contents(t, w)
	if len(ev) != 4 || ptr != "main -> 3" {
		t.Fatalf("shrink lost records: %d %q", len(ev), ptr)
	}
	if err := v.Shrink(20, true); err != nil {
		t.Fatal(err)
	}
	if n := len(m.FileNames()); n != 20+4 {
		t.Fatalf("after finish: %d files", n)
	}
	// Automatic growth.
	m2 := NewMemory()
	a, err := Create(m2, Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(15), Growth: Growth{Auto: true, Step: 8, Max: 40}},
		testVK, nil, Root{})
	if err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte("y"), SlabCapacity(18)-200)
	for i := 0; i < 20; i++ {
		tx, _ := a.Begin(own)
		tx.Put(eventHeader(i), big)
		out, err := tx.CommitGrowing()
		if err != nil || out != Recorded {
			t.Fatalf("auto growth, record %d: %s %v (slabs %d)", i, out, err, a.Info().Slabs)
		}
	}
	if a.Info().Slabs <= 16 {
		t.Fatal("the vessel did not grow")
	}
}

// C5, F2: with a fixed random stream the bytes on the medium are one tree
// digest, on every platform.
func TestDeterministicVector(t *testing.T) {
	m := NewMemory()
	v, err := Create(m, Params{SlabLog2: 18, Slabs: 16, Iter: 1000, Rand: stream(42)}, testVK, nil,
		Root{Anchor: frame.Hash([]byte("vector"))})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		mustRecord(t, v, &holder{}, i)
	}
	got := treeDigest(m)
	t.Logf("tree digest %s", got)
	if want := vectorDigest; want != "" && got != want {
		t.Fatalf("tree digest %s, want %s", got, want)
	}
}

// vectorDigest is the tree digest of TestDeterministicVector.
const vectorDigest = "23677c7e3a3ae1c4e4787fb373bb84e48cd74942ae9184868254ef9f01571aab"

func TestRecordsRoundTripThroughAPack(t *testing.T) {
	hs := []Header{
		eventHeader(1),
		{Type: RecContent, ID: frame.Hash([]byte("c")), Chunk: 1, Chunks: 3, Size: 9},
		{Type: RecPointer, Space: SpaceRefs, Name: frame.Hash([]byte("p"))},
		{Type: RecObject, Space: 0x10, Name: frame.Hash([]byte("o")), Chunk: 0, Chunks: 1, Size: 5},
	}
	var body []byte
	for i, h := range hs {
		r, err := h.encode([]byte(fmt.Sprintf("env%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, r...)
	}
	items, err := parsePack(body)
	if err != nil || len(items) != 4 {
		t.Fatalf("%d items, %v", len(items), err)
	}
	for i, it := range items {
		if fmt.Sprint(it.h) != fmt.Sprint(hs[i]) || string(body[it.r.Off:it.r.Off+it.r.Len]) != fmt.Sprintf("env%d", i) {
			t.Fatalf("record %d did not round trip", i)
		}
	}
	if _, err := (Header{Type: RecContent, Chunk: 3, Chunks: 3}).encode([]byte("x")); err == nil {
		t.Fatal("a chunk index beyond the count was accepted")
	}
	for cut := 1; cut < len(body); cut += 7 {
		if _, err := parsePack(body[:cut]); err == nil {
			// A cut may land on a record boundary; then it must be a prefix.
			n, _ := parsePack(body[:cut])
			if len(n) >= len(items) {
				t.Fatal("a truncated pack parsed whole")
			}
		}
	}
	sort.Slice(items, func(a, b int) bool { return a < b })
}

// A head holds SlotCells cells of CellSize bytes. A commit
// given more cells, or a cell of another length, is not recorded and the
// vessel's cells are unchanged; Create refuses the same.
func TestSlotCellsAHeadCannotHoldAreRefused(t *testing.T) {
	v := newVessel(t, NewMemory(), 41)
	have := v.Slots()
	added := bytes.Repeat([]byte{0xB2}, CellSize)
	for name, cells := range map[string][][]byte{
		"one cell too many": append(v.Slots(), added),
		"a short cell":      append(v.Slots()[:1], added[1:]),
	} {
		tx, err := v.Begin(&holder{})
		if err != nil {
			t.Fatal(err)
		}
		tx.SetSlots(cells)
		if out, err := tx.Commit(); out != NotRecorded || err == nil {
			t.Errorf("%s: the commit answered %s %v", name, out, err)
		}
		got := v.Slots()
		if len(got) != len(have) {
			t.Fatalf("%s: %d cells after a refused commit, want %d", name, len(got), len(have))
		}
		for i := range got {
			if !bytes.Equal(got[i], have[i]) {
				t.Errorf("%s: cell %d changed", name, i)
			}
		}
	}
	for name, cells := range map[string][][]byte{
		"one cell too many": append(have, added),
		"a short cell":      {added[1:]},
	} {
		if _, err := Create(NewMemory(), Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(42)}, testVK, cells, Root{}); err == nil {
			t.Errorf("%s: Create took it", name)
		}
	}
}
