package shell

import (
	"path/filepath"
	"strings"
	"testing"

	"rokh/tui"
)

// The three layers are read from the ledger: the Star is the rokh itself,
// a Planet the first component of an address, a Moon the second, and what
// lies deeper is listed inside its Moon. The screen draws them as a view of
// their own; the line surface says them as an aside of "see the ledger",
// where auxiliary detail goes, and the screen does not repeat them there.
// A ledger made here holds two system events from its making: its first
// event and the record of its owner's keyring, as one made by the command
// line does.
func TestTheThreeLayers(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	s, err := makeRokh(vault, "", "", "pass", "home", minRoom)
	if err != nil {
		t.Fatal(err)
	}
	defer s.closeAll()
	for _, a := range []string{"a/b/c", "a/b/d", "a/e", "f"} {
		run(t, s, "write at "+a+": a note at "+a)
		run(t, s, "write")
	}
	l := layersOf(s.current.led)
	if l.Star.Anchor != s.current.led.Genesis().Short() || l.Star.System != 2 || len(l.Planets) != 2 {
		t.Fatalf("layers %+v", l)
	}
	a, f := l.Planets[0], l.Planets[1]
	if a.Name != "a" || a.Events != 3 || len(a.Moons) != 2 || f.Name != "f" || f.Events != 1 || len(f.Moons) != 0 {
		t.Fatalf("planets %+v", l.Planets)
	}
	b, e := a.Moons[0], a.Moons[1]
	if b.Name != "b" || b.Events != 2 || strings.Join(b.Deeper, ",") != "c,d" || e.Name != "e" || e.Events != 1 || len(e.Deeper) != 0 {
		t.Fatalf("moons %+v", a.Moons)
	}

	var notes strings.Builder
	s.notes = &notes
	run(t, s, "see the ledger")
	for _, want := range []string{"(Star " + l.Star.Anchor + ": 2 system events, 0 keys, 0 seeds)",
		"(Planet a: 3 events; Moon b: 2 events, holding c, d; Moon e: 1 event)", "(Planet f: 1 event)"} {
		if !strings.Contains(notes.String(), want) {
			t.Errorf("the aside does not say %q:\n%s", want, notes.String())
		}
	}
	notes.Reset()
	s.onScreen = true
	run(t, s, "see the ledger")
	s.onScreen = false
	if strings.Contains(notes.String(), "Planet") {
		t.Errorf("on the screen the layers were repeated as an aside: %q", notes.String())
	}

	screen := tui.Render(tui.Options{Columns: 80, Rows: 24}, tui.View{Screen: tui.ScreenLayers}, s.snapshot())
	moonB := strings.Index(screen, "Moon  b")
	moonE := strings.Index(screen, "Moon  e")
	for _, want := range []string{"Star", "Planet  a", "Planet  f", "Moon  b", "Moon  e"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the layers view lacks %q:\n%s", want, screen)
		}
	}
	if between := screen[moonB:moonE]; moonB < 0 || moonE < moonB || !strings.Contains(between, " c ") || !strings.Contains(between, " d ") {
		t.Errorf("c and d are not listed inside Moon b:\n%s", screen)
	}
}
