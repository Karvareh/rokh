package bench

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/content"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// BenchmarkScaleContent measures one large thing kept in a carrier on disk:
// brought in with the content.put event that names it, in one recording
// (content.Bring, from a source read as a stream), then read back whole by a
// fresh opening of the folder (content.Fetch). The carrier is the one a
// person makes by default, growing by itself in steps of 64 slabs of 1 MiB,
// so a large thing also measures growing into it. Sizes run upwards, so the
// high-water mark of the process after each size is that size's own. Run it
// with -benchtime 1x.
func BenchmarkScaleContent(b *testing.B) {
	for _, size := range []int64{1 << 20, 16 << 20, 256 << 20, 1 << 30, 2 << 30} {
		b.Run("size="+sizeName(size), func(b *testing.B) {
			dir := b.TempDir()
			root := ed25519.NewKeyFromSeed(make([]byte, 32))
			rnd := mrand.NewChaCha8([32]byte{29})
			g, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, rnd)
			if err != nil {
				b.Fatal(err)
			}
			c := createV1(b, dir, "pass", root, g.ID, g)
			slabs0 := c.Vessel().Info().Slabs
			// The thing: bytes that do not compress, the same at every run,
			// read twice as a stream (once to name it, once to seal it).
			open := func() (io.Reader, error) {
				return io.LimitReader(mrand.NewChaCha8([32]byte{31}), size), nil
			}
			start := time.Now()
			lock, err := turn.Acquire(dir, 5*time.Second)
			if err != nil {
				b.Fatal(err)
			}
			rec, err := c.Begin(lock)
			if err != nil {
				b.Fatal(err)
			}
			d, payload, err := content.Bring(rec, "home/files", uint64(size), open, "application/octet-stream")
			if err != nil {
				b.Fatal(err)
			}
			e, err := event.SignFrom(event.Event{Carrier: &g.ID, Parents: []frame.ID{g.ID}, Address: "home/files",
				Verb: content.VerbPut, Payload: payload, Attest: stamp(0)}, root, rnd)
			if err != nil {
				b.Fatal(err)
			}
			if err := rec.Event(e.ID, e.Head, e.Body, e.Event.Address); err != nil {
				b.Fatal(err)
			}
			if err := rec.SetRef("main", e.ID); err != nil {
				b.Fatal(err)
			}
			if out, err := rec.Commit(); out != vessel.Recorded {
				b.Fatalf("commit: %s %v", out, err)
			}
			lock.Release()
			brought := time.Since(start)
			slabs := c.Vessel().Info().Slabs
			// Read back through a fresh opening, as another program would.
			start = time.Now()
			c2, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock("pass")), rand.Reader, nil)
			if err != nil {
				b.Fatal(err)
			}
			v := c2.Vessel()
			info := v.Info()
			sess, _, err := key.VesselSession("pass", v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
			if err != nil {
				b.Fatal(err)
			}
			c2.SetSealer(sess)
			opened := time.Since(start)
			start = time.Now()
			var n countWriter
			if err := content.Fetch(c2, d, &n); err != nil {
				b.Fatal(err)
			}
			fetched := time.Since(start)
			if int64(n) != size {
				b.Fatalf("fetched %d bytes of %d", n, size)
			}
			var total int64
			filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
				if err == nil && !fi.IsDir() {
					total += fi.Size()
				}
				return nil
			})
			b.ReportMetric(brought.Seconds(), "bring-s")
			b.ReportMetric(float64(size)/brought.Seconds()/(1<<20), "bring-MiB/s")
			b.ReportMetric(opened.Seconds(), "open-s")
			b.ReportMetric(fetched.Seconds(), "fetch-s")
			b.ReportMetric(float64(slabs-slabs0)/float64(c.Vessel().Info().Growth.Step), "growth-steps")
			b.ReportMetric(mib(total), "vessel-MiB")
			b.ReportMetric(float64(peakRSS())/float64(size), "peak-rss/size")
			b.ReportMetric(float64(peakRSS())/(1<<30), "peak-rss-GiB")
		})
	}
}

// countWriter counts what is written to it and keeps nothing.
type countWriter int64

func (w *countWriter) Write(p []byte) (int, error) {
	*w += countWriter(len(p))
	return len(p), nil
}
