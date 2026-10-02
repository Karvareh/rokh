// Package covenant decides which of a ledger's events may be disclosed to a
// peer.
//
// # Disclosure is not authorship
//
// event.VerbGrant is authority to *write*: may this key append an event about
// this address? It must never, by extension or convenience, come to answer
// "may this key receive that event?"
//
// So disclosure gets its own grammar, its own name and its own evaluator, and
// they live here rather than in the core. Adding a reserved verb to the core
// would extend the settled contract of docs/01, which needs its own ruling.
// Absent that ruling, a covenant is built from ordinary, non-reserved verbs
// and evaluated outside the core. The core never learns what a peer is.
//
// # What a covenant is
//
//	address  peer
//	verb     peer.share     (to withdraw: peer.unshare)
//
// A covenant is outbound and one-directional: "these events of mine may be
// disclosed to P". It says nothing about what P may send here; inbound bytes
// are judged by the receiving ledger on their own merits, as always.
//
// # What a covenant is not
//
//   - It does not start a transfer. A covenant permits; it never opens a
//     stream, and nothing here runs on its own.
//   - It does not make a courier trustworthy. Disclosure is decided by the
//     sender at bundling time; the courier is handed only what it may carry.
//   - It does not follow from shared ownership. Two carriers having the same
//     owner implies nothing. A same-owner mirror is a separate ruling.
package covenant

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// The address and verbs a covenant lives at. All ordinary and non-reserved:
// the core treats these events like any other.
const (
	Address     = "peer"
	VerbShare   = "peer.share"
	VerbUnshare = "peer.unshare"
)

// Share payload fields.
const (
	tagSubject frame.Tag = 0x0001 // 32 bytes, the recipient's Ed25519 key
	tagScope   frame.Tag = 0x0002 // UTF-8 address prefix; absent means whole ledger
	tagSealTo  frame.Tag = 0x0003 // 32 bytes, the recipient's X25519 key, optional
)

// Unshare payload fields.
const tagTarget frame.Tag = 0x0001 // 32 bytes, the id of the share being withdrawn

// SealKeySize is the size of an X25519 public key.
const SealKeySize = 32

var ErrShape = errors.New("covenant: bad payload")

// Share is the payload of a peer.share event.
type Share struct {
	Subject ed25519.PublicKey
	Scope   string
	SealTo  []byte   // optional X25519 key; see docs/07 section 6.4
	Room    frame.ID // one event instead of a scope; see room.go
}

// Reach is what this share names: its scope, or its room.
func (s Share) Reach() Reach { return Reach{Scope: s.Scope, Room: s.Room} }

// Unshare is the payload of a peer.unshare event.
type Unshare struct {
	Target frame.ID
}

// Covenant is a live disclosure permission, as seen from some point in the
// graph.
// "May write here" and "may read this" are two different things with two
// different instruments. Disclosure is outward and one-way, it comes from
// the owner's authority, and a right to write never extends into a right
// to read. Showing is an effect too, and it passes through this gate.
//
//	— T7, T7.1, T7.3, T4.3, N4.8
type Covenant struct {
	ID      frame.ID // the peer.share event that created it
	Subject ed25519.PublicKey
	Scope   string
	SealTo  []byte
	Room    frame.ID // one event instead of a scope; see room.go
}

// Reach is what this covenant names: its scope, or its room.
func (c Covenant) Reach() Reach { return Reach{Scope: c.Scope, Room: c.Room} }

// Covers reports whether a scope covers an address, on the component boundary:
// "home" covers "home/journal" and does not cover "homestead". Exposed so a
// caller can narrow a selection further without reimplementing the rule.
func Covers(scope, address string) bool { return event.ScopeCovers(scope, address) }

// Covers reports whether this covenant permits disclosing an event at the
// given address. Same component-boundary rule as a grant scope: "home" covers
// "home/journal" and does not cover "homestead".
func (c Covenant) Covers(address string) bool {
	return c.Reach().CoversAddress(address)
}

// For reports whether this covenant names the given recipient.
func (c Covenant) For(peer ed25519.PublicKey) bool {
	return bytes.Equal(c.Subject, peer)
}

func (s Share) check() error {
	if len(s.Subject) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: subject key size", ErrShape)
	}
	if err := s.Reach().Check(); err != nil {
		return err
	}
	if s.SealTo != nil && len(s.SealTo) != SealKeySize {
		return fmt.Errorf("%w: seal key size", ErrShape)
	}
	return nil
}

// Encode writes a share payload in canonical form.
func (s Share) Encode() ([]byte, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	fs := frame.Fields{{Tag: tagSubject, Value: append([]byte(nil), s.Subject...)}}
	if s.Scope != "" {
		fs = append(fs, frame.Field{Tag: tagScope, Value: []byte(s.Scope)})
	}
	if len(s.SealTo) > 0 {
		fs = append(fs, frame.Field{Tag: tagSealTo, Value: append([]byte(nil), s.SealTo...)})
	}
	if !s.Room.IsZero() {
		fs = append(fs, roomField(s.Room))
	}
	return frame.EncodeFields(fs)
}

// DecodeShare reads a share payload and enforces the canonical form.
func DecodeShare(b []byte) (Share, error) {
	var s Share
	fs, err := frame.DecodeFields(b)
	if err != nil {
		return s, err
	}
	for _, f := range fs {
		switch f.Tag {
		case tagSubject:
			if len(f.Value) != ed25519.PublicKeySize {
				return s, fmt.Errorf("%w: subject key size", ErrShape)
			}
			s.Subject = ed25519.PublicKey(append([]byte(nil), f.Value...))
		case tagScope:
			s.Scope = string(f.Value)
			if err := event.ValidAddress(s.Scope); err != nil {
				return s, err
			}
		case tagSealTo:
			if len(f.Value) != SealKeySize {
				return s, fmt.Errorf("%w: seal key size", ErrShape)
			}
			s.SealTo = append([]byte(nil), f.Value...)
		case tagRoom:
			if s.Room, err = roomFrom(f.Value); err != nil {
				return s, err
			}
		default:
			return s, fmt.Errorf("%w: unknown field (tag=%d)", ErrShape, f.Tag)
		}
	}
	if s.Subject == nil {
		return s, fmt.Errorf("%w: share without a subject", ErrShape)
	}
	if err := s.Reach().Check(); err != nil {
		return s, err
	}
	return s, nil
}

// Encode writes an unshare payload.
func (u Unshare) Encode() ([]byte, error) {
	if u.Target.IsZero() {
		return nil, fmt.Errorf("%w: unshare without a target", ErrShape)
	}
	return frame.EncodeFields(frame.Fields{
		{Tag: tagTarget, Value: append([]byte(nil), u.Target[:]...)},
	})
}

// DecodeUnshare reads an unshare payload.
func DecodeUnshare(b []byte) (Unshare, error) {
	var u Unshare
	fs, err := frame.DecodeFields(b)
	if err != nil {
		return u, err
	}
	for _, f := range fs {
		switch f.Tag {
		case tagTarget:
			if len(f.Value) != frame.IDSize {
				return u, fmt.Errorf("%w: target size", ErrShape)
			}
			copy(u.Target[:], f.Value)
		default:
			return u, fmt.Errorf("%w: unknown field (tag=%d)", ErrShape, f.Tag)
		}
	}
	if u.Target.IsZero() {
		return u, fmt.Errorf("%w: unshare without a target", ErrShape)
	}
	return u, nil
}

// ActiveAt returns the live covenants as seen from the given points in the
// graph. With no points given, it uses the ledger's heads.
//
// This reuses the causal pattern of docs/02 and nothing else: both sets grow
// monotonically along the graph, so a withdrawn covenant can never come back
// to life, and the answer at any named head is settled by that head's ancestry
// alone.
//
// It is deliberately not ledger.ActiveGrants, which means core writing
// authority and keeps that meaning.
//
// # Only the root may disclose, for now
//
// The address "peer" is an ordinary address, so a write-delegate holding a
// broad scope could otherwise author a covenant and disclose the ledger to
// itself. Every covenant whose author is not the genesis author is ignored
// here.
//
// Delegated disclosure - a "may share" right - is a later ruling. Until then,
// disclosure is the owner's act alone.
func ActiveAt(l *ledger.Ledger, at ...frame.ID) []Covenant {
	points := at
	if len(points) == 0 {
		points = l.Heads()
	}
	root := l.Root()

	shares := map[frame.ID]Covenant{}
	withdrawn := map[frame.ID]bool{}
	var order []frame.ID

	for _, id := range l.CausalPast(points...) {
		e, found := l.Get(id)
		if !found || e.Event.Address != Address {
			continue
		}
		// Only the owner discloses. See the note above.
		if !bytes.Equal(e.Event.Author, root) {
			continue
		}
		switch e.Event.Verb {
		case VerbShare:
			s, err := DecodeShare(e.Event.Payload)
			if err != nil {
				continue // a malformed covenant grants nothing
			}
			if _, seen := shares[id]; !seen {
				order = append(order, id)
			}
			shares[id] = Covenant{ID: id, Subject: s.Subject, Scope: s.Scope,
				SealTo: s.SealTo, Room: s.Room}
		case VerbUnshare:
			u, err := DecodeUnshare(e.Event.Payload)
			if err != nil {
				continue
			}
			withdrawn[u.Target] = true
		}
	}

	out := make([]Covenant, 0, len(order))
	for _, id := range order {
		if !withdrawn[id] {
			out = append(out, shares[id])
		}
	}
	return out
}

// Discloses reports whether any live covenant permits disclosing an event at
// the given address to the given peer, and returns the covenant that does.
//
// This is the question a sender's bundle layer asks before handing anything to
// a courier. It permits; it does not transfer.
func Discloses(covs []Covenant, peer ed25519.PublicKey, address string) (Covenant, bool) {
	for _, c := range covs {
		if c.For(peer) && c.Covers(address) {
			return c, true
		}
	}
	return Covenant{}, false
}

// DisclosesEvent is Discloses asked about one event rather than one address, so
// that a covenant naming a room is answered by the same call as the rest.
//
//	— T7.4, N4.8
func DisclosesEvent(covs []Covenant, peer ed25519.PublicKey, id frame.ID, address string) (Covenant, bool) {
	for _, c := range covs {
		if c.For(peer) && c.Reach().Covers(id, address) {
			return c, true
		}
	}
	return Covenant{}, false
}
