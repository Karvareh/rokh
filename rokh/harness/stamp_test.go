package harness

import (
	"encoding/json"
	"testing"

	"rokh/event"
)

// The version is bound to the event itself, not to a table somewhere that says
// which version was running that week. It goes inside the payload, which is
// inside the bytes the signature covers and the bytes the name is the hash of —
// so a reader years later, holding only the event, can still say which
// version's meaning to read it under.
//
//	— T11.10, T3.2
func TestTheVersionIsInsideTheSignedBytesNotBesideThem(t *testing.T) {
	c, err := Covenant{Namespace: "clerk", Version: "0.7",
		Can: []string{"clerk.ask"}, Unknown: Refuse}.Bind()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := c.Stamps("clerk.ask.v1", map[string]any{"question": "when?"})
	if err != nil {
		t.Fatal(err)
	}
	s := StampOf(payload)
	if s.Version != "0.7" || s.Type != "clerk.ask.v1" {
		t.Fatalf("the stamp reads %+v", s)
	}
	// The rest of the payload survived the stamping.
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if body["question"] != "when?" {
		t.Fatal("stamping lost the payload")
	}
	// The signature covers it, because it is the payload: change the version
	// and you have different bytes, hence a different name.
	e := event.Event{Address: "clerk/desk", Verb: "clerk.ask", Payload: payload}
	other, err := c.Stamps("clerk.ask.v1", map[string]any{"question": "when?"})
	if err != nil {
		t.Fatal(err)
	}
	if string(e.Payload) != string(other) {
		t.Fatal("the same value stamped twice gave different bytes")
	}

	// A harness may only stamp inside its own space.
	if _, err := c.Stamps("elsewhere.ask.v1", map[string]any{}); err == nil {
		t.Fatal("a harness stamped a type outside its namespace")
	}
}

// Address and verb say whose event it is. The type and version say what it is.
// A payload stamped by a different version may mean something this version does
// not implement, and a payload of another type is not this act at all — so a
// stamp that does not match is answered exactly as an unknown verb is: refuse
// or ignore, whichever the harness declared, and never a guess.
//
// It is never rejection. Rejection belongs to the ledger and means the event
// never was; this only declines to pass it through the harness's aperture.
//
//	— T11.10, T10.6, T12.3, T3.3
func TestAStampFromAnotherVersionGetsTheDeclaredAnswerNotAGuess(t *testing.T) {
	refusing, err := Covenant{Namespace: "clerk", Version: "0.7",
		Can: []string{"clerk.ask"}, Unknown: Refuse}.Bind()
	if err != nil {
		t.Fatal(err)
	}
	ignoring := refusing
	ignoring.Unknown = Ignore

	mine, err := refusing.Stamps("clerk.ask.v1", map[string]any{"q": 1})
	if err != nil {
		t.Fatal(err)
	}
	good := event.Event{Address: "clerk/desk", Verb: "clerk.ask", Payload: mine}
	if r := refusing.Reads(good, "clerk.ask.v1"); r != Known {
		t.Fatalf("its own stamped payload read as %s", r)
	}

	older, err := Covenant{Namespace: "clerk", Version: "0.6",
		Can: []string{"clerk.ask"}, Unknown: Refuse}.Bind()
	if err != nil {
		t.Fatal(err)
	}
	stale, err := older.Stamps("clerk.ask.v1", map[string]any{"q": 1})
	if err != nil {
		t.Fatal(err)
	}
	fromOlder := event.Event{Address: "clerk/desk", Verb: "clerk.ask", Payload: stale}
	if r := refusing.Reads(fromOlder, "clerk.ask.v1"); r != Refused {
		t.Fatalf("an older version's payload read as %s", r)
	}
	if r := ignoring.Reads(fromOlder, "clerk.ask.v1"); r != Ignored {
		t.Fatalf("a harness that declared ignore answered %s", r)
	}
	// Neither answer is "known": an unknown stamp never becomes a known one.
	for _, c := range []Covenant{refusing, ignoring} {
		if c.Reads(fromOlder, "clerk.ask.v1") == Known {
			t.Fatal("a foreign stamp was guessed into a known meaning")
		}
	}

	// A payload of another type, at the right address and verb, is not this act.
	wrongType, err := refusing.Stamps("clerk.answer.v1", map[string]any{"a": 2})
	if err != nil {
		t.Fatal(err)
	}
	crossed := event.Event{Address: "clerk/desk", Verb: "clerk.ask", Payload: wrongType}
	if r := refusing.Reads(crossed, "clerk.ask.v1"); r != Refused {
		t.Fatalf("a payload of another type read as %s", r)
	}

	// And an unstamped payload is not this harness's business either.
	bare := event.Event{Address: "clerk/desk", Verb: "clerk.ask", Payload: []byte(`{"q":1}`)}
	if r := refusing.Reads(bare, "clerk.ask.v1"); r != Refused {
		t.Fatalf("an unstamped payload read as %s", r)
	}

	// Outside the space, none of this applies: it is simply not its event.
	elsewhere := event.Event{Address: "archive/box", Verb: "clerk.ask", Payload: mine}
	if r := refusing.Reads(elsewhere, "clerk.ask.v1"); r != NotMine {
		t.Fatalf("an event outside the space read as %s", r)
	}
	// The events themselves were never altered by being read.
	if string(good.Payload) != string(mine) {
		t.Fatal("reading changed the event")
	}
}
