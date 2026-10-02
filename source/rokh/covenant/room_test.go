package covenant

import (
	"bytes"
	"errors"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// reachCovered returns, in the ledger's own order, every event one reach
// covers. It asks the rule the same way both readers of a covenant do.
func reachCovered(l *ledger.Ledger, r Reach) []frame.ID {
	var out []frame.ID
	for _, id := range l.Order() {
		e, found := l.Get(id)
		if !found {
			continue
		}
		if r.Covers(id, e.Event.Address) {
			out = append(out, id)
		}
	}
	return out
}

func reachHas(ids []frame.ID, want frame.ID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// ---------- the rule ----------

// A room covers the event it names, and no other, whatever address that other
// event stands at. A room is a key to one room, not to the corridor.
//
//	— T6.1, N4.4
func TestARoomCoversItsOwnEventAndNothingElse(t *testing.T) {
	mine := frame.Hash([]byte("the one event"))
	other := frame.Hash([]byte("some other event"))
	r := Reach{Room: mine}

	if !r.IsRoom() {
		t.Fatal("a reach that names an event is a room")
	}
	if !r.Covers(mine, "home/journal") {
		t.Fatal("a room does not cover the event it names")
	}
	// The same address, a different event: still no.
	if r.Covers(other, "home/journal") {
		t.Fatal("a room covered an event it does not name")
	}
	for _, addr := range []string{"", "home", "home/journal", "home/journal/today", "work"} {
		if r.Covers(other, addr) {
			t.Errorf("a room covered another event standing at %q", addr)
		}
	}
	// A room whose event is named by the zero id is no room at all, and the
	// zero id is not a wildcard.
	if (Reach{}).IsRoom() {
		t.Fatal("an empty reach is not a room")
	}
	if r.Covers(frame.ID{}, "home/journal") {
		t.Fatal("a room covered the zero id")
	}
}

// A room covers no address. The answer is no rather than absent so that a
// reader that asks only about addresses is answered safely: a room covenant
// carries an empty scope, and an empty scope means the whole ledger.
//
//	— T6.1, T7.3
func TestARoomCoversNoAddress(t *testing.T) {
	mine := frame.Hash([]byte("the one event"))
	r := Reach{Room: mine}
	for _, addr := range []string{"", "home", "home/journal", "home/journal/today", "work"} {
		if r.CoversAddress(addr) {
			t.Errorf("a room covered the whole address %q", addr)
		}
	}
	// The scope field of a room is empty, and that emptiness must not be read
	// as the whole ledger the way it is read for a scope covenant.
	whole := Reach{Scope: ""}
	if !whole.CoversAddress("anything/at/all") {
		t.Fatal("an empty scope should still mean the whole ledger")
	}
	if r.Scope != "" {
		t.Fatal("this test is about a room whose scope is empty")
	}
}

// Naming a scope and a room at once is refused, at the shape and on the wire.
//
// The two answer the same question in two ways. Read as a union the covenant
// widens, which is the one direction forbidden; read as an intersection it
// follows a rule nobody wrote. Neither is ours to choose, so the payload is
// refused rather than reconciled.
//
//	— T6.1, N4.1
func TestNamingAScopeAndARoomIsRefused(t *testing.T) {
	sub, _ := newKey(t)
	room := frame.Hash([]byte("the one event"))

	err := Reach{Scope: "home", Room: room}.Check()
	if !errors.Is(err, ErrScopeAndRoom) {
		t.Fatalf("naming both was accepted: %v", err)
	}
	if !errors.Is(err, ErrShape) {
		t.Fatal("a covenant that names both is a badly shaped payload")
	}
	// Each alone passes, so the refusal is of the pair and not of either part.
	if err := (Reach{Scope: "home"}).Check(); err != nil {
		t.Fatalf("a scope alone was refused: %v", err)
	}
	if err := (Reach{Room: room}).Check(); err != nil {
		t.Fatalf("a room alone was refused: %v", err)
	}
	if err := (Reach{}).Check(); err != nil {
		t.Fatalf("naming neither is the whole ledger, as before: %v", err)
	}
	// A scope that is not an address is still refused, exactly as before.
	if err := (Reach{Scope: "home//journal"}).Check(); err == nil {
		t.Fatal("a malformed scope was accepted")
	}

	// And on the wire, where a payload can be built by hand.
	both, err := frame.EncodeFields(frame.Fields{
		{Tag: tagSubject, Value: append([]byte(nil), sub...)},
		{Tag: tagScope, Value: []byte("home")},
		roomField(room),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := DecodeRoom(both); !errors.Is(err, ErrScopeAndRoom) || ok {
		t.Fatalf("a payload naming both was read as %v (%v)", ok, err)
	}
}

// A room only ever narrows: what it covers is a proper part of what the scope
// covenant reaching the same event covers, and it stays that size while the
// scope grows underneath it.
//
//	— T6.1, N4.4, T7.3
func TestARoomOnlyNarrows(t *testing.T) {
	w := newWorld(t)
	a := w.add(w.rootPriv, nil, []frame.ID{w.gen.ID}, "home/journal", "note", []byte("a"))
	b := w.add(w.rootPriv, nil, []frame.ID{a.ID}, "home/journal", "note", []byte("b"))
	c := w.add(w.rootPriv, nil, []frame.ID{b.ID}, "home/journal/today", "note", []byte("c"))
	w.add(w.rootPriv, nil, []frame.ID{c.ID}, "work", "note", []byte("d"))

	scope := reachCovered(w.led, Reach{Scope: "home/journal"})
	room := reachCovered(w.led, Reach{Room: b.ID})

	if len(room) != 1 || room[0] != b.ID {
		t.Fatalf("a room should cover exactly its own event, got %d", len(room))
	}
	if len(scope) != 3 {
		t.Fatalf("the scope covenant should reach three events, got %d", len(scope))
	}
	for _, id := range room {
		if !reachHas(scope, id) {
			t.Fatal("a room reached an event its scope covenant does not")
		}
	}
	if len(room) >= len(scope) {
		t.Fatal("a room should be strictly narrower than the scope over it")
	}

	// The owner writes at that address again. The scope covenant widens with
	// the ledger; the room does not move.
	w.add(w.rootPriv, nil, w.led.Heads(), "home/journal", "note", []byte("e"))
	grown := reachCovered(w.led, Reach{Scope: "home/journal"})
	still := reachCovered(w.led, Reach{Room: b.ID})
	if len(grown) != len(scope)+1 {
		t.Fatalf("the scope covenant should have grown, got %d", len(grown))
	}
	if len(still) != 1 || still[0] != b.ID {
		t.Fatalf("the room grew with the address: %d events", len(still))
	}

	// A room over the whole ledger is the same one event: there is no address
	// wide enough to widen it.
	if wide := reachCovered(w.led, Reach{Room: b.ID, Scope: ""}); len(wide) != 1 {
		t.Fatalf("a room with no scope covered %d events", len(wide))
	}
}

// A covenant that names a scope behaves exactly as it did. The room is a
// narrowing added beside the old answer, never a change to it.
//
//	— T7.1, N4.8
func TestAScopeCovenantIsUnchanged(t *testing.T) {
	id := frame.Hash([]byte("any event at all"))
	scopes := []string{"", "home", "home/journal"}
	addrs := []string{"home", "home/journal", "home/journalism", "home/journal/today", "work"}
	for _, scope := range scopes {
		for _, addr := range addrs {
			want := event.ScopeCovers(scope, addr)
			r := Reach{Scope: scope}
			if got := r.CoversAddress(addr); got != want {
				t.Errorf("Reach{%q}.CoversAddress(%q) = %v, want %v", scope, addr, got, want)
			}
			if got := r.Covers(id, addr); got != want {
				t.Errorf("Reach{%q}.Covers(%q) = %v, want %v", scope, addr, got, want)
			}
			if got := (Covenant{Scope: scope}).Covers(addr); got != want {
				t.Errorf("Covenant{%q}.Covers(%q) = %v, want %v", scope, addr, got, want)
			}
		}
	}

	// And end to end: a scope covenant written into a ledger is still live,
	// still only the owner's, and still answers by address.
	w := newWorld(t)
	alice, _ := newKey(t)
	sh := w.share([]frame.ID{w.gen.ID}, alice, "home/journal")
	live := ActiveAt(w.led)
	if len(live) != 1 || live[0].ID != sh.ID {
		t.Fatalf("expected exactly the scope covenant, got %+v", live)
	}
	if _, ok := Discloses(live, alice, "home/journal/today"); !ok {
		t.Fatal("a scope covenant stopped covering below its scope")
	}
	if _, ok := Discloses(live, alice, "home/journalism"); ok {
		t.Fatal("a scope covenant crossed the component boundary")
	}
	if got := (Reach{Scope: live[0].Scope}).CoversAddress("home/journal/today"); !got {
		t.Fatal("the reach of a live scope covenant disagrees with the covenant")
	}
}

// ---------- the wire ----------

func TestRoomShareCodec(t *testing.T) {
	sub, _ := newKey(t)
	room := frame.Hash([]byte("the one event"))
	key := make([]byte, SealKeySize)
	key[0] = 9

	b, err := EncodeRoomShare(sub, room, key)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := DecodeRoom(b)
	if err != nil || !ok || got != room {
		t.Fatalf("a room did not survive: %v %v %v", got, ok, err)
	}
	again, err := EncodeRoomShare(sub, room, key)
	if err != nil || !bytes.Equal(b, again) {
		t.Fatal("a room share is not encoded deterministically")
	}
	// The subject and the reading key travel in the fields they always did,
	// and the room takes a field of its own beside them.
	fs, err := frame.DecodeFields(b)
	if err != nil {
		t.Fatal(err)
	}
	carried := map[frame.Tag][]byte{}
	for _, f := range fs {
		carried[f.Tag] = f.Value
	}
	if !bytes.Equal(carried[tagSubject], sub) {
		t.Fatal("the subject did not survive a room share")
	}
	if !bytes.Equal(carried[tagSealTo], key) {
		t.Fatal("the reading key did not survive a room share")
	}
	if _, named := carried[tagScope]; named {
		t.Fatal("a room share carried a scope")
	}

	if _, err := EncodeRoomShare(sub, frame.ID{}, nil); !errors.Is(err, ErrNoRoom) {
		t.Fatal("a room naming no event was encoded")
	}
	if _, err := EncodeRoomShare([]byte{1, 2, 3}, room, nil); err == nil {
		t.Fatal("a room share with a bad subject was encoded")
	}
	if _, err := EncodeRoomShare(sub, room, []byte{1, 2, 3}); err == nil {
		t.Fatal("a room share with a bad reading key was encoded")
	}

	// A share that names a scope names no room, and says so without error.
	plain, err := (Share{Subject: sub, Scope: "home/journal"}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := DecodeRoom(plain); ok || err != nil {
		t.Fatalf("a scope share was read as a room: %v %v", ok, err)
	}

	// A room field of the wrong size, and one that names the zero event, are
	// both refused rather than read as something.
	bad, err := frame.EncodeFields(frame.Fields{
		{Tag: tagSubject, Value: append([]byte(nil), sub...)},
		{Tag: tagRoom, Value: []byte{1, 2, 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeRoom(bad); err == nil {
		t.Fatal("a room of the wrong size was accepted")
	}
	zero, err := frame.EncodeFields(frame.Fields{
		{Tag: tagSubject, Value: append([]byte(nil), sub...)},
		{Tag: tagRoom, Value: make([]byte, frame.IDSize)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeRoom(zero); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("a room naming the zero event was accepted: %v", err)
	}
}

// A room covenant is an ordinary event at the ordinary address, written by the
// owner and withdrawn the same way. Nothing about the instrument changes.
//
//	— T7.2, T7.3
func TestARoomIsAnOrdinaryCovenantEvent(t *testing.T) {
	w := newWorld(t)
	peer, _ := newKey(t)
	kept := w.add(w.rootPriv, nil, []frame.ID{w.gen.ID}, "home/journal", "note", []byte("kept"))

	payload, err := EncodeRoomShare(peer, kept.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	sh := w.add(w.rootPriv, nil, []frame.ID{kept.ID}, Address, VerbShare, payload)
	if w.led.State(sh.ID) != ledger.Accepted {
		t.Fatal("a room covenant is an ordinary event and should be accepted")
	}

	stored, found := w.led.Get(sh.ID)
	if !found {
		t.Fatal("the covenant is not held")
	}
	room, ok, err := DecodeRoom(stored.Event.Payload)
	if err != nil || !ok || room != kept.ID {
		t.Fatalf("the room was not readable from the stored event: %v %v %v", room, ok, err)
	}
	if !(Reach{Room: room}).Covers(kept.ID, stored.Event.Address) {
		t.Fatal("the room read back does not cover the event it names")
	}

	// Withdrawal is the same instrument, naming the same kind of target.
	un, err := (Unshare{Target: sh.ID}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeUnshare(un)
	if err != nil || back.Target != sh.ID {
		t.Fatalf("a room covenant is not withdrawn like any other: %v %v", back, err)
	}
}
