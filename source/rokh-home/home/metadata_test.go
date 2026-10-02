package home

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"rokh-home/sheet"
)

type paddingReader struct{ remaining int64 }

func (r *paddingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = ' '
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

// A valid JSON prefix at the byte limit must not hide the rest of an object.
// Whole-stream authentication and the metadata limit are both mandatory.
func TestMetadataDoesNotAcceptOnlyAValidPrefix(t *testing.T) {
	f := newFixture(t)
	r := io.MultiReader(strings.NewReader("{}"), &paddingReader{remaining: (64 << 20) - 2}, strings.NewReader("not part of the JSON object"))
	if _, err := f.h.st.Put("test-metadata", "oversized", r, nil); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := f.h.getJSON("test-metadata", "oversized", &value); err == nil {
		t.Fatal("accepted a valid JSON prefix without reading the complete sealed object")
	}
}

func TestRejectedVersionDoesNotChangeAnItemsNames(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("original.md", researchText), "research/note")
	before, err := f.h.Stat(OwnerActor, "research/note")
	if err != nil {
		t.Fatal(err)
	}
	f.h.mu.Lock()
	m, err := f.h.manifest(f.h.itemAt("research/note").Versions[0])
	if err != nil {
		f.h.mu.Unlock()
		t.Fatal(err)
	}
	m.Name = "uncommitted-name.md"
	_, err = f.h.recordVersion(OwnerActor, "research/note", m, sheet.Origin{})
	f.h.mu.Unlock()
	if err == nil {
		t.Fatal("recording without provenance was accepted")
	}
	after, err := f.h.Stat(OwnerActor, "research/note")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected record changed the visible item: before=%+v after=%+v err=%v", before, after, err)
	}
}
