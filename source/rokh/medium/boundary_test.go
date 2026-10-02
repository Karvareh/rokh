package medium

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// After a probe: the
// carrier's rokh entry is a symlink to a synthetic neighboring directory
// outside the carrier. A write through the medium must be refused and the
// neighbor must stay unchanged. The read, create, list and remove boundaries
// are held the same way.
func TestASymlinkOutOfTheCarrierIsNoWayOut(t *testing.T) {
	base := t.TempDir()
	carrier := filepath.Join(base, "carrier")
	outside := filepath.Join(base, "synthetic-neighbor")
	for _, p := range []string{carrier, outside} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(outside, "head0.rkh")
	initial := []byte("synthetic neighboring file that is outside this carrier")
	if err := os.WriteFile(target, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(carrier, "rokh")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	d := Dir{Root: carrier}
	if err := d.Write("rokh/head0.rkh", []byte("synthetic replacement")); err == nil {
		t.Error("a write through a symlink out of the carrier was accepted")
	}
	if after, _ := os.ReadFile(target); !bytes.Equal(after, initial) {
		t.Fatal("the neighboring file outside the carrier was changed")
	}
	if b, err := d.Read("rokh/head0.rkh", 1<<20); err == nil {
		t.Errorf("a read through a symlink out of the carrier returned %d bytes", len(b))
	}
	if err := d.Write("rokh/new.rkh", []byte("x")); err == nil {
		t.Error("a create through a symlink out of the carrier was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.rkh")); err == nil {
		t.Fatal("a file was created outside the carrier")
	}
	if err := d.Mkdir("rokh/000"); err == nil {
		t.Error("a directory was made through a symlink out of the carrier")
	}
	if _, err := d.Names("rokh"); err == nil {
		t.Error("a listing through a symlink out of the carrier was answered")
	}
	if err := d.Remove("rokh/head0.rkh"); err == nil {
		t.Error("a remove through a symlink out of the carrier was accepted")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("the neighboring file was removed")
	}
}

// A symlink placed as a file inside the carrier's own rokh directory, and a
// relative symlink climbing out of it, are refused the same way.
func TestAFileSymlinkOutOfTheCarrierIsNoWayOut(t *testing.T) {
	base := t.TempDir()
	carrier := filepath.Join(base, "carrier")
	if err := os.MkdirAll(filepath.Join(carrier, "rokh"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "neighbor.txt")
	initial := []byte("synthetic neighbor")
	os.WriteFile(target, initial, 0o600)
	if err := os.Symlink(target, filepath.Join(carrier, "rokh", "head1.rkh")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink("../../neighbor.txt", filepath.Join(carrier, "rokh", "head2.rkh")); err != nil {
		t.Fatal(err)
	}
	d := Dir{Root: carrier}
	for _, name := range []string{"rokh/head1.rkh", "rokh/head2.rkh"} {
		if err := d.Write(name, []byte("replacement")); err == nil {
			t.Errorf("%s: a write through a file symlink out of the carrier was accepted", name)
		}
		if _, err := d.Read(name, 1<<20); err == nil {
			t.Errorf("%s: a read through a file symlink out of the carrier was answered", name)
		}
	}
	if after, _ := os.ReadFile(target); !bytes.Equal(after, initial) {
		t.Fatal("the neighboring file outside the carrier was changed")
	}
}

// A directory of the vessel that is a link, even to a
// directory inside the carrier, is refused for listing, writing and removing.
func TestADirectoryLinkInsideTheCarrierIsRefused(t *testing.T) {
	carrier := t.TempDir()
	real := filepath.Join(carrier, "rokh", "001")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(real, "00000400.rkh"), []byte("real slab"), 0o600)
	if err := os.Symlink("001", filepath.Join(carrier, "rokh", "000")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	d := Dir{Root: carrier}
	if _, err := d.Names("rokh/000"); err == nil {
		t.Error("a linked directory was listed")
	}
	if err := d.Write("rokh/000/00000400.rkh", []byte("moved")); err == nil {
		t.Error("a write went through a linked directory")
	}
	if b, _ := os.ReadFile(filepath.Join(real, "00000400.rkh")); string(b) != "real slab" {
		t.Fatal("the file behind the link was changed")
	}
	if err := d.Remove("rokh/000/00000400.rkh"); err == nil {
		t.Error("a remove went through a linked directory")
	}
	if err := d.Mkdir("rokh/000/sub"); err == nil {
		t.Error("a directory was made through a linked directory")
	}
}
