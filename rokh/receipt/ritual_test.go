package receipt_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
	"rokh/receipt"
)

// keeper is a real ledger behind the Keeper interface: it signs the event,
// records it, and returns the name the ledger gave it. Nothing here is a
// stand-in for recording — the events in these tests are signed events in a
// ledger, and their names are the hashes of their own bytes.
type keeper struct {
	l      *ledger.Ledger
	priv   ed25519.PrivateKey
	head   frame.ID
	anchor frame.ID
}

func (k *keeper) Record(e event.Event) (frame.ID, error) {
	e.Carrier = &k.anchor
	// The branch head first, then whatever the event names for itself — and
	// not twice, since a result whose intent is also the head names one
	// parent, not the same one under two headings.
	parents := []frame.ID{k.head}
	for _, p := range e.Parents {
		if p != k.head {
			parents = append(parents, p)
		}
	}
	e.Parents = parents
	signed, err := event.SignFresh(e, k.priv)
	if err != nil {
		return frame.ID{}, err
	}
	if _, err := k.l.Add(signed.Raw); err != nil {
		return frame.ID{}, err
	}
	k.head = signed.ID
	return signed.ID, nil
}

func (k *keeper) Each(visit func(frame.ID, event.Event) bool) error {
	for _, id := range k.l.Order() {
		s, ok := k.l.Get(id)
		if !ok {
			continue
		}
		if !visit(id, s.Event) {
			return nil
		}
	}
	return nil
}

func newKeeper(t *testing.T) *keeper {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.Sign(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("genesis"),
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.New(gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return &keeper{l: l, priv: priv, head: gen.ID, anchor: gen.ID}
}

func witness() receipt.Witness {
	return receipt.Witness{Origin: "the shell", Authority: "the root grant",
		Audience: "the printer", State: "queued", WayBack: "cancel the job"}
}

// The unit of work is a receipt, and both halves of it are recorded events.
//
// This is the part that cannot be got right on paper. The intent's name is not
// a hash of its payload computed by whoever wants to refer to it; it is the
// name the ledger gave the signed event, which covers the payload, the author,
// the parents and the signature. The result names that name as its parent, so
// the pairing is the ledger's own link and not an agreement the two ends have
// to keep.
//
//	— T10, T10.1, T3.2
func TestBothHalvesAreRecordedEventsAndTheResultNamesTheIntent(t *testing.T) {
	k := newKeeper(t)
	r := receipt.New("home/print", k)

	before := k.l.Len()
	id, err := r.Open(receipt.Intent{Doing: "print the page", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}
	if k.l.Len() != before+1 {
		t.Fatal("opening an intent recorded nothing")
	}
	// The name is the ledger's, and the event under it is really an intent.
	s, ok := k.l.Get(id)
	if !ok {
		t.Fatal("the intent's name is not in the ledger")
	}
	if verb, yes := receipt.Concerns(s.Event, "home/print"); !yes || verb != receipt.VerbIntent {
		t.Fatal("what was recorded is not an intent")
	}
	if s.ID != id {
		t.Fatal("the name returned is not the name of the recorded bytes")
	}

	out, err := r.Close(id, receipt.Result{
		Outcome: receipt.Done, Saying: "printed", Witness: witness(),
	})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := k.l.Get(out)
	if !ok {
		t.Fatal("the result is not in the ledger")
	}
	// The parent link is the pairing.
	var names bool
	for _, p := range res.Event.Parents {
		if p == id {
			names = true
		}
	}
	if !names {
		t.Fatal("the result does not name the intent as a parent")
	}
	// And the sixth witness carries the same name, in the signed bytes.
	body, err := receipt.Read[receipt.Result](res.Event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if body.Witness.Receipt != id.String() {
		t.Fatalf("the witness names %q, the parent is %s", body.Witness.Receipt, id)
	}

	if found, yes, err := r.Answered(id); err != nil || !yes || found != out {
		t.Fatalf("the intent does not read as answered: %v %v", yes, err)
	}
}

// A result whose intent nobody recorded answers nothing. It is a claim about
// an outcome with no question in front of it, which is the one thing the
// parent link exists to prevent.
//
//	— T10.1
func TestAResultCannotAnswerAnIntentThatWasNeverRecorded(t *testing.T) {
	k := newKeeper(t)
	r := receipt.New("home/print", k)

	invented := frame.Hash([]byte("an intent nobody wrote"))
	_, err := r.Close(invented, receipt.Result{
		Outcome: receipt.Done, Saying: "printed", Witness: witness(),
	})
	if !errors.Is(err, receipt.ErrNoSuchIntent) {
		t.Fatalf("a result answered an invented intent: %v", err)
	}
	if k.l.Len() != 1 {
		t.Fatal("the refused result was recorded anyway")
	}

	// Nor may an event that is in the ledger but is not an intent be answered.
	other, err := k.Record(event.Event{Address: "home/print", Verb: "note",
		Payload: []byte("just a note")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Close(other, receipt.Result{
		Outcome: receipt.Done, Saying: "printed", Witness: witness(),
	}); !errors.Is(err, receipt.ErrNoSuchIntent) {
		t.Fatalf("a note was answered as though it were an intent: %v", err)
	}
}

// Failure carries a receipt too, and so does an ending that was never learned.
// All three endings go through the same act and land in the same ledger; the
// third is not a softer version of the second.
//
//	— T10.3, T10.4
func TestFailureAndTheLostEndingAreRecordedLikeAnySuccess(t *testing.T) {
	k := newKeeper(t)
	r := receipt.New("home/print", k)

	for _, want := range []receipt.Outcome{receipt.Done, receipt.Failed, receipt.Unknown} {
		id, err := r.Open(receipt.Intent{Doing: "print", Witness: witness()})
		if err != nil {
			t.Fatal(err)
		}
		out, err := r.Close(id, receipt.Result{
			Outcome: want, Saying: string(want), Witness: witness(),
		})
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		s, ok := k.l.Get(out)
		if !ok {
			t.Fatalf("%s: not recorded", want)
		}
		body, err := receipt.Read[receipt.Result](s.Event.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if body.Outcome != want {
			t.Fatalf("recorded %q for %q", body.Outcome, want)
		}
	}
}

// An intent that was begun and never closed stays open. Nothing here closes it
// on anyone's behalf — no timeout turns it into a failure, and no reopening
// quietly writes an ending. Someone writes the result, including the honest one
// that says the ending was never learned.
//
//	— T10.1, T10.4, N-Axiom2
func TestAnUnansweredIntentStaysOpenUntilSomeoneWritesTheResult(t *testing.T) {
	k := newKeeper(t)
	r := receipt.New("home/print", k)

	closed, err := r.Open(receipt.Intent{Doing: "one", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Close(closed, receipt.Result{
		Outcome: receipt.Done, Saying: "done", Witness: witness(),
	}); err != nil {
		t.Fatal(err)
	}
	stranded, err := r.Open(receipt.Intent{Doing: "two", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}

	open, err := r.Unanswered()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0] != stranded {
		t.Fatalf("the open intents are %v, expected just %s", open, stranded)
	}

	// Reading twice changes nothing.
	again, err := r.Unanswered()
	if err != nil || len(again) != 1 || again[0] != stranded {
		t.Fatal("reading the open intents altered them")
	}

	// The honest ending closes it, and it is not a failure.
	if _, err := receipt.Lost("home/print", stranded, witness(), "the printer went away"); err != nil {
		t.Fatal(err)
	}
	out, err := r.Close(stranded, receipt.Result{
		Outcome: receipt.Unknown, Saying: "the printer went away", Witness: witness(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := k.l.Get(out)
	body, _ := receipt.Read[receipt.Result](s.Event.Payload)
	if body.Outcome != receipt.Unknown {
		t.Fatalf("the lost ending was recorded as %q", body.Outcome)
	}
	if open, err := r.Unanswered(); err != nil || len(open) != 0 {
		t.Fatalf("still open after the ending was written: %v", open)
	}
}

// What Rokh can do about a repeat is tell a harness the effect was already
// claimed here. What it cannot do is reach the destination and stop a second
// one landing there — there is no transaction spanning both, and the ledger
// does not rule what is outside it.
//
//	— T10.7
func TestAHandleAlreadyUsedHereWillNotBeUsedAgainHere(t *testing.T) {
	k := newKeeper(t)
	r := receipt.New("home/print", k)

	first, err := r.Open(receipt.Intent{Doing: "charge the card", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Close(first, receipt.Result{Outcome: receipt.Done,
		Saying: "charged", Witness: witness(), Once: "order-9"}); err != nil {
		t.Fatal(err)
	}

	second, err := r.Open(receipt.Intent{Doing: "charge the card", Witness: witness()})
	if err != nil {
		t.Fatal(err)
	}
	before := k.l.Len()
	_, err = r.Close(second, receipt.Result{Outcome: receipt.Done,
		Saying: "charged again", Witness: witness(), Once: "order-9"})
	if !errors.Is(err, receipt.ErrRepeat) {
		t.Fatalf("the same handle closed a second receipt: %v", err)
	}
	if k.l.Len() != before {
		t.Fatal("the refused repeat was recorded anyway")
	}

	// A different handle is a different effect and passes.
	if _, err := r.Close(second, receipt.Result{Outcome: receipt.Done,
		Saying: "charged", Witness: witness(), Once: "order-10"}); err != nil {
		t.Fatal(err)
	}

	// And the limit, said plainly: the ledger knows the handle was used. It
	// does not know, and cannot know, whether the destination charged twice.
	if _, yes, err := r.Closed("order-9"); err != nil || !yes {
		t.Fatal("the handle is not readable as used")
	}
	if _, yes, err := r.Closed("order-11"); err != nil || yes {
		t.Fatal("a handle nobody used read as used")
	}
}

// The tests are the host here: they give the core its randomness.
func init() {
	if event.Entropy == nil {
		event.Entropy = rand.Reader
	}
}
