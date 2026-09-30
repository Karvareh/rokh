package working

import (
	"os"
	"path/filepath"
	"testing"

	"rokh/frame"
)

// Opening the working state as a real folder means a real folder: actual
// directories, actual files, editable by whatever you edit things in. Not a
// folder-shaped interface over something else.
//
//	— T13.3, T4.4
func TestTheWorkingStateOpensAsARealFolder(t *testing.T) {
	root := t.TempDir()
	st, err := Open(Folder{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	h, err := st.Write(Draft{Address: "home/journal", Verb: "note",
		Payload: []byte("the first sentence")})
	if err != nil {
		t.Fatal(err)
	}

	// The files are there, with the plain names a person would look for.
	dir := filepath.Join(root, string(h))
	for name, want := range map[string]string{
		"address": "home/journal\n",
		"verb":    "note\n",
		"payload": "the first sentence",
	} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(b) != want {
			t.Fatalf("%s reads %q, expected %q", name, b, want)
		}
	}
	// The payload is byte for byte what was written, with nothing wrapped
	// around it: an editor opens the payload, not a rendering of it.
	b, _ := os.ReadFile(filepath.Join(dir, "payload"))
	if len(b) != len("the first sentence") {
		t.Fatal("the payload was not written as itself")
	}
}

// Editing the file is editing the draft. There is no import step, because
// there is nothing between the folder and the working state to import across.
//
//	— T13.3, T4.4
func TestEditingTheFileWithAnyToolIsRevisingTheDraft(t *testing.T) {
	root := t.TempDir()
	st, err := Open(Folder{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	h, err := st.Write(Draft{Address: "home/journal", Verb: "note",
		Payload: []byte("a first go")})
	if err != nil {
		t.Fatal(err)
	}

	// Somebody opens the payload in an editor and saves. Rokh is not involved.
	payload := filepath.Join(root, string(h), "payload")
	if err := os.WriteFile(payload, []byte("what I actually meant"), 0o600); err != nil {
		t.Fatal(err)
	}
	// And a text editor leaves a trailing newline on the address; the address
	// is a name, not a line, so it survives that.
	if err := os.WriteFile(filepath.Join(root, string(h), "address"),
		[]byte("home/letters\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := st.Read(h)
	if err != nil {
		t.Fatal(err)
	}
	if string(d.Payload) != "what I actually meant" {
		t.Fatalf("the edit did not reach the draft: %q", d.Payload)
	}
	if d.Address != "home/letters" {
		t.Fatalf("the address reads %q", d.Address)
	}

	// It can be changed as many times as one likes, and none of it is final.
	for _, s := range []string{"again", "and again", "once more"} {
		if err := os.WriteFile(payload, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		d, err := st.Read(h)
		if err != nil {
			t.Fatal(err)
		}
		if string(d.Payload) != s {
			t.Fatalf("revision %q did not take", s)
		}
	}
}

// counter is a Recorder that only counts, so a test can ask how many times
// anything crossed the boundary.
type counter struct{ n int }

func (c *counter) Record(Draft) (frame.ID, error) {
	c.n++
	return frame.Hash([]byte("recorded")), nil
}

// The folder is the working state and nothing more. Saving a file records
// nothing, listing records nothing, reading records nothing, and closing the
// editor records nothing. One explicit act crosses, and it is the only one.
//
//	— T13.3, T4.4, T4.5, N-Axiom2
func TestSavingAFileRecordsNothingAndOnlyClosingCrosses(t *testing.T) {
	root := t.TempDir()
	rec := &counter{}
	st, err := Open(Folder{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	h, err := st.Write(Draft{Address: "home/journal", Verb: "note",
		Payload: []byte("one")})
	if err != nil {
		t.Fatal(err)
	}

	payload := filepath.Join(root, string(h), "payload")
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(payload, []byte{byte('a' + i)}, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Read(h); err != nil {
			t.Fatal(err)
		}
		if _, err := st.List(); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Preview(h, nil, "the root grant"); err != nil {
			t.Fatal(err)
		}
	}
	if rec.n != 0 {
		t.Fatalf("%d things crossed the boundary without being closed", rec.n)
	}

	if _, err := st.Close(h, rec); err != nil {
		t.Fatal(err)
	}
	if rec.n != 1 {
		t.Fatalf("closing recorded %d times", rec.n)
	}
	// And the folder is gone, because it is no longer a draft.
	if _, err := os.Stat(filepath.Join(root, string(h))); !os.IsNotExist(err) {
		t.Fatal("the draft's folder outlived the draft")
	}
}

// Reopening resumes. A folder that was there before opening is still there
// after, unrecorded and unlost, and the numbering carries on rather than
// colliding with what is already in it.
//
//	— T13.3, T8.2
func TestReopeningTheFolderResumesAndRecordsNothing(t *testing.T) {
	root := t.TempDir()
	f := Folder{Root: root}
	st, err := Open(f)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.Write(Draft{Address: "home/a", Verb: "note", Payload: []byte("A")})
	if err != nil {
		t.Fatal(err)
	}

	rec := &counter{}
	again, err := Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if rec.n != 0 {
		t.Fatal("opening recorded something")
	}
	hs, err := again.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 1 || hs[0] != first {
		t.Fatalf("the waiting draft came back as %v", hs)
	}
	d, err := again.Read(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(d.Payload) != "A" {
		t.Fatalf("the draft came back as %q", d.Payload)
	}
	// The next handle does not land on one already taken.
	second, err := again.Write(Draft{Address: "home/b", Verb: "note", Payload: []byte("B")})
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("reopening reused a handle that was taken")
	}

	// Anything else in the folder is not a draft and is passed over rather
	// than guessed at.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scratch"), 0o700); err != nil {
		t.Fatal(err)
	}
	hs, err = again.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 {
		t.Fatalf("the folder read %d drafts where two were written: %v", len(hs), hs)
	}
}
