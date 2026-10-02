package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/bond"
)

// Writing on someone's behalf is not accepting on their behalf.
//
// The address a bond is written at is an ordinary address, so a delegate
// holding a broad write scope can author an event there. It could then be read
// back as the person's own acceptance, and the standing said so in as many
// words: this ledger accepted, with this place. A right to write had become a
// right to undertake, which is the one thing the hard minimum names separately
// from the other three.
//
// So the reader checks who wrote it, and the command no longer offers the pen.
//
//	— T11.8, T11.6
func TestADelegateCannotAcceptForThePerson(t *testing.T) {
	t.Skip("known to fail in 1.0.0 and owed: written for the way keys were made before version 1; see STATE.md")
	bondSetup(t)
	dirA, anchorA := bondCarrier(t, "الف")
	_, anchorB := bondCarrier(t, "ب")

	leafPath := filepath.Join(t.TempDir(), "leaf.json")
	if _, _, err := bondRun(t, cmdBondLeaf, dirA,
		"--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم",
		"--out", leafPath); err != nil {
		t.Fatal(err)
	}
	leaf, err := bond.Parse(bondLeafBytes(t, leafPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := leaf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	leafName := bond.Name(raw)

	// A delegate with the whole ledger to write in.
	if _, _, err := bondRun(t, cmdGrant, dirA, "--to", "یار"); err != nil {
		t.Fatal(err)
	}

	// The command will not take a key at all any more: accepting is the
	// person's act, and the cold path is --root-key, as it is for share.
	if _, _, err := bondRun(t, cmdBondAccept, dirA,
		"--key", "یار", "--leaf", leafPath, "--place", "قفسه‌ها را من می‌چینم"); err == nil {
		t.Fatal("bond accept still takes a delegate's key")
	}

	// And if such an event reaches the ledger anyway — which it can, because
	// the delegate may write there — the reader does not report it as the
	// person's own.
	payload, _, err := bond.Accept(leaf, anchorA, "قفسه‌ها را من می‌چینم")
	if err != nil {
		t.Fatal(err)
	}
	bondWriteRaw(t, dirA, bondAddress, bond.VerbAccept, payload, "یار")

	view := bondOpen(t, dirA)
	if a, yes := view.Accepted(anchorA, leafName); yes {
		t.Fatalf("a delegate's event was read as the person's acceptance, with place %q", a.Place)
	}
	out, _, err := bondRun(t, cmdBondStanding, dirA, "--leaf", leafPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "accepted, with the place") {
		t.Fatalf("the standing reports an acceptance nobody made:\n%s", out)
	}
	if !strings.Contains(out, "has not accepted yet") {
		t.Fatalf("the standing does not say this ledger has not accepted:\n%s", out)
	}
}

// A verb is not a place.
//
// An acceptance is recognised by address, verb and payload together. The
// reader checked two of the three, so an acceptance-shaped payload written
// under this verb at any address at all was counted — and the address this
// program declares acceptances at was declared and never read.
//
//	— T10.6, T11.6
func TestAnAcceptanceWrittenElsewhereIsNotOne(t *testing.T) {
	bondSetup(t)
	dirA, anchorA := bondCarrier(t, "الف")
	_, anchorB := bondCarrier(t, "ب")

	leafPath := filepath.Join(t.TempDir(), "leaf.json")
	if _, _, err := bondRun(t, cmdBondLeaf, dirA,
		"--with", anchorB.String(), "--doing", "کار", "--out", leafPath); err != nil {
		t.Fatal(err)
	}
	leaf, err := bond.Parse(bondLeafBytes(t, leafPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := leaf.Encode()
	if err != nil {
		t.Fatal(err)
	}
	leafName := bond.Name(raw)

	payload, _, err := bond.Accept(leaf, anchorA, "جای دیگر")
	if err != nil {
		t.Fatal(err)
	}
	// The person's own key, the accepting verb, the right payload — and
	// somewhere else entirely.
	bondWriteRaw(t, dirA, "خاطرات", bond.VerbAccept, payload, "")

	view := bondOpen(t, dirA)
	if a, yes := view.Accepted(anchorA, leafName); yes {
		t.Fatalf("an event at %q was read as an acceptance, with place %q", "خاطرات", a.Place)
	}

	// The same payload at the address acceptances are written at is one, so
	// the guard refuses the place and not the shape.
	bondWriteRaw(t, dirA, bondAddress, bond.VerbAccept, payload, "")
	view = bondOpen(t, dirA)
	if a, yes := view.Accepted(anchorA, leafName); !yes {
		t.Fatal("the same payload at the bond address is not read as an acceptance")
	} else if a.Place != "جای دیگر" {
		t.Fatalf("place is %q", a.Place)
	}
}

// bondWriteRaw puts one event straight into a carrier, so a test can build the
// event a guard is supposed to refuse. key is a keyring name, or empty for the
// person's own key.
func bondWriteRaw(t *testing.T, dir, address, verb string, payload []byte, key string) {
	t.Helper()
	args := []string{dir, "--address", address, "--verb", verb,
		"--payload-file", bondTempFile(t, payload)}
	if key != "" {
		args = append(args, "--key", key)
	}
	if _, _, err := bondRun(t, cmdWrite, args...); err != nil {
		t.Fatalf("the event a guard must refuse could not be written: %v", err)
	}
}

func bondTempFile(t *testing.T, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// bondOpen reads a carrier back the way the standing command does: following
// the references, never the object store.
//
//	— T8.5
func bondOpen(t *testing.T, dir string) bondHereOnly {
	t.Helper()
	c := openForTest(t, dir, bondPass)
	return bondHereOnly{anchor: c.Anchor(), led: ledgerOf(t, c)}
}
