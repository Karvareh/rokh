package home

// A probe: what a home leaves in its folder after it is made,
// opened and written to.

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWhatAHomeLeavesInItsFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, "synthetic passphrase", "synthetic genesis of a review probe", 1000); err != nil {
		t.Fatal(err)
	}
	h, err := Open(root, "synthetic passphrase", "test")
	if err != nil {
		t.Fatal(err)
	}
	h.Close()
	tops := map[string]int{}
	var outside []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		top := parts[0]
		if len(parts) > 1 {
			top = parts[0] + "/" + parts[1]
		}
		if len(parts) > 2 {
			top += "/…"
		}
		tops[top]++
		if !strings.Contains(filepath.ToSlash(rel), "rokh/") {
			outside = append(outside, filepath.ToSlash(rel))
		}
		return nil
	})
	names := make([]string, 0, len(tops))
	for n := range tops {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t.Logf("%4d  %s", tops[n], n)
	}
	sort.Strings(outside)
	if len(outside) > 0 {
		t.Errorf("the home leaves %d file(s) outside a vessel, among them %v", len(outside), outside[:min(len(outside), 8)])
	}
}
