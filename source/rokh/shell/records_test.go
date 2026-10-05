package shell

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/frame"
	"rokh/ledger"
	"rokh/tui"
)

// eventsIn is every accepted event of every ledger in the vault and every
// berth in the seats folder, by id.
func eventsIn(t *testing.T, f *fixture) map[frame.ID]bool {
	t.Helper()
	out := map[frame.ID]bool{}
	for _, parent := range []string{filepath.Join(f.vault, LedgersFolder), f.mount} {
		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			dir := filepath.Join(parent, e.Name())
			if !e.IsDir() || !isCarrier(dir) {
				continue
			}
			led, err := replay(openV1(t, dir, testPass))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range led.Order() {
				if led.State(id) == ledger.Accepted {
					out[id] = true
				}
			}
		}
	}
	return out
}

// Each of the nineteen sentences is said on a real ledger, in an order that
// lets every one of them do its work, and the events it made are counted:
// a sentence makes an event exactly when the one list says it records, so
// what the list tells a person and what the sentence does cannot drift. An
// event carried over from a berth by a reunion was made before, and is not
// counted as made by the sentence that carried it.
func TestASentenceRecordsExactlyWhenTheListSaysSo(t *testing.T) {
	f := makeFixture(t)
	f.makeBerth(t, "seat-a", true)
	t.Chdir(t.TempDir()) // a bundle is written where the person stands
	file := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(file, []byte("a small file"), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	who := hex.EncodeToString(pub)
	s := f.session()
	defer s.closeAll()

	grant := func() string {
		ids := s.current.led.ActiveGrants()
		if len(ids) == 0 {
			t.Fatal("no grant to take back")
		}
		return ids[0].String()
	}
	script := []struct {
		say    string
		line   func() string
		before []string
	}{
		{"open the ledger {name}", func() string { return "open the ledger home" }, nil},
		{"see the ledger", nil, nil},
		{"see the ledgers", nil, nil},
		{"see the grants", nil, nil},
		{"read {address}", func() string { return "read rokh" }, nil},
		{"write at {address}: {text}", func() string { return "write at home/a: one" }, nil},
		{"cancel", nil, nil},
		{"write", nil, []string{"write at home/b: two"}},
		{"bring {thing} to {address}", func() string { return "bring " + file + " to files/one" }, nil},
		{"entrust writing at {place} to {who}", func() string { return "entrust writing at home to " + who }, nil},
		{"take back the grant {id}", func() string { return "take back the grant " + grant() }, nil},
		{"entrust reading {place} to {who}", func() string { return "entrust reading home to " + who }, nil},
		{"carry {address} to {path}", func() string { return "carry home to " + t.TempDir() }, nil},
		{"carry the bundle for {who}", func() string { return "carry the bundle for " + who }, nil},
		{"carry the ledger to {path}", func() string { return "carry the ledger to " + filepath.Join(t.TempDir(), "copy") }, nil},
		{"bring the returned ledger", nil, nil},
		{"reconcile", nil, nil},
		{"open a new ledger named {name}", func() string { return "open a new ledger named second" }, nil},
		{"leave", nil, nil},
	}
	said := map[string]bool{}
	for _, step := range script {
		for _, b := range step.before {
			run(t, s, b)
		}
		line := step.say
		if step.line != nil {
			line = step.line()
		}
		var sn tui.Sentence
		for _, x := range tui.Sentences {
			if x.Say == step.say {
				sn = x
			}
		}
		if sn.Say == "" {
			t.Fatalf("the script says %q, which is not in the list", step.say)
		}
		before := eventsIn(t, f)
		reply, err := runLine2(s, line)
		made := 0
		for id := range eventsIn(t, f) {
			if !before[id] {
				made++
			}
		}
		// A new ledger is made with two records at once: its first event and
		// the record of its owner's keyring, as the command line makes it.
		want := 1
		if step.say == "open a new ledger named {name}" {
			want = 2
		}
		if sn.Records && (err != nil || made != want) {
			t.Errorf("%q records, the list says; it made %d events (%q, %v)", line, made, reply, err)
		}
		if !sn.Records && made != 0 {
			t.Errorf("%q records nothing, the list says; it made %d events (%q)", line, made, reply)
		}
		if strings.Contains(sn.Does, "records one event") != sn.Records {
			t.Errorf("%q: the words and the mark disagree: %q", sn.Say, sn.Does)
		}
		if sn.Records && sn.Role != tui.RoleBoundary {
			t.Errorf("%q records and does not take the boundary's colour", sn.Say)
		}
		said[sn.Say] = true
	}
	for _, sn := range tui.Sentences {
		if !said[sn.Say] {
			t.Errorf("%q is in the list and was never said here", sn.Say)
		}
	}
}
