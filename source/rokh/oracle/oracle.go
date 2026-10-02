// Package oracle gathers the witnesses present when an event is made.
//
// An oracle is something that can observe one thing at one moment and testify
// to it. The testimony goes inside the event, and the hash and signature
// cover it.
//
// The hard line, restated here because the whole value of this package rests
// on it:
//
//	The signature proves the author *said* the oracle reported this.
//	Not that the report is true.
//
// So "clock" is this machine's claim about the time, not the world's time. An
// oracle that fails simply does not testify and the event is made without it;
// a missing optional witness is not an error.
//
// # Two rules that keep testimony small and harmless
//
// Small. Testimony is a stamp, not cargo. Every byte of it travels over the
// narrowest link we intend to support, so claims are raw bytes rather than
// hex text, and nothing the event already carries in a field is testified
// again.
//
// Quiet. Whatever goes into testimony is seen by anyone who ever receives the
// event, permanently. So the machine name and any other environmental mark is
// *not* a default; whoever wants it adds it explicitly.
package oracle

import (
	"crypto/rand"
	"fmt"
	"os"
	"runtime"
	"time"

	"rokh/event"
)

// Oracle is one witness.
type Oracle interface {
	Name() string
	Observe() ([]byte, error)
}

// Func is a simple oracle built from a function.
type Func struct {
	N string
	F func() ([]byte, error)
}

func (f Func) Name() string             { return f.N }
func (f Func) Observe() ([]byte, error) { return f.F() }

// Clock is this machine's wall clock, in UTC seconds, RFC 3339.
//
// It creates no ordering; it only claims. Sub-second precision is
// deliberately absent: nothing derives ordering from it and nothing relies on
// it, but it would still cost bytes.
func Clock() Oracle {
	return Func{N: "clock", F: func() ([]byte, error) {
		return []byte(time.Now().UTC().Format(time.RFC3339)), nil
	}}
}

// A clock in Rokh is testimony inside an event, never an ordering. Rokh
// knows what came before what; it does not know Tuesday, unless somebody
// testified and you trust that witness.
//
//	— N6.4
//
// Chance is fresh randomness — sixteen raw bytes.
//
// Why it is needed: events are content-addressed, so two events with the same
// content and parents get the same id and *are* one event. But a person may
// deliberately write the same thing twice and mean two separate events.
// Chance is what separates "twice, separately" from "once, repeated".
//
// Sixteen bytes (128 bits) is ample for occasion uniqueness; thirty-two was
// just cargo.
func Chance() Oracle {
	return Func{N: "chance", F: func() ([]byte, error) {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		return b, nil
	}}
}

// Host is the machine name and architecture.
//
// **Not a default, and that is deliberate.** A host name is identifying data
// and anyone who ever receives the event sees it forever. Adding it is a
// deliberate act by the ledger's owner, not a tool's default.
func Host() Oracle {
	return Func{N: "host", F: func() ([]byte, error) {
		h, err := os.Hostname()
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("%s %s/%s", h, runtime.GOOS, runtime.GOARCH)), nil
	}}
}

// Default is the oracle set that stamps an ordinary event.
//
// The carrier anchor is not here: the event already has a carrier field, and
// testifying to it again would be sixty-four repeated bytes carrying not one
// bit of new information.
func Default() []Oracle { return []Oracle{Clock(), Chance()} }

// ByName resolves oracle names for a caller that takes them from a user.
func ByName(name string) (Oracle, bool) {
	switch name {
	case "clock":
		return Clock(), true
	case "chance":
		return Chance(), true
	case "host":
		return Host(), true
	}
	return nil, false
}

// Observe asks every oracle and builds the testimony. An oracle that cannot
// observe is named in errs and testifies to nothing.
//
// No sorting happens here: event.Sign enforces the canonical form, and doing
// it twice would only create a second place to diverge.
func Observe(os []Oracle) (att []event.Attestation, errs []error) {
	for _, o := range os {
		v, err := o.Observe()
		if err != nil {
			errs = append(errs, fmt.Errorf("oracle %s: %w", o.Name(), err))
			continue
		}
		if len(v) == 0 || len(v) > event.MaxClaim {
			errs = append(errs, fmt.Errorf("oracle %s: bad claim size (%d)", o.Name(), len(v)))
			continue
		}
		att = append(att, event.Attestation{Oracle: o.Name(), Claim: v})
	}
	return att, errs
}
