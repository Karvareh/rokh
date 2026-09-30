package vessel

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"sync"

	"rokh/frame"
)

// Medium is the host's file surface (contract 2.10). Names are relative, use
// "/", and follow the vessel grammar. It lives outside the core; tests inject
// their own.
type Medium interface {
	Read(name string, max int) ([]byte, error) // whole file, bounded; ErrNotFound when absent
	Write(name string, b []byte) error         // create or overwrite in place, then flush; no temp file, no rename
	Names(dir string) ([]string, error)        // one directory, ASCII-lowercased, sorted
	Mkdir(name string) error                   // create and grow only; ErrExists if present
	Remove(name string) error                  // shrink only
}

// Owner is the writer's hold on the vessel, made by the host (2.9). Holds
// answers nil only while this process holds the vessel.
type Owner interface{ Holds() error }

// Growth is the size policy: fixed, or automatic growth by Step slabs up to
// Max slabs.
type Growth struct {
	Auto bool
	Step int
	Max  int
}

// Params are the creation parameters. Salt is the rokh's KDF salt (32 bytes),
// inherited by every seed; when empty Create draws it from Rand and Info
// reports it.
type Params struct {
	SlabLog2, Slabs int
	Growth          Growth
	Iter            int
	Rand            io.Reader
	Salt            []byte
}

// Unlock turns the slot cells of a head file into the vessel key VK. It is
// supplied by the key layer; the vessel never sees a passphrase.
type Unlock func(slots [][]byte, salt []byte, iter int) (vk []byte, err error)

// Root is the root record of one generation (contract 2.3).
type Root struct {
	Generation uint64
	Anchor     frame.ID
	Vessel     frame.ID
	Seed       frame.ID // zero on the root vessel
	SlabLog2   int
	Slabs      int
	Growth     Growth
	Token      [16]byte
	Scopes     []string // address prefixes this vessel holds; empty means whole
	segments   []segRef
}

type segRef struct {
	index  uint32
	digest [32]byte
}

// Info describes an opened vessel.
type Info struct {
	Format     string
	Generation uint64
	Anchor     frame.ID
	Vessel     frame.ID
	Seed       frame.ID
	SlabLog2   int
	SlabSize   int
	Slabs      int
	Growth     Growth
	Scopes     []string
	Salt       []byte
	Iter       int
	Live       int   // live slabs
	Free       int   // slabs a commit of the next generation may write
	Used       int64 // bytes the live records take inside their packs
}

// Fallback says the vessel opened an older generation than the highest seen.
type Fallback struct {
	From, To uint64
	Why      string
}

// Report is what opening found (2.7). Recovery writes nothing by itself.
type Report struct {
	Generation uint64
	FellBack   []Fallback
	Torn       []int    // head files that carry the magic and do not open
	Missing    []uint32 // slab files that are absent
	WrongSize  []uint32 // slab files of the wrong length
	Beyond     []string // slab files at or beyond N
}

type entry struct {
	state  byte
	kind   byte
	gen    uint64
	used   uint32
	digest [32]byte
}

type state struct {
	root  Root
	inv   []entry
	slots [][]byte
	head  int // head file number that holds this generation
}

// Vessel is an opened vessel. It is safe for concurrent use: many readers,
// and the one writer the host's owner allows. mu guards the state below it
// the keys and the medium do not change after opening.
type Vessel struct {
	mu   sync.Mutex
	m    Medium
	rnd  io.Reader
	vk   []byte
	slk  []byte
	hdk  []byte
	ptk  []byte
	salt []byte
	iter int
	st   state
	// seen is the four head files as this vessel last read or wrote them:
	// when the opened generation was chosen, or when this vessel committed
	// it. Moved compares the medium with it.
	seen [HeadSlots][]byte
	own  Owner // the owner of the last Begin; used by Grow, Shrink, Compact

	index  []item // live records in commit order, built lazily
	cache  map[uint32]cached
	report Report
}

type cached struct {
	gen  uint64
	body []byte
}

type item struct {
	h Header
	r Ref
}

var zeroNonce = make([]byte, 12)

func slabSize(k int) int { return 1 << k }

// SlabCapacity is the body capacity of one slab of size 2^k.
func SlabCapacity(k int) int { return slabSize(k) - SlabOverhead - SlabHeader }

// MaxChunk is the largest plaintext chunk of content (contract 2.5).
func MaxChunk(k int) int { return slabSize(k) - ChunkMargin }

func segmentsFor(n int) int { return (n + SegmentEntries - 1) / SegmentEntries }

func readFull(r io.Reader, b []byte) error {
	if r == nil {
		return fmt.Errorf("vessel: no source of randomness was given")
	}
	_, err := io.ReadFull(r, b)
	return err
}

func gcm(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// SharedKey returns PTK, the key of suite-0x02 envelopes (E7), for a VK.
func SharedKey(vk []byte) ([]byte, error) {
	return hkdf.Expand(sha256.New, vk, "rokh/vessel/1/shared", 32)
}

func (v *Vessel) setKeys(vk []byte) error {
	var err error
	if len(vk) != 32 {
		return fmt.Errorf("vessel: VK must be 32 bytes")
	}
	v.vk = append([]byte(nil), vk...)
	if v.slk, err = hkdf.Expand(sha256.New, vk, "rokh/vessel/1/slabs", 32); err != nil {
		return err
	}
	if v.hdk, err = hkdf.Expand(sha256.New, vk, "rokh/vessel/1/heads", 32); err != nil {
		return err
	}
	v.ptk, err = SharedKey(vk)
	return err
}

// SharedKey is PTK of this vessel.
func (v *Vessel) SharedKey() []byte { return append([]byte(nil), v.ptk...) }

// ---------- slabs ----------

func slabAAD(vessel frame.ID, index uint32) []byte {
	aad := make([]byte, 36)
	copy(aad, vessel[:])
	binary.BigEndian.PutUint32(aad[32:], index)
	return aad
}

func sealSlab(slk []byte, vessel frame.ID, index uint32, kind byte, body []byte, size int, rnd io.Reader) ([]byte, error) {
	if len(body) > size-SlabOverhead-SlabHeader {
		return nil, ErrTooLarge
	}
	out := make([]byte, size)
	if err := readFull(rnd, out[:32]); err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, slk, out[:32], "rokh/vessel/1/slab", 32)
	if err != nil {
		return nil, err
	}
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, size-SlabOverhead)
	plain[0] = kind
	binary.BigEndian.PutUint32(plain[4:8], uint32(len(body)))
	copy(plain[SlabHeader:], body)
	g.Seal(out[32:32], zeroNonce, plain, slabAAD(vessel, index))
	return out, nil
}

func openSlab(slk []byte, vessel frame.ID, index uint32, file []byte, size int) (byte, []byte, error) {
	if len(file) != size {
		return 0, nil, wrap(ErrCorrupt, "slab %d is %d bytes, want %d", index, len(file), size)
	}
	key, err := hkdf.Key(sha256.New, slk, file[:32], "rokh/vessel/1/slab", 32)
	if err != nil {
		return 0, nil, err
	}
	g, err := gcm(key)
	if err != nil {
		return 0, nil, err
	}
	plain, err := g.Open(nil, zeroNonce, file[32:], slabAAD(vessel, index))
	if err != nil {
		return 0, nil, wrap(ErrCorrupt, "slab %d does not open", index)
	}
	used := binary.BigEndian.Uint32(plain[4:8])
	if plain[1] != 0 || plain[2] != 0 || plain[3] != 0 || int(used) > len(plain)-SlabHeader {
		return 0, nil, wrap(ErrCorrupt, "slab %d has a bad header", index)
	}
	return plain[0], plain[SlabHeader : SlabHeader+int(used)], nil
}

// ---------- head files ----------

func headAAD(h []byte, num int) []byte {
	aad := make([]byte, 65)
	copy(aad, h[:64])
	aad[64] = byte(num)
	return aad
}

// checkSlots refuses slot cells a head cannot hold: more than SlotCells, or a
// cell that is not CellSize long. buildHead would drop the one and replace the
// other with noise, and a key whose cell is gone opens nothing, so none is
// taken without a word.
func checkSlots(slots [][]byte) error {
	if len(slots) > SlotCells {
		return fmt.Errorf("%w: %d slot cells; a head holds %d", ErrTooLarge, len(slots), SlotCells)
	}
	for i, s := range slots {
		if len(s) != CellSize {
			return fmt.Errorf("vessel: slot cell %d is %d bytes, not %d", i, len(s), CellSize)
		}
	}
	return nil
}

func buildHead(hdk, salt []byte, iter int, slots [][]byte, num int, root []byte, rnd io.Reader) ([]byte, error) {
	h := make([]byte, HeadSize)
	copy(h[0:4], Magic)
	h[4] = KDFPBKDF2
	binary.BigEndian.PutUint32(h[8:12], uint32(iter))
	copy(h[12:44], salt)
	for i := 0; i < SlotCells; i++ {
		cell := h[slotsOff+i*CellSize : slotsOff+(i+1)*CellSize]
		if i < len(slots) && len(slots[i]) == CellSize {
			copy(cell, slots[i])
		} else if err := readFull(rnd, cell); err != nil {
			return nil, err
		}
	}
	box := h[rootBoxOff:]
	if len(root)+4 > len(box)-SlabOverhead {
		return nil, fmt.Errorf("vessel: root record too large")
	}
	if err := readFull(rnd, box[:32]); err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, hdk, box[:32], "rokh/vessel/1/head", 32)
	if err != nil {
		return nil, err
	}
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(box)-SlabOverhead)
	binary.BigEndian.PutUint32(plain[0:4], uint32(len(root)))
	copy(plain[4:], root)
	g.Seal(box[32:32], zeroNonce, plain, headAAD(h, num))
	return h, nil
}

// headPublic reads the clear part of a head file: magic, KDF, salt, slots.
func headPublic(h []byte) (salt []byte, iter int, slots [][]byte, ok bool) {
	if len(h) != HeadSize || string(h[0:4]) != Magic || h[4] != KDFPBKDF2 {
		return nil, 0, nil, false
	}
	iter = int(binary.BigEndian.Uint32(h[8:12]))
	if iter < 1 || iter > MaxIter {
		return nil, 0, nil, false
	}
	salt = append([]byte(nil), h[12:44]...)
	for i := 0; i < SlotCells; i++ {
		slots = append(slots, append([]byte(nil), h[slotsOff+i*CellSize:slotsOff+(i+1)*CellSize]...))
	}
	return salt, iter, slots, true
}

func openHead(hdk, h []byte, num int) (Root, error) {
	if len(h) != HeadSize || string(h[0:4]) != Magic {
		return Root{}, ErrNotAVessel
	}
	box := h[rootBoxOff:]
	key, err := hkdf.Key(sha256.New, hdk, box[:32], "rokh/vessel/1/head", 32)
	if err != nil {
		return Root{}, err
	}
	g, err := gcm(key)
	if err != nil {
		return Root{}, err
	}
	plain, err := g.Open(nil, zeroNonce, box[32:], headAAD(h, num))
	if err != nil {
		return Root{}, wrap(ErrCorrupt, "head %d does not open", num)
	}
	n := binary.BigEndian.Uint32(plain[0:4])
	if int(n) > len(plain)-4 {
		return Root{}, wrap(ErrCorrupt, "head %d has a bad length", num)
	}
	return decodeRoot(plain[4 : 4+n])
}

// ---------- root record ----------

// The root record is canonical TLV in the wire form of frame.EncodeFields:
// tag(u16 BE) || len(u32 BE) || value, ascending tags, no duplicate, no empty
// value. It is written here, rather than through frame, only because the
// segment list may exceed frame's per-field limit of 8 KiB.
type tlv struct {
	tag uint16
	val []byte
}

func encodeTLV(fs []tlv) ([]byte, error) {
	var out []byte
	var hdr [6]byte
	for i, f := range fs {
		if len(f.val) == 0 {
			return nil, fmt.Errorf("vessel: empty field %d", f.tag)
		}
		if i > 0 && fs[i-1].tag >= f.tag {
			return nil, fmt.Errorf("vessel: fields out of order")
		}
		binary.BigEndian.PutUint16(hdr[0:2], f.tag)
		binary.BigEndian.PutUint32(hdr[2:6], uint32(len(f.val)))
		out = append(out, hdr[:]...)
		out = append(out, f.val...)
	}
	return out, nil
}

func decodeTLV(b []byte) ([]tlv, error) {
	var out []tlv
	for len(b) > 0 {
		if len(b) < 6 {
			return nil, wrap(ErrCorrupt, "truncated field")
		}
		t := binary.BigEndian.Uint16(b[0:2])
		n := binary.BigEndian.Uint32(b[2:6])
		b = b[6:]
		if n == 0 || uint64(n) > uint64(len(b)) {
			return nil, wrap(ErrCorrupt, "bad field length")
		}
		if len(out) > 0 && out[len(out)-1].tag >= t {
			return nil, wrap(ErrCorrupt, "fields out of order")
		}
		out = append(out, tlv{t, b[:n]})
		b = b[n:]
	}
	return out, nil
}

func u32(x uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, x); return b }
func u64(x uint64) []byte { b := make([]byte, 8); binary.BigEndian.PutUint64(b, x); return b }

// EncodeList writes a list of short strings as len(1) || bytes, repeated. An
// empty string is written as a zero length.
func EncodeList(xs []string) []byte {
	var out []byte
	for _, x := range xs {
		out = append(out, byte(len(x)))
		out = append(out, x...)
	}
	return out
}

// DecodeList reads EncodeList.
func DecodeList(b []byte) ([]string, error) {
	var out []string
	for len(b) > 0 {
		n := int(b[0])
		if len(b) < 1+n {
			return nil, fmt.Errorf("vessel: truncated list")
		}
		out = append(out, string(b[1:1+n]))
		b = b[1+n:]
	}
	return out, nil
}

func encodeRoot(r Root) ([]byte, error) {
	var fs []tlv
	fs = append(fs, tlv{0x0001, []byte(Format)})
	fs = append(fs, tlv{0x0002, u64(r.Generation)})
	fs = append(fs, tlv{0x0003, append([]byte(nil), r.Anchor[:]...)})
	fs = append(fs, tlv{0x0004, append([]byte(nil), r.Vessel[:]...)})
	if !r.Seed.IsZero() {
		fs = append(fs, tlv{0x0005, append([]byte(nil), r.Seed[:]...)})
	}
	fs = append(fs, tlv{0x0006, append([]byte{byte(r.SlabLog2)}, u32(uint32(r.Slabs))...)})
	mode := byte(0)
	if r.Growth.Auto {
		mode = 1
	}
	g := append([]byte{mode}, u32(uint32(r.Growth.Step))...)
	fs = append(fs, tlv{0x0007, append(g, u32(uint32(r.Growth.Max))...)})
	var segs []byte
	for _, s := range r.segments {
		segs = append(segs, u32(s.index)...)
		segs = append(segs, s.digest[:]...)
	}
	fs = append(fs, tlv{0x0008, segs})
	fs = append(fs, tlv{0x0009, append([]byte(nil), r.Token[:]...)})
	if len(r.Scopes) > 0 {
		fs = append(fs, tlv{0x000A, EncodeList(r.Scopes)})
	}
	return encodeTLV(fs)
}

func decodeRoot(b []byte) (Root, error) {
	var r Root
	fs, err := decodeTLV(b)
	if err != nil {
		return r, err
	}
	seen := map[uint16]bool{}
	for _, f := range fs {
		seen[f.tag] = true
		switch f.tag {
		case 0x0001:
			if string(f.val) != Format {
				return r, wrap(ErrNotAVessel, "format %q", f.val)
			}
		case 0x0002:
			if len(f.val) != 8 {
				return r, wrap(ErrCorrupt, "generation")
			}
			r.Generation = binary.BigEndian.Uint64(f.val)
		case 0x0003, 0x0004, 0x0005:
			if len(f.val) != 32 {
				return r, wrap(ErrCorrupt, "id size")
			}
			var id frame.ID
			copy(id[:], f.val)
			switch f.tag {
			case 0x0003:
				r.Anchor = id
			case 0x0004:
				r.Vessel = id
			default:
				r.Seed = id
			}
		case 0x0006:
			if len(f.val) != 5 {
				return r, wrap(ErrCorrupt, "slab field")
			}
			r.SlabLog2 = int(f.val[0])
			r.Slabs = int(binary.BigEndian.Uint32(f.val[1:]))
		case 0x0007:
			if len(f.val) != 9 {
				return r, wrap(ErrCorrupt, "growth field")
			}
			r.Growth = Growth{Auto: f.val[0] == 1, Step: int(binary.BigEndian.Uint32(f.val[1:5])), Max: int(binary.BigEndian.Uint32(f.val[5:9]))}
		case 0x0008:
			if len(f.val)%36 != 0 {
				return r, wrap(ErrCorrupt, "segment list")
			}
			for i := 0; i < len(f.val); i += 36 {
				var s segRef
				s.index = binary.BigEndian.Uint32(f.val[i : i+4])
				copy(s.digest[:], f.val[i+4:i+36])
				r.segments = append(r.segments, s)
			}
		case 0x0009:
			if len(f.val) != 16 {
				return r, wrap(ErrCorrupt, "token")
			}
			copy(r.Token[:], f.val)
		case 0x000A:
			if r.Scopes, err = DecodeList(f.val); err != nil {
				return r, wrap(ErrCorrupt, "scopes")
			}
		default:
			return r, wrap(ErrCorrupt, "unknown root field %d", f.tag)
		}
	}
	for _, t := range []uint16{1, 2, 3, 4, 6, 7, 8, 9} {
		if !seen[t] {
			return r, wrap(ErrCorrupt, "root record lacks field %d", t)
		}
	}
	if r.SlabLog2 < MinSlabLog2 || r.SlabLog2 > MaxSlabLog2 || r.Slabs < MinSlabs || r.Slabs > MaxSlabs {
		return r, wrap(ErrCorrupt, "slab parameters")
	}
	if len(r.segments) != segmentsFor(r.Slabs) {
		return r, wrap(ErrCorrupt, "segment count")
	}
	return r, nil
}

// ---------- inventory segments ----------

func encodeSegment(inv []entry, j int) []byte {
	out := make([]byte, SegmentEntries*EntrySize)
	for i := 0; i < SegmentEntries; i++ {
		idx := j*SegmentEntries + i
		if idx >= len(inv) {
			break
		}
		e := inv[idx]
		p := out[i*EntrySize : (i+1)*EntrySize]
		p[0], p[1] = e.state, e.kind
		binary.BigEndian.PutUint64(p[2:10], e.gen)
		binary.BigEndian.PutUint32(p[10:14], e.used)
		copy(p[14:46], e.digest[:])
	}
	return out
}

func decodeSegment(body []byte, inv []entry, j int) error {
	if len(body) != SegmentEntries*EntrySize {
		return wrap(ErrCorrupt, "segment %d has %d bytes", j, len(body))
	}
	for i := 0; i < SegmentEntries; i++ {
		idx := j*SegmentEntries + i
		p := body[i*EntrySize : (i+1)*EntrySize]
		if idx >= len(inv) {
			if !bytes.Equal(p, make([]byte, EntrySize)) {
				return wrap(ErrCorrupt, "segment %d names a slab beyond N", j)
			}
			continue
		}
		e := entry{state: p[0], kind: p[1], gen: binary.BigEndian.Uint64(p[2:10]), used: binary.BigEndian.Uint32(p[10:14])}
		copy(e.digest[:], p[14:46])
		if e.state > stateFree || (e.state == stateLive && e.kind != KindSegment && e.kind != KindPack) {
			return wrap(ErrCorrupt, "segment %d entry %d", j, i)
		}
		inv[idx] = e
	}
	return nil
}

// ---------- create ----------

func (p *Params) normalize() error {
	if p.SlabLog2 == 0 {
		p.SlabLog2 = DefaultSlabLog2
	}
	if p.Slabs == 0 {
		p.Slabs = DefaultSlabs
	}
	if p.Iter == 0 {
		p.Iter = DefaultIter
	}
	if p.SlabLog2 < MinSlabLog2 || p.SlabLog2 > MaxSlabLog2 {
		return fmt.Errorf("vessel: slab size 2^%d is outside 2^%d..2^%d", p.SlabLog2, MinSlabLog2, MaxSlabLog2)
	}
	if p.Slabs < MinSlabs || p.Slabs > MaxSlabs {
		return fmt.Errorf("vessel: slab count %d is outside %d..%d", p.Slabs, MinSlabs, MaxSlabs)
	}
	if p.Iter < 1 || p.Iter > MaxIter {
		return fmt.Errorf("vessel: iterations %d outside 1..%d", p.Iter, MaxIter)
	}
	if p.Growth.Auto && (p.Growth.Step < 1 || p.Growth.Max < p.Slabs || p.Growth.Max > MaxSlabs) {
		return fmt.Errorf("vessel: bad automatic growth (step %d, max %d)", p.Growth.Step, p.Growth.Max)
	}
	if !p.Growth.Auto {
		p.Growth = Growth{}
	}
	if p.Rand == nil {
		return fmt.Errorf("vessel: no source of randomness was given")
	}
	return nil
}

// Create makes a new vessel in the medium: N slab files of randomness, the
// first generation in head1.rkh and randomness in the other head files. It
// refuses a folder that already holds a vessel directory.
func Create(m Medium, p Params, vk []byte, slots [][]byte, root Root) (*Vessel, error) {
	if err := p.normalize(); err != nil {
		return nil, err
	}
	v := &Vessel{m: m, rnd: p.Rand, iter: p.Iter, cache: map[uint32]cached{}}
	if err := v.setKeys(vk); err != nil {
		return nil, err
	}
	if len(p.Salt) == 0 {
		v.salt = make([]byte, 32)
		if err := readFull(p.Rand, v.salt); err != nil {
			return nil, err
		}
	} else if len(p.Salt) != 32 {
		return nil, fmt.Errorf("vessel: salt must be 32 bytes")
	} else {
		v.salt = append([]byte(nil), p.Salt...)
	}
	if root.Vessel.IsZero() {
		if err := readFull(p.Rand, root.Vessel[:]); err != nil {
			return nil, err
		}
	}
	if err := checkSlots(slots); err != nil {
		return nil, err
	}
	if err := m.Mkdir(Dir); err != nil {
		return nil, err
	}
	n := p.Slabs
	size := slabSize(p.SlabLog2)
	for d := 0; d <= (n-1)>>10; d++ {
		if err := m.Mkdir(SlabDirName(uint32(d << 10))); err != nil {
			return nil, err
		}
	}
	for i := 0; i < n; i++ {
		b := make([]byte, size)
		if err := readFull(p.Rand, b); err != nil {
			return nil, err
		}
		if err := m.Write(SlabName(uint32(i)), b); err != nil {
			return nil, err
		}
	}
	cells := make([][]byte, SlotCells)
	for i := range cells {
		cells[i] = make([]byte, CellSize)
		if i < len(slots) && len(slots[i]) == CellSize {
			copy(cells[i], slots[i])
		} else if err := readFull(p.Rand, cells[i]); err != nil {
			return nil, err
		}
	}
	root.Generation = 0
	root.SlabLog2, root.Slabs, root.Growth = p.SlabLog2, n, p.Growth
	v.st = state{root: root, inv: make([]entry, n), slots: cells, head: -1}
	for _, i := range []int{0, 2, 3} {
		b := make([]byte, HeadSize)
		if err := readFull(p.Rand, b); err != nil {
			return nil, err
		}
		if err := m.Write(HeadName(i), b); err != nil {
			return nil, err
		}
	}
	// Generation 1 is an ordinary commit with no records, made by the
	// creator, who holds the only copy there is.
	t := &Tx{v: v, own: created{}}
	if out, err := t.Commit(); err != nil || out != Recorded {
		return nil, fmt.Errorf("vessel: the first generation was not recorded (%s): %v", out, err)
	}
	return v, nil
}

// created is the owner of a vessel still being made: nothing else can know
// of it yet.
type created struct{}

func (created) Holds() error { return nil }

// ---------- open ----------

type headSeen struct {
	num  int
	root Root
	raw  []byte
}

// Open unlocks a vessel and opens its highest whole generation (2.7).
func Open(m Medium, u Unlock, rnd io.Reader) (*Vessel, Report, error) {
	v := &Vessel{m: m, rnd: rnd, cache: map[uint32]cached{}}
	var raws [HeadSlots][]byte
	anyMagic := false
	for i := 0; i < HeadSlots; i++ {
		b, err := m.Read(HeadName(i), HeadSize)
		if err == nil && len(b) == HeadSize {
			raws[i] = b
			if string(b[0:4]) == Magic {
				anyMagic = true
			}
		}
	}
	if !anyMagic {
		return nil, Report{}, ErrNotAVessel
	}
	unlockedAt := -1
	for i := 0; i < HeadSlots && unlockedAt < 0; i++ {
		salt, iter, slots, ok := headPublic(raws[i])
		if !ok {
			continue
		}
		vk, err := u(slots, salt, iter)
		if err != nil || len(vk) != 32 {
			continue
		}
		if err := v.setKeys(vk); err != nil {
			continue
		}
		if _, err := openHead(v.hdk, raws[i], i); err != nil {
			continue
		}
		v.salt, v.iter, unlockedAt = salt, iter, i
	}
	if unlockedAt < 0 {
		return nil, Report{}, ErrLocked
	}
	rep, err := v.load(raws)
	if err != nil {
		return nil, rep, err
	}
	// The key must open the generation that was chosen, by that generation's
	// own cells: a cell that survives only in an older head file does not
	// open a newer generation that no longer holds it (a reviewed case). What a
	// key already held, or already read, cannot be taken back by this.
	if v.st.head != unlockedAt {
		vk, err := u(v.st.slots, v.salt, v.iter)
		if err != nil || !bytes.Equal(vk, v.vk) {
			return nil, rep, fmt.Errorf("%w: the key is not among the cells of generation %d", ErrLocked, v.st.root.Generation)
		}
	}
	v.report = rep
	return v, rep, nil
}

// readHeads reads every head file raws does not hold yet. A file that cannot
// be read stays nil: it holds no generation.
func (v *Vessel) readHeads(raws [HeadSlots][]byte) [HeadSlots][]byte {
	for i := 0; i < HeadSlots; i++ {
		if raws[i] != nil {
			continue
		}
		if b, err := v.m.Read(HeadName(i), HeadSize); err == nil {
			raws[i] = b
		}
	}
	return raws
}

// heads opens every head file of raws that opens, highest generation first.
// raws is read already (readHeads); a nil entry is a file that was not read.
func (v *Vessel) heads(raws [HeadSlots][]byte) ([]headSeen, []int) {
	var out []headSeen
	var torn []int
	for i := 0; i < HeadSlots; i++ {
		if raws[i] == nil {
			continue
		}
		r, err := openHead(v.hdk, raws[i], i)
		if err != nil {
			if len(raws[i]) == HeadSize && string(raws[i][0:4]) == Magic {
				torn = append(torn, i)
			}
			continue
		}
		if int(r.Generation%HeadSlots) != i {
			torn = append(torn, i)
			continue
		}
		out = append(out, headSeen{num: i, root: r, raw: raws[i]})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].root.Generation > out[b].root.Generation })
	return out, torn
}

// choose finds the highest generation whose inventory and recent slabs
// verify, and says which newer ones it passed over and why. raws is the head
// files as read (readHeads). It changes nothing.
func (v *Vessel) choose(raws [HeadSlots][]byte) (state, Report, error) {
	var rep Report
	hs, torn := v.heads(raws)
	rep.Torn = torn
	if len(hs) == 0 {
		return state{}, rep, wrap(ErrCorrupt, "no head file opens")
	}
	for i, h := range hs {
		st, err := v.verify(h)
		if err == nil {
			rep.Generation = st.root.Generation
			return st, rep, nil
		}
		to := uint64(0)
		if i+1 < len(hs) {
			to = hs[i+1].root.Generation
		}
		rep.FellBack = append(rep.FellBack, Fallback{From: h.root.Generation, To: to, Why: err.Error()})
	}
	return state{}, rep, wrap(ErrCorrupt, "no generation verifies")
}

// load takes the highest generation that verifies, from the head files in
// raws and the medium's own for those raws does not hold.
func (v *Vessel) load(raws [HeadSlots][]byte) (Report, error) {
	raws = v.readHeads(raws)
	st, rep, err := v.choose(raws)
	if err != nil {
		return rep, err
	}
	v.st = st
	v.seen = raws
	v.index = nil
	v.cache = map[uint32]cached{}
	v.survey(&rep)
	return rep, nil
}

// verify checks one generation: every segment, and every slab written in the
// last R generations.
func (v *Vessel) verify(h headSeen) (state, error) {
	r := h.root
	size := slabSize(r.SlabLog2)
	inv := make([]entry, r.Slabs)
	for j, s := range r.segments {
		if int(s.index) >= r.Slabs {
			return state{}, wrap(ErrCorrupt, "segment %d outside the vessel", j)
		}
		file, err := v.m.Read(SlabName(s.index), size)
		if err != nil {
			return state{}, wrap(ErrCorrupt, "segment %d: %v", j, err)
		}
		if sha256.Sum256(file) != s.digest {
			return state{}, wrap(ErrCorrupt, "segment %d digest", j)
		}
		kind, body, err := openSlab(v.slk, r.Vessel, s.index, file, size)
		if err != nil {
			return state{}, err
		}
		if kind != KindSegment {
			return state{}, wrap(ErrCorrupt, "segment %d kind", j)
		}
		if err := decodeSegment(body, inv, j); err != nil {
			return state{}, err
		}
	}
	for idx, e := range inv {
		if e.state != stateLive || e.kind != KindPack {
			continue
		}
		if e.gen+Retention > r.Generation {
			file, err := v.m.Read(SlabName(uint32(idx)), size)
			if err != nil {
				return state{}, wrap(ErrCorrupt, "slab %d: %v", idx, err)
			}
			if sha256.Sum256(file) != e.digest {
				return state{}, wrap(ErrCorrupt, "slab %d digest", idx)
			}
		}
	}
	_, _, slots, _ := headPublic(h.raw)
	return state{root: r, inv: inv, slots: slots, head: h.num}, nil
}

// survey lists slab files that are missing or beyond N. It reads names only.
func (v *Vessel) survey(rep *Report) {
	n := v.st.root.Slabs
	have := map[string]bool{}
	dirs, _ := v.m.Names(Dir)
	for _, d := range dirs {
		if len(d) != 3 {
			continue
		}
		names, _ := v.m.Names(Dir + "/" + d)
		for _, x := range names {
			p := Dir + "/" + d + "/" + x
			if !ValidName(p) {
				continue // foreign entries are ignored, never read, never deleted
			}
			have[p] = true
			var idx uint32
			fmt.Sscanf(x, "%08x.rkh", &idx)
			if int(idx) >= n {
				rep.Beyond = append(rep.Beyond, p)
			}
		}
	}
	for i := 0; i < n; i++ {
		if !have[SlabName(uint32(i))] {
			rep.Missing = append(rep.Missing, uint32(i))
		}
	}
}

// refresh reloads the highest whole generation from the medium.
func (v *Vessel) refresh() error {
	rep, err := v.load([HeadSlots][]byte{})
	if err != nil {
		return err
	}
	v.report = rep
	return nil
}

// highestValid returns the highest generation that verifies, by the rule
// load uses. A head that opens and does not verify is not a generation.
func (v *Vessel) highestValid() (uint64, error) {
	st, _, err := v.choose(v.readHeads([HeadSlots][]byte{}))
	if err != nil {
		return 0, err
	}
	return st.root.Generation, nil
}

// Moved reports whether the medium may hold a commit this opened vessel has
// not read. Another process, or another opening in this one, commits to the
// same medium, and an opened vessel reads the medium again only when asked:
// at its own commit, or here. Moved reads one head file and changes nothing,
// so a reader may ask it before every answer.
//
// The one file is enough. Every commit after the opened generation g begins
// with generation g+1 (its base is the highest whole generation, which is g
// until g+1 exists), and generation g+1 is written whole to head file
// (g+1) mod 4 (contract 2.6, step 5). Until that file changes, no commit
// after g exists. A file that changed does not prove a new generation: a
// head torn by a writer that died, or one that does not verify, changes it
// too; Refresh then keeps the generation it has.
func (v *Vessel) Moved() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.moved()
}

// moved is Moved under the lock the caller holds. A head file that cannot be
// read is compared as absent.
func (v *Vessel) moved() bool {
	next := int((v.st.root.Generation + 1) % HeadSlots)
	now, err := v.m.Read(HeadName(next), HeadSize)
	if err != nil {
		now = nil
	}
	return !bytes.Equal(now, v.seen[next])
}

// Refresh reads the medium's present generation when the medium moved
// (Moved), by the rule opening uses: the highest generation that verifies
// (2.7). It reports whether the opened generation changed. It writes nothing
// and needs no owner; a writer that holds the turn calls it before it judges
// what it will commit on, and a reader before it answers. When no generation
// verifies, the opened one is kept and the error says why.
func (v *Vessel) Refresh() (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.moved() {
		return false, nil
	}
	was := v.st.root
	if err := v.refresh(); err != nil {
		return false, err
	}
	now := v.st.root
	return now.Generation != was.Generation || now.Token != was.Token, nil
}

// ---------- reading ----------

// Info describes the opened generation.
func (v *Vessel) Info() Info {
	v.mu.Lock()
	defer v.mu.Unlock()
	r := v.st.root
	live := 0
	var used int64
	for _, e := range v.st.inv {
		if e.state == stateLive {
			live++
			if e.kind == KindPack {
				used += int64(e.used)
			}
		}
	}
	return Info{
		Format: Format, Generation: r.Generation, Anchor: r.Anchor, Vessel: r.Vessel, Seed: r.Seed,
		SlabLog2: r.SlabLog2, SlabSize: slabSize(r.SlabLog2), Slabs: r.Slabs, Growth: r.Growth,
		Scopes: append([]string(nil), r.Scopes...), Salt: append([]byte(nil), v.salt...), Iter: v.iter,
		Live: live, Free: len(v.eligible(r.Generation + 1)), Used: used,
	}
}

// Report is what the last open or refresh found.
func (v *Vessel) Report() Report {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.report
}

// Slots returns the 32 slot cells of the opened head.
func (v *Vessel) Slots() [][]byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([][]byte, len(v.st.slots))
	for i, s := range v.st.slots {
		out[i] = append([]byte(nil), s...)
	}
	return out
}

// Rand is the injected source of randomness this vessel was opened with, for
// the layers above that seal on its behalf (C5).
func (v *Vessel) Rand() io.Reader { return v.rnd }

// VK returns the vessel key. The key layer needs it to make slots.
func (v *Vessel) VK() []byte { return append([]byte(nil), v.vk...) }

// Reopen opens this vessel again, as a second opening in the same process
// would: the same medium, key and randomness, its own reading of the head
// files and its own memory of them. It reads and changes nothing of this
// opening, and the two follow the medium apart (Moved, Refresh). A booth
// reads one session's own view through it.
func (v *Vessel) Reopen() (*Vessel, Report, error) {
	vk := v.VK()
	return Open(v.m, func([][]byte, []byte, int) ([]byte, error) { return vk, nil }, v.rnd)
}

// slab reads and verifies one live slab, with a small cache.
func (v *Vessel) slab(idx uint32) ([]byte, error) {
	if int(idx) >= len(v.st.inv) {
		return nil, wrap(ErrCorrupt, "slab %d outside the vessel", idx)
	}
	e := v.st.inv[idx]
	if e.state != stateLive {
		return nil, wrap(ErrCorrupt, "slab %d is not live", idx)
	}
	if c, ok := v.cache[idx]; ok && c.gen == e.gen {
		return c.body, nil
	}
	size := slabSize(v.st.root.SlabLog2)
	file, err := v.m.Read(SlabName(idx), size)
	if err != nil {
		return nil, wrap(ErrCorrupt, "slab %d: %v", idx, err)
	}
	if sha256.Sum256(file) != e.digest {
		return nil, wrap(ErrCorrupt, "slab %d digest", idx)
	}
	kind, body, err := openSlab(v.slk, v.st.root.Vessel, idx, file, size)
	if err != nil {
		return nil, err
	}
	if kind != e.kind || uint32(len(body)) != e.used {
		return nil, wrap(ErrCorrupt, "slab %d does not match its entry", idx)
	}
	if len(v.cache) >= 8 {
		v.cache = map[uint32]cached{}
	}
	v.cache[idx] = cached{gen: e.gen, body: body}
	return body, nil
}

// packs lists live record packs in commit order.
func (v *Vessel) packs() []uint32 {
	var out []uint32
	for i, e := range v.st.inv {
		if e.state == stateLive && e.kind == KindPack {
			out = append(out, uint32(i))
		}
	}
	sort.Slice(out, func(a, b int) bool {
		ea, eb := v.st.inv[out[a]], v.st.inv[out[b]]
		if ea.gen != eb.gen {
			return ea.gen < eb.gen
		}
		return out[a] < out[b]
	})
	return out
}

func (v *Vessel) buildIndex() error {
	if v.index != nil {
		return nil
	}
	err := v.indexOnce()
	if err != nil {
		// One refresh of the head, then fail (2.7).
		if rerr := v.refresh(); rerr != nil {
			return err
		}
		err = v.indexOnce()
	}
	return err
}

func (v *Vessel) indexOnce() error {
	idx := []item{}
	for _, p := range v.packs() {
		body, err := v.slab(p)
		if err != nil {
			return err
		}
		recs, err := parsePack(body)
		if err != nil {
			return wrap(err, "slab %d", p)
		}
		for _, rc := range recs {
			rc.r.Slab, rc.r.Gen = p, v.st.inv[p].gen
			idx = append(idx, rc)
		}
	}
	v.index = idx
	return nil
}

// Scan calls fn for every live record header, in commit order. The header
// fields other than an event's head are locators and prove nothing (C8).
//
// The index is read under the vessel's lock and the callbacks run without
// it, so a callback may call Body.
func (v *Vessel) Scan(fn func(Header, Ref) error) error {
	v.mu.Lock()
	err := v.buildIndex()
	snap := append([]item(nil), v.index...)
	v.mu.Unlock()
	if err != nil {
		return err
	}
	for _, it := range snap {
		if err := fn(it.h, it.r); err != nil {
			return err
		}
	}
	return nil
}

// Body returns one record's envelope. Its slab is verified first.
func (v *Vessel) Body(r Ref) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.body(r)
}

// Found is one record Find read: its header, its reference and its envelope.
type Found struct {
	Header Header
	Ref    Ref
	Env    []byte
}

// Find reads every live record whose header want accepts, with its envelope,
// under one hold of the vessel's lock: each is found and read at the same
// generation, so no commit can move it in between. want runs under the lock
// and must not call the vessel. It is a reader's second try after a stale
// reference.
func (v *Vessel) Find(want func(Header) bool) ([]Found, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.buildIndex(); err != nil {
		return nil, err
	}
	var out []Found
	for _, it := range v.index {
		if !want(it.h) {
			continue
		}
		env, err := v.body(it.r)
		if err != nil {
			return nil, err
		}
		out = append(out, Found{Header: it.h, Ref: it.r, Env: env})
	}
	return out, nil
}

// body is Body under the lock the caller holds.
func (v *Vessel) body(r Ref) ([]byte, error) {
	if int(r.Slab) >= len(v.st.inv) || v.st.inv[r.Slab].gen != r.Gen || v.st.inv[r.Slab].state != stateLive {
		return nil, fmt.Errorf("%w (slab %d)", ErrStale, r.Slab)
	}
	body, err := v.slab(r.Slab)
	if err != nil {
		return nil, err
	}
	if int(r.Off)+int(r.Len) > len(body) {
		return nil, wrap(ErrCorrupt, "reference outside slab %d", r.Slab)
	}
	return append([]byte(nil), body[r.Off:r.Off+r.Len]...), nil
}

// eligible lists the slabs a commit of generation n may write: never used,
// or freed at least R generations before n. Ascending order.
func (v *Vessel) eligible(n uint64) []uint32 {
	var out []uint32
	for i, e := range v.st.inv {
		if e.state == stateNever || (e.state == stateFree && n >= e.gen+Retention) {
			out = append(out, uint32(i))
		}
	}
	return out
}
