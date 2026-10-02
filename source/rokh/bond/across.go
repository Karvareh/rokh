package bond

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"rokh/canon"
	"rokh/frame"
)

// A bond spans several ledgers, and it spans them without joining them.
//
// The shape is the whole content of the ruling. There is one leaf, and its name
// is the hash of its own bytes, so everyone can arrive at the same name without
// asking anyone. Then each person writes, in *their own* ledger, that they
// accept that name and their own place in it. Nothing is written into anyone
// else's ledger, nothing is written into a ledger of the bond, because there is
// no ledger of the bond. The ledgers never become one.
//
// What that proves when it is complete is mutual acceptance: each of these
// people, on their own authority, in their own record, said this. It proves
// nothing about anything outside those ledgers — not ownership, not an
// exclusive right, not that the work was done. Reading it as more than mutual
// acceptance is the mistake this whole band exists to prevent.
//
//	— T13.6, T11.6, T11.7

// VerbAccept is the verb of an acceptance. It lives in the bond's own space,
// not the core's: the ledger does not know what a bond is.
//
//	— T11.6, T11.3
const VerbAccept = "bond.accept"

// TypeAccept is the declared type of an acceptance payload.
//
//	— T10.6
const TypeAccept = "rokh.bond.accept.v1"

// Acceptance is what one person records in their own ledger.
//
// It names the leaf, and it names the place that person takes in it. Both are
// needed: accepting a leaf without saying which part is yours is agreement
// without an undertaking, and the leaf is a statement of who does what.
//
//	— T11.6, T11.7
type Acceptance struct {
	Type string `json:"type"`
	// Leaf is the name of the leaf being accepted — the hash of its bytes,
	// which everyone computes for themselves.
	Leaf frame.ID `json:"leaf"`
	// Place is what this person takes on in it, in their own words.
	Place string `json:"place"`
}

var (
	// ErrNoPlace is returned for an acceptance that names no place.
	ErrNoPlace = errors.New("bond: accepting a leaf means accepting a place in it")
	// ErrNotAnAcceptance is returned for a payload that is not one.
	ErrNotAnAcceptance = errors.New("bond: not an acceptance")
	// ErrNotAFounder is returned when someone accepts a leaf they are not in.
	ErrNotAFounder = errors.New("bond: that leaf does not name this anchor")
)

// Accept builds the payload one person writes in their own ledger. It does not
// write it: this package holds no key and no carrier, and whose ledger it goes
// into is not a thing anyone else decides.
//
//	— T11.6, T4.1
func Accept(l Leaf, mine frame.ID, place string) ([]byte, frame.ID, error) {
	b, err := l.Encode()
	if err != nil {
		return nil, frame.ID{}, err
	}
	name := Name(b)
	if strings.TrimSpace(place) == "" {
		return nil, name, ErrNoPlace
	}
	var found bool
	for _, f := range l.Founders {
		if f == mine {
			found = true
			break
		}
	}
	if !found {
		return nil, name, fmt.Errorf("%w: %s", ErrNotAFounder, mine.Short())
	}
	p, err := canon.Marshal(Acceptance{Type: TypeAccept, Leaf: name, Place: place})
	return p, name, err
}

// ReadAcceptance parses an acceptance payload, refusing anything else.
//
//	— T10.6
func ReadAcceptance(payload []byte) (Acceptance, error) {
	var a Acceptance
	// The one encoding, and only that. Without it {"TYPE":…} would read as
	// this artefact too, and two byte strings would be one acceptance under
	// two names.
	//   — N4.1
	if err := canon.Strict(payload, &a); err != nil {
		return a, fmt.Errorf("%w: %v", ErrNotAnAcceptance, err)
	}
	if a.Type != TypeAccept {
		return a, fmt.Errorf("%w: type %q", ErrNotAnAcceptance, a.Type)
	}
	if a.Leaf.IsZero() {
		return a, fmt.Errorf("%w: it names no leaf", ErrNotAnAcceptance)
	}
	if strings.TrimSpace(a.Place) == "" {
		return a, ErrNoPlace
	}
	return a, nil
}

// Ledgers is the several ledgers a bond spans, each read on its own.
//
// Note what is not here: no way to write into a ledger, and no way to combine
// two. Reading is all this needs and all it is given, because the only thing
// being established is what each person, separately, has said in their own
// record.
//
//	— T13.6, T11.6
type Ledgers interface {
	// Accepted reports what the ledger anchored here says about this leaf.
	// Whether the ledger is reachable at all is the caller's problem: a
	// ledger nobody can read is simply one that has not been seen to accept.
	Accepted(anchor, leaf frame.ID) (Acceptance, bool)
}

// Settled reports whether a leaf is closed across the ledgers that hold it:
// everyone the leaf names has referred to that same name in their own ledger.
//
// The word for what this establishes is "mutual acceptance". It is not a
// verdict, not a title, and not evidence about the world; it is the fact that
// these people each said this, separately, where each of them keeps their own
// record. That is a real and useful fact and it is the only one available.
//
//	— T13.6, T11.6, T11.7
func Settled(l Leaf, in Ledgers) (missing []frame.ID, closed bool, err error) {
	b, err := l.Encode()
	if err != nil {
		return nil, false, err
	}
	name := Name(b)
	var accepted []frame.ID
	for _, f := range l.Founders {
		a, ok := in.Accepted(f, name)
		if !ok {
			continue
		}
		// A ledger that accepted some *other* leaf has not accepted this one.
		// The name is the hash of the bytes, so there is no near miss here:
		// either it is that leaf or it is a different leaf.
		if a.Leaf != name || strings.TrimSpace(a.Place) == "" {
			continue
		}
		accepted = append(accepted, f)
	}
	missing, closed = Standing(l, accepted)
	return missing, closed, nil
}

// Places reports what each person said their place was, so that "everyone
// agreed" can be read as the several separate things they each actually said.
//
//	— T11.7
func Places(l Leaf, in Ledgers) (map[frame.ID]string, error) {
	b, err := l.Encode()
	if err != nil {
		return nil, err
	}
	name := Name(b)
	out := map[frame.ID]string{}
	fs := append([]frame.ID(nil), l.Founders...)
	sort.Slice(fs, func(i, j int) bool { return fs[i].Compare(fs[j]) < 0 })
	for _, f := range fs {
		if a, ok := in.Accepted(f, name); ok && a.Leaf == name {
			out[f] = a.Place
		}
	}
	return out, nil
}
