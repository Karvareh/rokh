package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/bond"
	"rokh/frame"
	"rokh/ledger"
)

// bondPass is the passphrase these tests use. It goes in through the same
// environment variable a person would use, so the commands are exercised on
// the path they actually run on rather than through a seam opened for tests.
const bondPass = "رمزِ آزمون"

func bondSetup(t *testing.T) {
	t.Helper()
	t.Setenv("ROKH_PASSPHRASE", bondPass)
}

// bondRun calls one command with its two streams caught, so a test can read
// exactly what a person would have seen.
func bondRun(t *testing.T, fn func([]string) error, args ...string) (string, string, error) {
	t.Helper()
	box := t.TempDir()
	outPath, errPath := filepath.Join(box, "out"), filepath.Join(box, "err")
	fo, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	fe, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	// A safety net only: a panic inside the command would otherwise leave the
	// rest of this test's output going into a temporary file nobody reads.
	defer func() { os.Stdout, os.Stderr = savedOut, savedErr }()
	os.Stdout, os.Stderr = fo, fe
	runErr := fn(args)
	os.Stdout, os.Stderr = savedOut, savedErr
	fo.Close()
	fe.Close()
	o, _ := os.ReadFile(outPath)
	e, _ := os.ReadFile(errPath)
	return string(o), string(e), runErr
}

// bondCarrier makes one person: their own carrier, their own key, their own
// anchor. Nothing is shared between two of them, which is the point.
//
//	— T11.8
func bondCarrier(t *testing.T, name string) (string, frame.ID) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if _, _, err := bondRun(t, cmdInit, dir, "--message", "آغازِ دفترِ "+name); err != nil {
		t.Fatalf("carrier %s was not made: %v", name, err)
	}
	c := openForTest(t, dir, bondPass)
	return dir, c.Anchor()
}

// bondLeafBytes is the leaf as it was filed, which is the form the other
// person receives.
func bondLeafBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimRight(b, "\r\n")
}

// A leaf's name is the hash of its own bytes, so two people arrive at the same
// name without asking each other and without either of them keeping the leaf.
//
// Each runs the command against their own carrier, naming the other. The
// founders are sorted inside the encoding, so the byte string does not depend
// on who wrote it down — which is the whole reason a name can be shared at all.
//
//	— T11.6, T3.2
func TestTwoPeopleReachOneNameForOneLeaf(t *testing.T) {
	bondSetup(t)
	dirA, anchorA := bondCarrier(t, "الف")
	dirB, anchorB := bondCarrier(t, "ب")
	doing := "کتابخانه را با هم نگه می‌داریم"
	fileA := filepath.Join(t.TempDir(), "برگه-الف.json")
	fileB := filepath.Join(t.TempDir(), "برگه-ب.json")

	outA, sayA, err := bondRun(t, cmdBond, "leaf", dirA,
		"--with", anchorB.String(), "--doing", doing, "--out", fileA)
	if err != nil {
		t.Fatalf("الف could not make the leaf: %v", err)
	}
	outB, _, err := bondRun(t, cmdBond, "leaf", dirB,
		"--with", anchorA.String(), "--doing", doing, "--out", fileB)
	if err != nil {
		t.Fatalf("ب could not make the leaf: %v", err)
	}

	rawA := strings.TrimRight(outA, "\n")
	rawB := strings.TrimRight(outB, "\n")
	if rawA != rawB {
		t.Fatalf("two people wrote the same bond down and got two byte strings:\n  %s\n  %s", rawA, rawB)
	}
	if got := string(bondLeafBytes(t, fileA)); got != rawA {
		t.Fatalf("the filed bytes are not the printed bytes:\n  %s\n  %s", got, rawA)
	}
	if !bytes.Equal(bondLeafBytes(t, fileA), bondLeafBytes(t, fileB)) {
		t.Fatal("the two files hold different bytes")
	}

	// And the name is the hash of exactly those bytes, not of anything else.
	name := frame.Hash([]byte(rawA))
	if name != bond.Name([]byte(rawA)) {
		t.Fatal("bond.Name is not the hash of the bytes")
	}
	if !strings.Contains(sayA, name.String()) {
		t.Fatalf("the printed name is not the hash of the printed bytes:\nwant %s\ngot\n%s", name, sayA)
	}

	// Nothing was recorded. A leaf is not an event.
	//   — T11.6
	s, err := openSession(dirA, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range s.led.Order() {
		e, _ := s.led.Get(id)
		if e.Event.Verb == bond.VerbAccept {
			t.Fatal("making a leaf put an event in the ledger")
		}
	}
}

// A knot needs more than one owner, so a leaf naming one founder is refused —
// and refused before anything is filed.
//
//	— T11.5, T11.8
func TestALeafNamingOneFounderIsRefused(t *testing.T) {
	bondSetup(t)
	dir, _ := bondCarrier(t, "تنها")
	file := filepath.Join(t.TempDir(), "برگه.json")

	_, _, err := bondRun(t, cmdBond, "leaf", dir, "--doing", "کار را خودم می‌کنم", "--out", file)
	if !errors.Is(err, bond.ErrAlone) {
		t.Fatalf("a bond with one owner was not refused: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("a refused leaf left a file behind")
	}

	// And the same anchor offered twice is a shared root key, not a bond.
	//   — T11.8
	mine := openForTest(t, dir, bondPass).Anchor()
	_, _, err = bondRun(t, cmdBond, "leaf", dir,
		"--with", mine.String(), "--doing", "خودم با خودم", "--out", file)
	if !errors.Is(err, bond.ErrSharedRoot) {
		t.Fatalf("one anchor offered twice was not refused: %v", err)
	}
}

// Accepting is the part that is an event. It goes into the accepter's own
// ledger, it names the leaf and the place, and it is still there when the
// carrier is opened again from disk and every event re-read.
//
//	— T11.6, T8.5
func TestAcceptingRecordsAnEventThatSurvivesAReopen(t *testing.T) {
	bondSetup(t)
	dirA, _ := bondCarrier(t, "الف")
	_, anchorB := bondCarrier(t, "ب")
	file := filepath.Join(t.TempDir(), "برگه.json")
	if _, _, err := bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم", "--out", file); err != nil {
		t.Fatal(err)
	}
	name := bond.Name(bondLeafBytes(t, file))
	place := "قفسه‌ها را من می‌چینم"

	if _, _, err := bondRun(t, cmdBond, "accept", dirA,
		"--leaf", file, "--place", place); err != nil {
		t.Fatalf("الف could not accept: %v", err)
	}

	// Reopen: carrier from disk, every event replayed and judged again.
	s, err := openSession(dirA, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	found := 0
	for _, id := range s.led.Order() {
		e, _ := s.led.Get(id)
		if e.Event.Verb != bond.VerbAccept {
			continue
		}
		if s.led.State(id) != ledger.Accepted {
			t.Fatalf("the acceptance is %s, not accepted", s.led.State(id))
		}
		a, err := bond.ReadAcceptance(e.Event.Payload)
		if err != nil {
			t.Fatalf("the recorded payload is not an acceptance: %v", err)
		}
		if a.Leaf != name {
			t.Fatalf("the acceptance names %s, the leaf is %s", a.Leaf, name)
		}
		if a.Place != place {
			t.Fatalf("the place came back as %q", a.Place)
		}
		if e.Event.Address != bondAddress {
			t.Fatalf("the acceptance sits at %q", e.Event.Address)
		}
		found++
	}
	if found != 1 {
		t.Fatalf("found %d acceptances in the ledger, want 1", found)
	}

	// Accepting without saying which part is yours is agreement with no
	// undertaking behind it, and is refused.
	//   — T11.7
	_, _, err = bondRun(t, cmdBond, "accept", dirA, "--leaf", file, "--place", "   ")
	if !errors.Is(err, bond.ErrNoPlace) {
		t.Fatalf("an acceptance with no place was not refused: %v", err)
	}
}

// A leaf is accepted by the people it names, and by nobody else. Somebody who
// is not in it has nothing to accept, and their ledger is left untouched.
//
//	— T11.6
func TestAcceptingALeafYouAreNotNamedInIsRefused(t *testing.T) {
	bondSetup(t)
	dirA, _ := bondCarrier(t, "الف")
	_, anchorB := bondCarrier(t, "ب")
	dirC, _ := bondCarrier(t, "ج")
	file := filepath.Join(t.TempDir(), "برگه.json")
	if _, _, err := bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم", "--out", file); err != nil {
		t.Fatal(err)
	}

	_, _, err := bondRun(t, cmdBond, "accept", dirC, "--leaf", file, "--place", "هرچه شد")
	if !errors.Is(err, bond.ErrNotAFounder) {
		t.Fatalf("ج accepted a leaf that does not name them: %v", err)
	}

	s, err := openSession(dirC, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range s.led.Order() {
		e, _ := s.led.Get(id)
		if e.Event.Verb == bond.VerbAccept {
			t.Fatal("a refused acceptance still reached the ledger")
		}
	}
}

// One character's difference is a different leaf, because the name is the hash
// of the bytes and there is no near miss in a hash.
//
// This is what makes the name worth agreeing on: two people who think they
// undertook slightly different things end up with two names, and find out.
//
//	— T11.6, T3.2
func TestOneCharacterMakesADifferentLeaf(t *testing.T) {
	bondSetup(t)
	dirA, _ := bondCarrier(t, "الف")
	_, anchorB := bondCarrier(t, "ب")
	box := t.TempDir()
	first := filepath.Join(box, "یک.json")
	second := filepath.Join(box, "دو.json")

	out1, _, err := bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم", "--out", first)
	if err != nil {
		t.Fatal(err)
	}
	out2, _, err := bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم.", "--out", second)
	if err != nil {
		t.Fatal(err)
	}
	raw1 := strings.TrimRight(out1, "\n")
	raw2 := strings.TrimRight(out2, "\n")
	if raw1 == raw2 {
		t.Fatal("a full stop was swallowed: two undertakings, one byte string")
	}
	if bond.Name([]byte(raw1)) == bond.Name([]byte(raw2)) {
		t.Fatal("two different leaves share one name")
	}

	// And the second does not quietly overwrite the first. The file may be the
	// only copy of a name another ledger has already accepted.
	//   — T11.6
	_, _, err = bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم.", "--out", first)
	if err == nil {
		t.Fatal("a different leaf was written over an existing one")
	}
	if got := string(bondLeafBytes(t, first)); got != raw1 {
		t.Fatalf("the first leaf's file changed:\n  %s", got)
	}
	// Filing the same leaf again is the same bytes, so it is not a clash.
	if _, _, err := bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم", "--out", first); err != nil {
		t.Fatalf("refiling the same leaf was refused: %v", err)
	}
}

// The standing command reads one ledger, because one ledger is all it has. It
// must not report the silence of a ledger it cannot open as a refusal.
//
// So the other founder's line says the ledger is out of reach and says nothing
// about what is in it, while this ledger's own line says accepted or not — the
// one place where absence really is absence.
//
//	— T13.6, T11.7
func TestStandingDoesNotReportSilenceAsRefusal(t *testing.T) {
	bondSetup(t)
	dirA, anchorA := bondCarrier(t, "الف")
	_, anchorB := bondCarrier(t, "ب")
	file := filepath.Join(t.TempDir(), "برگه.json")
	if _, _, err := bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم", "--out", file); err != nil {
		t.Fatal(err)
	}

	before, _, err := bondRun(t, cmdBond, "standing", dirA, "--leaf", file)
	if err != nil {
		t.Fatal(err)
	}
	if got := bondLineFor(t, before, anchorA); !strings.Contains(got, "has not accepted yet") {
		t.Fatalf("this ledger's own line before accepting: %q", got)
	}

	place := "قفسه‌ها را من می‌چینم"
	if _, _, err := bondRun(t, cmdBond, "accept", dirA,
		"--leaf", file, "--place", place); err != nil {
		t.Fatal(err)
	}
	after, _, err := bondRun(t, cmdBond, "standing", dirA, "--leaf", file)
	if err != nil {
		t.Fatal(err)
	}
	mine := bondLineFor(t, after, anchorA)
	if !strings.Contains(mine, "accepted, with the place") || !strings.Contains(mine, place) {
		t.Fatalf("this ledger's own line after accepting: %q", mine)
	}

	// The other founder, in both readings: out of reach, and no claim about
	// what their ledger holds.
	for _, text := range []string{before, after} {
		theirs := bondLineFor(t, text, anchorB)
		if !strings.Contains(theirs, "not readable from here") {
			t.Fatalf("the other founder's line does not say it is out of reach: %q", theirs)
		}
		if strings.Contains(theirs, "has not accepted yet") || strings.Contains(theirs, "accepted, with the place") {
			t.Fatalf("the command testified about a ledger it never opened: %q", theirs)
		}
	}
	// And it never announces a verdict on the whole, in either direction.
	for _, text := range []string{before, after} {
		if strings.Contains(text, "bound.") {
			t.Fatalf("the command called the work closed from inside one ledger:\n%s", text)
		}
	}
}

// The view this command reads through answers for its own anchor and refuses
// to answer for any other, even when the acceptance it holds is of the very
// leaf being asked about.
//
// This is the guard the printed output does not exercise, because the printer
// decides what to say about another founder from the anchor alone and never
// from what the view returned. Without it bond.Settled would gather one
// person's acceptance under two founders and report the work closed — which is
// the one claim a single ledger has no standing to make.
//
//	— T13.6, T11.6, T11.7
func TestThisLedgerAnswersOnlyForItself(t *testing.T) {
	bondSetup(t)
	dirA, anchorA := bondCarrier(t, "الف")
	_, anchorB := bondCarrier(t, "ب")
	file := filepath.Join(t.TempDir(), "برگه.json")
	if _, _, err := bondRun(t, cmdBond, "leaf", dirA, "--with", anchorB.String(),
		"--doing", "کتابخانه را با هم نگه می‌داریم", "--out", file); err != nil {
		t.Fatal(err)
	}
	leaf, err := bondReadLeaf(file)
	if err != nil {
		t.Fatal(err)
	}
	name := bond.Name(bondLeafBytes(t, file))
	if _, _, err := bondRun(t, cmdBond, "accept", dirA,
		"--leaf", file, "--place", "قفسه‌ها را من می‌چینم"); err != nil {
		t.Fatal(err)
	}

	s, err := openSession(dirA, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	view := bondHereOnly{anchor: s.car.Anchor(), led: s.led}

	if _, ok := view.Accepted(anchorA, name); !ok {
		t.Fatal("this ledger cannot see its own acceptance")
	}
	if a, ok := view.Accepted(anchorB, name); ok {
		t.Fatalf("this ledger answered for another anchor: %+v", a)
	}

	missing, closed, err := bond.Settled(leaf, view)
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("one ledger called the work closed across ledgers it never opened")
	}
	if len(missing) != 1 || missing[0] != anchorB {
		t.Fatalf("unseen founders came back as %v, want just %s", missing, anchorB)
	}
	said, err := bond.Places(leaf, view)
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed := said[anchorB]; claimed {
		t.Fatalf("this ledger reported what another person said their place was: %q", said[anchorB])
	}
}

// bondLineFor is the output line that names one anchor.
func bondLineFor(t *testing.T, text string, id frame.ID) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, id.String()) {
			return line
		}
	}
	t.Fatalf("no line names %s in:\n%s", id, text)
	return ""
}
