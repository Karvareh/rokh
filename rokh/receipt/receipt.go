// Package receipt is the unit of work a harness does on Rokh.
//
// It is not part of Rokh. Rokh knows events and nothing else; the receipt is
// a custom the harnesses keep on top of it, and this package sits above the
// core for exactly that reason — nothing below it may import it.
//
//	— T10, T10.5
package receipt

import (
	"errors"
	"fmt"
	"strings"

	"rokh/event"
	"rokh/frame"
)

// Addresses and verbs of the two events. A harness recognises a receipt the
// way it recognises anything else: by address, verb and payload type — never
// by the name, which is an unreadable hash.
//
//	— T10.6, T3.2
const (
	VerbIntent  = "intent"
	VerbOutcome = "outcome"
)

// The declared type of each half's payload. It is written inside the payload,
// so it is inside the bytes the signature covers and inside the bytes the name
// is the hash of: a reader that trusts the event trusts the type on the same
// evidence, and there is nowhere else to put a claim about what the payload is
// that the signature would not cover.
//
// A verb alone is not enough. Two customs may both say "intent" and mean
// different things; the type is what distinguishes them, and reading it is how
// a harness recognises its own without ever looking at the name.
//
//	— T10.6, T3.2
const (
	TypeIntent = "rokh.receipt.intent.v1"
	TypeResult = "rokh.receipt.result.v1"
)

// Outcome is how a receipt closes. Three endings, and the third is not a
// softer version of the second.
//
//	— T10.3, T10.4
type Outcome string

const (
	// Done: the step happened.
	Done Outcome = "done"
	// Failed: the step did not happen, and that is known. Failure has a
	// receipt too.
	//   — T10.3
	Failed Outcome = "failed"
	// Unknown: the end of the receipt was lost. Not a victory, not a defeat —
	// "I do not know", said out loud.
	//
	// It is reserved for exactly this. It is not the "incomplete" of a
	// measurement, which is an epistemic interval with bounds; this one may
	// have no bounds at all.
	//   — T10.4
	Unknown Outcome = "unknown"
)

func (o Outcome) valid() bool { return o == Done || o == Failed || o == Unknown }

// Witness is the six things every effect must be able to show. A harness that
// cannot fill all six has not finished the work — it has only done it.
//
//	— T10.2
type Witness struct {
	Origin    string `json:"origin"`    // where the request came from
	Authority string `json:"authority"` // the grant it was done under
	Audience  string `json:"audience"`  // who it was for
	Receipt   string `json:"receipt"`   // the name of the intent it answers
	State     string `json:"state"`     // where the thing stands now
	WayBack   string `json:"wayBack"`   // how to undo or appeal it
}

// Missing names the witnesses that are absent, so a harness can say what it
// is short of rather than quietly showing five.
func (w Witness) Missing() []string { return w.missing(true) }

// MissingForIntent is the same check minus the receipt, and the exception is
// not a loophole. An intent cannot name its own receipt: its name is the hash
// of bytes that include this very field, so a receipt written into it would
// have to be known before it existed. The intent declares the other five; the
// result, which is where the effect actually lands, owes all six.
//
//	— T10.2, T3.2
func (w Witness) MissingForIntent() []string { return w.missing(false) }

func (w Witness) missing(withReceipt bool) []string {
	fields := map[string]string{
		"origin": w.Origin, "authority": w.Authority, "audience": w.Audience,
		"state": w.State, "wayBack": w.WayBack,
	}
	if withReceipt {
		fields["receipt"] = w.Receipt
	}
	var out []string
	for name, v := range fields {
		if strings.TrimSpace(v) == "" {
			out = append(out, name)
		}
	}
	sortStrings(out)
	return out
}

// Intent is written before the step: what is about to be done.
//
//	— T10.1
//
// The unit of work a harness does on Rokh is the receipt: an intent before
// the step and a result after it.
//
//	— T10
type Intent struct {
	// Type is the declared payload type. OpenIntent writes it; nothing else
	// needs to, and a payload that arrives without it is not an intent.
	//   — T10.6
	Type    string  `json:"type"`
	Doing   string  `json:"doing"`
	Witness Witness `json:"witness"`
}

// Result is written after the step, and names the intent as its parent. The
// parent link is the whole of the pairing: without it there is a claim about
// an outcome with no question in front of it.
//
//	— T10.1
type Result struct {
	// Type is the declared payload type, written by CloseWith.
	//   — T10.6
	Type    string  `json:"type"`
	Outcome Outcome `json:"outcome"`
	Saying  string  `json:"saying"`
	Witness Witness `json:"witness"`
	// Once is a handle the destination may use to refuse a repeat. Rokh
	// alone does not promise "exactly once" for an effect that lands outside
	// it: there is no shared transaction and the ledger does not rule the
	// destination. A destination that cooperates can make the effect
	// idempotent by this handle; one that does not, cannot be made to.
	//   — T10.7
	Once string `json:"once,omitempty"`
}

var (
	// ErrNotAReceipt is returned for an event that is not part of one.
	ErrNotAReceipt = errors.New("receipt: not a receipt event")
	// ErrIncompleteWitness is returned when any of the six is missing.
	ErrIncompleteWitness = errors.New("receipt: an effect must show all six witnesses")
	// ErrOrphanResult is returned for a result that does not name its intent.
	ErrOrphanResult = errors.New("receipt: a result must name the intent it answers")
)

// Concerns reports whether an event belongs to this custom, and which half of
// a receipt it is. It reads three things — address, verb and the type declared
// inside the payload — and never the name.
//
// All three must agree. An event whose verb says "intent" over a payload that
// declares something else is not a malformed intent; it is not one, and this
// says so rather than guessing which of the two to believe.
//
// The type is read by its exact key, not by Go's case-insensitive field
// matching: a payload spelled {"TYPE":…} is a different byte string and a
// different event name, and reading the two as one would be the very thing
// "one content, one encoding" forbids. Whether the payload is the *one*
// encoding of a whole receipt is Read's question, not this one — this decides
// whose business the event is, and a reader decides that before it is willing
// to parse the rest.
//
// Saying no is not rejection. Rejection belongs to the ledger and means the
// event never was; this only declines to pass a foreign event through the
// harness's own aperture, and the event stays exactly as recorded.
//
//	— T10.6, T3.2, T3.3, N4.1
func Concerns(e event.Event, scope string) (verb string, yes bool) {
	if scope != "" && !within(e.Address, scope) {
		return "", false
	}
	var want string
	switch e.Verb {
	case VerbIntent:
		want = TypeIntent
	case VerbOutcome:
		want = TypeResult
	default:
		return "", false
	}
	if PayloadType(e.Payload) != want {
		return "", false
	}
	return e.Verb, true
}

func within(addr, scope string) bool {
	return addr == scope || strings.HasPrefix(addr, scope+"/")
}

// OpenIntent builds the first event of a receipt.
//
//	— T10.1, T10.2
func OpenIntent(addr string, in Intent) (event.Event, error) {
	in.Type = TypeIntent
	if m := in.Witness.MissingForIntent(); len(m) > 0 {
		return event.Event{}, fmt.Errorf("%w: %s", ErrIncompleteWitness, strings.Join(m, ", "))
	}
	b, err := encode(in)
	if err != nil {
		return event.Event{}, err
	}
	return event.Event{Address: addr, Verb: VerbIntent, Payload: b}, nil
}

// CloseWith builds the second event, which names the intent as its parent.
// Every ending goes through here, including failure and including the one
// that was lost.
//
//	— T10.1, T10.3, T10.4
func CloseWith(addr string, intent frame.ID, r Result) (event.Event, error) {
	r.Type = TypeResult
	if !r.Outcome.valid() {
		return event.Event{}, fmt.Errorf("%w: outcome %q", ErrNotAReceipt, r.Outcome)
	}
	if intent.IsZero() {
		return event.Event{}, ErrOrphanResult
	}
	if m := r.Witness.Missing(); len(m) > 0 {
		return event.Event{}, fmt.Errorf("%w: %s", ErrIncompleteWitness, strings.Join(m, ", "))
	}
	if r.Witness.Receipt != intent.String() {
		return event.Event{}, fmt.Errorf("%w: witness names %q, parent is %s",
			ErrOrphanResult, r.Witness.Receipt, intent)
	}
	b, err := encode(r)
	if err != nil {
		return event.Event{}, err
	}
	return event.Event{
		Address: addr, Verb: VerbOutcome,
		Parents: []frame.ID{intent},
		Payload: b,
	}, nil
}

// Lost closes a receipt whose end was never learned. It exists so that a
// harness has somewhere honest to go: the alternative is guessing, and a
// guessed ending is worse than an admitted gap.
//
//	— T10.4
func Lost(addr string, intent frame.ID, w Witness, saying string) (event.Event, error) {
	w.Receipt = intent.String()
	if strings.TrimSpace(w.State) == "" {
		w.State = "unknown"
	}
	return CloseWith(addr, intent, Result{Outcome: Unknown, Saying: saying, Witness: w})
}
