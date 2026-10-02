package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/event"
)

// A folder is known by what is on it, and by nothing else: no path is guessed,
// and no folder is taken for a vault because of where it is or what it is
// called. A ledgers folder with no carrier in it is not a Rokh either.
//
//	— T4.1, T1.2, T8
func TestAFolderIsKnownByWhatIsOnIt(t *testing.T) {
	root := t.TempDir()
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	empty := mk("empty")
	finder := mk("finder")
	if err := os.WriteFile(filepath.Join(finder, ".DS_Store"), []byte{0}, 0o600); err != nil {
		t.Fatal(err)
	}
	other := mk("other")
	if err := os.WriteFile(filepath.Join(other, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	hollow := mk("hollow")
	mk("hollow", LedgersFolder, "home")
	vault := filepath.Join(root, "vault")
	s, err := makeRokh(vault, "", "", "pass", "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	s.closeAll()

	cases := map[string]folderKind{
		filepath.Join(root, "missing"): kindMissing,
		file:                           kindNotAFolder,
		empty:                          kindEmpty,
		finder:                         kindEmpty,
		other:                          kindOther,
		hollow:                         kindOther,
		vault:                          kindRokh,
	}
	for dir, want := range cases {
		if got := inspect(dir); got != want {
			t.Errorf("%s: inspected as %d, want %d", dir, got, want)
		}
	}
}

// Making a Rokh records, as its first event, the one sentence that would have
// made it at the prompt, with the name the person gave; the parser knows that
// sentence as exactly that act. The folder is then a Rokh that opens with its
// passphrase and refuses another, and holds the ledgers folder and nothing
// else. A name that cannot be a folder makes nothing, not even the folder.
//
//	— T8.2, T4.1, T8.6
func TestMakingARokhRecordsTheSentenceThatWouldHaveMadeIt(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "new", "rokh") // not there yet; made
	s, err := makeRokh(vault, "", "", "pass", "خانه", 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.current.car.Get(s.current.led.Genesis())
	if err != nil {
		t.Fatal(err)
	}
	sg, err := event.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	// The making is recorded in the words of the making, and nothing else:
	// the room lives in the vessel, the size the person named, in whole
	// slabs, where it can be changed and genesis could not follow it.
	want := newLedgerStem + "خانه"
	if got := string(sg.Event.Payload); got != want {
		t.Fatalf("the first event says %q, want %q", got, want)
	}
	if info := s.current.car.Vessel().Info(); int64(info.Slabs)*int64(info.SlabSize) != 8<<20 {
		t.Fatalf("the vessel holds %d slabs of %d bytes; 8 MB was named", info.Slabs, info.SlabSize)
	}
	first, _, _ := strings.Cut(string(sg.Event.Payload), "\n")
	c, err := parse(first)
	if err != nil || c.Op != opOpenNew || c.Name != "خانه" {
		t.Fatalf("the parser does not know the recorded sentence as making a ledger: %+v %v", c, err)
	}
	if err := s.closeAll(); err != nil {
		t.Fatal(err)
	}

	if inspect(vault) != kindRokh {
		t.Fatal("the folder is not seen as a Rokh afterwards")
	}
	entries, err := os.ReadDir(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != LedgersFolder {
		t.Fatalf("the vault holds %v; only the ledgers folder was to be made", entries)
	}
	dir := filepath.Join(vault, LedgersFolder, "خانه")
	if err := tryPass(dir, "wrong"); err == nil {
		t.Fatalf("a wrong passphrase was answered with %v", err)
	}
	if err := tryPass(dir, "pass"); err != nil {
		t.Fatalf("the right passphrase was refused: %v", err)
	}

	bad := filepath.Join(t.TempDir(), "bad")
	if _, err := makeRokh(bad, "", "", "pass", "a/b", 0); err == nil {
		t.Fatal("a name with a slash made a Rokh")
	}
	if inspect(bad) != kindMissing {
		t.Fatal("a refused name still made the folder")
	}
	if _, err := makeRokh(bad, "", "", "pass", "", 0); err == nil {
		t.Fatal("no name made a Rokh")
	}
}
