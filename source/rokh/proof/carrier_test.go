package proof

// The carrier's own guarantees, witnessed on the carrier as it ships: a
// folder on the drive, the medium adapter, the kernel's writer's turn and
// the owner's passphrase. The 0.9 carrier suite held these proofs and they
// were removed with the 0.9 store (3dcc378); these are their v1 form, and
// each keeps the claim it made.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/content"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/turn"
	"rokh/vessel"
)

// The footprint is the whole trace. A folder the person already keeps a file
// in is made a carrier; a door records in it, content is brought in, a second
// branch is written, and the carrier is opened again and read. Everything
// that did lies inside the one directory the carrier declares, and none of it
// is legible there. Take the footprint away and what is left is exactly what
// was there before, untouched, and the folder is no longer a carrier: nothing
// the carrier needs, or leaves behind, lies anywhere else.
//
//	— T8, T1.2, N4.6, N7.3
func TestFootprintIsTheWholeTrace(t *testing.T) {
	p := onFolder(t)
	mine := filepath.Join(p.dir, "my-own-file.txt")
	if err := os.WriteFile(mine, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newWorldAt(t, p)
	s := daemon.New(w.car, w.load(), w.at(daemon.Options{AllowSign: true}))
	line := "a line that stays inside the carrier"
	written := recorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/journal",
		"verb": "note", "message": line}), "a recording through a door")
	brought := []byte("a file brought into the carrier")
	putContent(t, w, "home/files", brought)
	id, err := frame.ParseID(written["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	w.record(w.sign(w.root, nil, []frame.ID{id}, "home/journal", "note",
		[]byte("on a branch of its own")), "elsewhere")
	if got := w.load().Len(); got != 3 {
		t.Fatalf("the carrier holds %d events, want 3", got)
	}

	footprint := map[string]bool{}
	for _, name := range carrier.Footprint() {
		footprint[name] = true
	}
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "my-own-file.txt" && !footprint[e.Name()] {
			t.Fatalf("the carrier left %q outside its footprint %v", e.Name(), carrier.Footprint())
		}
	}
	all := allBytes(t, p.dir)
	for what, needle := range map[string][]byte{
		"the recorded line": []byte(line), "the content": brought,
		"an address": []byte("home/journal"), "a branch's name": []byte("elsewhere"),
	} {
		if bytes.Contains(all, needle) {
			t.Errorf("%s is legible in the folder", what)
		}
	}

	for _, name := range carrier.Footprint() {
		if err := os.RemoveAll(filepath.Join(p.dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	left, err := os.ReadDir(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name() != "my-own-file.txt" {
		var names []string
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Fatalf("something remained after the footprint was removed: %v", names)
	}
	if b, err := os.ReadFile(mine); err != nil || string(b) != "mine" {
		t.Fatalf("the person's own file was touched: %q, %v", b, err)
	}
	if _, _, _, err := p.openAs(pass); vessel.Code(err) != "not_a_vessel" {
		t.Fatalf("the folder is still taken for a carrier after its footprint was removed: %v", err)
	}
}

// The measure of ownership is one thing and it is simple: if you cannot take
// it and go, you are not the owner. So the whole carrier is carried somewhere
// else, file by file, and opened there with nothing from where it came, no
// network and nobody's permission; the original is destroyed first. Everything
// is still in it: the events with their bodies, the branches, the content,
// the owner's key, and the room to go on writing.
//
// The three rights come together and do not come apart: the data is yours,
// you say who sees it, and you can leave. The last one is what makes the
// other two more than a promise.
//
//	— N7.1, N7.2, N7.3, T1.2
func TestTakeItAndGo(t *testing.T) {
	w := newWorld(t)
	kept := w.record(w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/journal", "note",
		[]byte("everything I ever wrote")), "main")
	data := []byte("a file I brought in")
	cid := putContent(t, w, "home/files", data)
	before := w.load()

	// Carry it away, and destroy the original entirely: nothing may be left
	// to depend on.
	away := onFolder(t)
	copyTree(t, w.dir, away.dir)
	if err := os.RemoveAll(w.dir); err != nil {
		t.Fatal(err)
	}

	c, sec, _, err := away.openAs(pass)
	if err != nil {
		t.Fatalf("the carrier did not open where it was carried to: %v", err)
	}
	if c.Anchor() != w.anchor {
		t.Fatal("the ledger's anchor did not travel")
	}
	if raw, err := c.Get(kept.ID); err != nil || !bytes.Equal(raw, kept.Raw) {
		t.Fatalf("what was written did not travel: %v", err)
	}
	if got := branchOf(t, c, "main"); got != kept.ID {
		t.Fatal("the branch did not travel")
	}
	if got := contentOf(t, c, cid); !bytes.Equal(got, data) {
		t.Fatal("the content did not travel")
	}
	if after := loadFrom(t, c); !sameIDs(after.Order(), before.Order()) {
		t.Fatal("the ledger read where it was carried is not the ledger it was")
	}
	if !bytes.Equal(sec.Root(), w.root) {
		t.Fatal("the owner's key did not travel")
	}

	// And it goes on where it is now, signed by the key that travelled.
	next := w.sign(sec.Root(), nil, []frame.ID{kept.ID}, "home/journal", "note",
		[]byte("written where it was carried"))
	commitOn(t, away, c, recordOn(next, "main"))
	there, _ := away.open(t)
	if l := loadFrom(t, there); l.State(next.ID) != ledger.Accepted {
		t.Fatal("the carrier does not go on being written where it was carried")
	}
}

// Give away your key and it is over. Rokh is cryptography, not a miracle: no
// mechanism can stop a person from handing over their passphrase, by force or
// by choice, and with it everything opens. Saying so plainly is the point: a
// security claim that hides its own limit is advertising.
//
// It is also one row of the adversary table, exactly as that table has it:
// somebody who has taken the carrier can copy the files and see that it is a
// Rokh carrier, and can read nothing without the key; and somebody who has
// the key can read everything, the owner's own signing key included.
//
//	— N6.1, N5.1
func TestWithThePassphraseEverythingOpensAndThatIsTheLimit(t *testing.T) {
	w := newWorld(t)
	secret := []byte("something I would rather nobody read")
	diary := w.record(w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/diary", "note", secret), "the-diary")
	photo := []byte("a photograph I would rather nobody saw")
	cid := putContent(t, w, "home/diary", photo)

	// Somebody takes the carrier: a copy of every file.
	taken := onFolder(t)
	copyTree(t, w.dir, taken.dir)
	all := allBytes(t, taken.dir)
	if !bytes.Contains(all, []byte(vessel.Magic)) {
		t.Fatal("the copy does not say that it is a rokh vessel; the table says it does")
	}
	for what, needle := range map[string][]byte{
		"the diary's words": secret, "the photograph": photo, "the address": []byte("home/diary"),
		"the branch's name": []byte("the-diary"), "the anchor": w.anchor[:],
		"the event's signed head": diary.Head, "the root's seed": w.root.Seed(),
	} {
		if bytes.Contains(all, needle) {
			t.Errorf("%s is readable in the copy without the key", what)
		}
	}

	// Without the passphrase: nothing.
	for _, guess := range []string{"a guess", pass + "!", strings.ToUpper(pass)} {
		if _, _, _, err := taken.openAs(guess); !errors.Is(err, vessel.ErrLocked) {
			t.Fatalf("the copy answered the guess %q with %v, not a refusal", guess, err)
		}
	}

	// The same stranger, once the passphrase is handed over: everything.
	c, sec, _, err := taken.openAs(pass)
	if err != nil {
		t.Fatalf("the passphrase did not open it: %v", err)
	}
	if raw, err := c.Get(diary.ID); err != nil || !bytes.Equal(raw, diary.Raw) {
		t.Fatalf("the passphrase opened the carrier but not what was written: %v", err)
	}
	if got := contentOf(t, c, cid); !bytes.Equal(got, photo) {
		t.Fatal("the passphrase opened the carrier but not its content")
	}
	if got := branchOf(t, c, "the-diary"); got != diary.ID {
		t.Fatal("the passphrase opened the carrier but not its branches")
	}
	if !bytes.Equal(sec.Root(), w.root) {
		t.Fatal("the passphrase opened the contents but not the key; the limit is " +
			"that it opens everything, and this test exists to say so")
	}
}

// Key recovery is not in the design, and that is a ruling, not an inability
// (contract 4.6: the owner's passphrase has no recovery and gets none). There
// is no reset, no escrow, no recovery phrase and no back door: a wrong
// passphrase is refused, whichever of the carrier's cells it is tried against;
// nothing on the carrier's own files can be asked to produce the key; and no
// package of the core offers a road around the passphrase.
//
// The two losses are also not the same: lose the key and the encrypted copies
// are useless; lose every copy and the key buys nothing. Both are the owner's
// to guard.
//
//	— T8.6, N6.6
func TestThereIsNoWayBackFromALostPassphrase(t *testing.T) {
	w := newWorld(t)
	worth := []byte("a secret worth keeping")
	w.record(w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/vault", "note", worth), "main")

	for _, guess := range []string{"a guess", strings.ToUpper(pass), pass + " ", ""} {
		if _, _, _, err := w.openAs(guess); !errors.Is(err, vessel.ErrLocked) {
			t.Fatalf("the guess %q: %v; a wrong passphrase is refused and gets nothing else", guess, err)
		}
	}
	all := allBytes(t, w.dir)
	if bytes.Contains(all, worth) {
		t.Fatal("the secret is on the drive in the clear; the passphrase guards nothing")
	}
	if bytes.Contains(all, w.root.Seed()) {
		t.Fatal("the root's seed is on the drive in the clear")
	}

	// The surface offers no road around it: nothing the carrier, the vessel
	// or the key layer exports returns a key without the passphrase.
	for name := range exportedNames(t, "../carrier", "../vessel", "../key") {
		low := strings.ToLower(name)
		for _, road := range []string{"recover", "escrow", "reset", "backup", "forgot", "backdoor", "masterkey"} {
			if strings.Contains(low, road) {
				t.Errorf("the core offers %q; key recovery is outside the design", name)
			}
		}
	}
	// The one road in takes the passphrase and nothing else.
	var unlock func(passphrase string) func(slots [][]byte, salt []byte, iter int) ([]byte, error) = key.Unlock
	if unlock == nil {
		t.Fatal("the key layer's unlock is gone; this test no longer watches the only way in")
	}

	// Lose every copy, and the key buys nothing.
	if err := os.RemoveAll(filepath.Join(w.dir, vessel.Dir)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := w.openAs(pass); vessel.Code(err) != "not_a_vessel" {
		t.Fatalf("with every copy gone the passphrase still found something: %v", err)
	}
}

// Several carriers open at once and none of them knows the other. A carrier is
// self-contained: it needs nothing outside itself, and it learns nothing from
// a sibling opened beside it. Two carriers in two folders and a third in
// memory are open in one process at the same time, each with its own
// passphrase, its own anchor and its own writer's turn.
//
//	— T8.3, T8, N4.6
func TestSeveralCarriersOpenAtOnceAndNoneKnowsTheOther(t *testing.T) {
	type opened struct {
		p          place
		c          *carrier.Carrier
		root       ed25519.PrivateKey
		gen        event.Signed
		passphrase string
		only       event.Signed
	}
	open := func(p place, passphrase string) *opened {
		_, root := newKey(t)
		gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis,
			Payload: []byte(passphrase)}, root)
		if err != nil {
			t.Fatal(err)
		}
		c := p.createAs(t, gen.ID, root, passphrase)
		commitOn(t, p, c, recordOn(gen, "main"))
		return &opened{p: p, c: c, root: root, gen: gen, passphrase: passphrase}
	}
	all := []*opened{
		open(onFolder(t), "first passphrase"),
		open(onFolder(t), "second passphrase"),
		open(inMemory(), "third passphrase"),
	}

	// The first folder's turn is held while the second and the third record:
	// a turn belongs to one carrier and keeps no other waiting.
	lk, err := turn.Acquire(all[0].p.dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range all {
		a := o.gen.ID
		only, err := event.SignFresh(event.Event{Carrier: &a, Parents: []frame.ID{o.gen.ID},
			Address: "home/journal", Verb: "note", Payload: []byte("only in this one")}, o.root)
		if err != nil {
			t.Fatal(err)
		}
		o.only = only
		if i == 0 {
			rec, err := o.c.Begin(lk)
			if err != nil {
				t.Fatal(err)
			}
			if err := recordOn(only, "main")(rec); err != nil {
				t.Fatal(err)
			}
			if out, err := rec.Commit(); out != vessel.Recorded {
				t.Fatalf("the first carrier under its own turn: %s %v", out, err)
			}
			continue
		}
		commitOn(t, o.p, o.c, recordOn(only, "main"))
	}
	if err := lk.Release(); err != nil {
		t.Fatal(err)
	}

	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if a.c.Anchor() == b.c.Anchor() {
				t.Fatal("two carriers share an anchor; each ledger has exactly one")
			}
			// Neither passphrase opens the other's carrier.
			if _, _, _, err := b.p.openAs(a.passphrase); !errors.Is(err, vessel.ErrLocked) {
				t.Errorf("carrier %d's passphrase opened carrier %d: %v", i, j, err)
			}
			// An event recorded in one is not in the other.
			if _, err := b.c.Get(a.only.ID); !errors.Is(err, carrier.ErrNotFound) {
				t.Errorf("carrier %d read an event that lives only in carrier %d: %v", j, i, err)
			}
			// And one's session opens nothing of the other's: laid over the
			// other's vessel it sees a head, never a body.
			cross := carrier.Wrap(b.c.Vessel(), sessionOf(t, a.c, a.passphrase))
			if raw, err := cross.Get(b.only.ID); err != nil || !bytes.Equal(raw, b.only.Head) {
				t.Errorf("carrier %d's session opened carrier %d's event: %d bytes, %v", i, j, len(raw), err)
			}
		}
		// Each still reads its own, whole.
		if raw, err := a.c.Get(a.only.ID); err != nil || !bytes.Equal(raw, a.only.Raw) {
			t.Errorf("carrier %d does not read its own event: %v", i, err)
		}
	}
}

// The boundary of the claim, said as narrowly as it should be: what was shown
// is about an individual's ledger, not about every possible institution. If a
// layer above makes a single spendable item, something two people cannot both
// hold, that problem comes back, and it is the nature of that item, not a
// shortcoming of Rokh.
//
// Rokh has no way of its own there and claims none: it records the handing
// over and the receipt, and does not hold the item itself. This is what that
// looks like in the v1 carrier: nothing in its surface mints, transfers,
// balances or reserves anything; an event recorded again is still there,
// unchanged, as often as it is read; and content is named by its hash, handed
// over as a descriptor, and read as often as it is asked for, never consumed.
//
//	— N2.8, N2.9, N2.10, N2.11
func TestRokhOffersNoWayToHoldASingleSpendableThing(t *testing.T) {
	// The carrier records events and content and moves references. That is
	// the whole vocabulary; there is nothing in it that could make one item
	// exclusive.
	surface := exportedNames(t, "../carrier")
	for _, forbidden := range []string{
		"Mint", "Transfer", "Balance", "Spend", "Reserve", "Lock", "Owner", "Supply",
	} {
		if surface[forbidden] {
			t.Errorf("the carrier offers %q; a single spendable item needs a "+
				"discipline Rokh does not have and does not claim", forbidden)
		}
	}

	w := newFastWorld(t)
	thing := w.record(w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/things", "note",
		[]byte("the one thing")), "main")
	if frame.Hash(thing.Head) != thing.ID {
		t.Fatal("the name is not the hash of the thing's signed head")
	}
	// Recording it again does not consume it, and reading it does not either.
	for i := 0; i < 3; i++ {
		w.commit(func(r *carrier.Recording) error {
			return r.Event(thing.ID, thing.Head, thing.Body, thing.Event.Address)
		})
		raw, err := w.reopen().Get(thing.ID)
		if err != nil || !bytes.Equal(raw, thing.Raw) {
			t.Fatalf("recording it again spent it: %v", err)
		}
	}
	if got := w.load().Len(); got != 2 {
		t.Fatalf("recording one event again made %d events, want 2", got)
	}

	// Content: named by its hash, handed over as a descriptor, never consumed.
	data := []byte("a document handed over, not held")
	first := putContent(t, w, "home/things", data)
	second := putContent(t, w, "home/things", data)
	if first != second || first != frame.Hash(data) {
		t.Fatal("the same content is not one name, the hash of itself")
	}
	d, err := content.New(data, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	handed := w.record(w.sign(w.root, nil, []frame.ID{thing.ID}, "home/things", "hand", payload), "main")
	c := w.reopen()
	for i := 0; i < 3; i++ {
		if got := contentOf(t, c, first); !bytes.Equal(got, data) {
			t.Fatal("reading the content consumed it")
		}
	}
	e, ok := loadFrom(t, c).Get(handed.ID)
	if !ok {
		t.Fatal("the handing over is not recorded")
	}
	back, err := content.Decode(e.Event.Payload)
	if err != nil || back.Hash != first || back.Size != uint64(len(data)) {
		t.Fatalf("the handing over does not name the content it hands over: %+v, %v", back, err)
	}
}

// The base profile is a list of choices, and a list nobody exercises is a
// wish. This runs the real path (sign, hash, seal, name, store, open) on a
// carrier as it ships, and checks the primitives by what they produce: an
// Ed25519 signature of 64 bytes that verifies over the head it closes; a
// SHA-256 name of 32 bytes that is the hash of that very head, which names
// its body by SHA-256; a passphrase stretched by PBKDF2-HMAC-SHA256 with the
// salt and rounds the head file declares; an envelope sealed to the owner's
// X25519 reader; slabs of one fixed length, named by their place, that carry
// neither the plaintext nor a readable name. The list is v1's (contract C3);
// a change to it is a new generation, not an edit.
//
// None of it is our invention, and none may become one: hand-rolled
// cryptography is broken cryptography.
//
//	— N4.10, T8.7, T8.1
func TestTheBaseProfileIsExercisedNotJustNamed(t *testing.T) {
	w := newWorld(t)
	if len(w.rootPub) != ed25519.PublicKeySize || len(w.root) != ed25519.PrivateKeySize ||
		ed25519.PublicKeySize != 32 || ed25519.PrivateKeySize != 64 {
		t.Fatalf("the signature scheme is not Ed25519: keys are %d and %d bytes", len(w.rootPub), len(w.root))
	}
	payload := []byte("the base profile, exercised")
	e := w.record(w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/profile", "note", payload), "main")

	// SHA-256: the name is the hash of the very head, and the head names its
	// body by hash.
	if len(e.ID) != sha256.Size || [32]byte(e.ID) != sha256.Sum256(e.Head) {
		t.Fatal("the name is not the SHA-256 of the signed head")
	}
	if named, err := carrier.HeadBody(e.Head); err != nil || named != sha256.Sum256(e.Body) {
		t.Fatalf("the head does not name its body by SHA-256: %v", err)
	}
	// Ed25519: the head closes with a 64-byte signature over everything
	// before it.
	cut := len(e.Head) - frame.SigSize
	if frame.SigSize != ed25519.SignatureSize || !ed25519.Verify(w.rootPub, e.Head[:cut], e.Head[cut:]) {
		t.Fatal("the head's signature does not verify with Ed25519 over the head before it")
	}

	// The payload ceiling is what the document says, and it is enforced.
	if event.MaxPayload != 4096 {
		t.Fatalf("the payload ceiling is %d; the base profile says 4 KiB", event.MaxPayload)
	}
	a := w.anchor
	if _, err := event.SignFresh(event.Event{Carrier: &a, Parents: []frame.ID{e.ID}, Address: "home/profile",
		Verb: "note", Payload: make([]byte, event.MaxPayload+1)}, w.root); err == nil {
		t.Fatal("a payload over the ceiling was signed")
	}
	if _, err := event.SignFresh(event.Event{Carrier: &a, Parents: []frame.ID{e.ID}, Address: "home/profile",
		Verb: "note", Payload: make([]byte, event.MaxPayload)}, w.root); err != nil {
		t.Fatalf("a payload at the ceiling was refused: %v", err)
	}

	// The passphrase: PBKDF2-HMAC-SHA256 with the salt and the rounds the head
	// file declares in the clear.
	c, sec, _, err := w.openAs(pass)
	if err != nil {
		t.Fatal(err)
	}
	info := c.Vessel().Info()
	head, err := w.m.Read(vessel.HeadName(int(info.Generation%vessel.HeadSlots)), vessel.HeadSize)
	if err != nil {
		t.Fatal(err)
	}
	if string(head[:4]) != vessel.Magic || head[4] != vessel.KDFPBKDF2 {
		t.Fatalf("the head file does not declare format 1 and PBKDF2: % x", head[:5])
	}
	rounds := int(binary.BigEndian.Uint32(head[8:12]))
	if rounds != testIter || !bytes.Equal(head[12:44], info.Salt) {
		t.Fatalf("the head file declares %d rounds; the carrier was made with %d", rounds, testIter)
	}
	stretched, err := pbkdf2.Key(sha256.New, pass, info.Salt, rounds, 32)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := key.PassKey(pass, info.Salt, rounds)
	if err != nil || !bytes.Equal(stretched, theirs) {
		t.Fatalf("the passphrase is not stretched by PBKDF2-HMAC-SHA256: %v", err)
	}
	if vessel.DefaultIter != 600000 {
		t.Fatalf("the carrier key is derived in %d rounds; the base profile says 600,000", vessel.DefaultIter)
	}

	// The envelope of the event is sealed to the owner's X25519 reader.
	var env []byte
	if err := c.Events(func(r carrier.EventRecord) error {
		if r.ID == e.ID {
			var err error
			env, err = c.Vessel().Body(r.Ref)
			return err
		}
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(env) < 2 || env[0] != 0x01 || env[1] != 0x01 {
		t.Fatal("the event's envelope is not version 1 of the readers' suite")
	}
	reader, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		t.Fatal(err)
	}
	kids, err := key.Kids(env)
	named := false
	for _, k := range kids {
		named = named || k == reader.Kid()
	}
	if err != nil || !named {
		t.Fatalf("the envelope is not sealed to the owner's reader: %v", err)
	}

	// The files: each of its one fixed length, named by its place, and none
	// carrying the event, its payload, its name or the root's key.
	for name, size := range w.sizes(t) {
		want := 1 << slabLog2
		if strings.Contains(name, "/head") {
			want = vessel.HeadSize
		}
		if !vessel.ValidName(name) || size != want {
			t.Errorf("%s (%d bytes) is not a file of the profile", name, size)
		}
		if strings.Contains(name, e.ID.String()) || strings.Contains(name, hex.EncodeToString(e.ID[:4])) {
			t.Errorf("the file name %s carries the event's name", name)
		}
	}
	all := allBytes(t, w.dir)
	for what, secret := range map[string][]byte{
		"the event": e.Raw, "its head": e.Head, "its payload": payload, "the root's key": w.root.Seed(),
	} {
		if bytes.Contains(all, secret) {
			t.Errorf("%s is on the drive in the clear", what)
		}
	}
}

// Today's shape (one folder, the vessel's one directory in it, a passphrase)
// is the base profile, not the carrier itself. What matters is that nothing
// appears outside the declared inventory: the vessel is exactly its N slab
// files and four head files, named by the grammar of contract section 1, each
// of its fixed length, when it is made, after recordings through a door, after
// it is opened and read again, and after a grow, the one way the file set
// changes (contract 2.8).
//
//	— T8.7
func TestNothingIsCreatedBeyondTheInventory(t *testing.T) {
	w := newWorld(t)
	inventory(t, w.dir, slabs)

	s := daemon.New(w.car, w.load(), w.at(daemon.Options{AllowSign: true}))
	for i := 0; i < 5; i++ {
		recorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/journal",
			"verb": "note", "message": "an entry"}), "a recording")
	}
	cid := putContent(t, w, "home/files", bytes.Repeat([]byte("content "), 1000))
	w.record(w.sign(w.root, nil, []frame.ID{w.gen.ID}, "home/other", "note", []byte("another branch")), "other")
	inventory(t, w.dir, slabs)

	// Opening and reading create nothing either.
	c := w.reopen()
	if got := loadFrom(t, c).Len(); got != 7 {
		t.Fatalf("the carrier holds %d events, want 7", got)
	}
	contentOf(t, c, cid)
	inventory(t, w.dir, slabs)

	// A grow names a target, adds slab files of the grammar, and nothing else.
	lk, err := turn.Acquire(w.dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := w.car.Begin(lk)
	if err != nil {
		t.Fatal(err)
	}
	rec.Abandon()
	if err := w.car.Vessel().Grow(slabs + 16); err != nil {
		t.Fatal(err)
	}
	if err := lk.Release(); err != nil {
		t.Fatal(err)
	}
	inventory(t, w.dir, slabs+16)
	if got := w.load().Len(); got != 7 {
		t.Fatalf("after the grow the carrier holds %d events, want 7", got)
	}
}

// inventory insists that a carrier folder holds the vessel's inventory for n
// slabs and nothing else.
func inventory(t *testing.T, dir string, n int) {
	t.Helper()
	slabDir := regexp.MustCompile(`^` + vessel.Dir + `/[0-9a-f]{3}$`)
	files := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if rel != vessel.Dir && !slabDir.MatchString(rel) {
				t.Errorf("a directory outside the inventory: %s", rel)
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		want := int64(1) << slabLog2
		if strings.Contains(rel, "/head") {
			want = vessel.HeadSize
		}
		if !vessel.ValidName(rel) || fi.Size() != want {
			t.Errorf("a file outside the inventory: %s (%d bytes)", rel, fi.Size())
			return nil
		}
		files++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != n+vessel.HeadSlots {
		t.Fatalf("the vessel is %d files; for %d slabs it is %d", files, n, n+vessel.HeadSlots)
	}
}

// Encryption goes as far as the names. A v1 carrier's files are named by their
// place alone (contract section 1: four head files, and slab files named by
// their index), so the listing says nothing, not what is recorded and not how
// much. Two carriers of one shape, one holding only its genesis and one
// holding a diary, a photograph and a second branch, list the same names at
// the same lengths, and nothing in either reads without the key. And a file's
// bytes are bound to its place: the two slabs one recording wrote, each put
// under the other's name, do not open, and nothing of them is served
// (contract V3, V4).
//
//	— T8.1
func TestASlabCannotBeRelocatedAndItsNameSaysNothing(t *testing.T) {
	full := newFastWorld(t)
	empty := newFastWorld(t)
	words := []byte("what the listing must not say")
	diary := full.sign(full.root, nil, []frame.ID{full.gen.ID}, "home/diary", "note", words)

	// The recording's slabs: its pack, then its segment (contract 2.6).
	m := full.mem
	m.Record(true)
	full.record(diary, "main")
	var written []string
	for _, c := range m.Log() {
		if c.Op == "write" && !strings.Contains(c.Name, "/head") {
			written = append(written, c.Name)
		}
	}
	m.Record(false)
	if len(written) != 2 {
		t.Fatalf("the recording wrote %d slabs; this case wants its pack and its segment", len(written))
	}
	_, rep := full.open(t)
	generation := rep.Generation

	// Each under the other's name.
	pack, seg := m.Get(written[0]), m.Get(written[1])
	m.Put(written[0], seg)
	m.Put(written[1], pack)
	moved, rep := full.open(t)
	if rep.Generation == generation || len(rep.FellBack) == 0 {
		t.Fatalf("a generation whose slabs were moved was opened, or the fall back was not said: %+v", rep)
	}
	if raw, err := moved.Get(diary.ID); !errors.Is(err, carrier.ErrNotFound) {
		t.Fatalf("the moved recording was served: %d bytes, %v", len(raw), err)
	}
	m.Put(written[0], pack)
	m.Put(written[1], seg)
	back, rep := full.open(t)
	if rep.Generation != generation {
		t.Fatalf("put back, the carrier opens at generation %d, want %d", rep.Generation, generation)
	}
	if raw, err := back.Get(diary.ID); err != nil || !bytes.Equal(raw, diary.Raw) {
		t.Fatalf("put back, the recording does not read: %v", err)
	}

	photo := []byte("a photograph the listing must not show")
	putContent(t, full, "home/diary", photo)
	full.record(full.sign(full.root, nil, []frame.ID{diary.ID}, "home/diary", "note",
		[]byte("a second page")), "second-branch")

	// The listing: the same names at the same lengths, full or empty.
	if !reflect.DeepEqual(full.mem.Sizes(), empty.mem.Sizes()) {
		t.Fatal("the listing tells a full carrier from an empty one")
	}
	for name := range full.mem.Sizes() {
		if !vessel.ValidName(name) {
			t.Errorf("%s is outside the name grammar", name)
		}
		if strings.Contains(name, diary.ID.String()) || strings.Contains(name, hex.EncodeToString(diary.ID[:4])) {
			t.Errorf("the file name %s carries an event's name", name)
		}
	}
	// And nothing in the files reads without the key.
	for _, n := range full.mem.FileNames() {
		b := full.mem.Get(n)
		for what, needle := range map[string][]byte{
			"the diary's words": words, "the photograph": photo, "the address": []byte("home/diary"),
			"a branch's name": []byte("second-branch"), "the anchor": full.anchor[:], "a signed head": diary.Head,
		} {
			if bytes.Contains(b, needle) {
				t.Errorf("%s is readable in %s", what, n)
			}
		}
	}
}

// putContent brings content into the world's carrier in one recording and
// returns its id, the SHA-256 of the whole.
func putContent(t *testing.T, w *world, address string, data []byte) frame.ID {
	t.Helper()
	var id frame.ID
	w.commit(func(r *carrier.Recording) error {
		var err error
		id, err = r.Content(address, uint64(len(data)), func() (io.Reader, error) { return bytes.NewReader(data), nil })
		return err
	})
	return id
}

// contentOf reads content back whole from a carrier.
func contentOf(t *testing.T, c *carrier.Carrier, id frame.ID) []byte {
	t.Helper()
	var b bytes.Buffer
	if _, err := c.ContentTo(id, &b); err != nil {
		t.Fatalf("content %s: %v", id.Short(), err)
	}
	return b.Bytes()
}

// sessionOf opens the owner's session of an opened carrier with its
// passphrase: what that passphrase can seal and open, and nothing else.
func sessionOf(t *testing.T, c *carrier.Carrier, passphrase string) *key.Session {
	t.Helper()
	v := c.Vessel()
	info := v.Info()
	sess, _, err := key.VesselSession(passphrase, v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// copyTree copies a folder file by file, the way a person carries one away.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// allBytes is every byte stored under a folder, so a test can ask what is
// actually on the drive rather than what was meant to be.
func allBytes(t *testing.T, dir string) []byte {
	t.Helper()
	var all bytes.Buffer
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		all.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.Bytes()
}

// exportedNames lists what the non-test Go files of some packages export:
// functions, methods, types, variables and constants. A test asks what the
// surface offers rather than what it was meant to offer.
func exportedNames(t *testing.T, dirs ...string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				switch x := d.(type) {
				case *ast.FuncDecl:
					if x.Name.IsExported() {
						out[x.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, sp := range x.Specs {
						switch v := sp.(type) {
						case *ast.TypeSpec:
							if v.Name.IsExported() {
								out[v.Name.Name] = true
							}
						case *ast.ValueSpec:
							for _, nm := range v.Names {
								if nm.IsExported() {
									out[nm.Name] = true
								}
							}
						}
					}
				}
			}
		}
		if len(out) == 0 {
			t.Fatalf("%s exports nothing; this test would watch nothing", dir)
		}
	}
	return out
}
