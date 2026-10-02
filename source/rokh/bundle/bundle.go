// Package bundle is the sender's side of peering: it decides what may cross.
//
// It is not core. It reads a ledger and writes nothing. The four message kinds
// here answer one question, the one docs/07 section 5 states:
//
//	After named head H, and within scope S, which bytes are held, wanted,
//	offered, or acknowledged?
//
// All four travel over a wide link as JSON. **None of them ever goes over
// radio**; the narrow path carries an announcement and a knock, nothing else.
//
// # Where enforcement actually happens
//
// A courier is untrusted and keyless, so it cannot enforce anything. Disclosure
// is enforced *here*, on the sender's side, at bundling time: this package
// consults the live covenants and hands the courier only what it may carry.
// That is the same rule as docs/04 - scoping happens at bundling time, not at
// sending time.
package bundle

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"rokh/covenant"
	"rokh/frame"
	"rokh/ledger"
)

// Message kinds.
const (
	KindHave  = "have"
	KindWant  = "want"
	KindOffer = "offer"
	KindAck   = "ack"
)

// Have says: after H, within S, I hold these ids.
//
// After is a head the sender believes the receiver already has, so the list is
// a difference rather than a census. It may be the anchor, meaning "from the
// beginning".
type Have struct {
	Kind   string   `json:"kind"`
	Ledger string   `json:"ledger"`
	After  string   `json:"after,omitempty"`
	Scope  string   `json:"scope,omitempty"`
	IDs    []string `json:"ids"`
}

// Want says: after H, within S, send me these.
//
// A want is a request, never an entitlement. The sender still consults its
// covenants before answering, and may answer with nothing.
type Want struct {
	Kind   string   `json:"kind"`
	Ledger string   `json:"ledger"`
	After  string   `json:"after,omitempty"`
	Scope  string   `json:"scope,omitempty"`
	IDs    []string `json:"ids"`
}

// Item is one offered event and its size, so a receiver can decide before
// spending a metered link.
type Item struct {
	ID    string `json:"id"`
	Bytes int    `json:"bytes"`
}

// Offer says: I can supply these, at these sizes.
type Offer struct {
	Kind   string `json:"kind"`
	Ledger string `json:"ledger"`
	After  string `json:"after,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Items  []Item `json:"items"`
}

// Ack says: I judged these; here is what happened.
//
// The three outcomes are exactly the ledger's three verdicts and carry their
// settled meanings. Pending means the ancestry has not arrived, not that the
// event is bad: a courier seeing pending should fetch ancestors, not give up.
type Ack struct {
	Kind     string   `json:"kind"`
	Ledger   string   `json:"ledger"`
	Accepted []string `json:"accepted"`
	Rejected []string `json:"rejected"`
	Pending  []string `json:"pending"`
}

var ErrNoCovenant = errors.New("bundle: no live covenant permits this peer")

// Selection is what a bundle would carry, and what it would leave behind.
type Selection struct {
	// IDs may cross, in the ledger's deterministic order.
	IDs []frame.ID
	// Missing are ancestors of IDs that no covenant permits. Without them the
	// receiver will hold the corresponding events as pending forever.
	//
	// This is a real, unresolved tension and it is reported rather than hidden:
	// a scope like "home/journal" does not cover the authority chain at "rokh",
	// yet nothing verifies without it. See docs/07 section 11.
	Missing []frame.ID
}

// Disclosable decides what may cross to a peer.
//
// after may be zero, meaning "from the beginning". scope narrows further than
// the covenants do; it never widens them.
//
// withAncestry includes every accepted ancestor of a permitted event, whatever
// the covenant says. It is off by default because it discloses more than the
// covenant names, and turning it on is a decision, not a convenience.
// What may be disclosed is decided when the bundle is sealed, not when it
// is sent: the courier holds no key and is not trusted, so what must not
// travel never reaches it. An unknown peer gets nothing. And withholding a
// past the receiver needs suspends their judgement rather than refuting
// it.
//
//	— T7.2, T7.4, T7.6, N4.9
func Disclosable(l *ledger.Ledger, peer ed25519.PublicKey, after frame.ID, scope string,
	withAncestry bool) (Selection, error) {
	var sel Selection
	if len(peer) != ed25519.PublicKeySize {
		return sel, fmt.Errorf("bundle: peer key size (%d)", len(peer))
	}
	// Narrow to the covenants that name *this* peer first. Whether some other
	// peer has a covenant is beside the point: for this recipient, having none
	// is a refusal, not an empty answer.
	var covs []covenant.Covenant
	for _, c := range covenant.ActiveAt(l) {
		if c.For(peer) {
			covs = append(covs, c)
		}
	}
	if len(covs) == 0 {
		return sel, ErrNoCovenant
	}

	// Everything the receiver is assumed to have already.
	known := map[frame.ID]bool{}
	if !after.IsZero() {
		for _, id := range l.CausalPast(after) {
			known[id] = true
		}
	}

	permitted := map[frame.ID]bool{}
	for _, id := range l.Order() {
		if known[id] {
			continue
		}
		e, found := l.Get(id)
		if !found {
			continue
		}
		if scope != "" && !covenant.Covers(scope, e.Event.Address) {
			continue
		}
		if _, ok := covenant.DisclosesEvent(covs, peer, id, e.Event.Address); !ok {
			continue
		}
		permitted[id] = true
	}

	if withAncestry {
		var seeds []frame.ID
		for id := range permitted {
			seeds = append(seeds, id)
		}
		for _, id := range l.CausalPast(seeds...) {
			if !known[id] {
				permitted[id] = true
			}
		}
	}

	// Report what is still missing, so the sender sees the consequence.
	missing := map[frame.ID]bool{}
	for id := range permitted {
		e, _ := l.Get(id)
		for _, p := range e.Event.Parents {
			if !permitted[p] && !known[p] {
				missing[p] = true
			}
		}
		if a := e.Event.Authority; a != nil && !permitted[*a] && !known[*a] {
			missing[*a] = true
		}
	}

	for _, id := range l.Order() {
		if permitted[id] {
			sel.IDs = append(sel.IDs, id)
		}
		if missing[id] {
			sel.Missing = append(sel.Missing, id)
		}
	}
	return sel, nil
}
