package ledger

import (
	"fmt"
	"sort"

	"rokh/event"
	"rokh/frame"
)

// Load builds a ledger by following the references, not by reading whatever
// happens to lie in the object store.
//
// That distinction is the commit point. Recording an event takes two writes —
// the object, then the reference that names it — and each one alone is atomic
// while the pair is not. If the power goes between them, an object is on the
// carrier that no reference reaches. Reading the store would then accept, on
// the next open, an event whose recording never finished: before the point,
// something; which is exactly what the ruling forbids.
//
// So the walk starts at the heads and goes backwards through parents. What the
// reference reaches is the ledger. What it does not reach is not an event yet
// — it is a few bytes left behind by a recording that stopped, and the next
// write that names them will find them already there and go on.
//
// get returns an object's bytes by name; it is passed in so this package needs
// no carrier of its own. Each object is read and verified once: the walk
// parses it to learn its parents, and the same parsed event is what enters
// the ledger, parents first.
//
//	— T8.5, T4.4
func Load(genesisRaw []byte, get func(frame.ID) ([]byte, error), heads []frame.ID) (*Ledger, error) {
	return LoadWith(genesisRaw, get, nil, heads)
}

// LoadWith is Load with the completion of ExtendWith.
func LoadWith(genesisRaw []byte, get func(frame.ID) ([]byte, error), complete Completion, heads []frame.ID) (*Ledger, error) {
	l, err := New(genesisRaw)
	if err != nil {
		return nil, err
	}
	if _, err := l.ExtendWith(get, complete, heads); err != nil {
		return nil, err
	}
	return l, nil
}

// Completion gives an event whole just before it is judged. It is asked
// parents first, while the ledger already holds everything the event rests
// on, so it can read the fold at the event's own point (OwnerFoldAt of its
// parents) before it opens the body (contract 3.3, E3, E4). It answers
// the event's whole bytes, or nil to keep the head the walk read. An error
// leaves the event unproven, as a head that is not here does.
type Completion func(l *Ledger, head event.Signed) ([]byte, error)

// Extend brings the ledger up to what the references reach now, reading only
// what it does not already hold: the walk back from the heads stops at every
// event already in the ledger, so a carrier that gained a few events since
// the last look costs a few reads, not a whole reopen.
//
// It is the same walk Load makes, and it creates nothing: it follows the
// references and re-accepts what they reach. An event the ledger holds is not
// read again; one it holds pending is judged again once its ancestry is here.
// It returns the events newly accepted, in the order they settled, so a door
// that keeps a view beside the ledger can bring that view up too.
//
// What Extend cannot do is take away: an event held accepted that no
// reference reaches any more stays held. That never happens on a carrier
// written through the commit point, whose references only move forward; a
// view that must drop one rebuilds with Load.
//
//	— T8.5, T4.4
func (l *Ledger) Extend(get func(frame.ID) ([]byte, error), heads []frame.ID) ([]frame.ID, error) {
	return l.ExtendWith(get, nil, heads)
}

// ExtendWith is Extend for a reader that judges each body at its own point:
// get gives what the walk reads (a head is enough, since a head names its
// parents), and complete, when it is given, is asked for every event the walk
// read as a head only, just before that event is judged (Completion).
func (l *Ledger) ExtendWith(get func(frame.ID) ([]byte, error), complete Completion, heads []frame.ID) ([]frame.ID, error) {
	// Walk back from the heads, collecting everything the references reach
	// that is not held yet. Each object is read and parsed once.
	found := map[frame.ID]event.Signed{}
	seen := map[frame.ID]bool{}
	queue := append([]frame.ID(nil), heads...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] || l.Has(id) {
			continue
		}
		seen[id] = true
		// Only signed bytes count (C8). A head that is not there, that does
		// not parse, or that does not hash to the name it was asked for is
		// not taken: whatever rests on it stays pending and authorizes
		// nothing. A locator, a record header or an index that names it
		// proves nothing.
		raw, err := get(id)
		if err != nil {
			l.unproven[id] = "the head of " + id.Short() + " is not here"
			continue
		}
		s, err := event.Parse(raw)
		if err != nil {
			l.unproven[id] = "the bytes named " + id.Short() + " are not a valid event: " + err.Error()
			continue
		}
		if s.ID != id {
			l.unproven[id] = "the bytes named " + id.Short() + " hash to " + s.ID.Short()
			continue
		}
		found[id] = s
		queue = append(queue, s.Event.Parents...)
	}

	// Add them parents-first, so nothing waits that need not. Order does not
	// change any verdict — that is T5.4 — but it keeps the pending set empty
	// for a history that is whole, and judges each event exactly once.
	var added []frame.ID
	done := map[frame.ID]bool{}
	// judge adds one event whose parents have all been visited.
	judge := func(id frame.ID, s event.Signed) error {
		if complete != nil && s.HeadOnly {
			raw, err := complete(l, s)
			if err != nil {
				// A body that cannot be read at its point, or that is not the
				// one its head names, proves nothing; whatever rests on it waits.
				l.unproven[id] = "the body of " + id.Short() + " could not be given: " + err.Error()
				return nil
			}
			if raw != nil {
				whole, err := event.Parse(raw)
				if err != nil || whole.ID != id || whole.HeadOnly {
					l.unproven[id] = "the bytes given whole for " + id.Short() + " are not that event"
					return nil
				}
				s = whole
			}
		}
		before := l.state[id]
		st, err := l.add(s)
		if err != nil {
			return fmt.Errorf("event %s: %w", id.Short(), err)
		}
		if st == Accepted && before != Accepted {
			added = append(added, id)
		}
		return nil
	}
	// visit judges id after every parent the walk found, depth first and
	// parents in the order the event names them. The walk keeps its own
	// stack: a history is as deep as it is long, and a call per generation
	// of ancestors ran out of the goroutine's stack at a few hundred thousand
	// events on one branch, which ended the process rather than the read.
	type step struct {
		id      frame.ID
		parents []frame.ID
		next    int
	}
	visit := func(id frame.ID) error {
		if done[id] {
			return nil
		}
		done[id] = true
		s, ok := found[id]
		if !ok {
			return nil // held already, or not reached by these references
		}
		stack := []step{{id: id, parents: s.Event.Parents}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next < len(top.parents) {
				p := top.parents[top.next]
				top.next++
				if done[p] {
					continue
				}
				done[p] = true
				if ps, ok := found[p]; ok {
					stack = append(stack, step{id: p, parents: ps.Event.Parents})
				}
				continue
			}
			at := top.id
			stack = stack[:len(stack)-1]
			if err := judge(at, found[at]); err != nil {
				return err
			}
		}
		return nil
	}
	// Visit in a fixed order, so the same carrier read twice settles the same
	// way and the returned list is reproducible.
	ids := make([]frame.ID, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sortIDs(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return added, nil
}

// Unproven lists the names the references reached whose heads could not be
// proven, with the reason for each, in byte order. Whatever rests on them is
// pending.
func (l *Ledger) Unproven() []string {
	ids := make([]frame.ID, 0, len(l.unproven))
	for id := range l.unproven {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) < 0 })
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String()+": "+l.unproven[id])
	}
	return out
}
