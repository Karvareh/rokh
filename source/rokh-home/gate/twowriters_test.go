package gate

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"rokh-home/home"
	"rokh/carrier"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/medium"
	"rokh/vessel"
)

// The home's gate and the command line on one home's ledger (T10), as real
// processes: `rokh-home serve` holds the home open behind its gate, the owner
// speaks to it with `rokh-home owner`, and `rokh write` records on the same
// ledger folder from the command line. In turn and at once, every write
// answered recorded is in the ledger read afresh; at once on the same heads
// exactly one records, and the other is answered that the heads moved and
// records nothing.
//
//	— T10, T8.5, T4.4, contract C9

const twoWritersPass = "synthetic passphrase of a gate and a command line"

// buildTool builds one command of the workspace for a test that needs it as a
// process of its own.
func buildTool(t *testing.T, pkg, name string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

type gateHome struct {
	t                 *testing.T
	homeBin, rokhBin  string
	root, run, passFP string
}

// startGateHome makes a synthetic home with `rokh-home init` and serves it
// with `rokh-home serve`, which stops when the test ends.
func startGateHome(t *testing.T) *gateHome {
	t.Helper()
	h := &gateHome{t: t, homeBin: buildTool(t, "rokh-home/cmd/rokh-home", "rokh-home"),
		rokhBin: buildTool(t, "rokh/cmd/rokh", "rokh"), root: filepath.Join(t.TempDir(), "home")}
	h.passFP = filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(h.passFP, []byte(twoWritersPass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(h.homeBin, "init", h.root, "--message", "synthetic genesis of a gate and a command line",
		"--passphrase-file", h.passFP, "--test-iterations", "1000").CombinedOutput(); err != nil {
		t.Fatalf("rokh-home init: %v\n%s", err, out)
	}
	run, err := shortTemp(t, "rkw")
	if err != nil {
		t.Fatal(err)
	}
	h.run = run
	serve := exec.Command(h.homeBin, "serve", h.root, "--run", run, "--passphrase-file", h.passFP)
	stdout, err := serve.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		serve.Process.Signal(syscall.SIGTERM)
		serve.Wait()
		os.RemoveAll(run)
	})
	ready := make(chan string, 1)
	go func() {
		r := bufio.NewReader(stdout)
		line, _ := r.ReadString('\n')
		ready <- line
		for {
			if _, err := r.ReadString('\n'); err != nil {
				return
			}
		}
	}()
	select {
	case line := <-ready:
		var r map[string]any
		if json.Unmarshal([]byte(line), &r) != nil || r["ready"] != true {
			t.Fatalf("rokh-home serve did not say it was ready: %q", line)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("rokh-home serve never became ready")
	}
	return h
}

func (h *gateHome) ledgerDir() string { return filepath.Join(h.root, home.LedgerDir) }

// answerOf reads the first JSON object a command printed.
func answerOf(out []byte) map[string]any {
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "{") {
			var r map[string]any
			if json.Unmarshal([]byte(line), &r) == nil {
				return r
			}
		}
	}
	return nil
}

func exitOf(err error) int {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	}
	return -1
}

// owner is one `rokh-home owner` process asking the gate's ledger. It never
// calls t.Fatal, so goroutines may use it.
func (h *gateHome) owner(req map[string]any) (map[string]any, int, string) {
	b, err := json.Marshal(map[string]any{"request": req})
	if err != nil {
		return nil, -1, err.Error()
	}
	out, err := exec.Command(h.homeBin, "owner", h.root, "--run", h.run, "--passphrase-file", h.passFP,
		"ledger", string(b)).CombinedOutput()
	return answerOf(out), exitOf(err), string(out)
}

// cli is one `rokh write` process on the home's ledger folder, as the owner.
// It never calls t.Fatal.
func (h *gateHome) cli(args ...string) (map[string]any, int, string) {
	args = append([]string{"write", h.ledgerDir()}, args...)
	out, err := exec.Command(h.rokhBin, append(args, "--json", "--passphrase-file", h.passFP)...).CombinedOutput()
	return answerOf(out), exitOf(err), string(out)
}

// fresh reads the home's ledger afresh from its folder, in this process: a new
// opening with the owner's passphrase that shares nothing with the gate.
func (h *gateHome) fresh() *ledger.Ledger {
	h.t.Helper()
	c, _, err := carrier.Open(medium.Dir{Root: h.ledgerDir()}, vessel.Unlock(key.Unlock(twoWritersPass)), rand.Reader, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	v := c.Vessel()
	info := v.Info()
	sess, _, err := key.VesselSession(twoWritersPass, v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
	if err != nil {
		h.t.Fatal(err)
	}
	c.SetSealer(sess)
	heads, err := c.Heads()
	if err != nil {
		h.t.Fatal(err)
	}
	raw, err := c.Get(c.Anchor())
	if err != nil {
		h.t.Fatal(err)
	}
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		h.t.Fatal(err)
	}
	return l
}

// logged is the ledger as `rokh log` prints it from a process of its own.
func (h *gateHome) logged() map[string]bool {
	h.t.Helper()
	out, err := exec.Command(h.rokhBin, "log", h.ledgerDir(), "--json", "--passphrase-file", h.passFP).CombinedOutput()
	if err != nil {
		h.t.Fatalf("rokh log: %v\n%s", err, out)
	}
	ids := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		var r map[string]any
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &r) == nil {
			ids[fmt.Sprint(r["id"])] = true
		}
	}
	return ids
}

// status is the gate's answer about its ledger.
func (h *gateHome) status() (accepted float64, heads []string) {
	h.t.Helper()
	st, code, out := h.owner(map[string]any{"op": "status"})
	if code != 0 || st["ok"] != true {
		h.t.Fatalf("the gate's status: exit %d %s", code, out)
	}
	list, _ := st["heads"].([]any)
	for _, x := range list {
		heads = append(heads, fmt.Sprint(x))
	}
	sort.Strings(heads)
	accepted, _ = st["accepted"].(float64)
	return accepted, heads
}

func TestTheGateAndTheCommandLineWriteOneHomesLedgerInTurnAndAtOnce(t *testing.T) {
	h := startGateHome(t)
	recorded := map[string]bool{}

	// In turn: each writes on what the other recorded.
	var order []string
	for i := 0; i < 4; i++ {
		g, code, out := h.owner(map[string]any{"op": "write", "address": "home/gate", "message": fmt.Sprintf("the gate, in turn %d", i)})
		if code != 0 || g["record"] != "recorded" {
			t.Fatalf("the gate in turn %d: exit %d %s", i, code, out)
		}
		recorded[fmt.Sprint(g["event"])] = true
		order = append(order, fmt.Sprint(g["event"]))
		c, code, out := h.cli("--address", "home/cli", "--message", fmt.Sprintf("the command line, in turn %d", i))
		if code != 0 || c["record"] != "recorded" {
			t.Fatalf("the command line in turn %d: exit %d %s", i, code, out)
		}
		recorded[fmt.Sprint(c["id"])] = true
		order = append(order, fmt.Sprint(c["id"]))
	}
	l := h.fresh()
	for i := 1; i < len(order); i++ {
		id, err := frame.ParseID(order[i])
		if err != nil {
			t.Fatal(err)
		}
		e, ok := l.Get(id)
		if !ok || len(e.Event.Parents) != 1 || e.Event.Parents[0].String() != order[i-1] {
			t.Fatalf("recording %d does not rest on the one recorded before it (%s): %v", i, order[i-1], e.Event.Parents)
		}
	}
	if acc, heads := h.status(); acc != float64(len(order)+1) || len(heads) != 1 || heads[0] != order[len(order)-1] {
		t.Fatalf("the gate answers %v accepted and heads %v; the folder holds %d and %s", acc, heads, len(order)+1, order[len(order)-1])
	}

	// At once: three gate writers and three command-line writers, three
	// writes each.
	var mu sync.Mutex
	var failures []string
	var wg sync.WaitGroup
	for w := 0; w < 3; w++ {
		wg.Add(2)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				g, code, out := h.owner(map[string]any{"op": "write", "address": "home/gate/at-once", "message": fmt.Sprintf("gate writer %d, %d", w, i)})
				mu.Lock()
				if code != 0 || g["record"] != "recorded" {
					failures = append(failures, fmt.Sprintf("gate writer %d, %d: exit %d %s", w, i, code, out))
				} else {
					recorded[fmt.Sprint(g["event"])] = true
				}
				mu.Unlock()
			}
		}(w)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				c, code, out := h.cli("--address", "home/cli/at-once", "--message", fmt.Sprintf("cli writer %d, %d", w, i))
				mu.Lock()
				if code != 0 || c["record"] != "recorded" {
					failures = append(failures, fmt.Sprintf("cli writer %d, %d: exit %d %s", w, i, code, out))
				} else {
					recorded[fmt.Sprint(c["id"])] = true
				}
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	if len(failures) > 0 {
		t.Fatalf("writes at once not recorded:\n%s", strings.Join(failures, "\n"))
	}

	// At once on the same heads: exactly one of the two records.
	for round := 0; round < 3; round++ {
		_, heads := h.status()
		var g, c map[string]any
		var gCode, cCode int
		var gOut, cOut string
		var race sync.WaitGroup
		race.Add(2)
		go func() {
			defer race.Done()
			g, gCode, gOut = h.owner(map[string]any{"op": "write", "address": "home/race",
				"message": fmt.Sprintf("round %d, the gate", round), "expect_heads": heads})
		}()
		go func() {
			defer race.Done()
			c, cCode, cOut = h.cli("--address", "home/race", "--message", fmt.Sprintf("round %d, the command line", round),
				"--expect-heads", strings.Join(heads, ","))
		}()
		race.Wait()
		wins := 0
		switch {
		case gCode == 0 && g["record"] == "recorded":
			wins++
			recorded[fmt.Sprint(g["event"])] = true
		case g != nil && g["record"] == "not-recorded" && g["code"] == "precondition_failed" && g["event"] == nil:
		default:
			t.Fatalf("round %d, the gate: exit %d %s", round, gCode, gOut)
		}
		switch {
		case cCode == 0 && c["record"] == "recorded":
			wins++
			recorded[fmt.Sprint(c["id"])] = true
		case cCode == 1 && c["record"] == "not-recorded" && c["code"] == "precondition_failed":
		default:
			t.Fatalf("round %d, the command line: exit %d %s", round, cCode, cOut)
		}
		if wins != 1 {
			t.Fatalf("round %d: %d writers recorded on the same heads", round, wins)
		}
	}

	// Every write answered recorded is in the ledger read afresh, and nothing
	// else is but the genesis.
	back := h.logged()
	for id := range recorded {
		if !back[id] {
			t.Errorf("answered recorded, not in the ledger read afresh: %s", id)
		}
	}
	if len(back) != len(recorded)+1 {
		t.Fatalf("the ledger read afresh holds %d events: %d recorded plus the genesis", len(back), len(recorded))
	}
	if out, err := exec.Command(h.rokhBin, "verify", h.ledgerDir(), "--passphrase-file", h.passFP).CombinedOutput(); err != nil {
		t.Fatalf("rokh verify: %v\n%s", err, out)
	}
	// The gate answers from the same ledger.
	want := h.fresh()
	acc, heads := h.status()
	if acc != float64(len(want.Order())) || len(heads) != 1 || heads[0] != want.Heads()[0].String() {
		t.Fatalf("the gate answers %v accepted, heads %v; the folder holds %d, heads %v", acc, heads, len(want.Order()), want.Heads())
	}
	t.Logf("%d writes answered recorded (%d in turn, 18 at once, 3 race winners); the ledger read afresh holds %d events, the gate answers %v",
		len(recorded), len(order), len(back), acc)
}
