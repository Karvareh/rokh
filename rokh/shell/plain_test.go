package shell

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/key"
	"rokh/lineage"
	"rokh/turn"
	"rokh/vessel"
)

// jargon is what a person must never be handed: a layer's name in front of
// its error, the vessel's inner words, and the numbers of rulings and
// reviews.
var jargon = []string{"vessel:", "key:", "carrier:", "turn:", "lineage:", "content:", "slab", "commit",
	"generation", "R4", "E3", "K1", "<nil>"}

// Every error the core can hand up is said in one or two plain lines that say
// what happened and what to do or that nothing changed, and the stable code
// stays in brackets after the sentence.
func TestEveryCoreErrorIsSaidPlainly(t *testing.T) {
	inner := "/x/vault/ledgers/home/rokh/000/0000000a.rkh"
	cases := []struct {
		err  error
		code string
	}{
		{vessel.ErrLocked, "passphrase_refused"},
		{key.ErrPassphrase, "passphrase_refused"},
		{fmt.Errorf("%w: 41 slabs needed, 20 free", vessel.ErrFull), "vessel_full"},
		{fmt.Errorf("%w: head 2", vessel.ErrCorrupt), "vessel_corrupt"},
		{fmt.Errorf("%w: lost", vessel.ErrTurnLost), "turn_lost"},
		{vessel.ErrTurnUnavailable, "turn_unavailable"},
		{turn.ErrUnavailable, "turn_unavailable"},
		{fmt.Errorf("another writer: %w", turn.ErrBusy), "turn_busy"},
		{vessel.ErrNotAVessel, "not_a_vessel"},
		{vessel.ErrExists, "already_exists"},
		{errOwnerEmptied, "owner_unknown"},
		{fmt.Errorf("%w: no first owner generation", key.ErrOwnerUnknown), "owner_unknown"},
		{key.ErrNoOwner, "owner_unknown"},
		{key.ErrKeyNotLive, "key_revoked"},
		{carrier.ErrForged, "record_forged"},
		{key.ErrForged, "record_forged"},
		{carrier.ErrConflict, "content_conflict"},
		{carrier.ErrIncomplete, "content_incomplete"},
		{lineage.ErrAnchorDiffers, "anchor_differs"},
		{lineage.ErrAncestryUnproven, "ancestry_unproven"},
		{lineage.ErrSeedRefused, "seed_refused"},
		{errors.New("this key's generation gives it no system reader in this carrier, so it opens no session here"), "view_denied"},
		{&fs.PathError{Op: "write", Path: inner, Err: errors.New("file too large")}, "storage_failed"},
	}
	for _, c := range cases {
		got := Plain(c.err).Error()
		if n := strings.Count(got, "\n"); n > 1 {
			t.Errorf("%v is said in %d lines: %q", c.err, n+1, got)
		}
		for _, j := range jargon {
			if strings.Contains(got, j) {
				t.Errorf("%v is said with %q in it: %q", c.err, j, got)
			}
		}
		if strings.Contains(got, inner) {
			t.Errorf("%v names a file inside the Rokh: %q", c.err, got)
		}
		if !strings.Contains(got, "["+c.code) {
			t.Errorf("%v lost its code %s: %q", c.err, c.code, got)
		}
		said := false
		for _, w := range []string{"othing was", "run:", "again", "not shown", "opens nothing", "not opened", "nothing was opened", "do not meet"} {
			if strings.Contains(got, w) {
				said = true
			}
		}
		if !said {
			t.Errorf("%v says neither what to do nor what did not change: %q", c.err, got)
		}
		if !errors.Is(Plain(c.err), c.err) {
			t.Errorf("the plain sentence for %v no longer answers for it", c.err)
		}
	}
}

// A full Rokh names the way out: the command that makes room, for the folder
// of the ledger that is full, and a size that holds more than it does.
func TestAFullRokhNamesTheWayToMakeRoom(t *testing.T) {
	s, err := makeRokh(filepath.Join(t.TempDir(), "vault"), "", "", "pass", "home", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.closeAll()
	info := s.current.car.Vessel().Info()
	want := "rokh grow " + s.current.dir + " --to " + sizeFlag(2*int64(info.Slabs)*int64(info.SlabSize))
	got := s.plain(fmt.Errorf("%w: 41 slabs needed", vessel.ErrFull)).Error()
	if !strings.Contains(got, want) || !strings.Contains(got, "nothing was recorded") || !strings.HasSuffix(got, "[vessel_full; not recorded]") {
		t.Fatalf("a full Rokh was said as %q; want the way out %q", got, want)
	}
	// Said where the recording failed, the ending decides the words: a failure
	// whose ending is unknown never says that nothing was recorded.
	r := s.current.failed(fmt.Errorf("%w: 41 slabs needed", vessel.ErrFull), "unknown").Error()
	if strings.Contains(strings.ToLower(r), "nothing was recorded") || !strings.Contains(r, "unknown") || !strings.HasSuffix(r, "[vessel_full; unknown]") {
		t.Fatalf("an unknown ending was said as %q", r)
	}
	if got := s.current.failed(&fs.PathError{Op: "write", Path: "/x", Err: errors.New("file too large")}, "not recorded").Error(); !strings.HasSuffix(got, "try again. Nothing was recorded. [storage_failed; not recorded]") {
		t.Fatalf("a disk that would not write was said as %q", got)
	}
}

func TestSizeFlags(t *testing.T) {
	for n, want := range map[int64]string{128 << 20: "128M", 2 << 30: "2G", 4 << 20: "4M", 512 << 10: "512K", 1536 << 20: "1536M"} {
		if got := sizeFlag(n); got != want {
			t.Errorf("sizeFlag(%d) = %s, want %s", n, got, want)
		}
	}
}

// A ledger the passphrase does not open is listed as shut, with the plain
// reason, and never with a Go nil.
func TestALedgerThePassphraseDoesNotOpenIsListedPlainly(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	s, err := makeRokh(vault, "", "", "pass", "home", minRoom)
	if err != nil {
		t.Fatal(err)
	}
	s.closeAll()
	out, err := newSession(vault, "", "", "wrong").doSeeLedgers()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<nil>") || !strings.Contains(out, "— home: shut; that passphrase does not open this Rokh") {
		t.Fatalf("see the ledgers said %q", out)
	}
}

// A passphrase that came from a file is tried once, and the answer says the
// passphrase does not open this Rokh, not that three were tried.
func TestAPassphraseFromAFileIsTriedOnceAndSaidPlainly(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	s, err := makeRokh(vault, "", "", "pass", "home", minRoom)
	if err != nil {
		t.Fatal(err)
	}
	s.closeAll()
	file := filepath.Join(t.TempDir(), "wrong.pass")
	if err := os.WriteFile(file, []byte("wrong\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROKH_PASSPHRASE", "")
	t.Setenv("ROKH_PASSPHRASE_FILE", file)
	_, err = unlock(io.Discard, vault, "", "")
	if err == nil || strings.Contains(err.Error(), "three") || !strings.Contains(err.Error(), "that passphrase does not open this Rokh") {
		t.Fatalf("one wrong passphrase from a file was answered %v", err)
	}
}
