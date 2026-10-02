package main

import (
	"crypto/ed25519"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"rokh/booth"
	"rokh/frame"
	"rokh/ledger"
	"rokh/turn"
)

// Several continuous booths on one rokh, and a booth beside the command line,
// as real processes (T10, the owner's words of 2026-09-29): every write
// answered recorded is in the ledger read afresh, and the writes that lost the
// turn are answered so and recorded nothing.
//
//	— T10, T8.5, T4.4, contract C9

const fixturePass = "synthetic test passphrase"

// ownerRootOf is the owner's signing key, from the owner's cell, for a booth
// session bound as the owner.
func ownerRootOf(t *testing.T, f processFixture) ed25519.PrivateKey {
	t.Helper()
	s, err := openSession(f.dir, fixturePass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root := ownerRoot(s.sec)
	if root == nil {
		t.Fatal("the owner's cell holds no root")
	}
	return root
}

// freshLedger reads the ledger afresh from the folder, in this process: a new
// opening that shares nothing with the running doors.
func freshLedger(t *testing.T, f processFixture) *ledger.Ledger {
	t.Helper()
	s, err := openSession(f.dir, fixturePass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	return s.led
}

// runningDaemon starts `rokh daemon` with the owner's passphrase on a socket
// of its own, and stops it when the test ends.
func runningDaemon(t *testing.T, f processFixture) string {
	t.Helper()
	dir, err := shortTemp(t, "rkd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	d := exec.Command(f.bin, "daemon", f.dir, "--socket", sock, "--passphrase-file", f.passFile)
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Process.Signal(os.Interrupt); d.Wait() })
	dialWhenListening(t, sock).Close()
	return sock
}

// bindOwner opens a booth session on a running daemon and binds it as the
// owner. It never calls t.Fatal, so goroutines may use it.
func bindOwner(sock string, root ed25519.PrivateKey) (*booth.Client, net.Conn, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	c := booth.NewClient(conn)
	who, err := c.ProveKey([32]byte{}, root)
	if err != nil || who["owner"] != true {
		conn.Close()
		return nil, nil, fmt.Errorf("the owner did not bind: %v %v", who, err)
	}
	return c, conn, nil
}

func mustBindOwner(t *testing.T, sock string, root ed25519.PrivateKey) *booth.Client {
	t.Helper()
	c, conn, err := bindOwner(sock, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return c
}

// boothWrite asks a booth session to write and gives the answer and, when it
// recorded, the event's id.
func boothWrite(c *booth.Client, fields map[string]any) (map[string]any, string, error) {
	r, err := c.Call("write", fields)
	if err != nil {
		return nil, "", err
	}
	if r["ok"] == true && r["record"] == "recorded" {
		return r, fmt.Sprint(r["event"]), nil
	}
	return r, "", nil
}

// cliWrite is one `rokh write` process as the owner, with its typed answer.
func cliWrite(f processFixture, args ...string) (map[string]any, int, string) {
	out, code := f.run(append([]string{"write", f.dir}, append(args, "--json")...)...)
	return answerIn(out), code, out
}

// headsOf asks a booth session for the ledger's heads.
func headsOf(t *testing.T, c *booth.Client) []string {
	t.Helper()
	st, err := c.Call("status", nil)
	if err != nil || st["ok"] != true {
		t.Fatalf("status: %v %v", st, err)
	}
	var out []string
	list, _ := st["heads"].([]any)
	for _, h := range list {
		out = append(out, fmt.Sprint(h))
	}
	sort.Strings(out)
	return out
}

// onOneLine says whether every recorded event rests on the one recorded
// before it, in the order given: nothing any writer recorded was moved over.
func onOneLine(t *testing.T, l *ledger.Ledger, order []string) {
	t.Helper()
	for i := 1; i < len(order); i++ {
		id, err := frame.ParseID(order[i])
		if err != nil {
			t.Fatal(err)
		}
		e, ok := l.Get(id)
		if !ok {
			t.Fatalf("%s is not in the ledger read afresh", order[i])
		}
		if len(e.Event.Parents) != 1 || e.Event.Parents[0].String() != order[i-1] {
			t.Fatalf("recording %d rests on %v, not on the one recorded before it, %s", i, e.Event.Parents, order[i-1])
		}
	}
}

// everyRecordedIsThere checks the ledger read afresh: every id answered
// recorded is in it, and nothing but them and what init recorded (the
// genesis and the owner's keyring generation).
func everyRecordedIsThere(t *testing.T, f processFixture, recorded map[string]bool) {
	t.Helper()
	back := map[string]bool{}
	for _, r := range f.events(t) {
		back[fmt.Sprint(r["id"])] = true
	}
	for id := range recorded {
		if !back[id] {
			t.Errorf("answered recorded, not in the ledger read afresh: %s", id)
		}
	}
	if len(back) != len(recorded)+2 {
		t.Fatalf("the ledger read afresh holds %d events: %d recorded plus the two of init", len(back), len(recorded))
	}
	if out, code := f.run("verify", f.dir); code != 0 {
		t.Fatalf("verify: exit %d\n%s", code, out)
	}
}

// Two `rokh daemon` on one carrier, each its own process with its own opening
// of the folder. In turn: each writes on what the other recorded. At once:
// sessions on both write together, and every recording lands. At once on the
// same heads: exactly one of the two records, the other is answered that the
// heads moved and records nothing. Then both answer from the same ledger, the
// one the folder holds.
func TestTwoRunningDaemonsOnOneCarrierWriteInTurnAndAtOnce(t *testing.T) {
	// Its own folder, processes and sockets, and nothing of this package's
	// state: it runs beside the other tests of T10 that wait on real processes.
	t.Parallel()
	f := newProcessFixture(t)
	root := ownerRootOf(t, f)
	socks := []string{runningDaemon(t, f), runningDaemon(t, f)}
	clients := []*booth.Client{mustBindOwner(t, socks[0], root), mustBindOwner(t, socks[1], root)}
	recorded := map[string]bool{}

	// In turn.
	var order []string
	for i := 0; i < 5; i++ {
		for k, c := range clients {
			r, id, err := boothWrite(c, map[string]any{"address": fmt.Sprintf("home/daemon-%d", k),
				"message": fmt.Sprintf("daemon %d, in turn %d", k, i)})
			if err != nil || id == "" {
				t.Fatalf("daemon %d in turn %d: %v %v", k, i, r, err)
			}
			recorded[id] = true
			order = append(order, id)
		}
	}
	onOneLine(t, freshLedger(t, f), order)

	// At once: three sessions on each daemon, four writes each.
	var mu sync.Mutex
	var failures []string
	var wg sync.WaitGroup
	for k, sock := range socks {
		for j := 0; j < 3; j++ {
			wg.Add(1)
			go func(k, j int, sock string) {
				defer wg.Done()
				c, conn, err := bindOwner(sock, root)
				if err != nil {
					mu.Lock()
					failures = append(failures, err.Error())
					mu.Unlock()
					return
				}
				defer conn.Close()
				for i := 0; i < 4; i++ {
					r, id, err := boothWrite(c, map[string]any{"address": fmt.Sprintf("home/daemon-%d/session-%d", k, j),
						"message": fmt.Sprintf("daemon %d session %d, at once %d", k, j, i)})
					mu.Lock()
					if err != nil || id == "" {
						failures = append(failures, fmt.Sprintf("daemon %d session %d write %d: %v %v", k, j, i, r, err))
					} else {
						recorded[id] = true
					}
					mu.Unlock()
				}
			}(k, j, sock)
		}
	}
	wg.Wait()
	if len(failures) > 0 {
		t.Fatalf("writes at once not recorded:\n%s", strings.Join(failures, "\n"))
	}

	// At once, prepared against the same heads.
	for round := 0; round < 3; round++ {
		heads := headsOf(t, clients[0])
		answers := make([]map[string]any, 2)
		ids := make([]string, 2)
		var race sync.WaitGroup
		for k, c := range clients {
			race.Add(1)
			go func(k int, c *booth.Client) {
				defer race.Done()
				answers[k], ids[k], _ = boothWrite(c, map[string]any{"address": "home/race",
					"message": fmt.Sprintf("round %d, daemon %d", round, k), "expect_heads": heads})
			}(k, c)
		}
		race.Wait()
		wins := 0
		for k := range clients {
			a := answers[k]
			switch {
			case a != nil && a["record"] == "recorded":
				wins++
				recorded[ids[k]] = true
			case a != nil && a["record"] == "not-recorded" && a["code"] == "precondition_failed" && a["event"] == nil:
			default:
				t.Fatalf("round %d, daemon %d: %v", round, k, a)
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: %d daemons recorded on the same heads", round, wins)
		}
	}

	everyRecordedIsThere(t, f, recorded)
	want := freshLedger(t, f)
	for k, c := range clients {
		st, err := c.Call("status", nil)
		if err != nil || st["accepted"] != float64(len(want.Order())) {
			t.Fatalf("daemon %d answers %v accepted, the folder holds %d: %v", k, st["accepted"], len(want.Order()), err)
		}
		if h := headsOf(t, c); len(h) != 1 || h[0] != want.Heads()[0].String() {
			t.Fatalf("daemon %d answers the heads %v, the folder's are %v", k, h, want.Heads())
		}
	}
	t.Logf("%d writes answered recorded (%d in turn, 24 at once, 3 race winners); the ledger read afresh holds %d events",
		len(recorded), len(order), len(want.Order()))
}

// A running `rokh daemon` and `rokh write` from the command line, in turn:
// each writes on what the other recorded, and the daemon answers the next
// question from the ledger the folder holds. At once on the same heads:
// exactly one of the two records; the other is answered that the heads moved,
// and records nothing. A daemon write that cannot take the turn within its
// patience is answered so, and records nothing.
func TestARunningDaemonAndTheCommandLineWriteInTurn(t *testing.T) {
	// Its own folder, processes and sockets, and nothing of this package's
	// state: it runs beside the other tests of T10 that wait on real processes.
	t.Parallel()
	f := newProcessFixture(t)
	root := ownerRootOf(t, f)
	c := mustBindOwner(t, runningDaemon(t, f), root)
	recorded := map[string]bool{}

	var order []string
	for i := 0; i < 4; i++ {
		r, id, err := boothWrite(c, map[string]any{"address": "home/socket", "message": fmt.Sprintf("socket, in turn %d", i)})
		if err != nil || id == "" {
			t.Fatalf("the daemon in turn %d: %v %v", i, r, err)
		}
		recorded[id] = true
		order = append(order, id)
		a, code, out := cliWrite(f, "--address", "home/cli", "--message", fmt.Sprintf("cli, in turn %d", i))
		if code != 0 || a["record"] != "recorded" {
			t.Fatalf("the command line in turn %d: exit %d %s", i, code, out)
		}
		recorded[fmt.Sprint(a["id"])] = true
		order = append(order, fmt.Sprint(a["id"]))
	}
	onOneLine(t, freshLedger(t, f), order)
	if h := headsOf(t, c); len(h) != 1 || h[0] != order[len(order)-1] {
		t.Fatalf("the daemon's heads %v; the command line recorded %s last", h, order[len(order)-1])
	}

	for round := 0; round < 3; round++ {
		heads := headsOf(t, c)
		var socket map[string]any
		var socketID, out string
		var cli map[string]any
		var code int
		var race sync.WaitGroup
		race.Add(2)
		go func() {
			defer race.Done()
			socket, socketID, _ = boothWrite(c, map[string]any{"address": "home/race",
				"message": fmt.Sprintf("round %d, socket", round), "expect_heads": heads})
		}()
		go func() {
			defer race.Done()
			cli, code, out = cliWrite(f, "--address", "home/race", "--message", fmt.Sprintf("round %d, cli", round),
				"--expect-heads", strings.Join(heads, ","))
		}()
		race.Wait()
		wins := 0
		switch {
		case socket != nil && socket["record"] == "recorded":
			wins++
			recorded[socketID] = true
		case socket != nil && socket["record"] == "not-recorded" && socket["code"] == "precondition_failed":
		default:
			t.Fatalf("round %d, the daemon: %v", round, socket)
		}
		switch {
		case code == 0 && cli["record"] == "recorded":
			wins++
			recorded[fmt.Sprint(cli["id"])] = true
		case code == 1 && cli["record"] == "not-recorded" && cli["code"] == "precondition_failed":
		default:
			t.Fatalf("round %d, the command line: exit %d %s", round, code, out)
		}
		if wins != 1 {
			t.Fatalf("round %d: %d writers recorded on the same heads", round, wins)
		}
	}

	// A write the daemon cannot take the turn for within its patience, while
	// another writer holds it, is answered so and records nothing.
	before := len(f.events(t))
	lock, err := turn.Acquire(f.dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	r, id, err := boothWrite(c, map[string]any{"address": "home/busy", "message": "while another writer holds the turn"})
	lock.Release()
	if err != nil || id != "" || r["record"] != "not-recorded" || r["code"] != "turn_busy" {
		t.Fatalf("a daemon write that could not take the turn: %v %v", r, err)
	}
	if after := len(f.events(t)); after != before {
		t.Fatalf("a write that could not take the turn left %d events behind", after-before)
	}
	everyRecordedIsThere(t, f, recorded)
	t.Logf("%d writes answered recorded (%d in turn, 3 race winners); one daemon write answered %v", len(recorded), len(order), r["code"])
}
