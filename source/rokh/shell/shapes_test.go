package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both shapes open. A carrier made by rokh init, named on its own, is a
// Rokh and the one ledger of the session, named after its folder: it is
// read and written like any, listed as the one ledger, and a second ledger
// is not made inside it, with the reason. A folder of ledgers made by the
// gate opens as before.
func TestBothShapesOpen(t *testing.T) {
	f := makeFixture(t)
	carrier := filepath.Join(f.vault, "ledgers", f.name) // a carrier, named on its own
	if inspect(carrier) != kindRokh || inspect(f.vault) != kindRokh {
		t.Fatalf("a carrier is %d and a folder of ledgers is %d; both are a Rokh", inspect(carrier), inspect(f.vault))
	}
	s := sessionOn(carrier, "", "", testPass)
	defer s.closeAll()
	if names := s.ledgerNames(); len(names) != 1 || names[0] != f.name {
		t.Fatalf("the carrier's session lists %v", names)
	}
	if got := run(t, s, "see the ledger"); !strings.Contains(got, "all verified") {
		t.Fatalf("see the ledger on a carrier: %q", got)
	}
	run(t, s, "write at home/journal: in the carrier")
	run(t, s, "write")
	if got := run(t, s, "read home/journal"); !strings.Contains(got, "in the carrier") {
		t.Fatalf("read on a carrier: %q", got)
	}
	if got := run(t, s, "see the ledgers"); !strings.Contains(got, "— "+f.name+" · anchor") {
		t.Fatalf("see the ledgers on a carrier: %q", got)
	}
	if _, err := runLine2(s, "open a new ledger named second"); err == nil || !strings.Contains(err.Error(), "is one ledger, made by rokh init") {
		t.Fatalf("a second ledger inside a carrier: %v", err)
	}
	if _, err := os.Stat(filepath.Join(carrier, LedgersFolder)); !os.IsNotExist(err) {
		t.Fatalf("something was made inside the carrier: %v", err)
	}

	v := sessionOn(f.vault, "", "", testPass)
	defer v.closeAll()
	if v.carrier != "" || len(v.ledgerNames()) != 1 {
		t.Fatalf("a folder of ledgers opened as %+v", v.ledgerNames())
	}
	if got := run(t, v, "read home/journal"); !strings.Contains(got, "in the carrier") {
		t.Fatalf("the folder of ledgers does not read what was written: %q", got)
	}
}
