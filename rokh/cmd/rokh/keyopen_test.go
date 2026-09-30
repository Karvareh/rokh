package main

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/turn"
	"rokh/vessel"
)

// R6 step 4: a key opens the carrier with its own passphrase, through the
// commands' own opener, and holds its own view: a reader sees journal and not
// private, a key with no reads sees no body, a writer's scoped write (sealed
// by its own session, its branch pointer under the vessel's pointer key) is
// read by the owner, and a revoked key opens nothing. It never signs as root.
func TestAKeyOpensItsOwnViewWithItsOwnPassphrase(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	if _, _, err := bondRun(t, cmdInit, dir, "--message", "genesis"); err != nil {
		t.Fatal(err)
	}
	passes := map[string]string{"reader": "synthetic reader pass", "none": "synthetic none pass", "writer": "synthetic writer pass"}
	flags := map[string][]string{
		"reader": {"--reads", "journal"},
		"none":   {},
		"writer": {"--reads", "journal", "--write", "--scope", "journal"},
	}
	for _, name := range []string{"reader", "none", "writer"} {
		file := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(file, []byte(passes[name]), 0o600); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"add", dir, "--name", name, "--key-passphrase-file", file}, flags[name]...)
		if _, errOut, err := bondRun(t, cmdKey, args...); err != nil {
			t.Fatalf("key add %s: %v %s", name, err, errOut)
		}
	}
	// Written after the keys exist: an event's readers are judged at its
	// parents (E4), so what came before a key is not in its view.
	for addr, msg := range map[string]string{"journal/today": "in the view", "private/diary": "outside the view"} {
		if _, errOut, err := bondRun(t, cmdWrite, dir, "--address", addr, "--message", msg); err != nil {
			t.Fatalf("owner write %s: %v %s", addr, err, errOut)
		}
	}
	shows := func(pass string) (whole map[string]string) {
		t.Helper()
		c := openForTest(t, dir, pass)
		l := ledgerOf(t, c)
		whole = map[string]string{}
		for _, id := range l.Order() {
			if e, _ := l.Get(id); !e.HeadOnly && !e.System {
				whole[e.Event.Address] = string(e.Event.Payload)
			}
		}
		return whole
	}

	// A reader: journal, not private.
	r := shows(passes["reader"])
	if r["journal/today"] != "in the view" {
		t.Fatalf("the reader does not read journal: %v", r)
	}
	if _, seen := r["private/diary"]; seen {
		t.Fatalf("the reader read private: %v", r)
	}
	// The same through a command run with the reader's own passphrase.
	t.Setenv("ROKH_PASSPHRASE", passes["reader"])
	out, errOut, err := bondRun(t, cmdView, dir)
	if err != nil || !strings.Contains(out, "journal") || strings.Contains(out, "private") {
		t.Fatalf("rokh view with the reader's passphrase: %v\n%s%s", err, out, errOut)
	}
	// With the reader's passphrase the command line signs nothing as root.
	if _, _, err := bondRun(t, cmdWrite, dir, "--address", "journal/forged", "--message", "as root"); err == nil {
		t.Fatal("a write with a key's passphrase was signed as the root")
	}
	t.Setenv("ROKH_PASSPHRASE", bondPass)

	// A key that reads nothing sees no body.
	if n := shows(passes["none"]); len(n) != 0 {
		t.Fatalf("a key that reads nothing read: %v", n)
	}

	// A writer writes in its scope through its own session; the owner reads it.
	c, _, _, err := openCarrier(dir, passes["writer"], false)
	if err != nil {
		t.Fatal(err)
	}
	info := c.Vessel().Info()
	sec, _, err := key.Try(passes["writer"], c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		t.Fatal(err)
	}
	signer := ed25519.NewKeyFromSeed(sec.SignerSeed[:])
	l := ledgerOf(t, c)
	var grant frame.ID
	for _, id := range l.ActiveGrants() {
		e, _ := l.Get(id)
		if g, err := event.DecodeGrant(e.Event.Payload); err == nil && string(g.Subject) == string(signer.Public().(ed25519.PublicKey)) {
			grant = id
		}
	}
	if grant.IsZero() {
		t.Fatal("the writer's grant is not in its view")
	}
	heads, err := allHeads(c)
	if err != nil {
		t.Fatal(err)
	}
	anchor := c.Anchor()
	w, err := event.SignFresh(event.Event{Carrier: &anchor, Authority: &grant, Parents: heads,
		Address: "journal/by-writer", Verb: "note", Payload: []byte("written by the writer key")}, signer)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := turn.Acquire(dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := c.Begin(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Event(w.ID, w.Head, w.Body, w.Event.Address); err != nil {
		t.Fatal(err)
	}
	if err := rec.SetRef("main", w.ID); err != nil {
		t.Fatal(err)
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		t.Fatalf("the writer's commit: %s %v", out, err)
	}
	lock.Release()
	if o := shows(bondPass); o["journal/by-writer"] != "written by the writer key" {
		t.Fatalf("the owner does not read the writer's scoped write: %v", o)
	}
	if r := shows(passes["reader"]); r["journal/by-writer"] != "written by the writer key" {
		t.Fatalf("the reader does not read the writer's write in journal: %v", r)
	}

	// A revoked key opens nothing.
	if _, errOut, err := bondRun(t, cmdKey, "revoke", dir, "--key", "reader"); err != nil {
		t.Fatalf("revoke: %v %s", err, errOut)
	}
	if _, _, _, err := openCarrier(dir, passes["reader"], false); err == nil {
		t.Fatal("a revoked key opened the carrier")
	}
}
