package medium

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rokh/frame"
	"rokh/turn"
	"rokh/vessel"
)

var vk = bytes.Repeat([]byte{5}, 32)

func unlock(slots [][]byte, salt []byte, iter int) ([]byte, error) { return vk, nil }

type snapshot map[string][2]int64 // name -> logical length, allocated blocks

func snap(t *testing.T, root string) snapshot {
	t.Helper()
	out := snapshot{}
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = [2]int64{info.Size(), blocks(info)}
		return nil
	})
	return out
}

// A vessel in real files, written under the real kernel lock: the file set,
// the logical lengths and the allocated blocks are sampled during the
// writes and compared (A1 on this host's file system).
func TestAVesselInFilesUnderTheKernelLock(t *testing.T) {
	root := t.TempDir()
	d := Dir{Root: root}
	v, err := vessel.Create(d, vessel.Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: rand.Reader}, vk, nil,
		vessel.Root{Anchor: frame.Hash([]byte("a"))})
	if err != nil {
		t.Fatal(err)
	}
	before := snap(t, root)
	lock, err := turn.Acquire(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	blockDiffs := 0
	for i := 0; i < 200; i++ {
		tx, err := v.Begin(lock)
		if err != nil {
			t.Fatal(err)
		}
		head := []byte(fmt.Sprintf("head %d", i))
		tx.Put(vessel.Header{Type: vessel.RecEvent, ID: frame.Hash(head), Head: head}, bytes.Repeat([]byte("e"), 300))
		if out, err := tx.Commit(); err != nil || out != vessel.Recorded {
			t.Fatalf("recording %d: %s %v", i, out, err)
		}
		if i%20 == 0 {
			now := snap(t, root)
			if len(now) != len(before) {
				t.Fatalf("the file set changed at %d", i)
			}
			for n, s := range before {
				if now[n][0] != s[0] {
					t.Fatalf("%s changed length at %d", n, i)
				}
				if now[n][1] != s[1] {
					blockDiffs++
				}
			}
		}
	}
	if blockDiffs > 0 {
		t.Logf("allocated blocks differed %d times (reported, not explained away)", blockDiffs)
	}
	// Reopen from the files.
	w, rep, err := vessel.Open(d, unlock, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	w.Scan(func(h vessel.Header, r vessel.Ref) error { n++; return nil })
	if n != 200 || rep.Generation != 201 {
		t.Fatalf("reopened %d records at generation %d", n, rep.Generation)
	}
	// Foreign entries are ignored and not deleted.
	os.WriteFile(filepath.Join(root, "rokh", "desktop.ini"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, "rokh", "000", ".DS_Store"), []byte("x"), 0o600)
	if _, _, err := vessel.Open(d, unlock, rand.Reader); err != nil {
		t.Fatalf("a foreign entry broke the open: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "rokh", "desktop.ini")); err != nil {
		t.Fatal("a foreign entry was removed")
	}
}

func TestDirRefusesNamesOutsideTheRoot(t *testing.T) {
	d := Dir{Root: t.TempDir()}
	for _, bad := range []string{"", "/abs", "../x", "a/../../b", "a\\b", "c:x"} {
		if err := d.Write(bad, []byte("x")); err == nil {
			t.Fatalf("wrote %q", bad)
		}
	}
	if err := d.Mkdir("rokh"); err != nil {
		t.Fatal(err)
	}
	if err := d.Mkdir("rokh"); err != vessel.ErrExists {
		t.Fatalf("second mkdir: %v", err)
	}
	if _, err := d.Read("rokh/none.rkh", 10); err == nil {
		t.Fatal("read a missing file")
	}
}
