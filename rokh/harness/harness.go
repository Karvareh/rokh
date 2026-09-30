// Package harness is the covenant every harness binds with Rokh.
//
// The core weighs bytes, signature and authority. It does not know what a verb
// means, and it does not guess. Meaning comes from the harness, and a harness
// that wants Rokh to carry its meaning has to say four things first: the space
// its names live in, which version of itself is speaking, everything it can
// do, and what it does with a verb it has never heard.
//
//	— T11.10, T11.3
package harness

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"rokh/event"
	"rokh/receipt"
)

// Answer is what a harness does with a verb inside its own space that it does
// not know. There are two honest answers and no third: guessing is not among
// them, because a guessed meaning is a meaning nobody granted.
//
//	— T11.10, T12.3
type Answer string

const (
	// Refuse: say plainly that it does not know this verb.
	Refuse Answer = "refuse"
	// Ignore: let the event pass untouched, claiming nothing about it.
	Ignore Answer = "ignore"
)

func (a Answer) valid() bool { return a == Refuse || a == Ignore }

// Covenant is the four clauses, and all four are required. A harness that
// declares three of them has not bound anything.
//
//	— T11.10
type Covenant struct {
	// Namespace is the harness's own space for addresses and verbs. Its
	// events live under it and its verbs are prefixed by it.
	Namespace string
	// Version says which version of the harness is speaking. Meaning changes
	// between versions, and a reader has to be able to tell which one wrote.
	Version string
	// Can is everything the harness can do, exhaustively. A verb absent from
	// this list is a verb the harness does not have, and saying so is the
	// point of the list.
	Can []string
	// Unknown is what happens to a verb inside the namespace that is not in
	// Can.
	Unknown Answer
}

var (
	// ErrIncomplete is returned when a clause is missing.
	ErrIncomplete = errors.New("harness: the covenant has four clauses and all are required")
	// ErrReserved is returned when a harness reaches into the core's space.
	ErrReserved = errors.New("harness: that space belongs to the core")
	// ErrOutsideNamespace is returned for a verb that is not the harness's to
	// declare.
	ErrOutsideNamespace = errors.New("harness: a verb must live in the harness's own space")
	// ErrNamespaceTaken is returned when two harnesses claim one space.
	ErrNamespaceTaken = errors.New("harness: two harnesses cannot share one space")
	// ErrRitualWord is returned when a covenant declares "intent" or
	// "outcome" as a verb of its own. They are the receipt ritual's words,
	// written at the harness's root from outside, and no covenant owns them.
	ErrRitualWord = errors.New("harness: intent and outcome are the ritual's words, not the covenant's")
)

// Bind checks a covenant and returns it ready to use. It refuses rather than
// repairs: a covenant with a hole in it is not a covenant with a default.
//
// Two words are never a harness's to declare: intent and outcome. They are
// the receipt ritual's, written at the harness's own root from outside, and
// a covenant that lists them as verbs of its own is refused — otherwise the
// same word would mean two things at one address.
//
//	— T11.10, T11.11
func (c Covenant) Bind() (Covenant, error) {
	if strings.TrimSpace(c.Namespace) == "" {
		return c, fmt.Errorf("%w: no namespace", ErrIncomplete)
	}
	if strings.TrimSpace(c.Version) == "" {
		return c, fmt.Errorf("%w: no version", ErrIncomplete)
	}
	if len(c.Can) == 0 {
		return c, fmt.Errorf("%w: no list of what it can do", ErrIncomplete)
	}
	if !c.Unknown.valid() {
		// The two answers are named. A refusal that checks against a closed
		// set and withholds the set leaves the caller guessing at a word that
		// exists only in this file, which is how a covenant with no default
		// becomes a covenant nobody outside can bind.
		//   — T11.10
		return c, fmt.Errorf("%w: no answer for an unknown verb (%q is not one; %s or %s)",
			ErrIncomplete, c.Unknown, Refuse, Ignore)
	}
	// The core's address and verb space is not on offer.
	if c.Namespace == event.AddressRoot ||
		strings.HasPrefix(c.Namespace, event.AddressRoot+"/") ||
		event.IsReserved(c.Namespace+".") {
		return c, fmt.Errorf("%w: %q", ErrReserved, c.Namespace)
	}
	seen := map[string]bool{}
	out := c
	out.Can = append([]string(nil), c.Can...)
	for _, v := range out.Can {
		if !strings.HasPrefix(v, c.Namespace+".") {
			// The separator is spelled out. A verb is joined to its namespace
			// with a full stop while an address is joined with a slash, so a
			// reader who has only ever seen Rokh's addresses obeys this
			// sentence literally, writes the slash again, and is told the same
			// thing a second time with nothing new in it.
			//   — T11.10
			return c, fmt.Errorf("%w: %q is not under %q; a verb is the namespace, a full stop, then the rest, as in %q",
				ErrOutsideNamespace, v, c.Namespace, c.Namespace+".{verb}")
		}
		if event.IsReserved(v) {
			return c, fmt.Errorf("%w: %q", ErrReserved, v)
		}
		if rest := strings.TrimPrefix(v, c.Namespace+"."); rest == receipt.VerbIntent || rest == receipt.VerbOutcome {
			return c, fmt.Errorf("%w: %q", ErrRitualWord, v)
		}
		if seen[v] {
			return c, fmt.Errorf("%w: %q twice", ErrIncomplete, v)
		}
		seen[v] = true
	}
	sort.Strings(out.Can)
	return out, nil
}

// Speaks reports whether an event is in this harness's space at all. Address
// and verb decide it; the name never does, because the name is a hash and
// says nothing about what is inside.
//
//	— T10.6, T3.2
func (c Covenant) Speaks(e event.Event) bool {
	return e.Address == c.Namespace || strings.HasPrefix(e.Address, c.Namespace+"/")
}

// AtRoot reports whether an event stands at the harness's own root — its
// exact namespace — rather than in the subject space beneath it. The two are
// two places: the root is the harness itself and the shelf its receipts sit
// on; everything under it is where its verbs act.
//
//	— T11.11
func (c Covenant) AtRoot(e event.Event) bool { return e.Address == c.Namespace }

// Reading is what a harness concludes about one event.
type Reading int

const (
	// NotMine: outside this harness's space entirely.
	NotMine Reading = iota
	// Known: a verb it declared and can act on.
	Known
	// Refused: inside its space, but a verb it does not know, and its
	// covenant says to refuse.
	Refused
	// Ignored: inside its space, unknown, and its covenant says to ignore.
	Ignored
	// Shelf: at the harness's own root, one of the two words of the receipt
	// ritual. Not a verb of the covenant and never in Can — the ritual writes
	// it there from outside, and the covenant has no say over it.
	//   — T11.11
	Shelf
)

func (r Reading) String() string {
	switch r {
	case Known:
		return "known"
	case Refused:
		return "refused"
	case Ignored:
		return "ignored"
	case Shelf:
		return "shelf"
	}
	return "not mine"
}

// Read applies the covenant to one event. An unknown verb never becomes a
// known one: the answer is whichever of the two the harness declared, and
// there is no path here that guesses a meaning.
//
// The harness's space is two places. At its exact root the two words of the
// receipt ritual are admitted — that is the shelf — and nothing else is: the
// root is the harness itself, and its verbs act beneath it. Under the root
// the covenant governs as it always did, and a receipt is refused there
// whatever the covenant says about unknown verbs, because a receipt in the
// verbs' space is misplaced, not unknown.
//
//	— T11.10, T11.11, T12.3
func (c Covenant) Read(e event.Event) Reading {
	if !c.Speaks(e) {
		return NotMine
	}
	ritual := e.Verb == receipt.VerbIntent || e.Verb == receipt.VerbOutcome
	if c.AtRoot(e) {
		if ritual {
			return Shelf
		}
		return Refused
	}
	if ritual {
		return Refused
	}
	for _, v := range c.Can {
		if v == e.Verb {
			return Known
		}
	}
	if c.Unknown == Ignore {
		return Ignored
	}
	return Refused
}

// Bound is a harness as it stands on one ledger: what it means, and what its
// effects do. Both are required to bind, because they answer different
// questions and neither substitutes for the other — the covenant says what a
// verb means, the effects say what happens somewhere else when it is acted on.
//
//	— T11.10, T10.7
type Bound struct {
	Covenant
	Effects Effects
}

// Register holds the harnesses bound on one ledger and keeps their spaces
// apart, so that no two of them can claim one name.
type Register struct{ by map[string]Bound }

// NewRegister returns an empty register.
func NewRegister() *Register { return &Register{by: map[string]Bound{}} }

// Bind adds a harness: its covenant and its effect declarations, together.
// Two harnesses cannot share a space, because then a verb would have two
// meanings and the ledger would have to choose — which is exactly what it must
// never do.
//
//	— T11.10, T12, T10.7
func (r *Register) Bind(c Covenant, e Effects) error {
	cov, err := c.Bind()
	if err != nil {
		return err
	}
	eff, err := e.Declare()
	if err != nil {
		return err
	}
	for ns := range r.by {
		if ns == cov.Namespace ||
			strings.HasPrefix(cov.Namespace, ns+"/") ||
			strings.HasPrefix(ns, cov.Namespace+"/") {
			return fmt.Errorf("%w: %q and %q", ErrNamespaceTaken, ns, cov.Namespace)
		}
	}
	r.by[cov.Namespace] = Bound{Covenant: cov, Effects: eff}
	return nil
}

// For returns the harness whose space an event falls in, if any.
func (r *Register) For(e event.Event) (Bound, bool) {
	for _, b := range r.by {
		if b.Speaks(e) {
			return b, true
		}
	}
	return Bound{}, false
}

// All is every harness bound here, in a stable order.
func (r *Register) All() []Bound {
	out := make([]Bound, 0, len(r.by))
	for _, b := range r.by {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace < out[j].Namespace })
	return out
}
