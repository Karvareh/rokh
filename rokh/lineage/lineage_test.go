package lineage

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
	"sort"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

// kSealer seals every record to one shared key K, standing in for the key
// layer (suite 0x02): both seeds of a rokh hold it in these tests.
type kSealer struct {
	k   []byte
	rnd io.Reader
}

func envAAD(typ byte, id frame.ID, at []byte) []byte {
	a := append([]byte("rokh/env/1"), typ)
	a = append(a, id[:]...)
	return append(a, at...)
}

func (s kSealer) SealAt(kind byte, id frame.ID, address string, at []byte, plain []byte) ([]byte, error) {
	salt := make([]byte, 32)
	io.ReadFull(s.rnd, salt)
	k, _ := hkdf.Key(sha256.New, s.k, salt, "rokh/env/1/shared", 32)
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	out := append([]byte{0x01, 0x02}, make([]byte, 8)...)
	out = append(out, salt...)
	return g.Seal(out, make([]byte, 12), plain, envAAD(kind, id, at)), nil
}

func (s kSealer) OpenAt(kind byte, id frame.ID, at []byte, env []byte) ([]byte, error) {
	if len(env) < 58 || env[0] != 0x01 || env[1] != 0x02 {
		return nil, errors.New("unread")
	}
	k, _ := hkdf.Key(sha256.New, s.k, env[10:42], "rokh/env/1/shared", 32)
	blk, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(blk)
	return g.Open(nil, make([]byte, 12), env[42:], envAAD(kind, id, at))
}

func (s kSealer) Seal(kind byte, id frame.ID, address string, plain []byte) ([]byte, error) {
	return s.SealAt(kind, id, address, nil, plain)
}

func (s kSealer) Open(kind byte, id frame.ID, env []byte) ([]byte, error) {
	return s.OpenAt(kind, id, nil, env)
}

func stream(seed byte) *rand.ChaCha8 {
	var s [32]byte
	s[0] = seed
	return rand.NewChaCha8(s)
}

var sharedK = bytes.Repeat([]byte{4}, 32)

type holder struct{ no bool }

func (h *holder) Holds() error {
	if h.no {
		return errors.New("the lock is gone")
	}
	return nil
}

// body is an RKH3-shaped body: address, verb, payload, salt.
func body(addr, verb, payload string) []byte {
	fs := frame.Fields{
		{Tag: 0x0005, Value: []byte(addr)},
		{Tag: 0x0006, Value: []byte(verb)},
		{Tag: 0x000C, Value: bytes.Repeat([]byte{byte(len(payload))}, 32)},
	}
	if payload != "" {
		fs = append(fs, frame.Field{Tag: 0x0007, Value: []byte(payload)})
	}
	b, _ := frame.EncodeFields(fs)
	return b
}

// head is an RKH3-shaped head with parents; system marks the rokh address.
func head(author byte, parents []frame.ID, bd []byte, system bool) []byte {
	sum := sha256.Sum256(bd)
	fs := frame.Fields{
		{Tag: 0x0002, Value: bytes.Repeat([]byte{author}, 32)},
		{Tag: 0x0009, Value: sum[:4]},
		{Tag: 0x000A, Value: sum[:]},
	}
	if len(parents) > 0 {
		var ps []byte
		sort.Slice(parents, func(i, j int) bool { return parents[i].Compare(parents[j]) < 0 })
		for _, p := range parents {
			ps = append(ps, p[:]...)
		}
		fs = append(fs, frame.Field{Tag: 0x0004, Value: ps})
	}
	if system {
		fs = append(fs, frame.Field{Tag: 0x000B, Value: []byte{1}})
	}
	enc, _ := frame.EncodeFields(fs)
	h := []byte("RKH3\x01")
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(enc)))
	h = append(h, l[:]...)
	h = append(h, enc...)
	return append(h, bytes.Repeat([]byte{0xEE}, 64)...)
}

var genesisBody = body("rokh", "rokh.genesis", "the first page")
var genesisHead = head(1, nil, genesisBody, true)
var anchor = frame.Hash(genesisHead)

func newSide(t *testing.T, seed byte, scopes []string, withGenesis bool) (Side, *vessel.Memory) {
	t.Helper()
	m := vessel.NewMemory()
	vk := bytes.Repeat([]byte{seed}, 32)
	c, err := carrier.Create(m, vessel.Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(seed)}, vk, nil,
		anchor, frame.Zero, scopes, kSealer{k: sharedK, rnd: stream(seed + 100)})
	if err != nil {
		t.Fatal(err)
	}
	s := Side{C: c, Own: &holder{}, Unjudged: true}
	if withGenesis {
		r, _ := c.Begin(s.Own)
		if err := r.Event(anchor, genesisHead, genesisBody, "rokh"); err != nil {
			t.Fatal(err)
		}
		r.SetRef("main", anchor)
		if out, err := r.Commit(); err != nil || out != vessel.Recorded {
			t.Fatal(out, err)
		}
	}
	return s, m
}

func write(t *testing.T, s Side, author byte, addr, payload string) frame.ID {
	t.Helper()
	heads, _ := s.C.Heads()
	bd := body(addr, "note", payload)
	h := head(author, heads, bd, false)
	id := frame.Hash(h)
	r, _ := s.C.Begin(s.Own)
	if err := r.Event(id, h, bd, addr); err != nil {
		t.Fatal(err)
	}
	r.SetRef("main", id)
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	return id
}

// view lists what a side holds: whole events, head-only events, branches.
func view(t *testing.T, s Side) string {
	t.Helper()
	var whole, heads []string
	s.C.Events(func(e carrier.EventRecord) error {
		if carrier.IsHeadOnly(e.Ref) {
			heads = append(heads, e.ID.Short())
		} else {
			if _, err := s.C.Body(e); err != nil {
				t.Fatalf("a whole event does not open: %v", err)
			}
			whole = append(whole, e.ID.Short())
		}
		return nil
	}, nil)
	sort.Strings(whole)
	sort.Strings(heads)
	refs, _ := s.C.Refs()
	var tips []string
	for _, id := range refs {
		tips = append(tips, id.Short())
	}
	sort.Strings(tips)
	return fmt.Sprintf("whole=%v heads=%v", whole, heads)
}

// B1, B2: two seeds write apart; reconcile makes them equal, in either order,
// and repeating it changes nothing.
func TestTwoSeedsWriteApartAndBecomeEqual(t *testing.T) {
	for _, order := range []string{"a-then-b", "b-then-a"} {
		a, _ := newSide(t, 1, nil, true)
		b, _ := newSide(t, 2, nil, false)
		if res, err := Reconcile(a, b); err != nil || res.ExitCode() != 0 {
			t.Fatalf("first meeting: %v %s", err, res)
		}
		write(t, a, 1, "home/a", "one")
		write(t, a, 1, "home/a", "two")
		write(t, b, 2, "work/b", "three")
		var res Result
		var err error
		if order == "a-then-b" {
			res, err = Reconcile(a, b)
		} else {
			res, err = Reconcile(b, a)
		}
		if err != nil || !res.OK() || res.ExitCode() != 0 {
			t.Fatalf("%s: %v %s", order, err, res)
		}
		va, vb := view(t, a), view(t, b)
		if va != vb {
			t.Fatalf("%s: the seeds differ after reconcile:\n a %s\n b %s", order, va, vb)
		}
		ha, _ := a.C.Heads()
		hb, _ := b.C.Heads()
		if len(ha) < 2 || len(ha) != len(hb) {
			t.Fatalf("%s: both concurrent tips must be named on both sides: %d %d", order, len(ha), len(hb))
		}
		ga, gb := a.C.Vessel().Info().Generation, b.C.Vessel().Info().Generation
		res, err = Reconcile(a, b)
		if err != nil || !res.OK() || res.Local.Added != 0 || res.Remote.Added != 0 {
			t.Fatalf("%s: a repeated reconcile added something: %s", order, res)
		}
		if a.C.Vessel().Info().Generation != ga || b.C.Vessel().Info().Generation != gb {
			t.Fatalf("%s: a repeated reconcile wrote a generation", order)
		}
	}
}

// R1: two anchors never meet, and nothing is written.
func TestDifferentAnchorsNeverMeet(t *testing.T) {
	a, ma := newSide(t, 1, nil, true)
	m := vessel.NewMemory()
	other, err := carrier.Create(m, vessel.Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: stream(9)}, bytes.Repeat([]byte{9}, 32), nil,
		frame.Hash([]byte("another genesis")), frame.Zero, nil, kSealer{k: sharedK, rnd: stream(10)})
	if err != nil {
		t.Fatal(err)
	}
	before := ma.Writes()
	res, err := Reconcile(a, Side{C: other, Own: &holder{}, Unjudged: true})
	if Code(err) != "anchor_differs" || res.OK() || res.ExitCode() != 3 {
		t.Fatalf("different anchors: %v %s", err, res)
	}
	if ma.Writes() != before {
		t.Fatal("a refused reconcile wrote")
	}
}

// B5, R5: the second half failing is a failure that names both halves, never
// a success of the whole.
func TestTheSecondHalfFailingIsAFailure(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	b, _ := newSide(t, 2, nil, false)
	b.Own = &holder{no: true}
	res, err := Reconcile(a, b)
	if err == nil || res.OK() || res.ExitCode() != 3 {
		t.Fatalf("a failed second half was reported as %v %s", err, res)
	}
	if res.Local.Outcome != vessel.Recorded || res.Remote.Outcome != vessel.NotRecorded {
		t.Fatalf("the halves are not named: %s", res)
	}
}

// B6, S3 of 4.7: a scoped side receives its scope whole, the signed head of
// every ancestor of it, and nothing of what came after.
func TestAScopedSideHoldsItsScopeAndTheNecessaryLineage(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	before := write(t, a, 1, "home/diary", "an ancestor, out of scope")
	w := write(t, a, 1, "work/plan", "in scope")
	after := write(t, a, 1, "home/diary", "a descendant, out of scope")
	b, _ := newSide(t, 2, []string{"work"}, false)
	if res, err := Reconcile(a, b); err != nil || !res.OK() {
		t.Fatalf("%v %s", err, res)
	}
	got := map[frame.ID]string{}
	b.C.Events(func(e carrier.EventRecord) error {
		if carrier.IsHeadOnly(e.Ref) {
			got[e.ID] = "head"
		} else {
			got[e.ID] = "whole"
		}
		return nil
	}, nil)
	if got[anchor] != "whole" || got[w] != "whole" || got[before] != "head" || got[after] != "" {
		t.Fatalf("the scoped seed holds genesis %q, work %q, ancestor %q, descendant %q",
			got[anchor], got[w], got[before], got[after])
	}
	raw, err := b.C.Get(before)
	if err != nil || bytes.Contains(raw, []byte("out of scope")) {
		t.Fatalf("an out-of-scope body travelled: %v", err)
	}
	refs, _ := b.C.Refs()
	for name, id := range refs {
		if id != w {
			t.Fatalf("branch %q names %s, not the tip of what travelled", name, id.Short())
		}
	}
}

// bring records content and its content.put event in one commit.
func bring(t *testing.T, s Side, addr string, data []byte) frame.ID {
	t.Helper()
	r, _ := s.C.Begin(s.Own)
	id, err := r.Content(addr, uint64(len(data)), func() (io.Reader, error) { return bytes.NewReader(data), nil })
	if err != nil {
		t.Fatal(err)
	}
	var sz [8]byte
	binary.BigEndian.PutUint64(sz[:], uint64(len(data)))
	desc, _ := frame.EncodeFields(frame.Fields{{Tag: 1, Value: id[:]}, {Tag: 2, Value: sz[:]}, {Tag: 3, Value: []byte("text/plain")}})
	heads, _ := s.C.Heads()
	bd := body(addr, VerbContentPut, string(desc))
	h := head(1, heads, bd, false)
	eid := frame.Hash(h)
	if err := r.Event(eid, h, bd, addr); err != nil {
		t.Fatal(err)
	}
	r.SetRef("main", eid)
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	return id
}

// Content travels with the event that brings it, inside the scope only.
func TestContentTravelsOnlyInsideTheScope(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	in := bring(t, a, "work/files", bytes.Repeat([]byte("in scope "), 1000))
	out := bring(t, a, "home/files", bytes.Repeat([]byte("private "), 1000))
	b, _ := newSide(t, 2, []string{"work"}, false)
	if res, err := Reconcile(a, b); err != nil || !res.OK() {
		t.Fatalf("%v %s", err, res)
	}
	var got bytes.Buffer
	if _, err := b.C.ContentTo(in, &got); err != nil || !bytes.Contains(got.Bytes(), []byte("in scope")) {
		t.Fatalf("in-scope content did not travel: %v", err)
	}
	if _, err := b.C.ContentTo(out, io.Discard); err == nil {
		t.Fatal("out-of-scope content travelled to a scoped seed")
	}
	// A whole seed gets both.
	w, _ := newSide(t, 3, nil, false)
	if res, err := Reconcile(a, w); err != nil || !res.OK() {
		t.Fatalf("%v %s", err, res)
	}
	if _, err := w.C.ContentTo(out, io.Discard); err != nil {
		t.Fatalf("a whole seed lacks content: %v", err)
	}
}

// R6: a session transfers only what it can judge, and only
// envelopes that name the owner; what it holds back is counted in the answer.
func TestUnionTransfersOnlyJudgedRecordsThatNameTheOwner(t *testing.T) {
	a, _ := newSide(t, 1, nil, true)
	ok := write(t, a, 1, "work/a", "judged")
	pending := write(t, a, 1, "work/b", "not judged")
	b, _ := newSide(t, 2, nil, false)
	a.Judge = func(id frame.ID) string {
		if id == pending {
			return "pending"
		}
		return "full"
	}
	res, err := Reconcile(a, b)
	if err != nil || !res.OK() {
		t.Fatalf("%v %s", err, res)
	}
	got := map[frame.ID]bool{}
	b.C.Events(func(e carrier.EventRecord) error { got[e.ID] = true; return nil }, nil)
	if !got[ok] || got[pending] || res.Remote.Held == 0 {
		t.Fatalf("judged %v, pending %v, withheld %d", got[ok], got[pending], res.Remote.Held)
	}
	// An envelope that does not name the owner does not travel either.
	c, _ := newSide(t, 3, nil, false)
	a.Judge = nil
	a.Covers = func(frame.ID, []byte) bool { return false }
	res, err = Reconcile(a, c)
	if err != nil || !res.OK() {
		t.Fatalf("%v %s", err, res)
	}
	whole := 0
	c.C.Events(func(e carrier.EventRecord) error {
		if !carrier.IsHeadOnly(e.Ref) {
			whole++
		}
		return nil
	}, nil)
	if whole != 0 || res.Remote.Held == 0 {
		t.Fatalf("%d whole events travelled without the owner's entry; withheld %d", whole, res.Remote.Held)
	}
	// And the carrier refuses such an envelope at commit.
	c.C.SetCovers(func(frame.ID, []byte) bool { return false })
	r, _ := c.C.Begin(c.Own)
	e := carrier.EventRecord{}
	a.C.Events(func(x carrier.EventRecord) error {
		if x.ID == ok {
			e = x
		}
		return nil
	}, nil)
	env, _ := a.C.Vessel().Body(e.Ref)
	if err := r.EventEnvelope(e.ID, e.Head, env); err == nil {
		t.Fatal("an envelope without the owner was taken at commit")
	}
	r.Abandon()
}

// A side that gives no judge or no owner check is refused
// before anything moves; ancestry_unproven, not a union of everything.
func TestASideWithoutAJudgeIsRefused(t *testing.T) {
	a, ma := newSide(t, 1, nil, true)
	b, mb := newSide(t, 2, nil, false)
	wa, wb := ma.Writes(), mb.Writes()
	for name, s := range map[string]Side{
		"no judge":       {C: a.C, Own: a.Own, Covers: func(frame.ID, []byte) bool { return true }},
		"no owner check": {C: a.C, Own: a.Own, Judge: func(frame.ID) string { return "full" }},
	} {
		if _, err := Reconcile(s, b); Code(err) != "ancestry_unproven" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := Seed(Side{C: a.C, Own: a.Own}, SeedTarget{}, SeedPlan{}); Code(err) != "ancestry_unproven" {
		t.Fatalf("a seed without a judge: %v", err)
	}
	if ma.Writes() != wa || mb.Writes() != wb {
		t.Fatal("a refused union wrote")
	}
}
