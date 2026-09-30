package bond

import (
	"errors"
	"fmt"
	"sort"

	"rokh/frame"
)

// Pen is a shared pen: an authority to write that more than one person has a
// share in, held by one of them for all of them.
//
// It is the sharpest test of the two prohibitions, because a pen is exactly
// the thing a keeper would want to keep. So: the keeper holds it and does not
// own it, every holder's share is named, the keeping ends at a named event
// rather than at an hour, and handing it back is an act with a receipt — not a
// silence that someone later calls a return.
//
// A pen is not the ledgers. Holding it is a bounded authority to write towards
// the bond's work; it never becomes authority over a person or over anyone's
// ledger, and the ledgers stay separate throughout.
//
//	— T11.8, T11.9, T11.6
type Pen struct {
	// Of is the bond this pen belongs to.
	Of frame.ID `json:"of"`
	// Keeper holds it. One keeper at a time: a pen held by nobody in
	// particular is a pen nobody can be asked about.
	Keeper frame.ID `json:"keeper"`
	// Shares is each holder's share, in whatever smallest unit the bond
	// counts in. The keeper's own share is one among them and carries no
	// extra weight.
	Shares map[frame.ID]uint64 `json:"shares"`
	// Until is how the keeping ends, and it is an event, never an hour.
	Until Bound `json:"until"`
}

var (
	// ErrNoShare is returned for a pen with no shares, or a share of nothing.
	ErrNoShare = errors.New("bond: a shared pen names every holder's share")
	// ErrUnreturned is returned for a handing-back with no receipt behind it.
	ErrUnreturned = errors.New("bond: returning the pen is an act with a receipt")
)

// Check refuses a pen that is not really shared, not really kept, or not
// really bounded.
//
//	— T11.8, T11.9
func (p Pen) Check() error {
	if p.Of.IsZero() {
		return fmt.Errorf("%w: a pen belongs to a bond", ErrShape)
	}
	if p.Keeper.IsZero() {
		return fmt.Errorf("%w: a pen is held by someone", ErrShape)
	}
	if len(p.Shares) < 2 {
		return fmt.Errorf("%w: a pen shared by one is not shared", ErrNoShare)
	}
	// In name order, not map order. A check that returns on the first fault
	// found would otherwise name a different holder on each run, and two runs
	// over the same bad pen would disagree about what is wrong with it.
	for _, who := range p.Holders() {
		if who.IsZero() {
			return fmt.Errorf("%w: a share belonging to nobody", ErrNoShare)
		}
		if p.Shares[who] == 0 {
			return fmt.Errorf("%w: %s holds a share of nothing", ErrNoShare, who.Short())
		}
	}
	if _, ok := p.Shares[p.Keeper]; !ok {
		// The keeper is one of the sharers. A keeper from outside the sharing
		// is holding somebody else's property with no stake and no standing —
		// which is the arrangement the second prohibition is about.
		return fmt.Errorf("%w: the keeper holds no share in what they keep",
			ErrKeeperClaimsIt)
	}
	if !p.Until.Named() {
		return fmt.Errorf("%w: a pen kept with no named end is a claim on it",
			ErrKeeperClaimsIt)
	}
	return nil
}

// Holders lists everyone with a share, in a stable order.
func (p Pen) Holders() []frame.ID {
	out := make([]frame.ID, 0, len(p.Shares))
	for h := range p.Shares {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out
}

// Handback is the return of a shared pen, and the receipt is not paperwork.
//
// Returning is one of the five acts and is separate from leaving for a reason:
// somebody can go and still be holding the pen, and somebody can hand the pen
// back and still be in the bond. Without a receipt naming the return there is
// nothing to tell those apart afterwards, and "I gave it back" would be a
// claim rather than a record.
//
//	— T11.8, T11.7, T10.1
type Handback struct {
	Pen frame.ID `json:"pen"`
	// To is who holds it now. A pen is always held by someone.
	To frame.ID `json:"to"`
	// Receipt is the recorded event that closed the returning. It is a name
	// in a ledger, not a promise.
	Receipt frame.ID `json:"receipt"`
}

// Return hands the pen to another holder, and will not do it without a
// receipt. The share does not move with the keeping: whoever held a share
// before holds the same share after.
//
//	— T11.8, T11.9, T11.7
func (p Pen) Return(h Handback, until Bound) (Pen, error) {
	if err := p.Check(); err != nil {
		return p, err
	}
	if h.Receipt.IsZero() {
		return p, fmt.Errorf("%w: nothing records that it was handed back", ErrUnreturned)
	}
	if h.To.IsZero() {
		return p, fmt.Errorf("%w: a pen is always held by someone", ErrShape)
	}
	if h.To == p.Keeper {
		return p, fmt.Errorf("%w: it was already held by %s", ErrShape, h.To.Short())
	}
	next := p
	next.Keeper = h.To
	next.Until = until
	next.Shares = map[frame.ID]uint64{}
	for who, share := range p.Shares {
		next.Shares[who] = share
	}
	if err := next.Check(); err != nil {
		return p, err
	}
	return next, nil
}

// Writes is the authority a pen actually confers: the power to write, and that
// alone. Holding the pen is not reading someone's ledger, not acting in their
// name, and not keeping anything else of theirs.
//
//	— T11.8
func (p Pen) Writes() Held {
	h, _ := Grant(Writing)
	return h
}
