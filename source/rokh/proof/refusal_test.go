package proof

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// An event whose parent nobody has seen is not refused and not kept: "has not
// arrived" and "never was" are two different answers and they get two
// different words. The door says ancestry_unproven (v1's name for it,
// contract B7 and B8), records nothing, and lets go of the bytes at once — so
// five hundred offers of events with parents out of nowhere leave the view
// exactly the size it was, and the five hundredth costs what the first did.
// When the parent finally arrives, the very same child is offered again and
// is recorded, because a pending verdict was never a verdict.
//
//	— T5, T5.1, T3.3, T12.3, T10.3, T8.5
func TestAnUnknownAncestryIsPendingNotRefusedAndCostsNothingToHold(t *testing.T) {
	w := newFastWorld(t)
	led := w.load()
	s := daemon.New(w.car, led, w.at(daemon.Options{AllowSign: true}))

	strayParent := func() frame.ID {
		var id frame.ID
		if _, err := rand.Read(id[:]); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// One offer, looked at closely.
	stray := strayParent()
	lone := w.sign(w.root, nil, []frame.ID{stray}, "home/journal", "note", []byte("from nowhere"))
	r := notRecorded(t, ask(t, s, map[string]any{"op": "append", "raw": hexOf(lone.Raw)}), "ancestry_unproven")
	if msg, _ := r["error"].(string); !strings.Contains(msg, stray.Short()) {
		t.Fatalf("the sentence should say what is missing, the parent %s: %q", stray.Short(), msg)
	}
	if led.Has(lone.ID) {
		t.Fatal("the view kept an event it did not record")
	}
	if _, _, pending := led.Tally(); pending != 0 {
		t.Fatalf("pending = %d, want 0", pending)
	}
	if w.holds(t, lone.ID) {
		t.Fatal("a pending event reached the carrier")
	}

	// Five hundred of them, and the view does not grow.
	const n = 500
	before := led.Len()
	offers := make([]string, n)
	for i := range offers {
		e := w.sign(w.root, nil, []frame.ID{strayParent()}, "home/journal", "note", []byte("from nowhere"))
		offers[i] = hexOf(e.Raw)
	}
	start := time.Now()
	var firstBatch, lastBatch time.Duration
	batchStart := start
	for i, raw := range offers {
		notRecorded(t, ask(t, s, map[string]any{"op": "append", "raw": raw}), "ancestry_unproven")
		switch i {
		case n/10 - 1:
			firstBatch = time.Since(batchStart)
		case n - n/10 - 1:
			batchStart = time.Now()
		case n - 1:
			lastBatch = time.Since(batchStart)
		}
	}
	total := time.Since(start)
	if got := led.Len(); got != before {
		t.Fatalf("%d offers grew the view from %d to %d", n, before, got)
	}
	if _, _, pending := led.Tally(); pending != 0 {
		t.Fatalf("after %d offers, pending = %d", n, pending)
	}
	if total > 2*time.Second {
		t.Fatalf("%d offers took %v; holding what was never recorded is costing something", n, total)
	}
	// The last tenth must not cost more than the first tenth does, allowing
	// for the noise of a laptop. A view that kept the offers would show here.
	if lastBatch > 4*firstBatch+20*time.Millisecond {
		t.Fatalf("the last %d offers took %v against the first %d at %v", n/10, lastBatch, n/10, firstBatch)
	}

	// The parent arrives, and the child that was pending is now an event.
	head := refHeadOf(t, w, "main")
	parent := w.sign(w.root, nil, []frame.ID{head}, "home/journal", "note", []byte("the parent, late"))
	child := w.sign(w.root, nil, []frame.ID{parent.ID}, "home/journal", "note", []byte("the child, early"))

	notRecorded(t, ask(t, s, map[string]any{"op": "append", "raw": hexOf(child.Raw)}), "ancestry_unproven")
	if led.Has(child.ID) {
		t.Fatal("the child was kept while its parent was unknown")
	}
	recorded(t, ask(t, s, map[string]any{"op": "append", "raw": hexOf(parent.Raw)}), "the parent arriving")
	recorded(t, ask(t, s, map[string]any{"op": "append", "raw": hexOf(child.Raw)}), "the child offered again")
	if led.State(child.ID) != ledger.Accepted {
		t.Fatal("with its ancestry present, the child is still not accepted")
	}
	back := w.load()
	if !back.Has(parent.ID) || !back.Has(child.ID) {
		t.Fatal("the pair is not on the carrier")
	}
}

// Every refusal carries a code a program can branch on, a sentence a person
// can read, and — when the request could have recorded something — the one
// word that says it did not. This is the whole table of ways to be told no.
//
//	— T12.3, T8.5, T10.3, T10.4
func TestEveryRefusalHasAStableCodeAndAWord(t *testing.T) {
	w := newFastWorld(t)
	led := w.load()
	s := daemon.New(w.car, led, w.at(daemon.Options{AllowSign: true}))
	ro := daemon.New(w.car, w.load(), w.at(daemon.Options{ReadOnly: true, AllowSign: true}))
	quiet := daemon.New(w.car, w.load(), w.at(daemon.Options{AllowSign: false}))

	// Ground to refuse against: one recording under a name, and one harness
	// that has said what it can do.
	recorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/a",
		"verb": "note", "message": "one", "attempt": "proof:first"}), "the first recording")
	mustOK(t, ask(t, s, map[string]any{"op": "bind", "namespace": "gateway", "version": "0.1.0",
		"can": []any{"gateway.plan"}, "unknown": "refuse", "repeat": "duplicates",
		"retry": "never-retry", "compensate": "irreversible", "ending": "ask-a-person"}), "bind")
	stale := []any{w.gen.ID.String()}

	writing := map[string]bool{"write": true, "append": true, "intent": true, "outcome": true}
	cases := []struct {
		why  string
		door *daemon.Server
		req  map[string]any
		code string
	}{
		{"an op nobody answers", s,
			map[string]any{"op": "no-such-op"}, "unknown_op"},
		{"hex that is not hex", s,
			map[string]any{"op": "append", "raw": "zz"}, "bad_request"},
		{"an id that is not an id", s,
			map[string]any{"op": "get", "id": "cafe"}, "bad_request"},
		{"a cursor that is not a name", s,
			map[string]any{"op": "log", "after": "nope"}, "bad_request"},
		{"a door that does not write", ro,
			map[string]any{"op": "write", "address": "home/a", "message": "x"}, "read_only"},
		{"a door that does not write, taking signed bytes", ro,
			map[string]any{"op": "append", "raw": "00"}, "read_only"},
		{"a door that does not sign", quiet,
			map[string]any{"op": "write", "address": "home/a", "message": "x"}, "signing_disabled"},
		{"a door that does not sign, asked for a receipt", quiet,
			map[string]any{"op": "intent", "address": "home", "doing": "x", "witness": witness()}, "signing_disabled"},
		{"a key this carrier does not hold", s,
			map[string]any{"op": "write", "address": "home/a", "message": "x", "key": "nobody"}, "key_unknown"},
		{"heads the ledger has moved past", s,
			map[string]any{"op": "write", "address": "home/a", "message": "x", "expect_heads": stale}, "precondition_failed"},
		{"one name over two different requests", s,
			map[string]any{"op": "write", "address": "home/a", "verb": "note",
				"message": "something else", "attempt": "proof:first"}, "attempt_conflict"},
		{"a verb the harness never declared", s,
			map[string]any{"op": "write", "address": "gateway/work", "verb": "gateway.improvise",
				"message": "x"}, "harness_refused"},
		{"a receipt at the harness's root, written as a plain note", s,
			map[string]any{"op": "write", "address": "gateway", "verb": "intent", "message": "x"}, "harness_refused"},
		{"an ending for a beginning nobody recorded", s,
			map[string]any{"op": "outcome", "address": "home", "intent": strings.Repeat("ab", 32),
				"outcome": "done", "saying": "x", "witness": witness()}, "receipt_refused"},
		{"an intent that cannot show its witnesses", s,
			map[string]any{"op": "intent", "address": "home", "doing": "x",
				"witness": map[string]any{"origin": "only this one"}}, "receipt_refused"},
		{"a name too long to be a name", s,
			map[string]any{"op": "write", "address": "home/a", "message": "x",
				"attempt": strings.Repeat("x", 129)}, "attempt_invalid"},
		{"an event that names another ledger's anchor", s,
			map[string]any{"op": "append", "raw": hexOf(elsewhere(t).Raw)}, "not_accepted"},
	}

	accepted := func() int {
		a, _, _ := w.load().Tally()
		return a
	}
	before := accepted()
	for _, c := range cases {
		r := ask(t, c.door, c.req)
		op, _ := c.req["op"].(string)
		if writing[op] {
			notRecorded(t, r, c.code)
		} else {
			refused(t, r, c.code)
		}
		if got := accepted(); got != before {
			t.Fatalf("%s: the carrier gained an event while refusing (%d -> %d)", c.why, before, got)
		}
	}

	// A line that is not JSON at all never reaches an op, and is still told
	// what is wrong in the same shape.
	for _, broken := range []string{"{", "", "not json", `{"op":`, `{"op":"status"`} {
		r := s.Handle([]byte(broken))
		if broken == "" {
			continue // an empty line is skipped by the session loop
		}
		refused(t, r, "bad_request")
	}
	if got := accepted(); got != before {
		t.Fatalf("broken lines changed the carrier (%d -> %d)", before, got)
	}
}

// elsewhere signs a well-formed event belonging to another ledger entirely.
func elsewhere(t *testing.T) event.Signed {
	t.Helper()
	other := newFastWorld(t)
	return other.sign(other.root, nil, []frame.ID{other.gen.ID}, "home/a", "note", []byte("another ledger"))
}

// A failure is an ending and gets a receipt like any other. So does an ending
// nobody ever learned: "unknown" is not a softer "failed", and both are
// recorded rather than left out. Before the result the intent is open, after
// it the intent is not, and an ending that cannot show all six witnesses is
// refused with a code and records nothing at all.
//
//	— T10.1, T10.2, T10.3, T10.4, T12.3
func TestFailureAndTheEndingNobodyLearnedAreBothRecorded(t *testing.T) {
	w := newFastWorld(t)
	s := daemon.New(w.car, w.load(), w.at(daemon.Options{AllowSign: true}))

	open := func() []string {
		r := mustOK(t, ask(t, s, map[string]any{"op": "receipts", "address": "home"}), "receipts")
		rows, _ := r["open"].([]map[string]any)
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row["id"].(string))
		}
		return out
	}
	accepted := func() int {
		a, _, _ := w.load().Tally()
		return a
	}

	for _, ending := range []string{"failed", "unknown"} {
		t.Run(ending, func(t *testing.T) {
			in := recorded(t, ask(t, s, map[string]any{"op": "intent", "address": "home/work",
				"doing": "a step that will end " + ending, "witness": witness()}), "intent")
			id := in["id"].(string)

			listed := open()
			if !containsString(listed, id) {
				t.Fatalf("before its result the intent is not listed open: %v", listed)
			}

			// An ending that cannot account for itself is refused, and nothing
			// is written for it.
			before := accepted()
			notRecorded(t, ask(t, s, map[string]any{"op": "outcome", "address": "home/work",
				"intent": id, "outcome": ending, "saying": "short of a witness",
				"witness": map[string]any{"origin": "the proof suite", "authority": "the root key",
					"audience": "this test", "state": "synthetic"}}), "receipt_refused")
			if got := accepted(); got != before {
				t.Fatalf("an incomplete ending recorded something (%d -> %d)", before, got)
			}
			if listed := open(); !containsString(listed, id) {
				t.Fatal("a refused ending closed the receipt anyway")
			}

			out := recorded(t, ask(t, s, map[string]any{"op": "outcome", "address": "home/work",
				"intent": id, "outcome": ending, "saying": "said out loud", "witness": witness()}), "outcome")
			if out["outcome"] != ending {
				t.Fatalf("the answer does not say which ending: %v", out)
			}
			if got := accepted(); got != before+1 {
				t.Fatalf("the ending recorded %d events, want 1", got-before)
			}
			if listed := open(); containsString(listed, id) {
				t.Fatalf("after its result the intent is still listed open: %v", listed)
			}

			// And it is on the carrier, readable by anyone who reopens it.
			outID, err := frame.ParseID(out["id"].(string))
			if err != nil {
				t.Fatal(err)
			}
			back := w.load()
			if back.State(outID) != ledger.Accepted {
				t.Fatal("the recorded ending is not on the carrier")
			}
			e, _ := back.Get(outID)
			if !strings.Contains(string(e.Event.Payload), ending) {
				t.Fatalf("the recorded ending does not say %q: %s", ending, e.Event.Payload)
			}
		})
	}

	// An ending with no beginning is refused whatever it claims.
	notRecorded(t, ask(t, s, map[string]any{"op": "outcome", "address": "home/work",
		"intent": w.gen.ID.String(), "outcome": "done", "saying": "x", "witness": witness()}), "receipt_refused")
}

func containsString(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
