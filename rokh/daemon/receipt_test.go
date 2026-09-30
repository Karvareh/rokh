//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/ledger"
	"rokh/receipt"
)

// askReceipt puts one request through a receipt op.
//
// Handle's dispatch is a shared file, so the three cases are routed here the
// way the reported lines route them there. Everything below the dispatch — the
// guards, the decode, the covenant, the ritual — is the shipped path, and the
// lock is taken because Handle takes it.
func askReceipt(t *testing.T, s *Server, req map[string]any) map[string]any {
	t.Helper()
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch req["op"] {
	case "intent":
		return s.intent(line)
	case "outcome":
		return s.outcome(line)
	case "receipts":
		return s.receipts(line)
	}
	t.Fatalf("not a receipt op: %v", req["op"])
	return nil
}

// witnessAt is the five witnesses an intent can show. The sixth is the name of
// the intent a result answers, and no request ever carries it: it is filled
// from the name the ledger gave.
func witnessAt(state string) map[string]any {
	return map[string]any{
		"origin": "the socket", "authority": "the root key",
		"audience": "the printer", "state": state, "wayBack": "cancel the job",
	}
}

func mustID(t *testing.T, v any) frame.ID {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("not a name: %v", v)
	}
	id, err := frame.ParseID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func openRows(t *testing.T, resp map[string]any) []map[string]any {
	t.Helper()
	rows, ok := resp["open"].([]map[string]any)
	if !ok {
		t.Fatalf("the open receipts came back as %T", resp["open"])
	}
	return rows
}

// The unit of work a delegated engine does over this socket is a receipt, and
// both halves of it are real recorded events.
//
// The intent's name is the ledger's own name for the signed bytes — which
// cover the payload, the author, the parents and the signature — and not a
// hash of a payload computed by whoever wants to refer to it. The result names
// that name as its parent, so the pairing is the ledger's own link rather than
// a convention the two ends have to keep.
//
//	— T10, T10.1, T3.2
func TestBothHalvesOfASocketReceiptAreRecordedEvents(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	before := f.led.Len()
	opened := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "intent", "address": "home/print", "doing": "print the page",
		"witness": witnessAt("queued"),
	}), "intent")
	if f.led.Len() != before+1 {
		t.Fatal("opening an intent recorded nothing")
	}
	intent := mustID(t, opened["id"])
	e, found := f.led.Get(intent)
	if !found || f.led.State(intent) != ledger.Accepted {
		t.Fatal("the intent's name is not an accepted event in this ledger")
	}
	if frame.Hash(e.Raw) != intent {
		t.Fatal("the name returned is not the name of the recorded bytes")
	}
	if verb, yes := receipt.Concerns(e.Event, "home/print"); !yes || verb != receipt.VerbIntent {
		t.Fatal("what was recorded is not an intent")
	}

	closed := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/print", "intent": intent.String(),
		"outcome": "done", "saying": "printed", "witness": witnessAt("printed"),
	}), "outcome")
	result := mustID(t, closed["id"])
	res, found := f.led.Get(result)
	if !found || f.led.State(result) != ledger.Accepted {
		t.Fatal("the result is not an accepted event in this ledger")
	}
	if verb, yes := receipt.Concerns(res.Event, "home/print"); !yes || verb != receipt.VerbOutcome {
		t.Fatal("what was recorded is not a result")
	}
	names := false
	for _, p := range res.Event.Parents {
		if p == intent {
			names = true
		}
	}
	if !names {
		t.Fatal("the result does not name the intent as a parent")
	}
	// And the sixth witness carries the same name, inside the signed bytes.
	body, err := receipt.Read[receipt.Result](res.Event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if body.Witness.Receipt != intent.String() {
		t.Fatalf("the witness names %q, the parent is %s", body.Witness.Receipt, intent)
	}
	if body.Outcome != receipt.Done || body.Saying != "printed" {
		t.Fatalf("the ending was recorded as %q / %q", body.Outcome, body.Saying)
	}

	// Both halves read back through the socket's own read op, like any event.
	lg := mustOK(t, ask(t, s, map[string]any{"op": "log"}), "log")
	seen := map[string]bool{}
	for _, row := range lg["events"].([]map[string]any) {
		seen[row["id"].(string)] = true
	}
	if !seen[intent.String()] || !seen[result.String()] {
		t.Fatal("a half of the receipt is not in the log")
	}
}

// A result whose intent nobody recorded answers nothing: it is a claim about
// an ending with no question in front of it, which is the one thing the parent
// link exists to prevent. Neither an invented name nor an ordinary event that
// happens to be in the ledger becomes an intent by being named as one.
//
//	— T10.1
func TestASocketResultCannotAnswerAnIntentThatWasNeverRecorded(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	before := f.led.Len()
	invented := frame.Hash([]byte("an intent nobody wrote")).String()
	if r := askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/print", "intent": invented,
		"outcome": "done", "saying": "printed", "witness": witnessAt("printed"),
	}); r["ok"] == true {
		t.Fatal("a result answered an invented intent")
	}
	if f.led.Len() != before {
		t.Fatal("the refused result was recorded anyway")
	}

	note := mustOK(t, ask(t, s, map[string]any{
		"op": "write", "address": "home/print", "verb": "note", "message": "just a note",
	}), "write")
	before = f.led.Len()
	if r := askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/print", "intent": note["id"],
		"outcome": "done", "saying": "printed", "witness": witnessAt("printed"),
	}); r["ok"] == true {
		t.Fatal("a note was answered as though it were an intent")
	}
	// And a result that names nothing at all is not a result.
	if r := askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/print",
		"outcome": "done", "saying": "printed", "witness": witnessAt("printed"),
	}); r["ok"] == true {
		t.Fatal("a result with no intent was recorded")
	}
	if f.led.Len() != before {
		t.Fatal("a refused result grew the ledger")
	}
}

// A receipt is two signed events, so the two writing halves refuse where a
// write refuses: on a daemon opened read-only, and on one whose signing is
// off. Reading the open ones is not a write and stays available on both,
// because a harness closing its books has to see what is open even when it may
// not add to the ledger.
//
//	— T4.1, T10.1
func TestAReadOnlyDaemonAndAnUnsigningOneRefuseBothHalves(t *testing.T) {
	f := newFixture(t)

	// A live daemon opens one, so there is something for the others to refuse
	// to close.
	live := New(f.car, f.led, Options{AllowSign: true})
	opened := mustOK(t, askReceipt(t, live, map[string]any{
		"op": "intent", "address": "home/print", "doing": "print the page",
		"witness": witnessAt("queued"),
	}), "intent")

	for name, s := range map[string]*Server{
		"read-only":        New(f.car, f.led, Options{ReadOnly: true, AllowSign: true}),
		"signing disabled": New(f.car, f.led, Options{AllowSign: false}),
	} {
		before := f.led.Len()
		if r := askReceipt(t, s, map[string]any{
			"op": "intent", "address": "home/print", "doing": "print again",
			"witness": witnessAt("queued"),
		}); r["ok"] == true {
			t.Errorf("%s: an intent was recorded", name)
		}
		if r := askReceipt(t, s, map[string]any{
			"op": "outcome", "address": "home/print", "intent": opened["id"],
			"outcome": "done", "saying": "printed", "witness": witnessAt("printed"),
		}); r["ok"] == true {
			t.Errorf("%s: a result was recorded", name)
		}
		if f.led.Len() != before {
			t.Errorf("%s: a refused receipt grew the ledger", name)
		}
		open := mustOK(t, askReceipt(t, s, map[string]any{
			"op": "receipts", "address": "home/print",
		}), name+": receipts")
		if rows := openRows(t, open); len(rows) != 1 {
			t.Errorf("%s: the open intent is not listed: %v", name, rows)
		}
	}
}

// What Rokh can do about a repeat is tell a harness the effect was already
// claimed here. What it cannot do is reach the destination and stop a second
// one landing there: there is no transaction spanning both and the ledger does
// not rule what is outside it.
//
//	— T10.7
func TestAHandleAlreadyUsedHereWillNotCloseASecondSocketReceipt(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	charge := func() any {
		t.Helper()
		return mustOK(t, askReceipt(t, s, map[string]any{
			"op": "intent", "address": "home/pay", "doing": "charge the card",
			"witness": witnessAt("charging"),
		}), "intent")["id"]
	}
	first := charge()
	mustOK(t, askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/pay", "intent": first,
		"outcome": "done", "saying": "charged", "witness": witnessAt("charged"),
		"once": "order-9",
	}), "outcome")

	second := charge()
	before := f.led.Len()
	if r := askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/pay", "intent": second,
		"outcome": "done", "saying": "charged again", "witness": witnessAt("charged"),
		"once": "order-9",
	}); r["ok"] == true {
		t.Fatal("the same handle closed a second receipt")
	}
	if f.led.Len() != before {
		t.Fatal("the refused repeat was recorded anyway")
	}

	// A different handle is a different effect and passes.
	mustOK(t, askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/pay", "intent": second,
		"outcome": "done", "saying": "charged", "witness": witnessAt("charged"),
		"once": "order-10",
	}), "outcome")
}

// An intent that was begun and never closed stays open, and reading the list
// of open ones closes nothing. No timeout here turns one into a failure and no
// reading writes an ending; someone writes the result, including the honest one
// that says the ending was never learned.
//
//	— T10.1, T10.4, T4.1, N-Axiom2
func TestTheOpenSocketReceiptsAreListedAndReadingThemChangesNothing(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	answered := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "intent", "address": "home/print", "doing": "the first page",
		"witness": witnessAt("queued"),
	}), "intent")
	mustOK(t, askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/print", "intent": answered["id"],
		"outcome": "done", "saying": "printed", "witness": witnessAt("printed"),
	}), "outcome")
	stranded := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "intent", "address": "home/print", "doing": "the second page",
		"witness": witnessAt("queued"),
	}), "intent")

	settled := f.led.Len()
	list := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "receipts", "address": "home/print",
	}), "receipts")
	rows := openRows(t, list)
	if len(rows) != 1 || rows[0]["id"] != stranded["id"] {
		t.Fatalf("the open intents are %v, expected just %v", rows, stranded["id"])
	}
	if rows[0]["doing"] != "the second page" {
		t.Fatalf("the open intent does not say what it was for: %v", rows[0])
	}
	w, ok := rows[0]["witness"].(map[string]any)
	if !ok || w["wayBack"] != "cancel the job" {
		t.Fatalf("the open intent's witnesses did not come back: %v", rows[0]["witness"])
	}

	again := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "receipts", "address": "home/print",
	}), "receipts")
	if rows := openRows(t, again); len(rows) != 1 || rows[0]["id"] != stranded["id"] {
		t.Fatal("reading the open receipts altered them")
	}
	if f.led.Len() != settled {
		t.Fatal("reading the open receipts recorded something")
	}

	// An address is an aperture: another one's books are its own.
	other := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "receipts", "address": "home/scan",
	}), "receipts")
	if rows := openRows(t, other); len(rows) != 0 {
		t.Fatalf("another address's receipts leaked in: %v", rows)
	}

	// The honest ending closes it, and it is not a failure.
	mustOK(t, askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/print", "intent": stranded["id"],
		"outcome": "unknown", "saying": "the printer went away",
		"witness": witnessAt("unknown"),
	}), "outcome")
	shut := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "receipts", "address": "home/print",
	}), "receipts")
	if rows := openRows(t, shut); len(rows) != 0 {
		t.Fatalf("still open after the ending was written: %v", rows)
	}
}

// The one that matters most: a receipt written over the socket is on the
// carrier, not only in the daemon's memory. The daemon closes, the carrier is
// opened cold, and the ledger is rebuilt the way Rokh actually rebuilds one —
// by following the references back from the heads, never by reading whatever
// lies in the object store.
//
// A recorded event that no reference reaches is not on the next open's ledger
// at all: the bytes sit there and the walk never arrives. A receipt that
// vanishes when the process ends is not a receipt, so both halves and the link
// between them have to come back.
//
//	— T8.5, T4.4, T10.1
func TestASocketReceiptSurvivesTheDaemonClosingAndTheCarrierBeingReopened(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	sent := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "intent", "address": "home/office", "doing": "send the invoice",
		"witness": witnessAt("sending"),
	}), "intent")
	ended := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "outcome", "address": "home/office", "intent": sent["id"],
		"outcome": "done", "saying": "sent", "witness": witnessAt("sent"),
	}), "outcome")
	stranded := mustOK(t, askReceipt(t, s, map[string]any{
		"op": "intent", "address": "home/office", "doing": "call the bank",
		"witness": witnessAt("calling"),
	}), "intent")
	intent, result, open := mustID(t, sent["id"]), mustID(t, ended["id"]), mustID(t, stranded["id"])

	f.car.Close()

	back, err := carrier.Open(carrier.FS{Root: f.dir}, "pass")
	if err != nil {
		t.Fatalf("the carrier did not come back: %v", err)
	}
	t.Cleanup(func() { back.Close() })
	genesis, err := back.Get(back.Anchor())
	if err != nil {
		t.Fatalf("genesis unreadable: %v", err)
	}
	refs, err := back.Refs()
	if err != nil {
		t.Fatal(err)
	}
	heads := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		heads = append(heads, id)
	}
	led, err := ledger.Load(genesis, back.Get, heads)
	if err != nil {
		t.Fatalf("the ledger did not come back: %v", err)
	}

	for _, id := range []frame.ID{intent, result, open} {
		if led.State(id) != ledger.Accepted {
			t.Fatalf("%s did not survive the reopen", id.Short())
		}
	}
	res, _ := led.Get(result)
	names := false
	for _, p := range res.Event.Parents {
		if p == intent {
			names = true
		}
	}
	if !names {
		t.Fatal("the parent link did not survive the reopen")
	}
	body, err := receipt.Read[receipt.Result](res.Event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if body.Witness.Receipt != intent.String() {
		t.Fatal("the sixth witness did not survive the reopen")
	}

	// And the books read the same on the other side of the reopen.
	after := New(back, led, Options{AllowSign: true})
	rows := openRows(t, mustOK(t, askReceipt(t, after, map[string]any{
		"op": "receipts", "address": "home/office",
	}), "receipts"))
	if len(rows) != 1 || rows[0]["id"] != open.String() {
		t.Fatalf("the open receipt did not survive the reopen: %v", rows)
	}
	if rows[0]["doing"] != "call the bank" {
		t.Fatalf("the open receipt came back saying %v", rows[0]["doing"])
	}
}

// A bound harness writes its receipts at its own root and nowhere under it.
// At "clerk" the intent is recorded; at "clerk/desk" it is refused, and the
// refusal says where the root is. An "ignore" covenant does not turn that
// into silence — a receipt in the verbs' space is misplaced, not unknown.
// Outside any bound space no harness has a say, as before.
//
//	— T11.11, T10, T11.10
func TestAReceiptIsWrittenAtTheHarnessRootAndNowhereUnderIt(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	for _, b := range []map[string]any{
		{"op": "bind", "namespace": "clerk", "version": "0.1",
			"can": []any{"clerk.ask"}, "unknown": "refuse"},
		{"op": "bind", "namespace": "quiet", "version": "1",
			"can": []any{"quiet.note"}, "unknown": "ignore"},
	} {
		b["repeat"], b["retry"] = "idempotent", "never-retry"
		b["compensate"], b["ending"] = "compensable", "close-unknown"
		mustOK(t, ask(t, s, b), "bind")
	}

	before := f.led.Len()
	root := mustOK(t, askReceipt(t, s, map[string]any{"op": "intent", "address": "clerk",
		"doing": "answer the question", "witness": witnessAt("thinking")}), "intent at the root")
	if root["id"] == nil || f.led.Len() != before+1 {
		t.Fatalf("an intent at the harness's own root was not recorded: %v", root)
	}

	under := askReceipt(t, s, map[string]any{"op": "intent", "address": "clerk/desk",
		"doing": "answer the question", "witness": witnessAt("thinking")})
	if under["ok"] == true {
		t.Fatal("an intent was written under the root, in the verbs' space")
	}
	if msg, _ := under["error"].(string); !strings.Contains(msg, "root") || !strings.Contains(msg, `"clerk"`) {
		t.Fatalf("the refusal does not say where the root is: %v", under["error"])
	}

	q := askReceipt(t, s, map[string]any{"op": "intent",
		"address": "quiet/corner", "doing": "note it", "witness": witnessAt("queued")})
	if q["ok"] == true {
		t.Fatalf("an ignore-covenant let a misplaced receipt through: %v", q)
	}
	if f.led.Len() != before+1 {
		t.Fatal("a refused receipt grew the ledger")
	}

	// Outside any bound space, no harness has a say and the receipt is written.
	mustOK(t, askReceipt(t, s, map[string]any{"op": "intent",
		"address": "home/print", "doing": "print", "witness": witnessAt("queued")}),
		"intent outside any bound space")
	if f.led.Len() != before+2 {
		t.Fatal("an ordinary receipt was caught by somebody else's covenant")
	}

	// And the ritual's words are not a harness's to declare.
	r := ask(t, s, map[string]any{"op": "bind", "namespace": "loud", "version": "1",
		"can": []any{"loud.intent"}, "unknown": "refuse", "repeat": "idempotent",
		"retry": "never-retry", "compensate": "compensable", "ending": "close-unknown"})
	if r["ok"] == true {
		t.Fatal("a covenant declaring the ritual's word as its own verb was bound")
	}
}
