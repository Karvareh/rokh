package working

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"rokh/frame"
)

// countingRecorder is the far side of the boundary. It records how many times
// it was asked to cross, which is the only way to prove nothing crosses on
// its own.
type countingRecorder struct {
	calls int
	last  Draft
	err   error
}

func (r *countingRecorder) Record(d Draft) (frame.ID, error) {
	r.calls++
	r.last = d
	if r.err != nil {
		return frame.ID{}, r.err
	}
	return frame.Hash(append([]byte(d.Address+d.Verb), d.Payload...)), nil
}

// folder is a store on disk, defined here and shipped nowhere. A durable
// working state on a real carrier has to be sealed by the carrier, because
// Rokh writes nothing outside it. This one exists only to prove the claim of
// T4.5: that memory or disk makes no difference above the interface.
type folder struct{ root string }

func (f folder) path(n string) string { return filepath.Join(f.root, n+".draft") }

func (f folder) Put(name string, b []byte) error { return os.WriteFile(f.path(name), b, 0o600) }

func (f folder) Get(name string) ([]byte, bool, error) {
	b, err := os.ReadFile(f.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

func (f folder) Delete(name string) error { return os.Remove(f.path(name)) }

func (f folder) List() ([]string, error) {
	es, err := os.ReadDir(f.root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if n, ok := strings.CutSuffix(e.Name(), ".draft"); ok {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

func stores(t *testing.T) map[string]Store {
	t.Helper()
	return map[string]Store{
		"memory": NewMemory(),
		"disk":   folder{root: t.TempDir()},
	}
}

// The working state is not final. A draft may be revised as often as one
// likes and discarded without a trace, because only what crosses the boundary
// is permanent.
//
//	— T4.4, N4.7
func TestADraftIsRevisedFreelyAndDiscardedWithoutTrace(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s, err := Open(store)
			if err != nil {
				t.Fatal(err)
			}
			h, err := s.Write(Draft{Address: "home/journal", Verb: "note", Payload: []byte("first")})
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"second", "third", "fourth"} {
				if err := s.Revise(h, Draft{Address: "home/journal", Verb: "note", Payload: []byte(text)}); err != nil {
					t.Fatal(err)
				}
			}
			d, err := s.Read(h)
			if err != nil {
				t.Fatal(err)
			}
			if string(d.Payload) != "fourth" {
				t.Fatalf("the draft did not change: %q", d.Payload)
			}
			if err := s.Discard(h); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Read(h); !errors.Is(err, ErrNoSuchDraft) {
				t.Fatal("a discarded draft is still there")
			}
			hs, err := s.List()
			if err != nil || len(hs) != 0 {
				t.Fatalf("discarding left something behind: %v %v", hs, err)
			}
		})
	}
}

// The whole of the rule: nothing crosses the boundary by itself. Writing,
// revising, reading, listing, previewing and discarding are every operation
// this package has, and not one of them reaches the recorder.
//
//	— T4, T4.1, T4.4, T4.5, N4.7
func TestNothingCrossesTheBoundaryWithoutTheExplicitAct(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s, err := Open(store)
			if err != nil {
				t.Fatal(err)
			}
			rec := &countingRecorder{}

			h, _ := s.Write(Draft{Address: "home/a", Verb: "note", Payload: []byte("x")})
			g, _ := s.Write(Draft{Address: "home/b", Verb: "note", Payload: []byte("y")})
			s.Revise(h, Draft{Address: "home/a", Verb: "note", Payload: []byte("x2")})
			s.Read(h)
			s.List()
			s.Preview(h, []frame.ID{frame.Hash([]byte("parent"))}, "a grant")
			s.Preview(g, nil, "")
			s.Discard(g)
			// Reopening is not an act either.
			if _, err := Open(store); err != nil {
				t.Fatal(err)
			}
			if rec.calls != 0 {
				t.Fatalf("%d events were recorded without an explicit act", rec.calls)
			}

			// And now the one act that does cross.
			if _, err := s.Close(h, rec); err != nil {
				t.Fatal(err)
			}
			if rec.calls != 1 {
				t.Fatalf("closing recorded %d times, want exactly 1", rec.calls)
			}
			if string(rec.last.Payload) != "x2" {
				t.Fatalf("what crossed was not the latest draft: %q", rec.last.Payload)
			}
			if _, err := s.Read(h); !errors.Is(err, ErrNoSuchDraft) {
				t.Fatal("a closed draft is still a draft; it became an event")
			}
		})
	}
}

// Whether the working state sits in memory or on disk has nothing to do with
// the architecture. The same script through both stores must give the same
// answer, down to the handles.
//
//	— T4.5
func TestMemoryAndDiskAreIndistinguishableFromAbove(t *testing.T) {
	script := func(store Store) []string {
		s, err := Open(store)
		if err != nil {
			t.Fatal(err)
		}
		rec := &countingRecorder{}
		var out []string
		a, _ := s.Write(Draft{Address: "home/a", Verb: "note", Payload: []byte("one")})
		b, _ := s.Write(Draft{Address: "home/b", Verb: "note", Payload: []byte("two")})
		c, _ := s.Write(Draft{Address: "home/c", Verb: "note", Payload: []byte("three")})
		s.Revise(b, Draft{Address: "home/b", Verb: "note", Payload: []byte("two, revised")})
		s.Discard(a)
		f, _ := s.Preview(b, nil, "a grant")
		out = append(out, f.Address, f.Verb, string(rune('0'+f.PayloadBytes%10)))
		id, err := s.Close(b, rec)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, id.String())
		hs, _ := s.List()
		for _, h := range hs {
			d, _ := s.Read(h)
			out = append(out, string(h), d.Address, string(d.Payload))
		}
		_ = c
		return out
	}
	inMemory := script(NewMemory())
	onDisk := script(folder{root: t.TempDir()})
	if len(inMemory) != len(onDisk) {
		t.Fatalf("different shapes: %v vs %v", inMemory, onDisk)
	}
	for i := range inMemory {
		if inMemory[i] != onDisk[i] {
			t.Fatalf("step %d differs: memory %q, disk %q", i, inMemory[i], onDisk[i])
		}
	}
}

// What is shown before closing is a prediction of the effect, not the effect.
// The form therefore carries no name: a name is the hash of bytes that have
// not been made, and showing one would claim an event exists. Previewing also
// changes nothing at all.
//
//	— T9.2, T3.2
func TestThePreviewIsAPredictionAndCarriesNoName(t *testing.T) {
	s, err := Open(NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	h, _ := s.Write(Draft{Address: "home/journal", Verb: "note", Payload: []byte("a note")})
	parent := frame.Hash([]byte("head"))

	first, err := s.Preview(h, []frame.ID{parent}, "grant abcd")
	if err != nil {
		t.Fatal(err)
	}
	if first.Address != "home/journal" || first.PayloadBytes != 6 || len(first.Parents) != 1 {
		t.Fatalf("the form does not describe the draft: %+v", first)
	}
	// A hundred previews leave the working state exactly as it was.
	for i := 0; i < 100; i++ {
		if _, err := s.Preview(h, []frame.ID{parent}, "grant abcd"); err != nil {
			t.Fatal(err)
		}
	}
	hs, _ := s.List()
	if len(hs) != 1 {
		t.Fatalf("previewing changed the working state: %v", hs)
	}
	d, _ := s.Read(h)
	if string(d.Payload) != "a note" {
		t.Fatal("previewing changed the draft")
	}
	// The Form type has no field that could hold an event name.
	if got := formFieldNames(); contains(got, "ID") || contains(got, "Name") {
		t.Fatalf("the form carries a name: %v", got)
	}
}

// A crossing that fails leaves the working state untouched. The draft is
// still a draft, and may be closed again once whatever refused it is fixed.
//
//	— T4.4
func TestAFailedCrossingLeavesTheDraftWhereItWas(t *testing.T) {
	s, err := Open(NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	h, _ := s.Write(Draft{Address: "home/journal", Verb: "note", Payload: []byte("a note")})
	refused := &countingRecorder{err: errors.New("the ledger refused it")}
	if _, err := s.Close(h, refused); err == nil {
		t.Fatal("a refused recording reported success")
	}
	if _, err := s.Read(h); err != nil {
		t.Fatalf("a refused recording lost the draft: %v", err)
	}
	accepted := &countingRecorder{}
	if _, err := s.Close(h, accepted); err != nil {
		t.Fatalf("the draft could not be closed after the refusal: %v", err)
	}
}

// Opening raises the working state over whatever is already waiting. Nothing
// there is recorded by the act of opening, and nothing is lost.
//
//	— T8.2
func TestOpeningRaisesTheWorkingStateAndRecordsNothing(t *testing.T) {
	store := NewMemory()
	first, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := first.Write(Draft{Address: "home/journal", Verb: "note", Payload: []byte("unfinished")})

	rec := &countingRecorder{}
	again, err := Open(store)
	if err != nil {
		t.Fatal(err)
	}
	if rec.calls != 0 {
		t.Fatal("opening recorded something")
	}
	d, err := again.Read(h)
	if err != nil || string(d.Payload) != "unfinished" {
		t.Fatalf("the waiting draft did not survive reopening: %v %q", err, d.Payload)
	}
	// A new handle after reopening must not collide with the old one.
	h2, _ := again.Write(Draft{Address: "home/other", Verb: "note"})
	if h2 == h {
		t.Fatal("reopening reused a handle that is still in use")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
