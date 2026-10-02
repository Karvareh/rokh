package shell

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
)

// systemReaderIn is the system reader of a keyed ledger, opened from the
// owner's keyring generation, where its private half is sealed to the owner.
func systemReaderIn(t *testing.T, dir string) key.Reader {
	t.Helper()
	owner := ownerReaderOf(t, dir, testPass)
	led, err := replay(openV1(t, dir, testPass))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range led.Order() {
		e, _ := led.Get(id)
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || e.Event.Verb != event.VerbKeyring || !k.IsOwner() || len(k.System) == 0 {
			continue
		}
		priv, err := key.OpenFrom(*owner, k.System, key.InfoSystem)
		if err != nil {
			t.Fatal(err)
		}
		sys, err := key.ReaderFrom(priv)
		if err != nil {
			t.Fatal(err)
		}
		return sys
	}
	t.Fatal("the ledger holds no system reader")
	return key.Reader{}
}

// copyFolder copies a carrier folder file by file, as a person copies it: the
// copy is the same vessel, with the same salt and cost.
func copyFolder(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Step 2 (contract 4.8 R2; a ruling of the design): the
// sentence surface's reunion installs the cells of live keys on both sides,
// as the command line's reconcile does. The berth is a copy of the ledger's
// vessel. Apart, a key is added on each side, and the berth takes the reader
// back. Bringing the returned ledger installs on each side the cell of the
// key the other side added, and says so: each of those keys opens the other
// side with its own passphrase, and the key added on the berth reads its view
// in the ledger. The reader, taken back on the berth, opens nothing in the
// ledger after the reunion. Bringing it back again installs nothing and
// commits nothing.
func TestTheReunionInstallsTheCellsOfLiveKeysOnBothSides(t *testing.T) {
	kv := makeKeyedVault(t, keyedSpecs)
	owner := ownerReaderOf(t, kv.dir, testPass)
	sys := systemReaderIn(t, kv.dir)
	berthDir := filepath.Join(kv.mount, "seat-a")
	copyFolder(t, kv.dir, berthDir)
	decl, err := json.Marshal(seatDecl{Anchor: kv.genesis.ID.String(), Kind: "berth",
		Ruling: "planted by the test, standing in for the owner's ruling"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(berthDir, seatDeclName), decl, 0o600); err != nil {
		t.Fatal(err)
	}
	adds := map[string]frame.ID{}
	for n, id := range kv.adds {
		adds[n] = id
	}
	berth := &keyedVault{fixture: kv.fixture, dir: berthDir, root: kv.root, adds: adds}
	kv.addKey(t, *owner, sys, keySpec{name: "beta", pass: "shell beta pass", reads: []string{"journal"}})
	berth.addKey(t, *owner, sys, keySpec{name: "alpha", pass: "shell alpha pass", reads: []string{"journal"}})
	berth.revoke(t, "reader")
	// A key reads what is sealed to it, what is written in its reads after its
	// add (K4): one line on each side, after the adds.
	bc := openV1(t, berthDir, testPass)
	head, _, err := bc.Ref(defaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	anchor := bc.Anchor()
	onBerth, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{head},
		Address: "journal/berth", Verb: "note", Payload: []byte("written on the berth")}, kv.root)
	if err != nil {
		t.Fatal(err)
	}
	recordV1(t, bc, berthDir, onBerth, defaultBranch)
	if _, _, err := (&session{}).openCarrier(kv.dir, "shell alpha pass"); err == nil {
		t.Fatal("alpha opens the ledger before the reunion")
	}

	s := kv.session(testPass)
	var notes bytes.Buffer
	s.notes = &notes
	run(t, s, "write at journal/home: written at home")
	run(t, s, "write")
	run(t, s, "bring the returned ledger")
	if !strings.Contains(notes.String(), "(this ledger installed 1 key cell)") || !strings.Contains(notes.String(), "(the berth installed 1 key cell)") {
		t.Fatalf("the reunion does not say it installed a cell on each side:\n%s", notes.String())
	}
	if err := s.closeAll(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ dir, pass string }{{kv.dir, "shell alpha pass"}, {berthDir, "shell beta pass"}} {
		if _, sec, err := (&session{}).openCarrier(c.dir, c.pass); err != nil || sec.Key == ([32]byte{}) {
			t.Fatalf("%q does not open %s with its own cell after the reunion: %v", c.pass, filepath.Base(c.dir), err)
		}
	}
	a := kv.session("shell alpha pass")
	if got := run(t, a, "read journal"); !strings.Contains(got, "written on the berth") || strings.Contains(got, "written at home") {
		t.Fatalf("the key added on the berth does not read its own view in the ledger:\n%s", got)
	}
	if err := a.closeAll(); err != nil {
		t.Fatal(err)
	}
	b, _, err := (&session{}).openCarrier(berthDir, "shell beta pass")
	if err != nil {
		t.Fatal(err)
	}
	bl, err := replay(b)
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]string{}
	for _, id := range bl.Order() {
		if e, _ := bl.Get(id); !e.HeadOnly && strings.HasPrefix(e.Event.Address, "journal/") {
			read[e.Event.Address] = string(e.Event.Payload)
		}
	}
	if read["journal/home"] != "written at home" || read["journal/berth"] != "" {
		t.Fatalf("the key added in the ledger does not read its own view on the berth: %v", read)
	}
	// Its cell may stay, and its session is then refused (key.ErrKeyNotLive),
	// or the cell of a live key may have taken its place.
	for _, dir := range []string{kv.dir, berthDir} {
		if _, _, err := (&session{}).openCarrier(dir, "shell reader pass"); err == nil {
			t.Fatalf("the reader, taken back on the berth, opens %s after the reunion", filepath.Base(dir))
		}
	}
	if err := attempt(t, kv.session("shell reader pass"), "open the ledger home"); err == nil {
		t.Fatal("the reader, taken back on the berth, opens the ledger through the sentences after the reunion")
	}

	gl, gb := kv.generation(t), openV1(t, berthDir, testPass).Vessel().Info().Generation
	again := kv.session(testPass)
	notes.Reset()
	again.notes = &notes
	run(t, again, "open the ledger home")
	run(t, again, "bring the returned ledger")
	if strings.Contains(notes.String(), "installed") {
		t.Fatalf("bringing it back again installed a cell:\n%s", notes.String())
	}
	if err := again.closeAll(); err != nil {
		t.Fatal(err)
	}
	if kv.generation(t) != gl || openV1(t, berthDir, testPass).Vessel().Info().Generation != gb {
		t.Fatal("bringing it back again committed")
	}
}
