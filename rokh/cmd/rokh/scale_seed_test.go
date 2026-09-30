package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedHolds is what a seed's folder holds, counted from its own log: every
// event, the ledger's own among them, and the seed events (gives and takes)
// of every seed before it that reached this one.
type seedHolds struct {
	events, system, seeds, keyring, notes int
	bytes                                 int64
}

func (w *seedWorld) holds(dir string) seedHolds {
	w.t.Helper()
	var h seedHolds
	for _, r := range w.log("owner", dir) {
		h.events++
		switch {
		case r.Verb == "rokh.seed":
			h.seeds++
			h.system++
		case r.Verb == "rokh.keyring":
			h.keyring++
			h.system++
		case r.Address == "rokh":
			h.system++
		default:
			h.notes++
		}
	}
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			h.bytes += fi.Size()
		}
		return nil
	})
	return h
}

// BenchmarkScaleSeeds measures seeds as they multiply, through the real
// command line: a line of seeds, each the seed of the one before, and a fan
// of seeds all given by one root. At every step it records how long the seed
// took and what the new folder holds of those before it; at the end, a note
// written in the last seed is reconciled back into the root. Run it with
// -benchtime 1x -v to see every step.
func BenchmarkScaleSeeds(b *testing.B) {
	const steps = 24
	for _, shape := range []string{"line", "fan"} {
		b.Run(fmt.Sprintf("%s=%d", shape, steps), func(b *testing.B) {
			w := newSeedWorld(b)
			root := w.dir("root")
			w.must("owner", "init", root, "--message", "a synthetic genesis", "--size", "4M", "--slab", "256K")
			w.must("owner", "write", root, "--address", "journal/day", "--message", "the first note")
			src := root
			var last string
			var first, prev seedHolds
			var took []time.Duration
			b.Logf("%-5s %8s %7s %7s %6s %8s %6s %9s", "seed", "took", "events", "system", "seeds", "keyring", "notes", "bytes")
			for i := 1; i <= steps; i++ {
				dst := w.dir(fmt.Sprintf("seed%03d", i))
				start := time.Now()
				w.must("owner", "seed", src, "--size", "4M", "--slab", "256K", dst)
				t := time.Since(start)
				took = append(took, t)
				h := w.holds(dst)
				b.Logf("%-5d %8s %7d %7d %6d %8d %6d %9d", i, t.Round(time.Millisecond), h.events, h.system, h.seeds, h.keyring, h.notes, h.bytes)
				if i == 1 {
					first = h
				}
				prev = h
				last = dst
				if shape == "line" {
					src = dst
				}
			}
			w.must("owner", "write", last, "--address", "journal/day", "--message", "written in the last seed")
			start := time.Now()
			out, errOut, code := w.run("owner", "reconcile", root, last)
			reconciled := time.Since(start)
			if code != 0 {
				b.Fatalf("reconcile: exit %d\n%s%s", code, out, errOut)
			}
			rh := w.holds(root)
			if rh.notes != 2 {
				b.Fatalf("the root holds %d notes after reconcile; the one written in the last seed did not arrive", rh.notes)
			}
			b.Logf("reconcile root with seed %d: %s; the root now holds %d events, %d of them the ledger's own", steps, reconciled.Round(time.Millisecond), rh.events, rh.system)
			b.ReportMetric(took[0].Seconds(), "first-seed-s")
			b.ReportMetric(took[len(took)-1].Seconds(), "last-seed-s")
			b.ReportMetric(float64(prev.system-first.system)/float64(steps-1), "system-events-per-seed")
			b.ReportMetric(float64(prev.system), "last-seed-system-events")
			b.ReportMetric(reconciled.Seconds(), "reconcile-s")
		})
	}
}
