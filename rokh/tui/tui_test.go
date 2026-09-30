package tui

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden screens under testdata")

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visible(v string) string { return ansi.ReplaceAllString(v, "") }

// fixture is a ledger as the shell would hand it over: an owner, an
// assistant with a narrow grant, one reader, and a sentence waiting.
func fixture() Snapshot {
	return Snapshot{
		Vault: "~/rokh", Ledger: "home", Anchor: "a0b6443e", Head: "8c91d2af", Heads: 1,
		Custody: "warm", Room: "64 MB", Free: "58 MB", Growth: "fixed", Verified: true, Accepted: 18, System: 3,
		Events: []EventItem{
			{ID: "8c91d2af", Verdict: "accepted", Verb: "note", Address: "home/journal/today", Payload: "I named what mattered before I moved on.", Parent: "72b4e191", Signer: "root", Authority: "the owner's own right", Door: "rokh-shell"},
			{ID: "72b4e191", Verdict: "accepted", Verb: "rokh.grant", Address: "rokh", Payload: "writing entrusted to 3fa9c1d2… at home/journal", Parent: "31ab770c", Signer: "root", Authority: "the owner's own right", Door: "rokh", System: true},
			{ID: "31ab770c", Verdict: "accepted", Verb: "note", Address: "home/journal", Payload: "The first line belongs to this address.", Parent: "a0b6443e", Signer: "assistant", Authority: "grant 5f20c331", Door: "rokh-home/program"},
		},
		Drafts: []Draft{
			{Index: 1, Count: 2, Sentence: "write at home/journal/today: I named what mattered before I moved on.",
				Address: "home/journal/today", Verb: "note", PayloadBytes: 42, Signer: "root", Authority: "the owner's own right", Parent: "8c91d2af"},
			{Index: 2, Count: 2, Sentence: "write at home/journal/later: and then the rest.",
				Address: "home/journal/later", Verb: "note", PayloadBytes: 18, Signer: "root", Authority: "the owner's own right", Parent: "8c91d2af"},
		},
		TakenBack: []AuthorityItem{{ID: "0be21d6c", Subject: "9d4f0a12", Detail: "taken back by"}},
		Layers: Layers{Star: Star{Anchor: "a0b6443e", System: 3, Keys: 1},
			Planets: []Planet{
				{Name: "home", Events: 17, Moons: []Moon{{Name: "journal", Events: 17, Deeper: []string{"today"}}}},
				{Name: "work", Events: 1},
			}},
		Writing:    []AuthorityItem{{ID: "5f20c331", Subject: "3fa9c1d2…", Scope: "home/journal", Detail: "may write note"}},
		Disclosure: []AuthorityItem{{ID: "c91e407a", Subject: "77be04ac…", Scope: "home/journal", Detail: "may receive"}},
	}
}

// fresh is a ledger just made by rokh init: its genesis and the owner's key,
// and nothing of the person's yet.
func fresh() Snapshot {
	return Snapshot{
		Vault: "~/rokh", Ledger: "home", Anchor: "a0b6443e", Head: "5d21c0e9", Heads: 1, Custody: "warm",
		Verified: true, Accepted: 0, System: 2,
		Events: []EventItem{
			{ID: "5d21c0e9", Verdict: "accepted", Verb: "rokh.keyring", Address: "rokh", Payload: "the owner's key added, generation 1", Parent: "a0b6443e", Signer: "root", Authority: "the owner's own right", System: true},
			{ID: "a0b6443e", Verdict: "accepted", Verb: "rokh.genesis", Address: "rokh", Payload: "made: my notes", Signer: "root", Authority: "the owner's own right", System: true},
		},
	}
}

// A ledger with nothing of the person's in it says how to begin, and stops
// saying it once there is.
func TestAFreshLedgerSaysWhereToStart(t *testing.T) {
	for _, o := range []Options{{Columns: 80, Rows: 24}, {Columns: 118, Rows: 36}} {
		if out := Render(o, View{}, fresh()); !strings.Contains(out, "START HERE") || !strings.Contains(out, "write at home/journal: my first note") {
			t.Errorf("%dx%d: a fresh ledger does not say where to start:\n%s", o.Columns, o.Rows, out)
		}
		if out := Render(o, View{}, fixture()); strings.Contains(out, "START HERE") {
			t.Errorf("%dx%d: a ledger with notes still says where to start", o.Columns, o.Rows)
		}
	}
}

// full is the same ledger with no room left to write: it opens, it is read,
// and it says it is full and how room is made.
func full() Snapshot {
	st := fixture()
	st.Free, st.Full = "0 bytes", true
	return st
}

var sizes = []Options{
	{Columns: 120, Rows: 36}, {Columns: 97, Rows: 27}, {Columns: 80, Rows: 26},
	{Columns: 58, Rows: 22}, {Columns: 48, Rows: 20}, {Columns: 34, Rows: 15}, {Columns: 28, Rows: 8},
}

// Every layout, every screen, the palette open and shut, a reply showing and
// not: nothing reaches the last column, because a line that reaches it wraps
// and takes the prompt with it.
func TestNothingReachesTheLastColumn(t *testing.T) {
	views := []View{{}, {Screen: ScreenSentence}, {Screen: ScreenAuthority}, {MenuOpen: true},
		{MenuOpen: true, Query: "carry", Selected: 3}, {Reply: []string{"written. 8c91d2af", "second line"}},
		{Reply: []string{"no — I do not know that sentence"}, Failed: true}, {Input: "write at کارها: نخستین سطر"}}
	for _, o := range sizes {
		for _, v := range views {
			for _, st := range []Snapshot{fixture(), {Vault: "~/rokh"}} {
				rows := strings.Split(strings.TrimSuffix(Render(o, v, st), "\n"), "\n")
				// As many rows as the window has and no more: the last is not
				// followed by a line break (Frame), so a full window does not
				// scroll; TestTheScreenFillsTheWindowAndEndsWithTheHints holds
				// that it is exactly as many.
				if len(rows) > o.Rows {
					t.Fatalf("%dx%d drew %d rows", o.Columns, o.Rows, len(rows))
				}
				for i, r := range rows {
					if w := width(visible(r)); w >= o.Columns {
						t.Fatalf("%dx%d row %d is %d cells: %q", o.Columns, o.Rows, i, w, visible(r))
					}
				}
			}
		}
	}
}

// The screens are pinned as golden files, so a change to what a person sees
// is a change somebody meant. -update rewrites them.
func TestGoldenScreens(t *testing.T) {
	cases := []struct {
		name string
		o    Options
		v    View
		st   Snapshot
	}{
		{"wide-118x36-ledger", Options{Columns: 118, Rows: 36}, View{}, fixture()},
		{"medium-80x26-sentences", Options{Columns: 80, Rows: 26}, View{MenuOpen: true, Query: "entrust"}, fixture()},
		{"narrow-48x20-sentence", Options{Columns: 48, Rows: 20}, View{Screen: ScreenSentence}, fixture()},
		{"tiny-28x8", Options{Columns: 28, Rows: 8}, View{}, fixture()},
		{"wide-118x36-no-ledger", Options{Columns: 118, Rows: 36}, View{}, Snapshot{Vault: "~/rokh"}},
		{"wide-118x36-reply", Options{Columns: 118, Rows: 36}, View{Reply: []string{"written. 4d80f7f6f0bb190030a35590640b8c5420d97d4d2b8d18a3a0d1812d7f5360bb"}}, fixture()},
		{"wide-118x36-full", Options{Columns: 118, Rows: 36}, View{}, full()},
		{"medium-80x24-full", Options{Columns: 80, Rows: 24}, View{}, full()},
		{"medium-80x24-ledger", Options{Columns: 80, Rows: 24}, View{}, fixture()},
		{"medium-80x24-long-answer", Options{Columns: 80, Rows: 24}, View{Reply: longAnswer()}, fixture()},
		{"medium-80x25-ledger", Options{Columns: 80, Rows: 25}, View{}, fixture()},
		{"medium-80x25-long-answer", Options{Columns: 80, Rows: 25}, View{Reply: longAnswer()}, fixture()},
		{"wide-100x30-ledger", Options{Columns: 100, Rows: 30}, View{}, fixture()},
		{"wide-100x30-long-answer", Options{Columns: 100, Rows: 30}, View{Reply: longAnswer()}, fixture()},
		{"medium-58x22-ledger", Options{Columns: 58, Rows: 22}, View{}, fixture()},
		{"medium-58x22-long-answer", Options{Columns: 58, Rows: 22}, View{Reply: longAnswer()}, fixture()},
		{"compact-120x12-ledger", Options{Columns: 120, Rows: 12}, View{Reply: []string{"26 events, 1 head, all verified."}}, fixture()},
		{"wide-118x50-the-list", Options{Columns: 118, Rows: 50}, View{MenuOpen: true}, fixture()},
		{"wide-118x36-layers", Options{Columns: 118, Rows: 36}, View{Screen: ScreenLayers}, fixture()},
		{"medium-80x24-layers", Options{Columns: 80, Rows: 24}, View{Screen: ScreenLayers}, fixture()},
		{"narrow-48x20-layers", Options{Columns: 48, Rows: 20}, View{Screen: ScreenLayers}, fixture()},
		{"compact-120x12-layers", Options{Columns: 120, Rows: 12}, View{Screen: ScreenLayers}, fixture()},
		{"medium-80x24-a-fresh-ledger", Options{Columns: 80, Rows: 24}, View{}, fresh()},
		{"medium-80x24-reading-first-page", Options{Columns: 80, Rows: 24}, View{Reply: longAnswer(), Reading: true}, fixture()},
		{"medium-80x24-reading-last-page", Options{Columns: 80, Rows: 24}, View{Reply: longAnswer(), Reading: true, AnswerAt: 1000}, fixture()},
	}
	for _, c := range cases {
		got := Render(c.o, c.v, c.st)
		path := filepath.Join("testdata", c.name+".txt")
		if *update {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update to write it)", c.name, err)
		}
		if string(want) != got {
			t.Errorf("%s differs from its golden screen; run with -update if this was meant:\n%s", c.name, got)
		}
	}
}

func TestColourOffLeavesNoEscape(t *testing.T) {
	if strings.Contains(Render(Options{Columns: 100, Rows: 30}, View{MenuOpen: true}, fixture()), "\x1b[") {
		t.Fatal("colour was off and an escape was drawn")
	}
}

// Six colours, and exactly the six of the house: no seventh appears, none is
// missing, and each is the owner's own hex.
func TestSixColoursAndOnlySix(t *testing.T) {
	true24 := regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)
	found := map[string]bool{}
	for _, v := range []View{{}, {Screen: ScreenSentence}, {Screen: ScreenAuthority}, {MenuOpen: true}} {
		for _, m := range true24.FindAllStringSubmatch(Render(Options{Columns: 118, Rows: 36, Color: true}, v, fixture()), -1) {
			found[m[1]+","+m[2]+","+m[3]] = true
		}
	}
	want := map[string]bool{"46,90,172": true, "217,152,0": true, "0,155,149": true, "196,70,45": true, "91,127,44": true, "139,88,184": true}
	if len(found) != len(want) {
		t.Fatalf("drew %v", found)
	}
	for c := range want {
		if !found[c] {
			t.Fatalf("the palette lost %s", c)
		}
	}
}

// The words that keep Rokh's distinctions apart are on the screens that own
// them. Accepted is not true; verified is not honest; entrusted is not
// consent; opened-to is not sent.
func TestTheDistinctionsAreOnScreen(t *testing.T) {
	o := Options{Columns: 118, Rows: 36}
	for screen, phrases := range map[int][]string{
		ScreenLedger:    {"IN CAUSAL ORDER", "I am standing at", "No clock orders", "not the truth of its payload"},
		ScreenSentence:  {"NOT RECORDED", "recording boundary", "no undo"},
		ScreenAuthority: {"nothing has been sent", "does not erase the past"},
	} {
		out := Render(o, View{Screen: screen}, fixture())
		for _, p := range phrases {
			if !strings.Contains(out, p) {
				t.Errorf("screen %d lost %q", screen, p)
			}
		}
	}
}

// A payload is shown and never obeyed: a newline, a tab and a bidirectional
// override are drawn as their escapes, and the row still fits.
func TestAPayloadCannotCommandTheTerminal(t *testing.T) {
	st := fixture()
	st.Events[0].Payload = "فارسی 🌱\nnext\t\u202eevil"
	out := Render(Options{Columns: 48, Rows: 20}, View{}, st)
	if strings.ContainsRune(out, '\u202e') {
		t.Fatalf("the bidirectional override reached the terminal: %q", out)
	}
	for _, want := range []string{`\n`, `\t`, `\u202e`} {
		if !strings.Contains(out, want) {
			t.Fatalf("the payload's %s was not drawn as its escape: %q", want, out)
		}
	}
	for i, r := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if w := width(visible(r)); w >= 48 {
			t.Fatalf("row %d is %d cells", i, w)
		}
	}
}

// The keyboard: arrows, the escape that is alone and the one that begins a
// sequence, and a rune taken whole or not at all.
func TestDecodeKeys(t *testing.T) {
	cases := []struct {
		in      string
		expired bool
		kind    KeyKind
		text    string
		used    int
	}{
		{"\x1b[A", false, KindUp, "", 3}, {"\x1b[B", false, KindDown, "", 3},
		{"\x1b[C", false, KindRight, "", 3}, {"\x1b[D", false, KindLeft, "", 3},
		{"\x1b", false, KindNone, "", 0}, {"\x1b", true, KindEscape, "", 1},
		{"\x1b[1;5C", false, KindRight, "", 6}, // ctrl-right is still right
		{"/", false, KindText, "/", 1}, {"q", false, KindText, "q", 1},
		{"\x03", false, KindQuit, "", 1}, {"\r", false, KindEnter, "", 1}, {"\x7f", false, KindBackspace, "", 1},
		{"ک", false, KindText, "ک", 2}, {"\xda", false, KindNone, "", 0}, {"\xda", true, KindNone, "", 1},
		{"🌱", false, KindText, "🌱", 4}, {"\xff", false, KindNone, "", 1},
		// the editing keys, in the spellings terminals send them
		{"\x1b[H", false, KindHome, "", 3}, {"\x1b[1~", false, KindHome, "", 4}, {"\x1bOH", false, KindHome, "", 3},
		{"\x1b[F", false, KindEnd, "", 3}, {"\x1b[4~", false, KindEnd, "", 4}, {"\x1bOF", false, KindEnd, "", 3},
		{"\x1b[3~", false, KindDelete, "", 4}, {"\x1b[5~", false, KindPageUp, "", 4}, {"\x1b[6~", false, KindPageDown, "", 4},
		{"\x1bOD", false, KindLeft, "", 3}, {"\x1bOA", false, KindUp, "", 3}, {"\x1bO", false, KindNone, "", 0}, {"\x1bO", true, KindEscape, "", 1},
		{"\x01", false, KindHome, "", 1}, {"\x05", false, KindEnd, "", 1}, {"\x15", false, KindKillStart, "", 1},
		{"\x0b", false, KindKillEnd, "", 1}, {"\x17", false, KindKillWord, "", 1}, {"\r\n", false, KindEnter, "", 2},
		// the marks a terminal puts around pasted text
		{"\x1b[200~", false, KindPasteStart, "", 6}, {"\x1b[201~", false, KindPasteEnd, "", 6},
	}
	for _, c := range cases {
		k, used := decode([]byte(c.in), c.expired)
		if k.Kind != c.kind || k.Text != c.text || used != c.used {
			t.Errorf("decode(%q, expired=%v) = %v %q %d; want %v %q %d", c.in, c.expired, k.Kind, k.Text, used, c.kind, c.text, c.used)
		}
	}
}

func press(v View, h Hooks, keys ...Key) (View, bool, bool) {
	var quit, changed bool
	for _, k := range keys {
		var q, c bool
		v, q, c = step(v, k, h)
		quit, changed = quit || q, changed || c
	}
	return v, quit, changed
}

func typed(s string) []Key {
	var ks []Key
	for _, r := range s {
		ks = append(ks, Key{Kind: KindText, Text: string(r)})
	}
	return ks
}

// The list opens on "/" only when nothing is typed: a slash inside an
// address is part of the address.
func TestASlashInsideASentenceIsNotTheList(t *testing.T) {
	v, _, _ := press(View{}, Hooks{}, typed("read home/journal")...)
	if v.MenuOpen || v.Input != "read home/journal" {
		t.Fatalf("the slash in the address opened the list: %+v", v)
	}
	v, _, _ = press(View{}, Hooks{}, typed("/")...)
	if !v.MenuOpen {
		t.Fatal("a slash on an empty prompt did not open the list")
	}
}

// Choosing from the list places the sentence up to its first slot and
// leaves the slot to be typed; a sentence with no slot is placed whole.
func TestTheListPlacesAStem(t *testing.T) {
	v, _, _ := press(View{}, Hooks{}, append(typed("/entrust writing"), Key{Kind: KindEnter})...)
	if v.MenuOpen || v.Input != "entrust writing at " || v.Screen != ScreenAuthority {
		t.Fatalf("got %+v", v)
	}
	v, _, _ = press(View{}, Hooks{}, append(typed("/reconcile"), Key{Kind: KindEnter})...)
	if v.Input != "reconcile" {
		t.Fatalf("a slotless sentence was not placed whole: %q", v.Input)
	}
}

// Enter hands the line to the shell byte for byte, shows the answer, and
// asks for a fresh snapshot; a refusal is shown as one and changes nothing.
func TestEnterSaysTheLineAndShowsTheAnswer(t *testing.T) {
	var said string
	h := Hooks{
		Say: func(s string) (string, bool, error) {
			said = s
			if s == "go" {
				return "gone.", true, nil
			}
			if strings.HasPrefix(s, "write at") {
				return "at کارها, 19 bytes — nothing is recorded yet.", false, nil
			}
			return "", false, errors.New("I do not know that sentence")
		},
		Snapshot: func() Snapshot { return fixture() },
	}
	v, quit, changed := press(View{}, h, append(typed("write at کارها: نخستین سطر"), Key{Kind: KindEnter})...)
	if said != "write at کارها: نخستین سطر" {
		t.Fatalf("the shell was told %q", said)
	}
	if quit || !changed || v.Failed || v.Input != "" || len(v.Reply) != 1 {
		t.Fatalf("after a good sentence: quit=%v changed=%v %+v", quit, changed, v)
	}
	v, _, _ = press(v, h, append(typed("nonsense"), Key{Kind: KindEnter})...)
	if !v.Failed || !strings.HasPrefix(v.Reply[0], "no — ") {
		t.Fatalf("a refusal was not shown as one: %+v", v)
	}
	_, quit, _ = press(v, h, append(typed("go"), Key{Kind: KindEnter})...)
	if !quit {
		t.Fatal("\"go\" did not end the session")
	}
}

// Backspace removes a letter, not a byte of one.
func TestBackspaceRemovesARune(t *testing.T) {
	v, _, _ := press(View{}, Hooks{}, append(typed("کار"), Key{Kind: KindBackspace})...)
	if v.Input != "کا" {
		t.Fatalf("got %q", v.Input)
	}
}

// Tab and the arrows move between screens only while nothing is typed; the
// digits are text once a sentence has begun.
func TestNavigationYieldsToTyping(t *testing.T) {
	v, _, _ := press(View{}, Hooks{}, Key{Kind: KindTab}, Key{Kind: KindRight})
	if v.Screen != ScreenAuthority {
		t.Fatalf("two moves right landed on %d", v.Screen)
	}
	v, _, _ = press(View{}, Hooks{}, append(typed("read 1"), Key{Kind: KindTab}, Key{Kind: KindLeft})...)
	if v.Input != "read 1" || v.Screen != ScreenLedger {
		t.Fatalf("navigation interrupted typing: %+v", v)
	}
	v, _, _ = press(View{}, Hooks{}, typed("3")...)
	if v.Screen != ScreenAuthority || v.Input != "" {
		t.Fatalf("a bare digit did not jump: %+v", v)
	}
}

func TestStem(t *testing.T) {
	for say, want := range map[string]string{
		"write at {address}: {text}": "write at ", "go": "go", "carry {address} to {path}": "carry ",
	} {
		if got := Stem(say); got != want {
			t.Errorf("Stem(%q) = %q, want %q", say, got, want)
		}
	}
}

// What a raw terminal is sent returns the carriage before every new line.
//
// stty raw turns output processing off, so the terminal no longer turns a
// newline into a newline and a carriage return. The first screen a person
// saw on a real terminal came out as a staircase for exactly this reason:
// every row began where the row above it had ended. Render may keep its bare
// newlines, because it is the screen as a file holds it; Frame may not.
func TestTheFrameReturnsTheCarriageBeforeEveryNewLine(t *testing.T) {
	for _, o := range sizes {
		for _, v := range []View{{}, {MenuOpen: true}, {Reply: []string{"written. 8c91d2af"}}} {
			f := Frame(o, v, fixture())
			if !strings.HasPrefix(f, "\x1b[H") {
				t.Fatalf("%dx%d: the frame does not begin by homing the cursor", o.Columns, o.Rows)
			}
			if strings.Contains(strings.ReplaceAll(f, "\r\n", ""), "\n") {
				t.Fatalf("%dx%d: a bare newline was sent to a raw terminal", o.Columns, o.Rows)
			}
			if strings.Count(f, "\r\n") != strings.Count(Render(o, v, fixture()), "\n") {
				t.Fatalf("%dx%d: the frame and the rendering disagree on the number of rows", o.Columns, o.Rows)
			}
			if !strings.HasSuffix(f, "\x1b[J") {
				t.Fatalf("%dx%d: what lies below the last row is not erased", o.Columns, o.Rows)
			}
		}
	}
}

// The plain ways to ask open the list on the screen, as they print it on the
// line surface, and the shell never hears them: asking records nothing.
func TestHelpOpensTheListAndIsNeverSaid(t *testing.T) {
	said := 0
	h := Hooks{Say: func(string) (string, bool, error) { said++; return "", false, nil }, Snapshot: fixture}
	for _, word := range []string{"help", "HELP", "sentences", " ? ", "/"} {
		v, quit, _ := press(View{Input: word}, h, Key{Kind: KindEnter})
		if !v.MenuOpen || v.Input != "" || quit {
			t.Errorf("%q then Enter did not open the list: %+v", word, v)
		}
	}
	if said != 0 {
		t.Fatalf("asking for the list was said to the shell %d times", said)
	}
	if !Asking("Help") || Asking("help me") || Asking("read help") {
		t.Fatal("Asking takes the wrong lines")
	}
}

// Ctrl-D on an empty prompt leaves, as the end of input does anywhere a
// terminal reads a line; with a sentence half typed it does nothing.
func TestCtrlDLeavesOnlyAnEmptyPrompt(t *testing.T) {
	if k, n := decode([]byte{0x04}, false); k.Kind != KindEOF || n != 1 {
		t.Fatalf("Ctrl-D decoded as %v %d", k.Kind, n)
	}
	if _, quit, _ := press(View{}, Hooks{}, Key{Kind: KindEOF}); !quit {
		t.Fatal("Ctrl-D on an empty prompt did not leave")
	}
	v, quit, _ := press(View{}, Hooks{}, append(typed("read ho"), Key{Kind: KindEOF})...)
	if quit || v.Input != "read ho" {
		t.Fatalf("Ctrl-D with text typed: quit=%v %+v", quit, v)
	}
}

// The hint line names both ways to leave, at every width that has one.
func TestTheHintsSayHowToLeave(t *testing.T) {
	for _, w := range []int{30, 47, 60, 79, 115} {
		h := visible(Render(Options{Columns: w + 1, Rows: 40}, View{}, fixture()))
		if !strings.Contains(h, "Ctrl-C leaves") || !strings.Contains(h, "go") {
			t.Errorf("at %d columns the screen does not say how to leave:\n%s", w+1, h)
		}
	}
}

// longAnswer is what "read home" answers on a ledger of twelve notes: more
// lines than any box at 80x24 holds, some of them longer than a row.
func longAnswer() []string {
	out := []string{}
	for i := 1; i <= 12; i++ {
		out = append(out, fmt.Sprintf("— home/n%02d (%08x): note %d, written to be read whole, however narrow the window it is read in happens to be", i, i*2654435761, i))
	}
	return append(out, "12 events at home.")
}

// Every size a person may have, from the rail up: the screen is exactly as
// tall as the window, its last row says how to leave, custody is never cut,
// nothing reaches the last column, and while an answer shows the newest event
// stays on screen.
func TestTheScreenFillsTheWindowAndEndsWithTheHints(t *testing.T) {
	views := map[string]View{
		"plain": {}, "one-line answer": {Reply: []string{"26 events, 1 head, all verified."}},
		"long answer": {Reply: longAnswer()}, "refusal": {Reply: []string{"no — this Rokh is full, so nothing was recorded. To make room, leave and run: rokh grow /path/to/carrier/ledgers/home --to 128M [vessel_full; not recorded]"}, Failed: true},
		"list open": {MenuOpen: true}, "sentence": {Screen: ScreenSentence}, "authority": {Screen: ScreenAuthority},
		"layers": {Screen: ScreenLayers},
	}
	for cols := 34; cols <= 130; cols += 3 {
		for rows := 6; rows <= 40; rows++ {
			o := Options{Columns: cols, Rows: rows}
			for name, v := range views {
				got := strings.Split(Render(o, v, fixture()), "\n")
				where := fmt.Sprintf("%dx%d %s", cols, rows, name)
				if len(got) != rows {
					t.Fatalf("%s: %d rows", where, len(got))
				}
				last := got[rows-1]
				if !strings.Contains(last, "leaves") && !(v.MenuOpen && strings.Contains(last, "closes")) {
					t.Fatalf("%s: the last row does not say how to leave: %q", where, last)
				}
				screen := strings.Join(got, "\n")
				for i, r := range got {
					if width(r) >= cols {
						t.Fatalf("%s: row %d reaches the last column", where, i)
					}
				}
				if !strings.Contains(screen, "custody warm") {
					t.Fatalf("%s: custody was cut:\n%s", where, screen)
				}
				if !v.MenuOpen && v.Screen == ScreenLedger && !strings.Contains(screen, fixture().Events[0].ID) {
					t.Fatalf("%s: the newest event is not on screen:\n%s", where, screen)
				}
				// A short answer, and at the classic size a long refusal, are
				// shown whole: every word of them is on screen.
				if (name == "one-line answer" && rows >= 15) || (name == "refusal" && cols >= 80 && rows >= 24) {
					for _, word := range strings.Fields(v.Reply[0]) {
						if !strings.Contains(screen, word) {
							t.Fatalf("%s: the answer lost %q:\n%s", where, word, screen)
						}
					}
				}
			}
		}
	}
}

// An answer is wrapped at spaces and read whole: every word of it is on one
// row, no row is wider than asked, and a word wider than a row is broken, not
// dropped.
func TestAnAnswerWrapsByWords(t *testing.T) {
	long := strings.Repeat("x", 70)
	for _, w := range []int{10, 30, 76} {
		for _, in := range append(longAnswer(), "short", long+" tail", "write at کارها: نخستین\u200cسطر and more") {
			rows := wrapText(in, w)
			for _, r := range rows {
				if width(r) > w {
					t.Fatalf("width %d: row %q is wider", w, r)
				}
			}
			if got := strings.Join(rows, " "); strings.ReplaceAll(got, " ", "") != strings.ReplaceAll(inline(in), " ", "") {
				t.Fatalf("width %d: %q came back as %q", w, in, got)
			}
			if w >= 30 {
				for _, word := range strings.Fields(inline(in)) {
					if width(word) <= w && !strings.Contains("\n"+strings.Join(rows, "\n")+"\n", word) {
						t.Fatalf("width %d: the word %q was broken: %q", w, word, rows)
					}
				}
			}
		}
	}
}

// The tabs close their gaps and shorten their names before anything is cut.
func TestTheTabsShortenBeforeTheyAreCut(t *testing.T) {
	for _, w := range []int{60, 40, 34} {
		got := textOf(tabs(View{Screen: ScreenAuthority}, w))
		if strings.Contains(got, "…") || !strings.Contains(got, "3 auth") {
			t.Errorf("at %d the tabs read %q", w, got)
		}
	}
}

// A wide window that is short keeps the prompt, the hints and the newest
// event, instead of falling to four rows.
func TestAWideShortWindowKeepsThePromptAndTheNewestEvent(t *testing.T) {
	got := Render(Options{Columns: 120, Rows: 12}, View{}, fixture())
	for _, want := range []string{"›", "leaves", fixture().Events[0].ID, fixture().Events[0].Payload, "custody warm"} {
		if !strings.Contains(got, want) {
			t.Fatalf("120x12 lost %q:\n%s", want, got)
		}
	}
}

func textOf(l line) string {
	var b strings.Builder
	for _, p := range l {
		b.WriteString(p.text)
	}
	return b.String()
}

func keys(kinds ...KeyKind) []Key {
	var out []Key
	for _, k := range kinds {
		out = append(out, Key{Kind: k})
	}
	return out
}

// The cursor moves inside the line and typing goes where it stands; the
// editing keys a terminal user already knows do what they do elsewhere, and
// every step is a whole rune, in any script.
func TestThePromptEditsWhereTheCursorStands(t *testing.T) {
	type c struct {
		name string
		in   []Key
		want string
		back int
	}
	seq := func(parts ...[]Key) []Key {
		var out []Key
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	for _, tc := range []c{
		{"insert in the middle", seq(typed("read hom"), keys(KindLeft, KindLeft), typed("X")), "read hXom", 2},
		{"home then type", seq(typed("read hom"), keys(KindHome), typed("Y")), "Yread hom", 8},
		{"end after moving", seq(typed("read hom"), keys(KindLeft, KindLeft, KindEnd), typed("Z")), "read homZ", 0},
		{"backspace in the middle", seq(typed("read hom"), keys(KindLeft, KindLeft, KindBackspace)), "read om", 2},
		{"delete under the cursor", seq(typed("read hom"), keys(KindHome, KindDelete)), "ead hom", 7},
		{"a Persian letter goes whole", seq(typed("کارها"), keys(KindLeft, KindBackspace)), "کارا", 2},
		{"right after left", seq(typed("ab"), keys(KindLeft, KindLeft, KindRight), typed("-")), "a-b", 1},
		{"ctrl-u", seq(typed("read home"), keys(KindLeft, KindLeft, KindKillStart)), "me", 2},
		{"ctrl-k", seq(typed("read home"), keys(KindLeft, KindLeft, KindKillEnd)), "read ho", 0},
		{"ctrl-w", seq(typed("write at home: some words"), keys(KindKillWord)), "write at home: some ", 0},
	} {
		v, _, _ := press(View{}, Hooks{}, tc.in...)
		if v.Input != tc.want || v.Back != tc.back {
			t.Errorf("%s: %q back %d, want %q back %d", tc.name, v.Input, v.Back, tc.want, tc.back)
		}
	}
}

// Up and Down walk the sentences of this session, and what was being typed
// comes back at the end of the walk.
func TestTheSentencesOfTheSessionComeBack(t *testing.T) {
	h := Hooks{Say: func(string) (string, bool, error) { return "ok", false, nil }, Snapshot: fixture}
	v, _, _ := press(View{}, h, append(typed("see the ledger"), Key{Kind: KindEnter})...)
	v, _, _ = press(v, h, append(typed("read home"), Key{Kind: KindEnter})...)
	v, _, _ = press(v, h, typed("half")...)
	v, _, _ = press(v, h, Key{Kind: KindUp})
	if v.Input != "read home" {
		t.Fatalf("Up gave %q", v.Input)
	}
	v, _, _ = press(v, h, Key{Kind: KindUp}, Key{Kind: KindUp})
	if v.Input != "see the ledger" {
		t.Fatalf("Up, Up, Up gave %q", v.Input)
	}
	v, _, _ = press(v, h, Key{Kind: KindDown}, Key{Kind: KindDown})
	if v.Input != "half" || v.Recalled != 0 {
		t.Fatalf("walking down again gave %q", v.Input)
	}
}

// A paste that holds a line break says nothing to the shell: the break is
// kept in the line, drawn as \n, and Enter is still the person's own key.
func TestAPastedLineBreakIsNeverEnter(t *testing.T) {
	said := 0
	h := Hooks{Say: func(string) (string, bool, error) { said++; return "", false, nil }, Snapshot: fixture}
	in := []Key{{Kind: KindPasteStart}}
	in = append(in, typed("write at home/pasted: one")...)
	in = append(in, Key{Kind: KindEnter})
	in = append(in, typed("write")...)
	in = append(in, Key{Kind: KindEnter}, Key{Kind: KindPasteEnd})
	v, _, changed := press(View{}, h, in...)
	if said != 0 || changed {
		t.Fatalf("a paste was said to the shell %d times", said)
	}
	if v.Input != "write at home/pasted: one\nwrite\n" || v.Pasting {
		t.Fatalf("the paste left %q (pasting %v)", v.Input, v.Pasting)
	}
	if p := textOf(promptLine(v, 60)); strings.ContainsRune(p, '\n') || !strings.Contains(p, `one\nwrite`) {
		t.Fatalf("the pasted break was not drawn as its escape: %q", p)
	}
}

// A line longer than the row shows its end and the cursor after it; a cursor
// moved to the start shows the start.
func TestALongLineScrollsToTheCursor(t *testing.T) {
	long := "write at home/journal: " + strings.Repeat("word ", 20) + "ENDMARK"
	v, _, _ := press(View{}, Hooks{}, typed(long)...)
	p := textOf(promptLine(v, 30))
	if width(p) > 29 || !strings.HasSuffix(p, "ENDMARK█") || !strings.HasPrefix(p, "› …") {
		t.Fatalf("the end of a long line: %q", p)
	}
	v, _, _ = press(v, Hooks{}, Key{Kind: KindHome})
	p = textOf(promptLine(v, 30))
	if width(p) > 29 || !strings.HasPrefix(p, "› █write at") || !strings.HasSuffix(p, "…") {
		t.Fatalf("the start of a long line: %q", p)
	}
	for back := 0; back <= len(long); back++ {
		v.Back = back
		if p := textOf(promptLine(v, 30)); width(p) > 29 || !strings.Contains(p, "█") {
			t.Fatalf("back %d: %q", back, p)
		}
	}
}

// The keys that page through a long answer stay inside it: the first press
// opens it at full height, Up stops at the top, a page is a page, Esc steps
// back out of it and then closes it; with a sentence typed the arrows walk
// the history instead.
func TestPagingStaysInsideTheAnswer(t *testing.T) {
	o := Options{Columns: 80, Rows: 24}
	v := settle(o, View{Reply: longAnswer()}, fixture())
	if !v.Overflow || v.Reading {
		t.Fatalf("a long answer at 80x24: overflow %v reading %v", v.Overflow, v.Reading)
	}
	press1 := func(v View, k KeyKind) View {
		v, _, _ = step(v, Key{Kind: k}, Hooks{})
		return settle(o, v, fixture())
	}
	v = press1(v, KindDown)
	if !v.Reading || v.AnswerAt != 0 {
		t.Fatalf("the first Down: reading %v at %d", v.Reading, v.AnswerAt)
	}
	v = press1(v, KindDown)
	if v.AnswerAt != 1 {
		t.Fatalf("the second Down is at %d", v.AnswerAt)
	}
	v = press1(press1(press1(v, KindUp), KindUp), KindUp)
	if v.AnswerAt != 0 {
		t.Fatalf("Up went past the top: %d", v.AnswerAt)
	}
	v = press1(v, KindPageDown)
	if v.AnswerAt != v.Page-1 {
		t.Fatalf("a page down went to %d with pages of %d", v.AnswerAt, v.Page)
	}
	for i := 0; i < 100; i++ {
		v = press1(v, KindDown)
	}
	last := v.AnswerAt
	if last <= 0 || press1(v, KindDown).AnswerAt != last {
		t.Fatalf("Down went past the end: %d", v.AnswerAt)
	}
	v = press1(v, KindEscape)
	if v.Reading || len(v.Reply) == 0 {
		t.Fatalf("the first Esc: reading %v, answer %d lines", v.Reading, len(v.Reply))
	}
	v = press1(v, KindEscape)
	if len(v.Reply) != 0 {
		t.Fatal("the second Esc did not close the answer")
	}
	typedView := settle(o, View{Reply: longAnswer(), Input: "read", History: []string{"see the ledger"}}, fixture())
	if w := press1(typedView, KindUp); w.Reading || w.Input != "see the ledger" {
		t.Fatalf("with a sentence typed, Up paged the answer: %+v", w)
	}
}

// Every word of a long answer is on screen or reachable with the key the
// screen names, at every size a person may have.
func TestEveryWordOfALongAnswerCanBeRead(t *testing.T) {
	want := map[string]bool{}
	for _, ln := range longAnswer() {
		for _, w := range strings.Fields(ln) {
			want[w] = true
		}
	}
	for _, o := range []Options{{Columns: 80, Rows: 24}, {Columns: 120, Rows: 36}, {Columns: 58, Rows: 22}, {Columns: 40, Rows: 16}, {Columns: 120, Rows: 12}, {Columns: 28, Rows: 8}} {
		v := settle(o, View{Reply: longAnswer()}, fixture())
		seen := map[string]bool{}
		look := func() {
			for _, w := range strings.Fields(Render(o, v, fixture())) {
				seen[w] = true
			}
		}
		look()
		if !strings.Contains(Render(o, v, fixture()), "↓") {
			t.Fatalf("%dx%d: a long answer does not name the key that reads on", o.Columns, o.Rows)
		}
		for i := 0; i < 200; i++ {
			v, _, _ = step(v, Key{Kind: KindDown}, Hooks{})
			v = settle(o, v, fixture())
			look()
		}
		for w := range want {
			if !seen[w] {
				t.Errorf("%dx%d: %q of the answer was never on screen", o.Columns, o.Rows, w)
			}
		}
	}
}

// The environment decides colour: NO_COLOR and a dumb or missing TERM mean
// none, COLORTERM decides 24-bit, a TERM that names 256 colours gets those,
// and every other terminal the sixteen.
func TestColourFollowsWhatTheTerminalSays(t *testing.T) {
	for _, c := range []struct {
		env      map[string]string
		terminal bool
		colour   bool
		depth    int
	}{
		{map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, true, true, DepthTrue},
		{map[string]string{"TERM": "xterm-256color", "COLORTERM": "24bit"}, true, true, DepthTrue},
		{map[string]string{"TERM": "xterm-256color"}, true, true, Depth256},
		{map[string]string{"TERM": "xterm"}, true, true, Depth16},
		{map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": "1"}, true, false, DepthTrue},
		{map[string]string{"TERM": "dumb", "COLORTERM": "truecolor"}, true, false, DepthTrue},
		{map[string]string{}, true, false, DepthTrue},
		{map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, false, false, DepthTrue},
	} {
		colour, depth := colourDepth(func(k string) string { return c.env[k] }, c.terminal)
		if colour != c.colour || (colour && depth != c.depth) {
			t.Errorf("%v on a terminal %v: colour %v depth %d; want %v %d", c.env, c.terminal, colour, depth, c.colour, c.depth)
		}
	}
}

// At every depth the six meanings keep six colours of their own, and a
// terminal that did not say it draws 24-bit colour is never sent it.
func TestTheSixMeaningsStayApartAtEveryDepth(t *testing.T) {
	for _, depth := range []int{Depth256, Depth16} {
		seen := map[string]Role{}
		for r := range Palette {
			code := colourCode(r, depth)
			if other, dup := seen[code]; dup {
				t.Fatalf("depth %d: %v and %v share %s", depth, r, other, code)
			}
			seen[code] = r
		}
		out := Render(Options{Columns: 118, Rows: 36, Color: true, Depth: depth}, View{}, fixture())
		if strings.Contains(out, "38;2;") {
			t.Fatalf("depth %d drew 24-bit colour", depth)
		}
	}
	if colourCode(RolePlace, Depth256) != "38;5;25" || colourCode(RoleBoundary, Depth16) != "31" {
		t.Fatalf("place at 256 is %s, the boundary at 16 is %s", colourCode(RolePlace, Depth256), colourCode(RoleBoundary, Depth16))
	}
}

// A screen is not drawn where there is no terminal to draw it on; the caller
// is told why and the terminal is not touched.
func TestTheScreenIsNotStartedWithoutATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	var ns NoScreen
	if err := Run(r, w, Hooks{}); !errors.As(err, &ns) {
		t.Fatalf("Run on a pipe answered %v", err)
	}
}

// A system event is drawn in the same rows as any event, marked system, with
// Rokh's sentence for what it did and not in quotation marks; the tally counts
// the ledger's own events apart from the person's.
func TestASystemEventIsMarkedOnTheScreen(t *testing.T) {
	for _, o := range []Options{{Columns: 118, Rows: 36}, {Columns: 80, Rows: 24}, {Columns: 120, Rows: 12}} {
		out := Render(o, View{}, fixture())
		var line string
		for _, r := range strings.Split(out, "\n") {
			if strings.Contains(r, "72b4e191") && strings.Contains(r, "rokh.grant") {
				line = r
			}
		}
		if !strings.Contains(line, "system") {
			t.Errorf("%dx%d: the system event is not marked: %q", o.Columns, o.Rows, line)
		}
		if strings.Contains(out, "“writing entrusted") {
			t.Errorf("%dx%d: a system event's sentence is quoted as if somebody wrote it", o.Columns, o.Rows)
		}
		if !strings.Contains(out, "3 system") {
			t.Errorf("%dx%d: the tally does not count the system events apart:\n%s", o.Columns, o.Rows, out)
		}
	}
}

// The room is on screen at the classic size and in the rail, as the vessel
// holds it: free of how much, or full and how room is made.
func TestTheRoomIsOnScreenAtEverySize(t *testing.T) {
	for _, o := range []Options{{Columns: 80, Rows: 24}, {Columns: 118, Rows: 36}, {Columns: 48, Rows: 20}} {
		if out := Render(o, View{}, fixture()); !strings.Contains(out, "58 MB free of 64 MB") {
			t.Errorf("%dx%d does not show the room:\n%s", o.Columns, o.Rows, out)
		}
		if out := Render(o, View{}, full()); !strings.Contains(out, "rokh grow makes room") {
			t.Errorf("%dx%d does not say a full Rokh is full:\n%s", o.Columns, o.Rows, out)
		}
	}
}

// While nothing is typed, the prompt offers the sentence a person is likely
// to say on the view they look at, in the colour of its meaning.
func TestThePromptOffersTheLikelySentence(t *testing.T) {
	for _, c := range []struct {
		v    View
		st   Snapshot
		want string
		role Role
	}{
		{View{}, fixture(), "write", RoleBoundary},
		{View{}, settled(), "read home/journal/today", RoleSettled},
		{View{Screen: ScreenSentence}, fixture(), "write", RoleBoundary},
		{View{Screen: ScreenAuthority}, fixture(), "see the grants", RoleAuthority},
		{View{Screen: ScreenLayers}, fixture(), "read home", RoleSettled},
		{View{}, fresh(), "write at home/journal: my first note", RoleWorking},
		{View{Screen: ScreenSentence}, fresh(), "write at home/journal: my first note", RoleWorking},
	} {
		say, role := suggestion(c.v, c.st)
		if say != c.want || role != c.role {
			t.Errorf("view %d: %q in %v, want %q in %v", c.v.Screen, say, role, c.want, c.role)
		}
		if p := textOf(promptFor(c.v, c.st, 76)); !strings.Contains(p, "try "+c.want) || !strings.Contains(p, "?") {
			t.Errorf("view %d: the prompt reads %q", c.v.Screen, p)
		}
	}
	if p := textOf(promptFor(View{}, Snapshot{Vault: "~/rokh"}, 76)); !strings.Contains(p, "? for the list") {
		t.Errorf("no ledger: the prompt reads %q", p)
	}
}

// settled is the fixture with no sentence waiting.
func settled() Snapshot {
	st := fixture()
	st.Drafts = nil
	return st
}

// The sample that ships with the source, sample.json, is the snapshot Sample
// makes, and a snapshot file with a field the screen does not know is
// refused rather than drawn with a blank.
func TestTheSampleFileIsTheSample(t *testing.T) {
	if *update {
		f, err := os.Create("sample.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteSnapshot(f, Sample()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	f, err := os.Open("sample.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := ReadSnapshot(f)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st, Sample()) {
		t.Fatal("sample.json is not the sample; run with -update if the sample changed on purpose")
	}
	if _, err := ReadSnapshot(strings.NewReader(`{"ledger": "home", "ledgr": "typo"}`)); err == nil {
		t.Fatal("an unknown field was drawn as a blank")
	}
}

// A picture drawn where there is no terminal is printed once, plain, and
// records nothing because there is nothing behind it.
func TestAPictureWithoutATerminalIsPrintedOnce(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	t.Setenv("COLUMNS", "80")
	t.Setenv("LINES", "24")
	if err := Picture(r, w, Sample(), nil); err != nil {
		t.Fatal(err)
	}
	w.Close()
	got := <-done
	if strings.Count(got, "\n") != 24 || strings.Contains(got, "\x1b[") || !strings.Contains(got, "custody warm") {
		t.Fatalf("the picture printed:\n%s", got)
	}
}

// The demo draws every screen of the surface from the sample, at the size it
// is given, in colour and then plain, and says that nothing was opened.
func TestTheDemoDrawsEveryScreen(t *testing.T) {
	var b strings.Builder
	o := Options{Columns: 80, Rows: 24}
	if err := demo(&b, o, []bool{true, false}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, sh := range Shots() {
		for _, how := range []string{"in colour", "plain"} {
			if !strings.Contains(out, "── "+sh.Name+", 80x24, "+how+" ──\n") {
				t.Errorf("the demo did not draw %s %s", sh.Name, how)
			}
		}
	}
	plain := out[strings.Index(out, "── ledger, 80x24, plain ──"):]
	if strings.Contains(plain, "\x1b[") || !strings.Contains(out[:len(out)-len(plain)], "\x1b[") {
		t.Fatal("the colour pass or the plain pass is not what it says")
	}
	if !strings.Contains(out, "nothing was opened or recorded") {
		t.Fatal("the demo does not say it opened nothing")
	}
}
