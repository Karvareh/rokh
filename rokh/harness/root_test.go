package harness

import (
	"errors"
	"testing"

	"rokh/event"
)

// The harness's space is two places, not one: its own root, and the subject
// space of its verbs beneath it. At the exact root the two words of the
// receipt ritual are admitted and nothing else — the root is the harness
// itself, and its verbs act under it. Beneath the root the covenant governs
// as before, and a receipt is refused there: it is written at the root and
// nowhere under it.
//
//	— T11.11, T11.10, T10.1
func TestTheRootIsTheHarnessOwnAndTheSubtreeItsVerbs(t *testing.T) {
	c, err := clerk().Bind()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		addr, verb string
		want       Reading
	}{
		{"clerk", "intent", Shelf},
		{"clerk", "outcome", Shelf},
		{"clerk", "clerk.ask", Refused}, // the root is not where its verbs act
		{"clerk", "clerk.rummage", Refused},
		{"clerk/desk", "intent", Refused}, // a receipt belongs at the root
		{"clerk/desk", "outcome", Refused},
		{"clerk/desk", "clerk.ask", Known},
		{"clerk/desk", "clerk.rummage", Refused},
		{"home", "intent", NotMine},
		{"narine", "intent", NotMine}, // a longer name is another name
	} {
		if got := c.Read(event.Event{Address: tc.addr, Verb: tc.verb}); got != tc.want {
			t.Errorf("%q at %q read as %s; want %s", tc.verb, tc.addr, got, tc.want)
		}
	}

	// "Ignore" is an answer about unknown verbs in the verbs' space. A
	// receipt under the root is not an unknown verb; it is in the wrong
	// place, and the covenant's answer to the unknown does not reach it.
	q := clerk()
	q.Unknown = Ignore
	q, err = q.Bind()
	if err != nil {
		t.Fatal(err)
	}
	if got := q.Read(event.Event{Address: "clerk/desk", Verb: "intent"}); got != Refused {
		t.Fatalf("an ignore-covenant let a misplaced receipt through as %s", got)
	}
	if got := q.Read(event.Event{Address: "clerk/desk", Verb: "clerk.rummage"}); got != Ignored {
		t.Fatalf("an ignore-covenant stopped ignoring unknown verbs: %s", got)
	}
}

// Intent and outcome are the ritual's words. A covenant that declares them as
// verbs of its own is refused at binding, because the same word at one
// address would then mean two things — and the whole point of the receipt
// is that it means one.
//
//	— T11.11
func TestTheRitualsWordsAreNotAHarnessesToDeclare(t *testing.T) {
	for _, v := range []string{"clerk.intent", "clerk.outcome"} {
		c := clerk()
		c.Can = append(c.Can, v)
		if _, err := c.Bind(); !errors.Is(err, ErrRitualWord) {
			t.Errorf("%q was accepted as a verb of the covenant (%v)", v, err)
		}
	}
	// And a verb that merely contains the word is its own verb.
	c := clerk()
	c.Can = append(c.Can, "clerk.intention")
	if _, err := c.Bind(); err != nil {
		t.Fatalf("a verb of the harness's own was refused for resembling a ritual word: %v", err)
	}
}
