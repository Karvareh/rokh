// Package carrier is the bag: a folder whose whole footprint is one vessel
// (contract sections 1 to 3). Events and content live inside it; one
// recording is one vessel commit, the event and its reference together.
//
// The carrier is portable core (C1). It holds no file call, no clock and no
// lock: the vessel's Medium, the writer's Owner and every random byte come
// from the host. Sealing comes through a Sealer, which the key layer
// implements; the carrier never sees a passphrase.
//
// A header inside the vessel proves nothing (C8). An event's head proves
// itself by hashing to its id; its body is served only when it hashes to the
// body field of that head (E1). Content is served only when every chunk has
// opened at its own coordinates and the whole has hashed to its id (E8, C10).
package carrier

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"rokh/frame"
	"rokh/key"
	"rokh/vessel"
)

// Sealer seals and opens the envelope of one record. kind is the record type
// (vessel.RecEvent, RecContent, RecPointer, RecObject); address is where the
// record is written, which decides its readers.
type Sealer interface {
	Seal(kind byte, id frame.ID, address string, plain []byte) ([]byte, error)
	Open(kind byte, id frame.ID, envelope []byte) ([]byte, error)
}

// ChunkSealer binds a chunk's coordinates into its envelope (E8). Content is
// refused by a carrier whose sealer does not offer it.
type ChunkSealer interface {
	SealAt(kind byte, id frame.ID, address string, at []byte, plain []byte) ([]byte, error)
	OpenAt(kind byte, id frame.ID, at []byte, envelope []byte) ([]byte, error)
}

// Errors with stable codes (contract B7).
var (
	ErrConflict   = &codeError{"content_conflict", "two chunks at the same coordinates differ"}
	ErrIncomplete = &codeError{"content_incomplete", "a chunk of the content is missing"}
	ErrForged     = errors.New("carrier: a record does not match what names it; nothing of it is shown")
	ErrNotFound   = errors.New("carrier: not found")
	ErrNoChunks   = errors.New("carrier: the sealer does not bind chunk coordinates")
	ErrNoSealer   = errors.New("carrier: no key is open in this session; envelopes stay closed")
)

// sealer returns the session's sealer, or an error when there is none.
func (c *Carrier) sealer() (Sealer, error) {
	if c.s == nil {
		return nil, ErrNoSealer
	}
	return c.s, nil
}

type codeError struct{ code, msg string }

func (e *codeError) Error() string { return "carrier: " + e.msg }

// Code returns the stable code of an error from the carrier or its vessel.
func Code(err error) string {
	var ce *codeError
	if errors.As(err, &ce) {
		return ce.code
	}
	return vessel.Code(err)
}

// Footprint is everything a carrier puts in its folder.
func Footprint() []string { return vessel.Footprint() }

// RefName is the pointer name of a branch.
func RefName(branch string) frame.ID { return frame.Hash([]byte("rokh/ref/" + branch)) }

// Carrier is an opened carrier.
type Carrier struct {
	v      *vessel.Vessel
	s      Sealer
	covers func(id frame.ID, env []byte) bool

	// The id index Get reads, rebuilt only when the vessel has moved to
	// another generation, so a ledger load reads the records once rather than
	// once per event. mu guards it for concurrent readers.
	mu    sync.Mutex
	byID  map[frame.ID]EventRecord
	byGen uint64
}

// Create makes a new carrier: a new vessel whose anchor is the genesis id.
// The genesis itself is recorded by the first Recording.
func Create(m vessel.Medium, p vessel.Params, vk []byte, slots [][]byte, anchor frame.ID, seed frame.ID, scopes []string, s Sealer) (*Carrier, error) {
	return CreateRoot(m, p, vk, slots, vessel.Root{Anchor: anchor, Seed: seed, Scopes: scopes}, s)
}

// CreateRoot is Create with the root record given whole: a seed's folder may
// be made with the vessel id its seed names (a zero id is drawn at random, as
// Create draws it).
func CreateRoot(m vessel.Medium, p vessel.Params, vk []byte, slots [][]byte, root vessel.Root, s Sealer) (*Carrier, error) {
	v, err := vessel.Create(m, p, vk, slots, root)
	if err != nil {
		return nil, err
	}
	return &Carrier{v: v, s: s}, nil
}

// Open opens a carrier's vessel with the key layer's unlock.
func Open(m vessel.Medium, u vessel.Unlock, rnd io.Reader, s Sealer) (*Carrier, vessel.Report, error) {
	v, rep, err := vessel.Open(m, u, rnd)
	if err != nil {
		return nil, rep, err
	}
	return &Carrier{v: v, s: s}, rep, nil
}

// Wrap makes a carrier of a vessel already open.
func Wrap(v *vessel.Vessel, s Sealer) *Carrier { return &Carrier{v: v, s: s} }

// SetSealer replaces the sealer, once the keyring of the carrier is known.
func (c *Carrier) SetSealer(s Sealer) { c.s = s }

// Vessel is the carrier's vessel.
func (c *Carrier) Vessel() *vessel.Vessel { return c.v }

// Anchor is the genesis id.
func (c *Carrier) Anchor() frame.ID { return c.v.Info().Anchor }

// Moved reports whether another writer may have committed to this carrier
// since this opening last read it: its references and records are then not
// the carrier's present ones. It reads one head file and changes nothing
// (vessel.Moved).
func (c *Carrier) Moved() bool { return c.v.Moved() }

// Refresh brings this opening to what the carrier holds now: the vessel's
// present generation, and with it the present references and records
// (vessel.Refresh). It reports whether anything changed. It writes nothing.
// A door calls it under the writing turn before it judges a precondition or
// takes a branch's head as a parent, and before it answers from a view that
// the carrier may have moved past.
func (c *Carrier) Refresh() (bool, error) {
	moved, err := c.v.Refresh()
	if moved {
		c.mu.Lock()
		c.byID = nil
		c.mu.Unlock()
	}
	return moved, err
}

// ---------- recording ----------

// Recording is one commit in preparation.
type Recording struct {
	c  *Carrier
	tx *vessel.Tx
	// s is this recording's own sealer, when one was given: a session at the
	// recording's own point.
	s Sealer
}

// SealWith gives this recording a sealer of its own for the events and the
// content it adds from here on: a session at the recording's own point, the
// parents of the event it records, so that each record is sealed to the
// readers of that point (contract E3, E4) and not to a keyring read earlier.
// Without it the carrier's sealer seals.
func (r *Recording) SealWith(s Sealer) { r.s = s }

// sealer is the recording's own sealer, or the carrier's.
func (r *Recording) sealer() (Sealer, error) {
	if r.s != nil {
		return r.s, nil
	}
	return r.c.sealer()
}

// Begin starts a recording under the host's owner.
func (c *Carrier) Begin(own vessel.Owner) (*Recording, error) {
	tx, err := c.v.Begin(own)
	if err != nil {
		return nil, err
	}
	return &Recording{c: c, tx: tx}, nil
}

// Tx is the vessel transaction under the recording, for slots and the root.
func (r *Recording) Tx() *vessel.Tx { return r.tx }

// Event adds an event: its signed head in the clear of the pack and its body
// sealed for the readers of address. id must be the hash of head and the head
// must name the hash of body.
func (r *Recording) Event(id frame.ID, head, body []byte, address string) error {
	if frame.Hash(head) != id {
		return fmt.Errorf("%w: the head does not hash to %s", ErrForged, id.Short())
	}
	want, err := HeadBody(head)
	if err != nil {
		return err
	}
	if sha256.Sum256(body) != want {
		return fmt.Errorf("%w: the body does not hash to the head's body field", ErrForged)
	}
	// The readers follow from the address, and the address is the body's
	// own (tag 0x0005): the caller's word must agree with it.
	if got, ok := BodyAddress(body); !ok || got != address {
		return fmt.Errorf("%w: the body is written at %q, not at %q", ErrForged, got, address)
	}
	s, err := r.sealer()
	if err != nil {
		return err
	}
	env, err := s.Seal(vessel.RecEvent, id, address, body)
	if err != nil {
		return err
	}
	return r.tx.Put(vessel.Header{Type: vessel.RecEvent, ID: id, Head: head}, env)
}

// SetCovers gives the carrier the key layer's check that an envelope names
// the owner's live readers (E3). Envelopes carried in by union and seeding
// are refused at commit when it answers no.
//
// The id is the record's own: an event's id, or a content id, whose signed
// descriptor gives the historical point; the check refuses a point it does
// not know.
func (c *Carrier) SetCovers(f func(id frame.ID, env []byte) bool) { c.covers = f }

// EventEnvelope adds an event whose body is already sealed, as union and
// seeding carry it: the envelope travels unopened.
func (r *Recording) EventEnvelope(id frame.ID, head, envelope []byte) error {
	if frame.Hash(head) != id {
		return fmt.Errorf("%w: the head does not hash to %s", ErrForged, id.Short())
	}
	if r.c.covers != nil && !bytes.Equal(envelope, HeadOnly) && !r.c.covers(id, envelope) {
		return fmt.Errorf("%w: the envelope of %s does not name the owner", ErrForged, id.Short())
	}
	return r.tx.Put(vessel.Header{Type: vessel.RecEvent, ID: id, Head: head}, envelope)
}

// SetRef points a branch at an event (pointer space 0x01).
func (r *Recording) SetRef(branch string, id frame.ID) error {
	if branch == "" || strings.ContainsAny(branch, "/\\") {
		return fmt.Errorf("carrier: bad branch name %q", branch)
	}
	name := RefName(branch)
	plain := append([]byte(branch+"\x00"), id[:]...)
	// Branch references are pointers under suite 0x02 with K = PTK (E7):
	// every key that holds this vessel's VK reads them, and nothing else.
	env, err := key.SealShared(vessel.RecPointer, name, nil, r.c.v.SharedKey(), plain, r.c.v.Rand())
	if err != nil {
		return err
	}
	return r.tx.Put(vessel.Header{Type: vessel.RecPointer, Space: vessel.SpaceRefs, Name: name}, env)
}

// At is the chunk coordinates bound into a chunk's envelope:
// chunk(u32) || chunks(u32) || size(u64) || length(u32).
func At(chunk, chunks uint32, size uint64, length uint32) []byte {
	b := make([]byte, 20)
	binary.BigEndian.PutUint32(b[0:4], chunk)
	binary.BigEndian.PutUint32(b[4:8], chunks)
	binary.BigEndian.PutUint64(b[8:16], size)
	binary.BigEndian.PutUint32(b[16:20], length)
	return b
}

// Chunks is the chunk count and the length of chunk i for a size, with a
// maximum chunk length.
func Chunks(size uint64, max int) uint32 {
	if size == 0 {
		return 1
	}
	return uint32((size + uint64(max) - 1) / uint64(max))
}

func chunkLen(i, chunks uint32, size uint64, max int) uint32 {
	if i+1 < chunks {
		return uint32(max)
	}
	return uint32(size - uint64(i)*uint64(max))
}

// Content adds content read from src, chunk by chunk, sealed for the readers
// of address. It returns the content id (SHA-256 of the whole). src is read
// twice: once to name it, once to seal it, so memory stays one chunk.
func (r *Recording) Content(address string, size uint64, open func() (io.Reader, error)) (frame.ID, error) {
	s, err := r.sealer()
	if err != nil {
		return frame.Zero, err
	}
	cs, ok := s.(ChunkSealer)
	if !ok {
		return frame.Zero, ErrNoChunks
	}
	max := vessel.MaxChunk(r.c.v.Info().SlabLog2)
	h := sha256.New()
	src, err := open()
	if err != nil {
		return frame.Zero, err
	}
	if n, err := io.Copy(h, io.LimitReader(src, int64(size)+1)); err != nil || uint64(n) != size {
		return frame.Zero, fmt.Errorf("carrier: content is %d bytes, not %d (%v)", n, size, err)
	}
	var id frame.ID
	copy(id[:], h.Sum(nil))
	src, err = open()
	if err != nil {
		return frame.Zero, err
	}
	chunks := Chunks(size, max)
	buf := make([]byte, max)
	for i := uint32(0); i < chunks; i++ {
		l := chunkLen(i, chunks, size, max)
		if _, err := io.ReadFull(src, buf[:l]); err != nil {
			return frame.Zero, err
		}
		env, err := cs.SealAt(vessel.RecContent, id, address, At(i, chunks, size, l), buf[:l])
		if err != nil {
			return frame.Zero, err
		}
		if err := r.tx.Put(vessel.Header{Type: vessel.RecContent, ID: id, Chunk: i, Chunks: chunks, Size: size}, env); err != nil {
			return frame.Zero, err
		}
	}
	return id, nil
}

// ContentEnvelope adds one sealed chunk as union and seeding carry it.
func (r *Recording) ContentEnvelope(h vessel.Header, envelope []byte) error {
	if h.Type != vessel.RecContent {
		return fmt.Errorf("carrier: not a content record")
	}
	if r.c.covers != nil && !r.c.covers(h.ID, envelope) {
		return fmt.Errorf("%w: a chunk of %s does not name the owner", ErrForged, h.ID.Short())
	}
	return r.tx.Put(h, envelope)
}

// Commit takes the recording through the vessel's commit point.
func (r *Recording) Commit() (vessel.Outcome, error) { return r.tx.CommitGrowing() }

// Abandon drops the recording.
func (r *Recording) Abandon() error { return r.tx.Abandon() }

// ---------- reading ----------

// HeadBody returns the body hash an RKH3 head names (tag 0x000A). It checks
// the head's shape only; the signature is the ledger's to verify.
func HeadBody(head []byte) ([32]byte, error) {
	var out [32]byte
	if len(head) < 4+1+4+64 || string(head[:4]) != "RKH3" || head[4] != 0x01 {
		return out, fmt.Errorf("%w: not an RKH3 head", ErrForged)
	}
	n := binary.BigEndian.Uint32(head[5:9])
	if uint64(n)+9+64 != uint64(len(head)) {
		return out, fmt.Errorf("%w: head length", ErrForged)
	}
	fs, err := frame.DecodeFields(head[9 : 9+n])
	if err != nil {
		return out, fmt.Errorf("%w: head fields: %v", ErrForged, err)
	}
	v, ok := fs.Get(0x000A)
	if !ok || len(v) != 32 {
		return out, fmt.Errorf("%w: the head names no body", ErrForged)
	}
	copy(out[:], v)
	return out, nil
}

// BodyAddress reads the address field (tag 0x0005) of an RKH3 body.
func BodyAddress(body []byte) (string, bool) {
	fs, err := frame.DecodeFields(body)
	if err != nil {
		return "", false
	}
	v, ok := fs.Get(0x0005)
	return string(v), ok
}

// EventRecord is one event as the carrier holds it. One id may sit in
// several envelopes (E2): Refs lists every whole record of it, in arrival
// order; Ref is the first of them, or the head only when there is none.
type EventRecord struct {
	ID   frame.ID
	Head []byte
	Ref  vessel.Ref
	Refs []vessel.Ref
}

// HeadOnly is the envelope of an event whose body did not travel: a head
// proves lineage and authorship and nothing of content (3.3). Version 0x00 is
// unread by every reader, so the body is simply absent.
var HeadOnly = []byte{0x00}

// IsHeadOnly reports whether a record reference holds the HeadOnly envelope.
func IsHeadOnly(r vessel.Ref) bool { return r.Len == uint32(len(HeadOnly)) }

// Events lists every event head that proves itself, once, in the order it
// first arrived: the lineage index. Every whole record of an id is kept in
// Refs, so a reader can be given the envelope that opens for it (E2). A
// record whose head does not hash to its id is skipped and reported through
// bad.
func (c *Carrier) Events(fn func(EventRecord) error, bad func(frame.ID, error)) error {
	var g gather
	err := c.v.Scan(func(h vessel.Header, r vessel.Ref) error {
		g.add(h, r, bad)
		return nil
	})
	if err != nil {
		return err
	}
	for _, e := range g.list {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// gather is the rule of the lineage index: event records in the order they
// first arrived, every whole record of an id in Refs.
type gather struct {
	pos  map[frame.ID]int
	list []EventRecord
}

func (g *gather) add(h vessel.Header, r vessel.Ref, bad func(frame.ID, error)) {
	if h.Type != vessel.RecEvent {
		return
	}
	if frame.Hash(h.Head) != h.ID {
		if bad != nil {
			bad(h.ID, ErrForged)
		}
		return
	}
	if g.pos == nil {
		g.pos = map[frame.ID]int{}
	}
	i, ok := g.pos[h.ID]
	if !ok {
		i = len(g.list)
		g.pos[h.ID] = i
		g.list = append(g.list, EventRecord{ID: h.ID, Head: h.Head, Ref: r})
	}
	if !IsHeadOnly(r) {
		if len(g.list[i].Refs) == 0 {
			g.list[i].Ref = r
		}
		g.list[i].Refs = append(g.list[i].Refs, r)
	}
}

// Body opens an event's body and checks it against the head (E1). An event
// whose body this session cannot open is a head only.
func (c *Carrier) Body(e EventRecord) ([]byte, error) {
	return c.bodyFrom(e, c.v.Body)
}

// bodyFrom is Body with each envelope read by read.
func (c *Carrier) bodyFrom(e EventRecord, read func(vessel.Ref) ([]byte, error)) ([]byte, error) {
	refs := e.Refs
	if len(refs) == 0 && !IsHeadOnly(e.Ref) {
		refs = []vessel.Ref{e.Ref}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("%w: event %s is held as a head only", ErrNotFound, e.ID.Short())
	}
	s, err := c.sealer()
	if err != nil {
		return nil, err
	}
	want, err := HeadBody(e.Head)
	if err != nil {
		return nil, err
	}
	// Every envelope of the id is tried; the first that opens for this
	// session and hashes to the head's body field is the body (E2). One that
	// opens to other bytes is a forgery, and nothing of it is shown.
	var last error = fmt.Errorf("%w: no envelope of event %s opens for this session", ErrNotFound, e.ID.Short())
	for _, r := range refs {
		env, err := read(r)
		if err != nil {
			return nil, err
		}
		body, err := s.Open(vessel.RecEvent, e.ID, env)
		if err != nil {
			last = err
			continue
		}
		if sha256.Sum256(body) != want {
			return nil, fmt.Errorf("%w: event %s", ErrForged, e.ID.Short())
		}
		return body, nil
	}
	return nil, last
}

// Get returns an event's bytes, head || body, or the head only when the body
// does not open for this session. It is the getter a ledger loads with.
func (c *Carrier) Get(id frame.ID) ([]byte, error) {
	e, ok, err := c.record(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: event %s", ErrNotFound, id.Short())
	}
	body, err := c.Body(e)
	if errors.Is(err, vessel.ErrStale) {
		// A commit moved the record after the index was read. A second try
		// through a new index can lose the same race to the next commit, so
		// it finds the record and reads its envelopes at one generation, under
		// one hold of the vessel's lock.
		var read func(vessel.Ref) ([]byte, error)
		e, read, ok, err = c.recordNow(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: event %s", ErrNotFound, id.Short())
		}
		body, err = c.bodyFrom(e, read)
	}
	if err != nil {
		if Code(err) == "vessel_corrupt" || errors.Is(err, ErrForged) || errors.Is(err, vessel.ErrStale) {
			return nil, err
		}
		return append([]byte(nil), e.Head...), nil
	}
	return append(append([]byte(nil), e.Head...), body...), nil
}

// Head returns an event's signed head alone. A head proves itself: it hashes
// to the id that names it, and carries its author's signature, which the
// ledger verifies. Nothing is opened.
func (c *Carrier) Head(id frame.ID) ([]byte, error) {
	e, ok, err := c.record(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: event %s", ErrNotFound, id.Short())
	}
	return append([]byte(nil), e.Head...), nil
}

// Envelopes returns every sealed body this carrier holds for an event, in
// the order they arrived (E2); an event held as a head only has none. Nothing
// is opened: which envelope opens, for whom and at which point, is the key
// layer's to judge (contract 3.3, E3).
func (c *Carrier) Envelopes(id frame.ID) ([][]byte, error) {
	e, ok, err := c.record(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: event %s", ErrNotFound, id.Short())
	}
	out, err := envelopesOf(e, c.v.Body)
	if errors.Is(err, vessel.ErrStale) {
		// A commit moved the record after the index was read: read it again
		// at one generation, as Get does.
		var read func(vessel.Ref) ([]byte, error)
		e, read, ok, err = c.recordNow(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: event %s", ErrNotFound, id.Short())
		}
		out, err = envelopesOf(e, read)
	}
	return out, err
}

func envelopesOf(e EventRecord, read func(vessel.Ref) ([]byte, error)) ([][]byte, error) {
	refs := e.Refs
	if len(refs) == 0 && !IsHeadOnly(e.Ref) {
		refs = []vessel.Ref{e.Ref}
	}
	out := make([][]byte, 0, len(refs))
	for _, r := range refs {
		env, err := read(r)
		if err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, nil
}

// recordNow finds an event and reads every envelope of it in one Find, at the
// vessel's present generation. The reader it returns serves those envelopes.
func (c *Carrier) recordNow(id frame.ID) (EventRecord, func(vessel.Ref) ([]byte, error), bool, error) {
	found, err := c.v.Find(func(h vessel.Header) bool { return h.Type == vessel.RecEvent && h.ID == id })
	if err != nil {
		return EventRecord{}, nil, false, err
	}
	var g gather
	envs := map[vessel.Ref][]byte{}
	for _, f := range found {
		g.add(f.Header, f.Ref, nil)
		envs[f.Ref] = f.Env
	}
	if len(g.list) == 0 {
		return EventRecord{}, nil, false, nil
	}
	read := func(r vessel.Ref) ([]byte, error) {
		env, ok := envs[r]
		if !ok {
			return nil, fmt.Errorf("%w (slab %d)", vessel.ErrStale, r.Slab)
		}
		return env, nil
	}
	return g.list[0], read, true, nil
}

// record finds an event through the id index, rebuilt when the vessel has
// moved to another generation.
func (c *Carrier) record(id frame.ID) (EventRecord, bool, error) {
	gen := c.v.Info().Generation
	c.mu.Lock()
	if c.byID == nil || c.byGen != gen {
		c.mu.Unlock()
		idx := map[frame.ID]EventRecord{}
		if err := c.Events(func(e EventRecord) error { idx[e.ID] = e; return nil }, nil); err != nil {
			return EventRecord{}, false, err
		}
		c.mu.Lock()
		c.byID, c.byGen = idx, gen
	}
	e, ok := c.byID[id]
	c.mu.Unlock()
	return e, ok, nil
}

// Refs returns every branch and the event it names; the latest commit wins.
func (c *Carrier) Refs() (map[string]frame.ID, error) {
	out := map[string]frame.ID{}
	err := c.v.Scan(func(h vessel.Header, r vessel.Ref) error {
		if h.Type != vessel.RecPointer || h.Space != vessel.SpaceRefs {
			return nil
		}
		env, err := c.v.Body(r)
		if err != nil {
			return err
		}
		plain, err := key.OpenShared(vessel.RecPointer, h.Name, nil, env, c.v.SharedKey())
		if err != nil {
			return fmt.Errorf("%w: a branch pointer does not open under this vessel's shared key", ErrForged)
		}
		i := bytes.IndexByte(plain, 0)
		if i < 1 || len(plain) != i+1+32 || RefName(string(plain[:i])) != h.Name {
			return fmt.Errorf("%w: a branch pointer", ErrForged)
		}
		var id frame.ID
		copy(id[:], plain[i+1:])
		out[string(plain[:i])] = id
		return nil
	})
	return out, err
}

// Ref returns one branch.
func (c *Carrier) Ref(branch string) (frame.ID, bool, error) {
	refs, err := c.Refs()
	if err != nil {
		return frame.Zero, false, err
	}
	id, ok := refs[branch]
	return id, ok, nil
}

// Heads is every branch tip, sorted: the commit points a ledger walks from.
func (c *Carrier) Heads() ([]frame.ID, error) {
	refs, err := c.Refs()
	if err != nil {
		return nil, err
	}
	seen := map[frame.ID]bool{}
	var out []frame.ID
	for _, id := range refs {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Compare(out[j-1]) < 0; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

type chunkRec struct {
	h vessel.Header
	r vessel.Ref
}

// ContentTo writes content to w only after every chunk has opened at its own
// coordinates and the whole has hashed to id (E8). Memory stays one chunk:
// the chunks are opened twice, once to prove the whole and once to serve it.
func (c *Carrier) ContentTo(id frame.ID, w io.Writer) (uint64, error) {
	return c.contentTo(id, 0, false, w)
}

// ContentToSized is ContentTo for content whose size a signed descriptor
// names: the size is taken from the descriptor, never from a record, so a
// planted record of another size changes nothing.
func (c *Carrier) ContentToSized(id frame.ID, size uint64, w io.Writer) (uint64, error) {
	return c.contentTo(id, size, true, w)
}

func (c *Carrier) contentTo(id frame.ID, known uint64, sized bool, w io.Writer) (uint64, error) {
	cs, ok := c.s.(ChunkSealer)
	if !ok {
		return 0, ErrNoChunks
	}
	max := vessel.MaxChunk(c.v.Info().SlabLog2)
	var recs []chunkRec
	if err := c.v.Scan(func(h vessel.Header, r vessel.Ref) error {
		if h.Type == vessel.RecContent && h.ID == id {
			recs = append(recs, chunkRec{h, r})
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if len(recs) == 0 {
		return 0, fmt.Errorf("%w: content %s", ErrNotFound, id.Short())
	}
	// The coordinates follow from the size; a record that disagrees does not
	// open. Two records at one index must open to the same bytes.
	size := recs[0].h.Size
	if sized {
		size = known
	}
	chunks := Chunks(size, max)
	byIndex := make([][]chunkRec, chunks)
	for _, rc := range recs {
		if rc.h.Size != size || rc.h.Chunks != chunks || rc.h.Chunk >= chunks {
			continue // its coordinates disagree with the size: it does not open
		}
		byIndex[rc.h.Chunk] = append(byIndex[rc.h.Chunk], rc)
	}
	open := func(i uint32) ([]byte, error) {
		var got []byte
		found := false
		for _, rc := range byIndex[i] {
			env, err := c.v.Body(rc.r)
			if err != nil {
				return nil, err
			}
			l := chunkLen(i, chunks, size, max)
			p, err := cs.OpenAt(vessel.RecContent, id, At(i, chunks, size, l), env)
			if err != nil || uint32(len(p)) != l {
				continue // not openable at its coordinates
			}
			if found && !bytes.Equal(got, p) {
				return nil, ErrConflict
			}
			got, found = p, true
		}
		if !found {
			return nil, ErrIncomplete
		}
		return got, nil
	}
	h := sha256.New()
	for i := uint32(0); i < chunks; i++ {
		p, err := open(i)
		if err != nil {
			return 0, err
		}
		h.Write(p)
	}
	if !bytes.Equal(h.Sum(nil), id[:]) {
		return 0, fmt.Errorf("%w: content %s does not hash to its id", ErrForged, id.Short())
	}
	var n uint64
	for i := uint32(0); i < chunks; i++ {
		p, err := open(i)
		if err != nil {
			return n, err
		}
		if _, err := w.Write(p); err != nil {
			return n, err
		}
		n += uint64(len(p))
	}
	return n, nil
}
