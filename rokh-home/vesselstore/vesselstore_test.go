package vesselstore

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func newStore(t *testing.T) (*Store, *Memory) {
	t.Helper()
	m := NewMemory()
	s, err := New(m, bytes.Repeat([]byte{7}, 32), 8, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return s, m
}

func TestAnObjectRoundTripsInChunks(t *testing.T) {
	s, _ := newStore(t)
	body := []byte("a synthetic object of several chunks")
	if _, _, err := s.Put("blob", "one", bytes.NewReader(body), nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("blob", "one")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if ok, _ := s.Has("blob", "one"); !ok {
		t.Fatal("Has")
	}
	if _, err := s.Get("tree", "one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another kind under the same id: %v", err)
	}
}

// S3/C10: swapped, dropped, repeated-with-other-bytes and truncated chunks are
// refused before one byte is served.
func TestChunksBindTheirCoordinates(t *testing.T) {
	cases := map[string]func(map[string][]Chunk){
		"swapped": func(o map[string][]Chunk) {
			for _, cs := range o {
				cs[0].Envelope, cs[1].Envelope = cs[1].Envelope, cs[0].Envelope
			}
		},
		"dropped": func(o map[string][]Chunk) {
			for k, cs := range o {
				o[k] = cs[1:]
			}
		},
		"truncated": func(o map[string][]Chunk) {
			for _, cs := range o {
				cs[0].Envelope = cs[0].Envelope[:len(cs[0].Envelope)-1]
			}
		},
		"size changed": func(o map[string][]Chunk) {
			for _, cs := range o {
				for i := range cs {
					cs[i].Size++
				}
			}
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			s, m := newStore(t)
			if _, _, err := s.Put("blob", "x", bytes.NewReader([]byte("0123456789abcdefghij")), nil); err != nil {
				t.Fatal(err)
			}
			m.Tamper(spoil)
			if got, err := s.Get("blob", "x"); err == nil {
				t.Fatalf("%s: served %q", name, got)
			}
		})
	}
	t.Run("conflicting", func(t *testing.T) {
		s, m := newStore(t)
		s.Put("blob", "x", bytes.NewReader([]byte("0123456789abcdefghij")), nil)
		other, _ := newStore(t)
		other.rec = NewMemory()
		other.Put("blob", "x", bytes.NewReader([]byte("ZZZZZZZZ89abcdefghij")), nil)
		m.Tamper(func(o map[string][]Chunk) {
			for k := range o {
				theirs := other.rec.(*Memory)
				for _, cs := range theirs.objects {
					o[k] = append(o[k], cs[0])
				}
			}
		})
		if _, err := s.Get("blob", "x"); !errors.Is(err, ErrConflict) {
			t.Fatalf("two chunks in one place: %v", err)
		}
	})
}

func TestPointersAreSealedAndTheLatestWins(t *testing.T) {
	s, _ := newStore(t)
	if err := s.SetPointer("catalog", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPointer("catalog", []byte("second")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.Pointer("catalog")
	if err != nil || !ok || string(v) != "second" {
		t.Fatalf("pointer: %q %v %v", v, ok, err)
	}
	if _, ok, _ := s.Pointer("registry"); ok {
		t.Fatal("a pointer never set was found")
	}
}

func TestAnExistingObjectIsVerifiedNeverReplaced(t *testing.T) {
	s, _ := newStore(t)
	s.Put("blob", "x", bytes.NewReader([]byte("original")), nil)
	if _, _, err := s.Put("blob", "x", bytes.NewReader([]byte("different")), nil); !errors.Is(err, ErrMismatch) {
		t.Fatalf("an immutable object was replaced: %v", err)
	}
}
