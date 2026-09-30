package keyview

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/vessel"
)

// Step 1 of T2: opening and sealing judge the owner's generations at the
// record's own point, an event at its parents (contract E3, E4, 4.4).
// These tests build a carrier in memory whose owner rotates, and read it
// back through the layer every door reads through.

const fixtureIter = 1000

// The test is the host here: it gives the core its randomness (C5).
func init() {
	if event.Entropy == nil {
		event.Entropy = rand.Reader
	}
}

type holds struct{}

func (holds) Holds() error { return nil }

// rotation is a carrier in memory: its owner's first generation reads with
// o1 (the owner's cell), and every event is sealed by hand to the readers
// the test names.
type rotation struct {
	t      *testing.T
	c      *carrier.Carrier
	root   ed25519.PrivateKey
	anchor frame.ID
	o1     key.Reader
	sys    key.Reader
	vk     []byte
	head   frame.ID
	gen1   frame.ID // the owner's first add
}

func newRotation(t *testing.T) *rotation {
	t.Helper()
	f := &rotation{t: t}
	var err error
	_, f.root, _ = ed25519.GenerateKey(rand.Reader)
	if f.o1, err = key.NewReader(rand.Reader); err != nil {
		t.Fatal(err)
	}
	if f.sys, err = key.NewReader(rand.Reader); err != nil {
		t.Fatal(err)
	}
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("a synthetic rokh")}, f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.anchor = gen.ID
	f.vk = make([]byte, 32)
	salt := make([]byte, 32)
	rand.Read(f.vk)
	rand.Read(salt)
	kk, err := key.PassKey("synthetic owner passphrase", salt, fixtureIter)
	if err != nil {
		t.Fatal(err)
	}
	sec := key.Secret{Gen: 1}
	copy(sec.SignerSeed[:], f.root.Seed())
	copy(sec.Reader[:], f.o1.Bytes())
	cell, err := key.Cell(kk, sec, f.vk, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.c, err = carrier.Create(vessel.NewMemory(), vessel.Params{SlabLog2: 18, Slabs: 16, Iter: fixtureIter, Salt: salt, Rand: rand.Reader},
		f.vk, [][]byte{cell}, gen.ID, frame.Zero, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.put(gen, f.o1.Public(), f.sys.Public())
	sysSeal, err := key.SealTo(f.o1.Public(), f.sys.Bytes(), key.InfoSystem, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.gen1 = f.system(event.Keyring{Op: event.KeyringAdd, Gen: 1, Name: "owner", Reader: f.o1.Public(),
		Signer: f.root.Public().(ed25519.PublicKey), Reads: []string{""}, System: sysSeal}, f.o1.Public(), f.sys.Public())
	return f
}

// sign signs an event on the present head.
func (f *rotation) sign(address, verb string, payload []byte) event.Signed {
	f.t.Helper()
	e, err := event.SignFresh(event.Event{Carrier: &f.anchor, Parents: []frame.ID{f.head}, Address: address, Verb: verb, Payload: payload}, f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

// put records an event sealed to exactly the readers given, and moves main.
func (f *rotation) put(e event.Signed, readers ...[]byte) frame.ID {
	f.t.Helper()
	env, err := key.SealReaders(key.TypeEvent, e.ID, nil, readers, e.Body, rand.Reader)
	if err != nil {
		f.t.Fatal(err)
	}
	rec, err := f.c.Begin(holds{})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := rec.EventEnvelope(e.ID, e.Head, env); err != nil {
		f.t.Fatal(err)
	}
	if err := rec.SetRef("main", e.ID); err != nil {
		f.t.Fatal(err)
	}
	if out, err := rec.Commit(); out != vessel.Recorded {
		f.t.Fatalf("commit: %s %v", out, err)
	}
	f.head = e.ID
	return e.ID
}

// note records a note at an address, sealed to the readers given.
func (f *rotation) note(address string, readers ...[]byte) frame.ID {
	return f.put(f.sign(address, "note", []byte("a synthetic line at "+address)), readers...)
}

// system records a keyring change, sealed to the readers given.
func (f *rotation) system(k event.Keyring, readers ...[]byte) frame.ID {
	f.t.Helper()
	p, err := k.Encode()
	if err != nil {
		f.t.Fatal(err)
	}
	return f.put(f.sign(event.AddressRoot, event.VerbKeyring, p), readers...)
}

// rotate adds the owner's second generation (reader o2, the first reader
// sealed to it as heir) at a point where the first is live, then takes the
// first back.
func (f *rotation) rotate(o2 key.Reader) (add, revoke frame.ID) {
	f.t.Helper()
	heir, err := key.SealTo(o2.Public(), f.o1.Bytes(), key.InfoHeir, rand.Reader)
	if err != nil {
		f.t.Fatal(err)
	}
	sysSeal, err := key.SealTo(o2.Public(), f.sys.Bytes(), key.InfoSystem, rand.Reader)
	if err != nil {
		f.t.Fatal(err)
	}
	// At the add's point only the first generation is live.
	add = f.system(event.Keyring{Op: event.KeyringAdd, Gen: 2, Name: "owner", Reader: o2.Public(),
		Signer: f.root.Public().(ed25519.PublicKey), Reads: []string{""}, Heir: heir, System: sysSeal},
		f.o1.Public(), f.sys.Public())
	// At the revoke's point both are live.
	revoke = f.system(event.Keyring{Op: event.KeyringRevoke, Gen: 1, Target: f.gen1},
		f.o1.Public(), o2.Public(), f.sys.Public())
	return add, revoke
}

func whole(t *testing.T, l *ledger.Ledger, id frame.ID) bool {
	t.Helper()
	e, ok := l.Get(id)
	if !ok || l.State(id) != ledger.Accepted {
		t.Fatalf("%s is not accepted in this view (%v)", id.Short(), l.State(id))
	}
	return !e.HeadOnly
}

// The owner rotates its reader. Read afterwards by the owner, who holds the
// new reader and the old one through heir, every record opens at its own
// point: what was sealed before the rotation names the owner of its day and
// opens; what was sealed after names the new owner and opens; an envelope
// sealed after the rotation to the old owner alone does not name the owner
// of its point and stays a head. This is what a review's probe asks of the
// product, with the point given (key TestE3IsJudgedAtTheRecordsOwnPoint
// asks the same of the key package).
func TestAfterTheOwnerRotatesEveryRecordOpensAtItsOwnPoint(t *testing.T) {
	f := newRotation(t)
	o2, _ := key.NewReader(rand.Reader)
	before := f.note("journal/before", f.o1.Public())
	add, revoke := f.rotate(o2)
	after := f.note("journal/after", o2.Public())
	stale := f.note("journal/stale", f.o1.Public())

	l, err := Load(f.c, []key.Reader{o2, f.o1}, f.o1.Public())
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]frame.ID{"before the rotation": before, "the second generation's add": add,
		"the revoke of the first": revoke, "after the rotation": after} {
		if !whole(t, l, id) {
			t.Errorf("the record %s does not open at its own point", name)
		}
	}
	if whole(t, l, stale) {
		t.Error("an envelope sealed after the rotation to the revoked owner alone opened (E3)")
	}
	owners, err := OwnersAtEvent(l, before, f.o1.Public())
	if err != nil || len(owners) != 1 || !bytes.Equal(owners[0], f.o1.Public()) {
		t.Errorf("the owners before the rotation: %v %v", owners, err)
	}
	owners, err = OwnersAtEvent(l, after, f.o1.Public())
	if err != nil || len(owners) != 1 || !bytes.Equal(owners[0], o2.Public()) {
		t.Errorf("the owners after the rotation: %v %v", owners, err)
	}
	// "Any owner that ever lived" is not the rule: the view that holds the old
	// reader alone opens what was sealed to it at its own point, and not the
	// stale envelope, whose point names only the new owner.
	lo, err := Load(f.c, []key.Reader{f.o1}, f.o1.Public())
	if err != nil {
		t.Fatal(err)
	}
	if !whole(t, lo, before) {
		t.Error("the old reader does not open what was sealed to it before the rotation")
	}
	if whole(t, lo, stale) {
		t.Error("the old reader opened a record sealed after the rotation that does not name the owner of its point")
	}
}

// An owner fold emptied by revocation is known and empty: nothing sealed at
// such a point opens, and the owner's first generation does not stand in.
func TestAnEmptiedOwnerFoldOpensNothing(t *testing.T) {
	f := newRotation(t)
	inside := f.note("journal/inside", f.o1.Public())
	f.system(event.Keyring{Op: event.KeyringRevoke, Gen: 1, Target: f.gen1}, f.o1.Public(), f.sys.Public())
	orphan := f.note("journal/orphan", f.o1.Public())
	l, err := Load(f.c, []key.Reader{f.o1}, f.o1.Public())
	if err != nil {
		t.Fatal(err)
	}
	if !whole(t, l, inside) {
		t.Error("a record at a point with a live owner does not open")
	}
	if whole(t, l, orphan) {
		t.Error("a record at an owner fold emptied by revocation opened; the first generation stood in for it")
	}
	if _, err := OwnersAtEvent(l, orphan, f.o1.Public()); !errors.Is(err, ErrOwnerEmptied) {
		t.Errorf("the emptied point answered %v", err)
	}
}

// A point the ledger does not hold is unknown: a record resting on a head
// that is not here is not opened, and nothing stands in for its point.
func TestAnUnknownPointOpensNothing(t *testing.T) {
	f := newRotation(t)
	missing := frame.Hash([]byte("a parent that is not on this carrier"))
	e, err := event.SignFresh(event.Event{Carrier: &f.anchor, Parents: []frame.ID{missing}, Address: "journal/lost", Verb: "note",
		Payload: []byte("resting on nothing here")}, f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.put(e, f.o1.Public())
	l, err := Load(f.c, []key.Reader{f.o1}, f.o1.Public())
	if err != nil {
		t.Fatal(err)
	}
	if l.State(e.ID) == ledger.Accepted {
		t.Fatal("a record whose point is not here was accepted")
	}
	if got, ok := l.Get(e.ID); ok && !got.HeadOnly {
		t.Error("a record whose point is not here was opened")
	}
	if _, err := OwnersAtPoint(l, []frame.ID{missing}, f.o1.Public()); !errors.Is(err, key.ErrOwnerUnknown) {
		t.Errorf("an unknown point answered %v", err)
	}
}

// The owner seals at the record's own point: to the owner generations live
// at its parents, the old one before the rotation and the new one after,
// never by the keyring as it was when the session opened.
func TestTheOwnerSealsAtTheRecordsOwnPoint(t *testing.T) {
	f := newRotation(t)
	before := f.note("journal/before", f.o1.Public())
	sess, sec, k, err := OwnerSessionOf(f.c, f.secret(f.o1))
	if err != nil {
		t.Fatal(err)
	}
	_ = sess
	o2, _ := key.NewReader(rand.Reader)
	_, revoke := f.rotate(o2)
	// The writer's ledger, read now with both readers.
	l, err := Load(f.c, []key.Reader{o2, f.o1}, f.o1.Public())
	if err != nil {
		t.Fatal(err)
	}
	names := func(parents []frame.ID) (o1, o2Named, sys bool) {
		t.Helper()
		s, err := k.OwnerSessionAt(l, sec, f.c.Vessel().SharedKey(), parents)
		if err != nil {
			t.Fatalf("no session at %v: %v", parents, err)
		}
		id := frame.Hash([]byte("a record about to be signed"))
		env, err := s.Seal(key.TypeEvent, id, event.AddressRoot, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		kids, err := key.Kids(env)
		if err != nil {
			t.Fatal(err)
		}
		for _, kid := range kids {
			switch kid {
			case f.o1.Kid():
				o1 = true
			case o2.Kid():
				o2Named = true
			case f.sys.Kid():
				sys = true
			}
		}
		return
	}
	if a, b, s := names([]frame.ID{before}); !a || b || !s {
		t.Errorf("at a point before the rotation the record names o1=%v o2=%v system=%v", a, b, s)
	}
	if a, b, s := names([]frame.ID{revoke}); a || !b || !s {
		t.Errorf("at a point after the rotation the record names o1=%v o2=%v system=%v", a, b, s)
	}
	// A point the writer's ledger does not hold seals nothing.
	if _, err := k.OwnerSessionAt(l, sec, f.c.Vessel().SharedKey(), []frame.ID{frame.Hash([]byte("elsewhere"))}); !errors.Is(err, key.ErrOwnerUnknown) {
		t.Errorf("an unknown point gave a session: %v", err)
	}
}

// secret is the owner's secret for a reader, as the owner's cell holds it.
func (f *rotation) secret(r key.Reader) key.Secret {
	sec := key.Secret{Gen: 1}
	copy(sec.SignerSeed[:], f.root.Seed())
	copy(sec.Reader[:], r.Bytes())
	return sec
}

// A key's own session reads a carrier whose owner rotated: what was sealed
// to it before the rotation and after both open, each at its own point.
// Before T2 the key's opening read the ledger with every owner generation
// ever named at once, so a record that named only the owner of its own day
// was a head, and the owner's first add with it: the key's view stopped at
// the genesis.
func TestAKeysSessionReadsAcrossAnOwnerRotation(t *testing.T) {
	f := newRotation(t)
	reader, _ := key.NewReader(rand.Reader)
	kid := [32]byte{0x42}
	sysSeal, err := key.SealTo(reader.Public(), f.sys.Bytes(), key.InfoSystem, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// The key's add, sealed to the owner of its point, the system reader and
	// the key itself (its own envelope, E2).
	f.system(event.Keyring{Op: event.KeyringAdd, Key: kid, Gen: 1, Name: "reader", Reader: reader.Public(),
		Reads: []string{"journal"}, System: sysSeal}, f.o1.Public(), f.sys.Public(), reader.Public())
	before := f.note("journal/before", f.o1.Public(), reader.Public())
	private := f.note("private/before", f.o1.Public())
	o2, _ := key.NewReader(rand.Reader)
	f.rotate(o2)
	after := f.note("journal/after", o2.Public(), reader.Public())

	sec := key.Secret{Key: kid, Gen: 1}
	copy(sec.Reader[:], reader.Bytes())
	_, k, err := KeySessionOf(f.c, sec)
	if err != nil {
		t.Fatalf("the key's session does not open across the owner's rotation: %v", err)
	}
	l := k.Ledger()
	if !whole(t, l, before) {
		t.Error("the key does not read what was sealed to it before the owner rotated")
	}
	if !whole(t, l, after) {
		t.Error("the key does not read what was sealed to it after the owner rotated")
	}
	if whole(t, l, private) {
		t.Error("the key read what lies outside its view")
	}
	if k.Judge(after) != "lineage" {
		t.Errorf("a key's view judged %q, not lineage", k.Judge(after))
	}
}
