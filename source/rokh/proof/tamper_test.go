package proof

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
	"rokh/vessel"
)

// The bytes that are signed, the bytes that are hashed and the bytes that are
// stored are one byte string, so there is nowhere for a change to hide. A
// flipped bit in the payload, in a parent's name, in the signature or in the
// magic is caught at the door: either the frame will not parse, or it parses
// as something with a different name — never as the original under the
// original's name. Offered to the ledger, none of them displaces what is
// already held.
//
// A v1 event is its signed head and the body the head names by hash
// (contract 3.4), so the signature closes the head, not the stored bytes: it
// is looked for there.
//
//	— T3.1, T3.2, T3.3, T3.7, N-Axiom3
func TestOneFlippedBitIsNeverTheSameEvent(t *testing.T) {
	w := newWorld(t)
	l := w.fresh()

	marker := []byte("PAYLOAD-MARKER-0123456789-abcdef")
	first := w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/journal", "note", []byte("the parent"))
	mustVerdict(t, l, first, ledger.Accepted, "the parent event")
	orig := w.sign(w.root, nil, []frame.ID{first.ID}, "home/journal", "note", marker)
	mustVerdict(t, l, orig, ledger.Accepted, "the event about to be tampered with")

	payloadAt := bytes.Index(orig.Raw, marker)
	parentAt := bytes.Index(orig.Raw, first.ID[:])
	if payloadAt < 0 || parentAt < 0 {
		t.Fatal("the event does not contain its own payload and parent verbatim")
	}
	if !bytes.HasPrefix(orig.Raw, orig.Head) {
		t.Fatal("the stored bytes do not begin with the signed head")
	}
	sigAt := len(orig.Head) - frame.SigSize
	spots := []struct {
		where  string
		offset int
	}{
		{"the magic", 0},
		{"the magic's last byte", len(frame.Magic) - 1},
		{"the kind", len(frame.Magic)},
		{"the payload's first byte", payloadAt},
		{"the payload's middle", payloadAt + len(marker)/2},
		{"the payload's last byte", payloadAt + len(marker) - 1},
		{"a parent's first byte", parentAt},
		{"a parent's last byte", parentAt + frame.IDSize - 1},
		{"the signature's first byte", sigAt},
		{"the signature's last byte", sigAt + frame.SigSize - 1},
		{"the body's first byte", len(orig.Head)},
		{"the body's last byte", len(orig.Raw) - 1},
	}

	for _, sp := range spots {
		for _, bit := range []byte{0x01, 0x80, 0xFF} {
			bad := append([]byte(nil), orig.Raw...)
			bad[sp.offset] ^= bit
			got, err := event.Parse(bad)
			if err == nil && got.ID == orig.ID {
				t.Fatalf("%s (^%#x): tampered bytes parsed as the original", sp.where, bit)
			}
			if err == nil && bytes.Equal(got.Raw, orig.Raw) {
				t.Fatalf("%s (^%#x): a changed frame read back unchanged", sp.where, bit)
			}

			st, addErr := l.Add(bad)
			if st == ledger.Accepted {
				t.Fatalf("%s (^%#x): the ledger accepted tampered bytes: %v", sp.where, bit, addErr)
			}
			held, ok := l.Get(orig.ID)
			if !ok || !bytes.Equal(held.Raw, orig.Raw) {
				t.Fatalf("%s (^%#x): the original's bytes were displaced", sp.where, bit)
			}
			if l.State(orig.ID) != ledger.Accepted {
				t.Fatalf("%s (^%#x): the original's verdict reopened", sp.where, bit)
			}
		}
	}

	// Truncation is the same answer: fewer bytes are different bytes.
	for _, cut := range []int{0, 1, len(orig.Raw) / 2, len(orig.Raw) - 1} {
		if _, err := event.Parse(orig.Raw[:cut]); err == nil {
			t.Fatalf("a frame cut to %d bytes parsed", cut)
		}
	}
}

// A tampered record on the carrier is not read past. A v1 carrier keeps its
// events in slabs sealed to their place and named in the inventory by digest
// (contract 2.2, 2.4), so one overwritten byte in the slab that holds an event
// makes it unopenable, and the carrier never hands back whatever it found
// (V3). Where the damage lies in what the newest recording wrote, opening
// falls back to the last whole generation and says so; where it lies in a
// slab at rest, reading it is refused with vessel_corrupt, and the references
// kept beside it are refused with it (2.7). Either way a ledger read over the
// damaged carrier does not quietly miss its own past: the event is not in it,
// and what could not be proven is named (C8).
//
// Put back what was there, and everything reads again: the damage was in the
// bytes, not in the design.
//
//	— T3.1, T3.3, T8, T8.5, T12.3
func TestATamperedObjectOnDiskIsRefusedNotRead(t *testing.T) {
	t.Run("in what the newest recording wrote", tamperWithTheNewestRecording)
	t.Run("in a slab at rest", tamperWithASlabAtRest)
}

// damage flips one byte of a file in memory at each of three places, calls
// check after each, and puts the file back.
func damage(m *vessel.Memory, name string, check func(at int)) {
	orig := m.Get(name)
	for _, at := range []int{0, len(orig) / 2, len(orig) - 1} {
		b := append([]byte(nil), orig...)
		b[at] ^= 0xFF
		m.Put(name, b)
		check(at)
	}
	m.Put(name, orig)
}

// watched makes one recording on the world's carrier and returns the slab it
// wrote first. A commit writes its packs before its segments and its head
// (contract 2.6), its packs in their own order, and it lays new records
// behind the newest pack while they fit (vessel/tx.go): so the first slab is
// the newest pack, rewritten with what this recording added behind it.
func watched(t *testing.T, w *world, fill func(*carrier.Recording) error) string {
	t.Helper()
	m := w.mem
	m.Record(true)
	defer m.Record(false)
	w.commit(fill)
	for _, c := range m.Log() {
		if c.Op == "write" && !strings.Contains(c.Name, "/head") {
			return c.Name
		}
	}
	t.Fatal("the recording wrote no slab")
	return ""
}

// recordOn is the fill of a recording that puts one event on a branch.
func recordOn(e event.Signed, branch string) func(*carrier.Recording) error {
	return func(r *carrier.Recording) error {
		if err := r.Event(e.ID, e.Head, e.Body, e.Event.Address); err != nil {
			return err
		}
		return r.SetRef(branch, e.ID)
	}
}

func tamperWithTheNewestRecording(t *testing.T) {
	w := newFastWorld(t)
	e := w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/journal", "note",
		[]byte("the event that will be damaged"))
	pack := watched(t, w, recordOn(e, "main"))

	// Read it back cleanly first, so the damage below is the only difference.
	c, rep := w.open(t)
	generation := rep.Generation
	if _, err := c.Get(e.ID); err != nil {
		t.Fatalf("before the damage: %v", err)
	}
	if got := loadFrom(t, c).Len(); got != 2 {
		t.Fatalf("before the damage the carrier holds %d events, want 2", got)
	}

	damage(w.mem, pack, func(at int) {
		fresh, rep := w.open(t)
		if rep.Generation != generation-1 || len(rep.FellBack) == 0 {
			t.Fatalf("byte %d: opened generation %d with report %+v; the damaged generation %d is not to be opened, and the fall back is to be said",
				at, rep.Generation, rep, generation)
		}
		if raw, err := fresh.Get(e.ID); !errors.Is(err, carrier.ErrNotFound) {
			t.Fatalf("byte %d: the damaged recording was served: %d bytes, %v", at, len(raw), err)
		}
		back := loadFrom(t, fresh)
		if back.Has(e.ID) || back.Len() != 1 {
			t.Fatalf("byte %d: the ledger read over the fallen back carrier holds %d events", at, back.Len())
		}
	})

	fresh, rep := w.open(t)
	if rep.Generation != generation || len(rep.FellBack) != 0 {
		t.Fatalf("after repair: generation %d, report %+v", rep.Generation, rep)
	}
	if _, err := fresh.Get(e.ID); err != nil {
		t.Fatalf("after repair: %v", err)
	}
	if got := loadFrom(t, fresh).Len(); got != 2 {
		t.Fatalf("after repair the carrier holds %d events, want 2", got)
	}
}

func tamperWithASlabAtRest(t *testing.T) {
	w := newFastWorld(t)
	e := w.record(w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/journal", "note",
		[]byte("the event that will be damaged at rest")), "main")

	// Content that runs past one pack: the recording lays it behind the newest
	// pack, which holds the event, until that pack is full, and starts another
	// for the rest. A whole chunk and a second one of ChunkMargin bytes never
	// share a slab, whatever the pack held before. The first slab the
	// recording writes is the full pack that holds the event, and the
	// recordings after it rewrite only the newer one. R more recordings, and
	// opening no longer verifies the full pack (contract 2.7): the damage is
	// met on the first read.
	bulk := bytes.Repeat([]byte("bulk "), (vessel.MaxChunk(slabLog2)+vessel.ChunkMargin)/5)
	pack := watched(t, w, func(r *carrier.Recording) error {
		_, err := r.Content("home/files", uint64(len(bulk)), func() (io.Reader, error) { return bytes.NewReader(bulk), nil })
		return err
	})
	written := w.mem.Get(pack)
	parent := e.ID
	for i := 0; i < vessel.Retention; i++ {
		parent = w.record(w.sign(w.root, nil, []frame.ID{parent}, "home/journal", "note",
			[]byte("a later recording")), "main").ID
	}
	if !bytes.Equal(w.mem.Get(pack), written) {
		t.Fatal("the slab that holds the event was written again; this case needs it at rest")
	}

	c, rep := w.open(t)
	generation := rep.Generation
	if len(rep.FellBack) != 0 {
		t.Fatalf("before the damage the carrier fell back: %+v", rep)
	}
	if _, err := c.Get(e.ID); err != nil {
		t.Fatalf("before the damage: %v", err)
	}
	heads, err := c.Heads()
	if err != nil {
		t.Fatal(err)
	}
	whole := loadFrom(t, c).Len()
	if whole != 2+vessel.Retention {
		t.Fatalf("before the damage the carrier holds %d events, want %d", whole, 2+vessel.Retention)
	}

	damage(w.mem, pack, func(at int) {
		fresh, rep := w.open(t)
		if rep.Generation != generation || len(rep.FellBack) != 0 {
			t.Fatalf("byte %d: a slab at rest is read when it is used, not at opening: generation %d, report %+v",
				at, rep.Generation, rep)
		}
		if raw, err := fresh.Get(e.ID); carrier.Code(err) != "vessel_corrupt" {
			t.Fatalf("byte %d: Get returned %d bytes and %v, want vessel_corrupt (is the damaged slab still the event's?)", at, len(raw), err)
		}
		if _, err := fresh.Refs(); carrier.Code(err) != "vessel_corrupt" {
			t.Fatalf("byte %d: the references beside a damaged slab read as %v, want vessel_corrupt", at, err)
		}
		l, err := ledger.Load(w.gen.Raw, fresh.Get, heads)
		if err != nil {
			t.Fatalf("byte %d: %v", at, err)
		}
		if l.Has(e.ID) || l.Len() != 1 {
			t.Fatalf("byte %d: a ledger read over the damaged carrier holds %d events", at, l.Len())
		}
		if len(l.Unproven()) == 0 {
			t.Fatalf("byte %d: a ledger read over the damaged carrier names nothing it could not prove", at)
		}
	})

	fresh, _ := w.open(t)
	if _, err := fresh.Get(e.ID); err != nil {
		t.Fatalf("after repair: %v", err)
	}
	if got := loadFrom(t, fresh).Len(); got != whole {
		t.Fatalf("after repair the carrier holds %d events, want %d", got, whole)
	}
}
