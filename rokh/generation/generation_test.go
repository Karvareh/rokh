package generation

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"rokh/frame"
)

// a verifier for a made-up generation, so the set can be tested with more
// than one in it.
type fake struct {
	name Name
	good []byte
}

func (f fake) Generation() Name { return f.name }

func (f fake) Verify(raw []byte) error {
	if !bytes.Equal(raw, f.good) {
		return errors.New("these are not the bytes I know")
	}
	return nil
}

func frameOf(gen Name, tail string) []byte {
	return append([]byte(gen), tail...)
}

// A signed byte string is never rewritten. Every change of format or cipher is
// a new generation with its own name and its own verifier, and bringing a new
// one in does not touch what is already written.
//
//	— T3.7
func TestANewGenerationDoesNotTouchTheOldBytes(t *testing.T) {
	old := frameOf("RKH1", "the first event")
	before := append([]byte(nil), old...)

	s := New()
	if err := s.Keep(fake{name: "RKH1", good: old}); err != nil {
		t.Fatal(err)
	}
	if _, st, err := s.Read(old); st != Sound || err != nil {
		t.Fatalf("the old bytes did not read: %s %v", st, err)
	}

	// A second generation arrives.
	fresh := frameOf("RKH2", "a later event")
	if err := s.Keep(fake{name: "RKH2", good: fresh}); err != nil {
		t.Fatal(err)
	}
	if _, st, err := s.Read(fresh); st != Sound || err != nil {
		t.Fatalf("the new generation did not read: %s %v", st, err)
	}
	// And the old ones are exactly as they were, and still read.
	if !bytes.Equal(old, before) {
		t.Fatal("bringing in a generation rewrote the past")
	}
	if _, st, err := s.Read(old); st != Sound || err != nil {
		t.Fatalf("the old generation stopped reading when a new one arrived: %s %v", st, err)
	}
	if k := s.Kept(); len(k) != 2 {
		t.Fatalf("both generations should be kept: %v", k)
	}
}

// An old generation is readable for exactly as long as its verifier is kept.
// Stop keeping it and its events become unread — which is not the same as
// invalid, and not the same as gone.
//
//	— T3.7, T12.3
func TestAnOldGenerationReadsWhileItsVerifierIsKept(t *testing.T) {
	old := frameOf("RKH1", "the first event")
	s := New()
	s.Keep(fake{name: "RKH1", good: old})

	if _, st, _ := s.Read(old); st != Sound {
		t.Fatalf("kept and sound, yet read as %s", st)
	}
	s.Forget("RKH1")

	n, st, err := s.Read(old)
	if st != NotKept || !errors.Is(err, ErrNotKept) {
		t.Fatalf("forgetting a verifier made the bytes %s (%v), not unread", st, err)
	}
	if n != "RKH1" {
		t.Fatalf("the generation of unread bytes is still readable: got %q", n)
	}
	if st == Unsound {
		t.Fatal("unread was reported as refuted")
	}
	// The bytes themselves are untouched: keeping the verifier again brings
	// them straight back.
	s.Keep(fake{name: "RKH1", good: old})
	if _, st, _ := s.Read(old); st != Sound {
		t.Fatal("the event did not come back when its verifier did")
	}
}

// If an old cipher is one day broken, what moves is the boundary of security —
// not history. The bytes are the same bytes, they carry the same name, and
// they still read. What changes is what may be claimed about them.
//
//	— T3.7
func TestABrokenCipherMovesSecurityNotHistory(t *testing.T) {
	old := frameOf("RKH1", "the first event")
	name := frame.Hash(old)
	s := New()
	s.Keep(fake{name: "RKH1", good: old})

	if err := s.Weaken("RKH1", "the signature scheme was broken in 2031"); err != nil {
		t.Fatal(err)
	}
	n, st, err := s.Read(old)
	if err != nil {
		t.Fatalf("a weakened generation stopped reading: %v", err)
	}
	if st != Weakened {
		t.Fatalf("the standing did not move to weakened: %s", st)
	}
	if n != "RKH1" {
		t.Fatal("the generation changed")
	}
	if frame.Hash(old) != name {
		t.Fatal("the event's name changed when its cipher was broken")
	}
	why, ok := s.Why("RKH1")
	if !ok || !strings.Contains(why, "broken") {
		t.Fatalf("a reader cannot say why the claim is smaller: %q", why)
	}
	// Weakening one generation says nothing about another.
	fresh := frameOf("RKH2", "a later event")
	s.Keep(fake{name: "RKH2", good: fresh})
	if _, st, _ := s.Read(fresh); st != Sound {
		t.Fatalf("weakening one generation weakened another: %s", st)
	}
}

// The generation is the first thing in a frame and is read without any key —
// which is what lets a reader say "I cannot read this" instead of "this is
// wrong".
//
//	— T3.7
func TestTheGenerationIsReadableBeforeAnythingElse(t *testing.T) {
	if got, err := Of(frameOf("RKH1", "anything at all")); err != nil || got != "RKH1" {
		t.Fatalf("the generation did not come off the front: %q %v", got, err)
	}
	if _, err := Of([]byte("RK")); !errors.Is(err, ErrNoGeneration) {
		t.Fatal("bytes too short to name a generation were accepted")
	}
	if _, err := Of([]byte{0x00, 0x01, 0x02, 0x03, 'x'}); !errors.Is(err, ErrNoGeneration) {
		t.Fatal("bytes that name no generation were accepted")
	}
	if Current != Name(frame.Magic) {
		t.Fatalf("the generation this build writes (%q) is not the frame's magic (%q)",
			Current, frame.Magic)
	}
}

// One generation has one verifier. Two would mean one name with two meanings,
// and then reading would have to choose.
//
//	— T3.7
func TestOneGenerationHasOneVerifier(t *testing.T) {
	s := New()
	if err := s.Keep(fake{name: "RKH1", good: []byte("a")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Keep(fake{name: "RKH1", good: []byte("b")}); !errors.Is(err, ErrAlreadyKept) {
		t.Fatalf("a second verifier was kept for one generation: %v", err)
	}
}

// The upgrade that rewrites the past is the one thing this package exists to
// prevent, so there is no function here that turns old bytes into new ones.
//
//	— T3.7
func TestThereIsNoFunctionThatRewritesOldBytes(t *testing.T) {
	src, err := os.ReadFile("generation.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, bad := range []string{
		"func Upgrade", "func Migrate", "func Rewrite", "func Reencode", "func Convert",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("the package offers %q; a ledger that rewrites its past to be "+
				"brought up to date is not a ledger", bad)
		}
	}
	// And Verify is handed bytes it can only read: it returns an error and
	// nothing else, so there is nowhere to put a change.
	var v Verifier = fake{name: "RKH1", good: []byte("x")}
	before := []byte("x")
	keep := append([]byte(nil), before...)
	_ = v.Verify(before)
	if !bytes.Equal(before, keep) {
		t.Fatal("verifying changed the bytes")
	}
}
