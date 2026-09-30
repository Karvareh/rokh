package harness

import (
	"errors"
	"fmt"
)

// Rokh alone cannot promise that an effect outside it happens exactly once.
// The destination is not under the ledger, there is no transaction spanning
// both, and the ledger cannot reach across to undo what has landed. What Rokh
// can do is make a harness say, before it is bound, what its destination
// actually does — so that a reader who sees a receipt knows what a repeat of it
// would have meant.
//
// So every harness declares four things up front: what a repeat does, whether
// it may send again on its own, whether the effect can be undone, and what it
// does when it never learns how the step ended.
//
//	— T10.7

// Repeat says what a second identical effect does at the destination.
type Repeat string

const (
	// Idempotent: the destination cooperates. Given the same deduplication
	// handle, the effect lands once however many times it is sent.
	Idempotent Repeat = "idempotent"
	// Duplicates: it lands again. This is not a flaw to be fixed here — it is
	// a fact about somewhere else, and saying it is the only honest move.
	Duplicates Repeat = "duplicates"
)

func (r Repeat) valid() bool { return r == Idempotent || r == Duplicates }

// Retry says whether the harness may send again on its own initiative.
type Retry string

const (
	// MayRetry: it is allowed to send again without being asked.
	MayRetry Retry = "may-retry"
	// NeverRetry: a send that did not visibly succeed is not sent again by the
	// harness; someone decides.
	NeverRetry Retry = "never-retry"
)

func (r Retry) valid() bool { return r == MayRetry || r == NeverRetry }

// Compensate says whether there is an act that undoes the effect.
type Compensate string

const (
	// Compensable: an undoing act exists and the harness can perform it.
	Compensable Compensate = "compensable"
	// Irreversible: there is no undoing. A receipt over an irreversible effect
	// is the only record there will be of it.
	Irreversible Compensate = "irreversible"
)

func (c Compensate) valid() bool { return c == Compensable || c == Irreversible }

// Ending says what the harness does when it never learns how the step ended.
//
// Both answers close the receipt as unknown, and there is no third that closes
// it as failed. Assuming a failure is a guess, and a guessed ending recorded as
// a fact is worse than an admitted gap: the gap can be looked into, the guess
// cannot be told apart from knowledge afterwards.
//
//	— T10.4
type Ending string

const (
	// CloseUnknown: write the unknown ending and stop there.
	CloseUnknown Ending = "close-unknown"
	// AskAPerson: write the unknown ending and put it in front of someone.
	AskAPerson Ending = "ask-a-person"
)

func (e Ending) valid() bool { return e == CloseUnknown || e == AskAPerson }

// Effects is the four declarations. All four are required: a harness that has
// said three of them has not said what its effects do.
//
//	— T10.7
type Effects struct {
	Repeat     Repeat
	Retry      Retry
	Compensate Compensate
	Ending     Ending
}

// ErrUndeclared is returned when one of the four is missing.
var ErrUndeclared = errors.New("harness: an effect has four declarations and all are required")

// Declare checks the four and returns them ready to use. Like the covenant it
// refuses rather than repairs: an absent declaration is not a default, because
// a default here would be Rokh guessing what somewhere else does.
//
//	— T10.7
func (e Effects) Declare() (Effects, error) {
	if !e.Repeat.valid() {
		return e, fmt.Errorf("%w: what a repeat does (%q is not an answer; %s or %s)", ErrUndeclared, e.Repeat, Idempotent, Duplicates)
	}
	if !e.Retry.valid() {
		return e, fmt.Errorf("%w: whether it may retry (%q is not an answer; %s or %s)", ErrUndeclared, e.Retry, MayRetry, NeverRetry)
	}
	if !e.Compensate.valid() {
		return e, fmt.Errorf("%w: whether it can be undone (%q is not an answer; %s or %s)", ErrUndeclared, e.Compensate, Compensable, Irreversible)
	}
	if !e.Ending.valid() {
		return e, fmt.Errorf("%w: what a lost ending does (%q is not an answer; %s or %s)", ErrUndeclared, e.Ending, CloseUnknown, AskAPerson)
	}
	return e, nil
}

// MayResend reports whether sending the same effect again is safe on this
// harness's own declarations.
//
// It is not a fifth rule; it is the arithmetic of the four. A harness that may
// retry into a destination that duplicates has declared, in its own words, that
// its effect will land twice. Nothing here forbids that combination — a harness
// may have good reason, and forbidding it would be Rokh ruling on somewhere
// else — but nothing here calls it safe either.
//
//	— T10.7
func (e Effects) MayResend() bool { return e.Retry == MayRetry && e.Repeat == Idempotent }
