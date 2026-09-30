package medium

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/frame"
	"rokh/lineage"
	"rokh/turn"
	"rokh/vessel"
)

// sharedSealer stands in for the key layer: suite 0x02 under one key both
// seeds hold.
type sharedSealer struct{ k []byte }

func (s sharedSealer) aead(salt []byte) cipher.AEAD {
	k, _ := hkdf.Key(sha256.New, s.k, salt, "rokh/env/1/shared", 32)
	b, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(b)
	return g
}

func envAAD(kind byte, id frame.ID, at []byte) []byte {
	return append(append(append([]byte("rokh/env/1"), kind), id[:]...), at...)
}

func (s sharedSealer) SealAt(kind byte, id frame.ID, address string, at, plain []byte) ([]byte, error) {
	salt := make([]byte, 32)
	rand.Read(salt)
	return s.aead(salt).Seal(append([]byte{1, 2}, salt...), make([]byte, 12), plain, envAAD(kind, id, at)), nil
}

func (s sharedSealer) OpenAt(kind byte, id frame.ID, at, env []byte) ([]byte, error) {
	if len(env) < 50 {
		return nil, errors.New("short")
	}
	return s.aead(env[2:34]).Open(nil, make([]byte, 12), env[34:], envAAD(kind, id, at))
}

func (s sharedSealer) Seal(kind byte, id frame.ID, address string, plain []byte) ([]byte, error) {
	return s.SealAt(kind, id, address, nil, plain)
}

func (s sharedSealer) Open(kind byte, id frame.ID, env []byte) ([]byte, error) {
	return s.OpenAt(kind, id, nil, env)
}

func rkh3(parents []frame.ID, addr, verb, payload string, system bool) (frame.ID, []byte, []byte) {
	body, _ := frame.EncodeFields(frame.Fields{
		{Tag: 5, Value: []byte(addr)}, {Tag: 6, Value: []byte(verb)},
		{Tag: 7, Value: []byte(payload)}, {Tag: 0x0C, Value: bytes.Repeat([]byte{9}, 32)},
	})
	sum := sha256.Sum256(body)
	fs := frame.Fields{{Tag: 2, Value: bytes.Repeat([]byte{1}, 32)}, {Tag: 9, Value: sum[:4]}, {Tag: 0x0A, Value: sum[:]}}
	if len(parents) > 0 {
		var ps []byte
		for _, p := range parents {
			ps = append(ps, p[:]...)
		}
		fs = append(fs, frame.Field{Tag: 4, Value: ps})
	}
	if system {
		fs = append(fs, frame.Field{Tag: 0x0B, Value: []byte{1}})
	}
	enc, _ := frame.EncodeFields(fs)
	h := []byte("RKH3\x01")
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(enc)))
	h = append(append(append(h, l[:]...), enc...), bytes.Repeat([]byte{0xEE}, 64)...)
	return frame.Hash(h), h, body
}

func writeOne(t *testing.T, c *carrier.Carrier, own vessel.Owner, addr, text string) frame.ID {
	t.Helper()
	heads, _ := c.Heads()
	id, h, b := rkh3(heads, addr, "note", text, false)
	r, err := c.Begin(own)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Event(id, h, b, addr); err != nil {
		t.Fatal(err)
	}
	r.SetRef("main", id)
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatalf("%s %v", out, err)
	}
	return id
}

// Seed and reconcile between two real folders, each under its own kernel lock:
// both hold the same events afterwards, and each folder holds nothing but its
// vessel.
func TestSeedAndReconcileBetweenRealFolders(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	os.MkdirAll(a, 0o700)
	os.MkdirAll(b, 0o700)
	sealer := sharedSealer{k: bytes.Repeat([]byte{6}, 32)}
	gid, gh, gb := rkh3(nil, "rokh", "rokh.genesis", "first page", true)
	vkA := bytes.Repeat([]byte{1}, 32)
	src, err := carrier.Create(Dir{Root: a}, vessel.Params{SlabLog2: 18, Slabs: 32, Iter: 1, Rand: rand.Reader}, vkA, nil, gid, frame.Zero, nil, sealer)
	if err != nil {
		t.Fatal(err)
	}
	lockA, err := turn.Acquire(a, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lockA.Release()
	r, _ := src.Begin(lockA)
	r.Event(gid, gh, gb, "rokh")
	r.SetRef("main", gid)
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	writeOne(t, src, lockA, "home/a", "written on the root")
	var lockB *turn.Lock
	plan := lineage.SeedPlan{
		Seed: frame.Hash([]byte("seed b")),
		Give: func() []lineage.Event {
			heads, _ := src.Heads()
			id, h, bd := rkh3(heads, "rokh", "rokh.seed", "give", true)
			return []lineage.Event{{ID: id, Head: h, Body: bd, Address: "rokh"}}
		}(),
		Take: func(v frame.ID) (lineage.Event, error) {
			id, h, bd := rkh3([]frame.ID{gid}, "rokh", "rokh.seed", "take", true)
			return lineage.Event{ID: id, Head: h, Body: bd, Address: "rokh"}, nil
		},
	}
	res, err := lineage.Seed(lineage.Side{C: src, Own: lockA, Unjudged: true}, lineage.SeedTarget{
		M: Dir{Root: b}, Params: vessel.Params{SlabLog2: 18, Slabs: 32, Rand: rand.Reader},
		VK: bytes.Repeat([]byte{2}, 32), Sealer: sealer,
		Owner: func() (vessel.Owner, error) {
			l, err := turn.Acquire(b, time.Second)
			lockB = l
			return l, err
		},
	}, plan)
	if err != nil || !res.OK() {
		t.Fatalf("seed: %v %+v", err, res)
	}
	defer lockB.Release()
	dst, _, err := carrier.Open(Dir{Root: b}, func([][]byte, []byte, int) ([]byte, error) { return bytes.Repeat([]byte{2}, 32), nil }, rand.Reader, sealer)
	if err != nil {
		t.Fatal(err)
	}
	writeOne(t, src, lockA, "home/a", "later on the root")
	writeOne(t, dst, lockB, "work/b", "on the seed")
	out, err := lineage.Reconcile(lineage.Side{C: src, Own: lockA, Unjudged: true}, lineage.Side{C: dst, Own: lockB, Unjudged: true})
	if err != nil || !out.OK() {
		t.Fatalf("reconcile: %v %s", err, out)
	}
	count := func(c *carrier.Carrier) (n int) {
		c.Events(func(e carrier.EventRecord) error { n++; return nil }, nil)
		return
	}
	if count(src) != count(dst) || count(src) < 6 {
		t.Fatalf("after reconcile: %d events on the root, %d on the seed", count(src), count(dst))
	}
	for _, dir := range []string{a, b} {
		es, _ := os.ReadDir(dir)
		if len(es) != 1 || es[0].Name() != "rokh" {
			t.Fatalf("%s holds more than its vessel: %v", dir, es)
		}
	}
}
