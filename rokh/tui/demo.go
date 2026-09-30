package tui

import (
	"fmt"
	"io"
	"os"
)

// The art, without a ledger.
//
// Demo draws every screen of the surface from the sample that ships with
// the source, at the size of the terminal it runs in, once in colour and
// once plain, one after another, and returns. It opens nothing, asks for
// nothing and records nothing: it is how a newcomer sees what Rokh looks
// like before making anything.

// Shot is one screen of the surface: its name, the view and the snapshot.
type Shot struct {
	Name string
	View View
	St   Snapshot
}

// Shots are the screens of the surface, drawn from the sample: the four
// views, the list, a long answer being read, a refusal, a ledger just made,
// and no ledger open.
func Shots() []Shot {
	sample := Sample()
	fresh := Sample()
	fresh.Accepted, fresh.Drafts, fresh.Writing, fresh.Disclosure, fresh.TakenBack = 0, nil, nil, nil, nil
	fresh.Events = []EventItem{sample.Events[5], sample.Events[6]}
	fresh.Head, fresh.System = sample.Events[5].ID, 2
	fresh.Layers = Layers{Star: Star{Anchor: sample.Anchor, System: 2}}
	var answer []string
	for i := 1; i <= 12; i++ {
		answer = append(answer, fmt.Sprintf("— home/journal/day%02d (%08x): the note of day %d, as it was written", i, 0x3a7c1e00+i*97, i))
	}
	answer = append(answer, "12 events at home/journal.")
	return []Shot{
		{"ledger", View{}, sample},
		{"sentence", View{Screen: ScreenSentence}, sample},
		{"authority", View{Screen: ScreenAuthority}, sample},
		{"layers", View{Screen: ScreenLayers}, sample},
		{"list", View{MenuOpen: true}, sample},
		{"answer", View{Reply: answer}, sample},
		{"reading", View{Reply: answer, Reading: true}, sample},
		{"refusal", View{Failed: true, Reply: []string{"no — this Rokh is full, so nothing was recorded. To make room, leave and run: rokh grow /path/to/rokh/ledgers/home --to 128M [vessel_full; not recorded]"}}, sample},
		{"fresh", View{}, fresh},
		{"no-ledger", View{}, Snapshot{Vault: sample.Vault}},
	}
}

// Demo draws every shot at the size of the terminal out is, in colour
// (at the depth the terminal says, or 24-bit where it is not a terminal)
// and then plain. NO_COLOR is honoured: then it draws them plain only.
func Demo(out *os.File) error {
	o := Measure(out)
	colour := os.Getenv("NO_COLOR") == ""
	if !isTerminal(out) {
		o.Depth = DepthTrue
	}
	passes := []bool{true, false}
	if !colour {
		passes = []bool{false}
	}
	return demo(out, o, passes)
}

func demo(w io.Writer, o Options, passes []bool) error {
	for _, withColour := range passes {
		for _, sh := range Shots() {
			o.Color = withColour
			how := "plain"
			if withColour {
				how = "in colour"
			}
			if _, err := fmt.Fprintf(w, "── %s, %dx%d, %s ──\n%s\n\n", sh.Name, o.Columns, o.Rows, how, Render(o, sh.View, sh.St)); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(w, "These screens are drawn from a made-up ledger that ships with the source; nothing was opened or recorded.")
	return err
}
