// Package bond is the lasting knot between independent owners.
//
// A bond is not several ledgers placed side by side. It is a knot: each person
// keeps their own ledger and their own boundary, and undertakes work towards
// another such that both the doing and the abandoning leave receipts. An
// organisation is the named body of the work; a system is the machine that
// does and keeps it. Either can serve a knot, and neither alone brings one
// into being.
//
// An individual can only originate their own act. A two-sided relation, the
// keeping of a thing that stands between two people, and continuation after
// one of them is gone — none of those can be made alone.
//
//	— T11, T11.5
package bond

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rokh/canon"
	"rokh/frame"
)

// Kind separates the leaf that founds a bond from the leaf of one piece of
// work done inside it. Each shared work has its own leaf and its own name,
// apart from the bond's name.
//
//	— T11.7
type Kind string

const (
	Founding Kind = "founding" // the founding sheet
	Work     Kind = "work"     // a sheet of shared work
)

// Leaf is a unique byte string that says the founders' anchors and the work
// undertaken. Its name — like any event's — is the hash of exactly those
// bytes.
//
//	— T11.6, T3.2
//
// A bond is not several ledgers side by side; it is a lasting knot between
// independent owners, and each person brings their own anchor.
//
//	— T11, T11.5
type Leaf struct {
	Kind     Kind       `json:"kind"`
	Founders []frame.ID `json:"founders"`
	Doing    string     `json:"doing"`
	// Of names the bond a work leaf belongs to. A founding leaf has none.
	Of *frame.ID `json:"of,omitempty"`
}

var (
	// ErrAlone is returned for a bond with fewer than two independent owners.
	ErrAlone = errors.New("bond: a knot needs more than one owner")
	// ErrSharedRoot is returned when one anchor is offered twice, which is
	// the shape of a shared root key rather than a bond.
	ErrSharedRoot = errors.New("bond: each person brings their own anchor")
	// ErrShape is returned for a leaf that is not well formed.
	ErrShape = errors.New("bond: bad leaf")
)

// Encode produces the leaf's bytes, in one canonical form: the founders are
// sorted, so the same knot between the same people always has the same name
// no matter who writes it down.
//
//	— T11.6, N4.1
func (l Leaf) Encode() ([]byte, error) {
	if l.Kind != Founding && l.Kind != Work {
		return nil, fmt.Errorf("%w: kind %q", ErrShape, l.Kind)
	}
	if strings.TrimSpace(l.Doing) == "" {
		return nil, fmt.Errorf("%w: a leaf must say what is undertaken", ErrShape)
	}
	if len(l.Founders) < 2 {
		return nil, ErrAlone
	}
	seen := map[frame.ID]bool{}
	fs := append([]frame.ID(nil), l.Founders...)
	for _, f := range fs {
		if f.IsZero() {
			return nil, fmt.Errorf("%w: an empty anchor", ErrShape)
		}
		if seen[f] {
			return nil, ErrSharedRoot
		}
		seen[f] = true
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].Compare(fs[j]) < 0 })
	if l.Kind == Work && (l.Of == nil || l.Of.IsZero()) {
		return nil, fmt.Errorf("%w: a work leaf names the bond it belongs to", ErrShape)
	}
	if l.Kind == Founding && l.Of != nil {
		return nil, fmt.Errorf("%w: a founding leaf belongs to no other", ErrShape)
	}
	c := l
	c.Founders = fs
	return canon.Marshal(c)
}

// Name is the leaf's name: the hash of its own bytes, like everything else.
//
//	— T11.6, T3.2
func Name(b []byte) frame.ID { return frame.Hash(b) }

// Parse reads a leaf back and refuses a second spelling of the same content.
//
//	— N4.1
func Parse(b []byte) (Leaf, error) {
	var l Leaf
	if err := json.Unmarshal(b, &l); err != nil {
		return l, fmt.Errorf("%w: %v", ErrShape, err)
	}
	re, err := l.Encode()
	if err != nil {
		return l, err
	}
	if !bytes.Equal(re, b) {
		return l, fmt.Errorf("%w: non-canonical encoding", ErrShape)
	}
	return l, nil
}

// Act is one of the five, and they are five separate things. Collapsing any
// two of them loses something a person needs to be able to do on its own.
//
//	— T11.7
type Act string

const (
	Join   Act = "join"   // joining
	Leave  Act = "leave"  // leaving
	Amend  Act = "amend"  // amending the sheet
	Return Act = "return" // returning an item
	Settle Act = "settle" // settling the account
)

// Acts lists the five, in the order they are named in the specification.
func Acts() []Act { return []Act{Join, Leave, Amend, Return, Settle} }

// Leaving closes the future. It does not clear an open debt, and it is not
// settling: that is a separate act, and this is why the two are not one.
//
//	— T11.7
func (a Act) ClearsDebt() bool { return a == Settle }

// Standing reports how a leaf stands: who has accepted it in their own ledger
// and who has not. A leaf is closed when everyone who deemed it necessary has
// referred to that same name in their *own* ledger — and that proves mutual
// acceptance and nothing more. It is not an exclusive effect on anything
// outside those ledgers.
//
// There is no function here that merges two ledgers, and there will not be:
// the ledgers never become one.
//
//	— T11.6, T11.7
func Standing(l Leaf, acceptedBy []frame.ID) (missing []frame.ID, closed bool) {
	have := map[frame.ID]bool{}
	for _, a := range acceptedBy {
		have[a] = true
	}
	for _, f := range l.Founders {
		if !have[f] {
			missing = append(missing, f)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Compare(missing[j]) < 0 })
	return missing, len(missing) == 0
}
