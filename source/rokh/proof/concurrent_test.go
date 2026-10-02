package proof

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"rokh/daemon"
	"rokh/frame"
)

// complaints collects what goroutines find, because a goroutine may not fail
// a test itself.
type complaints struct {
	mu sync.Mutex
	in []string
}

func (c *complaints) say(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.in) < 20 {
		c.in = append(c.in, fmt.Sprintf(format, args...))
	}
}

func (c *complaints) report(t *testing.T) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.in {
		t.Error(s)
	}
	if len(c.in) > 0 {
		t.FailNow()
	}
}

// Readers share the door and a recording has it alone, so many programs may
// look at once while somebody writes. Thirty-two readers asking six thousand
// four hundred questions beside four writers recording a hundred events:
// every recording lands, every reader is answered, and at the end the ledger
// is exactly the hundred events plus the genesis, in one order.
//
//	— T8.5, T4.4
func TestManyReadersOneDoorAndEveryRecordingLands(t *testing.T) {
	w := newFastWorld(t)
	led := w.load()
	s := daemon.New(w.car, led, w.at(daemon.Options{AllowSign: true}))

	const readers, reads, writers, writes = 32, 200, 4, 25
	var c complaints
	var wg sync.WaitGroup

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			reqs := []map[string]any{
				{"op": "status"},
				{"op": "log", "limit": 5},
				{"op": "get", "id": w.gen.ID.String()},
			}
			for i := 0; i < reads; i++ {
				req := reqs[i%len(reqs)]
				b, err := json.Marshal(req)
				if err != nil {
					c.say("reader %d: %v", r, err)
					return
				}
				resp := s.Handle(b)
				if ok, _ := resp["ok"].(bool); !ok {
					c.say("reader %d, %v: %v", r, req["op"], resp)
					return
				}
			}
		}(r)
	}

	ids := make([][]string, writers)
	for k := 0; k < writers; k++ {
		ids[k] = make([]string, writes)
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for i := 0; i < writes; i++ {
				b, err := json.Marshal(map[string]any{"op": "write",
					"address": fmt.Sprintf("home/writer-%d/%02d", k, i), "verb": "note",
					"message": fmt.Sprintf("writer %d entry %d", k, i),
					"attempt": fmt.Sprintf("proof:writer-%d:%02d", k, i)})
				if err != nil {
					c.say("writer %d: %v", k, err)
					return
				}
				resp := s.Handle(b)
				if ok, _ := resp["ok"].(bool); !ok {
					c.say("writer %d entry %d: %v", k, i, resp)
					return
				}
				if resp["record"] != daemon.Recorded {
					c.say("writer %d entry %d: record = %v", k, i, resp["record"])
					return
				}
				id, _ := resp["id"].(string)
				if id == "" {
					c.say("writer %d entry %d: a recording with no name", k, i)
					return
				}
				ids[k][i] = id
			}
		}(k)
	}
	wg.Wait()
	c.report(t)

	seen := map[string]bool{}
	for _, byWriter := range ids {
		for _, id := range byWriter {
			if seen[id] {
				t.Fatalf("two recordings share the name %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != writers*writes {
		t.Fatalf("%d distinct recordings, want %d", len(seen), writers*writes)
	}

	want := writers*writes + 1
	acc, rej, pend := led.Tally()
	if acc != want || rej != 0 || pend != 0 {
		t.Fatalf("live tally %d/%d/%d, want %d/0/0", acc, rej, pend, want)
	}
	if got := len(led.Order()); got != want {
		t.Fatalf("the order holds %d events, want %d", got, want)
	}
	back := w.load()
	if got := len(back.Order()); got != want {
		t.Fatalf("the carrier holds %d events, want %d", got, want)
	}
	if !sameIDs(back.Order(), led.Order()) {
		t.Fatal("the carrier and the live view disagree about the order")
	}
}

// Two doors on one carrier, each with its own opened carrier and its own view
// of it, take turns. Every recording lands — the writing turn is taken on the
// carrier itself, so neither door has to know about the other — and after one
// question each door has seen what the other wrote, because a view that finds
// the references moved forward reads what it is missing rather than wedging.
//
// The writing turn of a v1 carrier is the kernel's lock on its folder's first
// head file (contract 2.9), so this runs on a carrier folder.
//
//	— T8.5, T4.4
func TestTwoDoorsOnOneCarrierBothRecordAndBothCatchUp(t *testing.T) {
	w := newWorld(t)
	first := daemon.New(w.car, w.load(), w.at(daemon.Options{AllowSign: true}))
	other := w.reopen()
	second := daemon.New(other, loadFrom(t, other), w.at(daemon.Options{AllowSign: true}))

	const rounds = 20
	doors := []*daemon.Server{first, second}
	for i := 0; i < rounds; i++ {
		for k, door := range doors {
			r := ask(t, door, map[string]any{"op": "write",
				"address": fmt.Sprintf("home/door-%d/%02d", k, i), "verb": "note",
				"message": fmt.Sprintf("door %d round %d", k, i)})
			if code, _ := r["code"].(string); code == "turn_busy" {
				t.Fatalf("door %d round %d waited out the turn: %v", k, i, r)
			}
			recorded(t, r, fmt.Sprintf("door %d round %d", k, i))
		}
	}

	want := 2*rounds + 1
	for k, door := range doors {
		st := mustOK(t, ask(t, door, map[string]any{"op": "status"}), "status")
		if got := int(st["accepted"].(int)); got != want {
			t.Fatalf("door %d sees %d accepted, want %d", k, got, want)
		}
	}
	firstHeads := mustOK(t, ask(t, first, map[string]any{"op": "status"}), "status")["heads"].([]string)
	secondHeads := mustOK(t, ask(t, second, map[string]any{"op": "status"}), "status")["heads"].([]string)
	if len(firstHeads) != 1 || len(secondHeads) != 1 || firstHeads[0] != secondHeads[0] {
		t.Fatalf("the two doors did not converge: %v vs %v", firstHeads, secondHeads)
	}
	if got := len(w.load().Order()); got != want {
		t.Fatalf("the carrier holds %d events, want %d", got, want)
	}
}

// A client that hangs up mid-sentence, and fifty that say nothing at all,
// cost the door one session each and nothing afterwards. The next caller is
// answered normally, and no goroutine is left holding the line.
//
//	— T8.5, T12.3
func TestHalfSentencesAndHangUpsLeaveNothingBehind(t *testing.T) {
	w := newFastWorld(t)
	s := daemon.New(w.car, w.load(), w.at(daemon.Options{AllowSign: true}))

	path := filepath.Join(shortDir(t), "d.sock")
	ln, err := daemon.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	served := make(chan error, 1)
	go func() { served <- s.Serve(ln) }()

	// Let the accept loop settle before counting.
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	before := runtime.NumGoroutine()

	// Half a line, then gone.
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(`{"op":"sta`)); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	// Fifty that never say anything.
	for i := 0; i < 50; i++ {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		c.Close()
	}

	// And one that behaves.
	good, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	if _, err := good.Write([]byte(`{"op":"status"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(good).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatal(err)
	}
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("after fifty-one hang-ups the door answers %v", resp)
	}
	if resp["anchor"] != w.gen.ID.String() {
		t.Fatalf("anchor = %v", resp["anchor"])
	}

	// A whole line split across two writes still arrives as one request.
	if _, err := good.Write([]byte(`{"op":"sta`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := good.Write([]byte(`tus"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	line, err = bufio.NewReader(good).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatal(err)
	}
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("a request split across two writes: %v", resp)
	}

	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	if after := runtime.NumGoroutine(); after > before+10 {
		t.Fatalf("goroutines went from %d to %d; sessions are being left behind", before, after)
	}

	ln.Close()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after the listener closed")
	}
}

// wait is a watcher, and a watcher writes nothing. It sleeps until a
// recording made elsewhere reaches the references and then hands over exactly
// what came after the cursor — not the whole ledger again. With nothing
// arriving it says so at the deadline rather than hanging or inventing an
// event.
//
//	— T4.1, T10.6, T12.3
func TestWaitWakesOnARecordingAndSaysSoWhenNothingComes(t *testing.T) {
	w := newFastWorld(t)
	led := w.load()
	s := daemon.New(w.car, led, w.at(daemon.Options{AllowSign: true}))
	recorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/a",
		"verb": "note", "message": "the cursor"}), "the event to wait from")

	order := led.Order()
	cursor := order[len(order)-1]

	type answer struct {
		resp map[string]any
		took time.Duration
	}
	done := make(chan answer, 1)
	go func() {
		start := time.Now()
		b, _ := json.Marshal(map[string]any{"op": "wait", "after": cursor.String(), "timeout": 5})
		done <- answer{resp: s.Handle(b), took: time.Since(start)}
	}()

	time.Sleep(100 * time.Millisecond)
	made := recorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/b",
		"verb": "note", "message": "the one the waiter is waiting for"}), "the recording the waiter wakes on")

	select {
	case got := <-done:
		mustOK(t, got.resp, "wait")
		if got.resp["timed_out"] != nil {
			t.Fatalf("the waiter timed out although something arrived: %v", got.resp)
		}
		if got.took > 2*time.Second {
			t.Fatalf("the waiter took %v to notice a recording made 100ms in", got.took)
		}
		events, _ := got.resp["events"].([]map[string]any)
		if len(events) != 1 {
			t.Fatalf("the waiter was handed %d events, want exactly the one", len(events))
		}
		if events[0]["id"] != made["id"] {
			t.Fatalf("the waiter was handed %v, want %v", events[0]["id"], made["id"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the waiter never woke")
	}

	// Nothing arriving: the deadline is an answer, and it is said out loud.
	newest, err := frame.ParseID(made["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r := mustOK(t, ask(t, s, map[string]any{"op": "wait", "after": newest.String(), "timeout": 1}), "wait")
	took := time.Since(start)
	if r["timed_out"] != true {
		t.Fatalf("a wait with nothing arriving: %v", r)
	}
	if events, _ := r["events"].([]map[string]any); len(events) != 0 {
		t.Fatalf("a timed-out wait handed over %d events", len(events))
	}
	if took < 900*time.Millisecond || took > 3*time.Second {
		t.Fatalf("a one-second wait took %v", took)
	}
}
