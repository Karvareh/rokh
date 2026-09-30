package gate

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// baselineFD is the highest descriptor a child of exec.Command opens on its
// own: 0, 1, 2 and what /bin/sh and ls take for themselves.
const baselineFD = 4

// A connection the gate makes for one program must reach no other process. The
// descriptors of a socket pair are born without close-on-exec and are marked in
// a second step; a program started in that window inherits both ends, and holds
// a live gate connection at another program's authority.
//
// The test starts programs while connections are being made, and reads the
// descriptors each one was handed. It cannot prove a race absent — a quiet run
// is not a proof — but it never reports one that did not happen: every
// descriptor it counts was really inherited.
//
// It makes connections at a pace, not in a spin: under the race detector a
// tight loop over the fork lock starves the very exec it is trying to catch.
func TestAGateConnectionReachesNoOtherChild(t *testing.T) {
	f := newGate(t)
	a, _ := f.program("a", map[string]string{"read": "a"}, nil)

	var wg sync.WaitGroup
	var bindings atomic.Int64
	bindErrors := make(chan error, 2)
	stop := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				file, err := f.s.Bind(a)
				if err != nil {
					bindErrors <- err
					return
				}
				bindings.Add(1)
				file.Close()
				time.Sleep(100 * time.Microsecond)
			}
		}()
	}

	worst, rounds, executed := 0, 0, 0
	deadline := time.Now().Add(30 * time.Second)
	for rounds = 0; rounds < 400 && time.Now().Before(deadline); rounds++ {
		out, err := exec.Command("/bin/sh", "-c", "ls /dev/fd").Output()
		if err != nil {
			continue
		}
		executed++
		high := 0
		for _, line := range strings.Fields(string(out)) {
			if n, err := strconv.Atoi(line); err == nil && n > high {
				high = n
			}
		}
		if high > worst {
			worst = high
			t.Logf("round %d: the child's highest descriptor was %d (%q)", rounds, high,
				strings.Join(strings.Fields(string(out)), ","))
		}
	}
	close(stop)
	wg.Wait()
	close(bindErrors)
	for err := range bindErrors {
		t.Errorf("connection producer failed: %v", err)
	}
	if executed < 100 || bindings.Load() == 0 {
		t.Fatalf("insufficient actual work: %d executed children and %d bound connections", executed, bindings.Load())
	}
	// A child of exec.Command sees 0, 1, 2 and the descriptors the shell and ls
	// open for themselves, which on this platform reach baselineFD. Anything
	// above that came from this process.
	if worst > baselineFD {
		t.Errorf("an unrelated child inherited descriptor %d over %d rounds", worst, rounds)
	} else {
		t.Logf("no inherited descriptor in %d executed children and %d bindings (highest %d)", executed, bindings.Load(), worst)
	}
}
