package bench

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
	"rokh/ledger"
	"rokh/lineage"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// door is a carrier on disk holding n notes, and a daemon opened on it the
// way a door opens: the carrier, the owner's session, the references, the
// ledger read back from them.
type door struct {
	dir    string
	root   ed25519.PrivateKey
	clerk  keys
	opened time.Duration
	heap   int64
	s      *daemon.Server
}

// filled makes a carrier with a delegated key and n notes on "main", written
// in commits of many notes each so that a large history is quick to make, and
// then opens a door on it.
func filled(tb testing.TB, dir string, n int) *door {
	root := ed25519.NewKeyFromSeed(make([]byte, 32))
	rnd := mrand.NewChaCha8([32]byte{17})
	g, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, rnd)
	if err != nil {
		tb.Fatal(err)
	}
	cpub, cpriv, _ := ed25519.GenerateKey(rand.Reader)
	gp, _ := event.Grant{Subject: cpub, Scope: "home"}.Encode()
	ge, err := event.SignFrom(event.Event{Carrier: &g.ID, Parents: []frame.ID{g.ID}, Address: event.AddressRoot,
		Verb: event.VerbGrant, Payload: gp}, root, rnd)
	if err != nil {
		tb.Fatal(err)
	}
	c := createV1(tb, dir, "pass", root, g.ID, g, ge)
	tip := ge.ID
	const batch = 2000
	for done := 0; done < n; {
		var evs []event.Signed
		for i := 0; i < batch && done < n; i++ {
			e, err := event.SignFrom(event.Event{Carrier: &g.ID, Parents: []frame.ID{tip},
				Address: "home/journal", Verb: "note", Payload: []byte(fmt.Sprintf("a short note, number %d", done)),
				Attest: stamp(done)}, root, rnd)
			if err != nil {
				tb.Fatal(err)
			}
			evs = append(evs, e)
			tip = e.ID
			done++
		}
		recordV1(tb, dir, c, evs...)
	}
	d := &door{dir: dir, root: root, clerk: keys{name: "clerk", priv: cpriv, authority: ge.ID}}
	heap0 := liveHeap()
	start := time.Now()
	d.s = openDoor(tb, dir, root, d.clerk)
	d.opened = time.Since(start)
	d.heap = int64(liveHeap()) - int64(heap0)
	return d
}

// openDoor opens a carrier and a daemon on it.
func openDoor(tb testing.TB, dir string, root ed25519.PrivateKey, clerk keys) *daemon.Server {
	c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock("pass")), rand.Reader, nil)
	if err != nil {
		tb.Fatal(err)
	}
	v := c.Vessel()
	info := v.Info()
	sess, _, err := key.VesselSession("pass", v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	c.SetSealer(sess)
	refs, err := c.Refs()
	if err != nil {
		tb.Fatal(err)
	}
	heads := []frame.ID{}
	for _, id := range refs {
		heads = append(heads, id)
	}
	raw, err := c.Get(c.Anchor())
	if err != nil {
		tb.Fatal(err)
	}
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		tb.Fatal(err)
	}
	return daemon.New(c, l, daemon.Options{AllowSign: true, Dir: dir, Root: own(root), Keys: clerk})
}

// BenchmarkScaleDoor measures a carrier's door as the ledger behind it grows:
// opening it, and what one write, one status, one short log and one get cost
// at that size, through the daemon's own Handle. Run it with -benchtime 1x.
func BenchmarkScaleDoor(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 300_000} {
		b.Run(fmt.Sprintf("events=%d", n), func(b *testing.B) {
			d := filled(b, b.TempDir(), n)
			timed := func(line string, k int) time.Duration {
				start := time.Now()
				for i := 0; i < k; i++ {
					if r := d.s.Handle([]byte(line)); r["ok"] != true {
						b.Fatalf("%s: %v", line, r)
					}
				}
				return time.Since(start) / time.Duration(k)
			}
			write := timed(`{"op":"write","address":"home/journal","verb":"note","message":"one more","key":"clerk"}`, 10)
			status := timed(`{"op":"status"}`, 10)
			log := timed(`{"op":"log","limit":50}`, 10)
			var total int64
			filepath.Walk(d.dir, func(p string, fi os.FileInfo, err error) error {
				if err == nil && !fi.IsDir() {
					total += fi.Size()
				}
				return nil
			})
			b.ReportMetric(d.opened.Seconds(), "open-s")
			b.ReportMetric(float64(d.heap)/float64(n), "heap-B/event")
			b.ReportMetric(write.Seconds()*1000, "write-ms")
			b.ReportMetric(status.Seconds()*1000, "status-ms")
			b.ReportMetric(log.Seconds()*1000, "log50-ms")
			b.ReportMetric(mib(total), "vessel-MiB")
			b.ReportMetric(float64(peakRSS())/(1<<30), "peak-rss-GiB")
		})
	}
}

// BenchmarkScaleWriters measures many writers on one carrier: several doors,
// each its own daemon on the same folder as separate programs would open it,
// and several writers at each door, all recording at once. The writer's turn
// (the kernel's lock on the carrier) lets one commit through at a time, so
// what grows with the writers is the wait. Run it with -benchtime 1x.
func BenchmarkScaleWriters(b *testing.B) {
	for _, c := range []struct{ doors, writers int }{
		{1, 1}, {1, 4}, {1, 16}, {1, 64}, {2, 1}, {4, 1}, {8, 1}, {16, 1}, {8, 8},
	} {
		b.Run(fmt.Sprintf("doors=%d/writers=%d", c.doors, c.writers), func(b *testing.B) {
			d := filled(b, b.TempDir(), 1000)
			doors := []*daemon.Server{d.s}
			for i := 1; i < c.doors; i++ {
				doors = append(doors, openDoor(b, d.dir, d.root, d.clerk))
			}
			const each = 8 // writes per writer
			var mu sync.Mutex
			var lat []time.Duration
			failed := map[string]int{}
			var wg sync.WaitGroup
			start := time.Now()
			for _, s := range doors {
				for w := 0; w < c.writers; w++ {
					wg.Add(1)
					go func(s *daemon.Server, w int) {
						defer wg.Done()
						for i := 0; i < each; i++ {
							line := fmt.Sprintf(`{"op":"write","address":"home/journal","verb":"note","message":"writer %d, note %d","key":"clerk"}`, w, i)
							t0 := time.Now()
							r := s.Handle([]byte(line))
							took := time.Since(t0)
							mu.Lock()
							if r["ok"] == true {
								lat = append(lat, took)
							} else {
								failed[fmt.Sprint(r["code"])]++
							}
							mu.Unlock()
						}
					}(s, w)
				}
			}
			wg.Wait()
			wall := time.Since(start)
			sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
			pct := func(p float64) float64 {
				if len(lat) == 0 {
					return 0
				}
				return lat[int(p*float64(len(lat)-1))].Seconds() * 1000
			}
			nfail := 0
			for _, n := range failed {
				nfail += n
			}
			// What the carrier holds afterwards, read afresh.
			fresh := openDoor(b, d.dir, d.root, d.clerk)
			st := fresh.Handle([]byte(`{"op":"status"}`))
			b.ReportMetric(float64(len(lat))/wall.Seconds(), "writes/s")
			b.ReportMetric(pct(0.5), "p50-ms")
			b.ReportMetric(pct(0.99), "p99-ms")
			b.ReportMetric(pct(1), "max-ms")
			b.ReportMetric(float64(nfail), "failed")
			b.ReportMetric(float64(st["accepted"].(int)), "accepted-after")
			if nfail > 0 {
				b.Logf("failed writes by code: %v", failed)
			}
		})
	}
}

// BenchmarkScaleReconcile measures bringing two copies of one ledger together
// as the ledger grows: each copy is opened with the owner's passphrase the way
// the command line opens it (keyview.Open), each records one note of its own,
// and the two are reconciled — the union of their records both ways, judged
// by each side. Run it with -benchtime 1x.
func BenchmarkScaleReconcile(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("events=%d", n), func(b *testing.B) {
			a := b.TempDir()
			d := filled(b, a, n)
			other := b.TempDir()
			if err := copyTree(a, other); err != nil {
				b.Fatal(err)
			}
			side := func(dir string) (lineage.Side, time.Duration, func()) {
				start := time.Now()
				c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock("pass")), rand.Reader, nil)
				if err != nil {
					b.Fatal(err)
				}
				sess, _, layer, err := keyview.Open(c, "pass")
				if err != nil {
					b.Fatal(err)
				}
				c.SetSealer(sess)
				opened := time.Since(start)
				lock, err := turn.Acquire(dir, 5*time.Second)
				if err != nil {
					b.Fatal(err)
				}
				return lineage.Side{C: c, Own: lock, Judge: layer.Judge, Covers: layer.Covers}, opened, func() { lock.Release() }
			}
			note := func(dir, text string) {
				s := openDoor(b, dir, d.root, d.clerk)
				if r := s.Handle([]byte(`{"op":"write","address":"home/journal","verb":"note","message":"` + text + `","key":"clerk"}`)); r["ok"] != true {
					b.Fatalf("write: %v", r)
				}
			}
			note(a, "only in the first copy")
			note(other, "only in the second copy")
			local, openedLocal, doneLocal := side(a)
			remote, _, doneRemote := side(other)
			start := time.Now()
			res, err := lineage.Reconcile(local, remote)
			took := time.Since(start)
			doneLocal()
			doneRemote()
			if err != nil || !res.OK() {
				b.Fatalf("reconcile: %v %s", err, res)
			}
			b.ReportMetric(openedLocal.Seconds(), "open-s")
			b.ReportMetric(took.Seconds(), "reconcile-s")
			b.ReportMetric(float64(res.Local.Added+res.Remote.Added), "records-moved")
			b.ReportMetric(float64(peakRSS())/(1<<30), "peak-rss-GiB")
		})
	}
}

// copyTree copies a carrier folder as a person copies it while nothing
// writes: every file, byte for byte.
func copyTree(from, to string) error {
	return filepath.Walk(from, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if fi.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o600)
	})
}
