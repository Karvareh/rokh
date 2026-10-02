package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// The home keeps two doors onto one carrier in one process, each with its own
// view and its own lock, and a program's wait lets go of its door so the other
// can record meanwhile. This is that shape without the home, as the host's
// integrated race run found it: one door waits while the other
// records, then both doors read at once from a carrier that has just moved.
//
// It says what the doors must answer. Whether the carrier they share is safe
// to read from both while one records is the race detector's to say: run it
// with -race. Until the vessel and carrier synchronize their own state, the
// detector reports buildIndex and indexOnce against Tx.Commit here.
//
//	— T4.1, T10.6
func TestADoorWaitsWhileAnotherDoorOnTheSameCarrierRecords(t *testing.T) {
	dir := t.TempDir()
	_, root, _ := ed25519.GenerateKey(rand.Reader)
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("two doors on one carrier")}, root)
	if err != nil {
		t.Fatal(err)
	}
	car := createV1(t, dir, "synthetic passphrase", root, gen)
	view := func() *ledger.Ledger {
		l, err := ledger.Load(gen.Raw, car.Get, []frame.ID{gen.ID})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	owner := New(car, view(), Options{AllowSign: true, Dir: dir, Door: "owner", Root: root})
	program := New(car, view(), Options{Dir: dir, Door: "program"})
	ask := func(door *Server, req map[string]any) map[string]any {
		b, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		return door.Handle(b)
	}

	const writes = 6
	woke := make(chan map[string]any, 1)
	go func() {
		woke <- ask(program, map[string]any{"op": "wait", "address": "shelf", "after": frame.ID{}.String(), "timeout": 20})
	}()
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < writes; i++ {
		r := ask(owner, map[string]any{"op": "write", "address": "shelf/task", "verb": "note", "message": fmt.Sprint("task ", i)})
		if r["record"] != Recorded {
			t.Fatalf("write %d through the owner's door: %v", i, r)
		}
	}
	select {
	case r := <-woke:
		evs, _ := r["events"].([]map[string]any)
		if r["ok"] != true || len(evs) == 0 || r["timed_out"] == true {
			t.Fatalf("the waiting door woke without the recording: %v", r)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the waiting door did not wake while the other recorded")
	}

	// Both doors read at once from a carrier that has just moved: each finds
	// its view stale and reads the references, as a review run did.
	r := ask(owner, map[string]any{"op": "write", "address": "shelf/task", "verb": "note", "message": "one more"})
	if r["record"] != Recorded {
		t.Fatalf("the last write: %v", r)
	}
	var wg sync.WaitGroup
	answers := make(chan map[string]any, 8)
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); answers <- ask(owner, map[string]any{"op": "status"}) }()
		go func() { defer wg.Done(); answers <- ask(program, map[string]any{"op": "log", "address": "shelf"}) }()
	}
	wg.Wait()
	close(answers)
	for a := range answers {
		if a["ok"] != true {
			t.Fatalf("a door read at the same time as the other failed: %v", a)
		}
		if evs, isLog := a["events"].([]map[string]any); isLog && len(evs) != writes+1 {
			t.Fatalf("the program's door saw %d of %d recordings", len(evs), writes+1)
		}
	}
	if lg := ask(program, map[string]any{"op": "log", "address": "shelf"}); lg["last"] != r["id"] {
		t.Fatalf("the program's door does not end at the last recording: %v, not %v", lg["last"], r["id"])
	}
}
