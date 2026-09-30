//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"strings"
	"testing"

	"rokh/ledger"
)

// A harness binds itself before Rokh will carry its meaning, and the covenant
// has four clauses: its own space for addresses and verbs, its version,
// everything it can do, and what it does with a verb it has never heard.
// Three of four binds nothing.
//
//	— T11.10, T13.5
func TestAHarnessBindsFourClausesOrNothing(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	full := map[string]any{
		"op": "bind", "repeat": "idempotent", "retry": "never-retry",
		"compensate": "compensable", "ending": "close-unknown",
		"namespace": "clerk", "version": "0.1",
		"can": []any{"clerk.ask", "clerk.answer"}, "unknown": "refuse",
	}
	mustOK(t, ask(t, s, full), "bind")

	for name, missing := range map[string]string{
		"no version": "version", "no list": "can", "no answer": "unknown",
	} {
		req := map[string]any{"op": "bind", "repeat": "idempotent", "retry": "never-retry",
			"compensate": "compensable", "ending": "close-unknown",
			"namespace": "other-" + missing,
			"version":   "1", "can": []any{"other-" + missing + ".do"}, "unknown": "refuse"}
		delete(req, missing)
		if r := ask(t, s, req); r["ok"] == true {
			t.Errorf("%s: bound anyway", name)
		}
	}
	// And a second harness cannot take a space that is taken.
	again := map[string]any{"op": "bind", "repeat": "idempotent", "retry": "never-retry",
		"compensate": "compensable", "ending": "close-unknown",
		"namespace": "clerk", "version": "0.2",
		"can": []any{"clerk.ask"}, "unknown": "ignore"}
	if r := ask(t, s, again); r["ok"] == true {
		t.Fatal("two harnesses took one space")
	}
}

// The core weighs bytes, signature and authority. It does not know what a verb
// means and does not guess: inside a bound harness's own space, a verb the
// harness did not declare gets the answer the harness declared, and nothing
// else happens.
//
//	— T11.10, T13.5, T12.3
func TestAnUnknownVerbGetsTheAnswerTheHarnessDeclared(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	mustOK(t, ask(t, s, map[string]any{
		"op": "bind", "repeat": "idempotent", "retry": "never-retry",
		"compensate": "compensable", "ending": "close-unknown",
		"namespace": "clerk", "version": "0.1",
		"can": []any{"clerk.ask", "clerk.answer"}, "unknown": "refuse",
	}), "bind")

	// A verb it declared: written.
	resp := mustOK(t, ask(t, s, map[string]any{
		"op": "write", "address": "clerk/desk", "verb": "clerk.ask", "message": "what is this",
	}), "declared verb")
	if resp["state"] != "accepted" {
		t.Fatalf("a declared verb was not written: %v", resp)
	}
	before := f.led.Len()

	// A verb it did not: refused, and the ledger does not grow.
	r := ask(t, s, map[string]any{
		"op": "write", "address": "clerk/desk", "verb": "clerk.rummage", "message": "x",
	})
	if r["ok"] == true {
		t.Fatal("an undeclared verb was written into the harness's own space")
	}
	if msg, _ := r["error"].(string); !strings.Contains(msg, "clerk") {
		t.Fatalf("the refusal does not say whose space it is: %v", r["error"])
	}
	if f.led.Len() != before {
		t.Fatal("a refused verb grew the ledger")
	}

	// Outside its space, the harness has no say at all.
	out := mustOK(t, ask(t, s, map[string]any{
		"op": "write", "address": "home/journal", "verb": "note", "message": "mine",
	}), "outside the space")
	if out["state"] != "accepted" {
		t.Fatalf("an ordinary write was caught by somebody else's covenant: %v", out)
	}
}

// The other honest answer: let it pass, claiming nothing about it. A harness
// that declares this is saying "not mine to judge", and the ledger does not
// record anything on its behalf.
//
//	— T11.10, T12.3
func TestAHarnessThatIgnoresAnUnknownVerbRecordsNothing(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	mustOK(t, ask(t, s, map[string]any{
		"op": "bind", "repeat": "idempotent", "retry": "never-retry",
		"compensate": "compensable", "ending": "close-unknown",
		"namespace": "quiet", "version": "1",
		"can": []any{"quiet.note"}, "unknown": "ignore",
	}), "bind")

	before := f.led.Len()
	r := mustOK(t, ask(t, s, map[string]any{
		"op": "write", "address": "quiet/corner", "verb": "quiet.shout", "message": "x",
	}), "ignored verb")
	if r["ignored"] != true {
		t.Fatalf("an ignored verb was not reported as ignored: %v", r)
	}
	if r["id"] != nil {
		t.Fatal("an ignored verb produced an event name")
	}
	if f.led.Len() != before {
		t.Fatal("an ignored verb grew the ledger")
	}
}

// A reader can see which meanings are answered for here, and by which version.
// Meaning changes between versions, so the version travels with the binding.
//
//	— T11.10
func TestWhoIsBoundAndAtWhatVersionIsReadable(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	mustOK(t, ask(t, s, map[string]any{
		"op": "bind", "repeat": "idempotent", "retry": "never-retry",
		"compensate": "compensable", "ending": "close-unknown",
		"namespace": "clerk", "version": "0.7",
		"can": []any{"clerk.ask"}, "unknown": "refuse",
	}), "bind")

	r := mustOK(t, ask(t, s, map[string]any{"op": "bound"}), "bound")
	hs, ok := r["harnesses"].([]map[string]any)
	if !ok || len(hs) != 1 {
		t.Fatalf("expected one bound harness, got %v", r["harnesses"])
	}
	h := hs[0]
	if h["namespace"] != "clerk" || h["version"] != "0.7" {
		t.Fatalf("the binding did not survive: %v", h)
	}
	if h["unknown"] != "refuse" {
		t.Fatalf("the answer for an unknown verb did not survive: %v", h)
	}
	_ = ledger.Accepted
}
