//go:build legacy09

// This test pins the 0.9 home store (home.json, objects/, pointers/) that
// rokh-home/2 replaced with records in the ledger vessel.

package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testIter = 1000

func newHome(t *testing.T) (*Home, string) {
	t.Helper()
	root := t.TempDir()
	h, err := Create(root, "synthetic passphrase", testIter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h, root
}

func putBytes(t *testing.T, h *Home, kind, id string, b []byte) Info {
	t.Helper()
	info, err := h.Put(kind, id, bytes.NewReader(b), nil)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func getBytes(h *Home, kind, id string) ([]byte, error) {
	r, err := h.Get(kind, id)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func objectPath(h *Home, kind, id string) string {
	return filepath.Join(h.root, filepath.FromSlash(h.objectName(kind, id)))
}

func TestCreateOpenAndWrongPassphrase(t *testing.T) {
	h, root := newHome(t)
	putBytes(t, h, "blob", "a", []byte("synthetic"))
	h2, err := Open(root, "synthetic passphrase")
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()
	got, err := getBytes(h2, "blob", "a")
	if err != nil || string(got) != "synthetic" {
		t.Fatalf("reopened home read %q, %v", got, err)
	}
	if _, err := Open(root, "not the passphrase"); !errors.Is(err, ErrPass) {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	if _, err := Create(root, "x", testIter); !errors.Is(err, ErrExists) {
		t.Fatalf("a second home over the first: %v", err)
	}
	if _, err := Open(t.TempDir(), "x"); !errors.Is(err, ErrNoHome) {
		t.Fatalf("an empty folder: %v", err)
	}
}

func TestStreamsRoundTripAtEverySegmentBoundary(t *testing.T) {
	h, _ := newHome(t)
	for _, n := range []int{0, 1, SegmentSize - 1, SegmentSize, SegmentSize + 1, 2 * SegmentSize, 3*SegmentSize + 5} {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i*7 + n)
		}
		id := fmt.Sprint(n)
		info := putBytes(t, h, "blob", id, b)
		if info.Size != int64(n) || info.SHA256 != sha256.Sum256(b) {
			t.Fatalf("%d bytes: info %v", n, info)
		}
		got, err := getBytes(h, "blob", id)
		if err != nil || !bytes.Equal(got, b) {
			t.Fatalf("%d bytes: read back %d bytes, %v", n, len(got), err)
		}
	}
}

func TestAlteredCutExtendedOrReorderedStreamsDoNotRead(t *testing.T) {
	for _, total := range []int{3*SegmentSize + 1000, 3 * SegmentSize} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			h, _ := newHome(t)
			b := make([]byte, total)
			for i := range b {
				b[i] = byte(i % 251)
			}
			putBytes(t, h, "blob", "x", b)
			path := objectPath(h, "blob", "x")
			orig, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			seg := SegmentSize + 16
			cases := map[string]func([]byte) []byte{
				"header byte":               func(c []byte) []byte { c[10] ^= 1; return c },
				"first segment byte":        func(c []byte) []byte { c[headerSize+5] ^= 1; return c },
				"last segment byte":         func(c []byte) []byte { c[len(c)-3] ^= 1; return c },
				"cut at a segment boundary": func(c []byte) []byte { return c[:headerSize+2*seg] },
				"cut inside a segment":      func(c []byte) []byte { return c[:headerSize+seg+100] },
				"cut to the header":         func(c []byte) []byte { return c[:headerSize] },
				"extended":                  func(c []byte) []byte { return append(c, 1, 2, 3) },
				"a whole segment appended":  func(c []byte) []byte { return append(c, c[headerSize:headerSize+seg]...) },
				"two segments swapped": func(c []byte) []byte {
					out := append([]byte(nil), c[:headerSize]...)
					out = append(out, c[headerSize+seg:headerSize+2*seg]...)
					out = append(out, c[headerSize:headerSize+seg]...)
					return append(out, c[headerSize+2*seg:]...)
				},
			}
			for name, mutate := range cases {
				c := mutate(append([]byte(nil), orig...))
				if err := os.WriteFile(path, c, 0o600); err != nil {
					t.Fatal(err)
				}
				if got, err := getBytes(h, "blob", "x"); err == nil {
					t.Errorf("%s: read %d bytes and no error", name, len(got))
				}
			}
			if err := os.WriteFile(path, orig, 0o600); err != nil {
				t.Fatal(err)
			}
			if got, err := getBytes(h, "blob", "x"); err != nil || !bytes.Equal(got, b) {
				t.Fatalf("the restored object does not read: %v", err)
			}
		})
	}
}

func TestAnObjectMovedToAnotherNameDoesNotRead(t *testing.T) {
	h, _ := newHome(t)
	putBytes(t, h, "blob", "one", []byte("synthetic one"))
	putBytes(t, h, "blob", "two", []byte("synthetic two"))
	one, err := os.ReadFile(objectPath(h, "blob", "one"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath(h, "blob", "two"), one, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := getBytes(h, "blob", "two"); err == nil {
		t.Fatalf("an object moved under another name read as %q", got)
	}
}

func TestAChangedSourceIsNotKept(t *testing.T) {
	h, root := newHome(t)
	want := sha256.Sum256([]byte("what the preview saw"))
	if _, err := h.Put("blob", "p", strings.NewReader("what the copy read"), want[:]); !errors.Is(err, ErrMismatch) {
		t.Fatalf("a changed source: %v", err)
	}
	if ok, _ := h.Has("blob", "p"); ok {
		t.Fatal("bytes that did not match were kept")
	}
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasPrefix(info.Name(), tmpPrefix) {
			t.Errorf("a temporary file was left: %s", p)
		}
		return nil
	})
	right := sha256.Sum256([]byte("what the copy read"))
	if _, err := h.Put("blob", "p", strings.NewReader("what the copy read"), right[:]); err != nil {
		t.Fatal(err)
	}
}

// Every file under the home is searched for the synthetic secret text, for the
// identifiers and kinds it was stored under, and for the passphrase. None may
// appear, in a file's bytes or in its path.
func TestNothingOnDiskReadsAsWhatWasStored(t *testing.T) {
	root := t.TempDir()
	h, err := Create(root, "synthetic passphrase", testIter)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	marker := "کتابخانهٔ ساختگیِ آزمون ۱۴۰۵"
	kind, id := "provenance", "صندوقچه/نوشته‌ها/نمونهٔ-ساختگی.md"
	putBytes(t, h, kind, id, []byte(strings.Repeat(marker+"\n", 5000)))
	if err := h.SetPointer("index/"+id, []byte(marker)); err != nil {
		t.Fatal(err)
	}
	if err := h.SetSecrets("synthetic passphrase", map[string][]byte{"holochain-lair": []byte(marker)}); err != nil {
		t.Fatal(err)
	}
	needles := [][]byte{[]byte(marker), []byte(id), []byte("نوشته‌ها"), []byte(kind), []byte("holochain-lair"),
		[]byte("synthetic passphrase")}
	files := 0
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range needles {
			if strings.Contains(p, string(n)) {
				t.Errorf("a path reads as what was stored: %s", p)
			}
		}
		if info.IsDir() {
			return nil
		}
		files++
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range needles {
			if bytes.Contains(b, n) {
				t.Errorf("%s holds %q in the clear", p, n)
			}
		}
		return nil
	})
	if files < 3 {
		t.Fatalf("only %d files were searched", files)
	}
}

func TestPointersAreSealedAndBoundToTheirName(t *testing.T) {
	h, root := newHome(t)
	if err := h.SetPointer("registry", []byte("synthetic registry 1")); err != nil {
		t.Fatal(err)
	}
	if err := h.SetPointer("registry", []byte("synthetic registry 2")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := h.Pointer("registry")
	if err != nil || !ok || string(v) != "synthetic registry 2" {
		t.Fatalf("pointer: %q %v %v", v, ok, err)
	}
	if err := h.SetPointer("other", []byte("x")); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(root, filepath.FromSlash(h.pointerName("registry")))
	b := filepath.Join(root, filepath.FromSlash(h.pointerName("other")))
	ca, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, ca, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.Pointer("other"); err == nil {
		t.Fatal("a pointer moved under another name opened")
	}
	if _, ok, err := h.Pointer("absent"); ok || err != nil {
		t.Fatalf("an absent pointer: %v %v", ok, err)
	}
}

func TestTheOwnerKeyComesFromThePassphraseAlone(t *testing.T) {
	h, root := newHome(t)
	k, err := OwnerKey(root, "synthetic passphrase")
	if err != nil || !bytes.Equal(k, h.OwnerProofKey()) {
		t.Fatalf("owner key from the passphrase differs from the opened home's: %v", err)
	}
	other, err := OwnerKey(root, "not the passphrase")
	if err != nil || bytes.Equal(other, k) {
		t.Fatal("a wrong passphrase gave the owner's key")
	}
}

type patternReader struct {
	left int64
	x    byte
}

func (r *patternReader) Read(p []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.left {
		n = int(r.left)
	}
	for i := 0; i < n; i++ {
		p[i] = r.x
		r.x = r.x*31 + 7
	}
	r.left -= int64(n)
	return n, nil
}

// A stream much larger than any bound on memory is sealed and read back with
// one segment in hand, not the whole stream.
func TestALargeStreamIsSealedInBoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("large stream")
	}
	h, _ := newHome(t)
	const size = 256 << 20
	want := sha256.New()
	io.Copy(want, &patternReader{left: size})
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	info, err := h.Put("blob", "large", &patternReader{left: size}, want.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.Get("blob", "large")
	if err != nil {
		t.Fatal(err)
	}
	got := sha256.New()
	n, err := io.Copy(got, r)
	r.Close()
	if err != nil || n != size || !bytes.Equal(got.Sum(nil), want.Sum(nil)) || info.Size != size {
		t.Fatalf("large stream: %d bytes, %v", n, err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grew := int64(after.Sys) - int64(before.Sys); grew > 64<<20 {
		t.Fatalf("memory grew by %d MiB for a %d MiB stream", grew>>20, size>>20)
	}
}
