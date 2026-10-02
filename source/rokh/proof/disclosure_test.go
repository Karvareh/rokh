package proof

import (
	"errors"
	"reflect"
	"testing"

	"rokh/bundle"
	"rokh/covenant"
	"rokh/daemon"
	"rokh/frame"
	"rokh/ledger"
)

// A right to write is not a right to be read. The widest writing grant there
// is — the whole ledger, every verb — buys no disclosure at all: asked what
// may cross to a peer, the bundle layer answers "no covenant", not "nothing".
// And a covenant the delegate writes for itself is an ordinary accepted event
// that confers nothing, because only the owner discloses. The core has no
// opinion about the payload; the disclosure layer, which does, refuses to
// read it as a permission.
//
//	— T7, T7.1, T7.3, T4.3, N4.8
func TestAWritingGrantOpensNoDoorToReading(t *testing.T) {
	w := newWorld(t)
	l := w.fresh()

	dPub, dPriv := newKey(t)
	peerPub, _ := newKey(t)

	// The widest grant the grammar can express: no scope, no verb list.
	g := w.grant([]frame.ID{w.gen.ID}, dPub, "", nil, true)
	mustVerdict(t, l, g, ledger.Accepted, "a grant over the whole ledger")

	note := w.sign(dPriv, &g.ID, []frame.ID{g.ID}, "home/journal", "note", []byte("mine"))
	mustVerdict(t, l, note, ledger.Accepted, "the delegate writing anywhere it likes")

	if _, err := bundle.Disclosable(l, peerPub, w.anchor, "", false); !errors.Is(err, bundle.ErrNoCovenant) {
		t.Fatalf("a writing grant disclosed something: %v", err)
	}

	// The delegate writes itself a covenant. The core accepts the event: it
	// weighs bytes, signature and authority and never reads a payload.
	share, err := covenant.Share{Subject: peerPub, Scope: ""}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	mine := w.sign(dPriv, &g.ID, []frame.ID{note.ID}, covenant.Address, covenant.VerbShare, share)
	mustVerdict(t, l, mine, ledger.Accepted, "a delegate's peer.share, as an event")

	// And it is not a covenant. Only the genesis author discloses.
	for _, c := range covenant.ActiveAt(l) {
		if c.ID == mine.ID {
			t.Fatal("a delegate's peer.share became a live covenant; disclosure followed writing")
		}
	}
	if got := len(covenant.ActiveAt(l)); got != 0 {
		t.Fatalf("live covenants = %d, want 0", got)
	}
	if _, err := bundle.Disclosable(l, peerPub, w.anchor, "", false); !errors.Is(err, bundle.ErrNoCovenant) {
		t.Fatalf("a delegate's own covenant disclosed something: %v", err)
	}

	// The same bytes, signed by the owner, are a covenant. That is the whole
	// of the boundary: the author, not the payload.
	theirs := w.sign(w.root, nil, []frame.ID{mine.ID}, covenant.Address, covenant.VerbShare, share)
	mustVerdict(t, l, theirs, ledger.Accepted, "the owner's peer.share")
	live := covenant.ActiveAt(l)
	if len(live) != 1 || live[0].ID != theirs.ID {
		t.Fatalf("the owner's covenant is not the live one: %v", live)
	}
	sel, err := bundle.Disclosable(l, peerPub, w.anchor, "", false)
	if err != nil {
		t.Fatalf("with the owner's covenant, disclosure should be decidable: %v", err)
	}
	if len(sel.IDs) == 0 {
		t.Fatal("the owner's covenant disclosed nothing at all")
	}

	// Withdrawn by the owner, it is gone; withdrawn by the delegate, it is
	// not, for the same reason it was never granted by one.
	undo, err := covenant.Unshare{Target: theirs.ID}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	byDelegate := w.sign(dPriv, &g.ID, []frame.ID{theirs.ID}, covenant.Address, covenant.VerbUnshare, undo)
	mustVerdict(t, l, byDelegate, ledger.Accepted, "a delegate's peer.unshare, as an event")
	if len(covenant.ActiveAt(l)) != 1 {
		t.Fatal("a delegate withdrew the owner's covenant")
	}
	byOwner := w.sign(w.root, nil, []frame.ID{byDelegate.ID}, covenant.Address, covenant.VerbUnshare, undo)
	mustVerdict(t, l, byOwner, ledger.Accepted, "the owner's peer.unshare")
	if got := len(covenant.ActiveAt(l)); got != 0 {
		t.Fatalf("after the owner withdrew it, live covenants = %d", got)
	}
	if _, err := bundle.Disclosable(l, peerPub, w.anchor, "", false); !errors.Is(err, bundle.ErrNoCovenant) {
		t.Fatalf("a withdrawn covenant still disclosed: %v", err)
	}
}

// A program's door holds no root. Asked to sign with the root key it refuses
// by name, asked to sign with nothing it refuses the same way rather than
// falling back, and asked for a key it does not hold it says so with a
// different word. In every case the ledger is the size it was, the references
// are where they were, and not one byte of the carrier was written.
//
//	— T7.1, T11.10, T8.5, N4.8
func TestAProgramDoorSignsWithNoRootAndWritesNothingTrying(t *testing.T) {
	w := newWorld(t)
	l := w.load()
	before := l.Len()
	refsBefore, err := w.car.Refs()
	if err != nil {
		t.Fatal(err)
	}
	filesBefore := w.files(t)

	s := daemon.New(w.car, l, w.at(daemon.Options{AllowSign: true, NoRoot: true}))

	cases := []struct {
		why  string
		req  map[string]any
		code string
	}{
		{"an unnamed key falls back to nothing",
			map[string]any{"op": "write", "address": "home/a", "message": "x"}, "root_refused"},
		{"the root named outright",
			map[string]any{"op": "write", "address": "home/a", "message": "x", "key": "root"}, "root_refused"},
		{"a key this door does not hold",
			map[string]any{"op": "write", "address": "home/a", "message": "x", "key": "nobody"}, "key_unknown"},
		{"a receipt's first half, unnamed",
			map[string]any{"op": "intent", "address": "home", "doing": "x", "witness": witness()}, "root_refused"},
		{"a receipt's first half, with an unheld key",
			map[string]any{"op": "intent", "address": "home", "doing": "x", "witness": witness(), "key": "nobody"}, "key_unknown"},
	}
	for _, c := range cases {
		r := ask(t, s, c.req)
		if r["record"] != nil {
			notRecorded(t, r, c.code)
		} else {
			refused(t, r, c.code)
		}
		if r["id"] != nil {
			t.Fatalf("%s: a refusal named an event: %v", c.why, r)
		}
	}

	if got := l.Len(); got != before {
		t.Fatalf("the ledger grew from %d to %d while refusing to sign", before, got)
	}
	refsAfter, err := w.car.Refs()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refsBefore, refsAfter) {
		t.Fatalf("the references moved: %v -> %v", refsBefore, refsAfter)
	}
	if moved := changed(filesBefore, w.files(t)); len(moved) != 0 {
		t.Fatalf("refusing to sign wrote to the carrier: %v", moved)
	}
	if got := w.load().Len(); got != before {
		t.Fatalf("read back from the carrier, the ledger is %d, want %d", got, before)
	}
}
