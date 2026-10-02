package bond

import (
	"errors"
	"fmt"

	"rokh/frame"
)

// Two prohibitions, and everything else in the band stands on them.
var (
	// ErrOwnsAPerson is returned for a keeping that claims the person or
	// their ledger rather than a named piece of work.
	//
	// Nobody comes to own another person. A child's keeper holds authority
	// over named work, bounded by an event — the end of the work, a taking
	// back, or a terminal event named in advance — not by the hour, and not
	// by ownership of the person or of their ledger.
	//
	//	— T11.9
	ErrOwnsAPerson = errors.New("bond: nobody comes to own another person")
	// ErrKeeperClaimsIt is returned when a keeper is made owner of what was
	// deposited. A shared item is kept with a share, a named service and a
	// receipt for returning it.
	//
	//	— T11.9
	ErrKeeperClaimsIt = errors.New("bond: a keeper does not become the owner of what is kept")
)

// Bound is how a keeping ends. Every way is an event, and there is
// deliberately no field here that holds a time: testimony about the hour does
// not close authority.
//
//	— T11.9, T6.5, T5.3
type Bound struct {
	// EndOfWork: the named work is finished.
	EndOfWork bool `json:"endOfWork,omitempty"`
	// TakenBack: the authority was revoked.
	TakenBack bool `json:"takenBack,omitempty"`
	// Terminal is an event named in advance whose arrival ends the keeping.
	Terminal *frame.ID `json:"terminal,omitempty"`
}

// Named reports whether the bound is one of the three. An unbounded keeping is
// not a keeping; it is a claim.
func (b Bound) Named() bool {
	return b.EndOfWork || b.TakenBack || (b.Terminal != nil && !b.Terminal.IsZero())
}

// Keeping is one keeper holding a named piece of work for someone else.
type Keeping struct {
	// Holder is the keeper's own anchor.
	Holder frame.ID `json:"holder"`
	// Beneficiary is whose the work is. Their ledger stays theirs.
	Beneficiary frame.ID `json:"beneficiary"`
	// Work is the named work, and only that. It is never the person.
	Work string `json:"work"`
	// Share is the beneficiary's share of a kept item, in the smallest unit
	// the bond counts in. A keeper holds it; a keeper does not own it.
	Share uint64 `json:"share,omitempty"`
	// Until is how it ends.
	Until Bound `json:"until"`
}

// Check refuses a keeping that would make one person the owner of another, or
// a keeper the owner of what is kept.
//
//	— T11.9
func (k Keeping) Check() error {
	if k.Holder.IsZero() || k.Beneficiary.IsZero() {
		return fmt.Errorf("%w: a keeping needs both anchors", ErrShape)
	}
	if k.Holder == k.Beneficiary {
		return fmt.Errorf("%w: a person does not keep themselves", ErrShape)
	}
	if k.Work == "" {
		return fmt.Errorf("%w: a keeping is over named work", ErrOwnsAPerson)
	}
	if !k.Until.Named() {
		return fmt.Errorf("%w: a keeping with no named end is a claim on the person",
			ErrOwnsAPerson)
	}
	return nil
}

// Handover moves a keeping to another keeper. The bond does not break when
// the keeper changes — that is exactly what carries it from one generation to
// the next — and the share does not become the new keeper's either.
//
//	— T11.9
func (k Keeping) Handover(to frame.ID, until Bound) (Keeping, error) {
	if to.IsZero() || to == k.Beneficiary {
		return k, fmt.Errorf("%w: a keeping is handed to someone else", ErrShape)
	}
	next := k
	next.Holder = to
	next.Until = until
	if err := next.Check(); err != nil {
		return k, err
	}
	if next.Beneficiary != k.Beneficiary || next.Share != k.Share || next.Work != k.Work {
		return k, ErrKeeperClaimsIt
	}
	return next, nil
}
