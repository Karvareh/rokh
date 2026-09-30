package gate

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rokh-home/home"
	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// Step 5, through real processes: the home's gate holds each program to
// its own key's view, opened with the program's own reader (contract B4,
// 3.3). `rokh-home serve` holds a synthetic home open; the owner gives a
// program a namespace of the ledger through `rokh-home owner`, which makes
// the program a key of its own in the home's keyring; the program speaks
// rokh.booth/1 on the gate's socket. What is recorded in its namespace is
// sealed to its key, and it is served what its own reader opens there: a
// record in its namespace sealed to the owner alone stays a head, though the
// owner reads it. Before T2 the program was served the owner's view cut to
// its namespace, and read that record whole.

// op is one `rokh-home owner` request on the owner's channel.
func (h *gateHome) op(op string, fields map[string]any) map[string]any {
	h.t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		h.t.Fatal(err)
	}
	out, err := exec.Command(h.homeBin, "owner", h.root, "--run", h.run, "--passphrase-file", h.passFP, op, string(b)).CombinedOutput()
	r := answerOf(out)
	if err != nil || r["ok"] != true {
		h.t.Fatalf("owner %s: %v\n%s", op, err, out)
	}
	return r
}

// sneak records, as a writer holding the home's vessel and root would, a
// note sealed to the owner's reader alone.
func (h *gateHome) sneak(address, message string) frame.ID {
	h.t.Helper()
	dir := h.ledgerDir()
	c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock(twoWritersPass)), rand.Reader, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	info := c.Vessel().Info()
	sec, _, err := key.Try(twoWritersPass, c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		h.t.Fatal(err)
	}
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		h.t.Fatal(err)
	}
	lock, err := turn.Acquire(dir, 15*time.Second)
	if err != nil {
		h.t.Fatal(err)
	}
	defer lock.Release()
	head, found, err := c.Ref("main")
	if err != nil || !found {
		h.t.Fatalf("main: %v %v", found, err)
	}
	anchor := c.Anchor()
	e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{head}, Address: address, Verb: "note",
		Payload: []byte(message)}, sec.Signer())
	if err != nil {
		h.t.Fatal(err)
	}
	env, err := key.SealReaders(key.TypeEvent, e.ID, nil, [][]byte{owner.Public()}, e.Body, rand.Reader)
	if err != nil {
		h.t.Fatal(err)
	}
	rec, err := c.Begin(lock)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := rec.EventEnvelope(e.ID, e.Head, env); err != nil {
		h.t.Fatal(err)
	}
	if err := rec.SetRef("main", e.ID); err != nil {
		h.t.Fatal(err)
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		h.t.Fatalf("sneak: %s %v", out, err)
	}
	return e.ID
}

func TestAProgramIsHeldToItsOwnKeysViewOpenedWithItsOwnReader(t *testing.T) {
	h := startGateHome(t)
	added := h.op("consumer.add", map[string]any{"name": "a reading program"})
	cons, _ := added["consumer"].(map[string]any)
	id := fmt.Sprint(cons["id"])
	cred := fmt.Sprint(added["credential"])
	h.op("grant", map[string]any{"consumer": id, "action": "ledger", "scope": "apps/reader"})

	write := func(address, message string) frame.ID {
		t.Helper()
		r, code, out := h.owner(map[string]any{"op": "write", "address": address, "verb": "note", "message": message})
		if code != 0 || r["record"] != "recorded" {
			t.Fatalf("the owner's write at %s: exit %d %s", address, code, out)
		}
		e, err := frame.ParseID(fmt.Sprint(r["event"]))
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	inside := write("apps/reader/one", "a line in the program's namespace")
	outside := write("apps/other/one", "a line outside the program's namespace")
	sneaked := h.sneak("apps/reader/sneaked", "in the program's namespace, sealed to the owner alone")

	// What is recorded in the program's namespace is sealed to its own key.
	l := h.fresh()
	var reader []byte
	adds, _ := l.Keyring()
	for _, a := range adds {
		e, _ := l.Get(a)
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && k.Key == home.ProgramKey(id) {
			reader = k.Reader
		}
	}
	if reader == nil {
		t.Fatal("the program holds no key in the home's keyring")
	}
	c, _, err := carrier.Open(medium.Dir{Root: h.ledgerDir()}, vessel.Unlock(key.Unlock(twoWritersPass)), rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := func(id frame.ID) bool {
		envs, err := c.Envelopes(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, env := range envs {
			kids, _ := key.Kids(env)
			for _, k := range kids {
				if k == key.Kid(reader) {
					return true
				}
			}
		}
		return false
	}
	if !names(inside) {
		t.Error("a record in the program's namespace is not sealed to the program's key")
	}
	if names(outside) {
		t.Error("a record outside the program's namespace is sealed to the program's key")
	}

	// The program, over rokh.booth/1 on the gate's socket: its own view.
	p, err := Dial(filepath.Join(h.run, "gate.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if r, err := p.Bind(cred); err != nil || r["ok"] != true || r["protocol"] != "rokh.booth/1" {
		t.Fatalf("the program did not bind in rokh.booth/1: %v %v", r, err)
	}
	lg, err := p.Call(map[string]any{"op": "ledger", "request": map[string]any{"op": "log", "address": "apps/reader"}})
	if err != nil || lg["ok"] != true {
		t.Fatalf("the program's log: %v %v", lg, err)
	}
	rows := map[string]map[string]any{}
	list, _ := lg["events"].([]any)
	for _, x := range list {
		m := x.(map[string]any)
		rows[fmt.Sprint(m["id"])] = m
	}
	if m := rows[inside.String()]; m == nil || m["address"] != "apps/reader/one" {
		t.Errorf("the program is not served what its own reader opens in its namespace: %v", m)
	}
	if m := rows[sneaked.String()]; m != nil && m["address"] != nil {
		t.Errorf("the program was served a record its own reader does not open, as the owner opened it: %v", m)
	}
	b, _ := json.Marshal(lg)
	if strings.Contains(string(b), "sealed to the owner alone") || strings.Contains(string(b), "outside the program's namespace") {
		t.Error("a body the program's reader does not open reached it")
	}
	// Outside its namespace the gate refuses it, as before.
	if r, _ := p.Call(map[string]any{"op": "ledger", "request": map[string]any{"op": "log", "address": "apps/other"}}); r["ok"] == true {
		t.Errorf("the program read outside its namespace: %v", r)
	}
	// The owner reads the sneaked record whole: it is the program's reader,
	// not the record, that keeps it from the program.
	if e, ok := l.Get(sneaked); !ok || e.HeadOnly {
		t.Error("the owner does not read the record sealed to the owner")
	}
}
