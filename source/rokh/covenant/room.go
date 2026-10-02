package covenant

import (
	"crypto/ed25519"
	"fmt"

	"rokh/event"
	"rokh/frame"
)

// A covenant may name one room instead of a scope.
//
// A scope names a place. Handing over the scope "home/journal" hands over
// everything ever written there and everything that will be written there
// tomorrow, because an address is somewhere events keep arriving. Sometimes
// that is what the owner means. Often it is not: what is meant is one event,
// shown once, and nothing else, ever.
//
// A room is that covenant. It names a single event by the only name an event
// has, the hash of its own bytes, and it covers exactly that event. It covers
// no address, so it does not grow when the owner writes there again.
//
// A room is therefore strictly narrower than any scope that would have reached
// the same event, and it can never be wider than one. A scope only ever
// narrows.
//
// What the room settles is what the covenant names. It is not the last word on
// what a bundle carries: asking for a selection with its ancestry adds every
// accepted ancestor of a permitted event whatever the covenant says, which is
// that flag's stated purpose and its stated cost, and a room's one event has
// ancestors like any other. The room narrows the covenant; it does not reach
// past the covenant to overrule a decision made afterwards.
//
//	— T6.1, N4.4, T3.2, T7.6

// tagRoom is the room field of a share payload: 32 bytes, the name of the one
// event the covenant discloses.
//
// It sorts after the three fields a share already carries, so a payload that
// names a room stays in the ascending order the canonical form requires and
// the older fields keep the positions they had.
//
//	— T3.2, N4.2
const tagRoom frame.Tag = 0x0004

var (
	// ErrScopeAndRoom is returned for a covenant that names both a scope and
	// a room.
	//
	// The two answer the same question in two ways, and a covenant is given
	// one reach, not two. Reconciling them would mean deciding something
	// nobody wrote down: read as a union it widens, which is the one
	// direction forbidden outright, and read as an intersection it is a rule
	// invented here. So a shape the format does not have is refused rather
	// than repaired, the same way a second encoding of one payload is refused
	// rather than corrected.
	//
	//	— T6.1, N4.1
	ErrScopeAndRoom = fmt.Errorf("%w: a covenant names a scope or one room, never both", ErrShape)

	// ErrNoRoom is returned for a room that names no event. Naming no room is
	// written by leaving the field out, so an all-zero name is a malformed
	// payload and not a second way of saying nothing.
	ErrNoRoom = fmt.Errorf("%w: a room that names no event", ErrShape)
)

// Reach is what a covenant names: a scope, or one room, and never both.
//
// It stands apart from Covenant because it is the whole of the covering rule,
// and a covenant has two readers - the one that decides what may cross, and
// the one that decides which reading key closes it. Both ask here, so neither
// restates the rule and the two cannot come to disagree.
//
//	— T7.1, T7.4, N4.8
type Reach struct {
	// Scope is an address prefix. Empty means the whole ledger, as it always
	// has, unless Room is set.
	Scope string
	// Room is the name of the one event this covenant discloses. Zero means
	// the covenant names no room and answers from its scope.
	Room frame.ID
}

// IsRoom reports whether this covenant names one event rather than a place.
func (r Reach) IsRoom() bool { return !r.Room.IsZero() }

// Check refuses a reach that names two things at once, and checks the scope it
// names if it names one.
//
//	— T6.1, N4.1
func (r Reach) Check() error {
	if r.IsRoom() && r.Scope != "" {
		return ErrScopeAndRoom
	}
	if r.Scope != "" {
		return event.ValidAddress(r.Scope)
	}
	return nil
}

// CoversAddress reports whether this reach covers a whole address.
//
// A room covers none, and the answer is no rather than absent on purpose: a
// reader that knows only about addresses is told no, so a room covenant read
// by such a reader discloses nothing instead of everything. A room whose scope
// is empty must never be mistaken for a covenant over the whole ledger.
//
//	— T6.1, T7.3
func (r Reach) CoversAddress(address string) bool {
	if r.IsRoom() {
		return false
	}
	return event.ScopeCovers(r.Scope, address)
}

// Covers reports whether this reach covers one event, named by its id and
// standing at its address.
//
// A room covers that one event and nothing else: not the address it stands at,
// not what is written beside it, not what the owner writes there tomorrow. A
// reach that names no room answers exactly as it did before there were rooms,
// from its scope.
//
//	— T6.1, T7.1, N4.4
func (r Reach) Covers(id frame.ID, address string) bool {
	if r.IsRoom() {
		return r.Room == id
	}
	return r.CoversAddress(address)
}

// roomField is a room as it goes on the wire.
func roomField(room frame.ID) frame.Field {
	return frame.Field{Tag: tagRoom, Value: append([]byte(nil), room[:]...)}
}

// roomFrom reads the bytes of a room field.
func roomFrom(v []byte) (frame.ID, error) {
	var id frame.ID
	if len(v) != frame.IDSize {
		return id, fmt.Errorf("%w: room size", ErrShape)
	}
	copy(id[:], v)
	if id.IsZero() {
		return id, ErrNoRoom
	}
	return id, nil
}

// EncodeRoomShare writes a share payload that names one room.
//
// Nothing else about the covenant changes: the same address, the same verb,
// the same withdrawal by peer.unshare, and the same rule that only the owner
// discloses. A room narrows what a covenant reaches; it does not make a second
// kind of instrument.
//
//	— T7.2, T7.3
func EncodeRoomShare(subject ed25519.PublicKey, room frame.ID, sealTo []byte) ([]byte, error) {
	if len(subject) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: subject key size", ErrShape)
	}
	if room.IsZero() {
		return nil, ErrNoRoom
	}
	if sealTo != nil && len(sealTo) != SealKeySize {
		return nil, fmt.Errorf("%w: seal key size", ErrShape)
	}
	fs := frame.Fields{
		{Tag: tagSubject, Value: append([]byte(nil), subject...)},
		roomField(room),
	}
	if len(sealTo) > 0 {
		fs = append(fs, frame.Field{Tag: tagSealTo, Value: append([]byte(nil), sealTo...)})
	}
	return frame.EncodeFields(fs)
}

// DecodeRoom reads the room a share payload names, and reports whether it
// names one at all.
//
// It reads the payload rather than a decoded Share so that a reader holding
// only bytes can ask, and so that the refusal of a payload naming both a scope
// and a room has one home. It judges those two fields and no others; whether
// the payload is a well-formed share at all is still DecodeShare's question.
//
//	— T6.1, N4.1
func DecodeRoom(payload []byte) (frame.ID, bool, error) {
	fs, err := frame.DecodeFields(payload)
	if err != nil {
		return frame.ID{}, false, err
	}
	var room frame.ID
	scoped := false
	for _, f := range fs {
		switch f.Tag {
		case tagRoom:
			if room, err = roomFrom(f.Value); err != nil {
				return frame.ID{}, false, err
			}
		case tagScope:
			scoped = true
		}
	}
	if room.IsZero() {
		return frame.ID{}, false, nil
	}
	if scoped {
		return frame.ID{}, false, ErrScopeAndRoom
	}
	return room, true, nil
}
