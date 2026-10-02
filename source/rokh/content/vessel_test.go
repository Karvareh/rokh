package content

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"io"
	"math/rand/v2"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

// sealer is a suite-0x02 stand-in for the key layer.
type sealer struct{ k []byte }

var zero12 = make([]byte, 12)

func (s sealer) aead(salt []byte) cipher.AEAD {
	k, _ := hkdf.Key(sha256.New, s.k, salt, "rokh/env/1/shared", 32)
	b, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(b)
	return g
}

func aad(kind byte, id frame.ID, at []byte) []byte {
	return append(append(append([]byte("rokh/env/1"), kind), id[:]...), at...)
}

func (s sealer) SealAt(kind byte, id frame.ID, address string, at, plain []byte) ([]byte, error) {
	salt := bytes.Repeat([]byte{byte(len(plain))}, 32)
	return s.aead(salt).Seal(append([]byte{1, 2}, salt...), zero12, plain, aad(kind, id, at)), nil
}

func (s sealer) OpenAt(kind byte, id frame.ID, at, env []byte) ([]byte, error) {
	if len(env) < 34+16 {
		return nil, errors.New("short")
	}
	return s.aead(env[2:34]).Open(nil, zero12, env[34:], aad(kind, id, at))
}

func (s sealer) Seal(kind byte, id frame.ID, address string, plain []byte) ([]byte, error) {
	return s.SealAt(kind, id, address, nil, plain)
}

func (s sealer) Open(kind byte, id frame.ID, env []byte) ([]byte, error) {
	return s.OpenAt(kind, id, nil, env)
}

type own struct{}

func (own) Holds() error { return nil }

// A brought file lives inside the vessel and comes back whole; nothing is
// written outside the carrier's folder, and the folder shows no byte of it.
func TestBroughtContentLivesInsideTheVessel(t *testing.T) {
	m := vessel.NewMemory()
	var seed [32]byte
	c, err := carrier.Create(m, vessel.Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: rand.NewChaCha8(seed)},
		bytes.Repeat([]byte{3}, 32), nil, frame.Hash([]byte("g")), frame.Zero, nil, sealer{k: bytes.Repeat([]byte{8}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("a picture, say; "), 40000) // 640,000 bytes: three chunks
	r, err := c.Begin(own{})
	if err != nil {
		t.Fatal(err)
	}
	d, payload, err := BringBytes(r, "home/pictures", body, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := r.Commit(); err != nil || out != vessel.Recorded {
		t.Fatal(out, err)
	}
	back, err := Decode(payload)
	if err != nil || back != d || d.Hash != frame.Hash(body) || d.Size != uint64(len(body)) {
		t.Fatalf("descriptor: %+v %v", back, err)
	}
	var out bytes.Buffer
	if err := Fetch(c, d, &out); err != nil || !bytes.Equal(out.Bytes(), body) {
		t.Fatalf("fetch: %v", err)
	}
	for _, n := range m.FileNames() {
		if !vessel.ValidName(n) {
			t.Fatalf("%s lies outside the vessel", n)
		}
		if bytes.Contains(m.Get(n), []byte("a picture, say")) {
			t.Fatalf("the content is readable in %s", n)
		}
	}
	// A descriptor that lies about the size is refused.
	d.Size++
	if err := Fetch(c, d, io.Discard); err == nil {
		t.Fatal("a wrong size was served")
	}
}
