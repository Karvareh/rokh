package bench

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rokh/frame"
	"rokh/medium"
	"rokh/vessel"
)

// vesselSize is one shape a vessel can take: slabs of 2^log2 bytes, and how
// many of them.
type vesselSize struct{ log2, slabs int }

func (s vesselSize) bytes() int64 { return int64(s.slabs) << s.log2 }

func (s vesselSize) name() string {
	return fmt.Sprintf("S=%s/N=%d/V=%s", sizeName(1<<s.log2), s.slabs, sizeName(s.bytes()))
}

// sparseSizes run from the smallest vessel to the most slabs the format
// allows (vessel.MaxSlabs, 4,194,304): with slabs of 256 KiB that is a vessel
// of 1 TiB, and the same slab count with slabs of 64 MiB is the format's
// ceiling of 256 TiB, whose per-commit cost the larger slabs scale by S.
var sparseSizes = []vesselSize{
	{18, 16}, {18, 256}, {18, 4096}, {18, 16384}, {18, 65536}, {18, 262144}, {18, 1 << 20}, {18, 1 << 22},
	{20, 16}, {20, 4096}, {20, 65536},
	{26, 16}, {26, 64}, {26, 256},
}

// diskSizes are vessels written to a real folder, as big as a test machine's
// disk takes: 4 MiB to 16 GiB.
var diskSizes = []vesselSize{
	{18, 16}, {20, 64}, {20, 1024}, {20, 4096}, {20, 16384}, {26, 16}, {26, 64},
}

// record is what one small recording puts in a vessel: an event's head in the
// clear of the pack and an envelope of a short note's body.
func record(rnd io.Reader) (vessel.Header, []byte) {
	var id frame.ID
	rnd.Read(id[:])
	head := make([]byte, 180)
	rnd.Read(head)
	env := make([]byte, 330)
	rnd.Read(env)
	return vessel.Header{Type: vessel.RecEvent, ID: id, Head: head}, env
}

// BenchmarkScaleVessel measures a vessel as it grows, on the sparse medium:
// how long it takes to make and to open, how much memory an opened vessel
// holds, and what one recording of a short note costs — the time, the bytes
// read and written, the files written — at every size up to the format's slab
// count. Run it with -benchtime 1x: each size is made once and measured over
// its own commits.
func BenchmarkScaleVessel(b *testing.B) {
	for _, s := range sparseSizes {
		b.Run(s.name(), func(b *testing.B) {
			m := newSparse()
			rnd := newQuickRandom(3)
			vk := make([]byte, 32)
			rnd.Read(vk)
			var anchor frame.ID
			rnd.Read(anchor[:])
			start := time.Now()
			if _, err := vessel.Create(m, vessel.Params{SlabLog2: s.log2, Slabs: s.slabs, Iter: 1, Rand: rnd}, vk, nil, vessel.Root{Anchor: anchor}); err != nil {
				b.Fatal(err)
			}
			created := time.Since(start)
			heap0 := liveHeap()
			before := m.counted()
			start = time.Now()
			v, _, err := vessel.Open(m, unlockWith(vk), rnd)
			if err != nil {
				b.Fatal(err)
			}
			opened := time.Since(start)
			openIO := m.counted().minus(before)
			held := int64(liveHeap()) - int64(heap0)
			// Fill a little first, so a commit rewrites a pack as it does
			// once a vessel holds something.
			for i := 0; i < 8; i++ {
				commitOne(b, v, rnd)
			}
			k := commitsFor(s)
			before = m.counted()
			start = time.Now()
			for i := 0; i < k; i++ {
				commitOne(b, v, rnd)
			}
			took := time.Since(start)
			io := m.counted().minus(before)
			_, virtual := m.held()
			n := float64(k)
			b.ReportMetric(created.Seconds(), "create-s")
			b.ReportMetric(opened.Seconds(), "open-s")
			b.ReportMetric(mib(openIO.ReadBytes), "open-read-MiB")
			b.ReportMetric(mib(held), "open-heap-MiB")
			b.ReportMetric(took.Seconds()*1000/n, "commit-ms")
			b.ReportMetric(mib(io.WriteBytes)/n, "write-MiB/commit")
			b.ReportMetric(mib(io.ReadBytes)/n, "read-MiB/commit")
			b.ReportMetric(float64(io.Writes)/n, "files-written/commit")
			b.ReportMetric(float64(io.WriteBytes)/n/510, "write-amp")
			b.ReportMetric(float64((s.slabs+vessel.SegmentEntries-1)/vessel.SegmentEntries), "segments")
			b.ReportMetric(mib(virtual)/1024, "vessel-GiB")
			b.ReportMetric(float64(peakRSS())/(1<<30), "peak-rss-GiB")
		})
	}
}

// commitsFor is how many commits are timed at a size: five, or two where one
// commit writes a quarter of a GiB or more.
func commitsFor(s vesselSize) int {
	segs := int64((s.slabs + vessel.SegmentEntries - 1) / vessel.SegmentEntries)
	if (segs+1)<<s.log2 >= 256<<20 {
		return 2
	}
	return 5
}

func commitOne(b *testing.B, v *vessel.Vessel, rnd io.Reader) {
	tx, err := v.Begin(holding{})
	if err != nil {
		b.Fatal(err)
	}
	h, env := record(rnd)
	if err := tx.Put(h, env); err != nil {
		b.Fatal(err)
	}
	if out, err := tx.Commit(); out != vessel.Recorded {
		b.Fatalf("commit: %s %v", out, err)
	}
}

// BenchmarkScaleVesselDisk is the same measurement on a real folder, with the
// host's randomness filling every slab and every file flushed, from 4 MiB to
// 16 GiB. Run it with -benchtime 1x. The folder is ROKH_SCALE_DIR when set, else a temporary one; a
// vessel is removed when its measurement ends.
func BenchmarkScaleVesselDisk(b *testing.B) {
	for _, s := range diskSizes {
		b.Run(s.name(), func(b *testing.B) {
			dir := b.TempDir()
			if base := os.Getenv("ROKH_SCALE_DIR"); base != "" {
				var err error
				if dir, err = os.MkdirTemp(base, "vessel-"); err != nil {
					b.Fatal(err)
				}
				defer os.RemoveAll(dir)
			}
			m := medium.Dir{Root: dir}
			vk := make([]byte, 32)
			rand.Read(vk)
			var anchor frame.ID
			rand.Read(anchor[:])
			start := time.Now()
			v, err := vessel.Create(m, vessel.Params{SlabLog2: s.log2, Slabs: s.slabs, Iter: 1, Rand: rand.Reader}, vk, nil, vessel.Root{Anchor: anchor})
			if err != nil {
				b.Fatal(err)
			}
			created := time.Since(start)
			start = time.Now()
			v, _, err = vessel.Open(m, unlockWith(vk), rand.Reader)
			if err != nil {
				b.Fatal(err)
			}
			opened := time.Since(start)
			for i := 0; i < 8; i++ {
				commitOne(b, v, rand.Reader)
			}
			k := commitsFor(s)
			start = time.Now()
			for i := 0; i < k; i++ {
				commitOne(b, v, rand.Reader)
			}
			took := time.Since(start)
			var files int
			var total int64
			filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
				if err == nil && !fi.IsDir() {
					files++
					total += fi.Size()
				}
				return nil
			})
			b.ReportMetric(created.Seconds(), "create-s")
			b.ReportMetric(opened.Seconds(), "open-s")
			b.ReportMetric(took.Seconds()*1000/float64(k), "commit-ms")
			b.ReportMetric(float64(files), "files")
			b.ReportMetric(mib(total)/1024, "vessel-GiB")
			b.ReportMetric(float64(total)/created.Seconds()/(1<<20), "create-MiB/s")
		})
	}
}
