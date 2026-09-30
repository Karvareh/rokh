package native

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A conductor stand-in that really is a process: a stop has to signal something,
// wait for it and close its files, and a test that skips that tests nothing.
// The shell traps the interrupt and leaves by itself, which is what makes the
// ending a consistent one.
func liveSession(t *testing.T, m *Manager, p *Profile, br *bridge) *session {
	t.Helper()
	dir := fakeRuntime(t, p)
	p.RuntimeDir = dir
	p.Live = &Live{PID: 0, RuntimeDir: dir, Generation: p.Generation}
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	// The marker is written after the trap is installed. Without waiting for it
	// the interrupt can land on a shell that has not armed yet, and the test
	// would be measuring its own startup rather than the stop.
	marker := filepath.Join(dir, "stand-in-ready")
	cmd := exec.Command("/bin/sh", "-c", `trap 'exit 0' INT; : > "$1"; while :; do sleep 0.05; done`, "sh", marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logf, err := os.CreateTemp(dir, "conductor-stand-in-")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
		}
	})
	waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })
	lock, err := holdRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{cmd: cmd, log: logf, lock: lock, bridge: br, dir: dir}
	p.Live.PID = cmd.Process.Pid
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.live[p.ID] = s
	m.mu.Unlock()
	return s
}

// headBridge answers chain_head with a well-formed head and everything else
// with an empty result.
func headBridge(t *testing.T, seq int) *bridge {
	t.Helper()
	// The action arrives as the byte array a zome answer carries.
	return fakeBridge(t, `{"ok":true,"result":{"seq":`+itoa(seq)+`,"action":`+testHashJSON()+`}}`)
}

func testHashJSON() string {
	b, _ := json.Marshal(validHashArray())
	return string(b)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

// A put-away that failed to seal leaves nothing live behind it, so a second one
// that looked only at live sessions would find an empty list and call the home
// closed over a chain nobody sealed. The second attempt has to find the profile
// from the sealed record and really seal it.
func TestASecondPutAwaySealsWhatTheFirstCouldNot(t *testing.T) {
	m, h := hookedManager(t)
	p := newProfile(t, m)
	liveSession(t, m, p, headBridge(t, 7))

	h.putErr = errors.New("synthetic: the store refused to seal")
	first := m.Quiesce("round4")
	if first["ok"] != false {
		t.Fatalf("a checkpoint that did not seal is not a clean put-away: %v", first)
	}
	m.mu.Lock()
	live := len(m.live)
	m.mu.Unlock()
	if live != 0 {
		t.Fatalf("the stop should have taken the session out of this process: %d left", live)
	}
	after, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !pendingSeal(after) {
		t.Fatal("an identity stopped and not sealed is unfinished business, and has to be seen as such")
	}

	h.putErr = nil
	second := m.Quiesce("round4")
	if second["ok"] != true {
		t.Fatalf("the second attempt should have sealed it: %v", second)
	}
	rows, _ := second["profiles"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("the second put-away should have found exactly the unsealed identity: %v", second["profiles"])
	}
	sealedAt, _ := rows[0]["checkpoint"].(map[string]any)
	if sealedAt["object"] == nil {
		t.Fatalf("no object was sealed: %v", rows[0])
	}
	final, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pendingSeal(final) {
		t.Fatalf("after a successful put-away nothing should be left unsealed: %+v", final.Snapshot)
	}
}

// The same thing across a reopened broker: the process that stopped the
// conductor is gone, and a new one over the same sealed store has to pick up
// the identity that was never sealed.
func TestAReopenedBrokerSealsWhatWasLeftBehind(t *testing.T) {
	seal := &hooked{Sealer: sealed(t)}
	first := New(seal, Binaries{})
	p := newProfile(t, first)
	liveSession(t, first, p, headBridge(t, 4))
	seal.putErr = errors.New("synthetic: the store refused to seal")
	if report := first.Quiesce("round4"); report["ok"] != false {
		t.Fatalf("want a failed put-away, got %v", report)
	}
	seal.putErr = nil

	// A brand new manager over the same home: no live sessions at all.
	reopened := New(seal, Binaries{})
	if ids, err := reopened.toPutAway(); err != nil || len(ids) != 1 || ids[0] != p.ID {
		t.Fatalf("a reopened broker should see the unsealed identity: %v", ids)
	}
	report := reopened.Quiesce("round4-after-reopen")
	if report["ok"] != true {
		t.Fatalf("the reopened broker should have sealed it: %v", report)
	}
	final, err := reopened.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Snapshot == nil || !final.Snapshot.Complete {
		t.Fatalf("nothing was sealed: %+v", final.Snapshot)
	}
}

// A profile save that fails after the object is sealed is not a clean put-away
// either, and the attempt that follows it must still be able to finish the job.
func TestAProfileSaveThatFailsIsNotACleanPutAway(t *testing.T) {
	m, h := hookedManager(t)
	p := newProfile(t, m)
	liveSession(t, m, p, headBridge(t, 9))
	// The stop saves the profile once; the checkpoint saves it again. Only the
	// second one fails, which is the case where an object exists and the
	// identity does not know about it.
	h.pointerErr, h.pointerOn, h.pointerSkip = errors.New("synthetic: the profile could not be saved"), ptrProfile, 1

	report := m.Quiesce("round4-save-fails")
	if report["ok"] != false {
		t.Fatalf("a put-away whose profile did not move is not clean: %v", report)
	}
	rows, _ := report["profiles"].([]map[string]any)
	if len(rows) != 1 || !strings.Contains(fmtAny(rows[0]["checkpoint_error"]), "did not move to it") {
		t.Fatalf("the report should name what happened: %v", report["profiles"])
	}

	h.pointerErr = nil
	again := m.Quiesce("round4-save-fails")
	if again["ok"] != true {
		t.Fatalf("the same attempt over the same state should finish: %v", again)
	}
	final, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Snapshot == nil || final.Generation != 2 {
		t.Fatalf("the identity should have moved on the second attempt: %+v", final.Snapshot)
	}
}

// Two closes racing. The second must wait for the first and be given the same
// answer: a failure the first one found must not be lost behind an empty
// success, because that is exactly what lets a home shut over unsealed state.
func TestASecondQuiesceJoinsTheFirstRatherThanAnsweringOverIt(t *testing.T) {
	m, h := hookedManager(t)
	p := newProfile(t, m)
	// The first put-away is caught in the middle of its stop: the head read is
	// the point where it lets the lock go, which is exactly where a second close
	// could get in. Holding it there is how this is measured rather than hoped.
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	liveSession(t, m, p, scriptedBridge(t, func(id float64) string {
		once.Do(func() { close(entered); <-release })
		return `{"ok":true,"id":` + itoa(int(id)) + `,"result":{"seq":3,"action":` + testHashJSON() + `}}`
	}))
	h.putErr = errors.New("synthetic: the store refused to seal")

	answers := make(chan map[string]any, 2)
	go func() { answers <- m.Quiesce("round4-race") }()
	<-entered
	second := make(chan map[string]any, 1)
	go func() {
		r := m.Quiesce("round4-race")
		second <- r
		answers <- r
	}()
	// The second close is in and waiting on the first before the first can move.
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.quiescing != nil })
	select {
	case r := <-second:
		t.Fatalf("the second close answered while the first was still stopping a conductor: %v", r)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	reports := []map[string]any{<-answers, <-answers}
	if reports[0]["joined"] == true {
		reports[0], reports[1] = reports[1], reports[0]
	}

	for i, r := range reports {
		if r["ok"] != false {
			t.Fatalf("close %d was told the engine was put away when it was not: %v", i, r)
		}
	}
	if reports[1]["joined"] != true {
		t.Fatalf("the second close should have joined the first, not run its own: %v", reports[1])
	}
	if reports[0]["started_utc"] != reports[1]["started_utc"] {
		t.Fatal("both closes should be answering about the same put-away")
	}
}

// A drain is only a drain if the door is shut first. Work that arrives while a
// stop is waiting has to be turned away, not counted after the wait has passed.
func TestWorkThatArrivesDuringAStopIsRefused(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	hold := make(chan struct{})
	var once sync.Once
	br := scriptedBridge(t, func(id float64) string {
		// The first call is held open; everything after it answers at once.
		once.Do(func() { <-hold })
		return `{"ok":true,"id":` + itoa(int(id)) + `,"result":{"seq":2,"action":` + testHashJSON() + `}}`
	})
	s := liveSession(t, m, p, br)

	inFlight := make(chan error, 1)
	go func() {
		_, err := m.Zome(p.ID, "list_manuscripts", nil, 20*time.Second)
		inFlight <- err
	}()
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return s.calls == 1 })

	stopped := make(chan error, 1)
	go func() {
		_, err := m.Stop(p.ID)
		stopped <- err
	}()
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return s.draining })

	if _, err := m.Zome(p.ID, "list_manuscripts", nil, time.Second); !errors.Is(err, ErrStopping) {
		t.Fatalf("work arriving during a stop must be refused, got %v", err)
	}
	close(hold)
	if err := <-inFlight; err != nil {
		t.Fatalf("the call that was already out should still have been answered: %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := m.Zome(p.ID, "list_manuscripts", nil, time.Second); !errors.Is(err, ErrNotLive) {
		t.Fatalf("after the stop there is nothing to call, got %v", err)
	}
}

// Two stops on one session: the second waits for the first rather than waiting
// on the same process twice and closing its files twice.
func TestTwoStopsDoNotBothWaitOnOneProcess(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	liveSession(t, m, p, headBridge(t, 5))
	out := make(chan map[string]any, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := m.Stop(p.ID)
			if err != nil {
				t.Errorf("stop: %v", err)
			}
			out <- r
		}()
	}
	wg.Wait()
	close(out)
	stops, already := 0, 0
	for r := range out {
		if r["already_stopped"] == true {
			already++
		} else if r["consistent"] == true {
			stops++
		}
	}
	if stops != 1 || already != 1 {
		t.Fatalf("one stop and one already-stopped, got %d and %d", stops, already)
	}
}

// A head is what a checkpoint is checked against. An answer that carries no
// head is not one, and the previous stop's answer must not be left standing in
// its place.
func TestAMisshapenAnswerIsNotAHead(t *testing.T) {
	for _, answer := range []string{
		`{"ok":true,"result":{"seq":4}}`,
		`{"ok":true,"result":"the conductor said something else"}`,
		`{"ok":true}`,
	} {
		m := manager(t)
		p := newProfile(t, m)
		// A head left over from an earlier stop, which is the thing that must
		// not be reused.
		p.LastHead, p.HeadRead = ChainHead{Seq: 99, Action: "uhCkkEARLIER"}, true
		if err := m.saveProfile(p); err != nil {
			t.Fatal(err)
		}
		liveSession(t, m, p, fakeBridge(t, answer))
		out, err := m.Stop(p.ID)
		if err != nil {
			t.Fatalf("%s: %v", answer, err)
		}
		if out["head_read"] != false || out["head_error"] == nil {
			t.Fatalf("%s: a head that was not read should say so: %v", answer, out)
		}
		after, err := m.loadProfile(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.HeadRead {
			t.Fatalf("%s: the earlier stop's head must not stand in for this one", answer)
		}
		if _, err := m.Checkpoint(p.ID, "after-a-head-nobody-read"); !errors.Is(err, ErrUnknownEnding) {
			t.Fatalf("%s: want ErrUnknownEnding, got %v", answer, err)
		}
	}
}

// The host's ceiling is asked before a start makes, opens or writes anything.
func TestTheCeilingIsAskedBeforeAnythingIsMade(t *testing.T) {
	m := New(sealed(t), fakeEngine(t))
	refusal := errors.New("gate: host cannot supply this session: host_network_not_offered")
	asked := 0
	m.UnderCeiling(Ceiling{
		Take: func(string) (func(), func(), error) { asked++; return nil, nil, refusal },
		Give: func(string) {},
	})
	p := newProfile(t, m)
	dir := filepath.Join(t.TempDir(), "runtime-that-should-not-appear")
	if _, err := m.Start(StartInput{Profile: p.ID, RuntimeDir: dir}); !errors.Is(err, refusal) {
		t.Fatalf("want the host's refusal, got %v", err)
	}
	if asked != 1 {
		t.Fatalf("the ceiling should have been asked exactly once, was asked %d times", asked)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused start must leave no runtime folder behind")
	}
}

// A start that fails gives its slot back; one that succeeds keeps it until the
// conductor is stopped.
func TestTheSlotIsGivenBackWhenAStartFails(t *testing.T) {
	m := New(sealed(t), Binaries{}) // no engine: the start cannot succeed
	committed, released := 0, 0
	m.UnderCeiling(Ceiling{
		Take: func(string) (func(), func(), error) {
			return func() { committed++ }, func() { released++ }, nil
		},
		Give: func(string) {},
	})
	p := newProfile(t, m)
	if _, err := m.Start(StartInput{Profile: p.ID, RuntimeDir: t.TempDir()}); !errors.Is(err, ErrNoEngine) {
		t.Fatalf("want ErrNoEngine, got %v", err)
	}
	if committed != 0 || released != 1 {
		t.Fatalf("a failed start keeps no slot: committed %d, released %d", committed, released)
	}
}

// A stop gives the conductor's slot back to the host.
func TestAStopGivesTheSlotBack(t *testing.T) {
	m := manager(t)
	given := []string{}
	m.UnderCeiling(Ceiling{Give: func(profile string) { given = append(given, profile) }})
	p := newProfile(t, m)
	s := liveSession(t, m, p, headBridge(t, 2))
	s.release = func() { m.ceiling.Give(p.ID) }
	if _, err := m.Stop(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(given) != 1 || given[0] != p.ID {
		t.Fatalf("the slot should have gone back exactly once: %v", given)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the condition never came true")
}

func fmtAny(v any) string {
	s, _ := v.(string)
	return s
}

var _ = strings.Contains
