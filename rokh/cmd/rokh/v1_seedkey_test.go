package main

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/vessel"
)

// kidsOf lists, sorted, the readers every envelope of one event names in a
// carrier, as the owner finds them. It opens nothing.
func (w *seedWorld) kidsOf(dir, id string) []string {
	w.t.Helper()
	want, err := frame.ParseID(id)
	if err != nil {
		w.t.Fatal(err)
	}
	c := w.open("owner", dir)
	seen := map[string]bool{}
	envelopes := 0
	err = c.Vessel().Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type != vessel.RecEvent || h.ID != want {
			return nil
		}
		env, err := c.Vessel().Body(ref)
		if err != nil {
			return err
		}
		kids, err := key.Kids(env)
		if err != nil {
			return err
		}
		envelopes++
		for _, k := range kids {
			seen[fmt.Sprintf("%x", k)] = true
		}
		return nil
	})
	if err != nil || envelopes == 0 {
		w.t.Fatalf("the envelopes of %s on %s: %v, %d found", id, filepath.Base(dir), err, envelopes)
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// keyringOf reads the keyring adds of a carrier's ledger by name, through the
// real command.
func (w *seedWorld) keyringOf(dir string) map[string][]event.Keyring {
	w.t.Helper()
	out := map[string][]event.Keyring{}
	for _, r := range w.log("owner", dir) {
		if r.Verb != event.VerbKeyring {
			continue
		}
		k, err := event.DecodeKeyring(r.payload(w.t))
		if err != nil {
			w.t.Fatal(err)
		}
		if k.Op == event.KeyringAdd {
			out[k.Name] = append(out[k.Name], k)
		}
	}
	return out
}

// Step 1 (contract 3.1, 4.7): a seed's key reads nothing. A key
// that reads is named in every envelope sealed where it reads, and an
// envelope names at most 32 readers (key.MaxReaders): a seed's key that read
// its scopes brought every later envelope one reader nearer that limit, and
// around the 31st whole seed nothing could be sealed any more, not even a
// revoke. Through the command line: one root gives 40 whole seeds. Every
// seed's key reads nothing, and the key list says so. Then, on the root and
// on the last seed, an event is recorded, sealed to the owner and to the live
// reader of its address and to nobody else, and read back by both. The root
// still records a keyring add and its revoke, and verifies.
func TestFortyWholeSeedsLeaveEveryEnvelopeSealable(t *testing.T) {
	w := newSeedWorld(t)
	src := w.dir("src")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "32M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "reader", "--reads", "journal", "--key-passphrase-file", w.pass("reader"))
	last := ""
	for i := 1; i <= 40; i++ {
		last = w.dir(fmt.Sprintf("seed%02d", i))
		out, errOut, code := w.run("owner", "seed", src, "--size", "4M", "--slab", "256K", last)
		if code != 0 {
			t.Fatalf("whole seed %d of 40 was not given: exit %d\n%s%s", i, code, out, errOut)
		}
	}

	ring := w.keyringOf(src)
	seeds := 0
	for name, gens := range ring {
		if !strings.HasPrefix(name, "seed-") {
			continue
		}
		for _, k := range gens {
			seeds++
			if k.Reads != nil {
				t.Fatalf("the key of %s reads %q", name, k.Reads)
			}
		}
	}
	if seeds != 40 {
		t.Fatalf("the root's keyring holds %d seeds' keys, not 40", seeds)
	}
	if list := w.must("owner", "key", "list", src); strings.Count(list, "reads nothing, can write") != 40 {
		t.Fatalf("the key list does not say that each seed's key reads nothing:\n%s", list)
	}
	var owner, reader []byte
	for _, k := range ring["owner"] {
		owner = k.Reader
	}
	for _, k := range ring["reader"] {
		reader = k.Reader
	}
	if owner == nil || reader == nil {
		t.Fatal("the owner's or the reader's generation is not in the keyring")
	}
	sealedTo := []string{fmt.Sprintf("%x", key.Kid(owner)), fmt.Sprintf("%x", key.Kid(reader))}
	sort.Strings(sealedTo)

	for _, dir := range []string{src, last} {
		addr := "journal/after-" + filepath.Base(dir)
		msg := "a synthetic line, written after forty seeds, on " + filepath.Base(dir)
		w.must("owner", "write", dir, "--address", addr, "--message", msg)
		id := ""
		for _, r := range w.log("owner", dir) {
			if r.Address == addr {
				id = r.ID
			}
		}
		if id == "" {
			t.Fatalf("%s does not hold the event written at %s", filepath.Base(dir), addr)
		}
		if got := w.kidsOf(dir, id); strings.Join(got, ",") != strings.Join(sealedTo, ",") {
			t.Fatalf("on %s the event is sealed to %v, not to the owner and the reader %v", filepath.Base(dir), got, sealedTo)
		}
		for _, role := range []string{"owner", "reader"} {
			got, err := base64.StdEncoding.DecodeString(bodies(w.log(role, dir))[addr])
			if err != nil || string(got) != msg {
				t.Fatalf("on %s the %s reads %q (%v), not what was written", filepath.Base(dir), role, got, err)
			}
		}
	}

	w.must("owner", "key", "add", src, "--name", "late", "--reads", "journal", "--key-passphrase-file", w.pass("late"))
	w.must("owner", "key", "revoke", src, "--key", "late")
	if v := w.must("owner", "verify", src); !strings.Contains(v, "0 rejected, 0 pending") {
		t.Fatalf("verify of the root after forty seeds:\n%s", v)
	}
}
