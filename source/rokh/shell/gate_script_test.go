package shell

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// terminal stands in for the person at the gate: it answers each question
// from a script, in order, and keeps everything the gate said and asked in
// one transcript, as a terminal would show it.
type terminal struct {
	t       *testing.T
	answers []string
	said    strings.Builder
}

func (tm *terminal) Write(p []byte) (int, error) { return tm.said.Write(p) }

func (tm *terminal) answer(prompt string) (string, error) {
	tm.said.WriteString(prompt)
	if len(tm.answers) == 0 {
		tm.t.Fatalf("the gate asked %q and the script had no answer left:\n%s", prompt, tm.said.String())
	}
	a := tm.answers[0]
	tm.answers = tm.answers[1:]
	tm.said.WriteString("\n")
	return a, nil
}

func scripted(t *testing.T, answers ...string) *terminal {
	tm := &terminal{t: t, answers: answers}
	oldAsk, oldVisible := ask, askVisible
	ask, askVisible = tm.answer, tm.answer
	t.Cleanup(func() { ask, askVisible = oldAsk, oldVisible })
	return tm
}

// The gate says what a Rokh is before it asks anything; two passphrases that
// differ are asked for again; a name the sentences could not say back is
// refused at once with the reason and asked for again; and what is sealed is
// said as it is. Nothing in the transcript is Persian or a raw line break.
func TestTheGateExplainsItselfAndAsksAgain(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "new")
	tm := scripted(t, "y", "", "n", "one", "two", "pass", "pass", "my name", "a/b", "..", "home")
	s, err := enter(tm, vault, "", "")
	if err != nil {
		t.Fatalf("the gate ended: %v\n%s", err, tm.said.String())
	}
	defer s.closeAll()
	said := tm.said.String()
	firstQuestion := strings.Index(said, "[y/N]")
	if i := strings.Index(said, "A Rokh is a folder that keeps"); i < 0 || i > firstQuestion {
		t.Fatalf("the gate asked before it said what a Rokh is:\n%s", said)
	}
	for _, want := range []string{
		"nothing shows while you type",
		"the two passphrases differ; type them again.\nchoose a passphrase",
		"a name is one word: it cannot hold a space.\nname this ledger",
		"a name cannot hold a slash",
		"a name cannot begin with a dot",
		"can be seen by anyone who sees the folder",
		`Type "?" to see what you can say.`,
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the gate did not say %q:\n%s", want, said)
		}
	}
	for _, r := range said {
		if unicode.Is(unicode.Arabic, r) {
			t.Fatalf("the gate said something in Arabic script:\n%s", said)
		}
	}
	if regexp.MustCompile(`\\n`).MatchString(said) {
		t.Fatalf("the gate showed a raw line break:\n%s", said)
	}
	if s.current == nil || s.current.name != "home" {
		t.Fatalf("the Rokh was not made with the name home: %+v", s.current)
	}
	g, _ := s.current.led.Get(s.current.led.Genesis())
	if strings.Contains(s.describePayload(s.current, g), `\n`) {
		t.Fatalf("the first event is shown with a raw line break: %q", s.describePayload(s.current, g))
	}
}

// Three pairs that differ make nothing, not even the folder.
func TestThreeMismatchesMakeNothing(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "new")
	tm := scripted(t, "y", "", "n", "a", "b", "c", "d", "e", "f")
	if _, err := enter(tm, vault, "", ""); err == nil || !strings.Contains(err.Error(), "nothing was made") {
		t.Fatalf("three mismatches ended with %v", err)
	}
	if _, err := os.Stat(vault); !os.IsNotExist(err) {
		t.Fatalf("the folder was made anyway: %v", err)
	}
}

// The first event is shown by its first line, with a word for the line under
// it, and never with a line break in the middle of a sentence.
func TestTheFirstEventIsOneLine(t *testing.T) {
	for payload, want := range map[string]string{
		"open a new ledger named home":                    "open a new ledger named home",
		"open a new ledger named home\nreserving 2 GB":    "open a new ledger named home  (and the room set aside)",
		"open a new ledger named home\nsomething else":    "open a new ledger named home  (and more lines)",
		"open a new ledger named home\u202e\nreserving 1": "open a new ledger named home\\u202e  (and the room set aside)",
	} {
		if got := genesisLine([]byte(payload)); got != want {
			t.Errorf("genesisLine(%q) = %q, want %q", payload, got, want)
		}
	}
}

// Every name the gate accepts is one the parser takes back as that name.
func TestAGateNameIsOneTheSentencesCanSay(t *testing.T) {
	for _, name := range []string{"home", "Home2", "خانه", "نوشته\u200cها", "a.b"} {
		if why := nameProblem(name); why != "" {
			t.Errorf("%q was refused: %s", name, why)
		}
	}
	for _, name := range []string{"", "my name", "a/b", `a\b`, ".", "..", ".hidden", "tab\there", "bell\x07", strings.Repeat("x", 65)} {
		if nameProblem(name) == "" {
			t.Errorf("%q was accepted", name)
		}
	}
}
