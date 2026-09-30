package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"rokh/turn"
)

// Several temporary runs of the command line, one after another and at once,
// with no booth running. The owner's words of 2026-09-29: several continuous
// booths are not to be confused with several temporary runs of the command
// line; both must hold, and each is proven on its own. Every run answered
// recorded is in the ledger read afresh; a run that lost the race to the heads
// it was prepared against, or could not take the turn, is answered so and
// recorded nothing.
//
//	— T10, T8.5, T4.4, contract C9
func TestSeveralCommandLineRunsOneAfterAnotherAndAtOnce(t *testing.T) {
	// Its own folder, processes and sockets, and nothing of this package's
	// state: it runs beside the other tests of T10 that wait on real processes.
	t.Parallel()
	f := newProcessFixture(t)
	recorded := map[string]bool{}

	// One after another: each run writes on the one before it.
	var order []string
	for i := 0; i < 5; i++ {
		a, code, out := cliWrite(f, "--address", "home/runs", "--message", fmt.Sprintf("run %d, one after another", i))
		if code != 0 || a["record"] != "recorded" {
			t.Fatalf("run %d: exit %d %s", i, code, out)
		}
		recorded[fmt.Sprint(a["id"])] = true
		order = append(order, fmt.Sprint(a["id"]))
	}
	onOneLine(t, freshLedger(t, f), order)

	// At once: every run records, and history stays one line.
	const runs = 8
	var mu sync.Mutex
	var failures []string
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, code, out := cliWrite(f, "--address", "home/runs/at-once", "--message", fmt.Sprintf("run %d, at once", i))
			mu.Lock()
			defer mu.Unlock()
			if code != 0 || a["record"] != "recorded" {
				failures = append(failures, fmt.Sprintf("run %d: exit %d %s", i, code, out))
				return
			}
			recorded[fmt.Sprint(a["id"])] = true
		}(i)
	}
	wg.Wait()
	if len(failures) > 0 {
		t.Fatalf("runs at once not recorded:\n%s", strings.Join(failures, "\n"))
	}
	if h := freshLedger(t, f).Heads(); len(h) != 1 {
		t.Fatalf("runs at once split history into %d heads", len(h))
	}

	// At once on the same heads: exactly one run records.
	for round := 0; round < 3; round++ {
		heads := strings.Join(f.heads(t), ",")
		answers := make([]map[string]any, 3)
		codes := make([]int, 3)
		outs := make([]string, 3)
		var race sync.WaitGroup
		for i := range answers {
			race.Add(1)
			go func(i int) {
				defer race.Done()
				answers[i], codes[i], outs[i] = cliWrite(f, "--address", "home/runs/race",
					"--message", fmt.Sprintf("round %d, run %d", round, i), "--expect-heads", heads)
			}(i)
		}
		race.Wait()
		wins := 0
		for i, a := range answers {
			switch {
			case codes[i] == 0 && a["record"] == "recorded":
				wins++
				recorded[fmt.Sprint(a["id"])] = true
			case codes[i] == 1 && a["record"] == "not-recorded" && a["code"] == "precondition_failed":
			default:
				t.Fatalf("round %d, run %d: exit %d %s", round, i, codes[i], outs[i])
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: %d runs recorded on the same heads", round, wins)
		}
	}

	// At once under one attempt: one run records it, and the others answer
	// the recording already made.
	answers := make([]map[string]any, 3)
	codes := make([]int, 3)
	outs := make([]string, 3)
	var same sync.WaitGroup
	for i := range answers {
		same.Add(1)
		go func(i int) {
			defer same.Done()
			answers[i], codes[i], outs[i] = cliWrite(f, "--address", "home/runs/once", "--message", "one request, one name",
				"--attempt", "t10-runs-once")
		}(i)
	}
	same.Wait()
	first := 0
	for i, a := range answers {
		if codes[i] != 0 || a["record"] != "recorded" || a["id"] != answers[0]["id"] {
			t.Fatalf("run %d under one attempt: exit %d %s", i, codes[i], outs[i])
		}
		if a["already"] == false {
			first++
		}
	}
	if first != 1 {
		t.Fatalf("%d runs recorded the one attempt", first)
	}
	recorded[fmt.Sprint(answers[0]["id"])] = true

	// A run that cannot take the turn within its patience says that nothing
	// was recorded, and nothing was.
	before := len(f.events(t))
	lock, err := turn.Acquire(f.dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	a, code, out := cliWrite(f, "--address", "home/runs/busy", "--message", "while another holds the turn")
	waited := time.Since(start)
	lock.Release()
	if code != 1 || a != nil || !strings.Contains(out, "nothing was recorded") {
		t.Fatalf("a run that could not take the turn: exit %d %s", code, out)
	}
	if waited < turnPatience {
		t.Fatalf("the run gave up after %v, before its patience of %v", waited, turnPatience)
	}
	if after := len(f.events(t)); after != before {
		t.Fatalf("a run that could not take the turn left %d events behind", after-before)
	}

	everyRecordedIsThere(t, f, recorded)
	t.Logf("%d runs answered recorded (%d one after another, %d at once, 3 race winners, 1 attempt); a run that could not take the turn waited %v",
		len(recorded), len(order), runs, waited.Round(time.Second))
}
