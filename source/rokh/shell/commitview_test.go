package shell

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"rokh/frame"
)

// The sentence surface held open beside the command line (T10, T2): the
// surface is its own process, standing on a ledger, while `rokh write` in
// other processes records on the same carrier. A sentence recorded after the
// command line wrote takes the command line's event as its parent, and every
// recording either side answered as recorded is in the ledger read afresh.
// Before T2 the surface judged and picked its parent from what its opening
// had read, and moved the branch over the command line's events.

// buildRokhCommand builds the rokh command, which carries this surface, into
// the test's own folder.
func buildRokhCommand(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "rokh")
	cmd := exec.Command("go", "build", "-o", bin, "rokh/cmd/rokh")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// heldSurface is the line surface running as a process, held open on a pipe:
// every sentence it answers is one line of its standard output, and a
// sentence it refuses is one line of its standard error.
type heldSurface struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   chan string
	notes chan string
	done  chan struct{} // closed when the process has ended; ended says how
	ended error
}

func holdSurface(t *testing.T, bin, vault, passFile string) *heldSurface {
	t.Helper()
	cmd := exec.Command(bin, vault)
	cmd.Env = append(os.Environ(), "ROKH_PASSPHRASE_FILE="+passFile)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	h := &heldSurface{cmd: cmd, in: in, out: make(chan string, 64), notes: make(chan string, 64), done: make(chan struct{})}
	lines := func(r io.Reader, to chan<- string) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			to <- sc.Text()
		}
		close(to)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go lines(stdout, h.out)
	go lines(stderr, h.notes)
	t.Cleanup(func() {
		h.in.Close()
		select {
		case <-h.done:
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
		}
	})
	go func() { h.ended = cmd.Wait(); close(h.done) }()
	return h
}

// say sends one sentence and waits for its answer or its refusal.
func (h *heldSurface) say(t *testing.T, sentence string) (answer string, refused bool) {
	t.Helper()
	if _, err := fmt.Fprintln(h.in, sentence); err != nil {
		t.Fatalf("%q: the surface does not take it: %v", sentence, err)
	}
	select {
	case a, ok := <-h.out:
		if !ok {
			t.Fatalf("%q: the surface ended", sentence)
		}
		return a, false
	case n, ok := <-h.notes:
		if !ok {
			t.Fatalf("%q: the surface ended", sentence)
		}
		return n, true
	case <-time.After(2 * time.Minute):
		t.Fatalf("%q: no answer in two minutes", sentence)
	}
	return "", false
}

var fullID = regexp.MustCompile(`[0-9a-f]{64}`)

func TestAnOpenSurfaceAndTheCommandLineWriteInTurnAndLoseNothing(t *testing.T) {
	bin := buildRokhCommand(t)
	base := t.TempDir()
	vault := filepath.Join(base, "vault")
	if err := os.Mkdir(vault, 0o700); err != nil {
		t.Fatal(err)
	}
	passFile := filepath.Join(base, "pass")
	const pass = "synthetic surface passphrase"
	if err := os.WriteFile(passFile, []byte(pass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(vault, LedgersFolder, "main")
	commandLine := func(args ...string) (string, int) {
		cmd := exec.Command(bin, append(args, "--passphrase-file", passFile)...)
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		if err != nil {
			return string(out) + err.Error(), -1
		}
		return string(out), 0
	}

	h := holdSurface(t, bin, vault, passFile)
	if a, refused := h.say(t, "open a new ledger named main"); refused {
		t.Fatalf("the surface did not make its ledger: %s", a)
	}

	// In turn: a sentence, then the command line, four times over. Each
	// recording is answered before the next begins.
	var recorded []frame.ID
	for i := 0; i < 4; i++ {
		if a, refused := h.say(t, fmt.Sprintf("write at notes/surface: line %d of the surface", i)); refused {
			t.Fatalf("sentence %d was not drafted: %s", i, a)
		}
		a, refused := h.say(t, "write")
		if refused {
			t.Fatalf("sentence %d, said after the command line recorded, was refused: %s", i, a)
		}
		id, err := frame.ParseID(fullID.FindString(a))
		if err != nil {
			t.Fatalf("sentence %d: the answer names no event: %q", i, a)
		}
		recorded = append(recorded, id)

		out, code := commandLine("write", dir, "--address", "notes/command-line",
			"--message", fmt.Sprintf("line %d of the command line", i), "--json")
		var r map[string]any
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &r) == nil {
				break
			}
		}
		if code != 0 || r["record"] != "recorded" {
			t.Fatalf("command line %d: exit %d\n%s", i, code, out)
		}
		cid, err := frame.ParseID(fmt.Sprint(r["id"]))
		if err != nil {
			t.Fatalf("command line %d: %v", i, err)
		}
		recorded = append(recorded, cid)
	}
	// And the surface once more, on the command line's last event.
	if a, refused := h.say(t, "write at notes/surface: the last line"); refused {
		t.Fatalf("the last sentence was not drafted: %s", a)
	}
	a, refused := h.say(t, "write")
	if refused {
		t.Fatalf("the last sentence was refused: %s", a)
	}
	last, err := frame.ParseID(fullID.FindString(a))
	if err != nil {
		t.Fatalf("the last answer names no event: %q", a)
	}
	recorded = append(recorded, last)

	h.in.Close()
	select {
	case <-h.done:
		if h.ended != nil {
			t.Fatalf("the surface ended with %v", h.ended)
		}
	case <-time.After(time.Minute):
		t.Fatal("the surface did not end when its input did")
	}

	// The ledger read afresh, by a new process: every recording answered as
	// recorded is there.
	out, code := commandLine("log", dir, "--json")
	if code != 0 {
		t.Fatalf("log: exit %d\n%s", code, out)
	}
	held := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		var row map[string]any
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &row) == nil {
			held[fmt.Sprint(row["id"])] = true
		}
	}
	for i, id := range recorded {
		if !held[id.String()] {
			t.Errorf("recording %d (%s) was answered recorded and is not in the ledger read afresh", i, id.Short())
		}
	}

	// And they are one line of history: each rests on the one recorded
	// before it, whichever writer made it.
	c, _, err := OpenCarrier(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	led, err := replay(c)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(recorded); i++ {
		e, ok := led.Get(recorded[i])
		if !ok {
			continue // already reported above
		}
		if len(e.Event.Parents) != 1 || e.Event.Parents[0] != recorded[i-1] {
			t.Errorf("recording %d rests on %v, not on recording %d (%s)", i, e.Event.Parents, i-1, recorded[i-1].Short())
		}
	}
	t.Logf("%d recordings in turn, surface and command line, looked for in the ledger read afresh", len(recorded))
}

// Step 7 on the sentence surface: a surface that stays open seals by the
// keyring as it is now. A key the owner adds through the command line while
// the surface is open reads what the surface records afterwards, with its
// own passphrase. Before T2 the surface sealed by the keyring its opening
// had read, and the key was not named.
func TestAnOpenSurfaceSealsByTheKeyringAsItIsNow(t *testing.T) {
	bin := buildRokhCommand(t)
	base := t.TempDir()
	vault := filepath.Join(base, "vault")
	if err := os.Mkdir(vault, 0o700); err != nil {
		t.Fatal(err)
	}
	passFile := filepath.Join(base, "pass")
	if err := os.WriteFile(passFile, []byte("synthetic surface passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyPass := filepath.Join(base, "key.pass")
	if err := os.WriteFile(keyPass, []byte("synthetic key passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(vault, LedgersFolder, "main")
	run := func(pass string, args ...string) string {
		t.Helper()
		out, err := exec.Command(bin, append(args, "--passphrase-file", pass)...).CombinedOutput()
		if err != nil {
			t.Fatalf("rokh %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	h := holdSurface(t, bin, vault, passFile)
	if a, refused := h.say(t, "open a new ledger named main"); refused {
		t.Fatalf("the surface did not make its ledger: %s", a)
	}
	// While the surface stays open, the owner adds a key on the command line.
	run(passFile, "key", "add", dir, "--name", "reader", "--reads", "notes", "--key-passphrase-file", keyPass)
	if a, refused := h.say(t, "write at notes/after-key: recorded by the open surface after the key was added"); refused {
		t.Fatalf("not drafted: %s", a)
	}
	a, refused := h.say(t, "write")
	if refused {
		t.Fatalf("the surface did not record after the key was added: %s", a)
	}
	id := fullID.FindString(a)
	if id == "" {
		t.Fatalf("the answer names no event: %q", a)
	}
	h.in.Close()
	<-h.done

	out := run(keyPass, "log", dir, "--json")
	found := false
	for _, line := range strings.Split(out, "\n") {
		var row map[string]any
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &row) == nil && row["id"] == id {
			found = row["address"] == "notes/after-key"
		}
	}
	if !found {
		t.Errorf("the key added while the surface was open does not read what the surface recorded afterwards:\n%s", out)
	}
}
