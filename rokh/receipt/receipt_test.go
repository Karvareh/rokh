package receipt

import (
	"errors"
	"strings"
	"testing"

	"rokh/frame"
)

func full() Witness {
	return Witness{
		Origin: "the owner, at the shell", Authority: "grant 4f2a",
		Audience: "the archive at home/box", Receipt: "",
		State: "not started", WayBack: "take it back with the same grant",
	}
}

// Every piece of work opens a receipt: intent before the step, result after
// it. Both are events, and the second names the first as its parent — that
// link is the pairing, and without it a result is a claim with no question in
// front of it.
//
//	— T10, T10.1
func TestIntentBeforeTheStepAndResultAfterIt(t *testing.T) {
	w := full()
	in, err := OpenIntent("home/work", Intent{Doing: "copy the archive", Witness: w})
	if err != nil {
		t.Fatal(err)
	}
	if in.Verb != VerbIntent {
		t.Fatalf("the first event is not an intent: %q", in.Verb)
	}
	if len(in.Parents) != 0 {
		t.Fatal("an intent parents itself on the ledger, not on a receipt")
	}

	// The intent, once written, has a name; the result names it.
	id := frame.Hash(in.Payload)
	rw := w
	rw.Receipt, rw.State = id.String(), "copied"
	out, err := CloseWith("home/work", id, Result{Outcome: Done, Saying: "all of it", Witness: rw})
	if err != nil {
		t.Fatal(err)
	}
	if out.Verb != VerbOutcome {
		t.Fatalf("the second event is not an outcome: %q", out.Verb)
	}
	if len(out.Parents) != 1 || out.Parents[0] != id {
		t.Fatal("the result does not name the intent as its parent")
	}
}

// A result that does not name its intent is refused, and so is one whose
// witness names a different intent than its parent does.
//
//	— T10.1
func TestAResultMustNameTheIntentItAnswers(t *testing.T) {
	w := full()
	if _, err := CloseWith("home/work", frame.ID{}, Result{Outcome: Done, Witness: w}); !errors.Is(err, ErrOrphanResult) {
		t.Fatalf("a result with no intent was accepted: %v", err)
	}
	real := frame.Hash([]byte("the intent"))
	other := frame.Hash([]byte("some other intent"))
	w.Receipt = other.String()
	if _, err := CloseWith("home/work", real, Result{Outcome: Done, Witness: w}); !errors.Is(err, ErrOrphanResult) {
		t.Fatalf("a result answering one intent while parented on another was accepted: %v", err)
	}
}

// Six witnesses of every effect: origin, authority, audience, receipt, state,
// way back. Five is not four fewer than nine — it is an effect that cannot be
// accounted for, and it is refused.
//
//	— T10.2
func TestAnEffectMustShowAllSixWitnesses(t *testing.T) {
	for _, drop := range []string{"origin", "authority", "audience", "state", "wayBack"} {
		w := full()
		switch drop {
		case "origin":
			w.Origin = ""
		case "authority":
			w.Authority = ""
		case "audience":
			w.Audience = ""
		case "state":
			w.State = ""
		case "wayBack":
			w.WayBack = ""
		}
		_, err := OpenIntent("home/work", Intent{Doing: "x", Witness: w})
		if !errors.Is(err, ErrIncompleteWitness) {
			t.Errorf("an intent missing %s was accepted: %v", drop, err)
			continue
		}
		if !strings.Contains(err.Error(), drop) {
			t.Errorf("the error does not say which witness is missing: %v", err)
		}
	}
	if m := full().Missing(); len(m) != 1 || m[0] != "receipt" {
		t.Fatalf("Missing does not report exactly what is absent: %v", m)
	}
	// The one exception, and the reason for it: an intent cannot name its own
	// receipt, because its name is the hash of bytes that would have to
	// contain it. It owes the other five, and the result owes all six.
	if m := full().MissingForIntent(); len(m) != 0 {
		t.Fatalf("an intent was asked for something it cannot know: %v", m)
	}
	w := full()
	w.Origin = ""
	if m := w.MissingForIntent(); len(m) != 1 || m[0] != "origin" {
		t.Fatalf("the intent's five are not enforced: %v", m)
	}
}

// Failure has a receipt too. It is not silence, and it is not an error
// returned to nobody: it is the same closing, with a different ending.
//
//	— T10.3
func TestFailureHasAReceipt(t *testing.T) {
	id := frame.Hash([]byte("the intent"))
	w := full()
	w.Receipt, w.State = id.String(), "nothing was copied"
	out, err := CloseWith("home/work", id, Result{
		Outcome: Failed, Saying: "the destination refused it", Witness: w,
	})
	if err != nil {
		t.Fatalf("a failure could not be recorded: %v", err)
	}
	r, err := Read[Result](out.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != Failed {
		t.Fatalf("the ending was not kept: %q", r.Outcome)
	}
}

// A receipt whose end was lost closes as unknown — not a victory, not a
// defeat. It is "I do not know", said out loud, and it exists so a harness
// never has to guess an ending.
//
//	— T10.4
func TestALostEndingClosesAsUnknown(t *testing.T) {
	id := frame.Hash([]byte("the intent"))
	out, err := Lost("home/work", id, full(), "the machine went away mid-step")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Read[Result](out.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != Unknown {
		t.Fatalf("a lost ending was recorded as %q", r.Outcome)
	}
	if r.Outcome == Done || r.Outcome == Failed {
		t.Fatal("unknown collapsed into one of the other two")
	}
	// And it still names its intent and carries all six.
	if len(out.Parents) != 1 || out.Parents[0] != id {
		t.Fatal("a lost ending lost its intent too")
	}
}

// A harness knows an event concerns it from address, verb and payload type —
// never from the name, which is an unreadable hash and says nothing about
// what is inside.
//
//	— T10.6, T3.2
func TestAHarnessRecognisesWorkByAddressAndVerbNotByName(t *testing.T) {
	mine, err := OpenIntent("home/work/copy", Intent{Doing: "copy", Witness: fiveWitnesses()})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := Concerns(mine, "home/work"); !ok || v != VerbIntent {
		t.Fatal("an event in scope with a known verb was not recognised")
	}
	elsewhere := mine
	elsewhere.Address = "elsewhere/copy"
	if _, ok := Concerns(elsewhere, "home/work"); ok {
		t.Fatal("an event outside the aperture was claimed")
	}
	other := mine
	other.Verb = "note"
	if _, ok := Concerns(other, "home/work"); ok {
		t.Fatal("an unrelated verb inside the scope was claimed")
	}
	// A prefix that is not a path segment is not inside the scope.
	near := mine
	near.Address = "home/workshop"
	if _, ok := Concerns(near, "home/work"); ok {
		t.Fatal("home/workshop was read as being inside home/work")
	}
}

// The third of the three is the payload type, and it is not decoration. Two
// customs may both use the verb "intent" and mean unrelated things; without
// the type there is nothing to tell them apart but the name, and the name is a
// hash that says nothing about what is inside.
//
// Declining is not rejecting. The foreign event stays exactly as recorded — it
// simply does not pass through this aperture.
//
//	— T10.6, T3.2, T3.3
func TestAPayloadOfAnotherTypeIsNotClaimed(t *testing.T) {
	mine, err := OpenIntent("home/work/copy", Intent{Doing: "copy", Witness: fiveWitnesses()})
	if err != nil {
		t.Fatal(err)
	}
	if got := PayloadType(mine.Payload); got != TypeIntent {
		t.Fatalf("the intent declared its type as %q", got)
	}

	// Right address, right verb, someone else's payload.
	foreign := mine
	foreign.Payload = []byte(`{"type":"someone.else.intent.v1","doing":"copy"}`)
	if _, ok := Concerns(foreign, "home/work"); ok {
		t.Fatal("a payload of another type was claimed as a receipt")
	}
	// No type at all is not a receipt with a problem; it is not one.
	bare := mine
	bare.Payload = []byte(`{"doing":"copy"}`)
	if _, ok := Concerns(bare, "home/work"); ok {
		t.Fatal("an untyped payload was claimed as a receipt")
	}
	// A verb saying one half over the other half's payload is neither.
	crossed := mine
	crossed.Verb = VerbOutcome
	if _, ok := Concerns(crossed, "home/work"); ok {
		t.Fatal("an outcome verb over an intent payload was claimed")
	}
	// And nothing about any of this altered the events themselves.
	if PayloadType(mine.Payload) != TypeIntent {
		t.Fatal("recognising changed the event")
	}
}

func fiveWitnesses() Witness {
	return Witness{Origin: "the shell", Authority: "g1", Audience: "the file",
		State: "about to copy", WayBack: "delete the copy"}
}

// For an effect that lands outside Rokh, Rokh alone does not promise "exactly
// once": there is no shared transaction and the ledger does not rule the
// destination. What it offers is a handle a cooperating destination can use
// to refuse a repeat — and a destination that will not cooperate cannot be
// made to.
//
//	— T10.7
func TestExactlyOnceIsTheDestinationsToGiveNotRokhs(t *testing.T) {
	id := frame.Hash([]byte("the intent"))
	w := full()
	w.Receipt = id.String()
	out, err := CloseWith("home/work", id, Result{
		Outcome: Done, Saying: "sent", Witness: w, Once: id.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := Read[Result](out.Payload)
	if r.Once != id.String() {
		t.Fatal("the handle a destination would deduplicate on was not kept")
	}

	// A destination that honours the handle sees the same work twice and acts
	// once. One that ignores it acts twice, and nothing in Rokh prevents that.
	cooperating := map[string]int{}
	stubborn := 0
	for i := 0; i < 3; i++ {
		cooperating[r.Once]++
		stubborn++
	}
	if len(cooperating) != 1 {
		t.Fatal("the handle did not identify one piece of work")
	}
	if stubborn == 1 {
		t.Fatal("this test would be claiming a guarantee Rokh does not give")
	}
}
