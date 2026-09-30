package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rokh/booth"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/transport"
)

// A reading door catches up (T10): after one question it has seen what
// another opening of the folder recorded. A view that finds the carrier moved
// reads what it is missing; it does not wedge, and it does not answer from the
// old view as if it were the present one.

// A door that never writes is asked once, and its answer holds what the other
// opening recorded: the count, the heads, the log, one event by name, and a
// cursor that hands over what came after it and nothing twice.
func TestAReadingDoorSeesAnotherOpeningsRecordingsAtItsFirstQuestion(t *testing.T) {
	o := twoOpenings(t)
	var ids []frame.ID
	for i := 0; i < 3; i++ {
		ids = append(ids, o.recorded(o.a, write("home/a", fmt.Sprint("recorded through the first opening ", i))))
	}
	st := o.ask(o.b, map[string]any{"op": "status"})
	if st["ok"] != true || st["accepted"] != 4 {
		t.Fatalf("the reading door's first answer: %v", st)
	}
	if h, _ := st["heads"].([]string); len(h) != 1 || h[0] != ids[2].String() {
		t.Fatalf("the reading door's heads: %v, want %s", st["heads"], ids[2])
	}
	lg := o.ask(o.b, map[string]any{"op": "log"})
	rows, _ := lg["events"].([]map[string]any)
	if len(rows) != 4 || lg["last"] != ids[2].String() {
		t.Fatalf("the reading door's log: %d rows, last %v", len(rows), lg["last"])
	}
	if g := o.ask(o.b, map[string]any{"op": "get", "id": ids[1].String()}); g["ok"] != true || g["state"] != "accepted" {
		t.Fatalf("one event by name: %v", g)
	}
	next := o.recorded(o.a, write("home/a", "after the cursor"))
	after := o.ask(o.b, map[string]any{"op": "log", "after": ids[2].String()})
	rows, _ = after["events"].([]map[string]any)
	if len(rows) != 1 || rows[0]["id"] != next.String() {
		t.Fatalf("after the cursor: %v, want exactly %s", rows, next)
	}
}

// A wait on one opening wakes on a recording made through another: nobody
// knocks, and the waiter looks at the carrier itself.
func TestAWaitOnOneOpeningWakesOnAnotherOpeningsRecording(t *testing.T) {
	o := twoOpenings(t)
	cursor := o.recorded(o.a, write("home/a", "the cursor"))
	done := make(chan map[string]any, 1)
	go func() {
		done <- o.b.Handle([]byte(fmt.Sprintf(`{"op":"wait","after":%q,"timeout":10}`, cursor.String())))
	}()
	time.Sleep(200 * time.Millisecond)
	made := o.recorded(o.a, write("home/a", "the one the waiter waits for"))
	select {
	case r := <-done:
		rows, _ := r["events"].([]map[string]any)
		if r["ok"] != true || r["timed_out"] != nil || len(rows) != 1 || rows[0]["id"] != made.String() {
			t.Fatalf("the waiter woke with %v, want exactly %s", r, made)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("the waiter on the other opening never woke")
	}
}

// The booth of one opening serves what another opening recorded, cut to the
// session's key, at the first question after it: a key added through the
// other opening binds at its first hello, sees inside its reads and only the
// heads outside them, and is unbound at its next question once the other
// opening records its revocation (across openings).
func TestABoothOnOneOpeningFollowsWhatAnotherOpeningRecorded(t *testing.T) {
	o := keyedOpenings(t)
	reader, err := key.NewReader(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kid := [32]byte{0x51}
	// The key is added the v1 way (T2): its add carries the system reader
	// sealed to it, and an envelope of its own (E2), as `rokh key add` makes.
	sysSeal, err := key.SealTo(reader.Public(), o.sys.Bytes(), key.InfoSystem, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := event.Keyring{Op: event.KeyringAdd, Key: kid, Gen: 1, Name: "journal-reader",
		Reader: reader.Public(), Reads: []string{"journal"}, System: sysSeal}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	add := o.recorded(o.a, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbKeyring,
		"payload": base64.StdEncoding.EncodeToString(kr)})
	o.envelopeFor(add, reader)

	near, far := transport.Pipe()
	defer near.Close()
	defer far.Close()
	go o.b.ServeBooth(far)
	c := booth.NewClient(near)
	if who, err := c.ProveReader(kid, reader); err != nil || who["ok"] != true || who["owner"] == true {
		t.Fatalf("a key added through the other opening did not bind at its first hello: %v %v", who, err)
	}

	in := o.recorded(o.a, write("journal/today", "inside the key's reads"))
	out := o.recorded(o.a, write("private/diary", "outside the key's reads"))
	lg, err := c.Call("log", nil)
	if err != nil || lg["ok"] != true {
		t.Fatalf("the key's log: %v %v", lg, err)
	}
	seen := map[string]map[string]any{}
	rows, _ := lg["events"].([]any)
	for _, r := range rows {
		m := r.(map[string]any)
		seen[fmt.Sprint(m["id"])] = m
	}
	if m := seen[in.String()]; m == nil || m["address"] != "journal/today" {
		t.Fatalf("the recording inside the key's reads is not shown whole: %v", m)
	}
	if m := seen[out.String()]; m == nil || m["address"] != nil || m["head_only"] != true {
		t.Fatalf("the recording outside the key's reads showed more than its head: %v", m)
	}

	rv, err := event.Keyring{Op: event.KeyringRevoke, Key: kid, Gen: 1, Target: add}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	o.recorded(o.a, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbKeyring,
		"payload": base64.StdEncoding.EncodeToString(rv)})
	if r, err := c.Call("log", nil); err != nil || r["code"] != booth.CodeKeyRevoked || r["events"] != nil {
		t.Fatalf("a key revoked through the other opening was served: %v %v", r, err)
	}
	if r, _ := c.Call("status", nil); r["code"] != booth.CodeHelloFirst {
		t.Fatalf("the revoked key's session is still bound: %v", r)
	}
}

// A door whose carrier cannot be read now does not answer from what it read
// before: not a count, not a log, not one event, not the booth's view, and it
// records nothing. When the carrier reads again, the door answers from what it
// holds and writes on it: it did not wedge.
func TestADoorThatCannotReadThePresentDoesNotAnswerFromThePast(t *testing.T) {
	o := twoOpenings(t)
	if st := o.ask(o.b, map[string]any{"op": "status"}); st["accepted"] != 1 {
		t.Fatalf("before: %v", st)
	}
	made := o.recorded(o.a, write("home/a", "the present, which the second door has not read"))
	saved := map[string][]byte{}
	for i := 0; i < 4; i++ {
		name := filepath.Join(o.dir, "rokh", fmt.Sprintf("head%d.rkh", i))
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		saved[name] = b
		if err := os.WriteFile(name, make([]byte, len(b)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, req := range []map[string]any{
		{"op": "status"}, {"op": "log"}, {"op": "get", "id": o.gen.ID.String()}, {"op": "capabilities"},
	} {
		if r := o.ask(o.b, req); r["ok"] == true || r["code"] != "carrier_unreadable" {
			t.Fatalf("%v answered from the old view: %v", req["op"], r)
		}
	}
	if r := o.ask(o.b, write("home/b", "on what cannot be read")); r["record"] != NotRecorded || r["code"] != "carrier_unreadable" {
		t.Fatalf("a write on a carrier that cannot be read: %v", r)
	}
	near, far := transport.Pipe()
	defer near.Close()
	defer far.Close()
	go o.b.ServeBooth(far)
	c := booth.NewClient(near)
	if who, err := c.ProveKey([32]byte{}, o.root); err != nil || who["owner"] != true {
		t.Fatalf("the owner did not bind: %v %v", who, err)
	}
	if r, err := c.Call("view", nil); err != nil || r["ok"] == true || r["code"] != "carrier_unreadable" || r["planets"] != nil {
		t.Fatalf("the booth's view answered from the old view: %v %v", r, err)
	}

	for name, b := range saved {
		if err := os.WriteFile(name, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	st := o.ask(o.b, map[string]any{"op": "status"})
	if st["ok"] != true || st["accepted"] != 2 {
		t.Fatalf("after the carrier reads again: %v", st)
	}
	if h, _ := st["heads"].([]string); len(h) != 1 || h[0] != made.String() {
		t.Fatalf("after the carrier reads again, heads %v, want %s", st["heads"], made)
	}
	next := o.recorded(o.b, write("home/b", "the door did not wedge"))
	if p := parentsOf(t, o.b.led, next); len(p) != 1 || p[0] != made {
		t.Fatalf("signed on %v, not on %s", p, made.Short())
	}
	if r, _ := c.Call("view", nil); r["ok"] != true {
		t.Fatalf("the booth's view after the carrier reads again: %v", r)
	}
}

// keyedOpening is two doors on two openings of one carrier folder, each over
// the owner's key layer and sealing every record at its own point (T2).
type keyedOpening struct {
	*openings
	owner key.Reader
	sys   key.Reader
}

func keyedOpenings(t *testing.T) *keyedOpening {
	t.Helper()
	dir := t.TempDir()
	_, root, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte("two openings of one folder, with keys")}, root)
	if err != nil {
		t.Fatal(err)
	}
	first, sec := createBare(t, dir, openingsPass, root, gen.ID)
	k := &keyedOpening{}
	if k.owner, err = key.ReaderFrom(sec.Reader[:]); err != nil {
		t.Fatal(err)
	}
	if k.sys, err = key.NewReader(rand.Reader); err != nil {
		t.Fatal(err)
	}
	putSealed(t, dir, first, gen, "main", k.owner.Public(), k.sys.Public())
	second, sec2 := openV1(t, dir, openingsPass)
	k.openings = &openings{t: t, dir: dir, root: root, gen: gen,
		a: ownerDoor(t, first, sec, Options{AllowSign: true, Dir: dir, Door: "a", Root: root}),
		b: ownerDoor(t, second, sec2, Options{AllowSign: true, Dir: dir, Door: "b", Root: root})}
	return k
}

// envelopeFor records a second envelope of an event for one more reader
// beside the owner of its point and the system reader (E2): what `rokh key
// add` records for the key it adds.
func (k *keyedOpening) envelopeFor(id frame.ID, r key.Reader) {
	k.t.Helper()
	k.a.mu.RLock()
	e, ok := k.a.led.Get(id)
	k.a.mu.RUnlock()
	if !ok || e.HeadOnly {
		k.t.Fatalf("%s is not held whole", id.Short())
	}
	c, _ := openV1(k.t, k.dir, openingsPass)
	putSealed(k.t, k.dir, c, e, "", k.owner.Public(), k.sys.Public(), r.Public())
}
