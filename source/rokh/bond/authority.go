package bond

import (
	"errors"
	"fmt"
	"sort"
)

// Power is one of the four authorities a bond has to keep apart.
//
// The hard part is the separation, not the list. It is easy to write four
// names and then have the code imply three of them from one — reading follows
// from writing, keeping follows from acting for someone, and before long
// whoever was given the smallest thing has all four. So each is granted on its
// own and none is ever derived.
//
//	— T11.8
type Power string

const (
	// Reading: seeing what is in someone's ledger.
	Reading Power = "read" // reading
	// Writing: putting an event into it.
	Writing Power = "write" // writing
	// Acting: doing a thing in someone's name, which is not writing in their
	// ledger and does not become it.
	Acting Power = "on-behalf" // acting on another's behalf
	// Keeping a thing for them, which is holding, not owning, and not any of
	// the other three.
	Holding Power = "keep" // keeping
)

// Powers lists the four, in the order the ruling names them.
func Powers() []Power { return []Power{Reading, Writing, Acting, Holding} }

func (p Power) valid() bool {
	switch p {
	case Reading, Writing, Acting, Holding:
		return true
	}
	return false
}

// ErrNotGranted is returned for a power that was never given.
var ErrNotGranted = errors.New("bond: that authority was not granted")

// ErrNoSuchPower is returned for something that is not one of the four.
var ErrNoSuchPower = errors.New("bond: not one of the four authorities")

// Held is what somebody was actually given, and only that.
//
// There is no widening operation here and no implication table. Can answers
// from what was granted and from nothing else, which is the whole content of
// "none follows from another": the way to hold two authorities is to have been
// given two.
//
//	— T11.8, T6.1
type Held struct{ granted map[Power]bool }

// Grant gives exactly the powers named. Anything not named is not held.
//
//	— T11.8
func Grant(ps ...Power) (Held, error) {
	h := Held{granted: map[Power]bool{}}
	for _, p := range ps {
		if !p.valid() {
			return Held{}, fmt.Errorf("%w: %q", ErrNoSuchPower, p)
		}
		h.granted[p] = true
	}
	return h, nil
}

// Can reports whether this power was granted. It does not ask what else was.
func (h Held) Can(p Power) bool { return h.granted[p] }

// Must is Can as a refusal, so a caller can say what is missing rather than
// silently doing less.
func (h Held) Must(p Power) error {
	if !p.valid() {
		return fmt.Errorf("%w: %q", ErrNoSuchPower, p)
	}
	if !h.granted[p] {
		return fmt.Errorf("%w: %s", ErrNotGranted, p)
	}
	return nil
}

// All lists what is held, in a stable order.
func (h Held) All() []Power {
	out := make([]Power, 0, len(h.granted))
	for p := range h.granted {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

// Narrow drops powers. Authority narrows and never widens, so this is the only
// way one Held becomes another: there is deliberately no Widen.
//
//	— T11.8, T6.1
func (h Held) Narrow(drop ...Power) Held {
	out := Held{granted: map[Power]bool{}}
	gone := map[Power]bool{}
	for _, p := range drop {
		gone[p] = true
	}
	for p := range h.granted {
		if !gone[p] {
			out.granted[p] = true
		}
	}
	return out
}
