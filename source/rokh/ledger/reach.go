package ledger

import "rokh/frame"

// Reach is how far a receiver may go on what it has actually checked.
//
// The distinction it draws is the one the ruling insists on and that is easy to
// lose: verifying a history proves the history is *sound*, and proves nothing
// at all about whether it is *whole*.
//
//	— T5.6
type Reach int

const (
	// Sound: every byte matched its hash, every hash its signature, and every
	// signature the authority live at that point. That is the whole of what
	// was established. It says nothing about what was not shown.
	Sound Reach = iota
	// Extends: sound, and it contains everything the receiver had already
	// witnessed. The history has grown, and grown from where it was.
	Extends
	// Diverges: sound, and something the receiver witnessed before is not in
	// it. Two branches, and this is not the one the earlier sighting was on.
	Diverges
)

func (r Reach) String() string {
	switch r {
	case Extends:
		return "extends what was witnessed"
	case Diverges:
		return "diverges from what was witnessed"
	}
	return "sound, and nothing more"
}

// Against reports what a receiver holding this ledger may conclude, given what
// they had already witnessed of it, and names whatever is missing.
//
// The honest reading of the ruling lives in the zero case. A receiver with no
// prior sighting gets Sound and an empty list, and that is not a weak result
// that better checking would improve — it is the ceiling. The holder of the key
// can write two branches and show one; both verify perfectly, neither mentions
// the other, and nothing inside the shown history betrays the hidden one.
//
// No agreement among peers repairs this, because there is no peer whose word
// would count: the signature is valid on both branches and the ledger is one
// person's. What breaks the concealment is only evidence from before — a
// receipt someone kept, or an anchor laid down earlier somewhere else — and
// that evidence has to have existed already. It cannot be manufactured after
// the fact by looking harder at what was handed over.
//
//	— T5.6, N2.6, T12
func (l *Ledger) Against(witnessed []frame.ID) (Reach, []frame.ID) {
	var missing []frame.ID
	for _, id := range witnessed {
		if l.State(id) != Accepted {
			missing = append(missing, id)
		}
	}
	switch {
	case len(witnessed) == 0:
		return Sound, nil
	case len(missing) > 0:
		return Diverges, missing
	default:
		return Extends, nil
	}
}
