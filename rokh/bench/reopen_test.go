package bench

import (
	"crypto/rand"
	"fmt"
	"testing"

	"rokh/carrier"
	"rokh/daemon"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/medium"
	"rokh/vessel"
)

// After a carrier is reopened, a door with the same root and the same
// delegated key writes again, with and without an attempt name. Found by the
// bench: the write with an attempt answered "signature does not verify",
// because the fixture handed the door its own key, which the door wipes
// . The fixture gives copies now.
func TestAReopenedDoorWritesWithItsDelegatedKey(t *testing.T) {
	dir := t.TempDir()
	_, _, root, clerk := build(t, dir, 3)
	c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock("pass")), rand.Reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := c.Vessel()
	info := v.Info()
	sess, _, err := key.VesselSession("pass", v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c.SetSealer(sess)
	refs, _ := c.Refs()
	var heads []frame.ID
	for _, id := range refs {
		heads = append(heads, id)
	}
	raw, _ := c.Get(c.Anchor())
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	s := daemon.New(c, l, daemon.Options{AllowSign: true, Dir: dir, Root: own(root), Keys: clerk})
	for i, line := range []string{
		`{"op":"write","address":"home/journal","verb":"note","message":"after reopen","key":"clerk"}`,
		`{"op":"write","address":"home/journal","verb":"note","message":"after reopen, named","key":"clerk","attempt":"z-1"}`,
		`{"op":"write","address":"home/journal","verb":"note","message":"by the root"}`,
	} {
		r := s.Handle([]byte(line))
		t.Logf("write %d after reopen: %v", i, r)
		if r["ok"] != true {
			t.Errorf("write %d after reopen was refused: %s", i, fmt.Sprint(r["error"]))
		}
	}
}

// Which read before the write leads to "signature does not verify"? Each read
// op of the bench is run alone on a fresh reopened door, then one named
// delegated write.
func TestWhichReadBeforeAWriteBreaksIt(t *testing.T) {
	for _, read := range []string{
		`{"op":"status"}`,
		`{"op":"log","limit":50}`,
		`{"op":"log"}`,
		`{"op":"receipts","address":"home"}`,
	} {
		dir := t.TempDir()
		_, _, root, clerk := build(t, dir, 3)
		c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock("pass")), rand.Reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		v := c.Vessel()
		info := v.Info()
		sess, _, _ := key.VesselSession("pass", v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
		c.SetSealer(sess)
		refs, _ := c.Refs()
		var heads []frame.ID
		for _, id := range refs {
			heads = append(heads, id)
		}
		raw, _ := c.Get(c.Anchor())
		l, _ := ledger.Load(raw, c.Get, heads)
		s := daemon.New(c, l, daemon.Options{AllowSign: true, Dir: dir, Root: own(root), Keys: clerk})
		s.Handle([]byte(read))
		r := s.Handle([]byte(`{"op":"write","address":"home/journal","verb":"note","message":"x","key":"clerk","attempt":"z-1"}`))
		t.Logf("after %s: %v", read, r)
		if r["ok"] != true {
			t.Errorf("after %s a named delegated write was refused: %v", read, r["error"])
		}
	}
}
