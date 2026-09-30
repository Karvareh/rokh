// Package ledger holds the ledger and decides authority.
//
// # The central rule: authority is judged in the causal past, not the whole ledger
//
// The earlier implementation judged a right like this: "a key has the right
// if a grant exists for it and no revocation exists *anywhere in the
// ledger*." The consequence was that a late-arriving revocation could
// *shrink* the accepted set: adding an event killed another, already
// accepted, event. Acceptance was not monotonic, and that is what left the
// "revocation on a concurrent branch" question open.
//
// Here it is inverted:
//
//	event e has the right if, in *e's own causal past*, there is a grant
//	that has not been revoked.
//
// e's causal past is fixed the moment e is signed and never changes. So:
//
//   - every event's verdict is settled at creation and never reverses when
//     new events arrive;
//   - adding can only turn "pending" into "accepted" or "rejected", never
//     "accepted" into "rejected";
//   - revocation means exactly what it should: from here on, with no bearing
//     on the past.
//
// The price is that a partitioned node which never saw the revocation keeps
// producing valid events on its own branch. That is correct: those events
// really were written before it knew. At a merge the revocation is in the
// merge point's past, so from there on the grant is dead. Contradictory
// accounts sit side by side; the core does not flatten them into a
// manufactured truth.
package ledger

import (
	"bytes"
	"container/heap"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sort"
	"sync"

	"rokh/event"
	"rokh/frame"
)

// State is an event's verdict. Accepted and Rejected are final.
type State uint8

const (
	Pending  State = iota // ancestry incomplete: not arrived is not the same as not existing
	Accepted              // valid, permanently
	Rejected              // invalid, permanently
)

func (s State) String() string {
	switch s {
	case Accepted:
		return "accepted"
	case Rejected:
		return "rejected"
	}
	return "pending"
}

var ErrNotGenesis = errors.New("ledger: not a genesis event")

// Level is how far an accepted event was judged (contract 3.3).
//
//   - Full: the head and the body of the event and of every ancestor were
//     verified, and every rule was applied. The owner's judgement.
//   - Lineage: the event's own head, body and signature were verified; every
//     ancestor's head was verified by hash link and signature; every system
//     event in its causal past was judged in full; its own grant was live,
//     unrevoked, made to its author (or open) and covering what it did. Not
//     judged: whether an ancestor whose body was not here obeyed its grant.
//
// Pending is not a level: a head that is missing, that does not hash to its
// name, or a system event that is needed and absent leaves the event pending,
// and nothing pending authorizes anything (C8).
type Level uint8

const (
	Full    Level = iota + 1 // the ledger's own judgement
	Lineage                  // heads proven, a body not opened somewhere in the past
)

func (v Level) String() string {
	switch v {
	case Full:
		return "full"
	case Lineage:
		return "lineage"
	}
	// A name that is not accepted has no level: its state says what it is
	// (R2), and "rejected (pending)" is not a thing to print.
	return ""
}

// Ledger is one person's ledger.
type Ledger struct {
	genesis frame.ID
	root    ed25519.PublicKey

	events   map[frame.ID]event.Signed
	state    map[frame.ID]State
	children map[frame.ID][]frame.ID

	// Authority state at each node. Both sets are monotone and only grow.
	grants  map[frame.ID]idset // accepted grants in the causal past, plus self
	revoked map[frame.ID]idset // revoked grants in the causal past, plus self
	// system is the accepted keyring and seed events in the causal past, plus
	// self; unkeyed is the keyring adds taken back in the causal past. Both
	// only grow, so live(P) = system adds \ unkeyed is a fold of the past
	// and nothing else (contract 4.4).
	system  map[frame.ID]idset
	unkeyed map[frame.ID]idset
	// level is how far each accepted event was judged.
	level map[frame.ID]Level
	// unproven is every name the references reached whose head could not be
	// proven, and why.
	unproven map[frame.ID]string
	// overturned is what the last body that overturned its head took with it:
	// the names that had been accepted and are refused now.
	overturned []frame.ID

	// pool interns authority sets so they are shared between events. Its
	// size grows with the number of *distinct authority states*, not with
	// the number of events, and since grants and revocations are few it
	// stays small in practice. It is never emptied; it lives as long as this
	// in-memory ledger does.
	pool interner

	// Views kept as the ledger grows, so that asking for the heads, the
	// tally or the order does not walk every event each time. None of them
	// is truth: each is rebuilt from the events above whenever it is lost,
	// and nothing here is ever written to the carrier.
	//
	//   - heads is the set Heads returns, maintained as events are accepted;
	//   - tally counts the three verdicts;
	//   - order is Order's answer, cached until the next accepted event
	//     changes it (nil means "compute again"), and pos is each accepted
	//     event's index in it.
	heads map[frame.ID]struct{}
	tally [3]int
	order []frame.ID
	pos   map[frame.ID]int
	// reasons says, for each name refused, which check failed — for the
	// person who offered the bytes, not for the verdict, which needs no
	// reason to be final. Nothing of the event itself is kept beside it.
	reasons map[frame.ID]string
	// viewMu guards filling the cached order. Adding to the ledger is one
	// writer's work and is not guarded here; but a door lets many readers
	// look at once, and two of them must not fill the cache together.
	viewMu sync.Mutex
}

// New builds a ledger from the bytes of a genesis event.
func New(raw []byte) (*Ledger, error) {
	g, err := event.Parse(raw)
	if err != nil {
		return nil, err
	}
	if !g.IsGenesis() || g.Event.Verb != event.VerbGenesis {
		return nil, ErrNotGenesis
	}
	l := &Ledger{
		genesis:  g.ID,
		root:     g.Event.Author,
		events:   map[frame.ID]event.Signed{},
		state:    map[frame.ID]State{},
		children: map[frame.ID][]frame.ID{},
		grants:   map[frame.ID]idset{},
		revoked:  map[frame.ID]idset{},
		system:   map[frame.ID]idset{},
		unkeyed:  map[frame.ID]idset{},
		level:    map[frame.ID]Level{},
		unproven: map[frame.ID]string{},
		pool:     interner{},
		heads:    map[frame.ID]struct{}{},
		reasons:  map[frame.ID]string{},
	}
	if _, err := l.add(g); err != nil {
		return nil, err
	}
	if l.state[g.ID] != Accepted {
		return nil, fmt.Errorf("ledger: genesis was not accepted")
	}
	return l, nil
}

func (l *Ledger) Genesis() frame.ID { return l.genesis }

// Root returns a copy of the root key, so a caller cannot reach into the
// ledger's own state.
func (l *Ledger) Root() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), l.root...)
}

func (l *Ledger) Len() int                { return len(l.events) }
func (l *Ledger) State(id frame.ID) State { return l.state[id] }
func (l *Ledger) Has(id frame.ID) bool    { _, ok := l.events[id]; return ok }

func (l *Ledger) Get(id frame.ID) (event.Signed, bool) {
	e, ok := l.events[id]
	return e, ok
}

// Add takes the bytes of one event and returns its verdict. Adding the same
// event again has no effect (idempotence: name is hash).
//
// The input is deliberately *bytes*, not a parsed struct. The ledger trusts
// nothing but bytes: every input goes through event.Parse, so the canonical
// form and the signature are checked right here and no caller can hand the
// ledger a hand-built event.
// ErrNameCollision is returned when two different byte strings arrive under
// one full name. Seeing the collision is settled; what to do about it beyond
// refusing the second is not.
//
//	— T3.5, T13.8
var ErrNameCollision = errors.New("ledger: two different byte strings under one name")

// ErrAlreadyRejected is returned for a name this ledger has already refused.
// A settled verdict does not reopen, so the second offer is not a new question.
//
//	— T3.3, T6.4
var ErrAlreadyRejected = errors.New("ledger: that name was refused; it never was")

func (l *Ledger) Add(raw []byte) (State, error) {
	// Rokh trusts nothing but bytes: every input goes through Parse, and a
	// rejected event is not stored. Rejection means "never was", not
	// "exists but bad": nothing about it enters the ledger.
	//
	// And note what adding is: an event is *added*, never spent. That is why
	// the double-spend problem does not arise in one person's ledger, and why
	// two copies of it never compete — a structural result, not a claim.
	//   — T3, T3.3, N2.3, N2.4, N2.7, N2.12, N9.1
	s, err := event.Parse(raw)
	if err != nil {
		return Rejected, err
	}
	return l.add(s)
}

// add is Add after the parse: the one place an event enters the maps. Load
// and Extend come through here with what they parsed on their walk, so an
// object read once is verified once; nothing arrives here that did not pass
// event.Parse.
func (l *Ledger) add(s event.Signed) (State, error) {
	if l.state[s.ID] == Rejected {
		// A name already refused stays refused. Re-offering the bytes does
		// not get them a second hearing, and offering *different* bytes under
		// that name does not get the name reused.
		//   — T3.3, T6.4, N-Axiom2
		return Rejected, fmt.Errorf("%w: %s", ErrAlreadyRejected, s.ID.Short())
	}
	if held, ok := l.events[s.ID]; ok {
		// A head held alone and its body arriving later are one event: the
		// name is the head's hash, and the body is the one the head names. The
		// body is kept; a pending event is judged again. An event accepted on
		// its head alone keeps its level: a settled verdict does not reopen.
		if held.HeadOnly && !s.HeadOnly && bytes.Equal(held.Head, s.Head) {
			if l.state[s.ID] == Pending {
				l.events[s.ID] = s
				l.settle(s.ID)
				return l.state[s.ID], nil
			}
			return l.judgeBody(s)
		}
		if s.HeadOnly && bytes.Equal(held.Head, s.Head) {
			return l.state[s.ID], nil
		}
		// The same name arriving again is the ordinary case — a replay, a
		// re-sync — and it is a valid no-op, not an error. But "the same
		// name" is not "the same event" until the bytes say so.
		//   — T3.6
		if !bytes.Equal(held.Raw, s.Raw) {
			// Provisional guard while T13.8 is open: the ledger sees the
			// collision and refuses to hold both under one name.
			//   — T3.5, T13.8
			return Rejected, fmt.Errorf("%w: %s", ErrNameCollision, s.ID.Short())
		}
		return l.state[s.ID], nil
	}
	delete(l.unproven, s.ID)
	l.events[s.ID] = s
	l.state[s.ID] = Pending
	l.tally[Pending]++
	for _, p := range s.Event.Parents {
		l.children[p] = append(l.children[p], s.ID)
	}
	l.settle(s.ID)
	return l.state[s.ID], nil
}

// judgeBody judges the body of an event that was accepted from its head
// alone. Lineage judged the head: the author, the grant it names, the grant's
// standing in the causal past. It did not judge what the head's body does
// (U7). Now the body is here, so it is judged in full, exactly as it would
// have been had it arrived first: arrival order never authorizes a write.
//
// A body its grant covers is kept, and the event and everything resting on it
// is raised to Full where every ancestor now is. A body its grant does not
// cover is refused and not kept, and the event it belongs to is refused with
// it, and so is every event that rests on it — the same verdicts the ledger
// gives when the whole event comes first. The acceptance of the head was
// lineage's, and lineage never judged this; it is not reversed so much as
// completed.
func (l *Ledger) judgeBody(s event.Signed) (State, error) {
	head := l.events[s.ID]
	l.events[s.ID] = s
	v := l.evaluate(s.ID)
	if v.state == Accepted {
		l.level[s.ID] = v.level
		l.raise(s.ID)
		return Accepted, nil
	}
	l.events[s.ID] = head
	why := v.why
	if why == "" {
		why = "its body cannot be judged in its causal past"
	}
	l.refuse(s.ID, "its body, which arrived after its head, was judged: "+why)
	return Rejected, fmt.Errorf("ledger: %s: %s", s.ID.Short(), l.reasons[s.ID])
}

// Overturned lists the names the last overturning body refused that had been
// accepted: the event whose body it was, and everything that rested on it. A
// door answers the offer with them and drops them from its views (R1).
func (l *Ledger) Overturned() []frame.ID { return append([]frame.ID(nil), l.overturned...) }

// raise lifts to Full the accepted descendants of start whose every parent is
// now Full and whose own body is here. A level only rises.
func (l *Ledger) raise(start frame.ID) {
	if l.level[start] != Full {
		return
	}
	queue := append([]frame.ID(nil), l.children[start]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if l.state[id] != Accepted || l.level[id] == Full || l.events[id].HeadOnly {
			continue
		}
		full := true
		for _, p := range l.events[id].Event.Parents {
			if l.level[p] != Full {
				full = false
				break
			}
		}
		if !full {
			continue
		}
		l.level[id] = Full
		queue = append(queue, l.children[id]...)
	}
}

// refuse turns an event that its own body refused into what it would have
// been had the body come first: refused, never was, and every event resting
// on it refused because the chain stops at the break.
func (l *Ledger) refuse(start frame.ID, why string) {
	l.overturned = nil
	queue := []frame.ID{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		st, known := l.state[id]
		if !known || st == Rejected {
			continue
		}
		if st == Accepted {
			l.overturned = append(l.overturned, id)
		}
		l.tally[st]--
		l.tally[Rejected]++
		l.state[id] = Rejected
		if id == start {
			l.reasons[id] = why
		} else {
			l.reasons[id] = "it rests on " + start.Short() + ", which was refused when its body arrived; the chain stops at the break"
		}
		for _, m := range []map[frame.ID]idset{l.grants, l.revoked, l.system, l.unkeyed} {
			delete(m, id)
		}
		delete(l.level, id)
		delete(l.heads, id)
		if e, held := l.events[id]; held {
			queue = append(queue, l.children[id]...)
			for _, p := range e.Event.Parents {
				kids := l.children[p]
				for i, c := range kids {
					if c == id {
						l.children[p] = append(kids[:i:i], kids[i+1:]...)
						break
					}
				}
			}
			delete(l.children, id)
			delete(l.events, id)
		}
	}
	// The heads and the order are views; they are rebuilt from the verdicts.
	l.heads = map[frame.ID]struct{}{}
	for id, st := range l.state {
		if st != Accepted {
			continue
		}
		head := true
		for _, c := range l.children[id] {
			if l.state[c] == Accepted {
				head = false
				break
			}
		}
		if head {
			l.heads[id] = struct{}{}
		}
	}
	l.order, l.pos = nil, nil
}

// Forget drops an event whose verdict is not Accepted from this in-memory
// view: one still pending, or one refused.
//
// It is for the door that judged bytes it will not store. The verdict was
// given and answered; what the view must not do afterwards is go on holding
// an event the carrier does not hold, or grow without bound under a stream
// of offers that were never events. Nothing about the verdict changes: the
// same bytes offered again are judged again from the same causal past and
// reach the same answer (T6.4). An accepted event is never forgotten here —
// it is on the carrier, and only reading the carrier again may change the
// view of it.
//
// It reports whether anything was dropped.
//
//	— T3.3, T8.5
func (l *Ledger) Forget(id frame.ID) bool {
	st, known := l.state[id]
	if !known || st == Accepted {
		return false
	}
	if s, held := l.events[id]; held {
		for _, p := range s.Event.Parents {
			kids := l.children[p]
			for i, c := range kids {
				if c == id {
					l.children[p] = append(kids[:i:i], kids[i+1:]...)
					break
				}
			}
			if len(l.children[p]) == 0 {
				delete(l.children, p)
			}
		}
		delete(l.events, id)
	}
	delete(l.state, id)
	delete(l.reasons, id)
	l.tally[st]--
	return true
}

// Why says which check a refused name failed, in a sentence for the person
// who offered it. It is not part of the verdict — a verdict needs no reason
// to be final — and there is none for an accepted or a pending name.
//
//	— T3.3, T12.3
func (l *Ledger) Why(id frame.ID) (string, bool) {
	why, ok := l.reasons[id]
	return why, ok
}

// Judged says how far an event was judged: "full", "lineage" or "pending".
// A refused name and a name never seen are "pending" too: nothing about them
// authorizes anything.
func (l *Ledger) Judged(id frame.ID) Level {
	if l.state[id] != Accepted {
		return 0
	}
	return l.level[id]
}

// settle closes the verdict for this event and for any child that becomes
// decidable as a result.
// A settled verdict does not reopen. Adding can turn pending into accepted
// or rejected and never accepted into rejected: the ledger only adds, and
// an event does not come back.
//
//	— T6.4, N-Axiom2
func (l *Ledger) settle(start frame.ID) {
	queue := []frame.ID{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if l.state[id] != Pending {
			continue // a settled verdict does not reopen
		}
		v := l.evaluate(id)
		st := v.state
		if st == Pending {
			if v.why != "" {
				l.reasons[id] = v.why
			}
			continue
		}
		delete(l.reasons, id)
		l.state[id] = st
		l.tally[Pending]--
		l.tally[st]++
		if st == Rejected {
			l.reasons[id] = v.why
		}
		if st == Accepted {
			// The common event changes no authority: its sets are its
			// parent's own slices, interned already, and are kept as they
			// are. Only a set that is new here goes through the pool.
			l.grants[id] = l.intern(v.g, id, l.grants)
			l.revoked[id] = l.intern(v.r, id, l.revoked)
			l.system[id] = l.intern(v.sys, id, l.system)
			l.unkeyed[id] = l.intern(v.unkeyed, id, l.unkeyed)
			l.level[id] = v.level
			// It is a head until an accepted event claims it as a parent;
			// its accepted parents stop being heads now. No accepted child
			// can exist yet, since a child settles only after its parents.
			for _, p := range l.events[id].Event.Parents {
				delete(l.heads, p)
			}
			l.heads[id] = struct{}{}
			l.order, l.pos = nil, nil
		}
		queue = append(queue, l.children[id]...)
		if st == Rejected {
			// "Never was", and not only for input that failed to parse. An
			// event that parsed perfectly and was refused on lineage or
			// authority is dropped here too: the ledger keeps the verdict
			// against the name, which it must in order to refuse the
			// children, and keeps nothing of the event — not its bytes, not
			// its author, not its payload.
			//
			// The children are queued above, before the drop, and every later
			// reader of a parent's authority reads only accepted ones, so
			// nothing downstream needs what is going away. Its parents are
			// settled already — a child is judged only after them — so their
			// lists of children are not walked again for it, and it leaves
			// them too.
			//   — T3.3, T1
			for _, p := range l.events[id].Event.Parents {
				kids := l.children[p]
				for i, c := range kids {
					if c == id {
						l.children[p] = append(kids[:i:i], kids[i+1:]...)
						break
					}
				}
			}
			delete(l.events, id)
		}
	}
}

// evaluate judges one event from its causal past. The second and third
// results are the authority state after this event, valid only if accepted.
// Authority is judged only in the event's own causal past, which is fixed
// the moment it is signed. So a verdict never reverses when later events
// arrive, revocation closes the future without touching the past, and
// merging two copies of a ledger commutes: nobody has to vote on which
// branch won.
//
// The carrier check is here too: one ledger, one anchor, one owner. An
// event anchored elsewhere is not refused for being bad — it is refused
// for not belonging here.
//
// Note what this function never looks at: the payload. A verdict is about
// bytes, lineage and authority, so no degree in the content can become an
// acceptance, no plain lie is refused for being one, and there is nothing for
// a token or a fee to be spent on. Being right is not a vote, and one reader
// alone settles the whole ledger.
//
//	— T6.2, T6.3, T5.4, T8.4, T12, T12.1, T12.2, T12.5, T1.3, N7.4, N2.5, N-Axiom1, N-Axiom4, N-Axiom6, N-Axiom7
//
// verdict is what evaluate decides about one event: its state, and when it is
// accepted, the authority state after it and how far it was judged.
type verdict struct {
	state              State
	g, r, sys, unkeyed idset
	level              Level
	why                string
}

func pending(why string) verdict  { return verdict{state: Pending, why: why} }
func rejected(why string) verdict { return verdict{state: Rejected, why: why} }

func (l *Ledger) evaluate(id frame.ID) verdict {
	s := l.events[id]
	e := s.Event

	// Genesis is the only event without a carrier. A second genesis, or the
	// genesis of another ledger, is rejected.
	if e.Carrier == nil {
		if id != l.genesis {
			return rejected("a second genesis: this ledger has its anchor already")
		}
		if s.HeadOnly {
			return pending("the genesis is needed in full and only its head is here")
		}
		return verdict{state: Accepted, level: Full}
	}
	if *e.Carrier != l.genesis {
		return rejected("anchored to another ledger; it does not belong here")
	}

	var G, R, S, U idset
	level := Full
	for _, p := range e.Parents {
		st, known := l.state[p]
		if !known || st == Pending {
			// Not arrived is not the same as not existing, and a head that
			// is not here proves nothing (C8).
			return pending("the head of its parent " + p.Short() + " is not proven here")
		}
		if st == Rejected {
			return rejected("its parent " + p.Short() + " was refused; the chain stops at the break")
		}
		G = union(G, l.grants[p])
		R = union(R, l.revoked[p])
		S = union(S, l.system[p])
		U = union(U, l.unkeyed[p])
		if l.level[p] == Lineage {
			level = Lineage
		}
	}
	if s.HeadOnly {
		// A system event is needed in full: a grant, a revoke or a keyring
		// change whose body is not here cannot be judged, and whatever rests
		// on it waits (C8). A hidden revoke is exactly this.
		if s.System {
			return pending("a system event is needed in full and only its head is here")
		}
		level = Lineage
	}

	var mine *event.Grant // the grant this event's authority comes from
	if bytes.Equal(e.Author, l.root) {
		// Root neither takes nor needs external authority; the canonical
		// form enforces that there is only one way to write it.
		if e.Authority != nil {
			return rejected("the root key names an authority; the root writes under none")
		}
	} else {
		if e.Authority == nil {
			return rejected("a key other than the root names no grant to write under")
		}
		d := *e.Authority
		if !G.contains(d) {
			// Either not a grant, or not in this event's causal past.
			return rejected("the grant " + d.Short() + " it names is not in its causal past")
		}
		if R.contains(d) {
			// Already revoked before this event.
			return rejected("the grant " + d.Short() + " it names was taken back in its causal past")
		}
		g, err := event.DecodeGrant(l.events[d].Event.Payload)
		if err != nil {
			return rejected("the grant " + d.Short() + " it names cannot be read")
		}
		if g.Open {
			// An open address: any key writes there, inside the grant, and
			// never a reserved verb (contract 4.5).
			if !s.HeadOnly && event.IsReserved(e.Verb) {
				return rejected("an open address takes no reserved verb")
			}
			if s.System {
				return rejected("an open address takes no system event")
			}
		} else if !bytes.Equal(g.Subject, e.Author) {
			return rejected("the grant " + d.Short() + " was made to another key")
		}
		// Reserved verbs sit at the reserved address, so checking the scope
		// against the event's own address is meaningless for them. Each has
		// its own rule below, and none of them can widen a right. A head only
		// says neither address nor verb: that is the part lineage leaves
		// unjudged (U7).
		if !s.HeadOnly && !event.IsReserved(e.Verb) && !g.Allows(e.Address, e.Verb) {
			return rejected(fmt.Sprintf("the grant %s does not cover %q with verb %q", d.Short(), e.Address, e.Verb))
		}
		mine = &g
	}
	if s.HeadOnly {
		return verdict{state: Accepted, g: G, r: R, sys: S, unkeyed: U, level: level}
	}
	root := bytes.Equal(e.Author, l.root)

	// Rules specific to reserved verbs.
	switch e.Verb {
	case event.VerbGrant:
		ng, err := event.DecodeGrant(e.Payload)
		if err != nil {
			return rejected("the grant it carries cannot be read")
		}
		if ng.Open && !root {
			return rejected("only the root writes an open grant")
		}
		if mine != nil {
			// CanDelegate is itself the permission to write a grant event,
			// not something that appears in a verb list. Without it nobody
			// delegates anything. And what is delegated is never wider than
			// what the delegator holds.
			if !mine.CanDelegate {
				return rejected("its grant does not allow granting further")
			}
			if !event.ScopeWithin(ng.Scope, mine.Scope) {
				return rejected("it would grant a wider scope than its own; a scope only narrows")
			}
			if !event.VerbsWithin(ng.Verbs, mine.Verbs) {
				return rejected("it would grant verbs it does not hold; a right only narrows")
			}
		}
		G = G.add(id)

	case event.VerbRevoke:
		rv, err := event.DecodeRevoke(e.Payload)
		if err != nil {
			return rejected("the revocation it carries cannot be read")
		}
		if !G.contains(rv.Target) {
			return rejected("it takes back " + rv.Target.Short() + ", which is not a grant in its causal past") // what is not in the past cannot be revoked
		}
		// Anyone may withdraw what they themselves granted, and root may
		// withdraw anything. Nobody withdraws someone else's grant. So
		// "revoke" never needs to appear in a verb list.
		tgt := l.events[rv.Target].Event
		if !root && !bytes.Equal(e.Author, tgt.Author) {
			return rejected("only the root or the granter takes a grant back")
		}
		R = R.add(rv.Target)

	case event.VerbKeyring:
		k, err := event.DecodeKeyring(e.Payload)
		if err != nil {
			return rejected("the keyring change it carries cannot be read")
		}
		switch k.Op {
		case event.KeyringAdd:
			// K1: the root alone adds a key. Delegated keyring writing is not
			// in v1.
			if !root {
				return rejected("only the root adds a key to the keyring")
			}
		case event.KeyringRevoke:
			if !S.contains(k.Target) {
				return rejected("it takes back " + k.Target.Short() + ", which is not a keyring add in its causal past")
			}
			t := l.events[k.Target].Event
			if t.Verb != event.VerbKeyring {
				return rejected("it takes back " + k.Target.Short() + ", which is not a keyring add")
			}
			added, err := event.DecodeKeyring(t.Payload)
			if err != nil || added.Op != event.KeyringAdd {
				return rejected("it takes back " + k.Target.Short() + ", which is not a keyring add")
			}
			if added.Key != k.Key || added.Gen != k.Gen {
				return rejected("its key and generation are not those of the add it takes back")
			}
			// K1: the root, or the key itself revoking its own generation.
			if !root && (added.Signer == nil || !bytes.Equal(added.Signer, e.Author)) {
				return rejected("only the root, or the key itself, takes a key generation back")
			}
			U = U.add(k.Target)
		}
		S = S.add(id)

	case event.VerbSeed:
		sd, err := event.DecodeSeed(e.Payload)
		if err != nil {
			return rejected("the seed it carries cannot be read")
		}
		switch sd.Op {
		case event.SeedGive:
			if !root {
				return rejected("only the root gives a seed")
			}
		case event.SeedTake:
			if !S.contains(sd.Give) {
				return rejected("it takes the seed " + sd.Give.Short() + ", whose give is not in its causal past")
			}
			gv := l.events[sd.Give].Event
			if gv.Verb != event.VerbSeed {
				return rejected("it names " + sd.Give.Short() + " as its give, and that is not a seed")
			}
			give, err := event.DecodeSeed(gv.Payload)
			if err != nil || give.Op != event.SeedGive {
				return rejected("it names " + sd.Give.Short() + " as its give, and that is not a give")
			}
			if give.Seed != sd.Seed || give.Key != sd.Key {
				return rejected("its seed and key are not those of the give it names")
			}
			// S1: only the signer of the key the give names takes it, under
			// a live grant (checked above).
			signer := false
			for _, a := range S {
				ae := l.events[a].Event
				if ae.Verb != event.VerbKeyring || U.contains(a) {
					continue
				}
				k, err := event.DecodeKeyring(ae.Payload)
				if err != nil || k.Op != event.KeyringAdd || k.Key != give.Key {
					continue
				}
				if k.Signer != nil && bytes.Equal(k.Signer, e.Author) {
					signer = true
					break
				}
			}
			if !signer || root {
				return rejected("only the signer of the key the give names takes the seed")
			}
		}
		S = S.add(id)
	}

	return verdict{state: Accepted, g: G, r: R, sys: S, unkeyed: U, level: level}
}

// ---------- views ----------

// Heads are the accepted events that no accepted event claims as a parent.
// Several heads means two bodies wrote while apart, not a broken ledger.
// Concurrent acts both stay heads. Two of them can contradict each other
// out in the world, and resolving that is the application layer's work:
// the ledger keeps both and names no winner.
//
//	— T5.5
func (l *Ledger) Heads() []frame.ID {
	out := make([]frame.ID, 0, len(l.heads))
	for id := range l.heads {
		out = append(out, id)
	}
	sortIDs(out)
	return out
}

// intern returns the interned form of an authority set computed for id. A
// set that is exactly one of the parents' own — the same backing array, which
// union and add hand back untouched when nothing was added — is already
// interned and is returned as it is, so an event that changes no authority
// costs the pool nothing.
func (l *Ledger) intern(s idset, id frame.ID, at map[frame.ID]idset) idset {
	if len(s) == 0 {
		return nil
	}
	for _, p := range l.events[id].Event.Parents {
		if ps := at[p]; len(ps) == len(s) && &ps[0] == &s[0] {
			return ps
		}
	}
	return l.pool.canon(s)
}

// Order is the deterministic topological order of accepted events.
//
// Ties break on id bytes alone: no clock, no counter, no arrival order. Two
// bodies holding the same data always reach the same order.
//
// The answer is computed once and kept until an accepted event changes it;
// the caller gets a copy, so nothing it does to the slice reaches the ledger.
func (l *Ledger) Order() []frame.ID {
	l.ordered()
	return append([]frame.ID(nil), l.order...)
}

// ordered makes sure the cached order and the index into it are current.
func (l *Ledger) ordered() {
	l.viewMu.Lock()
	defer l.viewMu.Unlock()
	if l.order != nil {
		return
	}
	indeg := make(map[frame.ID]int, l.tally[Accepted])
	for id, st := range l.state {
		if st != Accepted {
			continue
		}
		n := 0
		for _, p := range l.events[id].Event.Parents {
			if l.state[p] == Accepted {
				n++
			}
		}
		indeg[id] = n
	}
	h := &idHeap{}
	heap.Init(h)
	for id, n := range indeg {
		if n == 0 {
			heap.Push(h, id)
		}
	}
	out := make([]frame.ID, 0, len(indeg))
	pos := make(map[frame.ID]int, len(indeg))
	for h.Len() > 0 {
		id := heap.Pop(h).(frame.ID)
		pos[id] = len(out)
		out = append(out, id)
		for _, c := range l.children[id] {
			if l.state[c] != Accepted {
				continue
			}
			indeg[c]--
			if indeg[c] == 0 {
				heap.Push(h, c)
			}
		}
	}
	l.order, l.pos = out, pos
}

// Position is an accepted event's index in Order, so a reader can walk the
// order from a place it remembers without being handed the whole of it.
func (l *Ledger) Position(id frame.ID) (int, bool) {
	if l.state[id] != Accepted {
		return 0, false
	}
	l.ordered()
	i, ok := l.pos[id]
	return i, ok
}

// After is the accepted events that come after the one named, in Order, at
// most limit of them (no limit when limit is not positive). The zero id means
// "from the beginning". It is the cursor a program keeps between two visits:
// what it has already read, it does not read again.
func (l *Ledger) After(id frame.ID, limit int) []frame.ID {
	l.ordered()
	start := 0
	if !id.IsZero() {
		i, ok := l.pos[id]
		if !ok {
			return nil
		}
		start = i + 1
	}
	if start >= len(l.order) {
		return []frame.ID{}
	}
	end := len(l.order)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return append([]frame.ID(nil), l.order[start:end]...)
}

// CausalPast returns the accepted ancestors of the given points, inclusive of
// the points themselves, in the ledger's deterministic order.
//
// This is a graph query, not a policy. It knows nothing about peers, covenants
// or disclosure; it exists so that layers above can evaluate their own
// questions over causal history without reaching into the ledger's internals.
//
// Cost is linear in the ledger. Fine at this size; an index, if one is ever
// needed, belongs outside the carrier and outside the truth.
// Rokh's time is not a clock; it is lineage. "Earlier" means reachable
// through parents, and two events neither of which is reachable from the
// other are concurrent — whatever their clocks say.
//
//	— T5, T5.2
func (l *Ledger) CausalPast(at ...frame.ID) []frame.ID {
	seen := make(map[frame.ID]bool, len(at))
	stack := make([]frame.ID, 0, len(at))
	for _, id := range at {
		if l.state[id] == Accepted && !seen[id] {
			seen[id] = true
			stack = append(stack, id)
		}
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, p := range l.events[id].Event.Parents {
			if l.state[p] == Accepted && !seen[p] {
				seen[p] = true
				stack = append(stack, p)
			}
		}
	}
	// The past is ordered by each event's place in the deterministic order,
	// which costs the size of the past, not of the ledger.
	l.ordered()
	out := make([]frame.ID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return l.pos[out[i]] < l.pos[out[j]] })
	return out
}

// Tally counts the verdicts.
func (l *Ledger) Tally() (accepted, rejected, pending int) {
	return l.tally[Accepted], l.tally[Rejected], l.tally[Pending]
}

// ActiveGrants lists the live grants as seen from given points (default: all
// heads).
//
// With several points the answer is "as seen by an event that took all of
// them as parents": grants union, revocations union too. So if one branch
// revoked a grant, it is dead in the joint answer — exactly as it would be
// if you merged those points right now.
//
// To see one branch's state, pass that one head.
//
// This is why revocation is not retroactive and cannot be. The answer is
// always relative to a point, and an event already written asked this question
// at its own point, where the grant was live. A revocation added later is a
// fact about later points; it does not travel backwards and it does not unmake
// what was validly written. Closing a grant closes the future.
//
// And the same shape says what revocation cannot do. It shuts what comes next;
// it does not unsee what was seen. No system can, and any that claims to is
// lying.
//
//	— N6.2, T6.2
func (l *Ledger) ActiveGrants(at ...frame.ID) []frame.ID {
	points := at
	if len(points) == 0 {
		points = l.Heads()
	}
	var G, R idset
	for _, p := range points {
		G = union(G, l.grants[p])
		R = union(R, l.revoked[p])
	}
	var out []frame.ID
	for _, g := range G {
		if !R.contains(g) {
			out = append(out, g)
		}
	}
	return out
}

// Keyring lists the live keyring adds and the live seed events as seen from
// given points (default: all heads): live(P) = adds in past(P) \ targets of
// keyring revokes in past(P). Both sets only grow, so the union of two copies
// commutes and a revoked generation never comes back (contract 4.4).
func (l *Ledger) Keyring(at ...frame.ID) (adds, seeds []frame.ID) {
	points := at
	if len(points) == 0 {
		points = l.Heads()
	}
	var S, U idset
	for _, p := range points {
		S = union(S, l.system[p])
		U = union(U, l.unkeyed[p])
	}
	for _, id := range S {
		e := l.events[id].Event
		switch e.Verb {
		case event.VerbKeyring:
			k, err := event.DecodeKeyring(e.Payload)
			if err == nil && k.Op == event.KeyringAdd && !U.contains(id) {
				adds = append(adds, id)
			}
		case event.VerbSeed:
			seeds = append(seeds, id)
		}
	}
	return adds, seeds
}

// OwnerFoldAt is the owner's keyring fold at a point named by its parents:
// the point of an accepted event, or of one about to be signed on those
// parents. It answers the owner's reader generations live there,
// whether any owner generation was ever added in its past (revoked since or
// not), and whether this ledger knows the point: every parent accepted here.
// No parents is the genesis's point, which has no keyring before it.
//
// Its three answers are three states, and only a caller can say what stands
// in for the first (R4): live generations; none ever added (the first
// bootstrap, where the owner's first generation is still only in the owner
// cell); none live where one was added (a fold emptied by revocation, known
// and refused); and an unknown point, refused.
func (l *Ledger) OwnerFoldAt(parents ...frame.ID) (readers [][]byte, ever, known bool) {
	if len(parents) == 0 {
		return nil, false, true
	}
	var S idset
	for _, p := range parents {
		if l.state[p] != Accepted {
			return nil, false, false
		}
		S = union(S, l.system[p])
	}
	adds, _ := l.Keyring(parents...)
	for _, a := range adds {
		k, err := event.DecodeKeyring(l.events[a].Event.Payload)
		if err != nil || !k.IsOwner() || len(k.Reader) == 0 {
			continue
		}
		readers = append(readers, append([]byte(nil), k.Reader...))
	}
	return readers, l.ownerAdded(S), true
}

// OwnerReadersAt answers the owner's reader generations the keyring holds
// live at an accepted event's own point: the fold over its parents.
// known is false for an event this ledger does not hold as accepted, so an
// unknown point is refused, never replaced by the present owner. A genesis
// has no keyring before it. The owner's first generation is in the vessel's
// owner cell, not in the keyring; the caller that opened the cell adds it.
func (l *Ledger) OwnerReadersAt(id frame.ID) (readers [][]byte, known bool) {
	e, ok := l.events[id]
	if !ok || l.state[id] != Accepted {
		return nil, false
	}
	readers, _, known = l.OwnerFoldAt(e.Event.Parents...)
	return readers, known
}

// OwnerEverAt says whether any owner keyring generation was added in the
// past of an accepted event's point, revoked since or not. An empty owner
// fold where one was added is a fold emptied by revocation, which is known
// and refused, never the genesis case (R4).
func (l *Ledger) OwnerEverAt(id frame.ID) (ever, known bool) {
	e, ok := l.events[id]
	if !ok || l.state[id] != Accepted {
		return false, false
	}
	_, ever, known = l.OwnerFoldAt(e.Event.Parents...)
	return ever, known
}

// OwnerEver says whether this ledger holds any owner keyring generation.
func (l *Ledger) OwnerEver() bool {
	var S idset
	for _, p := range l.Heads() {
		S = union(S, l.system[p])
	}
	return l.ownerAdded(S)
}

func (l *Ledger) ownerAdded(S idset) bool {
	for _, sid := range S {
		ev := l.events[sid].Event
		if ev.Verb != event.VerbKeyring {
			continue
		}
		if k, err := event.DecodeKeyring(ev.Payload); err == nil && k.Op == event.KeyringAdd && k.IsOwner() {
			return true
		}
	}
	return false
}

// OpenGrants lists the live open grants as seen from given points.
func (l *Ledger) OpenGrants(at ...frame.ID) []frame.ID {
	var out []frame.ID
	for _, id := range l.ActiveGrants(at...) {
		g, err := event.DecodeGrant(l.events[id].Event.Payload)
		if err == nil && g.Open {
			out = append(out, id)
		}
	}
	return out
}

func sortIDs(s []frame.ID) {
	sort.Slice(s, func(i, j int) bool { return s[i].Compare(s[j]) < 0 })
}

// idHeap is the priority queue behind the deterministic topological order.
type idHeap []frame.ID

func (h idHeap) Len() int           { return len(h) }
func (h idHeap) Less(i, j int) bool { return h[i].Compare(h[j]) < 0 }
func (h idHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *idHeap) Push(x any)        { *h = append(*h, x.(frame.ID)) }
func (h *idHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// Measure is what stands where a vote would be, written down so it can be
// pointed at.
//
// There is no quorum here, no threshold, no set of peers whose agreement makes
// a thing true. Nothing replaced consensus: measurement did. One person,
// offline, with a small program, checks the whole ledger — every byte against
// its hash, every hash against its signature, every signature against the
// authority that was live at that moment. If it read, it read. Add below is
// that program, applied one event at a time.
//
//	— N2.6, T12.1
const Measure = "measurement stands where the vote would be"

// Witness is what the ledger testifies to, and the line it does not cross.
//
// Rokh does not say you told the truth. It says you said it. The ledger
// witnesses the writer and the order, not the fact: write a lie and you have a
// signed, dated lie, accepted exactly as any other event, because nothing here
// reads what a payload means. That is not a gap to be closed later — a ledger
// that judged content would need somewhere to get the truth from, and there is
// nowhere.
//
//	— N6.3, T12.2
const Witness = "witness of the writer and the order, never of the fact"

// SeatOfJudgement is the seat left empty, on purpose.
//
// Measurement took the place of the vote and the byte took the place of trust.
// Nothing took the place of judgement: that seat stayed empty, deliberately,
// for a person. The minute where a decision is made belongs to a human and is
// not counted — not weighed, not scored, not averaged into anything.
//
// So an untouched ledger produces nothing. It has no timer, no watcher, no
// rule that fires on its own; every event in it is there because someone wrote
// it. Whoever looks for the place where Rokh decides will not find one, and the
// absence is the design.
//
//	— T12.4, N7.7, N9.2, N-Axiom2
const SeatOfJudgement = "left empty, for a person"

// ClosedAndAlive is what a ledger is, in the two words the first ruling uses.
//
// **Closed**: it is one person's. Its authority comes from its own causal past
// and from nowhere else, so an event signed by a key this ledger never granted
// anything to is rejected — not queued for someone's approval, not accepted at
// a lower confidence. Rejected, meaning never was. Nothing external can be
// pointed at to make a writer legitimate here: there is no registry, no
// authority server, no quorum. Closed is not a security posture bolted on; it
// is where the ledger gets the right to say yes.
//
// **Alive**: it is not an archive. It keeps being written to, it grows without
// a ceiling, and opening it again resumes it rather than starting it over. The
// past in it is settled permanently — accepted stays accepted, rejected stays
// rejected — and that settledness is what makes further writing possible
// instead of what stops it.
//
// The two hold together and neither is the other's cost. A ledger closed and
// dead is a file; a ledger alive and open is somebody else's database.
//
//	— T1, T1.1, T3.3, T6.2
const ClosedAndAlive = "one person's, and still being written"
