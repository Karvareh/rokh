package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rokh/booth"
	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// Step 1 of T2 through real processes: opening judges the owner's
// generations at the record's own point, on the command line and in a booth
// (contract E3, E4). The owner's reader rotates; a key's own
// passphrase then reads what was sealed to it before the rotation, to the
// owner of that day, and what was sealed to it after, to the new owner, each
// at its own point. Before T2 a key's opening read the ledger with every
// owner generation ever named at once, so the owner's first add and every
// record of the first owner's days were heads, and the key's view stopped at
// the genesis.

// rotated is what the rotation left: the owner's second generation reads
// with o2, and the records the test made around it.
type rotated struct {
	o2      key.Reader
	before  frame.ID
	after   frame.ID
	private frame.ID
}

// forge records one event, signed by the root and sealed to exactly the
// readers given, under the writer's turn, and moves main: what a union or a
// second opening could bring. It is the owner's session at that point that
// would seal it so.
func forge(t *testing.T, dir string, e event.Signed, readers ...[]byte) {
	t.Helper()
	lock, err := turn.Acquire(dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock("synthetic test passphrase")), rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	env, err := key.SealReaders(key.TypeEvent, e.ID, nil, readers, e.Body, rand.Reader)
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
		t.Fatalf("forge: %s %v", out, err)
	}
}

// rotateOwner adds the owner's second generation where the first is live,
// then takes the first back, and records one line on each side of it.
func rotateOwner(t *testing.T, f liveKeyFixture) rotated {
	t.Helper()
	var r rotated
	r.before = f.write(t, "journal/before", "sealed to the owner of its day, before the rotation")
	r.private = f.write(t, "private/before", "outside the key's reads")
	s, err := openSession(f.dir, "synthetic test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	root := ownerRoot(s.sec)
	o1, err := key.ReaderFrom(s.sec.Reader[:])
	if err != nil {
		t.Fatal(err)
	}
	var gen1 frame.ID
	var sys key.Reader
	adds, _ := s.led.Keyring()
	for _, id := range adds {
		e, _ := s.led.Get(id)
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || !k.IsOwner() || k.Gen != 1 {
			continue
		}
		gen1 = id
		priv, err := key.OpenFrom(o1, k.System, key.InfoSystem)
		if err != nil {
			t.Fatal(err)
		}
		if sys, err = key.ReaderFrom(priv); err != nil {
			t.Fatal(err)
		}
	}
	head, _, err := s.car.Ref(defaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	anchor := s.car.Anchor()
	s.Close()
	if gen1.IsZero() {
		t.Fatal("rokh init recorded no owner generation")
	}
	if r.o2, err = key.NewReader(rand.Reader); err != nil {
		t.Fatal(err)
	}
	heir, _ := key.SealTo(r.o2.Public(), o1.Bytes(), key.InfoHeir, rand.Reader)
	sysSeal, _ := key.SealTo(r.o2.Public(), sys.Bytes(), key.InfoSystem, rand.Reader)
	sign := func(parent frame.ID, address, verb string, payload []byte) event.Signed {
		t.Helper()
		e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{parent}, Address: address, Verb: verb, Payload: payload}, root)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	p, _ := event.Keyring{Op: event.KeyringAdd, Gen: 2, Name: "owner", Reader: r.o2.Public(),
		Signer: root.Public().(ed25519.PublicKey), Reads: []string{""}, Heir: heir, System: sysSeal}.Encode()
	add := sign(head, event.AddressRoot, event.VerbKeyring, p)
	forge(t, f.dir, add, o1.Public(), sys.Public()) // at its point only the first is live
	p, _ = event.Keyring{Op: event.KeyringRevoke, Gen: 1, Target: gen1}.Encode()
	revoke := sign(add.ID, event.AddressRoot, event.VerbKeyring, p)
	forge(t, f.dir, revoke, o1.Public(), r.o2.Public(), sys.Public()) // both live at its point
	after := sign(revoke.ID, "journal/after", "note", []byte("sealed to the new owner, after the rotation"))
	forge(t, f.dir, after, r.o2.Public(), f.reader.Public()) // the owner of its point and the key that reads there
	r.after = after.ID
	return r
}

// bodiesIn reads `rokh log --json` output: event id to payload, for the rows
// that show a body.
func bodiesIn(out string) map[string]string {
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		var row map[string]any
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if p, ok := row["payload"].(string); ok {
			b, _ := base64.StdEncoding.DecodeString(p)
			got[fmt.Sprint(row["id"])] = string(b)
		}
	}
	return got
}

func TestAKeysPassphraseReadsAcrossTheOwnersRotation(t *testing.T) {
	t.Parallel()
	f := newLiveKeyFixture(t)
	r := rotateOwner(t, f)

	// The command line, with the key's own passphrase, in its own process.
	cmd := exec.Command(f.bin, "log", f.dir, "--json", "--passphrase-file", f.keyPassFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the key's log across the owner's rotation: %v\n%s", err, out)
	}
	got := bodiesIn(string(out))
	if !strings.Contains(got[r.before.String()], "before the rotation") {
		t.Errorf("the key does not read what was sealed to it before the owner rotated: %q", got[r.before.String()])
	}
	if !strings.Contains(got[r.after.String()], "after the rotation") {
		t.Errorf("the key does not read what was sealed to it after the owner rotated: %q", got[r.after.String()])
	}
	if _, shown := got[r.private.String()]; shown {
		t.Error("the key read what lies outside its view")
	}

	// The sentence surface, with the key's own passphrase: the carrier is an
	// ordinary folder, moved into a vault's ledgers, and read there.
	vault := filepath.Join(filepath.Dir(f.dir), "vault")
	if err := os.MkdirAll(filepath.Join(vault, "ledgers"), 0o700); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(vault, "ledgers", "main")
	if err := os.Rename(f.dir, moved); err != nil {
		t.Fatal(err)
	}
	f.dir = moved
	sc := exec.Command(f.bin, vault, "-c", "read journal")
	sc.Env = append(os.Environ(), "ROKH_PASSPHRASE_FILE="+f.keyPassFile)
	var said, notes bytes.Buffer
	sc.Stdout, sc.Stderr = &said, &notes
	if err := sc.Run(); err != nil {
		t.Fatalf("the surface with the key's passphrase: %v\n%s%s", err, said.String(), notes.String())
	}
	if !strings.Contains(said.String(), "before the rotation") || !strings.Contains(said.String(), "after the rotation") {
		t.Errorf("the surface does not show the key what was sealed to it on both sides of the rotation:\n%s", said.String())
	}
	if strings.Contains(said.String(), "outside the key's reads") {
		t.Error("the surface showed the key what lies outside its view")
	}

	// A booth opened with the key's passphrase, and a session of that key.
	sockDir, err := shortTemp(t, "rkr")
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
	if who, err := c.ProveReader(f.kid, f.reader); err != nil || who["ok"] != true {
		t.Fatalf("the key did not bind on its booth: %v %v", who, err)
	}
	for name, id := range map[string]frame.ID{"before": r.before, "after": r.after} {
		g, err := c.Call("get", map[string]any{"event": id.String()})
		if err != nil || g["ok"] != true {
			t.Fatalf("get %s: %v %v", name, g, err)
		}
		raw, _ := hex.DecodeString(fmt.Sprint(g["raw"]))
		e, err := event.Parse(raw)
		if err != nil || e.HeadOnly || !bytes.Contains(e.Event.Payload, []byte(name+" the rotation")) {
			t.Errorf("the key's booth does not show whole the record sealed to it %s the owner rotated: %v", name, g)
		}
	}
}
