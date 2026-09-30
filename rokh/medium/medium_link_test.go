package medium

// A probe. Every path is under the test's own temporary folder, inside the
// work tree.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The way out of the carrier is closed. A link that stays inside the
// carrier is still followed: a write to one name lands in another file of the
// same vessel.
func TestALinkInsideTheCarrierMovesAWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rokh", "000"), 0o700); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(root, "rokh", "head1.rkh")
	if err := os.WriteFile(head, bytes.Repeat([]byte{0xAA}, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	slab := filepath.Join(root, "rokh", "000", "00000003.rkh")
	if err := os.Symlink(filepath.Join("..", "head1.rkh"), slab); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	d := Dir{Root: root}
	err := d.Write("rokh/000/00000003.rkh", bytes.Repeat([]byte{0xBB}, 128))
	got, _ := os.ReadFile(head)
	t.Logf("write through a link that stays inside: err=%v; head1.rkh is now %d bytes, first byte %#x", err, len(got), got[0])
	if err == nil && got[0] == 0xBB {
		t.Errorf("a write to a slab name overwrote the head file through a link inside the carrier")
	}
	// The way out stays closed.
	outside := filepath.Join(t.TempDir(), "neighbour.txt")
	os.WriteFile(outside, []byte("neighbour"), 0o600)
	out := filepath.Join(root, "rokh", "000", "00000004.rkh")
	if err := os.Symlink(outside, out); err != nil {
		t.Fatal(err)
	}
	err = d.Write("rokh/000/00000004.rkh", []byte("overwritten"))
	n, _ := os.ReadFile(outside)
	t.Logf("write through a link that leaves: err=%v; the neighbour reads %q", err, n)
	if string(n) != "neighbour" {
		t.Errorf("DEFECT: the neighbour was overwritten")
	}
}
