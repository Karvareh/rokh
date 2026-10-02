package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rokh/frame"
	"rokh/key"
)

// Step 7, the live fold, through real processes: a session that stays
// open seals and judges by the keyring as it is now, not as it was when it
// opened (contract E4, K4, 4.4). Before T2 a running booth sealed every record
// by the keyring its opening had read: a key added while it ran was not
// sealed to, and a key revoked while it ran still was.

// readersNamed reads which readers the envelopes of an event name, opening
// nothing: the carrier as the owner's passphrase opens it.
func readersNamed(t *testing.T, dir string, id frame.ID) map[[key.KidSize]byte]bool {
	t.Helper()
	c := openForTest(t, dir, "synthetic test passphrase")
	envs, err := c.Envelopes(id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[[key.KidSize]byte]bool{}
	for _, env := range envs {
		kids, err := key.Kids(env)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range kids {
			out[k] = true
		}
	}
	return out
}

func TestARunningBoothSealsByTheKeyringAsItIsNow(t *testing.T) {
	t.Parallel()
	f := newBoothsFixture(t)
	sockDir, err := shortTemp(t, "rkf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	b := startBooth(t, f.bin, f.dir, f.passFile, "unix:"+filepath.Join(sockDir, "o.sock"))
	o := b.dial(t)
	if who, err := o.ProveKey([32]byte{}, f.root); err != nil || who["owner"] != true {
		t.Fatalf("the owner did not bind: %v %v", who, err)
	}
	record := func(address, message string) frame.ID {
		t.Helper()
		w, err := o.Call("write", map[string]any{"address": address, "verb": "note", "message": message})
		if err != nil || w["record"] != "recorded" {
			t.Fatalf("the booth's write at %s: %v %v", address, w, err)
		}
		id, err := frame.ParseID(strings.TrimSpace(w["event"].(string)))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	before := record("journal/before", "recorded before the key existed")

	// A key added while the booth runs, by another process.
	k := f.addKey(t, "late-reader", "journal", "")
	added := record("journal/after-add", "sealed to the key added while the booth ran")
	if !readersNamed(t, f.dir, added)[k.reader.Kid()] {
		t.Error("a running booth did not seal to a key added after it opened")
	}
	if readersNamed(t, f.dir, before)[k.reader.Kid()] {
		t.Error("a record made before the key existed names its reader")
	}
	cmd := exec.Command(f.bin, "log", f.dir, "--json", "--passphrase-file", k.passFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the key's log: %v\n%s", err, out)
	}
	got := bodiesIn(string(out))
	if !strings.Contains(got[added.String()], "while the booth ran") {
		t.Errorf("the key does not read what the running booth recorded after it was added: %q", got[added.String()])
	}

	// The key revoked while the booth runs: from then on it is not sealed to.
	if out, code := f.run("key", "revoke", f.dir, "--key", "late-reader"); code != 0 {
		t.Fatalf("revoke: exit %d\n%s", code, out)
	}
	revoked := record("journal/after-revoke", "not sealed to the revoked key")
	if readersNamed(t, f.dir, revoked)[k.reader.Kid()] {
		t.Error("a running booth sealed to a key revoked after it opened (K4)")
	}
	owner := readersNamed(t, f.dir, revoked)
	if len(owner) == 0 {
		t.Fatal("the record after the revocation names no reader")
	}
}

// A key's own session on the owner's booth, which stays open while the owner
// records through the command line: what is recorded after it opened is
// shown as it is, whole where the key's own reader opens it and a head
// where it does not; it does not answer from the fold of the moment it
// opened.
func TestAKeysLongLivedSessionSeesLaterRecordsAsTheyAre(t *testing.T) {
	t.Parallel()
	f := newBoothsFixture(t)
	k := f.addKey(t, "journal-reader", "journal", "")
	sockDir, err := shortTemp(t, "rkg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	b := startBooth(t, f.bin, f.dir, f.passFile, "unix:"+filepath.Join(sockDir, "o.sock"))
	c := b.dial(t)
	if who, err := c.ProveReader(k.kid, k.reader); err != nil || who["reader_held"] != true {
		t.Fatalf("the key did not bind with its reader: %v %v", who, err)
	}
	first := rowsOf(t, c, nil) // the session's view is read now
	inside := f.write(t, "journal/later", "recorded after the session opened, inside its reads")
	outside := f.write(t, "private/later", "recorded after the session opened, outside its reads")
	rows := rowsOf(t, c, nil)
	if len(rows) <= len(first) {
		t.Fatalf("the session answered from the view of the moment it opened: %d rows, then %d", len(first), len(rows))
	}
	if !isWhole(rows[inside.String()], "journal/later") {
		t.Errorf("a later record the key may open is not shown whole: %v", rows[inside.String()])
	}
	if !isHead(rows[outside.String()]) || carries(rows, "outside its reads") {
		t.Errorf("a later record outside the key's reads is not a head: %v", rows[outside.String()])
	}
}
