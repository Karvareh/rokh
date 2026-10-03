// Weighs a pledge of allegiance as a harness would write it: one-sided, in
// the pledger's own ledger, naming the anchors pledged to. Then counts the
// pledges of a small village as the owner described: every person is one
// voice; a pledge carries onward the whole of what its pledger holds, their
// own voice and every voice they gathered; a pledge to several splits it
// equally; a voice ends with whoever pledges to nobody. Copy into
// source/rokh/oracle and run:
//
//	go test -count=1 -run TestPledgeCount -v ./oracle
//
// It changes nothing in the tree and asserts nothing; it reports.
package oracle

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/harness"
	"rokh/ledger"
)

func TestPledgeCount(t *testing.T) {
	cov, err := harness.Covenant{Namespace: "pledge", Version: "1",
		Can: []string{"pledge.give"}, Unknown: harness.Refuse}.Bind()
	if err != nil {
		t.Fatal(err)
	}

	type person struct {
		name    string
		priv    ed25519.PrivateKey
		gen     event.Signed
		pledges []event.Signed
	}
	var people []*person
	who := map[string]*person{}
	byAnchor := map[frame.ID]*person{}
	newPerson := func(name string) *person {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		gen, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, priv, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		p := &person{name: name, priv: priv, gen: gen}
		people = append(people, p)
		who[name] = p
		byAnchor[gen.ID] = p
		return p
	}
	// A pledge names the whole of its pledger's present choice. Naming
	// nobody takes the pledge back.
	pledge := func(p *person, after frame.ID, to ...*person) event.Signed {
		ids := []frame.ID{}
		for _, q := range to {
			ids = append(ids, q.gen.ID)
		}
		payload, err := cov.Stamps("pledge.give", struct {
			To []frame.ID `json:"to"`
		}{ids})
		if err != nil {
			t.Fatal(err)
		}
		anchor := p.gen.ID
		e, err := event.SignFrom(event.Event{Carrier: &anchor, Parents: []frame.ID{after},
			Address: "pledge/allegiance", Verb: "pledge.give", Payload: payload}, p.priv, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		p.pledges = append(p.pledges, e)
		return e
	}

	// The village. Members pledge to the head of their family, heads to the
	// elder of their clan, elders to the village. The village, and a
	// neighbour outside it, pledge to nobody and write nothing but their
	// genesis.
	village, neighbour := newPerson("village"), newPerson("neighbour")
	elder1, elder2 := newPerson("elder-1"), newPerson("elder-2")
	head1, head2, head3 := newPerson("head-1"), newPerson("head-2"), newPerson("head-3")
	for _, f := range []struct {
		head *person
		n    int
		tag  string
	}{{head1, 3, "1"}, {head2, 2, "2"}, {head3, 1, "3"}} {
		for i := 1; i <= f.n; i++ {
			m := newPerson(fmt.Sprintf("member-%s.%d", f.tag, i))
			pledge(m, m.gen.ID, f.head)
		}
	}
	pledge(head1, head1.gen.ID, elder1)
	pledge(head2, head2.gen.ID, elder1)
	pledge(head3, head3.gen.ID, elder2)
	pledge(elder1, elder1.gen.ID, village)
	pledge(elder2, elder2.gen.ID, village)
	// One pledges to the village directly; one splits between two heads.
	direct, split := newPerson("direct"), newPerson("split")
	pledge(direct, direct.gen.ID, village)
	pledge(split, split.gen.ID, head1, head3)
	// Two pledge to each other, and to nobody else.
	pactA, pactB := newPerson("pact-a"), newPerson("pact-b")
	pledge(pactA, pactA.gen.ID, pactB)
	pledge(pactB, pactB.gen.ID, pactA)
	// Two in a cycle with a way out: one splits between the other and the
	// village, the other pledges back.
	loopA, loopB := newPerson("loop-a"), newPerson("loop-b")
	pledge(loopA, loopA.gen.ID, loopB, village)
	pledge(loopB, loopB.gen.ID, loopA)
	// One writes two pledges from two seeds that have not met: neither is in
	// the other's causal past.
	twoSeeds := newPerson("two-seeds")
	pledge(twoSeeds, twoSeeds.gen.ID, village)
	pledge(twoSeeds, twoSeeds.gen.ID, neighbour)
	// One pledges to a head, and later takes it back.
	withdrawn := newPerson("withdrawn")
	first := pledge(withdrawn, withdrawn.gen.ID, head1)
	pledge(withdrawn, first.ID)

	// Sizes, and what each pledger's own ledger says of its pledges.
	t.Logf("%-54s %5d bytes", "a genesis, the anchor of a person", len(village.gen.Raw))
	t.Logf("%-54s %5d bytes", "a pledge naming one anchor", len(head1.pledges[0].Raw))
	t.Logf("%-54s %5d bytes", "a pledge naming two anchors", len(split.pledges[0].Raw))
	t.Logf("%-54s %5d bytes", "a pledge naming nobody: the pledge taken back", len(withdrawn.pledges[1].Raw))
	total, count, accepted := 0, 0, 0
	held := map[*person]*ledger.Ledger{}
	for _, p := range people {
		l, err := ledger.New(p.gen.Raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.pledges {
			st, err := l.Add(e.Raw)
			if st == ledger.Accepted {
				accepted++
			} else {
				t.Logf("%s: a pledge was %s: %v", p.name, st, err)
			}
			total += len(e.Raw)
			count++
		}
		held[p] = l
	}
	t.Logf("%-54s %5d bytes", fmt.Sprintf("every pledge of the village: %d, %d accepted", count, accepted), total)
	t.Logf("%-54s %5d", "heads of the ledger of two-seeds", len(held[twoSeeds].Heads()))

	// The count, as a harness reads it. It holds every ledger's events, each
	// judged by its own ledger; reads only what its covenant knows; and takes
	// as live the pledges that no later pledge of the same ledger has in its
	// causal past. Two live pledges are concurrent, and the rule for them is
	// the one the count is given.
	read := func(e event.Signed) ([]frame.ID, bool) {
		if cov.Read(e.Event) != harness.Known {
			return nil, false
		}
		if s := harness.StampOf(e.Event.Payload); s.Type != "pledge.give" || s.Version != "1" {
			return nil, false
		}
		var v struct {
			To []frame.ID `json:"to"`
		}
		if err := json.Unmarshal(e.Event.Payload, &v); err != nil {
			return nil, false
		}
		// One anchor once, in one order.
		seen := map[frame.ID]bool{}
		var to []frame.ID
		for _, q := range v.To {
			if !seen[q] {
				seen[q] = true
				to = append(to, q)
			}
		}
		sort.Slice(to, func(i, j int) bool { return to[i].Compare(to[j]) < 0 })
		return to, true
	}
	choices := func(rule string) map[frame.ID][]frame.ID {
		out := map[frame.ID][]frame.ID{}
		for _, p := range people {
			parents := map[frame.ID][]frame.ID{}
			var decls []event.Signed
			for _, e := range p.pledges {
				if held[p].State(e.ID) != ledger.Accepted {
					continue
				}
				parents[e.ID] = e.Event.Parents
				if _, ok := read(e); ok {
					decls = append(decls, e)
				}
			}
			past := map[frame.ID]bool{}
			var walk func(id frame.ID)
			walk = func(id frame.ID) {
				for _, q := range parents[id] {
					if !past[q] {
						past[q] = true
						walk(q)
					}
				}
			}
			for _, d := range decls {
				walk(d.ID)
			}
			var live [][]frame.ID
			for _, d := range decls {
				if !past[d.ID] {
					to, _ := read(d)
					live = append(live, to)
				}
			}
			switch {
			case len(live) == 0:
			case len(live) == 1:
				out[p.gen.ID] = live[0]
			case rule == "union":
				seen := map[frame.ID]bool{}
				for _, to := range live {
					for _, q := range to {
						if !seen[q] {
							seen[q] = true
							out[p.gen.ID] = append(out[p.gen.ID], q)
						}
					}
				}
				sort.Slice(out[p.gen.ID], func(i, j int) bool { return out[p.gen.ID][i].Compare(out[p.gen.ID][j]) < 0 })
			case rule == "neither":
				// Counted for nobody else until a later pledge has both in its
				// past, unless they name the same anchors.
				same := true
				for _, to := range live[1:] {
					if fmt.Sprint(to) != fmt.Sprint(live[0]) {
						same = false
					}
				}
				if same {
					out[p.gen.ID] = live[0]
				}
			}
		}
		return out
	}
	name := func(id frame.ID) string {
		if p, ok := byAnchor[id]; ok {
			return p.name
		}
		return "an anchor not held: " + id.Short()
	}
	// Each voice starts at its own anchor and moves along the live pledges,
	// split equally, until it reaches an anchor that pledges to nobody. A
	// voice that can reach no such anchor is held in a closed cycle.
	const rounds = 4000
	flow := func(choice map[frame.ID][]frame.ID, from []frame.ID) (settled, carried map[frame.ID]float64, heldMass float64) {
		settled, carried = map[frame.ID]float64{}, map[frame.ID]float64{}
		moving := map[frame.ID]float64{}
		for _, id := range from {
			moving[id] += 1
		}
		for r := 0; r < rounds && len(moving) > 0; r++ {
			next := map[frame.ID]float64{}
			for id, m := range moving {
				carried[id] += m
				to := choice[id]
				if len(to) == 0 {
					settled[id] += m
					continue
				}
				for _, q := range to {
					next[q] += m / float64(len(to))
				}
			}
			moving = next
		}
		for _, m := range moving {
			heldMass += m
		}
		return settled, carried, heldMass
	}
	reachesEnd := func(choice map[frame.ID][]frame.ID, id frame.ID) bool {
		seen := map[frame.ID]bool{}
		var walk func(frame.ID) bool
		walk = func(x frame.ID) bool {
			if len(choice[x]) == 0 {
				return true
			}
			if seen[x] {
				return false
			}
			seen[x] = true
			for _, q := range choice[x] {
				if walk(q) {
					return true
				}
			}
			return false
		}
		return walk(id)
	}
	all := make([]frame.ID, 0, len(people))
	for _, p := range people {
		all = append(all, p.gen.ID)
	}
	for _, rule := range []string{"union", "neither"} {
		choice := choices(rule)
		settled, carried, heldMass := flow(choice, all)
		t.Logf("the count; two concurrent pledges are read by the rule %q", rule)
		sum := 0.0
		for _, p := range people {
			id := p.gen.ID
			var to []string
			for _, q := range choice[id] {
				to = append(to, name(q))
			}
			c := fmt.Sprintf("%9.4f", carried[id])
			if !reachesEnd(choice, id) {
				c = "   no end"
			}
			t.Logf("  %-11s pledges to %-24s carries %s  ends with it %8.4f",
				p.name, strings.Join(to, ", ")+";", c, settled[id])
			sum += settled[id]
		}
		t.Logf("  voices %d: ended %.4f, held in a closed cycle %.4f", len(people), sum, heldMass)
	}
	choice := choices("union")
	t.Logf("where one voice ends, by the rule \"union\"")
	for _, p := range []*person{who["member-1.1"], direct, split, twoSeeds, loopA, pactA, withdrawn} {
		settled, _, heldMass := flow(choice, []frame.ID{p.gen.ID})
		var ends []string
		for _, q := range people {
			if v := settled[q.gen.ID]; v > 0 {
				ends = append(ends, fmt.Sprintf("%s %.4f", q.name, v))
			}
		}
		if heldMass > 0 {
			ends = append(ends, fmt.Sprintf("held in a closed cycle %.4f", heldMass))
		}
		t.Logf("  %-11s %s", p.name, strings.Join(ends, ", "))
	}
}
