package vessel

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"rokh/frame"
)

// Outcome is the answer to "was it recorded?".
type Outcome string

const (
	Recorded    Outcome = "recorded"
	NotRecorded Outcome = "not-recorded"
	Unknown     Outcome = "unknown"
)

// MaxHead is the largest signed event head a record carries (contract 3.4).
const MaxHead = 1024

// Header is a record's clear part inside its pack (contract 2.5).
type Header struct {
	Type   byte
	ID     frame.ID // event and content
	Head   []byte   // event: the signed head
	Space  byte     // pointer and object
	Name   frame.ID // pointer and object
	Chunk  uint32   // content and object
	Chunks uint32
	Size   uint64
}

// Ref locates one record's envelope.
type Ref struct {
	Slab uint32
	Gen  uint64
	Off  uint32
	Len  uint32
}

func (h Header) encode(envelope []byte) ([]byte, error) {
	var b []byte
	switch h.Type {
	case RecEvent:
		if len(h.Head) == 0 || len(h.Head) > MaxHead {
			return nil, fmt.Errorf("vessel: event head of %d bytes", len(h.Head))
		}
		b = append(b, h.ID[:]...)
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(h.Head)))
		b = append(b, l[:]...)
		b = append(b, h.Head...)
	case RecContent:
		b = append(b, h.ID[:]...)
		b = append(b, u32(h.Chunk)...)
		b = append(b, u32(h.Chunks)...)
		b = append(b, u64(h.Size)...)
	case RecPointer:
		b = append(b, h.Space)
		b = append(b, h.Name[:]...)
	case RecObject:
		b = append(b, h.Space)
		b = append(b, h.Name[:]...)
		b = append(b, u32(h.Chunk)...)
		b = append(b, u32(h.Chunks)...)
		b = append(b, u64(h.Size)...)
	default:
		return nil, fmt.Errorf("vessel: unknown record type 0x%02x", h.Type)
	}
	if (h.Type == RecContent || h.Type == RecObject) && (h.Chunks == 0 || h.Chunk >= h.Chunks) {
		return nil, fmt.Errorf("vessel: chunk %d of %d", h.Chunk, h.Chunks)
	}
	if len(envelope) == 0 {
		return nil, fmt.Errorf("vessel: empty envelope")
	}
	rec := []byte{h.Type}
	rec = append(rec, u32(uint32(len(b)+len(envelope)))...)
	rec = append(rec, b...)
	return append(rec, envelope...), nil
}

// parsePack reads the records of one pack body.
func parsePack(body []byte) ([]item, error) {
	var out []item
	off := 0
	for off < len(body) {
		if len(body)-off < 5 {
			return nil, wrap(ErrCorrupt, "truncated record")
		}
		t := body[off]
		n := int(binary.BigEndian.Uint32(body[off+1 : off+5]))
		start := off + 5
		if n < 1 || n > len(body)-start {
			return nil, wrap(ErrCorrupt, "record length")
		}
		rb := body[start : start+n]
		h := Header{Type: t}
		var hl int
		switch t {
		case RecEvent:
			if len(rb) < 34 {
				return nil, wrap(ErrCorrupt, "event record")
			}
			copy(h.ID[:], rb[:32])
			l := int(binary.BigEndian.Uint16(rb[32:34]))
			if l == 0 || l > MaxHead || len(rb) < 34+l {
				return nil, wrap(ErrCorrupt, "event head length")
			}
			h.Head = append([]byte(nil), rb[34:34+l]...)
			hl = 34 + l
		case RecContent:
			if len(rb) < 48 {
				return nil, wrap(ErrCorrupt, "content record")
			}
			copy(h.ID[:], rb[:32])
			h.Chunk = binary.BigEndian.Uint32(rb[32:36])
			h.Chunks = binary.BigEndian.Uint32(rb[36:40])
			h.Size = binary.BigEndian.Uint64(rb[40:48])
			hl = 48
		case RecPointer:
			if len(rb) < 33 {
				return nil, wrap(ErrCorrupt, "pointer record")
			}
			h.Space = rb[0]
			copy(h.Name[:], rb[1:33])
			hl = 33
		case RecObject:
			if len(rb) < 49 {
				return nil, wrap(ErrCorrupt, "object record")
			}
			h.Space = rb[0]
			copy(h.Name[:], rb[1:33])
			h.Chunk = binary.BigEndian.Uint32(rb[33:37])
			h.Chunks = binary.BigEndian.Uint32(rb[37:41])
			h.Size = binary.BigEndian.Uint64(rb[41:49])
			hl = 49
		default:
			return nil, wrap(ErrCorrupt, "unknown record type 0x%02x", t)
		}
		if len(rb) == hl {
			return nil, wrap(ErrCorrupt, "record without envelope")
		}
		out = append(out, item{h: h, r: Ref{Off: uint32(start + hl), Len: uint32(n - hl)}})
		off = start + n
	}
	return out, nil
}

// Tx is one recording in preparation. Nothing reaches the medium before
// Commit.
type Tx struct {
	v        *Vessel
	own      Owner
	recs     [][]byte
	slots    [][]byte
	edits    []func(*Root)
	done     bool
	base     Report // what the commit found when it chose its base
	capacity int    // the body capacity of one slab when the recording began
	slabs    int    // a grow or shrink target, 0 when unchanged
	repack   bool   // compact or shrink: drop superseded pointers and repack every live pack
}

// Begin starts a recording under the host's owner. It never waits: the host
// waited for the lock. Without an owner the vessel is read-only.
func (v *Vessel) Begin(own Owner) (*Tx, error) {
	if own == nil {
		return nil, ErrTurnUnavailable
	}
	if err := own.Holds(); err != nil {
		return nil, wrap(ErrTurnLost, "%v", err)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.own = own
	return &Tx{v: v, own: own, capacity: SlabCapacity(v.st.root.SlabLog2)}, nil
}

// Put adds one record to the recording.
func (t *Tx) Put(h Header, envelope []byte) error {
	if t.done {
		return fmt.Errorf("vessel: the recording is closed")
	}
	rec, err := h.encode(envelope)
	if err != nil {
		return err
	}
	if len(rec) > t.capacity {
		return fmt.Errorf("%w: a record of %d bytes does not fit one slab", ErrTooLarge, len(rec))
	}
	t.recs = append(t.recs, rec)
	return nil
}

// SetSlots replaces the slot cells written with this commit.
func (t *Tx) SetSlots(slots [][]byte) {
	t.slots = make([][]byte, len(slots))
	for i, s := range slots {
		t.slots[i] = append([]byte(nil), s...)
	}
}

// SetRoot edits the root record of this commit. Generation, vessel id, token,
// slab parameters and segments are kept by the vessel whatever the edit does.
func (t *Tx) SetRoot(edit func(*Root)) { t.edits = append(t.edits, edit) }

// Base is the report of the generation the commit was built on: its number,
// and every newer generation it passed over because it did not verify.
func (t *Tx) Base() Report { return t.base }

// Abandon drops the recording. Nothing was written.
func (t *Tx) Abandon() error { t.done = true; return nil }

// pick chooses need distinct slabs from elig with the injected randomness.
func (v *Vessel) pick(elig []uint32, need int) ([]uint32, error) {
	c := append([]uint32(nil), elig...)
	var b [4]byte
	for i := 0; i < need; i++ {
		n := uint32(len(c) - i)
		limit := (^uint32(0) / n) * n
		var x uint32
		for {
			if err := readFull(v.rnd, b[:]); err != nil {
				return nil, err
			}
			x = binary.BigEndian.Uint32(b[:])
			if x < limit {
				break
			}
		}
		j := i + int(x%n)
		c[i], c[j] = c[j], c[i]
	}
	out := c[:need]
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	return out, nil
}

// pack lays records into slab bodies. start is the body of the pack that is
// rewritten with new records appended, or nil.
func pack(start []byte, recs [][]byte, capacity int) [][]byte {
	var out [][]byte
	cur := append([]byte(nil), start...)
	for _, r := range recs {
		if len(cur)+len(r) > capacity && len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
		cur = append(cur, r...)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// Commit takes the recording through the commit point (contract 2.6).
func (t *Tx) Commit() (Outcome, error) {
	if t.done {
		return NotRecorded, fmt.Errorf("vessel: the recording is closed")
	}
	t.done = true
	if t.slots != nil {
		if err := checkSlots(t.slots); err != nil {
			return NotRecorded, err
		}
	}
	v := t.v
	// One commit at a time, and no reader sees its state half replaced; it
	// holds the lock for its own bounded length only.
	v.mu.Lock()
	defer v.mu.Unlock()
	// 1. The owner still holds; the highest whole generation is the base.
	if err := t.own.Holds(); err != nil {
		return NotRecorded, wrap(ErrTurnLost, "%v", err)
	}
	if v.st.head >= 0 { // a vessel being created has no generation yet
		if err := v.refresh(); err != nil {
			return NotRecorded, err
		}
	}
	// The base is the highest generation that verifies. When the vessel fell
	// back to it, the report says so, and the caller reads it from the
	// transaction; the commit overwrites the head of the generation that did
	// not verify.
	t.base = v.report
	base := v.st
	n := base.root.Generation + 1
	root := base.root
	for _, e := range t.edits {
		e(&root)
	}
	root.Generation, root.Vessel, root.SlabLog2 = n, base.root.Vessel, base.root.SlabLog2
	root.Slabs = base.root.Slabs
	k := root.SlabLog2
	size, capacity := slabSize(k), SlabCapacity(k)
	count := base.root.Slabs
	if t.slabs != 0 {
		count = t.slabs
	}
	inv := make([]entry, count)
	copy(inv, base.inv)
	// Records: rewrite the last pack when the new records fit behind it.
	var bodies [][]byte
	var freed []uint32
	if t.repack {
		var all [][]byte
		for _, p := range v.packs() {
			freed = append(freed, p)
		}
		live, err := v.liveRecords()
		if err != nil {
			return NotRecorded, err
		}
		all = append(all, live...)
		all = append(all, t.recs...)
		bodies = pack(nil, all, capacity)
	} else if len(t.recs) > 0 {
		var start []byte
		if ps := v.packs(); len(ps) > 0 {
			last := ps[len(ps)-1]
			body, err := v.slab(last)
			if err != nil {
				return NotRecorded, err
			}
			if len(body)+len(t.recs[0]) <= capacity {
				start = body
				freed = append(freed, last)
			}
		}
		bodies = pack(start, t.recs, capacity)
	}
	for _, f := range freed {
		if int(f) < len(inv) {
			inv[f] = entry{state: stateFree, gen: n}
		}
	}
	for _, s := range base.root.segments {
		if int(s.index) < len(inv) {
			inv[s.index] = entry{state: stateFree, gen: n}
		}
	}
	segs := segmentsFor(count)
	need := len(bodies) + segs
	var elig []uint32
	for i, e := range inv {
		if i >= count {
			break
		}
		if e.state == stateNever || (e.state == stateFree && n >= e.gen+Retention) {
			if !(e.state == stateFree && e.gen == n) { // freed by this commit: live in n-1
				elig = append(elig, uint32(i))
			}
		}
	}
	if len(elig) < need {
		return NotRecorded, wrap(ErrFull, "%d slabs needed, %d free", need, len(elig))
	}
	chosen, err := v.pick(elig, need)
	if err != nil {
		return NotRecorded, err
	}
	packIdx, segIdx := chosen[:len(bodies)], chosen[len(bodies):]
	// Packs of one commit are written in ascending index, which is their order.
	sort.Slice(packIdx, func(a, b int) bool { return packIdx[a] < packIdx[b] })
	// 2. Every new or rewritten slab.
	for i, body := range bodies {
		if err := t.own.Holds(); err != nil {
			return NotRecorded, wrap(ErrTurnLost, "%v", err)
		}
		file, err := sealSlab(v.slk, root.Vessel, packIdx[i], KindPack, body, size, v.rnd)
		if err != nil {
			return NotRecorded, err
		}
		if err := v.m.Write(SlabName(packIdx[i]), file); err != nil {
			return NotRecorded, err
		}
		inv[packIdx[i]] = entry{state: stateLive, kind: KindPack, gen: n, used: uint32(len(body)), digest: sha256.Sum256(file)}
	}
	// 3. The inventory segments.
	segBody := uint32(SegmentEntries * EntrySize)
	for _, s := range segIdx {
		inv[s] = entry{state: stateLive, kind: KindSegment, gen: n, used: segBody}
	}
	root.segments = make([]segRef, segs)
	for j := 0; j < segs; j++ {
		if err := t.own.Holds(); err != nil {
			return NotRecorded, wrap(ErrTurnLost, "%v", err)
		}
		file, err := sealSlab(v.slk, root.Vessel, segIdx[j], KindSegment, encodeSegment(inv, j), size, v.rnd)
		if err != nil {
			return NotRecorded, err
		}
		if err := v.m.Write(SlabName(segIdx[j]), file); err != nil {
			return NotRecorded, err
		}
		root.segments[j] = segRef{index: segIdx[j], digest: sha256.Sum256(file)}
	}
	// 4. Still the owner, and still on n-1.
	if err := t.own.Holds(); err != nil {
		return NotRecorded, wrap(ErrTurnLost, "%v", err)
	}
	// The head files as they are now; the ones this commit does not write
	// stay as read here, and Moved compares the medium with them afterwards.
	raws := v.readHeads([HeadSlots][]byte{})
	if base.head >= 0 {
		// The highest generation that verifies, under the owner's hold: a
		// newer head that does not verify is not another writer (a review
		// F1), and a newer one that does verify stops this commit. The
		// commit's identity is compared, not only its number, so a different
		// commit under the same generation is caught too.
		cur, _, err := v.choose(raws)
		g := cur.root.Generation
		switch {
		case err != nil:
			return NotRecorded, err
		case g > n-1:
			return NotRecorded, wrap(ErrTurnLost, "another writer committed generation %d first", g)
		case g < n-1:
			return NotRecorded, wrap(ErrCorrupt, "the base generation %d no longer verifies", n-1)
		case cur.root.Token != base.root.Token:
			return NotRecorded, wrap(ErrTurnLost, "another writer replaced generation %d", g)
		}
	}
	root.Slabs = count
	if err := readFull(v.rnd, root.Token[:]); err != nil {
		return NotRecorded, err
	}
	rb, err := encodeRoot(root)
	if err != nil {
		return NotRecorded, err
	}
	slots := base.slots
	if t.slots != nil {
		slots = t.slots
	}
	num := int(n % HeadSlots)
	head, err := buildHead(v.hdk, v.salt, v.iter, slots, num, rb, v.rnd)
	if err != nil {
		return NotRecorded, err
	}
	// 5. The commit point.
	werr := v.m.Write(HeadName(num), head)
	// 6. Read it back.
	back, err := v.m.Read(HeadName(num), HeadSize)
	if err != nil {
		return Unknown, fmt.Errorf("vessel: the head could not be read back: %v (write: %v)", err, werr)
	}
	got, err := openHead(v.hdk, back, num)
	if err != nil || got.Token != root.Token {
		if werr != nil {
			return NotRecorded, werr
		}
		return NotRecorded, wrap(ErrTurnLost, "the head read back is not this commit")
	}
	if werr != nil {
		// The head reads back as this commit, but its write reported an
		// error: the bytes may sit in a cache that the flush did not empty.
		// Recorded means written, flushed and read back (U4), so this is
		// neither recorded nor a clean not-recorded. The view is
		// refreshed from the medium, whatever it now holds.
		_ = v.refresh()
		return Unknown, fmt.Errorf("vessel: the head reads back as this commit but its write failed; durability is not confirmed: %w", werr)
	}
	_, _, cells, _ := headPublic(back)
	v.st = state{root: got, inv: inv, slots: cells, head: num}
	raws[num] = back
	v.seen = raws
	v.index = nil
	v.cache = map[uint32]cached{}
	return Recorded, nil
}

// liveRecords returns every live record, with superseded pointers dropped.
func (v *Vessel) liveRecords() ([][]byte, error) {
	if err := v.buildIndex(); err != nil {
		return nil, err
	}
	last := map[[33]byte]int{}
	for i, it := range v.index {
		if it.h.Type == RecPointer {
			var k [33]byte
			k[0] = it.h.Space
			copy(k[1:], it.h.Name[:])
			last[k] = i
		}
	}
	var out [][]byte
	for i, it := range v.index {
		if it.h.Type == RecPointer {
			var k [33]byte
			k[0] = it.h.Space
			copy(k[1:], it.h.Name[:])
			if last[k] != i {
				continue
			}
		}
		env, err := v.body(it.r)
		if err != nil {
			return nil, err
		}
		rec, err := it.h.encode(env)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// ---------- size ----------

func (v *Vessel) owner() (Owner, error) {
	if v.own == nil {
		return nil, ErrTurnUnavailable
	}
	return v.own, nil
}

// Grow raises the slab count to a target (never a delta): it creates slab
// files N..N'-1 of randomness, then commits N'. Repeating it changes
// nothing. It writes under the owner of the last Begin.
func (v *Vessel) Grow(slabs int) error {
	own, done, err := v.growFiles(slabs)
	if err != nil || done {
		return err
	}
	t := &Tx{v: v, own: own, slabs: slabs}
	out, err := t.Commit()
	if err != nil {
		return err
	}
	if out != Recorded {
		return fmt.Errorf("vessel: grow was %s", out)
	}
	return nil
}

// growFiles writes the new slab files of a grow under the lock; done says the
// vessel already has that many slabs.
func (v *Vessel) growFiles(slabs int) (Owner, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	own, err := v.owner()
	if err != nil {
		return nil, false, err
	}
	if err := own.Holds(); err != nil {
		return nil, false, wrap(ErrTurnLost, "%v", err)
	}
	if err := v.refresh(); err != nil {
		return nil, false, err
	}
	cur := v.st.root.Slabs
	if slabs == cur {
		return own, true, nil
	}
	if slabs < cur {
		return nil, false, fmt.Errorf("vessel: grow to %d is below the current %d slabs", slabs, cur)
	}
	if slabs > MaxSlabs {
		return nil, false, fmt.Errorf("vessel: %d slabs is above the limit %d", slabs, MaxSlabs)
	}
	size := slabSize(v.st.root.SlabLog2)
	for d := (cur - 1) >> 10; d <= (slabs-1)>>10; d++ {
		if err := v.m.Mkdir(SlabDirName(uint32(d << 10))); err != nil && err != ErrExists {
			return nil, false, err
		}
	}
	for i := cur; i < slabs; i++ {
		if err := own.Holds(); err != nil {
			return nil, false, wrap(ErrTurnLost, "%v", err)
		}
		b := make([]byte, size)
		if err := readFull(v.rnd, b); err != nil {
			return nil, false, err
		}
		if err := v.m.Write(SlabName(uint32(i)), b); err != nil {
			return nil, false, err
		}
	}
	return own, false, nil
}

// Shrink lowers the slab count to a target. It is refused unless the live
// slabs fit; live slabs above the target move below it in the same commit.
// The tail files are removed only with finish; until then they are reported
// as beyond N and ignored.
func (v *Vessel) Shrink(slabs int, finish bool) error {
	own, cur, live, err := v.shrinkBase()
	if err != nil {
		return err
	}
	if slabs < MinSlabs {
		return fmt.Errorf("vessel: at least %d slabs", MinSlabs)
	}
	if slabs > cur {
		return fmt.Errorf("vessel: shrink to %d is above the current %d slabs", slabs, cur)
	}
	if slabs < cur {
		// Room for the moved packs, the segments, and R generations of turnover.
		if live+segmentsFor(slabs)*(Retention+1)+Retention+1 > slabs {
			return wrap(ErrFull, "the live slabs do not fit in %d", slabs)
		}
		t := &Tx{v: v, own: own, slabs: slabs, repack: true}
		out, err := t.Commit()
		if err != nil {
			return err
		}
		if out != Recorded {
			return fmt.Errorf("vessel: shrink was %s", out)
		}
	}
	if !finish {
		return nil
	}
	names, err := v.m.Names(Dir)
	if err != nil {
		return err
	}
	for _, d := range names {
		if len(d) != 3 {
			continue
		}
		files, err := v.m.Names(Dir + "/" + d)
		if err != nil {
			return err
		}
		for _, f := range files {
			p := Dir + "/" + d + "/" + f
			var idx uint32
			if !ValidName(p) {
				continue
			}
			fmt.Sscanf(f, "%08x.rkh", &idx)
			if int(idx) >= slabs {
				if err := own.Holds(); err != nil {
					return wrap(ErrTurnLost, "%v", err)
				}
				if err := v.m.Remove(p); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// shrinkBase reads, under the lock, the owner, the slab count and the live
// packs a shrink starts from.
func (v *Vessel) shrinkBase() (Owner, int, int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	own, err := v.owner()
	if err != nil {
		return nil, 0, 0, err
	}
	if err := own.Holds(); err != nil {
		return nil, 0, 0, wrap(ErrTurnLost, "%v", err)
	}
	if err := v.refresh(); err != nil {
		return nil, 0, 0, err
	}
	live := 0
	for _, e := range v.st.inv {
		if e.state == stateLive && e.kind == KindPack {
			live++
		}
	}
	return own, v.st.root.Slabs, live, nil
}

// Compact rewrites the live packs without superseded pointer records, in one
// commit, and frees the packs it replaced.
func (v *Vessel) Compact() error {
	v.mu.Lock()
	own, err := v.owner()
	v.mu.Unlock()
	if err != nil {
		return err
	}
	t := &Tx{v: v, own: own, repack: true}
	out, err := t.Commit()
	if err != nil {
		return err
	}
	if out != Recorded {
		return fmt.Errorf("vessel: compact was %s", out)
	}
	return nil
}

// Auto growth: CommitGrowing commits, and when a fixed vessel would answer
// vessel_full and the vessel grows automatically, grows one step and tries
// again.
func (t *Tx) CommitGrowing() (Outcome, error) {
	recs, slots, edits := t.recs, t.slots, t.edits
	out, err := t.Commit()
	info := t.v.Info()
	if Code(err) != "vessel_full" || !info.Growth.Auto {
		return out, err
	}
	g := info.Growth
	for Code(err) == "vessel_full" {
		cur := t.v.Info().Slabs
		if cur >= g.Max {
			return out, err
		}
		next := cur + g.Step
		if next > g.Max {
			next = g.Max
		}
		if gerr := t.v.Grow(next); gerr != nil {
			return NotRecorded, gerr
		}
		nt := &Tx{v: t.v, own: t.own, recs: recs, slots: slots, edits: edits}
		out, err = nt.Commit()
	}
	return out, err
}
