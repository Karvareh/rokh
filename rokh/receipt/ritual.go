package receipt

import (
	"errors"
	"fmt"

	"rokh/event"
	"rokh/frame"
)

// Keeper is the ledger as this ritual needs it: somewhere to record an event,
// and a way to walk what is already recorded.
//
// The ritual neither signs nor stores. It decides what the two halves say and
// hands them over; whoever satisfies this interface holds the key and the
// carrier. That is why Record returns a name rather than taking one — the name
// is the hash of the bytes as finally signed, so only the recorder can know it,
// and a name invented before the recording would be a name for bytes nobody
// wrote.
//
//	— T10.1, T3.2
type Keeper interface {
	Record(e event.Event) (frame.ID, error)
	Each(visit func(id frame.ID, e event.Event) bool) error
}

// ErrRepeat is returned when a handle has already closed a receipt here.
var ErrRepeat = errors.New("receipt: that handle already closed a receipt")

// ErrNoSuchIntent is returned for a result whose intent is not in the ledger.
var ErrNoSuchIntent = errors.New("receipt: the intent is not recorded")

// Ritual writes both halves of a receipt into a real ledger.
//
// This is the part that could not be got right on paper. An intent is not a
// struct in memory that a result points at by agreement; it is a recorded,
// signed event, and its name is the name of those bytes. The result names that
// name as its parent, so the pairing is the ledger's own parent link and not a
// convention the two ends have to keep.
//
//	— T10, T10.1
type Ritual struct {
	addr string
	k    Keeper
}

// New opens a ritual that writes at one address.
func New(addr string, k Keeper) *Ritual { return &Ritual{addr: addr, k: k} }

// Address is where this ritual writes.
func (r *Ritual) Address() string { return r.addr }

// Open records the intent, before the step, and returns the recorded event's
// own name. That name is what the result will answer.
//
//	— T10.1, T10.2
func (r *Ritual) Open(in Intent) (frame.ID, error) {
	e, err := OpenIntent(r.addr, in)
	if err != nil {
		return frame.ID{}, err
	}
	return r.k.Record(e)
}

// Close records the result, after the step.
//
// The intent must already be in the ledger. A result whose parent is a name
// nobody recorded answers nothing: it is a statement about an outcome with no
// question in front of it, which is exactly what the parent link exists to
// prevent.
//
//	— T10.1, T10.3, T10.4
func (r *Ritual) Close(intent frame.ID, res Result) (frame.ID, error) {
	var zero frame.ID
	opened, ok, err := r.intent(intent)
	if err != nil {
		return zero, err
	}
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrNoSuchIntent, intent)
	}
	// The witness names the intent by its real name. Filling it here rather
	// than trusting the caller removes the one way the two halves could
	// disagree about which receipt they are.
	res.Witness.Receipt = intent.String()
	if res.Once != "" {
		if prior, found, err := r.Closed(res.Once); err != nil {
			return zero, err
		} else if found {
			return zero, fmt.Errorf("%w: %s did", ErrRepeat, prior)
		}
	}
	// The result is written where its intent stands, not where this ritual
	// happens to be open.
	//
	// The address is the aperture. A ritual opened at a place reaches the
	// intents written below it too, so a ritual at "x" can close an intent at
	// "x/child" — and writing the result at "x" put the two halves of one
	// receipt on opposite sides of the narrower aperture. A reader at
	// "x/child" then saw the intent, never the result, and reported a closed
	// receipt as open for as long as it existed. Keeping the pair together is
	// what makes every aperture that sees either half see both.
	//   — T10, T10.1, T10.6
	e, err := CloseWith(opened.Address, intent, res)
	if err != nil {
		return zero, err
	}
	return r.k.Record(e)
}

// Closed reports whether a result carrying this handle is already recorded,
// and which one.
//
// This is the whole of what Rokh can do about a repeat, and the limit is worth
// saying plainly: it can tell a harness that the effect was already claimed
// here. It cannot reach the destination and stop a second one landing there.
// The destination is not under the ledger, there is no transaction spanning
// both, and no amount of care on this side changes that. A destination that
// cooperates can make itself idempotent by this handle; one that does not,
// cannot be made to.
//
//	— T10.7
func (r *Ritual) Closed(once string) (frame.ID, bool, error) {
	var found frame.ID
	var yes bool
	err := r.k.Each(func(id frame.ID, e event.Event) bool {
		if verb, ok := Concerns(e, r.addr); !ok || verb != VerbOutcome {
			return true
		}
		res, err := Read[Result](e.Payload)
		if err != nil || res.Once != once {
			return true
		}
		found, yes = id, true
		return false
	})
	return found, yes, err
}

// answers reports which intent a result answers.
//
// It reads the sixth witness, which is the field that exists to say exactly
// this, and then checks that the same name is among the parents — so a result
// is paired with its intent only where the two agree.
//
// Reading the parents alone was wrong, and quietly so. A result carries every
// parent the recording gave it, and a keeper that builds on the branch head
// hands it one more: whichever event the branch happened to be sitting on. When
// that event was an open intent of somebody else's, the result answered it as
// well, and the receipt nobody had closed vanished from the open list. Two
// receipts running at once was all it took, which is the case the receipt is
// for.
//
//	— T10.1, T10.2
func answers(e event.Event) (frame.ID, bool) {
	var zero frame.ID
	res, err := Read[Result](e.Payload)
	if err != nil {
		// Not readable as the one encoding of a result, so it names no intent
		// this can act on. Saying "answers nothing" leaves the intent open,
		// which is the honest direction to fail in: an unreadable event must
		// not be able to close somebody's receipt.
		//   — N4.1
		return zero, false
	}
	named, err := frame.ParseID(res.Witness.Receipt)
	if err != nil {
		return zero, false
	}
	for _, p := range e.Parents {
		if p == named {
			return named, true
		}
	}
	return zero, false
}

// Answered reports whether an intent already has a result, and which.
//
//	— T10.1
func (r *Ritual) Answered(intent frame.ID) (frame.ID, bool, error) {
	var found frame.ID
	var yes bool
	err := r.k.Each(func(id frame.ID, e event.Event) bool {
		if verb, ok := Concerns(e, r.addr); !ok || verb != VerbOutcome {
			return true
		}
		if named, ok := answers(e); ok && named == intent {
			found, yes = id, true
			return false
		}
		return true
	})
	return found, yes, err
}

// Open reports the intents here that no result answers — the steps that were
// begun and never closed either way.
//
// A harness that has to close its books reads this. Nothing here closes them
// for it: an unanswered intent stays unanswered until someone writes a result,
// including the honest one that says the ending was never learned.
//
//	— T10.1, T10.4, N-Axiom2
func (r *Ritual) Unanswered() ([]frame.ID, error) {
	answered := map[frame.ID]bool{}
	var intents []frame.ID
	err := r.k.Each(func(id frame.ID, e event.Event) bool {
		switch verb, ok := Concerns(e, r.addr); {
		case !ok:
		case verb == VerbIntent:
			intents = append(intents, id)
		case verb == VerbOutcome:
			// The one intent this result answers, not every parent it
			// happens to carry. See answers.
			//   — T10.2
			if named, ok := answers(e); ok {
				answered[named] = true
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	var out []frame.ID
	for _, id := range intents {
		if !answered[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (r *Ritual) intent(id frame.ID) (event.Event, bool, error) {
	var found event.Event
	var yes bool
	err := r.k.Each(func(got frame.ID, e event.Event) bool {
		if got != id {
			return true
		}
		if verb, ok := Concerns(e, r.addr); ok && verb == VerbIntent {
			found, yes = e, true
		}
		return false
	})
	return found, yes, err
}
