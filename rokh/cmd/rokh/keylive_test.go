package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"rokh/booth"
	"rokh/carrier"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// A long-lived limited session (T10): `rokh daemon` opened with a key's own
// passphrase lives on while the owner records through the command line, in
// other processes. When it reads what they recorded it opens what its key may
// read and nothing else: the owners at a record's point are read from the
// ledger as it now is, not from what it was when the session opened.
//
//	— T10, contract 3.3, E3, E4

const liveKeyPass = "synthetic reader passphrase"

type liveKeyFixture struct {
	processFixture
	keyPassFile string
	kid         [32]byte
	reader      key.Reader
}

func newLiveKeyFixture(t *testing.T) liveKeyFixture {
	t.Helper()
	f := liveKeyFixture{processFixture: newProcessFixture(t), keyPassFile: filepath.Join(t.TempDir(), "reader-pass")}
	if err := os.WriteFile(f.keyPassFile, []byte(liveKeyPass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := f.run("key", "add", f.dir, "--name", "reader", "--reads", "journal", "--key-passphrase-file", f.keyPassFile); code != 0 {
		t.Fatalf("key add: exit %d\n%s", code, out)
	}
	s, err := openSession(f.dir, "synthetic test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range liveKeys(s) {
		if k.k.Name == "reader" {
			f.kid = k.k.Key
		}
	}
	s.Close()
	c := openForTest(t, f.dir, liveKeyPass)
	info := c.Vessel().Info()
	sec, _, err := key.Try(liveKeyPass, c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		t.Fatal(err)
	}
	if f.reader, err = key.ReaderFrom(sec.Reader[:]); err != nil {
		t.Fatal(err)
	}
	return f
}

// write records through the command line, in its own process, as the owner.
func (f liveKeyFixture) write(t *testing.T, address, message string) frame.ID {
	t.Helper()
	out, code := f.run("write", f.dir, "--address", address, "--message", message, "--json")
	a := answerIn(out)
	if code != 0 || a["record"] != "recorded" {
		t.Fatalf("write %s: exit %d %s", address, code, out)
	}
	id, err := frame.ParseID(fmt.Sprint(a["id"]))
	if err != nil {
		t.Fatalf("write %s: %v", address, err)
	}
	return id
}

// sneak records, as a writer holding the vessel's key and the root would, an
// event sealed to the reader key alone and not to the owner of its point: a
// record E3 refuses to every session, whoever it is sealed to.
func (f liveKeyFixture) sneak(t *testing.T, address, message string) frame.ID {
	t.Helper()
	s, err := openSession(f.dir, "synthetic test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	root := ownerRoot(s.sec)
	s.Close()
	lock, err := turn.Acquire(f.dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	c, _, err := carrier.Open(medium.Dir{Root: f.dir}, vessel.Unlock(key.Unlock("synthetic test passphrase")), rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	heads, err := allHeads(c)
	if err != nil {
		t.Fatal(err)
	}
	anchor := c.Anchor()
	e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: heads, Address: address, Verb: "note",
		Payload: []byte(message)}, root)
	if err != nil {
		t.Fatal(err)
	}
	env, err := key.SealReaders(key.TypeEvent, e.ID, nil, [][]byte{f.reader.Public()}, e.Body, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := c.Begin(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.EventEnvelope(e.ID, e.Head, env); err != nil {
		t.Fatal(err)
	}
	if err := rec.SetRef(defaultBranch, e.ID); err != nil {
		t.Fatal(err)
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		t.Fatalf("the sneaked record: %s %v", out, err)
	}
	return e.ID
}

// whole asks a door for one event and says whether its answer held the body:
// false for a head only.
func whole(t *testing.T, srv *daemon.Server, id frame.ID) bool {
	t.Helper()
	line, _ := json.Marshal(map[string]any{"op": "get", "id": id.String()})
	r := srv.Handle(line)
	if r["ok"] != true || r["state"] != "accepted" {
		t.Fatalf("get %s: %v", id.Short(), r)
	}
	raw, err := hex.DecodeString(fmt.Sprint(r["raw"]))
	if err != nil {
		t.Fatal(err)
	}
	e, err := event.Parse(raw)
	if err != nil || e.ID != id {
		t.Fatalf("get %s gave bytes that are not the event: %v", id.Short(), err)
	}
	return !e.HeadOnly
}

// The key's session, as `rokh daemon` opens it with the key's passphrase, in
// this process: what the owner records afterwards, in other processes, is
// read at the next question, and opened where the key reads and nowhere else.
func TestALongLivedKeySessionOpensWhatItMayAndNothingElseOnRefresh(t *testing.T) {
	// Its own folder, processes and sockets, and nothing of this package's
	// state: it runs beside the other tests of T10 that wait on real processes.
	t.Parallel()
	f := newLiveKeyFixture(t)
	before := f.write(t, "journal/before", "recorded before the session opened")
	s, err := openSession(f.dir, liveKeyPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.sec.Key != f.kid {
		t.Fatal("the reader's passphrase did not open the reader's own session")
	}
	srv := daemon.New(s.car, s.led, daemon.Options{Dir: f.dir, Release: Release, Door: doorName, Root: ownerRoot(s.sec)})
	if !whole(t, srv, before) {
		t.Fatal("the key does not read journal at opening")
	}

	inside := f.write(t, "journal/today", "inside the key's reads, recorded while the session lives")
	outside := f.write(t, "private/diary", "outside the key's reads, recorded while the session lives")
	sneaked := f.sneak(t, "journal/sneaked", "sealed to the key and not to the owner")
	later := f.write(t, "journal/later", "after the sneaked record")

	if !whole(t, srv, inside) {
		t.Fatal("the session refused a record its key reads, recorded after it opened")
	}
	if !whole(t, srv, later) {
		t.Fatal("the session refused a record its key reads, recorded after the sneaked one")
	}
	if whole(t, srv, outside) {
		t.Fatal("the session disclosed a record outside its key's reads")
	}
	if whole(t, srv, sneaked) {
		t.Fatal("the session opened a record that does not name the owner of its point (E3)")
	}
	// The owner's own reading, afresh, agrees about the sneaked record: it is
	// the owner's to judge, and it is a head only there too.
	o, err := openSession(f.dir, "synthetic test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if e, _ := o.led.Get(sneaked); !e.HeadOnly {
		t.Fatal("the owner's session opened the sneaked record")
	}
	if e, _ := o.led.Get(inside); e.HeadOnly {
		t.Fatal("the owner's session does not read journal/today")
	}
}

// The same through a real `rokh daemon` opened with the key's passphrase, and
// a session bound to that key on its booth: what the owner records through
// the command line while the daemon runs is shown whole inside the key's
// reads, and as its head only outside them.
func TestARunningKeyDaemonShowsWhatTheOwnerRecordsAfterItStarted(t *testing.T) {
	// Its own folder, processes and sockets, and nothing of this package's
	// state: it runs beside the other tests of T10 that wait on real processes.
	t.Parallel()
	f := newLiveKeyFixture(t)
	sockDir, err := shortTemp(t, "rkl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockDir)
	sock := filepath.Join(sockDir, "k.sock")
	d := exec.Command(f.bin, "daemon", f.dir, "--socket", sock, "--passphrase-file", f.keyPassFile)
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { d.Process.Signal(os.Interrupt); d.Wait() }()
	conn := dialWhenListening(t, sock)
	defer conn.Close()
	c := booth.NewClient(conn)
	if who, err := c.ProveReader(f.kid, f.reader); err != nil || who["ok"] != true || who["owner"] == true {
		t.Fatalf("the key did not bind on its own daemon: %v %v", who, err)
	}

	inside := f.write(t, "journal/today", "inside, recorded while the key's daemon runs")
	outside := f.write(t, "private/diary", "outside, recorded while the key's daemon runs")
	lg, err := c.Call("log", nil)
	if err != nil || lg["ok"] != true {
		t.Fatalf("log: %v %v", lg, err)
	}
	rows := map[string]map[string]any{}
	list, _ := lg["events"].([]any)
	for _, r := range list {
		m := r.(map[string]any)
		rows[fmt.Sprint(m["id"])] = m
	}
	if m := rows[inside.String()]; m == nil || m["address"] != "journal/today" || m["head_only"] == true {
		t.Fatalf("the key's daemon did not show whole what the key reads: %v", m)
	}
	if m := rows[outside.String()]; m == nil || m["address"] != nil || m["head_only"] != true {
		t.Fatalf("the key's daemon showed more than the head outside the key's reads: %v", m)
	}
}

// dialWhenListening connects to a daemon's socket once it listens.
func dialWhenListening(t *testing.T, sock string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if c, err := net.Dial("unix", sock); err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon never listened")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
