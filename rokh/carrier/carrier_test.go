package carrier

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"testing"

	"rokh/frame"
	"rokh/vessel"
)

// ptkSealer is the suite-0x02 sealer under PTK that stands in for the key
// layer in these tests (contract 3.1, E7): every record is sealed to the
// vessel's shared key.
type ptkSealer struct {
	k   []byte
	rnd io.Reader
}

func envAAD(typ byte, id frame.ID, at []byte) []byte {
	a := append([]byte("rokh/env/1"), typ)
	a = append(a, id[:]...)
	return append(a, at...)
}

func (s ptkSealer) SealAt(kind byte, id frame.ID, address string, at []byte, plain []byte) ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := io.ReadFull(s.rnd, salt); err != nil {
		return nil, err
	}
	k, _ := hkdf.Key(sha256.New, s.k, salt, "rokh/env/1/shared", 32)
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	kid := sha256.Sum256(append([]byte("rokh/kid/1"), s.k...))
	out := append([]byte{0x01, 0x02}, kid[:8]...)
	out = append(out, salt...)
	return g.Seal(out, make([]byte, 12), plain, envAAD(kind, id, at)), nil
}

func (s ptkSealer) OpenAt(kind byte, id frame.ID, at []byte, env []byte) ([]byte, error) {
	if len(env) < 2+8+32+16 || env[0] != 0x01 || env[1] != 0x02 {
		return nil, errors.New("not suite 0x02")
	}
	salt := env[10:42]
	k, _ := hkdf.Key(sha256.New, s.k, salt, "rokh/env/1/shared", 32)
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	return g.Open(nil, make([]byte, 12), env[42:], envAAD(kind, id, at))
}

func (s ptkSealer) Seal(kind byte, id frame.ID, address string, plain []byte) ([]byte, error) {
	return s.SealAt(kind, id, address, nil, plain)
}

func (s ptkSealer) Open(kind byte, id frame.ID, env []byte) ([]byte, error) {
	return s.OpenAt(kind, id, nil, env)
}

func stream(seed byte) *rand.ChaCha8 {
	var s [32]byte
	s[0] = seed
	return rand.NewChaCha8(s)
}

var vk = bytes.Repeat([]byte{9}, 32)

func unlock(slots [][]byte, salt []byte, iter int) ([]byte, error) { return vk, nil }

type holder struct{}

func (holder) Holds() error { return nil }

// head makes an RKH3-shaped head naming body. The signature is filler: the
// carrier checks shape and hashes, the ledger checks signatures.
func head(n int, body []byte) []byte {
	sum := sha256.Sum256(body)
	fs, _ := frame.EncodeFields(frame.Fields{
		{Tag: 0x0002, Value: bytes.Repeat([]byte{byte(n)}, 32)},
		{Tag: 0x0009, Value: []byte{byte(n), 1, 2, 3}},
		{Tag: 0x000A, Value: sum[:]},
	})
	h := []byte("RKH3\x01")
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(fs)))
	h = append(h, l[:]...)
	h = append(h, fs...)
	return append(h, bytes.Repeat([]byte{0xEE}, 64)...)
}

func newCarrier(t *testing.T, m vessel.Medium) *Carrier {
	t.Helper()
	anchor := frame.Hash(head(0, []byte("genesis body")))
	c, err := Create(m, vessel.Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(1)}, vk, nil, anchor, frame.Zero, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ptk, _ := vessel.SharedKey(vk)
	c.SetSealer(ptkSealer{k: ptk, rnd: stream(2)})
	return c
}

func recordEvent(t *testing.T, c *Carrier, n int, body string) frame.ID {
	t.Helper()
	bd := bodyAt("home/notes", body)
	h := head(n, bd)
	id := frame.Hash(h)
	r, err := c.Begin(holder{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Event(id, h, bd, "home/notes"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetRef("main", id); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatalf("recording %d: %s %v", n, out, err)
	}
	return id
}

// One recording is one commit: the event and its reference land together,
// and reopening serves head and body, checked against each other.
func TestOneRecordingIsOneCommit(t *testing.T) {
	m := vessel.NewMemory()
	c := newCarrier(t, m)
	g0 := c.Vessel().Info().Generation
	var ids []frame.ID
	for i := 0; i < 5; i++ {
		ids = append(ids, recordEvent(t, c, i+1, fmt.Sprintf("body %d", i)))
	}
	if c.Vessel().Info().Generation != g0+5 {
		t.Fatal("a recording was not exactly one commit")
	}
	ptk, _ := vessel.SharedKey(vk)
	d, _, err := Open(m, unlock, stream(3), ptkSealer{k: ptk, rnd: stream(4)})
	if err != nil {
		t.Fatal(err)
	}
	ref, ok, err := d.Ref("main")
	if err != nil || !ok || ref != ids[4] {
		t.Fatalf("branch: %v %v %v", ref, ok, err)
	}
	raw, err := d.Get(ids[2])
	if err != nil {
		t.Fatal(err)
	}
	bd2 := bodyAt("home/notes", "body 2")
	h := head(3, bd2)
	if !bytes.Equal(raw, append(append([]byte(nil), h...), bd2...)) {
		t.Fatal("head || body did not come back")
	}
	heads, _ := d.Heads()
	if len(heads) != 1 || heads[0] != ids[4] {
		t.Fatal("heads")
	}
	for _, n := range m.FileNames() {
		if !vessel.ValidName(n) {
			t.Fatalf("%s is outside the vessel grammar", n)
		}
	}
	// A session that cannot open the body sees the head only.
	e, _, _ := Open(m, unlock, stream(5), ptkSealer{k: bytes.Repeat([]byte{1}, 32), rnd: stream(6)})
	raw, err = e.Get(ids[2])
	if err != nil || !bytes.Equal(raw, h) {
		t.Fatalf("a head only: %v", err)
	}
}

// E1: a body that does not hash to its head's body field is refused, at
// recording and at reading, and nothing of it is shown.
func TestAForgedBodyIsRefused(t *testing.T) {
	c := newCarrier(t, vessel.NewMemory())
	real, other := bodyAt("a", "the real body"), bodyAt("a", "another body")
	h := head(1, real)
	id := frame.Hash(h)
	r, _ := c.Begin(holder{})
	if err := r.Event(id, h, other, "a"); !errors.Is(err, ErrForged) {
		t.Fatalf("recorded a body that its head does not name: %v", err)
	}
	if err := r.Event(frame.Hash([]byte("x")), h, real, "a"); !errors.Is(err, ErrForged) {
		t.Fatalf("recorded a head under another id: %v", err)
	}
	// A sealed forgery carried as an envelope is refused on reading.
	env, _ := c.s.Seal(vessel.RecEvent, id, "a", other)
	if err := r.EventEnvelope(id, h, env); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	if raw, err := c.Get(id); !errors.Is(err, ErrForged) || raw != nil {
		t.Fatalf("a forged body was served: %v", err)
	}
}

func contentOf(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/1000)
	}
	return b
}

func putContent(t *testing.T, c *Carrier, data []byte) frame.ID {
	t.Helper()
	r, _ := c.Begin(holder{})
	id, err := r.Content("home/files", uint64(len(data)), func() (io.Reader, error) { return bytes.NewReader(data), nil })
	if err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	return id
}

// Content lives inside the vessel and comes back whole, in chunks.
func TestContentComesBackWholeInChunks(t *testing.T) {
	m := vessel.NewMemory()
	c := newCarrier(t, m)
	for _, n := range []int{0, 1, 5000, vessel.MaxChunk(18), vessel.MaxChunk(18) + 1, 600000} {
		data := contentOf(n)
		id := putContent(t, c, data)
		if id != frame.Hash(data) {
			t.Fatal("content id is not its hash")
		}
		var out bytes.Buffer
		if _, err := c.ContentTo(id, &out); err != nil || !bytes.Equal(out.Bytes(), data) {
			t.Fatalf("%d bytes did not come back: %v", n, err)
		}
	}
}

// S3, C10, E8: swapped, dropped, repeated, truncated and conflicting chunks
// are refused before one byte is served.
func TestChunksBindTheirCoordinates(t *testing.T) {
	data := contentOf(600000) // three chunks at 2^18
	id := frame.Hash(data)
	size := uint64(len(data))
	max := vessel.MaxChunk(18)
	chunks := Chunks(size, max)
	part := func(i uint32) []byte {
		lo := int(i) * max
		hi := lo + int(chunkLen(i, chunks, size, max))
		return data[lo:hi]
	}
	type rec struct {
		chunk uint32
		at    []byte
		plain []byte
		size  uint64
	}
	honest := func(i uint32) rec { return rec{i, At(i, chunks, size, uint32(len(part(i)))), part(i), size} }
	cases := map[string]struct {
		recs []rec
		code string
	}{
		"swapped": {[]rec{
			{0, At(1, chunks, size, uint32(len(part(1)))), part(1), size},
			{1, At(0, chunks, size, uint32(len(part(0)))), part(0), size},
			honest(2)}, "content_incomplete"},
		"dropped":  {[]rec{honest(0), honest(2)}, "content_incomplete"},
		"repeated": {[]rec{honest(0), {1, At(0, chunks, size, uint32(len(part(0)))), part(0), size}, honest(2)}, "content_incomplete"},
		"truncated": {[]rec{honest(0), honest(1),
			{2, At(2, chunks, size, uint32(len(part(2))-10)), part(2)[:len(part(2))-10], size}}, "content_incomplete"},
		"conflicting": {[]rec{honest(0), honest(1), honest(2),
			{1, At(1, chunks, size, uint32(len(part(1)))), append([]byte{part(1)[0] ^ 1}, part(1)[1:]...), size}}, "content_conflict"},
		"wrong size": {[]rec{honest(0), honest(1), {2, At(2, chunks, size, uint32(len(part(2)))), part(2), size + 1}}, "content_incomplete"},
	}
	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			c := newCarrier(t, vessel.NewMemory())
			sealer := c.s.(ChunkSealer)
			r, _ := c.Begin(holder{})
			for _, x := range cs.recs {
				env, err := sealer.SealAt(vessel.RecContent, id, "a", x.at, x.plain)
				if err != nil {
					t.Fatal(err)
				}
				if err := r.ContentEnvelope(vessel.Header{Type: vessel.RecContent, ID: id, Chunk: x.chunk, Chunks: chunks, Size: x.size}, env); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := r.Commit(); err != nil || out != vessel.Recorded {
				t.Fatal(out, err)
			}
			var out bytes.Buffer
			_, err := c.ContentTo(id, &out)
			if Code(err) != cs.code {
				t.Fatalf("%s: %v, want %s", name, err, cs.code)
			}
			if out.Len() != 0 {
				t.Fatalf("%s: %d bytes were served before the refusal", name, out.Len())
			}
		})
	}
}

// Without the key nothing in the folder is readable: no body, no content,
// no branch name.
func TestNothingReadableInTheFolder(t *testing.T) {
	m := vessel.NewMemory()
	c := newCarrier(t, m)
	recordEvent(t, c, 1, "a secret body")
	putContent(t, c, []byte("secret content bytes"))
	for _, n := range m.FileNames() {
		b := m.Get(n)
		for _, needle := range []string{"a secret body", "secret content", "main", "home/notes"} {
			if bytes.Contains(b, []byte(needle)) {
				t.Fatalf("%q is readable in %s", needle, n)
			}
		}
	}
}

// E7: a branch pointer is sealed under suite 0x02 with this vessel's PTK, so
// any key holding VK reads it, and another vessel's shared key does not.
func TestBranchPointersAreUnderTheSharedKey(t *testing.T) {
	c := newCarrier(t, vessel.NewMemory())
	recordEvent(t, c, 1, "one")
	found := 0
	c.Vessel().Scan(func(h vessel.Header, r vessel.Ref) error {
		if h.Type != vessel.RecPointer {
			return nil
		}
		env, err := c.Vessel().Body(r)
		if err != nil {
			t.Fatal(err)
		}
		if len(env) < 2 || env[0] != 0x01 || env[1] != 0x02 {
			t.Fatalf("a branch pointer is not suite 0x02: % x", env[:2])
		}
		found++
		return nil
	})
	if found == 0 {
		t.Fatal("no branch pointer was written")
	}
	// A session with no sealer at all still reads the branches: PTK is the key.
	if refs, err := Wrap(c.Vessel(), nil).Refs(); err != nil || len(refs) != 1 {
		t.Fatalf("branches under PTK: %v %v", refs, err)
	}
}

// Review 002, note: a record's size is a locator; the signed descriptor's is
// the truth. A planted record of another size, arriving first, does not keep
// the true content from being served when the size comes from the descriptor.
func TestTheSizeComesFromTheDescriptor(t *testing.T) {
	c := newCarrier(t, vessel.NewMemory())
	data := contentOf(600000)
	id := frame.Hash(data)
	sealer := c.s.(ChunkSealer)
	r, _ := c.Begin(holder{})
	env, _ := sealer.SealAt(vessel.RecContent, id, "a", At(0, 1, 7, 7), []byte("planted"))
	if err := r.ContentEnvelope(vessel.Header{Type: vessel.RecContent, ID: id, Chunk: 0, Chunks: 1, Size: 7}, env); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	putContent(t, c, data)
	var out bytes.Buffer
	if _, err := c.ContentToSized(id, uint64(len(data)), &out); err != nil || !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("the content with its descriptor's size: %v", err)
	}
	if _, err := c.ContentTo(id, io.Discard); err == nil {
		t.Log("ContentTo took the size of a true record")
	}
}

// At the carrier: branch and event readers while recordings land.
func TestCarrierReadersWhileRecording(t *testing.T) {
	c := newCarrier(t, vessel.NewMemory())
	recordEvent(t, c, 1, "first")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = c.Refs()
			_ = c.Events(func(e EventRecord) error { _, _ = c.Body(e); return nil }, nil)
		}
	}()
	for i := 2; i < 30; i++ {
		recordEvent(t, c, i, "more")
	}
	close(stop)
	<-done
}

// Review 003 note: Get reads through an id index that follows the vessel's
// generation: a record recorded after the index was built is found, and
// readers may Get while recordings land.
func TestGetFollowsNewRecordingsThroughItsIndex(t *testing.T) {
	c := newCarrier(t, vessel.NewMemory())
	first := recordEvent(t, c, 1, "first")
	if _, err := c.Get(first); err != nil {
		t.Fatal(err)
	}
	second := recordEvent(t, c, 2, "second")
	raw, err := c.Get(second)
	if err != nil || !bytes.Contains(raw, []byte("second")) {
		t.Fatalf("a record after the index was built: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			if _, err := c.Get(first); err != nil {
				t.Errorf("get while recording: %v", err)
				return
			}
		}
	}()
	for i := 3; i < 12; i++ {
		recordEvent(t, c, i, "more")
	}
	<-done
}

// Readers that Get over and over while recordings land never
// see a stale reference, and each read opens the record whole.
func TestConcurrentGetsNeverAnswerAStaleReference(t *testing.T) {
	c := newCarrier(t, vessel.NewMemory())
	first := recordEvent(t, c, 1, "first")
	const readers = 4
	stop := make(chan struct{})
	errs := make(chan error, readers)
	for r := 0; r < readers; r++ {
		go func() {
			for {
				select {
				case <-stop:
					errs <- nil
					return
				default:
				}
				raw, err := c.Get(first)
				if err == nil && !bytes.Contains(raw, []byte("first")) {
					err = fmt.Errorf("the body did not open (%d bytes)", len(raw))
				}
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	for i := 2; i < 40; i++ {
		recordEvent(t, c, i, "more")
	}
	close(stop)
	for r := 0; r < readers; r++ {
		if err := <-errs; err != nil {
			t.Errorf("get while recording: %v", err)
		}
	}
}

// A ledger's load reads every event once: Get over n events is linear.
func TestGetOverManyEventsIsLinear(t *testing.T) {
	c := newCarrier(t, vessel.NewMemory())
	var ids []frame.ID
	r, _ := c.Begin(holder{})
	for i := 0; i < 2000; i++ {
		bd := bodyAt("home/notes", fmt.Sprintf("event %d", i))
		h := head(i%250+1, bd)
		id := frame.Hash(h)
		if err := r.Event(id, h, bd, "home/notes"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	// The core's tests read no clock (S4); the timing is the bench's.
	for _, id := range ids {
		if _, err := c.Get(id); err != nil {
			t.Fatal(err)
		}
	}
}
