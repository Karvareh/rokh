package proof

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/bundle"
	"rokh/carrier"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// courierLine mirrors the line format cmd/rokh-courier writes: one event, its
// name beside its own bytes in hex.
type courierLine struct {
	ID  string `json:"id"`
	Raw string `json:"raw,omitempty"`
}

// mirrorDoor is a second carrier holding the same genesis and nothing else —
// what a courier delivers into. It signs nothing: its owner's cell holds no
// signing seed and its door is given no key, so append is the only way in.
func mirrorDoor(t *testing.T, w *world) (*daemon.Server, *ledger.Ledger, place) {
	t.Helper()
	p := inMemory()
	c := p.create(t, w.gen.ID, nil)
	commitOn(t, p, c, func(r *carrier.Recording) error {
		if err := r.Event(w.gen.ID, w.gen.Head, w.gen.Body, w.gen.Event.Address); err != nil {
			return err
		}
		return r.SetRef("main", w.gen.ID)
	})
	l, err := ledger.New(w.gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return daemon.New(c, l, p.door(daemon.Options{})), l, p
}

// A courier is untrusted and dumb, and a bundle is a file that may stop in the
// middle of a sentence. The line that was cut is refused for what it is — a
// request that is not one — and every whole line before it is recorded. Half
// an event is not half recorded: the name in the broken line never reaches the
// ledger, and the carrier read back afterwards holds the whole ones and
// nothing else.
//
//	— T3, T5.1, T8.5, T12.3
func TestABundleCutMidLineAppliesTheWholeLinesAndNotTheBrokenOne(t *testing.T) {
	w := newFastWorld(t)

	const n = 5
	events := make([]event.Signed, 0, n)
	parent := w.gen.ID
	for i := 0; i < n; i++ {
		e := w.sign(w.root, nil, []frame.ID{parent}, fmt.Sprintf("home/journal/%d", i),
			"note", []byte(fmt.Sprintf("carried %d", i)))
		events = append(events, e)
		parent = e.ID
	}

	var file bytes.Buffer
	have := bundle.Have{Kind: bundle.KindHave, Ledger: w.gen.ID.String()}
	for _, e := range events {
		have.IDs = append(have.IDs, e.ID.String())
	}
	if err := json.NewEncoder(&file).Encode(have); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if err := json.NewEncoder(&file).Encode(courierLine{ID: e.ID.String(), Raw: hexOf(e.Raw)}); err != nil {
			t.Fatal(err)
		}
	}

	// Cut inside the last line's hex, so the JSON never closes.
	whole := file.Bytes()
	lastNL := bytes.LastIndexByte(whole[:len(whole)-1], '\n')
	cut := lastNL + 1 + (len(whole)-lastNL)/2
	truncated := whole[:cut]
	path := filepath.Join(t.TempDir(), "carried.bundle")
	if err := os.WriteFile(path, truncated, 0o600); err != nil {
		t.Fatal(err)
	}

	door, led, car := mirrorDoor(t, w)
	applied, refusedLines := applyBundle(t, path, door)

	if len(refusedLines) != 1 {
		t.Fatalf("%d lines were refused, want exactly the cut one: %v", len(refusedLines), refusedLines)
	}
	if len(applied) != n-1 {
		t.Fatalf("%d lines landed, want %d", len(applied), n-1)
	}
	for i, e := range events[:n-1] {
		if led.State(e.ID) != ledger.Accepted {
			t.Fatalf("event %d did not land", i)
		}
	}
	if led.Has(events[n-1].ID) {
		t.Fatal("the event whose line was cut in half is in the ledger")
	}
	if car.holds(t, events[n-1].ID) {
		t.Fatal("the event whose line was cut in half reached the carrier")
	}
	reread, _ := car.open(t)
	back := loadFrom(t, reread)
	if got, want := back.Len(), n; got != want {
		t.Fatalf("the carrier holds %d events, want %d (genesis and %d whole lines)", got, want, n-1)
	}
	if back.Has(events[n-1].ID) {
		t.Fatal("read back from the carrier, the half line became an event")
	}

	// A package that is whole as JSON and partial as bytes is refused too, and
	// for its own reason: hex that is not hex is a bad request, and hex that is
	// a piece of an event is bytes that are not one.
	full := hexOf(events[n-1].Raw)
	for _, c := range []struct {
		why  string
		raw  string
		code string
	}{
		{"an odd number of hex digits", full[:len(full)-1], "bad_request"},
		{"hex that is not hex", "not hex at all", "bad_request"},
		{"half an event, cleanly encoded", full[:len(full)/2], "not_accepted"},
		{"no bytes at all", "", "not_accepted"},
	} {
		r := ask(t, door, map[string]any{"op": "append", "raw": c.raw})
		notRecorded(t, r, c.code)
		if led.Has(events[n-1].ID) {
			t.Fatalf("%s: a partial package landed", c.why)
		}
	}
	reread, _ = car.open(t)
	if got, want := loadFrom(t, reread).Len(), n; got != want {
		t.Fatalf("after the partial packages the carrier holds %d events, want %d", got, want)
	}

	// The whole file, delivered in reverse, still lands entirely: a courier
	// may reorder, and the round that finds nothing new is the one that stops.
	// Nothing invalid is accepted as a result — order is not evidence.
	//   — T5.1, T5.4
	reverse, _, reverseCar := mirrorDoor(t, w)
	waiting := make([]event.Signed, 0, n)
	for i := len(events) - 1; i >= 0; i-- {
		waiting = append(waiting, events[i])
	}
	for len(waiting) > 0 {
		var again []event.Signed
		progress := false
		for _, e := range waiting {
			r := ask(t, reverse, map[string]any{"op": "append", "raw": hexOf(e.Raw)})
			if ok, _ := r["ok"].(bool); ok {
				progress = true
				continue
			}
			// Not arrived is not refused: v1 names it ancestry_unproven
			// and records nothing (contract B7, B8).
			if r["code"] != "ancestry_unproven" {
				t.Fatalf("out of order, %s was refused with %v: %v", e.ID.Short(), r["code"], r["error"])
			}
			again = append(again, e)
		}
		if !progress {
			t.Fatalf("%d events never landed", len(again))
		}
		waiting = again
	}
	reverseBack, _ := reverseCar.open(t)
	if got, want := loadFrom(t, reverseBack).Len(), n+1; got != want {
		t.Fatalf("delivered in reverse, the carrier holds %d events, want %d", got, want)
	}
}

// applyBundle is what cmd/rokh-courier's apply does, in the small: read the
// file line by line, skip the have line, and offer each event to a door. A
// line that is not a line is handed over as it stands, so the door — not this
// loop — says what is wrong with it.
func applyBundle(t *testing.T, path string, door *daemon.Server) (applied, refusedLines []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	first := true
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		if first {
			first = false
			var h bundle.Have
			if err := json.Unmarshal([]byte(raw), &h); err == nil && h.Kind == bundle.KindHave {
				continue
			}
		}
		var ln courierLine
		if err := json.Unmarshal([]byte(raw), &ln); err != nil {
			// Not a line. The door is asked about it anyway, because it is
			// the door that owes an answer with a code in it.
			refused(t, door.Handle([]byte(raw)), "bad_request")
			refusedLines = append(refusedLines, raw)
			continue
		}
		recorded(t, ask(t, door, map[string]any{"op": "append", "raw": ln.Raw}), "applying "+ln.ID)
		applied = append(applied, ln.ID)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return applied, refusedLines
}
