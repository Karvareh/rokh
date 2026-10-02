package conformance

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// texts is where the texts this code is measured against are kept: docs/ of
// this repository, two folders above the module. Their pin is texts.tsv, in
// this folder.
var texts = filepath.Join(root, "..", "..", "docs")

// TestTheTextsAreTheVersionTheirPinNames holds every text this code is
// measured against to the SHA-256 that texts.tsv names for it. The texts are
// kept in docs/ and change there, by a ruling; this code takes a new version
// of them up by changing its pin, with the obligations and the code the new
// version asks for, so that what the map measures against is always the
// version the pin names. A text that differs from its pin, a pin of a text
// that is not in docs/, and a text the map reads that the pin does not name
// all fail.
func TestTheTextsAreTheVersionTheirPinNames(t *testing.T) {
	f, err := os.Open("texts.tsv")
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
	for _, read := range []string{"رساله.md", "without-consensus.md"} {
		if _, ok := pin[read]; !ok {
			t.Errorf("the map reads %s, and texts.tsv does not pin it", read)
		}
	}
	var paths []string
	for p := range pin {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(texts, filepath.FromSlash(p)))
		if err != nil {
			t.Errorf("texts.tsv pins %s, which is not in docs/: %v", p, err)
			continue
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != pin[p] {
			t.Errorf("docs/%s is not the text texts.tsv pins: its SHA-256 is %x, and the pin says %s. "+
				"A text changes in docs/ by a ruling, and this code takes it up by changing its pin, "+
				"with the obligations and the code the new version asks for.", p, sum, pin[p])
		}
	}
}
