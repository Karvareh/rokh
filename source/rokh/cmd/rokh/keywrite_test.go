package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/vessel"
)

// T1 step 2: a key writes with its own passphrase through the command itself,
// rokh write under the key's passphrase file, as a person or a program runs
// it. Nothing here signs or begins a recording on its own.

// keyed is a carrier the commands made, with the owner and four keys: reader
// reads journal, spy reads private, blind has no right, and writer writes
// under journal and reads nothing.
type keyed struct {
	dir   string
	files map[string]string // passphrase files, "owner" included
}

func keyedCarrier(t *testing.T) keyed {
	t.Helper()
	bondSetup(t)
	k := keyed{dir: filepath.Join(t.TempDir(), "c"), files: map[string]string{}}
	mustRun(t, cmdInit, k.dir, "--message", "genesis")
	k.files["owner"] = writeFile(t, "owner.pass", []byte(bondPass))
	flags := map[string][]string{
		"reader": {"--reads", "journal"},
		"spy":    {"--reads", "private"},
		"blind":  {},
		"writer": {"--write", "--scope", "journal"},
	}
	for _, name := range []string{"reader", "spy", "blind", "writer"} {
		k.files[name] = writeFile(t, name+".pass", []byte("synthetic passphrase of "+name))
		mustRun(t, cmdKey, append([]string{"add", k.dir, "--name", name, "--key-passphrase-file", k.files[name]}, flags[name]...)...)
	}
	return k
}

// write runs rokh write with who's passphrase file and its JSON answer.
func (k keyed) write(t *testing.T, who string, args ...string) (map[string]any, error) {
	t.Helper()
	out, errOut, err := bondRun(t, cmdWrite, append([]string{k.dir, "--passphrase-file", k.files[who], "--json"}, args...)...)
	var ans map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &ans); jerr != nil {
		t.Fatalf("rokh write %v as %s answered no JSON: %v\n%s%s", args, who, jerr, out, errOut)
	}
	return ans, err
}

// sees says whether who's rokh log shows the body written at addr.
func (k keyed) sees(t *testing.T, who, addr string, body []byte) bool {
	t.Helper()
	out, errOut, err := bondRun(t, cmdLog, k.dir, "--passphrase-file", k.files[who], "--json")
	if err != nil {
		t.Fatalf("rokh log as %s: %v %s", who, err, errOut)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) == nil && row["address"] == addr && row["payload"] == base64.StdEncoding.EncodeToString(body) {
			return true
		}
	}
	return false
}

func (k keyed) generation(t *testing.T) uint64 {
	t.Helper()
	return openForTest(t, k.dir, bondPass).Vessel().Info().Generation
}

// keyringAdd is the live keyring generation of a key, by name, as the owner
// reads it.
func keyringAdd(t *testing.T, dir, name string) event.Keyring {
	t.Helper()
	l := ledgerOf(t, openForTest(t, dir, bondPass))
	adds, _ := l.Keyring()
	for _, id := range adds {
		e, _ := l.Get(id)
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && k.Name == name {
			return k
		}
	}
	t.Fatalf("no live key %q", name)
	return event.Keyring{}
}

// kidsOf lists the readers every envelope of one event names.
func kidsOf(t *testing.T, dir string, id frame.ID) map[[key.KidSize]byte]bool {
	t.Helper()
	c := openForTest(t, dir, bondPass)
	kids := map[[key.KidSize]byte]bool{}
	envs := 0
	err := c.Vessel().Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type != vessel.RecEvent || h.ID != id {
			return nil
		}
		env, err := c.Vessel().Body(ref)
		if err != nil {
			return err
		}
		envs++
		ks, err := key.Kids(env)
		if err != nil {
			return err
		}
		for _, kid := range ks {
			kids[kid] = true
		}
		return nil
	})
	if err != nil || envs == 0 {
		t.Fatalf("the envelopes of %s: %d, %v", id.Short(), envs, err)
	}
	return kids
}

// A writer key's own passphrase records inside its grant: the record is
// signed by the key's signer under its grant, never by the root, and sealed
// at its own point to the owner and to every live key whose reads cover its
// address (E3, E4), the system reader too where the address is read-open.
// Keys whose reads do not cover it, the writer itself included (it reads
// nothing, K2), do not read it. The same attempt again records nothing new,
// and rokh attempt finds it with the key's passphrase. Without --key the
// write is the session key's (contract section 5, B3).
func TestAWriterKeyRecordsInsideItsGrantWithItsOwnPassphrase(t *testing.T) {
	k := keyedCarrier(t)
	writer := keyringAdd(t, k.dir, "writer")
	reader := keyringAdd(t, k.dir, "reader")
	body := []byte("written by the writer key")
	ans, err := k.write(t, "writer", "--key", "writer", "--address", "journal/by-writer", "--message", string(body), "--attempt", "writer-1")
	if err != nil || ans["record"] != "recorded" {
		t.Fatalf("the writer's write inside its grant: %v %v", ans, err)
	}
	id, err := frame.ParseID(ans["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	c := openForTest(t, k.dir, bondPass)
	l := ledgerOf(t, c)
	e, ok := l.Get(id)
	if !ok || e.HeadOnly {
		t.Fatal("the owner does not hold the writer's record whole")
	}
	if !bytes.Equal(e.Event.Author, writer.Signer) || bytes.Equal(e.Event.Author, l.Root()) {
		t.Fatalf("the record is signed by %x, not by the writer's signer %x", e.Event.Author, []byte(writer.Signer))
	}
	if e.Event.Authority == nil {
		t.Fatal("the writer's record names no grant")
	}
	ge, _ := l.Get(*e.Event.Authority)
	if g, err := event.DecodeGrant(ge.Event.Payload); err != nil || g.Open || !bytes.Equal(g.Subject, writer.Signer) || !g.Allows("journal/by-writer", "note") {
		t.Fatalf("the writer's record names a grant that is not its own: %+v %v", g, err)
	}
	sl, err := v1Sealer(c, bondPass)
	if err != nil {
		t.Fatal(err)
	}
	owner := sl.(*key.Session)
	want := map[[key.KidSize]byte]bool{key.Kid(owner.Ring.Owner()[0]): true, key.Kid(reader.Reader): true}
	if got := kidsOf(t, k.dir, id); len(got) != len(want) || !got[key.Kid(owner.Ring.Owner()[0])] || !got[key.Kid(reader.Reader)] {
		t.Fatalf("the writer's record is sealed to %d readers, not to the owner and the reader alone (E4)", len(got))
	}
	for who, want := range map[string]bool{"owner": true, "reader": true, "spy": false, "blind": false, "writer": false} {
		if got := k.sees(t, who, "journal/by-writer", body); got != want {
			t.Fatalf("%s reads the writer's record: %v, want %v", who, got, want)
		}
	}

	// The same attempt again. The writer reads nothing, so the record that
	// carries its attempt is a head to it: it cannot find the first result,
	// and it records nothing a second time; rokh attempt says unknown with
	// its passphrase and finds the record with the owner's.
	before := k.generation(t)
	ans, err = k.write(t, "writer", "--key", "writer", "--address", "journal/by-writer", "--message", string(body), "--attempt", "writer-1")
	if err == nil || ans["record"] != "not-recorded" || ans["code"] != "attempt_unsupported" {
		t.Fatalf("the same attempt again by a key that cannot read it: %v %v", ans, err)
	}
	if k.generation(t) != before {
		t.Fatal("the same attempt again committed")
	}
	out, errOut, err := bondRun(t, cmdAttempt, k.dir, "--passphrase-file", k.files["writer"], "--attempt", "writer-1", "--json")
	var st exitStatus
	if !errors.As(err, &st) || st.code != exitUnknown || !strings.Contains(out, `"record":"unknown"`) {
		t.Fatalf("rokh attempt with the writer's passphrase: %v\n%s%s", err, out, errOut)
	}
	out, errOut, err = bondRun(t, cmdAttempt, k.dir, "--passphrase-file", k.files["owner"], "--attempt", "writer-1",
		"--author", hex.EncodeToString(writer.Signer), "--json")
	if err != nil || !strings.Contains(out, `"record":"recorded"`) || !strings.Contains(out, id.String()) {
		t.Fatalf("rokh attempt with the owner's passphrase: %v\n%s%s", err, out, errOut)
	}

	// Without --key: the session's own key signs.
	ans, err = k.write(t, "writer", "--address", "journal/no-key", "--message", "the session key")
	if err != nil || ans["record"] != "recorded" {
		t.Fatalf("a write without --key under the writer's passphrase: %v %v", ans, err)
	}
	nid, _ := frame.ParseID(ans["id"].(string))
	if ne, _ := ledgerOf(t, openForTest(t, k.dir, bondPass)).Get(nid); !bytes.Equal(ne.Event.Author, writer.Signer) {
		t.Fatal("a write without --key under the writer's passphrase was not the writer's")
	}

	// A read-open address inside the grant: the system reader reads it too.
	mustRun(t, cmdGrant, k.dir, "--open", "--read-open", "--scope", "journal/public")
	ans, err = k.write(t, "writer", "--key", "writer", "--address", "journal/public/notice", "--message", "open to every key")
	if err != nil || ans["record"] != "recorded" {
		t.Fatalf("the writer's write at a read-open address: %v %v", ans, err)
	}
	pid, _ := frame.ParseID(ans["id"].(string))
	if got := kidsOf(t, k.dir, pid); len(got) != 3 || !got[key.Kid(owner.System)] {
		t.Fatalf("a record at a read-open address is sealed to %d readers, without the system reader (E4)", len(got))
	}
	for who, want := range map[string]bool{"spy": true, "blind": true, "writer": true} {
		if got := k.sees(t, who, "journal/public/notice", []byte("open to every key")); got != want {
			t.Fatalf("%s reads the read-open record: %v, want %v", who, got, want)
		}
	}
}

// Outside its grant a writer key is refused before anything is signed, and
// nothing is recorded: another scope, a verb its grant does not hold, a
// reserved verb, a branch whose head is before its grant, the root, and
// another key.
func TestAWriterKeyOutsideItsGrantRecordsNothing(t *testing.T) {
	k := keyedCarrier(t)
	mustRun(t, cmdGrant, k.dir, "--to", "writer", "--scope", "notes", "--verbs", "note")
	before := k.generation(t)
	for _, c := range []struct {
		why, code string
		args      []string
	}{
		{"another scope", "view_denied", []string{"--key", "writer", "--address", "private/forbidden", "--message", "x"}},
		{"a verb its grant does not hold", "view_denied", []string{"--key", "writer", "--address", "notes/today", "--verb", "edit", "--message", "x"}},
		{"a reserved verb", "view_denied", []string{"--key", "writer", "--address", event.AddressRoot, "--verb", event.VerbMerge}},
		{"a branch that starts before its grant", "view_denied", []string{"--key", "writer", "--branch", "elsewhere", "--address", "journal/x", "--message", "x"}},
		{"the root named", "root_refused", []string{"--key", "root", "--address", "journal/x", "--message", "x"}},
		{"another key named", "key_unknown", []string{"--key", "reader", "--address", "journal/x", "--message", "x"}},
	} {
		ans, err := k.write(t, "writer", c.args...)
		if err == nil || ans["record"] != "not-recorded" || ans["code"] != c.code {
			t.Fatalf("%s: %v %v", c.why, ans, err)
		}
	}
	if after := k.generation(t); after != before {
		t.Fatalf("a refused write committed: generation %d, then %d", before, after)
	}
	// Inside the second grant, with its one verb: recorded.
	if ans, err := k.write(t, "writer", "--key", "writer", "--address", "notes/today", "--verb", "note", "--message", "x"); err != nil || ans["record"] != "recorded" {
		t.Fatalf("inside the writer's second grant: %v %v", ans, err)
	}
}

// A key without the right to write records nothing, with --key or without;
// a revoked writer opens nothing and so writes nothing.
func TestAKeyWithoutTheRightToWriteRecordsNothing(t *testing.T) {
	k := keyedCarrier(t)
	before := k.generation(t)
	for _, who := range []string{"reader", "spy", "blind"} {
		for _, args := range [][]string{
			{"--address", "journal/x", "--message", "x"},
			{"--key", who, "--address", "journal/x", "--message", "x"},
		} {
			ans, err := k.write(t, who, args...)
			if err == nil || ans["record"] != "not-recorded" || ans["code"] != "view_denied" {
				t.Fatalf("%s wrote %v: %v %v", who, args, ans, err)
			}
		}
	}
	if after := k.generation(t); after != before {
		t.Fatalf("a refused write committed: generation %d, then %d", before, after)
	}
	mustRun(t, cmdKey, "revoke", k.dir, "--key", "writer")
	before = k.generation(t)
	if _, errOut, err := bondRun(t, cmdWrite, k.dir, "--passphrase-file", k.files["writer"], "--key", "writer", "--address", "journal/x", "--message", "x"); err == nil {
		t.Fatalf("a revoked writer wrote: %s", errOut)
	}
	if after := k.generation(t); after != before {
		t.Fatalf("a revoked writer's write committed: generation %d, then %d", before, after)
	}
}

// A key that reads where it writes finds its attempt as the owner does: the
// same attempt again is the first result and records nothing new, the same
// name for another request is a conflict, and rokh attempt with the key's
// passphrase finds it.
func TestAKeyThatReadsWhereItWritesKeepsItsAttempts(t *testing.T) {
	k := keyedCarrier(t)
	k.files["scribe"] = writeFile(t, "scribe.pass", []byte("synthetic passphrase of scribe"))
	mustRun(t, cmdKey, "add", k.dir, "--name", "scribe", "--reads", "journal", "--write", "--scope", "journal",
		"--key-passphrase-file", k.files["scribe"])
	ans, err := k.write(t, "scribe", "--address", "journal/scribe", "--message", "once", "--attempt", "scribe-1")
	if err != nil || ans["record"] != "recorded" {
		t.Fatalf("the scribe's write: %v %v", ans, err)
	}
	id := ans["id"]
	before := k.generation(t)
	ans, err = k.write(t, "scribe", "--address", "journal/scribe", "--message", "once", "--attempt", "scribe-1")
	if err != nil || ans["already"] != true || ans["id"] != id {
		t.Fatalf("the same attempt again: %v %v", ans, err)
	}
	ans, err = k.write(t, "scribe", "--address", "journal/scribe", "--message", "twice", "--attempt", "scribe-1")
	if err == nil || ans["code"] != "attempt_conflict" {
		t.Fatalf("another request under the same attempt: %v %v", ans, err)
	}
	if k.generation(t) != before {
		t.Fatal("a repeated or conflicting attempt committed")
	}
	out, errOut, err := bondRun(t, cmdAttempt, k.dir, "--passphrase-file", k.files["scribe"], "--attempt", "scribe-1", "--json")
	if err != nil || !strings.Contains(out, `"record":"recorded"`) || !strings.Contains(out, id.(string)) {
		t.Fatalf("rokh attempt with the scribe's passphrase: %v\n%s%s", err, out, errOut)
	}
}
