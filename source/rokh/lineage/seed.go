package lineage

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

// Event is one signed event as the key layer made it: the id, the signed
// head, the body, and the address it is sealed for.
type Event struct {
	ID      frame.ID
	Head    []byte
	Body    []byte
	Address string
}

// SeedPlan is what the key layer prepares for `rokh seed SRC DST` (S2): the
// events recorded on the source in one commit (keyring add for the seed's
// key, one rokh.grant per scope, and give), and the take recorded on the new
// vessel once its id is known. Signing is the key layer's; this package
// moves bytes and never signs.
type SeedPlan struct {
	Seed   frame.ID // the seed's id, named by give and take
	Scopes []string // absent means whole
	Give   []Event
	Take   func(vessel frame.ID) (Event, error)
	Branch string // the branch that names the recorded events; "main" when empty
}

// SeedTarget is the new vessel: its medium, its parameters (the salt is the
// source's, inherited), its own VK and slots, its sealer, the host's owner of
// it once it exists, and the vessel id it is made with (zero: drawn at
// random).
type SeedTarget struct {
	M      vessel.Medium
	Params vessel.Params
	VK     []byte
	Slots  [][]byte
	Sealer carrier.Sealer
	Owner  func() (vessel.Owner, error)
	Vessel frame.ID
}

// SeedResult names the three steps of a seed.
type SeedResult struct {
	Give, Copy, Take Half
	Vessel           frame.ID
	Repeated         bool // the target had already taken this give: nothing changed (S5)
}

// OK is true when every step is recorded.
func (r SeedResult) OK() bool {
	return r.Give.Outcome == vessel.Recorded && r.Copy.Outcome == vessel.Recorded && r.Take.Outcome == vessel.Recorded
}

// Seed makes a new vessel of the same rokh from the source (4.7, S2..S5): it
// records the give on the source, creates the target with its own VK and the
// inherited salt, copies genesis, every system event, the signed head of
// every ancestor and the events and content in scope, and records the take on
// the target. A target that already took this give is left as it is.
func Seed(src Side, dst SeedTarget, plan SeedPlan) (SeedResult, error) {
	var res SeedResult
	if err := src.ready(); err != nil {
		res.Give = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	branch := plan.Branch
	if branch == "" {
		branch = "main"
	}
	// S1: a seed's scopes lie inside the giver's own. The whole rokh lies
	// inside no slice: a source that holds a slice gives no whole seed.
	if own := src.C.Vessel().Info().Scopes; len(plan.Scopes) == 0 && len(own) > 0 {
		res.Give = Half{Outcome: vessel.NotRecorded, Err: ErrSeedRefused}
		return res, fmt.Errorf("%w: the source holds only %q, and a whole seed does not lie inside it", ErrSeedRefused, own)
	}
	for _, s := range plan.Scopes {
		if !Within(s, src.C.Vessel().Info().Scopes) {
			res.Give = Half{Outcome: vessel.NotRecorded, Err: ErrSeedRefused}
			return res, fmt.Errorf("%w: scope %q is outside the source's own scopes", ErrSeedRefused, s)
		}
	}
	// The give first, on the source, in one commit: a target is never made
	// for a give that is not recorded. A give already recorded is not
	// recorded again.
	srcHas, err := holdings(src.C)
	if err != nil {
		res.Give = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	var give []Event
	for _, e := range plan.Give {
		if !srcHas.events[e.ID] {
			give = append(give, e)
		}
	}
	res.Give = recordEvents(src, give, branch)
	if res.Give.Outcome != vessel.Recorded {
		return res, fmt.Errorf("lineage: the give was not recorded on the source: %s", res.Give.Outcome)
	}
	// The target: made now, or found as it was left.
	info := src.C.Vessel().Info()
	p := dst.Params
	p.Salt, p.Iter = info.Salt, info.Iter
	d, err := carrier.CreateRoot(dst.M, p, dst.VK, dst.Slots, vessel.Root{Anchor: src.C.Anchor(), Vessel: dst.Vessel, Seed: plan.Seed, Scopes: plan.Scopes}, dst.Sealer)
	existed := errors.Is(err, vessel.ErrExists)
	if existed {
		d, _, err = carrier.Open(dst.M, func([][]byte, []byte, int) ([]byte, error) { return dst.VK, nil }, dst.Params.Rand, dst.Sealer)
		if err != nil {
			res.Copy = Half{Outcome: vessel.NotRecorded, Err: err}
			return res, fmt.Errorf("%w: the target holds something that is not this seed: %v", ErrSeedRefused, err)
		}
		if ti := d.Vessel().Info(); ti.Anchor != src.C.Anchor() || ti.Seed != plan.Seed {
			res.Copy = Half{Outcome: vessel.NotRecorded, Err: ErrSeedRefused}
			return res, fmt.Errorf("%w: the target is another vessel", ErrSeedRefused)
		}
	} else if err != nil {
		res.Copy = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	res.Vessel = d.Vessel().Info().Vessel
	took := existed && tookSeed(d, plan.Seed)
	if took {
		// S5: a target that already took this give changes nothing.
		res.Repeated = true
		res.Copy = Half{Outcome: vessel.Recorded}
		res.Take = Half{Outcome: vessel.Recorded}
		return res, nil
	}
	own, err := dst.Owner()
	if err != nil {
		res.Copy = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	target := Side{C: d, Own: own}
	// Everything in scope and the necessary lineage, one way; a copy begun
	// before goes on from what the target holds.
	has, err := holdings(d)
	if err != nil {
		res.Copy = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	recs, tips, held, err := offer(src, d, has)
	if err != nil {
		res.Copy = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	// The tip the source's own branch names takes that branch here too, when
	// the target has none yet: the seed's branch goes on from the source's.
	srcTip, _, err := src.C.Ref(branch)
	if err != nil {
		res.Copy = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	refs, err := pinsPreferring(d, tips, srcTip)
	if err != nil {
		res.Copy = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	res.Copy = apply(target, recs, refs)
	res.Copy.Held = held
	if res.Copy.Outcome != vessel.Recorded {
		return res, fmt.Errorf("lineage: the copy was not recorded on the target: %s", res.Copy.Outcome)
	}
	take, err := plan.Take(res.Vessel)
	if err != nil {
		res.Take = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	// The take moves the branch. A tip the branch named that is not in the
	// take's past keeps a name of its own: every event the copy brought stays
	// named, so the target's ledger, which is read from its names, holds it.
	named, err := namedWithTake(d, branch, take)
	if err != nil {
		res.Take = Half{Outcome: vessel.NotRecorded, Err: err}
		return res, err
	}
	res.Take = recordNamed(target, []Event{take}, named)
	if res.Take.Outcome != vessel.Recorded {
		return res, fmt.Errorf("lineage: the take was not recorded on the target: %s", res.Take.Outcome)
	}
	return res, nil
}

// Took reports whether a vessel holds the take of a seed, by the rule Seed
// itself follows (S5): a caller that would change a seed's vessel first asks
// whether it already took its give.
func Took(c *carrier.Carrier, seed frame.ID) bool { return tookSeed(c, seed) }

// tookSeed reports whether a vessel holds the take of a seed: a system event
// rokh.seed whose payload (contract 4.7) says op take (0x02) and names the
// seed. Only what the target's session opens is read.
func tookSeed(c *carrier.Carrier, seed frame.ID) bool {
	found := false
	c.Events(func(e carrier.EventRecord) error {
		if found || !System(e.Head) {
			return nil
		}
		body, err := c.Body(e)
		if err != nil {
			return nil
		}
		fs, err := frame.DecodeFields(body)
		if err != nil {
			return nil
		}
		if verb, _ := fs.Get(0x0006); string(verb) != "rokh.seed" {
			return nil
		}
		payload, _ := fs.Get(0x0007)
		pf, err := frame.DecodeFields(payload)
		if err != nil {
			return nil
		}
		op, _ := pf.Get(0x0001)
		id, _ := pf.Get(0x0002)
		if len(op) == 1 && op[0] == 0x02 && bytes.Equal(id, seed[:]) {
			found = true
		}
		return nil
	}, nil)
	return found
}

// recordEvents commits events on one side, in one commit, and names the last one
// on the branch.
func recordEvents(s Side, evs []Event, branch string) Half {
	if len(evs) == 0 {
		return Half{Outcome: vessel.Recorded}
	}
	return recordNamed(s, evs, map[string]frame.ID{branch: evs[len(evs)-1].ID})
}

// recordNamed commits events on one side, in one commit, with the references
// given.
func recordNamed(s Side, evs []Event, refs map[string]frame.ID) Half {
	if len(evs) == 0 {
		return Half{Outcome: vessel.Recorded}
	}
	r, err := s.C.Begin(s.Own)
	if err != nil {
		return Half{Outcome: vessel.NotRecorded, Err: err}
	}
	for _, e := range evs {
		if err := r.Event(e.ID, e.Head, e.Body, e.Address); err != nil {
			r.Abandon()
			return Half{Outcome: vessel.NotRecorded, Err: err}
		}
	}
	names := make([]string, 0, len(refs))
	for n := range refs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := r.SetRef(n, refs[n]); err != nil {
			r.Abandon()
			return Half{Outcome: vessel.NotRecorded, Err: err}
		}
	}
	out, err := r.Commit()
	return Half{Outcome: out, Added: len(evs), Err: err}
}

// namedWithTake is the references the take is recorded with on the target:
// the branch moves to the take, and the tip the branch named before keeps a
// name of its own when no other reference names it and it is not in the
// take's past. A seed cut after its give and finished after the source moved
// on holds events beyond the give; none of them is left without a name.
func namedWithTake(c *carrier.Carrier, branch string, take Event) (map[string]frame.ID, error) {
	out := map[string]frame.ID{branch: take.ID}
	refs, err := c.Refs()
	if err != nil {
		return nil, err
	}
	old, ok := refs[branch]
	if !ok || old == take.ID {
		return out, nil
	}
	for n, id := range refs {
		if n != branch && id == old {
			return out, nil
		}
	}
	parents := map[frame.ID][]frame.ID{}
	if err := c.Events(func(e carrier.EventRecord) error {
		parents[e.ID] = Parents(e.Head)
		return nil
	}, nil); err != nil {
		return nil, err
	}
	seen := map[frame.ID]bool{}
	stack := Parents(take.Head)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == old {
			return out, nil
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, parents[id]...)
	}
	out[pinName(old)] = old
	return out, nil
}
