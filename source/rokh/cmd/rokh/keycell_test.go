package main

import (
	"os"
	"path/filepath"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/vessel"
)

// W2-004 R1: rokh key add put the key's cell after the vessel's 32, where it
// was dropped, and still said the key was added. The cell now goes where
// nobody's cell is, so each key's own passphrase opens the vessel afterwards,
// and the owner's still does, as the owner.
func TestAKeysCellIsInstalledWhereNobodysCellIs(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	if _, _, err := bondRun(t, cmdInit, dir, "--message", "genesis"); err != nil {
		t.Fatal(err)
	}
	passes := map[string]string{"reader-one": "synthetic key passphrase one", "reader-two": "synthetic key passphrase two"}
	for name, pass := range passes {
		file := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(file, []byte(pass), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, errOut, err := bondRun(t, cmdKey, "add", dir, "--name", name, "--reads", "journal", "--key-passphrase-file", file); err != nil {
			t.Fatalf("key add %s: %v %s", name, err, errOut)
		}
	}
	c := openForTest(t, dir, bondPass)
	info, slots := c.Vessel().Info(), c.Vessel().Slots()
	seen := map[[32]byte]bool{}
	for name, pass := range passes {
		sec, _, err := key.Try(pass, slots, info.Salt, info.Iter)
		if err != nil || sec.Key == ([32]byte{}) {
			t.Fatalf("%s's own passphrase does not open the vessel: %v", name, err)
		}
		seen[sec.Key] = true
	}
	if len(seen) != len(passes) {
		t.Fatalf("the keys opened %d distinct cells, not %d", len(seen), len(passes))
	}
	if sec, _, err := key.Try(bondPass, slots, info.Salt, info.Iter); err != nil || sec.Key != ([32]byte{}) {
		t.Fatalf("the owner's passphrase no longer opens the owner's cell: %v", err)
	}
}

// Bootstrap step 2: a key's add carries the system reader sealed to the key
// (0x000B), and the carrier holds an envelope of that add the key's own
// reader opens, so the key can read its generation and, through it, the
// system reader, the same one the owner's session seals system events to.
func TestAKeysAddGivesItTheSystemReaderAndAnEnvelopeOfItsOwn(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	if _, _, err := bondRun(t, cmdInit, dir, "--message", "genesis"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "kp")
	if err := os.WriteFile(file, []byte("synthetic reader passphrase"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, err := bondRun(t, cmdKey, "add", dir, "--name", "reader", "--reads", "journal", "--key-passphrase-file", file); err != nil {
		t.Fatalf("key add: %v %s", err, errOut)
	}
	c := openForTest(t, dir, bondPass)
	info := c.Vessel().Info()
	sec, _, err := key.Try("synthetic reader passphrase", c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, c)
	adds, _ := l.Keyring()
	var addID frame.ID
	var sysSeal []byte
	for _, id := range adds {
		e, _ := l.Get(id)
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && k.Key == sec.Key {
			addID, sysSeal = id, k.System
		}
	}
	if addID.IsZero() || len(sysSeal) == 0 {
		t.Fatalf("the key's add carries no system seal")
	}
	opened := 0
	c.Vessel().Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type == vessel.RecEvent && h.ID == addID {
			if env, err := c.Vessel().Body(ref); err == nil {
				if _, err := key.OpenReaders(key.TypeEvent, addID, nil, env, mine); err == nil {
					opened++
				}
			}
		}
		return nil
	})
	if opened == 0 {
		t.Fatal("no envelope of the key's own add opens to the key")
	}
	priv, err := key.OpenFrom(mine, sysSeal, key.InfoSystem)
	if err != nil {
		t.Fatalf("the key does not open its system seal: %v", err)
	}
	sys, _ := key.ReaderFrom(priv)
	sl, err := v1Sealer(c, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	if owner := sl.(*key.Session); string(owner.System) != string(sys.Public()) {
		t.Fatal("the key's system reader is not the one the owner seals system events to")
	}
}
