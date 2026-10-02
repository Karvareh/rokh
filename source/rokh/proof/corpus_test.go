package proof

import (
	"bytes"
	"encoding/binary"
	mrand "math/rand"
	"testing"

	"rokh/event"
	"rokh/frame"
)

// fieldSpan is where one field sits inside a frame's body.
type fieldSpan struct{ start, end int }

// spansOf walks an event's fields, so a mutation can move, repeat or resize a
// whole field rather than only flipping bits inside one. A v1 event is two
// byte strings (contract 3.4): the head's fields lie between its header and
// its signature, and the body is fields to the end. Both are walked.
func spansOf(raw []byte) []fieldSpan {
	n, err := frame.HeadLen(raw)
	if err != nil || n > len(raw) {
		return nil
	}
	return append(fieldsIn(raw, frame.HeaderSize, n-frame.SigSize), fieldsIn(raw, n, len(raw))...)
}

// fieldsIn walks the fields of raw[at:end].
func fieldsIn(raw []byte, at, end int) []fieldSpan {
	var out []fieldSpan
	for at+frame.FieldHeaderSize <= end {
		n := int(binary.BigEndian.Uint32(raw[at+2 : at+6]))
		next := at + frame.FieldHeaderSize + n
		if n < 0 || next > end {
			break
		}
		out = append(out, fieldSpan{start: at, end: next})
		at = next
	}
	return out
}

// adjacent lists the spans that are followed at once by another, so a swap
// exchanges two neighbouring fields of one byte string and drops nothing.
func adjacent(spans []fieldSpan) []int {
	var out []int
	for i := 0; i+1 < len(spans); i++ {
		if spans[i].end == spans[i+1].start {
			out = append(out, i)
		}
	}
	return out
}

// safeParse runs a parser and turns a panic into a test failure, because
// "returned an error" and "took the process down" are not the same refusal.
func safeParse(t *testing.T, what string, in []byte, run func([]byte)) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked on %d bytes (%x): %v", what, len(in), head(in), r)
		}
	}()
	run(in)
}

func head(b []byte) []byte {
	if len(b) > 48 {
		return b[:48]
	}
	return b
}

// Nothing reaches the ledger except through a parser, so the parsers are the
// wall. Four thousand deliberately hostile byte strings — pure noise, and
// every way of damaging a real frame this package could think of: flipped
// bits, cuts, extra bytes, fields swapped out of order, a field written twice,
// a length that claims more than exists and one that claims nothing — and the
// answer is always the same two: an error, or a frame whose own bytes
// re-encode to exactly themselves. Never a panic, and never a second spelling
// of one content.
//
//	— T3, T3.1, T12.3, N4.1
func TestAHostileCorpusIsAlwaysRefusedOrExactlyItself(t *testing.T) {
	w := newWorld(t)
	valid := w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/journal", "note",
		bytes.Repeat([]byte("a corpus seed. "), 8),
		event.Attestation{Oracle: "clock", Claim: []byte("2026-09-15T00:00:00Z")},
		event.Attestation{Oracle: "chance", Claim: []byte("0123456789abcdef")})

	check := func(in []byte) {
		safeParse(t, "frame.ParseHead", in, func(b []byte) {
			f, _, err := frame.ParseHead(b)
			if err != nil {
				return
			}
			re, err := f.Reencode()
			if err != nil {
				t.Fatalf("a frame parsed and would not re-encode: %v", err)
			}
			if !bytes.Equal(re, f.Raw) {
				t.Fatalf("a frame parsed into something that is not its own bytes")
			}
			if f.ID != frame.Hash(f.Raw) {
				t.Fatal("a frame's name is not the hash of its bytes")
			}
		})
		safeParse(t, "event.Parse", in, func(b []byte) {
			s, err := event.Parse(b)
			if err != nil {
				return
			}
			if !bytes.Equal(s.Raw, b) {
				t.Fatal("event.Parse returned bytes other than the ones it was given")
			}
			if s.ID != frame.Hash(s.Head) {
				t.Fatal("an event's name is not the hash of its head")
			}
			again, err := event.Parse(s.Raw)
			if err != nil || again.ID != s.ID {
				t.Fatalf("an accepted frame did not parse a second time: %v", err)
			}
		})
	}

	// Noise.
	rng := mrand.New(mrand.NewSource(20260915))
	for i := 0; i < 2000; i++ {
		b := make([]byte, rng.Intn(8301))
		rng.Read(b)
		if i%7 == 0 && len(b) >= len(frame.Magic) {
			// Some of it wearing the right hat, so the walk past the magic is
			// exercised rather than stopped at the door.
			copy(b, frame.Magic)
		}
		if i%11 == 0 && len(b) >= frame.HeaderSize {
			copy(b, frame.Magic)
			b[len(frame.Magic)] = byte(frame.KindEvent)
		}
		check(b)
	}

	// Damage to something that really was a frame.
	spans := spansOf(valid.Raw)
	if len(spans) < 10 {
		t.Fatalf("the seed event has only %d fields in its head and body; the mutations need more", len(spans))
	}
	pairs := adjacent(spans)
	headEnd := len(valid.Head) - frame.SigSize
	if len(pairs) == 0 || spans[0].start != frame.HeaderSize || spans[len(spans)-1].end != len(valid.Raw) ||
		!(spans[0].end <= headEnd && spans[len(spans)-1].start >= len(valid.Head)) {
		t.Fatal("the walk does not cover both the head's and the body's fields")
	}
	kinds := 0
	for i := 0; i < 2000; i++ {
		b := append([]byte(nil), valid.Raw...)
		switch i % 8 {
		case 0: // one bit
			b[rng.Intn(len(b))] ^= 1 << uint(rng.Intn(8))
		case 1: // several bits
			for k := 0; k < 1+rng.Intn(6); k++ {
				b[rng.Intn(len(b))] ^= byte(1 + rng.Intn(255))
			}
		case 2: // cut
			b = b[:rng.Intn(len(b))]
		case 3: // padded
			extra := make([]byte, 1+rng.Intn(64))
			rng.Read(extra)
			b = append(b, extra...)
		case 4: // two fields swapped, so the tags no longer ascend
			x := pairs[rng.Intn(len(pairs))]
			a, c := spans[x], spans[x+1]
			out := append([]byte(nil), b[:a.start]...)
			out = append(out, b[c.start:c.end]...)
			out = append(out, b[a.start:a.end]...)
			out = append(out, b[c.end:]...)
			b = out
		case 5: // one field written twice
			x := rng.Intn(len(spans))
			a := spans[x]
			out := append([]byte(nil), b[:a.end]...)
			out = append(out, b[a.start:a.end]...)
			out = append(out, b[a.end:]...)
			b = out
		case 6: // a length that claims more than is there
			a := spans[rng.Intn(len(spans))]
			sizes := []uint32{0xFFFFFFFF, frame.MaxFrame + 1, uint32(len(b) * 4), 1 << 30}
			binary.BigEndian.PutUint32(b[a.start+2:a.start+6], sizes[rng.Intn(len(sizes))])
		case 7: // a length that claims nothing, which is not how absence is said
			a := spans[rng.Intn(len(spans))]
			binary.BigEndian.PutUint32(b[a.start+2:a.start+6], 0)
		}
		kinds |= 1 << uint(i%8)
		check(b)
	}
	if kinds != 0xFF {
		t.Fatalf("only %08b of the mutation kinds ran", kinds)
	}

	// The control: the undamaged frame still passes, so the wall above is not
	// simply refusing everything.
	if _, err := event.Parse(valid.Raw); err != nil {
		t.Fatalf("the seed frame stopped parsing: %v", err)
	}
	// And the earlier generation is named rather than called malformed: an
	// unread event is not a refuted one.
	old := append([]byte(nil), valid.Raw...)
	copy(old, frame.Superseded[len(frame.Superseded)-1])
	if _, _, err := frame.ParseHead(old); err == nil {
		t.Fatal("a frame of the earlier generation parsed under this verifier")
	}
}
