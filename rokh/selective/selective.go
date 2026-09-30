// Package selective is disclosure that does not have to go all at once.
//
// What is wanted is this: one field is shown on its own, and the receiver
// checks the signature without seeing the rest. That much is settled — the
// least that is needed to check goes, and nothing more.
//
// What is not settled is the byte form: the name, type and place of a field,
// the ordering, repetition, the link back to the event itself, and what a
// receiver needs besides the field. Those are an open ruling, and this
// package is deliberately built so as not to decide them: the commitment
// scheme is an interface, not a choice made here.
//
//	— T7.5, T13.1
package selective

import (
	"errors"
	"fmt"
	"sort"
)

var (
	// ErrShowsMore is returned for a showing that carries a field nobody
	// asked for. More than the least is not generosity; it is disclosure
	// nobody authorised.
	//
	//	— T7.5, T7.3
	ErrShowsMore = errors.New("selective: shows more than was asked for")
	// ErrShowsLess is returned for a showing that leaves out something the
	// receiver needs to check what it was shown.
	ErrShowsLess = errors.New("selective: shows less than checking needs")
	// ErrNotCommitted is returned when a withheld field travels as anything
	// other than a commitment.
	ErrNotCommitted = errors.New("selective: a withheld field must travel as a commitment only")
	// ErrUnknownField is returned when a request names a field the event
	// does not have.
	ErrUnknownField = errors.New("selective: no such field")
)

// Commit is the byte-level question this package refuses to answer. Whatever
// scheme is chosen — a salted hash per field, a signature scheme that admits
// selective opening, something else — it plugs in here, and the rule above
// does not change with it.
//
//	— T7.5, T13.1
type Commit interface {
	// Commit produces the opaque stand-in for a withheld field.
	Commit(field string, value []byte) ([]byte, error)
	// Opens reports whether a shown value is the one this commitment stands
	// for, which is what lets a receiver check what it was given without
	// being given the rest.
	Opens(field string, value, commitment []byte) bool
}

// Whole is an event as its owner holds it: named fields, all of them.
type Whole struct {
	Fields map[string][]byte
	// Binding is whatever ties the fields to the event itself — a signature
	// over the commitments, typically. Its form is part of the open ruling,
	// so it travels as opaque bytes.
	Binding []byte
}

// Showing is what actually travels.
type Showing struct {
	// Shown carries the fields asked for, in the clear.
	Shown map[string][]byte
	// Commitments carries one commitment for every field the event has — for
	// the shown ones so the receiver can check that what it was handed is
	// really the event's, and for the rest because a withheld field travels
	// as a commitment and nothing else.
	//
	// Both are needed and for different reasons. Without a commitment over
	// the shown field the receiver has a value and no reason to believe it;
	// without commitments over the withheld ones the binding covers less than
	// the event and the owner could quietly drop a field.
	//
	//	— T7.5
	Commitments map[string][]byte
	// Binding travels too: without it the receiver has fields but no reason
	// to believe they came from the event.
	Binding []byte
}

// Least builds the showing for exactly the fields asked for: those in the
// clear, every other one only as a commitment, and the binding. It is not
// possible to ask this function for more, which is the point of it existing.
//
//	— T7.5
func Least(w Whole, c Commit, want ...string) (Showing, error) {
	asked := map[string]bool{}
	for _, f := range want {
		if _, ok := w.Fields[f]; !ok {
			return Showing{}, fmt.Errorf("%w: %q", ErrUnknownField, f)
		}
		asked[f] = true
	}
	out := Showing{
		Shown:       map[string][]byte{},
		Commitments: map[string][]byte{},
		Binding:     append([]byte(nil), w.Binding...),
	}
	for name, v := range w.Fields {
		cm, err := c.Commit(name, v)
		if err != nil {
			return Showing{}, err
		}
		if len(cm) == 0 {
			return Showing{}, fmt.Errorf("%w: %q", ErrNotCommitted, name)
		}
		out.Commitments[name] = cm
		if asked[name] {
			out.Shown[name] = append([]byte(nil), v...)
		}
	}
	return out, nil
}

// Check is the receiver's side, and it takes the showing and nothing else.
//
// That is the whole requirement: one field is shown on its own and the receiver
// checks it *without seeing the rest*. A check that needed the whole event
// would be no disclosure at all — the receiver would already hold everything it
// was supposed to be spared, and the showing would be a formality over a full
// copy.
//
// So the receiver holds: the shown values, a commitment for every field, and
// the binding. It verifies that exactly what was asked for came, that each
// shown value opens its own commitment, and that nothing else arrived in the
// clear. Whether it can conclude anything further — that no field was quietly
// dropped, that the commitments belong to one event and not several — depends
// on what the binding covers, and the form of the binding is the open ruling
// this package will not decide.
//
//	— T7.5, T13.1
func Check(s Showing, c Commit, asked ...string) error {
	want := map[string]bool{}
	for _, f := range asked {
		want[f] = true
	}
	for _, name := range sorted(s.Shown) {
		if !want[name] {
			return fmt.Errorf("%w: %q", ErrShowsMore, name)
		}
	}
	for _, name := range sorted(want) {
		v, ok := s.Shown[name]
		if !ok {
			return fmt.Errorf("%w: %q was asked for and did not come", ErrShowsLess, name)
		}
		cm, ok := s.Commitments[name]
		if !ok {
			return fmt.Errorf("%w: %q came with nothing tying it to the event",
				ErrShowsLess, name)
		}
		if !c.Opens(name, v, cm) {
			return fmt.Errorf("%w: what was shown for %q is not what the commitment "+
				"stands for", ErrNotCommitted, name)
		}
	}
	// Every other field is a commitment and nothing else. The receiver cannot
	// know what those fields hold, and must not be able to.
	for _, name := range sorted(s.Commitments) {
		cm := s.Commitments[name]
		if want[name] {
			continue
		}
		if len(cm) == 0 {
			return fmt.Errorf("%w: %q", ErrNotCommitted, name)
		}
		if _, leaked := s.Shown[name]; leaked {
			return fmt.Errorf("%w: %q", ErrShowsMore, name)
		}
	}
	if len(s.Binding) == 0 {
		return fmt.Errorf("%w: nothing ties these fields to the event", ErrShowsLess)
	}
	return nil
}

// Audit is the owner's side, and it is a different question.
//
// The owner has the whole event, so they can ask what the receiver cannot:
// does every commitment really stand for the field it was made from, and did
// anything travel in the clear that was meant to be withheld. This is how one
// checks what left, rather than hoping.
//
//	— T7.5, T7.3
func Audit(s Showing, c Commit, w Whole, asked ...string) error {
	if err := Check(s, c, asked...); err != nil {
		return err
	}
	want := map[string]bool{}
	for _, f := range asked {
		want[f] = true
	}
	for _, name := range sorted(w.Fields) {
		v := w.Fields[name]
		cm, ok := s.Commitments[name]
		if !ok {
			return fmt.Errorf("%w: %q is missing entirely", ErrShowsLess, name)
		}
		if !c.Opens(name, v, cm) {
			return fmt.Errorf("%w: the commitment for %q does not stand for it",
				ErrNotCommitted, name)
		}
		if want[name] {
			continue
		}
		// A withheld field must not be recoverable from what travels.
		if len(cm) == len(v) && string(cm) == string(v) {
			return fmt.Errorf("%w: %q travelled in the clear", ErrNotCommitted, name)
		}
	}
	for _, name := range sorted(s.Commitments) {
		if _, ok := w.Fields[name]; !ok {
			return fmt.Errorf("%w: %q is not a field of this event", ErrShowsMore, name)
		}
	}
	return nil
}

// Withheld names what did not travel in the clear, so an owner can say what
// they showed rather than hope.
func (s Showing) Withheld() []string {
	out := make([]string, 0, len(s.Commitments))
	for n := range s.Commitments {
		if _, shown := s.Shown[n]; !shown {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// sorted names a map's keys in a stable order.
//
// Every check here returns on the first fault it finds, and a map is walked in
// a different order each run — so without this, two runs over the same bad
// showing would name different fields as the problem. A complaint that changes
// between runs is a complaint nobody can act on.
func sorted[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
