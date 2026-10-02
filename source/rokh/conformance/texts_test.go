package conformance

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// texts holds the copies of the texts this tree is measured against, and
// their pin, texts.tsv.
var texts = filepath.Join(root, "texts")

// TestTheTextsAreTheCopiesTheirPinNames holds every copy in texts/ to the
// SHA-256 that texts.tsv names for it. The texts are kept in rokh-docs and
// change there, by a ruling; this tree takes a new version up whole, the
// copies and their pin in one change, so that what the map measures against
// is a version of the texts that exists, as it is here, where it is ruled. A
// copy edited here, a copy the pin does not name, and a pin with no copy all
// fail.
func TestTheTextsAreTheCopiesTheirPinNames(t *testing.T) {
	f, err := os.Open(filepath.Join(texts, "texts.tsv"))
	if err != nil {
		t.Fatalf("reading the pin: %v", err)
	}
	defer f.Close()
	pin := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 || len(cols[1]) != 64 {
			t.Fatalf("texts.tsv:%d: want a path, a SHA-256 and a version, separated by tabs", n)
		}
		if _, dup := pin[cols[0]]; dup {
			t.Fatalf("texts.tsv:%d: %s is pinned twice", n, cols[0])
		}
		pin[cols[0]] = cols[1]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(pin) == 0 {
		t.Fatal("texts.tsv pins nothing, and a pin that names nothing holds anything")
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(texts, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") || p == filepath.Join(texts, "README.md") {
			return nil
		}
		rel, err := filepath.Rel(texts, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		want, ok := pin[rel]
		if !ok {
			t.Errorf("%s is in texts/, and texts.tsv does not pin it", rel)
			return nil
		}
		seen[rel] = true
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != want {
			t.Errorf("%s is not the text texts.tsv pins: its SHA-256 is %x, and the pin says %s. "+
				"The texts change in rokh-docs, by a ruling, and come here whole, with their pin.", rel, sum, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for p := range pin {
		if !seen[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	for _, p := range missing {
		t.Errorf("texts.tsv pins %s, which is not in texts/", p)
	}
}
