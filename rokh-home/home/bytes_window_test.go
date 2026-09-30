package home

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestByteRangeDoesNotRetainTheWholeFile(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("large.txt", strings.Repeat("synthetic ", 700000)), "notes/large")
	b, total, err := f.h.Bytes(OwnerActor, "notes/large", 1, "", 0, 13)
	if err != nil || total != 7000000 || string(b) != "synthetic syn" {
		t.Fatalf("range: %q total=%d error=%v", b, total, err)
	}
	if cap(b) > 65536 {
		t.Fatalf("13 returned bytes retain %d bytes of backing storage", cap(b))
	}
}

func TestByteContentHashIsNotTheVersionManifestHash(t *testing.T) {
	f := newFixture(t)
	text := "synthetic text with a separate manifest"
	f.ownerImport(f.file("source.txt", text), "notes/source")
	b, total, digest, err := f.h.BytesWithHash(OwnerActor, "notes/source", 1, "", 4, 7)
	expected := sha256.Sum256([]byte(text))
	view, statErr := f.h.Stat(OwnerActor, "notes/source")
	if err != nil || statErr != nil || string(b) != text[4:11] || total != int64(len(text)) || digest != hex.EncodeToString(expected[:]) {
		t.Fatalf("content witness: %q %d %s %v %v", b, total, digest, err, statErr)
	}
	if digest == view.Versions[0].SHA256 {
		t.Fatal("content and archive manifest were conflated")
	}
}

func TestByteRangeStillAuthenticatesTheUnreturnedTail(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("large.txt", strings.Repeat("synthetic ", 700000)), "notes/large")
	// rokh-home/2 keeps the blob as chunk records in the ledger's vessel:
	// damage the last byte of every slab file (never a head file), so the
	// chunks that hold the unreturned tail no longer verify.
	damaged := 0
	err := filepath.WalkDir(filepath.Join(f.root, "ledger", "rokh"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), "head") {
			return nil
		}
		fh, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		defer fh.Close()
		info, _ := fh.Stat()
		last := []byte{0}
		if _, err := fh.ReadAt(last, info.Size()-1); err != nil {
			return err
		}
		last[0] ^= 1
		_, err = fh.WriteAt(last, info.Size()-1)
		damaged++
		return err
	})
	if err != nil || damaged == 0 {
		t.Fatalf("no slab was damaged: %v", err)
	}
	b, _, _, err := f.h.BytesWithHash(OwnerActor, "notes/large", 1, "", 0, 13)
	if err == nil || len(b) != 0 {
		t.Fatalf("unverified prefix escaped: %q, %v", b, err)
	}
}
