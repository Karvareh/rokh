package main

import (
	"path/filepath"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/vessel"
)

// The key layer's hooks are set, and they read the carrier's own ledger: the
// judge gives the ledger's verdict, the owner check wants the owner of the
// record's own point, and a point the ledger cannot give is refused, never
// replaced by the present owner.
func TestTheKeyLayerJudgesAndCoversFromTheLedger(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	if _, _, err := bondRun(t, cmdInit, dir, "--message", "genesis"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bondRun(t, cmdWrite, dir, "--address", "journal/today", "--message", "a line"); err != nil {
		t.Fatal(err)
	}
	if v1Judge == nil || v1Sealer == nil {
		t.Fatal("the key layer's hooks are not set")
	}
	c := openForTest(t, dir, bondPass)
	judge, covers, err := v1Judge(c, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	l := ledgerOf(t, c)
	var written frame.ID
	for _, id := range l.Order() {
		if e, _ := l.Get(id); e.Event.Address == "journal/today" {
			written = id
		}
	}
	if written.IsZero() {
		t.Fatal("the written event is not in the ledger")
	}
	if got := judge(written); got != "full" {
		t.Fatalf("the judge of an accepted event: %q", got)
	}
	if got := judge(frame.Hash([]byte("no such event"))); got != "pending" {
		t.Fatalf("the judge of an unknown event: %q", got)
	}
	var env []byte
	err = c.Vessel().Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type == vessel.RecEvent && h.ID == written {
			b, err := c.Vessel().Body(ref)
			env = b
			return err
		}
		return nil
	})
	if err != nil || env == nil {
		t.Fatalf("the written event's envelope: %v", err)
	}
	if !covers(written, env) {
		t.Fatal("an envelope sealed by the owner does not cover its own point")
	}
	if covers(frame.Hash([]byte("a point nobody knows")), env) {
		t.Fatal("an unknown point was covered")
	}
	// The session the commands open is judged at each record's point.
	sl, err := v1Sealer(c, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	s := sl.(*key.Session)
	if len(s.System) == 0 {
		t.Fatal("the owner's session holds no system reader: rokh init sealed none to the owner")
	}
	if owners, known := s.OwnerAt(written); !known || len(owners) != 1 {
		t.Fatalf("the owner at the written event's point: %v %v", owners, known)
	}
	if _, known := s.OwnerAt(frame.Hash([]byte("unknown"))); known {
		t.Fatal("an unknown point was answered")
	}
	if _, err := s.Open(key.TypeEvent, written, env); err != nil {
		t.Fatalf("the owner's dressed session does not open what it wrote: %v", err)
	}
}

// R4: an owner fold emptied by revocation is
// known and empty, and refused; the cell's owner never stands in for it,
// neither at a point nor in the session's ring. The genesis, before any owner
// generation existed, still takes the cell's first generation.
func TestAnOwnerFoldEmptiedByRevocationIsRefusedNotTheCell(t *testing.T) {
	bondSetup(t)
	dir := filepath.Join(t.TempDir(), "c")
	if _, _, err := bondRun(t, cmdInit, dir, "--message", "genesis"); err != nil {
		t.Fatal(err)
	}
	s, err := openWriting(dir, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root := ownerRoot(s.sec)
	adds, _ := s.led.Keyring()
	var ownerAdd frame.ID
	for _, id := range adds {
		e, _ := s.led.Get(id)
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && k.IsOwner() {
			ownerAdd = id
		}
	}
	if ownerAdd.IsZero() {
		t.Fatal("rokh init recorded no owner generation")
	}
	anchor := s.car.Anchor()
	p, _ := event.Keyring{Op: event.KeyringRevoke, Gen: 1, Target: ownerAdd}.Encode()
	rv, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{ownerAdd}, Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: p}, root)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := s.led.Add(rv.Raw); err != nil || st != ledger.Accepted {
		t.Fatalf("the revoke of the owner's generation: %v %v", st, err)
	}
	after, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{rv.ID}, Address: "journal/after", Verb: "note", Payload: []byte("after the revoke")}, root)
	if st, err := s.led.Add(after.Raw); err != nil || st != ledger.Accepted {
		t.Fatalf("an event after the revoke: %v %v", st, err)
	}
	owner, _ := key.ReaderFrom(s.sec.Reader[:])
	cell := key.Gen{Keyring: event.Keyring{Op: event.KeyringAdd, Gen: 1, Name: "owner", Reader: owner.Public(), Reads: []string{""}}}
	k := layerOf(s.car, s.led, cell)
	if owners, known := k.ownersAt(after.ID); known {
		t.Fatalf("an owner fold emptied by revocation was answered: %v", owners)
	}
	if owners, known := k.ownersAt(anchor); !known || len(owners) != 1 {
		t.Fatalf("the genesis lost its owner: %v %v", owners, known)
	}
	sess := &key.Session{Mine: []key.Reader{owner}}
	if err := k.dress(sess); err != nil {
		t.Fatal(err)
	}
	if len(sess.Ring.Owner()) != 0 {
		t.Fatal("the cell's owner stood in for an owner fold emptied by revocation")
	}
	if _, err := sess.Seal(key.TypeEvent, after.ID, "journal/after", []byte("x")); err == nil {
		t.Fatal("a session whose owner fold is empty sealed a record")
	}
}
