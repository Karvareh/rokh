package shell

import (
	"strings"

	"rokh/event"
	"rokh/ledger"
)

// System events, said as sentences.
//
// The ledger's own events (its making, keys, grants, seeds, merges) sit at
// the address rokh and carry the mark system on every surface, in the same
// line format as any event (contract 4.1). Their payloads are the core's
// binary records; a person is told what each one did, decoded, and never
// shown its bytes.

// systemLine says what a system event did, from its decoded payload.
func systemLine(e event.Signed) string {
	p := e.Event.Payload
	switch e.Event.Verb {
	case event.VerbGenesis:
		return "made: " + genesisLine(p)
	case event.VerbMerge:
		return "two heads met here; neither was erased"
	case event.VerbKeyring:
		k, err := event.DecodeKeyring(p)
		if err != nil {
			break
		}
		who := "the owner's key"
		if !k.IsOwner() {
			who = "key " + quoteName(k.Name)
		}
		if k.Op == event.KeyringRevoke {
			return who + ", generation " + digits(int(k.Gen)) + ", taken back (it was added in " + k.Target.Short() + ")"
		}
		line := who + " added, generation " + digits(int(k.Gen))
		if !k.IsOwner() {
			line += ", " + readsOf(k.Reads)
			if len(k.Signer) > 0 {
				line += ", may sign"
			}
		}
		return line
	case event.VerbGrant:
		g, err := event.DecodeGrant(p)
		if err != nil {
			break
		}
		if g.Open {
			line := "an open address: any key may write at " + scopeName(g.Scope)
			if g.Read {
				line += ", and every key may read it"
			}
			return line
		}
		return "writing entrusted to " + shortKey(g.Subject) + " at " + scopeName(g.Scope)
	case event.VerbRevoke:
		r, err := event.DecodeRevoke(p)
		if err != nil {
			break
		}
		return "grant " + r.Target.Short() + " taken back, from here on"
	case event.VerbSeed:
		sd, err := event.DecodeSeed(p)
		if err != nil {
			break
		}
		if sd.Op == event.SeedTake {
			return "seed taken (given in " + sd.Give.Short() + ")"
		}
		where := "the whole ledger"
		if len(sd.Scopes) > 0 {
			where = strings.Join(sd.Scopes, ", ")
		}
		return "seed given for " + where
	default:
		return "a system event, " + e.Event.Verb
	}
	return "a " + e.Event.Verb + " whose record cannot be read here"
}

// quoteName is a key's name as the keyring holds it, safe to print.
func quoteName(name string) string {
	if name == "" {
		return "(unnamed)"
	}
	return `"` + escapePayload([]byte(name)) + `"`
}

// readsOf says where a key may read.
func readsOf(reads []string) string {
	switch {
	case len(reads) == 0:
		return "reads nothing"
	case len(reads) == 1 && reads[0] == "":
		return "reads everything"
	}
	shown := make([]string, len(reads))
	for i, r := range reads {
		shown[i] = escapePayload([]byte(r))
	}
	return "reads " + strings.Join(shown, ", ")
}

// eventCount is how many events a ledger holds, the person's and its own
// apart: "12 events and 2 system events". Both surfaces count this way.
func eventCount(led *ledger.Ledger) string {
	mine, system := 0, 0
	for _, id := range led.Order() {
		if led.State(id) != ledger.Accepted {
			continue
		}
		if e, ok := led.Get(id); ok && e.System {
			system++
		} else {
			mine++
		}
	}
	return countOf(mine, "event") + " and " + countOf(system, "system event")
}

// systemCount is the accepted system events of a ledger.
func systemCount(led *ledger.Ledger) int {
	n := 0
	for _, id := range led.Order() {
		if e, ok := led.Get(id); ok && e.System && led.State(id) == ledger.Accepted {
			n++
		}
	}
	return n
}
