package shell

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/content"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
	"rokh/oracle"
	"rokh/vessel"
)

const (
	testPass = "shell test pass"
	testIter = 2000 // fixtures only; the shell's own init path uses the default
)

// fixture is a vault with one ledger, built through the same doors the shell
// uses but with a low KDF round count so tests stay fast.
type fixture struct {
	vault, mount, library string
	name                  string
	rootPriv              ed25519.PrivateKey
	genesis               event.Signed
}

func makeFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{
		vault:   filepath.Join(base, "vault"),
		mount:   filepath.Join(base, "mount"),
		library: filepath.Join(base, "library"),
		name:    "home",
	}
	for _, d := range []string{filepath.Join(f.vault, "ledgers"), f.mount, f.library} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.rootPriv = priv
	att, _ := oracle.Observe(oracle.Default())
	gen, err := event.Sign(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte("fixture genesis"), Attest: att,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	f.genesis = gen
	dir := filepath.Join(f.vault, "ledgers", f.name)
	makeV1(t, dir, testPass, gen, priv)
	return f
}

func (f *fixture) session() *session {
	return newSession(f.vault, f.mount, f.library, testPass)
}

func run(t *testing.T, s *session, sentence string) string {
	t.Helper()
	c, err := parse(sentence)
	if err != nil {
		t.Fatalf("parse %q: %v", sentence, err)
	}
	reply, err := s.execute(c)
	if err != nil {
		t.Fatalf("execute %q: %v", sentence, err)
	}
	return reply
}

func snapshot(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		out[p] = fi.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSnapshot(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ---------- gates ----------

// A Persian payload with ZWNJ round-trips byte for byte through the write
// sentence and comes back out of the read sentence.
func TestZWNJRoundTripsThroughWriteAndRead(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()

	text := "می‌خواهیم نیم‌فاصله‌ها دست‌نخورده بمانند"
	// Writing is two acts now: the sentence, then its closing. The form is
	// shown in between and carries no name — the name does not exist yet.
	//   — T9.2, T4.4
	form := run(t, s, "write at کارها: "+text)
	// Recognised by the written template's own opening, not by a loose word:
	// the draft reply is allowed to talk about writing, and once did.
	if strings.HasPrefix(form, "written.") {
		t.Fatalf("the sentence recorded itself before it was closed: %q", form)
	}
	reply := run(t, s, "write")
	m := regexp.MustCompile(`[0-9a-f]{64}`).FindString(reply)
	if m == "" {
		t.Fatalf("write reply carries no full id: %q", reply)
	}
	id, err := frame.ParseID(m)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := s.current.led.Get(id)
	if !ok {
		t.Fatal("written event not in the ledger")
	}
	if !bytes.Equal(e.Event.Payload, []byte(text)) {
		t.Fatal("payload is not byte-identical; ZWNJ was not preserved")
	}
	if got := run(t, s, "read کارها"); !strings.Contains(got, text) {
		t.Fatalf("read did not show the exact text back:\n%s", got)
	}
}

// Frame folding applies to keywords only: a frame typed with Arabic yeh still
// parses, and a payload holding a decomposed combining mark survives.
func TestFrameFoldsButPayloadDoesNot(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()

	decomposed := "آب" // alef + combining madda + beh
	run(t, s, "WRITE AT يادها: "+decomposed)
	reply := run(t, s, "WRITE") // the closing act, typed with Arabic yeh too
	id, _ := frame.ParseID(regexp.MustCompile(`[0-9a-f]{64}`).FindString(reply))
	e, _ := s.current.led.Get(id)
	if !bytes.Equal(e.Event.Payload, []byte(decomposed)) {
		t.Fatal("the decomposed payload was normalized; it must survive untouched")
	}
	if e.Event.Address != "يادها" {
		t.Fatalf("the address slot was rewritten: %q", e.Event.Address)
	}
}

// An idle open session writes nothing - the same guarantee, the same style of
// test, as the daemon's.
// A session that is merely open is not a writer.
//
//	— T4, T4.1
func TestIdleSessionWritesNothing(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()

	run(t, s, "open the ledger home")
	before := snapshot(t, f.vault)
	beforeLen := s.current.led.Len()

	// Reading sentences are not events and sign nothing.
	run(t, s, "see the ledger")
	run(t, s, "read کارها")
	run(t, s, "see the ledgers")
	run(t, s, "see the grants")

	if !sameSnapshot(before, snapshot(t, f.vault)) {
		t.Fatal("a reading session changed the carrier")
	}
	if s.current.led.Len() != beforeLen {
		t.Fatal("a reading session signed an event nobody asked for")
	}
}

// In v1 no folder becomes a rokh except by init or seed: carrying the ledger
// is answered with the seed command, and nothing is written at the path.
func TestCarryingTheLedgerIsASeed(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()
	run(t, s, "write at کارها: پیش از رونوشت")
	dest := filepath.Join(t.TempDir(), "berth")
	c, err := parse("carry the ledger to " + dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.execute(c); err == nil || !strings.Contains(err.Error(), "rokh seed") {
		t.Fatalf("carrying the ledger was not answered with the seed: %v", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("something was written at the path")
	}
}

// makeBerth plants a divergent same-anchor berth in the mount folder. This is
// a test fixture built from core packages; the ruling that forbids a
// key-carrying copy *command* stands, and no command produced this.
func (f *fixture) makeBerth(t *testing.T, seatName string, diverge bool) string {
	t.Helper()
	src := openV1(t, filepath.Join(f.vault, "ledgers", f.name), testPass)
	dir := filepath.Join(f.mount, seatName)
	// A berth is a vessel of the same ledger: it keeps the ledger's owner
	// reader, as the key layer's owner check (E3) requires.
	dst := makeV1With(t, dir, testPass, f.genesis, f.rootPriv, ownerReaderOf(t, filepath.Join(f.vault, "ledgers", f.name), testPass))
	if err := src.Events(func(e carrier.EventRecord) error {
		if e.ID == f.genesis.ID {
			return nil
		}
		raw, err := src.Get(e.ID)
		if err != nil {
			return err
		}
		sg, err := event.Parse(raw)
		if err != nil {
			return err
		}
		recordV1(t, dst, dir, sg, defaultBranch)
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	led, err := replay(dst)
	if err != nil {
		t.Fatal(err)
	}
	head := led.Heads()[0]
	if diverge {
		anchor := f.genesis.ID
		e, err := event.Sign(event.Event{
			Carrier: &anchor, Parents: []frame.ID{head},
			Address: "travel", Verb: "note", Payload: []byte("written while apart"),
		}, f.rootPriv)
		if err != nil {
			t.Fatal(err)
		}
		if st, err := led.Add(e.Raw); err != nil || st != ledger.Accepted {
			t.Fatalf("berth divergence not accepted: %v %v", st, err)
		}
		recordV1(t, dst, dir, e, defaultBranch)
		head = e.ID
	}
	_ = head
	decl, _ := json.Marshal(seatDecl{Anchor: f.genesis.ID.String(), Kind: "berth",
		Ruling: "planted by the test, standing in for the owner's ruling"})
	if err := os.WriteFile(filepath.Join(dir, seatDeclName), decl, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Reunion: verdicts per side, heads close only on the exact reply, and
// nothing merges without it.
func TestReunionNeedsTheExactReply(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()

	// Our side diverges one way, the berth another.
	run(t, s, "write at کارها: نوشتهٔ خانه")
	run(t, s, "write")
	// The berth branched from genesis-only state? No: makeBerth copies the
	// carrier as it is NOW, then diverges on top. To force two heads, plant
	// the berth first, then write locally.
	s.closeAll()

	f2 := makeFixture(t)
	s2 := f2.session()
	defer s2.closeAll()
	f2.makeBerth(t, "seat-a", true)           // berth = genesis + travel note
	run(t, s2, "write at کارها: نوشتهٔ خانه") // ours = genesis + home note
	run(t, s2, "write")

	reply := run(t, s2, "bring the returned ledger")
	if !strings.Contains(reply, "reconcile") {
		t.Fatalf("two open heads did not ask the question: %q", reply)
	}
	if s2.pending == nil {
		t.Fatal("no pending merge was remembered")
	}
	if len(s2.current.led.Heads()) != 2 {
		t.Fatalf("expected two heads after union, got %d", len(s2.current.led.Heads()))
	}

	// A wrong reply merges nothing.
	if _, err := parse("unreconcile"); err == nil {
		t.Fatal("a near-miss reply parsed")
	}
	if len(s2.current.led.Heads()) != 2 || s2.pending == nil {
		t.Fatal("something merged without the exact reply")
	}

	// The exact reply closes the heads, on both sides.
	merged := run(t, s2, "reconcile")
	if !strings.Contains(merged, "they met") {
		t.Fatalf("merge reply wrong: %q", merged)
	}
	if len(s2.current.led.Heads()) != 1 {
		t.Fatal("heads did not close after the exact reply")
	}
	bc := openV1(t, filepath.Join(f2.mount, "seat-a"), testPass)
	bl, err := replay(bc)
	if err != nil {
		t.Fatal(err)
	}
	if len(bl.Heads()) != 1 || bl.Heads()[0] != s2.current.led.Heads()[0] {
		t.Fatal("the berth did not meet the same point")
	}
}

// A different anchor is refused with a clear sentence; union and merge are
// never offered. Mirrors and undeclared seats are never united.
//
// This is the half of the mirror ruling that Rokh owes: the seat declares
// itself, its anchor is read off the carrier's own public face rather than off
// its claim about itself, and nothing ever merges a foreign ledger into the
// host's. Running a mirror is a service somebody offers; making one possible,
// and keeping it read-only, is Rokh's part.
//
//	— T13.4, T8.4
func TestForeignAndUndeclaredSeatsAreRefused(t *testing.T) {
	f := makeFixture(t)

	// A foreign ledger in a declared "berth" seat: anchors differ.
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	og, err := event.Sign(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("someone else"),
	}, otherPriv)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.mount, "foreign")
	makeV1(t, dir, testPass, og, otherPriv)
	decl, _ := json.Marshal(seatDecl{Kind: "berth", Ruling: "wrongly declared"})
	os.WriteFile(filepath.Join(dir, seatDeclName), decl, 0o600)

	s := f.session()
	defer s.closeAll()
	run(t, s, "open the ledger home")
	before := s.current.led.Len()
	reply := run(t, s, "bring the returned ledger")
	if !strings.Contains(reply, "they do not meet") {
		t.Fatalf("a foreign anchor was not refused plainly: %q", reply)
	}
	if strings.Contains(reply, "reconcile") {
		t.Fatal("merge was offered for a foreign ledger")
	}
	if s.current.led.Len() != before || s.pending != nil {
		t.Fatal("a foreign seat changed the ledger")
	}

	// A mirror and an undeclared seat are skipped, never united.
	os.WriteFile(filepath.Join(dir, seatDeclName),
		mustJSON(t, seatDecl{Kind: "mirror", Ruling: "read-only beside ours"}), 0o600)
	if _, err := s.execute(mustParse(t, "bring the returned ledger")); err == nil {
		t.Fatal("with only a mirror present, reunion should find no berth")
	}
	os.Remove(filepath.Join(dir, seatDeclName))
	if _, err := s.execute(mustParse(t, "bring the returned ledger")); err == nil {
		t.Fatal("an undeclared seat was operated on")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustParse(t *testing.T, s string) command {
	t.Helper()
	c, err := parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The folder import: one manifest plus blobs in the library, symlinks
// refused, and the carrier inventory untouched.
func TestFolderImportAndInventory(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()

	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "sub"), 0o700)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o600)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("بتا با نیم‌فاصله"), 0o600)

	reply := run(t, s, "bring "+src+" to کارها/درخت")
	if !strings.Contains(reply, "brought it, checked it, wrote it") {
		t.Fatalf("folder import failed: %q", reply)
	}

	// The content lives inside the vessel (contract E5): the two files and
	// the manifest are content records there, and the library holds nothing.
	libFiles := 0
	filepath.Walk(f.library, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			libFiles++
		}
		return nil
	})
	if libFiles != 0 {
		t.Fatalf("the library holds %d files; content belongs inside the carrier", libFiles)
	}
	ids := map[frame.ID]bool{}
	s.current.car.Vessel().Scan(func(h vessel.Header, r vessel.Ref) error {
		if h.Type == vessel.RecContent {
			ids[h.ID] = true
		}
		return nil
	})
	if len(ids) != 3 { // two files + one manifest
		t.Fatalf("the vessel holds %d pieces of content, want 3", len(ids))
	}
	for _, want := range []string{"alpha", "بتا با نیم‌فاصله"} {
		var buf bytes.Buffer
		d, _ := content.New([]byte(want), "application/octet-stream")
		if err := content.Fetch(s.current.car, d, &buf); err != nil || buf.String() != want {
			t.Fatalf("%q does not come back from the vessel: %v", want, err)
		}
	}

	// A symlink refuses the whole import and the source stays untouched.
	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "real.txt"), []byte("x"), 0o600)
	os.Symlink("real.txt", filepath.Join(bad, "link.txt"))
	reply = run(t, s, "bring "+bad+" to کارها/بد")
	if !strings.Contains(reply, "did not write it") {
		t.Fatalf("a symlink did not refuse the import: %q", reply)
	}

	// The carrier inventory is exactly rokh.json and .rokh, nothing else.
	dir := filepath.Join(f.vault, "ledgers", f.name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != vessel.Dir {
			t.Fatalf("the shell put %q inside the carrier", e.Name())
		}
	}
}

// The library refuses to sit inside a carrier or the vault's ledgers tree.
func TestLibraryPlacementIsRefused(t *testing.T) {
	f := makeFixture(t)

	badLib := filepath.Join(f.vault, "ledgers", "sneaky")
	s := newSession(f.vault, "", badLib, testPass)
	defer s.closeAll()
	run(t, s, "open the ledger home")
	if _, err := s.contentRoot(s.current.car.Anchor()); err == nil {
		t.Fatal("a library inside the ledgers tree was accepted")
	}

	insideCarrier := filepath.Join(f.vault, "ledgers", f.name, "lib")
	s2 := newSession(f.vault, "", insideCarrier, testPass)
	defer s2.closeAll()
	run(t, s2, "open the ledger home")
	if _, err := s2.contentRoot(s2.current.car.Anchor()); err == nil {
		t.Fatal("a library inside a carrier was accepted")
	}
}

// Export writes files and changes nothing in the ledger.
func TestExportChangesNothing(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()
	run(t, s, "write at کارها: بیرون‌رفتنی")
	run(t, s, "write")
	before := s.current.led.Len()

	out := filepath.Join(t.TempDir(), "out")
	reply := run(t, s, "carry کارها to "+out)
	if !strings.Contains(reply, "Nothing in the ledger moved") {
		t.Fatalf("export reply wrong: %q", reply)
	}
	if s.current.led.Len() != before {
		t.Fatal("export created events")
	}
	files, _ := os.ReadDir(out)
	if len(files) != 1 {
		t.Fatalf("expected one exported file, got %d", len(files))
	}
}

// The boundary, in the product and not only in the package.
//
// Between doing and recording there is a line. A sentence typed at the shell
// lands in the working state, where it is not final: the form of its effect is
// shown, and what is shown is a prediction, carrying no name because the name
// is the hash of bytes not yet made. One explicit act — the same verb, bare —
// carries it across, and after that there is no way back.
//
// Whether the working state sits in memory or on a disk has nothing to do with
// this. What matters is that nothing crosses on its own, and the count below
// is how that is checked: the ledger does not grow until the closing act.
//
//	— T4.4, T4.5, T9.2, T8.2, N4.7
func TestASentenceDoesNotReachTheLedgerUntilItIsClosed(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()
	run(t, s, "open the ledger home")

	before := s.current.led.Len()

	form := run(t, s, "write at کارها: چیزی که هنوز نرفته")
	if strings.HasPrefix(form, "written.") {
		t.Fatalf("the sentence announced itself as written: %q", form)
	}
	if regexp.MustCompile(`[0-9a-f]{64}`).FindString(form) != "" {
		t.Fatalf("the form carries a name, and the name does not exist yet: %q", form)
	}
	if s.current.led.Len() != before {
		t.Fatal("the ledger grew before the closing act")
	}

	// Revising is free: the sentence is not final until it is closed.
	run(t, s, "write at کارها: و این هم نه")
	run(t, s, "write at کارها: این یکی می‌ماند")
	if s.current.led.Len() != before {
		t.Fatal("revising a waiting sentence recorded something")
	}

	// Reading, looking, listing — none of them is the act.
	run(t, s, "read کارها")
	run(t, s, "see the ledger")
	if s.current.led.Len() != before {
		t.Fatal("looking at the ledger recorded something")
	}

	// And now the one act that crosses.
	reply := run(t, s, "write")
	if !strings.Contains(reply, "written") {
		t.Fatalf("the closing act did not record: %q", reply)
	}
	if s.current.led.Len() != before+1 {
		t.Fatalf("the ledger holds %d, want %d — one sentence, one event",
			s.current.led.Len(), before+1)
	}
	id, _ := frame.ParseID(regexp.MustCompile(`[0-9a-f]{64}`).FindString(reply))
	e, ok := s.current.led.Get(id)
	if !ok {
		t.Fatal("what was closed is not in the ledger")
	}
	if string(e.Event.Payload) != "این یکی می‌ماند" {
		t.Fatalf("what crossed was not the latest revision: %q", e.Event.Payload)
	}

	// Nothing is waiting any more, so there is nothing to close.
	if _, err := s.execute(mustParse(t, "write")); err == nil {
		t.Fatal("closing twice recorded a second event")
	}
	if s.current.led.Len() != before+1 {
		t.Fatal("closing twice grew the ledger")
	}
}

// A mirror is somebody else's ledger, and it is read.
//
// This is the second half of what Rokh owes the mirror ruling. The first half
// is that the seat declares itself, is verified against its own anchor, and is
// never merged — refused elsewhere in this file. But a seat nobody can read is
// not read-only; it is inert. So a declared mirror opens, its events are
// readable, and every sentence that would record something is refused at the
// door rather than signing and failing at the end.
//
// Rokh makes a mirror possible. Whether anybody runs one, and for whom, is a
// service somebody offers and not Rokh's to decide.
//
//	— T13.4, T8.4
func TestAMirrorIsReadAndNeverWritten(t *testing.T) {
	f := makeFixture(t)

	// Somebody else's ledger, with something in it, sitting in a seat.
	_, opriv, _ := ed25519.GenerateKey(rand.Reader)
	og, err := event.Sign(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("another ledger"),
	}, opriv)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.mount, "همسایه")
	oc := makeV1(t, dir, testPass, og, opriv)
	anchor := og.ID
	note, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{og.ID},
		Address: "خانه/روزنگار", Verb: "note", Payload: []byte("نوشتهٔ همسایه")}, opriv)
	if err != nil {
		t.Fatal(err)
	}
	recordV1(t, oc, dir, note, "main")
	decl, _ := json.Marshal(seatDecl{Kind: "mirror", Ruling: "read-only, never made one with this"})
	if err := os.WriteFile(filepath.Join(dir, seatDeclName), decl, 0o600); err != nil {
		t.Fatal(err)
	}

	s := f.session()
	defer s.closeAll()

	// It opens, and what is inside is readable.
	run(t, s, "open the ledger همسایه")
	if s.current == nil || !s.current.mirror {
		t.Fatal("a declared mirror did not open as one")
	}
	if s.current.led.Genesis() != og.ID {
		t.Fatal("the mirror opened on the wrong anchor")
	}
	reply := run(t, s, "read خانه/روزنگار")
	if !strings.Contains(reply, "نوشتهٔ همسایه") {
		t.Fatalf("the neighbour's own writing was not readable: %q", reply)
	}
	// Its two events are counted: the neighbour's note, and the genesis,
	// which is the ledger's own and is counted apart.
	if r := run(t, s, "see the ledger"); !strings.Contains(r, "1 event and 1 system event") {
		t.Fatalf("the mirror's own count did not read: %q", r)
	}

	// And every sentence that would record something is refused at the door.
	for _, say := range []string{
		"write at خانه/روزنگار: این را من می‌نویسم",
		"entrust writing at خانه to کسی",
		"bring the returned ledger",
	} {
		_, err := runLine2(s, say)
		if err == nil {
			t.Fatalf("a mirror accepted %q", say)
		}
		if !strings.Contains(err.Error(), "mirror") {
			t.Fatalf("%q was refused, but not as a mirror: %v", say, err)
		}
	}

	// Nothing about looking at it changed it.
	if n := s.current.led.Len(); n != 2 {
		t.Fatalf("the mirror holds %d events after being read", n)
	}
}

// runLine2 runs one sentence and hands back the error rather than failing.
func runLine2(s *session, line string) (string, error) {
	c, err := parse(line)
	if err != nil {
		return "", err
	}
	return s.execute(c)
}

// Several sentences wait at once, one per address: a second sentence for
// another address waits beside the first, saying a sentence again for the
// same address revises it, "write" records the newest, "write {n}" the n-th,
// and "cancel" lets one go without recording. Each reply says which key
// signs under what, and each refusal carries its code.
//
//	— T4.4, T9.2, T12.5
func TestSeveralSentencesWaitAndEachIsItsOwnAct(t *testing.T) {
	f := makeFixture(t)
	s := f.session()
	defer s.closeAll()
	run(t, s, "open the ledger home")
	before := s.current.led.Len()
	r1 := run(t, s, "write at کارها: نخست")
	if !strings.Contains(r1, "waiting as sentence 1:") || !strings.Contains(r1, "signed by root under the owner's own right") {
		t.Fatalf("first: %q", r1)
	}
	r2 := run(t, s, "write at یادها: دوم")
	if !strings.Contains(r2, "waiting as sentence 2 of 2:") {
		t.Fatalf("second: %q", r2)
	}
	r3 := run(t, s, "write at کارها: نخست، بازنویسی")
	if !strings.Contains(r3, "waiting as sentence 1 of 2:") {
		t.Fatalf("revising the first keeps its place: %q", r3)
	}
	if s.current.led.Len() != before {
		t.Fatal("waiting recorded something")
	}
	if _, err := s.execute(mustParse(t, "write 3")); err == nil || !strings.Contains(err.Error(), "[nothing_waiting]") {
		t.Fatalf("naming a sentence that is not there: %v", err)
	}
	// "write" records the newest — the second — and says so with the key.
	reply := run(t, s, "write")
	if !strings.Contains(reply, "written and recorded.") || !strings.Contains(reply, "signed by root under the owner's own right, through rokh-shell") {
		t.Fatalf("closing: %q", reply)
	}
	if s.current.led.Len() != before+1 {
		t.Fatal("one closing, one event")
	}
	id, _ := frame.ParseID(regexp.MustCompile(`[0-9a-f]{64}`).FindString(reply))
	if e, ok := s.current.led.Get(id); !ok || e.Event.Address != "یادها" {
		t.Fatalf("the newest was recorded: %v %v", ok, e.Event.Address)
	}
	// The revised first still waits; cancel lets it go and records nothing.
	c := run(t, s, "cancel 1")
	if !strings.Contains(c, "let go.") || s.current.led.Len() != before+1 {
		t.Fatalf("cancel: %q (len %d)", c, s.current.led.Len())
	}
	if _, err := s.execute(mustParse(t, "write")); err == nil {
		t.Fatal("nothing waits after the cancel")
	}
	if _, _, err := s.say("sing a song"); err == nil || !strings.Contains(err.Error(), "[unknown_sentence]") || !strings.Contains(err.Error(), `"?" or "/"`) {
		t.Fatalf("an unknown sentence carries its code and names both keys: %v", err)
	}
}

// Leaving with sentences still waiting says so, before the parting line:
// they were never recorded, and nothing was recorded by leaving either.
func TestLeavingNamesTheSentencesStillWaiting(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	s, err := makeRokh(vault, "", "", "pass", "home", minRoom)
	if err != nil {
		t.Fatal(err)
	}
	before := s.current.led.Len()
	for _, in := range []string{"write at home/a: one\nwrite at home/b: two\nleave\n", "write at home/c: three\n"} {
		var out, notes strings.Builder
		if code := runLines(s, strings.NewReader(in), &out, &notes, false); code != 0 {
			t.Fatalf("exit %d: %s", code, notes.String())
		}
		n := strings.Count(in, "write at")
		want := letGo(n, false)
		if !strings.Contains(notes.String(), want) || !strings.Contains(want, "nothing was recorded") {
			t.Fatalf("leaving with %d waiting said %q, want %q", n, notes.String(), want)
		}
		if !strings.Contains(out.String(), tplClosed) {
			t.Fatalf("no parting line: %q", out.String())
		}
		s.drafts = nil
		if err := s.openByName("home"); err != nil {
			t.Fatal(err)
		}
	}
	if after := s.current.led.Len(); after != before {
		t.Fatalf("leaving recorded %d events", after-before)
	}
}
