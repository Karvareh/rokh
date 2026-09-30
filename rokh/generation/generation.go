// Package generation is how Rokh changes without rewriting its past.
//
// A signed byte string is never rewritten. So every change of format or of
// cipher is a new generation: it brings its own name and its own verifier,
// and upgrading does not touch what is already written. An old generation
// stays readable for exactly as long as its verifier is kept.
//
// And if an old cipher is one day broken, what moves is the boundary of
// security — not history. The bytes are the same bytes, they still carry the
// same name, and what changes is what may be claimed about them.
//
// A system that has to rewrite its past in order to be brought up to date is
// not a ledger.
//
//	— T3.7
package generation

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"rokh/frame"
)

// Name is a generation's own name, and it is the first bytes of every frame
// written under it. Reading it needs no key and no verifier.
type Name string

// Current is the generation this build writes.
const Current = Name(frame.Magic)

// Verifier is what one generation brings with it. Nothing else in this
// package knows how to check bytes; the generation does.
type Verifier interface {
	// Generation is the name this verifier answers for.
	Generation() Name
	// Verify reports whether these bytes are sound under this generation. It
	// must not modify them, and there is nowhere for it to put a change if
	// it tried: it is handed a copy and returns only an error.
	Verify(raw []byte) error
}

// Standing is what can be said about bytes right now. It is about the present
// claim, never about the past fact.
type Standing int

const (
	// Sound: the verifier is kept and the bytes check out.
	Sound Standing = iota
	// Weakened: the verifier is kept and the bytes check out, but this
	// generation's cipher is no longer trusted. The event still happened and
	// still has this name; what has moved is what may be claimed about it.
	//   — T3.7
	Weakened
	// NotKept: nobody here holds a verifier for this generation. The bytes
	// are not wrong — they are unread.
	NotKept
	// Unsound: the verifier is kept and the bytes do not check out.
	Unsound
)

func (s Standing) String() string {
	switch s {
	case Sound:
		return "sound"
	case Weakened:
		return "weakened"
	case NotKept:
		return "not kept"
	}
	return "unsound"
}

var (
	// ErrNotKept means no verifier for that generation is held. It is
	// deliberately not the same as "invalid": an unread event is not a
	// refuted one.
	//
	//	— T3.7, T12.3
	ErrNotKept = errors.New("generation: no verifier is kept for it")
	// ErrNoGeneration means the bytes do not begin with a generation name.
	ErrNoGeneration = errors.New("generation: the bytes name no generation")
	// ErrAlreadyKept means a second verifier was offered for one generation.
	ErrAlreadyKept = errors.New("generation: a generation has one verifier")
)

type kept struct {
	v        Verifier
	weakened bool
	why      string
}

// Set is the generations this reader keeps. Keeping one is what makes its
// events readable; forgetting one makes them unread, and neither act touches
// a byte of what is stored.
type Set struct{ by map[Name]*kept }

// New returns a set that keeps nothing.
func New() *Set { return &Set{by: map[Name]*kept{}} }

// Keep adds a generation's verifier.
func (s *Set) Keep(v Verifier) error {
	n := v.Generation()
	if strings.TrimSpace(string(n)) == "" {
		return fmt.Errorf("%w: a verifier with no generation", ErrNoGeneration)
	}
	if _, ok := s.by[n]; ok {
		return fmt.Errorf("%w: %s", ErrAlreadyKept, n)
	}
	s.by[n] = &kept{v: v}
	return nil
}

// Forget stops keeping a verifier. What was written under that generation
// becomes unread — not invalid, and not gone.
//
//	— T3.7
func (s *Set) Forget(n Name) { delete(s.by, n) }

// Weaken records that a generation's cipher is no longer trusted. Reading
// still works, because history did not change; what changed is the boundary
// of security, and Standing says so from then on.
//
//	— T3.7
func (s *Set) Weaken(n Name, why string) error {
	k, ok := s.by[n]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotKept, n)
	}
	k.weakened, k.why = true, why
	return nil
}

// Kept names every generation this set can read, in a stable order.
func (s *Set) Kept() []Name {
	out := make([]Name, 0, len(s.by))
	for n := range s.by {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Of reads which generation these bytes were written under. It is the first
// thing in a frame, before anything that needs a key.
func Of(raw []byte) (Name, error) {
	if len(raw) < len(frame.Magic) {
		return "", ErrNoGeneration
	}
	n := Name(raw[:len(frame.Magic)])
	for _, r := range n {
		if r < 0x20 || r > 0x7e {
			return "", ErrNoGeneration
		}
	}
	return n, nil
}

// Read reports how these bytes stand, and why. It never writes: there is no
// path in this package that produces new bytes from old ones, because an
// upgrade that rewrites the past is the one thing a generation exists to
// avoid.
//
//	— T3.7
func (s *Set) Read(raw []byte) (Name, Standing, error) {
	n, err := Of(raw)
	if err != nil {
		return "", Unsound, err
	}
	k, ok := s.by[n]
	if !ok {
		return n, NotKept, fmt.Errorf("%w: %s", ErrNotKept, n)
	}
	if err := k.v.Verify(raw); err != nil {
		return n, Unsound, err
	}
	if k.weakened {
		return n, Weakened, nil
	}
	return n, Sound, nil
}

// Why explains a weakening, for a reader that has to say why a claim is
// smaller than it used to be.
func (s *Set) Why(n Name) (string, bool) {
	k, ok := s.by[n]
	if !ok || !k.weakened {
		return "", false
	}
	return k.why, true
}

// Profile is what the list of algorithms is, and is not.
//
// The chosen primitives are a base profile, not an axiom. None of them is our
// invention and none should be: hand-rolled cryptography is broken
// cryptography, and what is built here is the rule, not the cipher.
//
// A signed byte string is never rewritten, so a change to the profile is a new
// generation rather than an edit: it brings its own name and its own verifier,
// and upgrading does not touch the past. An old generation stays readable for
// as long as its verifier is kept. And if an old cipher breaks, what moves is
// the security boundary, not the history — the events are still exactly the
// bytes that were written, still saying who wrote them; what changed is how
// much that signature is worth from here on.
//
//	— N4.10, T1.1
const Profile = "a base profile, not an axiom; a change is a generation, not an edit"
