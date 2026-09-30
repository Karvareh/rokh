package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// Two doors, each on its own opening of one carrier folder, as two processes
// hold them: what one records is on the folder, not in the other's memory.
// The writing turn is the folder's, and under it a door reads what the folder
// holds now before it judges a precondition, looks for an attempt or takes a
// branch's head as a parent. So an answer of recorded stays recorded: no door
// moves a branch over what another committed (contract C9, T10).

const openingsPass = "synthetic passphrase of two openings"

type openings struct {
	t    *testing.T
	dir  string
	root ed25519.PrivateKey
	gen  event.Signed
	a, b *Server
}

// loadV1 reads a ledger from a carrier's references.
func loadV1(t *testing.T, c *carrier.Carrier) *ledger.Ledger {
	t.Helper()
	heads, err := c.Heads()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.Get(c.Anchor())
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// twoOpenings makes one carrier folder and two doors on two openings of it.
func twoOpenings(t *testing.T) *openings {
	t.Helper()
	dir := t.TempDir()
	_, root, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte("two openings of one folder")}, root)
	if err != nil {
		t.Fatal(err)
	}
	first := createV1(t, dir, openingsPass, root, gen)
	second, _ := openV1(t, dir, openingsPass)
	return &openings{t: t, dir: dir, root: root, gen: gen,
		a: New(first, loadV1(t, first), Options{AllowSign: true, Dir: dir, Door: "a", Root: root}),
		b: New(second, loadV1(t, second), Options{AllowSign: true, Dir: dir, Door: "b", Root: root})}
}

func (o *openings) ask(door *Server, req map[string]any) map[string]any {
	o.t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		o.t.Fatal(err)
	}
	return door.Handle(b)
}

// recorded asks a door to write and insists on recorded; it gives the id.
func (o *openings) recorded(door *Server, req map[string]any) frame.ID {
	o.t.Helper()
	r := o.ask(door, req)
	if r["ok"] != true || r["record"] != Recorded {
		o.t.Fatalf("%v: %v", req, r)
	}
	id, err := frame.ParseID(fmt.Sprint(r["id"]))
	if err != nil {
		o.t.Fatalf("a recording with no id: %v", r)
	}
	return id
}

// fresh is the recorded truth: the folder opened again, read by its
// references.
func (o *openings) fresh() *ledger.Ledger {
	o.t.Helper()
	c, _ := openV1(o.t, o.dir, openingsPass)
	return loadV1(o.t, c)
}

func write(address, message string) map[string]any {
	return map[string]any{"op": "write", "address": address, "verb": "note", "message": message}
}

func parentsOf(t *testing.T, l *ledger.Ledger, id frame.ID) []frame.ID {
	t.Helper()
	e, ok := l.Get(id)
	if !ok {
		t.Fatalf("%s is not held", id.Short())
	}
	return e.Event.Parents
}

// A door takes as a parent the head another opening recorded, so the
// branch it moves reaches both: neither recording is left behind no
// reference.
func TestADoorTakesAsParentWhatAnotherOpeningRecorded(t *testing.T) {
	o := twoOpenings(t)
	first := o.recorded(o.a, write("home/a", "through the first opening"))
	second := o.recorded(o.b, write("home/b", "through the second opening"))
	if p := parentsOf(t, o.b.led, second); len(p) != 1 || p[0] != first {
		t.Fatalf("the second door signed on %v, not on the first door's %s", p, first.Short())
	}
	third := o.recorded(o.a, write("home/a", "the first opening again"))
	if p := parentsOf(t, o.a.led, third); len(p) != 1 || p[0] != second {
		t.Fatalf("the first door signed on %v, not on the second door's %s", p, second.Short())
	}
	back := o.fresh()
	if n := len(back.Order()); n != 4 {
		t.Fatalf("the folder holds %d events, want the genesis and three recordings", n)
	}
	for _, id := range []frame.ID{first, second, third} {
		if back.State(id) != ledger.Accepted {
			t.Fatalf("%s was answered recorded and the folder does not hold it", id.Short())
		}
	}
	if h := back.Heads(); len(h) != 1 || h[0] != third {
		t.Fatalf("the folder's heads are %v, want the last recording", h)
	}
}

// A precondition is judged on what the folder holds under the turn: heads a
// door read before another opening recorded are no longer the heads, and
// nothing is written on them.
func TestAPreconditionIsJudgedOnWhatTheFolderHoldsNow(t *testing.T) {
	o := twoOpenings(t)
	before := o.b.headNames()
	first := o.recorded(o.a, write("home/a", "moves the heads"))
	req := write("home/b", "prepared against the heads before")
	req["expect_heads"] = before
	r := o.ask(o.b, req)
	if r["ok"] == true || r["record"] != NotRecorded || r["code"] != "precondition_failed" {
		t.Fatalf("a write prepared against heads another opening moved: %v", r)
	}
	if h, _ := r["heads"].([]string); len(h) != 1 || h[0] != first.String() {
		t.Fatalf("the refusal names the heads %v, want the present %s", r["heads"], first)
	}
	if n := len(o.fresh().Order()); n != 2 {
		t.Fatalf("a refused precondition left %d events on the folder, want 2", n)
	}
	req["expect_heads"] = []string{first.String()}
	second := o.recorded(o.b, req)
	if p := parentsOf(t, o.b.led, second); len(p) != 1 || p[0] != first {
		t.Fatalf("signed on %v, not on %s", p, first.Short())
	}
}

// An attempt recorded through one opening is found through the other: the
// same request under the same name is the event already recorded, and a
// different request under the name is refused. The folder holds it once.
func TestAnAttemptRecordedThroughAnotherOpeningIsNotRecordedAgain(t *testing.T) {
	o := twoOpenings(t)
	req := write("home/once", "once only")
	req["attempt"] = "t10-once"
	first := o.ask(o.a, req)
	if first["record"] != Recorded || first["already"] != false {
		t.Fatalf("the first attempt: %v", first)
	}
	again := o.ask(o.b, req)
	if again["record"] != Recorded || again["already"] != true || again["id"] != first["id"] {
		t.Fatalf("the same attempt through the other opening: %v, first %v", again, first["id"])
	}
	other := write("home/once", "a different request")
	other["attempt"] = "t10-once"
	if r := o.ask(o.b, other); r["record"] != NotRecorded || r["code"] != "attempt_conflict" {
		t.Fatalf("a different request under the name: %v", r)
	}
	n := 0
	back := o.fresh()
	for _, id := range back.Order() {
		if e, _ := back.Get(id); e.Event.Address == "home/once" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the folder holds the attempt %d times", n)
	}
}

// Two openings recording at once: the turn keeps them in line, each reads
// the other's before it signs, and every recording answered recorded is on
// the folder, on one line of history.
func TestTwoOpeningsRecordingAtOnceLoseNothing(t *testing.T) {
	o := twoOpenings(t)
	const each = 12
	var mu sync.Mutex
	var got []string
	var bad []string
	var wg sync.WaitGroup
	for k, door := range []*Server{o.a, o.b} {
		wg.Add(1)
		go func(k int, door *Server) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				b, _ := json.Marshal(write(fmt.Sprintf("home/door-%d", k), fmt.Sprintf("door %d entry %d", k, i)))
				r := door.Handle(b)
				mu.Lock()
				if r["ok"] == true && r["record"] == Recorded {
					got = append(got, fmt.Sprint(r["id"]))
				} else {
					bad = append(bad, fmt.Sprint(r))
				}
				mu.Unlock()
			}
		}(k, door)
	}
	wg.Wait()
	if len(bad) > 0 {
		t.Fatalf("writes not recorded: %v", bad)
	}
	back := o.fresh()
	held := map[string]bool{}
	for _, id := range back.Order() {
		held[id.String()] = true
	}
	for _, id := range got {
		if !held[id] {
			t.Errorf("answered recorded, not on the folder: %s", id)
		}
	}
	if n := len(back.Order()); n != 2*each+1 {
		t.Fatalf("the folder holds %d events, want %d", n, 2*each+1)
	}
	if h := back.Heads(); len(h) != 1 {
		t.Fatalf("the folder's history split into %d heads", len(h))
	}
}
