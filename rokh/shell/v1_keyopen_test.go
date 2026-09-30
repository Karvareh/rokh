package shell

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/turn"
	"rokh/vessel"
)

// T1 step 3: the sentence surface opens a carrier with a key's own
// passphrase the way the command line does, and holds that key's view.

// keySpec is one key of a keyed vault.
type keySpec struct {
	name, pass string
	reads      []string
	write      bool
	scope      string
}

// keyedVault is a vault whose one ledger is made the way rokh init and rokh
// key add make one (cmd/rokh): the genesis and the owner's first keyring
// generation in one commit, sealed to the owner and to a system reader whose
// private half that add seals to the owner (0x000B); then, for each key, its
// add signed by the root and sealed by the owner's session, the key's cell
// with an envelope of the add sealed to the owner and the key, the system
// reader sealed to the key, and for a writer a grant to its signer. The owner
// then writes one line at journal and one at private through the sentences.
type keyedVault struct {
	*fixture
	dir  string
	root ed25519.PrivateKey
	adds map[string]frame.ID
}

var keyedSpecs = []keySpec{
	{name: "reader", pass: "shell reader pass", reads: []string{"journal"}},
	{name: "blind", pass: "shell blind pass"},
	{name: "writer", pass: "shell writer pass", write: true, scope: "journal"},
}

func makeKeyedVault(t *testing.T, specs []keySpec) *keyedVault {
	t.Helper()
	base := t.TempDir()
	f := &fixture{vault: filepath.Join(base, "vault"), mount: filepath.Join(base, "mount"),
		library: filepath.Join(base, "library"), name: "home"}
	for _, d := range []string{filepath.Join(f.vault, "ledgers"), f.mount, f.library} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	kv := &keyedVault{fixture: f, dir: filepath.Join(f.vault, "ledgers", f.name), adds: map[string]frame.ID{}}
	_, root, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("keyed fixture genesis")}, root)
	if err != nil {
		t.Fatal(err)
	}
	kv.root, f.rootPriv, f.genesis = root, root, gen
	if err := os.MkdirAll(kv.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c, sec, err := createCarrier(kv.dir, testPass, gen.ID, root)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		t.Fatal(err)
	}
	sys, err := key.NewReader(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := key.SealTo(owner.Public(), sys.Bytes(), key.InfoSystem, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p, err := event.Keyring{Op: event.KeyringAdd, Gen: 1, Name: "owner", Reader: owner.Public(),
		Signer: root.Public().(ed25519.PublicKey), Reads: []string{""}, System: sealed}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	anchor := gen.ID
	add, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: p}, root)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sess.PTK, sess.System = c.Vessel().SharedKey(), sys.Public()
	c.SetSealer(sess)
	kv.commit(t, c, func(r *carrier.Recording) error {
		if err := r.Event(gen.ID, gen.Head, gen.Body, gen.Event.Address); err != nil {
			return err
		}
		if err := r.Event(add.ID, add.Head, add.Body, add.Event.Address); err != nil {
			return err
		}
		return r.SetRef(defaultBranch, add.ID)
	})
	for _, sp := range specs {
		kv.addKey(t, owner, sys, sp)
	}
	s := kv.session(testPass)
	defer s.closeAll()
	run(t, s, "write at journal/today: in the view")
	run(t, s, "write")
	run(t, s, "write at private/diary: outside the view")
	run(t, s, "write")
	return kv
}

// commit makes one vessel commit on the carrier under its writer's turn.
func (kv *keyedVault) commit(t *testing.T, c *carrier.Carrier, fill func(*carrier.Recording) error) {
	t.Helper()
	lock, err := turn.Acquire(kv.dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	r, err := c.Begin(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := fill(r); err != nil {
		r.Abandon()
		t.Fatal(err)
	}
	if out, err := r.Commit(); out != vessel.Recorded {
		t.Fatalf("commit: %s %v", out, err)
	}
}

// addKey records one key as rokh key add does: the add, sealed by the
// owner's dressed session, then the key's cell with an envelope of the add of
// the key's own; then a writer's grant.
func (kv *keyedVault) addKey(t *testing.T, owner, sys key.Reader, sp keySpec) {
	t.Helper()
	c := openV1(t, kv.dir, testPass)
	head, found, err := c.Ref(defaultBranch)
	if err != nil || !found {
		t.Fatalf("main: %v %v", found, err)
	}
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	rd, err := key.NewReader(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := key.Secret{Key: id, Gen: 1}
	copy(secret.Reader[:], rd.Bytes())
	k := event.Keyring{Op: event.KeyringAdd, Key: id, Gen: 1, Name: sp.name, Reader: rd.Public(), Reads: sp.reads}
	if sp.write {
		if _, err := rand.Read(secret.SignerSeed[:]); err != nil {
			t.Fatal(err)
		}
		k.Signer = secret.Signer().Public().(ed25519.PublicKey)
	}
	info := c.Vessel().Info()
	kk, err := key.PassKey(sp.pass, info.Salt, info.Iter)
	if err != nil {
		t.Fatal(err)
	}
	if k.Slot, err = key.Blob(kk, secret, rand.Reader); err != nil {
		t.Fatal(err)
	}
	cell, err := key.CellFrom(k.Slot, secret, c.Vessel().VK(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if k.System, err = key.SealTo(rd.Public(), sys.Bytes(), key.InfoSystem, rand.Reader); err != nil {
		t.Fatal(err)
	}
	p, err := k.Encode()
	if err != nil {
		t.Fatal(err)
	}
	anchor := c.Anchor()
	add, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{head},
		Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: p}, kv.root)
	if err != nil {
		t.Fatal(err)
	}
	kv.commit(t, c, func(r *carrier.Recording) error {
		if err := r.Event(add.ID, add.Head, add.Body, add.Event.Address); err != nil {
			return err
		}
		return r.SetRef(defaultBranch, add.ID)
	})
	// The key's cell where nobody's cell is (the owner's is the first), and
	// the add's envelope of the key's own: the owner at the add's point and
	// the key.
	env, err := key.SealReaders(key.TypeEvent, add.ID, nil, [][]byte{owner.Public(), rd.Public()}, add.Body, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	slots := c.Vessel().Slots()
	slots[1+len(kv.adds)] = cell
	kv.commit(t, c, func(r *carrier.Recording) error {
		if err := r.EventEnvelope(add.ID, add.Head, env); err != nil {
			return err
		}
		r.Tx().SetSlots(slots)
		return nil
	})
	kv.adds[sp.name] = add.ID
	if !sp.write {
		return
	}
	g, err := event.Grant{Subject: k.Signer, Scope: sp.scope}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	ge, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{add.ID},
		Address: event.AddressRoot, Verb: event.VerbGrant, Payload: g}, kv.root)
	if err != nil {
		t.Fatal(err)
	}
	c = openV1(t, kv.dir, testPass)
	kv.commit(t, c, func(r *carrier.Recording) error {
		if err := r.Event(ge.ID, ge.Head, ge.Body, ge.Event.Address); err != nil {
			return err
		}
		return r.SetRef(defaultBranch, ge.ID)
	})
}

// revoke takes one key's generation back, signed by the root.
func (kv *keyedVault) revoke(t *testing.T, name string) {
	t.Helper()
	c := openV1(t, kv.dir, testPass)
	head, _, err := c.Ref(defaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	led, err := replay(c)
	if err != nil {
		t.Fatal(err)
	}
	ae, _ := led.Get(kv.adds[name])
	k, err := event.DecodeKeyring(ae.Event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	p, err := event.Keyring{Op: event.KeyringRevoke, Key: k.Key, Gen: k.Gen, Target: kv.adds[name]}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	anchor := c.Anchor()
	rv, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{head},
		Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: p}, kv.root)
	if err != nil {
		t.Fatal(err)
	}
	recordV1(t, c, kv.dir, rv, defaultBranch)
}

func (kv *keyedVault) session(pass string) *session {
	return newSession(kv.vault, kv.mount, kv.library, pass)
}

func (kv *keyedVault) generation(t *testing.T) uint64 {
	t.Helper()
	return openV1(t, kv.dir, testPass).Vessel().Info().Generation
}

// attempt runs one sentence and returns its error, for a sentence expected
// to be refused.
func attempt(t *testing.T, s *session, sentence string) error {
	t.Helper()
	c, err := parse(sentence)
	if err != nil {
		t.Fatalf("parse %q: %v", sentence, err)
	}
	_, err = s.execute(c)
	return err
}

// A key's own passphrase opens the ledger through the sentence surface, in
// that key's own view, as on the command line: a reader reads journal and
// not private; a key with no right, and a writer that reads nothing, open it
// and read no body. None of them holds the root there or drafts a write, and
// nothing is recorded.
func TestTheShellOpensAKeysOwnViewWithItsPassphrase(t *testing.T) {
	kv := makeKeyedVault(t, keyedSpecs)
	before := kv.generation(t)

	s := kv.session("shell reader pass")
	if got := run(t, s, "read journal"); !strings.Contains(got, "in the view") {
		t.Fatalf("the reader does not read journal through the shell:\n%s", got)
	}
	if got := run(t, s, "read private"); strings.Contains(got, "outside the view") {
		t.Fatalf("the reader read private through the shell:\n%s", got)
	}
	if custodyOf(s.current.sec) == "warm" {
		t.Fatal("the reader's passphrase holds the root in the shell")
	}
	if err := attempt(t, s, "write at journal/x: as the owner"); err == nil {
		t.Fatal("the reader drafted a write through the shell")
	}
	if err := s.closeAll(); err != nil {
		t.Fatal(err)
	}

	for _, pass := range []string{"shell blind pass", "shell writer pass"} {
		s := kv.session(pass)
		run(t, s, "open the ledger home")
		for _, addr := range []string{"journal", "private"} {
			if got := run(t, s, "read "+addr); strings.Contains(got, "view") {
				t.Fatalf("a key that reads nothing read %s through the shell:\n%s", addr, got)
			}
		}
		if custodyOf(s.current.sec) == "warm" {
			t.Fatal("a key's passphrase holds the root in the shell")
		}
		if err := attempt(t, s, "write at journal/x: as the owner"); err == nil {
			t.Fatal("a key's passphrase drafted a write through the shell")
		}
		if err := s.closeAll(); err != nil {
			t.Fatal(err)
		}
	}
	if after := kv.generation(t); after != before {
		t.Fatalf("a key's session in the shell committed: generation %d, then %d", before, after)
	}
}

// A revoked key opens nothing through the sentence surface; the owner still
// opens and reads.
func TestTheShellRefusesARevokedKey(t *testing.T) {
	kv := makeKeyedVault(t, keyedSpecs)
	kv.revoke(t, "reader")
	s := kv.session("shell reader pass")
	defer s.closeAll()
	if err := attempt(t, s, "open the ledger home"); err == nil || !errors.Is(err, key.ErrKeyNotLive) {
		t.Fatalf("a revoked key opened the ledger through the shell: %v", err)
	}
	o := kv.session(testPass)
	defer o.closeAll()
	if got := run(t, o, "read journal"); !strings.Contains(got, "in the view") {
		t.Fatalf("the owner no longer reads through the shell:\n%s", got)
	}
}

// The shell's opener refuses what the command line's refuses: a record at a
// point whose owner fold was emptied by revocation (R4). The owner's
// generation is revoked, then its next generation added after it, so the
// ring at the head holds an owner again and only the rule can refuse.
func TestTheShellRefusesARecordAtAnOwnerFoldEmptiedByRevocation(t *testing.T) {
	kv := makeKeyedVault(t, keyedSpecs[:1])
	if err := attempt(t, kv.session("shell reader pass"), "open the ledger home"); err != nil {
		t.Fatalf("the reader does not open before the revocation: %v", err)
	}
	c := openV1(t, kv.dir, testPass)
	led, err := replay(c)
	if err != nil {
		t.Fatal(err)
	}
	var first frame.ID
	var firstK event.Keyring
	for _, id := range led.Order() {
		e, _ := led.Get(id)
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && e.Event.Verb == event.VerbKeyring && k.Op == event.KeyringAdd && k.IsOwner() {
			first, firstK = id, k
		}
	}
	if first.IsZero() {
		t.Fatal("no owner generation")
	}
	head, _, err := c.Ref(defaultBranch)
	if err != nil {
		t.Fatal(err)
	}
	anchor := c.Anchor()
	p, _ := event.Keyring{Op: event.KeyringRevoke, Gen: 1, Target: first}.Encode()
	rv, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{head},
		Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: p}, kv.root)
	if err != nil {
		t.Fatal(err)
	}
	next := firstK
	next.Gen = 2
	p, err = next.Encode()
	if err != nil {
		t.Fatal(err)
	}
	g2, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{rv.ID},
		Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: p}, kv.root)
	if err != nil {
		t.Fatal(err)
	}
	// Both sealed to the owner and the system reader, as the owner's session
	// sealed the first generation's add.
	owner := ownerReaderOf(t, kv.dir, testPass)
	ownSess, _, _, err := keyLayerOf(openV1(t, kv.dir, testPass), testPass)
	if err != nil {
		t.Fatal(err)
	}
	readers := [][]byte{owner.Public(), ownSess.System}
	kv.commit(t, c, func(r *carrier.Recording) error {
		for _, e := range []event.Signed{rv, g2} {
			env, err := key.SealReaders(key.TypeEvent, e.ID, nil, readers, e.Body, rand.Reader)
			if err != nil {
				return err
			}
			if err := r.EventEnvelope(e.ID, e.Head, env); err != nil {
				return err
			}
		}
		return r.SetRef(defaultBranch, g2.ID)
	})
	err = attempt(t, kv.session("shell reader pass"), "open the ledger home")
	if err == nil || !errors.Is(err, key.ErrOwnerUnknown) || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("the reader opened past an owner fold emptied by revocation: %v", err)
	}
}
