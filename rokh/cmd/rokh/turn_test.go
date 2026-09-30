package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// buildRokh builds this command, for the tests that need a second process.
func buildRokh(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "rokh")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

type processFixture struct {
	bin, dir, passFile string
}

func newProcessFixture(t *testing.T) processFixture {
	t.Helper()
	base := t.TempDir()
	f := processFixture{bin: buildRokh(t), dir: filepath.Join(base, "carrier"), passFile: filepath.Join(base, "pass")}
	if err := os.Mkdir(f.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.passFile, []byte("synthetic test passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := f.run("init", f.dir, "--message", "synthetic genesis"); code != 0 {
		t.Fatalf("init: exit %d\n%s", code, out)
	}
	return f
}

// run executes the command with the fixture's passphrase file and returns its
// combined output and exit status. It never calls t.Fatal, so goroutines may
// use it.
func (f processFixture) run(args ...string) (string, int) {
	cmd := exec.Command(f.bin, append(args, "--passphrase-file", f.passFile)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	return string(out) + err.Error(), -1
}

// events reads the ledger back through a fresh process: the recorded truth.
func (f processFixture) events(t *testing.T) []map[string]any {
	t.Helper()
	out, code := f.run("log", f.dir, "--json")
	if code != 0 {
		t.Fatalf("log: exit %d\n%s", code, out)
	}
	var rows []map[string]any
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	return rows
}

// heads reads the ledger's heads from the carrier itself, as the daemon's
// status does: the references, walked.
func (f processFixture) heads(t *testing.T) []string {
	t.Helper()
	s, err := openSession(f.dir, "synthetic test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var heads []string
	for _, h := range s.led.Heads() {
		heads = append(heads, h.String())
	}
	return heads
}

func answerIn(out string) map[string]any {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "{") {
			var r map[string]any
			if json.Unmarshal([]byte(line), &r) == nil {
				return r
			}
		}
	}
	return nil
}

// Two command-line writers, each its own process, race on the same heads.
// Exactly one records; the other exits 1 and says the heads moved. Nothing is
// orphaned: the ledger read back afterwards holds every recorded event.
//
//	— T8.5, T4.4
func TestTwoCommandLineWritersOneWinner(t *testing.T) {
	f := newProcessFixture(t)
	for round := 0; round < 3; round++ {
		expect := strings.Join(f.heads(t), ",")
		outs := make([]string, 2)
		codes := make([]int, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				outs[i], codes[i] = f.run("write", f.dir, "--address", "home/race",
					"--message", fmt.Sprintf("round %d writer %d", round, i),
					"--expect-heads", expect, "--json")
			}(i)
		}
		wg.Wait()
		wins := 0
		for i := range outs {
			a := answerIn(outs[i])
			switch codes[i] {
			case 0:
				wins++
				if a["record"] != "recorded" {
					t.Fatalf("round %d: exit 0 without recorded: %s", round, outs[i])
				}
			case 1:
				if a["record"] != "not-recorded" || a["code"] != "precondition_failed" {
					t.Fatalf("round %d: the loser should say the heads moved: %s", round, outs[i])
				}
			default:
				t.Fatalf("round %d: exit %d: %s", round, codes[i], outs[i])
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: %d writers recorded on the same heads", round, wins)
		}
		// The genesis, the owner's keyring generation recorded with it, and
		// one winner per round.
		if got := len(f.events(t)); got != round+3 {
			t.Fatalf("round %d: %d events read back, want %d", round, got, round+3)
		}
	}
}

// The daemon and the command line write one carrier at the same time. Every
// event either of them says it recorded is in the ledger read back afterwards,
// and nothing else is: the two doors take one turn.
//
// The socket speaks rokh.booth/1: each connection binds as the owner (hello,
// prove) before it writes, where the 0.9 socket took a bare request line. The
// ledger read back holds what init recorded, the genesis and the owner's
// keyring generation, beside what the two doors recorded.
//
//	— T8.5, T4.4, T10
func TestTheDaemonAndTheCommandLineTakeOneTurn(t *testing.T) {
	f := newProcessFixture(t)
	root := ownerRootOf(t, f)
	sockDir, err := shortTemp(t, "rkt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockDir)
	sock := filepath.Join(sockDir, "d.sock")
	d := exec.Command(f.bin, "daemon", f.dir, "--socket", sock, "--passphrase-file", f.passFile)
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { d.Process.Signal(os.Interrupt); d.Wait() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon never listened")
		}
		time.Sleep(50 * time.Millisecond)
	}

	var mu sync.Mutex
	recorded := map[string]bool{}
	var failures []string
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, code := f.run("write", f.dir, "--address", "home/cli", "--message", fmt.Sprintf("cli %d", i), "--json")
			a := answerIn(out)
			mu.Lock()
			defer mu.Unlock()
			if code != 0 || a["record"] != "recorded" {
				failures = append(failures, fmt.Sprintf("cli %d: exit %d %s", i, code, out))
				return
			}
			recorded[fmt.Sprint(a["id"])] = true
		}(i)
	}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, conn, err := bindOwner(sock, root)
			if err != nil {
				mu.Lock()
				failures = append(failures, err.Error())
				mu.Unlock()
				return
			}
			defer conn.Close()
			a, err := c.Call("write", map[string]any{"address": "home/socket", "message": fmt.Sprintf("socket %d", i)})
			mu.Lock()
			defer mu.Unlock()
			if err != nil || a["record"] != "recorded" {
				failures = append(failures, fmt.Sprintf("socket %d: %v %v", i, err, a))
				return
			}
			recorded[fmt.Sprint(a["event"])] = true
		}(i)
	}
	wg.Wait()
	if len(failures) > 0 {
		t.Fatalf("writes failed:\n%s", strings.Join(failures, "\n"))
	}
	back := map[string]bool{}
	for _, r := range f.events(t) {
		back[fmt.Sprint(r["id"])] = true
	}
	for id := range recorded {
		if !back[id] {
			t.Errorf("recorded %s is not in the ledger read back: orphaned", id)
		}
	}
	if len(back) != len(recorded)+2 {
		t.Fatalf("%d events read back, %d recorded plus the genesis and the owner's keyring generation", len(back), len(recorded))
	}
	if out, code := f.run("verify", f.dir); code != 0 {
		t.Fatalf("verify: exit %d\n%s", code, out)
	}
}

// A writer killed at an arbitrary moment leaves either its event recorded or
// nothing, never a reference to bytes that are not there. The attempt query
// then says which, and repeating the same attempt records at most once.
//
//	— T8.5, T4.4, T10.4
func TestAKilledWriterLeavesAnAnswerableEnding(t *testing.T) {
	f := newProcessFixture(t)
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("kill-%d", i)
		cmd := exec.Command(f.bin, "write", f.dir, "--address", "home/killed", "--message", name,
			"--attempt", name, "--passphrase-file", f.passFile)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(200+rng.Intn(700)) * time.Millisecond)
		cmd.Process.Signal(syscall.SIGKILL)
		cmd.Wait()

		out, code := f.run("attempt", f.dir, "--attempt", name, "--json")
		if code != 0 {
			t.Fatalf("%s: the attempt query failed: %s", name, out)
		}
		a := answerIn(out)
		inLog := 0
		for _, r := range f.events(t) {
			if a["id"] != nil && fmt.Sprint(r["id"]) == fmt.Sprint(a["id"]) {
				inLog++
			}
		}
		switch a["record"] {
		case "recorded":
			if inLog != 1 {
				t.Fatalf("%s: the query says recorded, the ledger holds it %d times", name, inLog)
			}
		case "not-recorded":
		default:
			t.Fatalf("%s: untyped query answer: %s", name, out)
		}
		again, code := f.run("write", f.dir, "--address", "home/killed", "--message", name, "--attempt", name, "--json")
		if code != 0 {
			t.Fatalf("%s: the repeat failed: %s", name, again)
		}
		r := answerIn(again)
		if a["record"] == "recorded" && (r["already"] != true || r["id"] != a["id"]) {
			t.Fatalf("%s: a recorded attempt was recorded again: %s", name, again)
		}
		if out, code := f.run("verify", f.dir); code != 0 {
			t.Fatalf("%s: the carrier does not verify after the kill: %s", name, out)
		}
	}
	perName := map[string]int{}
	for _, r := range f.events(t) {
		if fmt.Sprint(r["address"]) == "home/killed" {
			perName[fmt.Sprint(r["payload"])]++
		}
	}
	for payload, n := range perName {
		if n != 1 {
			t.Fatalf("the payload %s is in the ledger %d times", payload, n)
		}
	}
}
