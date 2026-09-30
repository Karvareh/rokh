package shell

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"rokh/event"
	"rokh/ledger"
	"rokh/tui"
)

// The three layers.
//
// Three layers of a ledger are shown, and nothing else of its layout
// (contract section 6, axiom 6): the Star is the rokh itself (its anchor,
// its own events, its keys, its seeds); a Planet is the first component of
// an address; a Moon is the second. Deeper components are listed inside
// their Moon. They are drawn as a view of their own on the screen, and on the
// line surface they are an aside of "see the ledger": no sentence is added.

// layersOf reads the three layers out of a ledger's accepted events.
func layersOf(led *ledger.Ledger) tui.Layers {
	l := tui.Layers{Star: tui.Star{Anchor: led.Genesis().Short()}}
	keys := map[[32]byte]bool{}
	planets := map[string]*tui.Planet{}
	moons := map[string]map[string]*tui.Moon{}
	for _, id := range led.Order() {
		if led.State(id) != ledger.Accepted {
			continue
		}
		e, ok := led.Get(id)
		if !ok {
			continue
		}
		if e.System {
			l.Star.System++
			switch e.Event.Verb {
			case event.VerbKeyring:
				if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && !k.IsOwner() {
					keys[k.Key] = k.Op == event.KeyringAdd
				}
			case event.VerbSeed:
				if sd, err := event.DecodeSeed(e.Event.Payload); err == nil && sd.Op == event.SeedGive {
					l.Star.Seeds++
				}
			}
			continue
		}
		parts := strings.Split(e.Event.Address, "/")
		p := planets[parts[0]]
		if p == nil {
			p = &tui.Planet{Name: parts[0]}
			planets[parts[0]] = p
			moons[parts[0]] = map[string]*tui.Moon{}
		}
		p.Events++
		if len(parts) < 2 {
			continue
		}
		m := moons[parts[0]][parts[1]]
		if m == nil {
			m = &tui.Moon{Name: parts[1]}
			moons[parts[0]][parts[1]] = m
		}
		m.Events++
		if len(parts) > 2 {
			deeper := strings.Join(parts[2:], "/")
			seen := false
			for _, d := range m.Deeper {
				seen = seen || d == deeper
			}
			if !seen {
				m.Deeper = append(m.Deeper, deeper)
			}
		}
	}
	for _, live := range keys {
		if live {
			l.Star.Keys++
		}
	}
	for name, p := range planets {
		for _, m := range moons[name] {
			sort.Strings(m.Deeper)
			p.Moons = append(p.Moons, *m)
		}
		sort.Slice(p.Moons, func(i, j int) bool { return p.Moons[i].Name < p.Moons[j].Name })
		l.Planets = append(l.Planets, *p)
	}
	sort.Slice(l.Planets, func(i, j int) bool { return l.Planets[i].Name < l.Planets[j].Name })
	return l
}

// sayLayers writes the three layers as an aside, one parenthesised line for
// the Star and one for each Planet with its Moons.
func sayLayers(w io.Writer, l tui.Layers) {
	fmt.Fprintf(w, "(Star %s: %s, %s, %s)\n", l.Star.Anchor, countOf(l.Star.System, "system event"),
		countOf(l.Star.Keys, "key"), countOf(l.Star.Seeds, "seed"))
	for _, p := range l.Planets {
		line := fmt.Sprintf("(Planet %s: %s", escapePayload([]byte(p.Name)), countOf(p.Events, "event"))
		for _, m := range p.Moons {
			line += fmt.Sprintf("; Moon %s: %s", escapePayload([]byte(m.Name)), countOf(m.Events, "event"))
			if len(m.Deeper) > 0 {
				line += ", holding " + escapePayload([]byte(strings.Join(m.Deeper, ", ")))
			}
		}
		fmt.Fprintln(w, line+")")
	}
}
