package daemon

import (
	"rokh/answer"
	"rokh/frame"
	"rokh/ledger"
	"rokh/receipt"
)

// ground is the real ledger behind answer.Ground.
//
// Every one of the four questions is answered by asking the ledger, and none of
// them is answered by asking the caller. That is the whole difference between
// a check and a formality: an answer that names its own authority is not
// checked by believing it.
//
//	— T2.1, T2.2, T2.3
type ground struct{ l *ledger.Ledger }

// Anchor is whose ledger this is: the genesis, which is the one name a ledger
// cannot have got from anywhere else.
//
//	— T2.3
func (g ground) Anchor() frame.ID { return g.l.Genesis() }

// Holds is accepted, not present. A pending event has ancestry that has not
// arrived, and a rejected one never was — citing either is citing nothing this
// ledger stands behind.
//
//	— T2.1, T3.3
func (g ground) Holds(id frame.ID) bool { return g.l.State(id) == ledger.Accepted }

// Grants asks whether the id is an authority still standing: granted somewhere
// in the causal past and not revoked since. An ordinary event named as an
// authority is not one, and this is where that stops being possible.
//
//	— T2.2, T6.1
func (g ground) Grants(id frame.ID) bool {
	for _, a := range g.l.ActiveGrants() {
		if a == id {
			return true
		}
	}
	return false
}

// Opened asks whether the id is a receipt's intent recorded here — an event
// this ledger holds, whose verb and payload type say it is one.
//
//	— T2.2, T10.1, T10.6
func (g ground) Opened(id frame.ID) bool {
	if !g.Holds(id) {
		return false
	}
	s, ok := g.l.Get(id)
	if !ok {
		return false
	}
	verb, yes := receipt.Concerns(s.Event, "")
	return yes && verb == receipt.VerbIntent
}

var _ answer.Ground = ground{}
