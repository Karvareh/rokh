package bench

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/medium"
	"rokh/vessel"
)

// A before/after measurement harness: build a carrier with N events through
// the real daemon Handle path (delegate key, attempt names), then time reopen,
// status, log, get, and a further write with the ledger at size N.
func build(t *testing.T, dir string, n int) (*daemon.Server, frame.ID, ed25519.PrivateKey, keys) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g, err := event.SignFresh(event.Event{Author: pub, Address: "rokh", Verb: event.VerbGenesis}, priv)
	if err != nil {
		t.Fatal(err)
	}
	dpub, dpriv, _ := ed25519.GenerateKey(rand.Reader)
	gp, _ := event.Grant{Subject: dpub, Scope: "home"}.Encode()
	anchor := g.ID
	ge, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{g.ID}, Address: "rokh", Verb: event.VerbGrant, Payload: gp}, priv)
	c := createV1(t, dir, "pass", priv, g.ID, g, ge)
	l, _ := ledger.New(g.Raw)
	l.Add(ge.Raw)
	// The delegated key comes from the program that embeds the door (v1 keeps
	// no private key in a keyring file).
	clerk := keys{name: "clerk", priv: dpriv, authority: ge.ID}
	s := daemon.New(c, l, daemon.Options{AllowSign: true, Dir: dir, Root: own(priv), Keys: clerk})
	start := time.Now()
	for i := 0; i < n; i++ {
		line := fmt.Sprintf(`{"op":"write","address":"home/journal","verb":"note","message":"entry %d","key":"clerk","attempt":"a-%d"}`, i, i)
		r := s.Handle([]byte(line))
		if r["ok"] != true {
			t.Fatalf("write %d: %v", i, r)
		}
	}
	el := time.Since(start)
	t.Logf("write %d events via daemon.Handle: %v (%.1f/s)", n, el, float64(n)/el.Seconds())
	return s, g.ID, priv, clerk
}

func TestBench(t *testing.T) {
	if os.Getenv("ROKH_BENCH") == "" {
		t.Skip("set ROKH_BENCH=1 (and N, BENCH_DIR) to run the 10k measurement")
	}
	n := 10000
	if v := os.Getenv("N"); v != "" {
		fmt.Sscanf(v, "%d", &n)
	}
	dir := filepath.Join(os.Getenv("BENCH_DIR"), fmt.Sprintf("ledger-%d", n))
	if os.Getenv("BENCH_DIR") == "" {
		dir = t.TempDir()
	}
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o700)
	s, _, root, clerk := build(t, dir, n)
	_ = s
	// reopen
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	start := time.Now()
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
	heads := []frame.ID{}
	for _, id := range refs {
		heads = append(heads, id)
	}
	raw, _ := c.Get(c.Anchor())
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reopen (carrier.Open+ledger.Load) %d events: %v", l.Len(), time.Since(start))
	runtime.GC()
	runtime.ReadMemStats(&m1)
	t.Logf("heap after load: %.1f MiB (delta %.1f MiB), %.0f bytes/event", float64(m1.HeapAlloc)/1048576, float64(m1.HeapAlloc-m0.HeapAlloc)/1048576, float64(m1.HeapAlloc-m0.HeapAlloc)/float64(l.Len()))
	s2 := daemon.New(c, l, daemon.Options{AllowSign: true, Dir: dir, Root: own(root), Keys: clerk})
	tm := func(name, line string, k int) {
		start := time.Now()
		for i := 0; i < k; i++ {
			r := s2.Handle([]byte(line))
			if r["ok"] != true {
				t.Fatalf("%s: %v", name, r)
			}
		}
		t.Logf("%-28s %v each", name, time.Since(start)/time.Duration(k))
	}
	tm("status", `{"op":"status"}`, 20)
	tm("log limit=50", `{"op":"log","limit":50}`, 20)
	tm("log all", `{"op":"log"}`, 3)
	tm("receipts", `{"op":"receipts","address":"home"}`, 5)
	tm("write+attempt at N", `{"op":"write","address":"home/journal","verb":"note","message":"x","key":"clerk","attempt":"z-1"}`, 1)
	tm("write no attempt at N", `{"op":"write","address":"home/journal","verb":"note","message":"x","key":"clerk"}`, 20)
	tm("intent at N", `{"op":"intent","address":"home/desk","doing":"d","key":"clerk","witness":{"origin":"o","authority":"a","audience":"u","state":"s","wayBack":"w"}}`, 5)
	start = time.Now()
	for i := 0; i < 20; i++ {
		s2.Handle([]byte(`{"op":"status"}`))
	}
	t.Logf("status again %v each", time.Since(start)/20)
	// size on disk
	var total int64
	var files int
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			total += fi.Size()
			files++
		}
		return nil
	})
	t.Logf("carrier on disk: %d files, %.1f MiB, %.0f bytes/event", files, float64(total)/1048576, float64(total)/float64(l.Len()))
}
