package shell

import (
	"os"
	"strings"
	"testing"
)

// The greeting, the prompt and the parting line are for a person at a
// terminal. A driver reading this surface through pipes must see exactly the
// bytes it saw before they existed: one standing block, one answer a sentence,
// and nothing else on either stream.
func TestHomeSurfaceAddsNothingForADriver(t *testing.T) {
	g, _ := fakeGate(t, func(op string, req map[string]any) map[string]any {
		return hello()
	})
	var out, notes strings.Builder
	code := runHome(g.rw, "", strings.NewReader("see the grants\n"), &out, &notes)
	if code != 0 {
		t.Fatalf("exit=%d notes=%q", code, notes.String())
	}
	if strings.Contains(notes.String(), "? ") || strings.Contains(notes.String(), "rokh — your event ledger") {
		t.Errorf("a driver was greeted or prompted: notes=%q", notes.String())
	}
	if strings.Contains(out.String(), tplHomeClosed) {
		t.Errorf("a driver was told goodbye: out=%q", out.String())
	}
	if first, _, _ := strings.Cut(out.String(), "\n"); !strings.HasPrefix(first, "a home, through its gate.") {
		t.Errorf("the first line a driver reads changed: %q", first)
	}
}

// "go" ends the conversation on any stream, and says the one sentence both
// this and the end of input say.
func TestHomeCloseSaysTheSameSentence(t *testing.T) {
	g, _ := fakeGate(t, func(op string, req map[string]any) map[string]any {
		return hello()
	})
	var out, notes strings.Builder
	if code := runHome(g.rw, "", strings.NewReader("go\n"), &out, &notes); code != 0 {
		t.Fatalf("exit=%d notes=%q", code, notes.String())
	}
	if !strings.Contains(out.String(), tplHomeClosed) {
		t.Errorf("go said nothing: out=%q", out.String())
	}
}

// A pipe is an *os.File too. If the surface decided by type rather than by
// asking the descriptor what it is, every redirected run would be greeted and
// prompted — so this is the check that keeps the two apart.
func TestPersonThereRefusesBuffersAndPipes(t *testing.T) {
	if personThere(strings.NewReader(""), &strings.Builder{}) {
		t.Error("a buffer was taken for a person")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if personThere(r, w) {
		t.Error("a pipe was taken for a person")
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if personThere(null, null) {
		t.Error("a non-terminal character device was taken for a person")
	}
}
