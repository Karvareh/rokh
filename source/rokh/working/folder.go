package working

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Folder opens the working state as a real folder.
//
// Not a folder-shaped abstraction: an actual directory of actual files that
// any editor can open and any tool can read. One directory per draft, and
// inside it the address, the verb and the payload, each in its own file. Edit
// the payload in whatever you edit things in, and the draft has changed —
// there is no import step, because the file *is* the draft.
//
// This is the point of the working state made literal. What you see on screen
// and what you have done is not final and may be changed as often as you like;
// one explicit act turns it into an event. A folder makes both halves obvious:
// ordinary tools reach the first, and nothing whatsoever reaches the second.
// No watcher notices the save, no timer picks it up, and closing the editor
// records nothing.
//
// Where it may live is not a free choice. Rokh writes nothing outside the
// carrier, so a durable working state belongs under the carrier's own
// directory, sealed with the rest. Root is taken rather than guessed so that
// whoever holds the carrier decides.
//
//	— T13.3, T4.4, T4.5, T8
type Folder struct{ Root string }

// The three files a draft is made of, plus the optional fourth. Their names
// are the plain words, because a person reads them.
const (
	fileAddress = "address"
	fileVerb    = "verb"
	filePayload = "payload"
	fileAttest  = "attest.json"
)

// ErrBadDraftName is returned for a handle that is not a plain name.
var ErrBadDraftName = errors.New("working: a draft name must be a plain name")

func (f Folder) dir(name string) (string, error) {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%w: %q", ErrBadDraftName, name)
	}
	return filepath.Join(f.Root, name), nil
}

// Put writes a draft out as files. The payload is written as it is, byte for
// byte, so that what an editor opens is the payload and not a rendering of it.
func (f Folder) Put(name string, b []byte) error {
	dir, err := f.dir(name)
	if err != nil {
		return err
	}
	var d Draft
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	write := func(base string, content []byte) error {
		return os.WriteFile(filepath.Join(dir, base), content, 0o600)
	}
	if err := write(fileAddress, []byte(d.Address+"\n")); err != nil {
		return err
	}
	if err := write(fileVerb, []byte(d.Verb+"\n")); err != nil {
		return err
	}
	if err := write(filePayload, d.Payload); err != nil {
		return err
	}
	if len(d.Attest) == 0 {
		os.Remove(filepath.Join(dir, fileAttest))
		return nil
	}
	att, err := json.MarshalIndent(d.Attest, "", "  ")
	if err != nil {
		return err
	}
	return write(fileAttest, att)
}

// Get reads a draft back out of the files as they stand *now*.
//
// So an edit made with any other tool is simply there the next time anyone
// looks. That is what makes it a working state rather than a copy of one: it
// has no private idea of the draft that the folder could fall behind.
//
//	— T4.4, T13.3
func (f Folder) Get(name string) ([]byte, bool, error) {
	dir, err := f.dir(name)
	if err != nil {
		return nil, false, err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, false, nil
	}
	read := func(base string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, base))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return b, err
	}
	addr, err := read(fileAddress)
	if err != nil {
		return nil, false, err
	}
	verb, err := read(fileVerb)
	if err != nil {
		return nil, false, err
	}
	payload, err := read(filePayload)
	if err != nil {
		return nil, false, err
	}
	d := Draft{
		// A trailing newline is what a text editor leaves behind, and an
		// address is not a line of text. The payload keeps every byte it has.
		Address: strings.TrimRight(string(addr), "\r\n"),
		Verb:    strings.TrimRight(string(verb), "\r\n"),
		Payload: payload,
	}
	if att, err := read(fileAttest); err != nil {
		return nil, false, err
	} else if len(att) > 0 {
		if err := json.Unmarshal(att, &d.Attest); err != nil {
			return nil, false, err
		}
	}
	out, err := json.Marshal(d)
	return out, true, err
}

// Delete throws the folder away. It leaves nothing behind, because the draft
// never was an event.
//
//	— T4.4
func (f Folder) Delete(name string) error {
	dir, err := f.dir(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// List names the drafts waiting, which is to say the directories that are
// there. A file dropped in by hand is not a draft and is passed over rather
// than guessed at.
func (f Folder) List() ([]string, error) {
	entries, err := os.ReadDir(f.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(f.Root, e.Name(), fileAddress)); err != nil {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

var _ Store = Folder{}
