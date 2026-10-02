package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// R4: the key's bootstrap tells
// three states of an empty owner list apart. No owner generation ever added
// before a point is the first bootstrap, where the owner's first generation
// stands in; an owner fold emptied by revocation is refused; an unknown point
// is refused. These tests reach the emptied state with the owner's dressed
// session still holding an owner, so nothing but the bootstrap's own rule can
// refuse it.

// ownerOf reads what the owner's passphrase holds in a carrier: the root, the
// owner's reader, the system reader and the owner's first keyring add.
type ownerOf struct {
	root   ed25519.PrivateKey
	reader key.Reader
	system key.Reader
	first  frame.ID
	firstK event.Keyring
}

func ownerHolds(t *testing.T, dir string) ownerOf {
	t.Helper()
	c := openForTest(t, dir, bondPass)
	info := c.Vessel().Info()
	sec, _, err := key.Try(bondPass, c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		t.Fatal(err)
	}
	var o ownerOf
	o.root = sec.Signer()
	if o.reader, err = key.ReaderFrom(sec.Reader[:]); err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, c)
	for _, id := range l.Order() {
		e, _ := l.Get(id)
		if e.Event.Verb != event.VerbKeyring {
			continue
		}
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && k.Op == event.KeyringAdd && k.IsOwner() && k.Gen == 1 {
			o.first, o.firstK = id, k
		}
	}
	if o.first.IsZero() {
		t.Fatal("rokh init recorded no owner generation")
	}
	priv, err := key.OpenFrom(o.reader, o.firstK.System, key.InfoSystem)
	if err != nil {
		t.Fatalf("the owner's add seals no system reader to the owner: %v", err)
	}
	if o.system, err = key.ReaderFrom(priv); err != nil {
		t.Fatal(err)
	}
	return o
}

// secondOwnerGen is the owner's next generation with the same reader, the
// same root as signer and the same system seal: the add of a rotation.
func (o ownerOf) secondOwnerGen(t *testing.T) []byte {
	t.Helper()
	p, err := event.Keyring{Op: event.KeyringAdd, Gen: 2, Name: "owner", Reader: o.reader.Public(),
		Signer: o.root.Public().(ed25519.PublicKey), Reads: []string{""}, System: o.firstK.System}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustRun(t *testing.T, fn func([]string) error, args ...string) string {
	t.Helper()
	out, errOut, err := bondRun(t, fn, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s%s", args, err, out, errOut)
	}
	return out
}

// Item 1, keySessionOf: an owner rotation made revoke-first leaves the next
// generation's add at a point whose owner fold was emptied by revocation. The
// ring at the heads holds that next generation, so the key's session would
// open; the bootstrap's own E3 re-check must refuse the record, and with it
// the opening, rather than let the first owner generation stand in.
func TestAKeysOpeningRefusesARecordAtAnOwnerFoldEmptiedByRevocation(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	mustRun(t, cmdInit, dir, "--message", "genesis")
	const readerPass = "synthetic reader passphrase"
	mustRun(t, cmdKey, "add", dir, "--name", "reader", "--reads", "journal",
		"--key-passphrase-file", writeFile(t, "reader.pass", []byte(readerPass)))
	if _, _, _, err := openCarrier(dir, readerPass, false); err != nil {
		t.Fatalf("the reader does not open before the revocation: %v", err)
	}
	o := ownerHolds(t, dir)
	// The root takes back the owner's only generation, through the command.
	mustRun(t, cmdKey, "revoke", dir, "--key", "owner")
	// Then the owner's next generation. The owner's own command cannot record
	// it now (its session, judged by the emptied fold, seals nothing), so it
	// is recorded as a union or a second branch could bring it: signed by the
	// root on the revoke, sealed to the owner's reader and the system reader.
	c := openForTest(t, dir, bondPass)
	head, found, err := c.Ref(defaultBranch)
	if err != nil || !found {
		t.Fatalf("main: %v %v", found, err)
	}
	anchor := c.Anchor()
	g2, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{head},
		Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: o.secondOwnerGen(t)}, o.root)
	if err != nil {
		t.Fatal(err)
	}
	env, err := key.SealReaders(key.TypeEvent, g2.ID, nil, [][]byte{o.reader.Public(), o.system.Public()}, g2.Body, rand.Reader)
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
	if err := rec.EventEnvelope(g2.ID, g2.Head, env); err != nil {
		t.Fatal(err)
	}
	if err := rec.SetRef(defaultBranch, g2.ID); err != nil {
		t.Fatal(err)
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		t.Fatalf("the owner's next generation: %s %v", out, err)
	}
	lock.Release()
	// Since T2 the owner's own opening judges the next generation at its own
	// point too: the owner fold there was emptied by revocation, so the owner
	// does not take it as live either, and the owner's ring at the heads holds
	// no owner. (Before T2 the owner's session judged it by the present ring
	// and held it.) The key's refusal below is the key's own rule at that
	// point, and nothing the owner's session holds stands in for it.
	sl, err := v1Sealer(openForTest(t, dir, bondPass), bondPass)
	if err != nil {
		t.Fatal(err)
	}
	if len(sl.(*key.Session).Ring.Owner()) != 0 {
		t.Fatal("the owner's session took as live an owner generation added where the owner fold was emptied by revocation")
	}
	_, _, _, err = openCarrier(dir, readerPass, false)
	if err == nil {
		t.Fatal("the reader opened a carrier holding a record at an owner fold emptied by revocation; the first generation stood in for it")
	}
	if !errors.Is(err, key.ErrOwnerUnknown) || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("the reader was refused for another reason: %v", err)
	}
}

// Item 2, keyAdd: the owner's generation is taken back on main while a second
// branch holds its next one. The owner's session, judged at the heads, holds
// an owner and seals, but a key added on main sits where the owner fold was
// emptied by revocation. The add is refused before anything is recorded, and
// the cell of the owner does not stand in for the owners of its envelope.
func TestAKeyAddAtAnOwnerFoldEmptiedByRevocationRecordsNothing(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	mustRun(t, cmdInit, dir, "--message", "genesis")
	o := ownerHolds(t, dir)
	mustRun(t, cmdBranch, dir, "--create", "side", "--at", o.first.String())
	mustRun(t, cmdWrite, dir, "--branch", "side", "--address", event.AddressRoot, "--verb", event.VerbKeyring,
		"--payload-file", writeFile(t, "owner-gen-2", o.secondOwnerGen(t)))
	revoke, err := event.Keyring{Op: event.KeyringRevoke, Gen: 1, Target: o.first}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, cmdWrite, dir, "--address", event.AddressRoot, "--verb", event.VerbKeyring,
		"--payload-file", writeFile(t, "revoke-owner-gen-1", revoke))
	c := openForTest(t, dir, bondPass)
	sl, err := v1Sealer(c, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	if len(sl.(*key.Session).Ring.Owner()) == 0 {
		t.Fatal("the owner's ring holds no owner; the case under test needs one")
	}
	before := c.Vessel().Info().Generation
	const latePass = "synthetic late key passphrase"
	_, errOut, err := bondRun(t, cmdKey, "add", dir, "--name", "late", "--reads", "journal",
		"--key-passphrase-file", writeFile(t, "late.pass", []byte(latePass)))
	if err == nil {
		t.Fatal("a key was added where the owner fold was emptied by revocation")
	}
	if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("the add was refused for another reason: %v %s", err, errOut)
	}
	c = openForTest(t, dir, bondPass)
	if after := c.Vessel().Info().Generation; after != before {
		t.Fatalf("the refused add committed: generation %d, then %d", before, after)
	}
	l := ledgerOf(t, c)
	adds, _ := l.Keyring()
	for _, id := range adds {
		e, _ := l.Get(id)
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && k.Name == "late" {
			t.Fatal("the refused key's add was recorded")
		}
	}
	info := c.Vessel().Info()
	if _, _, err := key.Try(latePass, c.Vessel().Slots(), info.Salt, info.Iter); err == nil {
		t.Fatal("the refused key's cell was installed")
	}
}

// The rule itself: live owners are the owners; none ever added lets the
// first generation stand in, and only when one is held; an emptied fold and
// an unknown point are refused.
func TestTheOwnerRuleTellsTheThreeStatesApart(t *testing.T) {
	first := []byte("first generation's reader")
	live := [][]byte{[]byte("a live owner generation")}
	if got, err := ownersOf(live, true, true, first); err != nil || len(got) != 1 || string(got[0]) != string(live[0]) {
		t.Fatalf("live owners: %q %v", got, err)
	}
	if got, err := ownersOf(nil, false, true, first); err != nil || len(got) != 1 || string(got[0]) != string(first) {
		t.Fatalf("the first bootstrap: %q %v", got, err)
	}
	if _, err := ownersOf(nil, false, true, nil); !errors.Is(err, key.ErrOwnerUnknown) {
		t.Fatalf("the first bootstrap with no first generation held: %v", err)
	}
	if _, err := ownersOf(nil, true, true, first); !errors.Is(err, errOwnerEmptied) || !errors.Is(err, key.ErrOwnerUnknown) {
		t.Fatalf("an owner fold emptied by revocation: %v", err)
	}
	if _, err := ownersOf(live, true, false, first); !errors.Is(err, key.ErrOwnerUnknown) {
		t.Fatalf("an unknown point: %v", err)
	}
}

// bareCarrier makes a carrier as rokh init does, but whose keyring never held
// an owner generation: the genesis alone on main, as a carrier made before
// the owner's add was recorded, or by the sentence surface, holds it.
func bareCarrier(t *testing.T, dir string) {
	t.Helper()
	params, err := vesselParams("8M", "256K", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	salt, vk := make([]byte, 32), make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(vk); err != nil {
		t.Fatal(err)
	}
	params.Salt = salt
	_, root, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("genesis")}, root)
	if err != nil {
		t.Fatal(err)
	}
	var seed [32]byte
	copy(seed[:], root.Seed())
	cell, sec, err := key.NewOwner(bondPass, salt, params.Iter, seed, vk, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if sess.PTK, err = vessel.SharedKey(vk); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := carrier.Create(medium.Dir{Root: dir}, params, vk, [][]byte{cell}, gen.ID, frame.Zero, nil, sess)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := turn.Acquire(dir, turnPatience)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	rec, err := c.Begin(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Event(gen.ID, gen.Head, gen.Body, gen.Event.Address); err != nil {
		t.Fatal(err)
	}
	if err := rec.SetRef(defaultBranch, gen.ID); err != nil {
		t.Fatal(err)
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		t.Fatalf("the genesis: %s %v", out, err)
	}
}

// The first bootstrap still bootstraps: on a carrier whose keyring never held
// an owner generation, a key's add sits where none was ever added, so the
// owner's cell stands in for the owners of its own envelope (E3), and the
// key's cell is installed.
func TestAKeyAddWhereNoOwnerGenerationWasEverAddedTakesTheCell(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	bareCarrier(t, dir)
	const readerPass = "synthetic reader passphrase"
	mustRun(t, cmdKey, "add", dir, "--name", "reader", "--reads", "journal",
		"--key-passphrase-file", writeFile(t, "reader.pass", []byte(readerPass)))
	c := openForTest(t, dir, bondPass)
	info := c.Vessel().Info()
	sec, _, err := key.Try(readerPass, c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil || sec.Key == ([32]byte{}) {
		t.Fatalf("the key's cell was not installed: %v", err)
	}
	ownerSec, _, err := key.Try(bondPass, c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := key.ReaderFrom(ownerSec.Reader[:])
	mine, _ := key.ReaderFrom(sec.Reader[:])
	l := ledgerOf(t, c)
	var add frame.ID
	adds, _ := l.Keyring()
	for _, id := range adds {
		e, _ := l.Get(id)
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && k.Key == sec.Key {
			add = id
		}
	}
	if add.IsZero() {
		t.Fatal("the key's add is not in the ledger")
	}
	if ever, known := l.OwnerEverAt(add); !known || ever {
		t.Fatalf("the case under test needs an add where no owner was ever added: ever %v known %v", ever, known)
	}
	own := 0
	c.Vessel().Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type != vessel.RecEvent || h.ID != add {
			return nil
		}
		env, err := c.Vessel().Body(ref)
		if err != nil {
			return err
		}
		if _, err := key.OpenReaders(key.TypeEvent, add, nil, env, mine); err == nil {
			own++
			if key.NamesAll(env, [][]byte{owner.Public()}) != nil {
				t.Fatal("the key's own envelope of its add does not name the owner's cell")
			}
		}
		return nil
	})
	if own == 0 {
		t.Fatal("no envelope of the key's add opens to the key")
	}
}

// A key's own passphrase changes no keyring: add, revoke and rotate are the
// owner's (K1), and nothing is recorded.
func TestAKeysPassphraseChangesNoKeyring(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	mustRun(t, cmdInit, dir, "--message", "genesis")
	const writerPass = "synthetic writer passphrase"
	mustRun(t, cmdKey, "add", dir, "--name", "writer", "--write", "--scope", "journal",
		"--key-passphrase-file", writeFile(t, "writer.pass", []byte(writerPass)))
	before := openForTest(t, dir, bondPass).Vessel().Info().Generation
	t.Setenv("ROKH_PASSPHRASE", writerPass)
	other := writeFile(t, "other.pass", []byte("synthetic other passphrase"))
	for _, args := range [][]string{
		{"add", dir, "--name", "other", "--reads", "journal", "--key-passphrase-file", other},
		{"revoke", dir, "--key", "writer"},
		{"rotate", dir, "--key", "writer", "--key-passphrase-file", other},
	} {
		if _, _, err := bondRun(t, cmdKey, args...); err == nil || !strings.Contains(err.Error(), "K1") {
			t.Fatalf("key %s with a key's passphrase: %v", args[0], err)
		}
	}
	t.Setenv("ROKH_PASSPHRASE", bondPass)
	if after := openForTest(t, dir, bondPass).Vessel().Info().Generation; after != before {
		t.Fatalf("a refused keyring change committed: generation %d, then %d", before, after)
	}
}
