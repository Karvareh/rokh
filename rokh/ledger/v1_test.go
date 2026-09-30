package ledger

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"rokh/event"
	"rokh/frame"
)

// v1 (contract 3.3, 3.4, 4.2 to 4.7): the head and the body, the keyring, the
// seed, the open address, and the three verdicts.

func (w *world) keyring(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID, k event.Keyring) event.Signed {
	w.t.Helper()
	p, err := k.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.write(priv, auth, parents, event.AddressRoot, event.VerbKeyring, p)
}

func (w *world) seed(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID, s event.Seed) event.Signed {
	w.t.Helper()
	p, err := s.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.write(priv, auth, parents, event.AddressRoot, event.VerbSeed, p)
}

func keyID(b byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = b
	}
	return k
}

func addFor(key [32]byte, gen uint32, name string, signer ed25519.PublicKey, reads []string) event.Keyring {
	return event.Keyring{Op: event.KeyringAdd, Key: key, Gen: gen, Name: name,
		Reader: bytes.Repeat([]byte{byte(gen)}, event.ReaderSize), Signer: signer, Reads: reads}
}

func TestKeyringAndSeedCodecsRoundTrip(t *testing.T) {
	pub, _ := newKey(t)
	k := addFor(keyID(7), 2, "reader", pub, []string{"home/journal", "work"})
	b, err := k.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := event.DecodeKeyring(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != k.Key || got.Gen != 2 || got.Name != "reader" || !bytes.Equal(got.Signer, pub) ||
		len(got.Reads) != 2 || !got.Covers("home/journal/x") || got.Covers("home") {
		t.Fatalf("keyring did not round-trip: %+v", got)
	}
	everything := addFor(keyID(8), 1, "all", nil, []string{""})
	b, _ = everything.Encode()
	got, err = event.DecodeKeyring(b)
	if err != nil || !got.Covers("any/where") || got.Signer != nil {
		t.Fatalf("one empty prefix must read everything and no signer means no writing: %+v %v", got, err)
	}
	nothing := addFor(keyID(9), 1, "none", nil, nil)
	b, _ = nothing.Encode()
	if got, _ = event.DecodeKeyring(b); got.Covers("any") {
		t.Fatal("absent reads must read nothing")
	}
	if _, err := (event.Keyring{Op: event.KeyringAdd, Key: keyID(1), Gen: 0, Name: "x", Reader: make([]byte, 32)}).Encode(); err == nil {
		t.Fatal("generation 0 accepted")
	}
	if _, err := (event.Keyring{Op: event.KeyringRevoke, Key: keyID(1), Gen: 1}).Encode(); err == nil {
		t.Fatal("a revoke without a target accepted")
	}
	s := event.Seed{Op: event.SeedGive, Seed: keyID(3), Key: keyID(4), Scopes: []string{"home"}}
	b, err = s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	gs, err := event.DecodeSeed(b)
	if err != nil || gs.Seed != s.Seed || gs.Key != s.Key || !gs.Covers("home/x") || gs.Covers("work") {
		t.Fatalf("seed did not round-trip: %+v %v", gs, err)
	}
	if _, err := (event.Seed{Op: event.SeedTake, Seed: keyID(3), Key: keyID(4)}).Encode(); err == nil {
		t.Fatal("a take without its give and vessel accepted")
	}
}

func TestAnOpenGrantHasNoSubjectAndOnlyTheRootWritesIt(t *testing.T) {
	pub, _ := newKey(t)
	if _, err := (event.Grant{Open: true, Subject: pub}).Encode(); err == nil {
		t.Fatal("an open grant with a subject was accepted")
	}
	if _, err := (event.Grant{Open: true, CanDelegate: true}).Encode(); err == nil {
		t.Fatal("an open grant that delegates was accepted")
	}
	if _, err := (event.Grant{Subject: pub, Read: true}).Encode(); err == nil {
		t.Fatal("a read-open grant that is not open was accepted")
	}
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	g := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, true)
	must(t, l, g, Accepted, "a delegating grant")
	open, err := event.Grant{Open: true, Scope: "public"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	byDelegate := w.write(kPriv, &g.ID, []frame.ID{g.ID}, event.AddressRoot, event.VerbGrant, open)
	must(t, l, byDelegate, Rejected, "only the root writes an open grant")
}

func TestAStrangerWritesOnlyAtAnOpenAddress(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	p, err := event.Grant{Open: true, Scope: "public", Read: true}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	og := w.write(w.rootPriv, nil, []frame.ID{w.gen.ID}, event.AddressRoot, event.VerbGrant, p)
	must(t, l, og, Accepted, "the root's open grant")
	_, stranger := newKey(t)
	inside := w.write(stranger, &og.ID, []frame.ID{og.ID}, "public/board", "note", []byte("hello"))
	must(t, l, inside, Accepted, "a stranger inside the open address")
	outside := w.write(stranger, &og.ID, []frame.ID{og.ID}, "private", "note", []byte("hello"))
	must(t, l, outside, Rejected, "a stranger outside the open address")
	unnamed := w.write(stranger, nil, []frame.ID{og.ID}, "public/board", "note", []byte("no grant named"))
	must(t, l, unnamed, Rejected, "a stranger who names no grant")
	kr := addFor(keyID(5), 1, "sneak", stranger.Public().(ed25519.PublicKey), []string{""})
	reserved := w.keyring(stranger, &og.ID, []frame.ID{og.ID}, kr)
	must(t, l, reserved, Rejected, "a reserved verb under an open grant")
	if got := l.OpenGrants(); len(got) != 1 || got[0] != og.ID {
		t.Fatalf("open grants: %v", got)
	}
	rv := w.revoke(w.rootPriv, nil, []frame.ID{inside.ID}, og.ID)
	must(t, l, rv, Accepted, "the root closes the open address")
	after := w.write(stranger, &og.ID, []frame.ID{rv.ID}, "public/board", "note", []byte("too late"))
	must(t, l, after, Rejected, "a stranger after the open address closed")
}

func TestOnlyTheRootAddsAKeyAndAKeyMayRevokeItsOwnGeneration(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	g := w.grant([]frame.ID{w.gen.ID}, kPub, "work", nil, false)
	must(t, l, g, Accepted, "grant")
	byKey := w.keyring(kPriv, &g.ID, []frame.ID{g.ID}, addFor(keyID(1), 1, "self", kPub, []string{"work"}))
	must(t, l, byKey, Rejected, "K1: a key adds to the keyring")
	add := w.keyring(w.rootPriv, nil, []frame.ID{g.ID}, addFor(keyID(1), 1, "worker", kPub, []string{"work"}))
	must(t, l, add, Accepted, "K1: the root adds a key")
	if adds, _ := l.Keyring(); len(adds) != 1 || adds[0] != add.ID {
		t.Fatalf("live keyring: %v", adds)
	}
	_, otherPriv := newKey(t)
	og := w.grant([]frame.ID{add.ID}, otherPriv.Public().(ed25519.PublicKey), "", nil, false)
	must(t, l, og, Accepted, "a grant to another key")
	byOther := w.keyring(otherPriv, &og.ID, []frame.ID{og.ID},
		event.Keyring{Op: event.KeyringRevoke, Key: keyID(1), Gen: 1, Target: add.ID})
	must(t, l, byOther, Rejected, "K1: another key revokes a generation not its own")
	self := w.keyring(kPriv, &g.ID, []frame.ID{og.ID},
		event.Keyring{Op: event.KeyringRevoke, Key: keyID(1), Gen: 1, Target: add.ID})
	must(t, l, self, Accepted, "K1: a key revokes its own generation")
	if adds, _ := l.Keyring(); len(adds) != 0 {
		t.Fatalf("a revoked generation is still live: %v", adds)
	}
	again := w.keyring(w.rootPriv, nil, []frame.ID{self.ID},
		event.Keyring{Op: event.KeyringRevoke, Key: keyID(1), Gen: 2, Target: add.ID})
	must(t, l, again, Rejected, "a revoke whose generation is not the add's")
}

func TestConcurrentAddsBothStayLiveInEitherOrder(t *testing.T) {
	w := newWorld(t)
	a := w.keyring(w.rootPriv, nil, []frame.ID{w.gen.ID}, addFor(keyID(2), 1, "phone", nil, []string{"notes"}))
	b := w.keyring(w.rootPriv, nil, []frame.ID{w.gen.ID}, addFor(keyID(2), 2, "phone", nil, []string{"mail", "notes"}))
	for _, order := range [][]event.Signed{{a, b}, {b, a}} {
		l := w.ledger()
		for _, e := range order {
			must(t, l, e, Accepted, "concurrent add")
		}
		adds, _ := l.Keyring()
		if len(adds) != 2 {
			t.Fatalf("concurrent adds of one key must both be live (concurrent): %v", adds)
		}
		m := w.write(w.rootPriv, nil, []frame.ID{a.ID, b.ID}, event.AddressRoot, event.VerbMerge, nil)
		must(t, l, m, Accepted, "merge")
		rv := w.keyring(w.rootPriv, nil, []frame.ID{m.ID},
			event.Keyring{Op: event.KeyringRevoke, Key: keyID(2), Gen: 1, Target: a.ID})
		must(t, l, rv, Accepted, "the owner revokes all but one")
		if adds, _ := l.Keyring(); len(adds) != 1 || adds[0] != b.ID {
			t.Fatalf("after the revoke: %v", adds)
		}
	}
}

func TestOnlyTheSeedsKeyTakesTheSeedTheRootGave(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	add := w.keyring(w.rootPriv, nil, []frame.ID{w.gen.ID}, addFor(keyID(6), 1, "seed", kPub, []string{"trip"}))
	must(t, l, add, Accepted, "the seed's key")
	g := w.grant([]frame.ID{add.ID}, kPub, "trip", nil, false)
	must(t, l, g, Accepted, "the seed's grant")
	give := w.seed(w.rootPriv, nil, []frame.ID{g.ID}, event.Seed{Op: event.SeedGive, Seed: keyID(0x51), Key: keyID(6), Scopes: []string{"trip"}})
	must(t, l, give, Accepted, "S1: the root gives")
	byKey := w.seed(kPriv, &g.ID, []frame.ID{g.ID}, event.Seed{Op: event.SeedGive, Seed: keyID(0x52), Key: keyID(6)})
	must(t, l, byKey, Rejected, "S1: a key gives")
	take := w.seed(kPriv, &g.ID, []frame.ID{give.ID}, event.Seed{Op: event.SeedTake, Seed: keyID(0x51), Key: keyID(6), Give: give.ID, Vessel: keyID(0x77)})
	must(t, l, take, Accepted, "S1: the seed's key takes")
	oPub, oPriv := newKey(t)
	og := w.grant([]frame.ID{give.ID}, oPub, "", nil, false)
	must(t, l, og, Accepted, "a grant to another key")
	byOther := w.seed(oPriv, &og.ID, []frame.ID{og.ID}, event.Seed{Op: event.SeedTake, Seed: keyID(0x51), Key: keyID(6), Give: give.ID, Vessel: keyID(0x78)})
	must(t, l, byOther, Rejected, "S1: another key takes")
	early := w.seed(kPriv, &g.ID, []frame.ID{g.ID}, event.Seed{Op: event.SeedTake, Seed: keyID(0x51), Key: keyID(6), Give: give.ID, Vessel: keyID(0x79)})
	must(t, l, early, Rejected, "S1: a take whose give is not in its past")
}

// A head only is lineage: the author and the grant are proven, what was done
// is not. Everything written on it is judged lineage, never full.
func TestAHeadOnlyAncestorMakesTheVerdictLineage(t *testing.T) {
	w := newWorld(t)
	l := w.ledger()
	kPub, kPriv := newKey(t)
	g := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	must(t, l, g, Accepted, "grant")
	secret := w.write(kPriv, &g.ID, []frame.ID{g.ID}, "private/diary", "note", []byte("not for you"))
	head := secret.WithoutBody()
	if !head.HeadOnly || len(head.Raw) != len(secret.Head) {
		t.Fatal("WithoutBody did not leave the head only")
	}
	parsed, err := event.Parse(head.Raw)
	if err != nil || !parsed.HeadOnly || parsed.ID != secret.ID || parsed.Event.Address != "" {
		t.Fatalf("a head only did not parse as one: %v %+v", err, parsed)
	}
	must(t, l, parsed, Accepted, "a head only on a live grant")
	if l.Judged(secret.ID) != Lineage {
		t.Fatalf("a head only is %s, want lineage", l.Judged(secret.ID))
	}
	child := w.write(kPriv, &g.ID, []frame.ID{secret.ID}, "public", "note", []byte("on top"))
	must(t, l, child, Accepted, "a whole event on a head only")
	if l.Judged(child.ID) != Lineage {
		t.Fatalf("an event with a head-only ancestor is %s, want lineage", l.Judged(child.ID))
	}
	if l.Judged(g.ID) != Full {
		t.Fatalf("a whole history is %s, want full", l.Judged(g.ID))
	}
	// The body arriving later is the same event.
	if st, err := l.Add(secret.Raw); err != nil || st != Accepted {
		t.Fatalf("the body of a held head: %v %v", st, err)
	}
	if got, _ := l.Get(secret.ID); got.HeadOnly {
		t.Fatal("the arriving body was not kept")
	}
}

// S1 (contract C8): a forged record header, a missing head, a head that does
// not hash to its name and a hidden revoke each end pending, and a write on
// top of them is refused.
func TestUnprovenAncestryIsPendingAndAuthorizesNothing(t *testing.T) {
	w := newWorld(t)
	kPub, kPriv := newKey(t)
	g := w.grant([]frame.ID{w.gen.ID}, kPub, "", nil, false)
	a := w.write(kPriv, &g.ID, []frame.ID{g.ID}, "notes", "note", []byte("a"))
	b := w.write(kPriv, &g.ID, []frame.ID{a.ID}, "notes", "note", []byte("b"))
	store := map[frame.ID][]byte{w.gen.ID: w.gen.Raw, g.ID: g.Raw, a.ID: a.Raw, b.ID: b.Raw}
	get := func(id frame.ID) ([]byte, error) {
		raw, ok := store[id]
		if !ok {
			return nil, errors.New("not here")
		}
		return raw, nil
	}
	cases := map[string]func(){
		"a missing head":                     func() { delete(store, a.ID) },
		"a forged record header":             func() { store[a.ID] = g.Raw },
		"a head that does not hash its name": func() { h := append([]byte(nil), a.Raw...); h[20] ^= 1; store[a.ID] = h },
		"a body that does not hash to its head": func() {
			x := append([]byte(nil), a.Raw...)
			x[len(x)-1] ^= 1
			store[a.ID] = x
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			store = map[frame.ID][]byte{w.gen.ID: w.gen.Raw, g.ID: g.Raw, a.ID: a.Raw, b.ID: b.Raw}
			spoil()
			l, err := Load(w.gen.Raw, get, []frame.ID{b.ID})
			if err != nil {
				t.Fatal(err)
			}
			if l.State(b.ID) == Accepted || l.State(a.ID) == Accepted {
				t.Fatalf("%s: something rests on unproven ancestry: a=%s b=%s", name, l.State(a.ID), l.State(b.ID))
			}
			if len(l.Unproven()) == 0 {
				t.Fatalf("%s: the unproven head was not reported", name)
			}
			write := w.write(kPriv, &g.ID, []frame.ID{b.ID}, "notes", "note", []byte("on top of "+name))
			if st, _ := l.Add(write.Raw); st != Pending {
				t.Fatalf("%s: a write on unproven ancestry was %s, want pending (refused)", name, st)
			}
			if why, _ := l.Why(write.ID); why == "" {
				t.Fatalf("%s: a pending write has no sentence", name)
			}
		})
	}
	t.Run("a hidden revoke", func(t *testing.T) {
		l := w.ledger()
		must(t, l, g, Accepted, "grant")
		rv := w.revoke(w.rootPriv, nil, []frame.ID{g.ID}, g.ID)
		hidden, err := event.Parse(rv.WithoutBody().Raw)
		if err != nil {
			t.Fatal(err)
		}
		if st, _ := l.Add(hidden.Raw); st != Pending {
			t.Fatalf("a system event without its body was %s, want pending", st)
		}
		write := w.write(kPriv, &g.ID, []frame.ID{rv.ID}, "notes", "note", []byte("after the hidden revoke"))
		if st, _ := l.Add(write.Raw); st != Pending {
			t.Fatalf("a write after a hidden revoke was %s, want pending (refused)", st)
		}
		if st, _ := l.Add(rv.Raw); st != Accepted {
			t.Fatalf("the revoke's body arriving: %s", st)
		}
		if l.State(write.ID) != Rejected {
			t.Fatalf("once the revoke is proven the write is judged under it: %s", l.State(write.ID))
		}
	})
}

// E1: a body that does not hash to the value the signed head names is a
// forgery, and nothing of it is read.
func TestAForgedBodyIsRefused(t *testing.T) {
	w := newWorld(t)
	raw := append([]byte(nil), w.gen.Raw...)
	raw[len(raw)-3] ^= 0x40
	if _, err := event.Parse(raw); !errors.Is(err, event.ErrBody) {
		t.Fatalf("a forged body parsed: %v", err)
	}
	if !w.gen.System {
		t.Fatal("genesis sits at rokh and carries the system mark")
	}
}

// The author's side: a body that arrives after its head is judged in
// full. A body its grant does not cover is refused, and so is everything that
// rests on it; the final verdicts are those of the whole events arriving
// first, whatever the order.
func TestALateBodyIsJudgedAndArrivalOrderAuthorizesNothing(t *testing.T) {
	w := newWorld(t)
	kPub, kPriv := newKey(t)
	g := w.grant([]frame.ID{w.gen.ID}, kPub, "allowed", []string{"note"}, false)
	denied := w.write(kPriv, &g.ID, []frame.ID{g.ID}, "forbidden/private", "note", []byte("unauthorized"))
	child := w.write(kPriv, &g.ID, []frame.ID{denied.ID}, "allowed/x", "note", []byte("rests on it"))
	grand := w.write(kPriv, &g.ID, []frame.ID{child.ID}, "allowed/y", "note", []byte("and on that"))

	full := w.ledger()
	for _, e := range []event.Signed{g, denied, child, grand} {
		full.Add(e.Raw)
	}
	split := w.ledger()
	must(t, split, g, Accepted, "grant")
	must(t, split, denied.WithoutBody(), Accepted, "the head alone, on a live grant")
	must(t, split, child, Accepted, "a child of the head")
	must(t, split, grand, Accepted, "a grandchild")
	if split.Judged(child.ID) != Lineage {
		t.Fatal("a child of a head only is lineage")
	}
	st, err := split.Add(denied.Raw)
	if st != Rejected || err == nil {
		t.Fatalf("the late unauthorized body: %v %v", st, err)
	}
	if _, held := split.Get(denied.ID); held {
		t.Fatal("the refused body is still held and readable")
	}
	for _, e := range []event.Signed{g, denied, child, grand} {
		if full.State(e.ID) != split.State(e.ID) {
			t.Fatalf("%s: whole-first says %s, head-first says %s", e.ID.Short(), full.State(e.ID), split.State(e.ID))
		}
	}
	if why, _ := split.Why(grand.ID); why == "" {
		t.Fatal("a refused descendant has no sentence")
	}
	if heads := split.Heads(); len(heads) != 1 || heads[0] != g.ID {
		t.Fatalf("heads after the refusal: %v", heads)
	}
	if acc, rej, pen := split.Tally(); acc != 2 || rej != 3 || pen != 0 {
		t.Fatalf("tally after the refusal: %d %d %d", acc, rej, pen)
	}
	later := w.write(kPriv, &g.ID, []frame.ID{grand.ID}, "allowed/z", "note", []byte("after the break"))
	if st, _ := split.Add(later.Raw); st != Rejected {
		t.Fatalf("a write on a refused chain: %s", st)
	}
}

// A late body its grant covers is kept, and the levels rise: the event and
// what rests on it become Full once every ancestor is.
func TestALateValidBodyRaisesTheLevels(t *testing.T) {
	w := newWorld(t)
	kPub, kPriv := newKey(t)
	g := w.grant([]frame.ID{w.gen.ID}, kPub, "notes", nil, false)
	a := w.write(kPriv, &g.ID, []frame.ID{g.ID}, "notes/a", "note", []byte("a"))
	b := w.write(kPriv, &g.ID, []frame.ID{a.ID}, "notes/b", "note", []byte("b"))
	l := w.ledger()
	must(t, l, g, Accepted, "grant")
	must(t, l, a.WithoutBody(), Accepted, "head only")
	must(t, l, b, Accepted, "child")
	if l.Judged(b.ID) != Lineage {
		t.Fatal("before the body: lineage")
	}
	if st, err := l.Add(a.Raw); st != Accepted || err != nil {
		t.Fatalf("the late valid body: %v %v", st, err)
	}
	if l.Judged(a.ID) != Full || l.Judged(b.ID) != Full {
		t.Fatalf("after the body: %s %s, want full full", l.Judged(a.ID), l.Judged(b.ID))
	}
	if got, _ := l.Get(a.ID); got.HeadOnly || string(got.Event.Payload) != "a" {
		t.Fatal("the valid body was not kept")
	}
}

// R3: a prefix list has one spelling, ascending byte order, on encode and on
// decode.
func TestPrefixListsHaveOneSpelling(t *testing.T) {
	if _, err := (event.Keyring{Op: event.KeyringAdd, Key: keyID(1), Gen: 1, Name: "k", Reader: make([]byte, 32),
		Reads: []string{"work", "journal"}}).Encode(); err == nil {
		t.Fatal("reads out of order were encoded")
	}
	if _, err := (event.Seed{Op: event.SeedGive, Seed: keyID(2), Key: keyID(3), Scopes: []string{"b", "a"}}).Encode(); err == nil {
		t.Fatal("scopes out of order were encoded")
	}
	good, err := (event.Keyring{Op: event.KeyringAdd, Key: keyID(1), Gen: 1, Name: "k", Reader: make([]byte, 32),
		Reads: []string{"journal", "work"}}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	// Swap the two prefixes in place: same bytes, other order.
	i := bytes.Index(good, []byte("\x07journal\x04work"))
	if i < 0 {
		t.Fatal("the prefix list was not found in the encoding")
	}
	swapped := append([]byte(nil), good...)
	copy(swapped[i:], []byte("\x04work\x07journal"))
	if _, err := event.DecodeKeyring(swapped); err == nil {
		t.Fatal("reads out of order were decoded")
	}
}
