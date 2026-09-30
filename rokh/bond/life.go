package bond

import (
	"errors"
	"fmt"

	"rokh/frame"
)

// Stage is one place in the cycle a bond has to be able to account for.
//
// The eight are named by the ruling and this is the whole of that list. They
// are not a state machine: what the ruling settles is that a bond which cannot
// say what happens at each of them has not been thought through. Which stage
// may follow which, and who holds the authority across the change, it names as
// still open — see Open below.
//
//	— T11.8
type Stage string

const (
	Entry     Stage = "entry"      // joining a bond that already stands
	Birth     Stage = "birth"      // arriving with no ledger of one's own
	Capable   Stage = "capable"    // reaching the capacity to hold a ledger of one's own
	Separated Stage = "separated"  // separation
	Incapable Stage = "incapable"  // incapacity
	Death     Stage = "death"      // death
	Succeeded Stage = "succession" // succession
	Closed    Stage = "settled"    // settling the account
)

// Stages lists the eight, in the order the ruling names them.
func Stages() []Stage {
	return []Stage{Entry, Birth, Capable, Separated, Incapable, Death, Succeeded, Closed}
}

func (s Stage) valid() bool {
	for _, k := range Stages() {
		if k == s {
			return true
		}
	}
	return false
}

// Open is the part of the cycle that is not settled, written here so that
// nothing in this package quietly settles it.
//
// The ruling names the bound and names the cycle. It does not close the
// transitions, and it does not say who holds the authority across a change. So
// Permitted below refuses every passage, including the ones that look obvious
// — because a function that answered the obvious ones would have decided the
// shape of the answer for the rest.
//
//	— T13.6, T11.8, T11.9
const Open = "which stage follows which, and who holds authority across the change"

var (
	// ErrCycleOpen is returned by Permitted, always.
	ErrCycleOpen = errors.New("bond: " + Open + " — an open ruling")
	// ErrNoSuchStage is returned for something that is not one of the eight.
	ErrNoSuchStage = errors.New("bond: not one of the eight stages")
)

// Passage is one move through the cycle: who it is about, where from, where
// to, and whatever keeping covers them across it.
type Passage struct {
	// Who is the person whose passage this is. Their ledger is theirs at
	// every stage of it, including the ones where they cannot hold it.
	Who  frame.ID
	From Stage
	To   Stage
	// Cover is whoever acts for them across the change, if anyone does. Nil
	// means nobody does, which is a real answer and not a missing one.
	Cover *Keeping
	// Outstanding is a debt still open across the passage.
	Outstanding bool
}

// Check says what can be said about a passage: the bounds that hold whatever
// the transition turns out to be.
//
// Those bounds are not open. Whoever acts for someone at any stage does so
// under a keeping over named work, bounded by an event, and never over the
// person or their ledger — and that is as true at birth and at incapacity as
// anywhere else, which is exactly where it matters. A debt open across the
// passage stays open: leaving closes the future, death closes the future, and
// neither of them settles anything. The account is closed by settling, which
// is an act somebody performs.
//
//	— T11.8, T11.9, T11.7
func (p Passage) Check() error {
	if p.Who.IsZero() {
		return fmt.Errorf("%w: a passage is somebody's", ErrShape)
	}
	if !p.From.valid() {
		return fmt.Errorf("%w: %q", ErrNoSuchStage, p.From)
	}
	if !p.To.valid() {
		return fmt.Errorf("%w: %q", ErrNoSuchStage, p.To)
	}
	if p.Cover != nil {
		if p.Cover.Beneficiary != p.Who {
			return fmt.Errorf("%w: the keeping is for %s, the passage is %s's",
				ErrShape, p.Cover.Beneficiary.Short(), p.Who.Short())
		}
		if err := p.Cover.Check(); err != nil {
			return err
		}
	}
	if p.Outstanding && p.To == Closed {
		// Settling with a debt still open is not settling. The act that
		// closes the account is the one that clears it, and saying the
		// account is closed while it is not would put the claim in the
		// ledger where the clearing should have been.
		return fmt.Errorf("%w: an open debt is not closed by declaring it closed",
			ErrShape)
	}
	return nil
}

// Permitted answers whether a passage may be made, and it cannot.
//
// It returns ErrCycleOpen for every passage, including from a stage to itself
// and including the ones anybody would call obvious. That is the point: the
// transitions and the holders of authority across them are the owner's to settle,
// and a code path that quietly allowed the easy cases would have settled them.
//
//	— T13.6, T11.8
func (p Passage) Permitted() error { return ErrCycleOpen }

// Accounts reports whether an arrangement has said what happens at every one
// of the eight, which is the whole of what the ruling requires of it.
//
// It takes what the arrangement covers and names what it has not thought
// about. A bond that is silent about incapacity has not decided that
// incapacity will not happen; it has decided nothing, and the difference shows
// up exactly once, at the worst moment.
//
//	— T11.8
func Accounts(covered []Stage) (missing []Stage, whole bool) {
	have := map[Stage]bool{}
	for _, s := range covered {
		have[s] = true
	}
	for _, s := range Stages() {
		if !have[s] {
			missing = append(missing, s)
		}
	}
	return missing, len(missing) == 0
}
