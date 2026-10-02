package home

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rokh-home/authority"
)

// Search and reading at size: N versions imported by the owner, then the
// cost of one listing, one search, one context and one stat for a program
// that may read a scope, and the reopen of the whole home.
func TestSearchBench(t *testing.T) {
	if os.Getenv("HOME_BENCH") == "" {
		t.Skip("set HOME_BENCH=1 to run")
	}
	n := 2000
	fmt.Sscanf(os.Getenv("HOME_BENCH"), "%d", &n)
	f := newFixture(t)
	start := time.Now()
	for i := 0; i < n; i++ {
		src := f.file(fmt.Sprintf("n%d.md", i), fmt.Sprintf("# synthetic note %d\nlantern %d lights the synthetic library shelf %d\n", i, i, i%37))
		f.ownerImport(src, fmt.Sprintf("notes/n%d", i))
	}
	t.Logf("imported %d versions: %v (%.1f/s)", n, time.Since(start), float64(n)/time.Since(start).Seconds())
	reader := f.consumer("reader", Places{}, map[authority.Action]string{authority.Read: "notes", authority.Search: "notes", authority.Bytes: "notes"})
	tm := func(name string, fn func()) {
		start := time.Now()
		fn()
		t.Logf("%-24s %v", name, time.Since(start))
	}
	tm("list notes", func() { f.h.List(reader, "notes") })
	tm("stat", func() { f.h.Stat(reader, "notes/n17") })
	tm("search lantern", func() { f.h.Search(reader, "lantern", 20) })
	tm("search shelf 5", func() { f.h.Search(reader, "shelf 5", 20) })
	tm("context lantern", func() { f.h.Context(reader, "lantern", 4096) })
	tm("bytes", func() { f.h.Bytes(reader, "notes/n17", 0, "", 0, 0) })
	// Reopen: everything read back from the sealed store and the ledger.
	f.h.Close()
	start = time.Now()
	h, err := Open(f.root, pass, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reopen home with %d versions: %v", n, time.Since(start))
	f.h = h
	_ = filepath.Join
}
