package main

import (
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// copyVessel copies a carrier's vessel folder file by file, as a person
// copies a folder: the copy is the same vessel, to reconcile in another order.
func copyVessel(t *testing.T, from, to string) {
	t.Helper()
	root := filepath.Join(from, "rokh")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// installedLine is the line of a reconcile's answer that says a side
// installed key cells.
var installedLine = regexp.MustCompile(`(?m)^(local|remote): \d+ key cells? installed$`)

// opensNothing says that a role's passphrase opens nothing of a carrier
// through the command: its log is refused.
func (w *seedWorld) opensNothing(role, dir string) bool {
	w.t.Helper()
	out, _, code := w.run(role, "log", dir, "--json")
	return code != 0 && strings.TrimSpace(out) == ""
}

// Step 2 (contract 4.8 R2; a ruling of the design), through the
// command line: two whole seeds of one root change their keyrings apart. On
// a, a key is added, and another is added and taken back; on b, another key
// is added, and a key both seeds hold is taken back. Under a key's passphrase
// a reconcile unites the records and installs no cell, and says so on each
// side. The owner's reconcile then installs, on each side, the cell of the
// key the other side added, in both orders (the second from copies made
// before any reconcile): each of those keys opens the other side with its own
// passphrase and sees its own view there, byte for byte. A key taken back on
// one side opens nothing on either side after the reconcile, and a key added
// and taken back apart has a cell on neither. Reconciling again records
// nothing and writes no byte.
func TestAReconcileInstallsTheCellsOfLiveKeysOnBothSides(t *testing.T) {
	w := newSeedWorld(t)
	src, a, b := w.dir("src"), w.dir("a"), w.dir("b")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "all", "--reads", "*", "--key-passphrase-file", w.pass("all"))
	w.must("owner", "key", "add", src, "--name", "gone", "--reads", "journal", "--key-passphrase-file", w.pass("gone"))
	w.must("owner", "write", src, "--address", "journal/day", "--message", "JOURNAL: written before the seeds")
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", a)
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", b)
	w.must("owner", "key", "add", a, "--name", "alpha", "--reads", "journal", "--key-passphrase-file", w.pass("alpha"))
	w.must("owner", "key", "add", a, "--name", "brief", "--reads", "journal", "--key-passphrase-file", w.pass("brief"))
	w.must("owner", "key", "revoke", a, "--key", "brief")
	w.must("owner", "key", "add", b, "--name", "beta", "--reads", "work", "--key-passphrase-file", w.pass("beta"))
	w.must("owner", "key", "revoke", b, "--key", "gone")
	w.must("owner", "write", a, "--address", "journal/a", "--message", "JOURNAL: written on a")
	w.must("owner", "write", b, "--address", "work/b", "--message", "WORK: written on b")
	a2, b2 := w.dir("a2"), w.dir("b2")
	copyVessel(t, a, a2)
	copyVessel(t, b, b2)
	if _, ok := w.opensOwnCell("alpha", b); ok {
		t.Fatal("alpha has a cell on b before any reconcile")
	}

	// Under a key's passphrase: the union, and no cell.
	out := w.must("all", "reconcile", a, b)
	if !strings.Contains(out, "local recorded (+") || !strings.Contains(out, "remote recorded (+") ||
		strings.Count(out, "no key cell installed") != 2 || installedLine.MatchString(out) {
		t.Fatalf("a key's reconcile: %q", out)
	}
	if _, ok := w.opensOwnCell("alpha", b); ok {
		t.Fatal("a reconcile under a key's passphrase installed a cell")
	}

	// The owner's, in both orders.
	for _, pair := range [][2]string{{a, b}, {b2, a2}} {
		out := w.must("owner", "reconcile", pair[0], pair[1])
		if !strings.Contains(out, "local recorded (+") || !strings.Contains(out, "remote recorded (+") ||
			!strings.Contains(out, "local: 1 key cell installed") || !strings.Contains(out, "remote: 1 key cell installed") {
			t.Fatalf("reconcile %s %s: %q", filepath.Base(pair[0]), filepath.Base(pair[1]), out)
		}
	}
	journal := func(role, dir string) map[string]string {
		got := bodies(w.log(role, dir))
		for addr := range got {
			if !strings.HasPrefix(addr, "journal/") {
				t.Fatalf("%s reads %s on %s", role, addr, filepath.Base(dir))
			}
		}
		return got
	}
	// A key reads what was sealed to it: what was written in its reads after
	// its add (K4), here journal/a.
	wantAlpha := journal("alpha", a)
	if len(wantAlpha) != 1 || wantAlpha["journal/a"] == "" {
		t.Fatalf("alpha's view on a: %v", wantAlpha)
	}
	for _, d := range []string{b, a2, b2} {
		if got := journal("alpha", d); !reflect.DeepEqual(got, wantAlpha) {
			t.Fatalf("alpha's view on %s is not its view on a:\n%v\n%v", filepath.Base(d), got, wantAlpha)
		}
	}
	wantBeta := bodies(w.log("beta", b))
	if len(wantBeta) != 1 || wantBeta["work/b"] == "" {
		t.Fatalf("beta's view on b: %v", wantBeta)
	}
	for _, d := range []string{a, a2, b2} {
		if got := bodies(w.log("beta", d)); !reflect.DeepEqual(got, wantBeta) {
			t.Fatalf("beta's view on %s is not its view on b:\n%v\n%v", filepath.Base(d), got, wantBeta)
		}
	}
	for _, d := range []string{a, b, a2, b2} {
		for _, role := range []string{"gone", "brief"} {
			if !w.opensNothing(role, d) {
				t.Fatalf("%s, taken back, opens %s after the reconcile", role, filepath.Base(d))
			}
		}
		if _, ok := w.opensOwnCell("brief", d); ok && (d == b || d == b2) {
			t.Fatalf("brief, added and taken back on a, has a cell on %s", filepath.Base(d))
		}
		if got := bodies(w.log("all", d)); len(got) != 3 {
			t.Fatalf("the key that reads everything reads %d bodies on %s", len(got), filepath.Base(d))
		}
	}

	fa, fb := w.digests(a), w.digests(b)
	out = w.must("owner", "reconcile", a, b)
	if !strings.Contains(out, "local recorded (+0") || !strings.Contains(out, "remote recorded (+0") || strings.Contains(out, "installed") {
		t.Fatalf("a second reconcile: %q", out)
	}
	if installedLine.MatchString(out) {
		t.Fatalf("a second reconcile installed a cell: %q", out)
	}
	if !reflect.DeepEqual(w.digests(a), fa) || !reflect.DeepEqual(w.digests(b), fb) {
		t.Fatal("a second reconcile wrote")
	}
}

// sideLines are the lines a reconcile's answer gives for one side, without
// the side's name: its heads and its concurrent keys.
func sideLines(t *testing.T, out, side string) []string {
	t.Helper()
	var got []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, side+": "); ok && (strings.HasPrefix(rest, "heads ") || strings.HasPrefix(rest, "concurrent keys: ")) {
			got = append(got, rest)
		}
	}
	if len(got) != 2 {
		t.Fatalf("the answer does not show the %s side's heads and concurrent keys:\n%s", side, out)
	}
	return got
}

// keyLines is `rokh key list` of a carrier, its lines sorted.
func (w *seedWorld) keyLines(dir string) []string {
	w.t.Helper()
	var out []string
	for _, line := range strings.Split(w.must("owner", "key", "list", dir), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

// headsOf is the heads line of `rokh verify`.
func (w *seedWorld) headsOf(dir string) string {
	w.t.Helper()
	for _, line := range strings.Split(w.must("owner", "verify", dir), "\n") {
		if strings.HasPrefix(line, "heads") {
			return strings.Join(strings.Fields(line)[1:], " ")
		}
	}
	w.t.Fatalf("verify of %s shows no heads", filepath.Base(dir))
	return ""
}

// Step 3 (contract 4.4, R7; acceptance B4), through the command line, in
// both orders: two whole seeds change the keyring at the same time. Each
// rotates the key helper, so each holds a generation 2 of it the other never
// saw; one takes the key other back while the other rotates it; and each
// writes. After a reconcile, both sides list the same concurrent keys and the
// same heads, and the reconcile's answer shows them for each side: helper
// with its two generations, two heads, and nothing chosen. The key other is
// not concurrent: a revoke does not touch an add it never saw, and the
// generation made on one side is the one live generation. Every copy, in
// either order, ends with the same keys and the same heads, the two
// generations of helper each open both sides with their own passphrases, and
// nothing was merged or revoked by the reconcile.
func TestAConflictIsShownOnBothSidesAndNothingIsChosen(t *testing.T) {
	w := newSeedWorld(t)
	src, a, b := w.dir("src"), w.dir("a"), w.dir("b")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	for _, name := range []string{"helper", "other"} {
		w.must("owner", "key", "add", src, "--name", name, "--reads", "journal", "--key-passphrase-file", w.pass(name))
	}
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", a)
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", b)
	w.must("owner", "key", "rotate", a, "--key", "helper", "--key-passphrase-file", w.pass("helper-a"))
	w.must("owner", "key", "rotate", b, "--key", "helper", "--key-passphrase-file", w.pass("helper-b"))
	w.must("owner", "key", "revoke", a, "--key", "other")
	w.must("owner", "key", "rotate", b, "--key", "other", "--key-passphrase-file", w.pass("other-b"))
	w.must("owner", "write", a, "--address", "journal/a", "--message", "JOURNAL: written on a")
	w.must("owner", "write", b, "--address", "journal/b", "--message", "JOURNAL: written on b")
	a2, b2 := w.dir("a2"), w.dir("b2")
	copyVessel(t, a, a2)
	copyVessel(t, b, b2)

	var shown [][]string
	for _, pair := range [][2]string{{a, b}, {b2, a2}} {
		out := w.must("owner", "reconcile", pair[0], pair[1])
		local, remote := sideLines(t, out, "local"), sideLines(t, out, "remote")
		if !reflect.DeepEqual(local, remote) {
			t.Fatalf("the two sides do not show the same heads and keys:\n%s", out)
		}
		if !strings.Contains(out, "nothing was chosen") {
			t.Fatalf("the answer does not say that nothing was chosen:\n%s", out)
		}
		shown = append(shown, local)
	}
	if !reflect.DeepEqual(shown[0], shown[1]) {
		t.Fatalf("the two orders show different heads or keys:\n%v\n%v", shown[0], shown[1])
	}
	heads, keys := strings.Fields(strings.TrimPrefix(shown[0][0], "heads ")), strings.TrimPrefix(shown[0][1], "concurrent keys: ")
	if len(heads) != 2 {
		t.Fatalf("the two seeds' tips are not shown as two heads: %v", heads)
	}
	if !strings.HasPrefix(keys, "helper gen 2 (event ") || strings.Count(keys, "gen 2 (event ") != 2 || strings.Contains(keys, "other") || strings.Contains(keys, ";") {
		t.Fatalf("the concurrent keys shown: %q", keys)
	}

	wantKeys, wantHeads := w.keyLines(a), w.headsOf(a)
	if strings.Join(strings.Fields(wantHeads), " ") != strings.Join(heads, " ") {
		t.Fatalf("verify shows heads %q, the reconcile %v", wantHeads, heads)
	}
	for _, d := range []string{b, a2, b2} {
		if got := w.keyLines(d); !reflect.DeepEqual(got, wantKeys) {
			t.Fatalf("%s lists other keys than a:\n%s\n%s", filepath.Base(d), strings.Join(got, "\n"), strings.Join(wantKeys, "\n"))
		}
		if got := w.headsOf(d); got != wantHeads {
			t.Fatalf("%s has heads %q, a has %q", filepath.Base(d), got, wantHeads)
		}
	}
	concurrent, others := 0, 0
	for _, line := range wantKeys {
		switch {
		case strings.Contains(line, " helper ") && strings.Contains(line, "gen 2") && strings.HasSuffix(line, "concurrent"):
			concurrent++
		case strings.Contains(line, " other "):
			others++
			if !strings.Contains(line, "gen 2") || strings.HasSuffix(line, "concurrent") {
				t.Fatalf("other is listed as %q", line)
			}
		}
	}
	if concurrent != 2 || others != 1 {
		t.Fatalf("the key list does not show helper's two concurrent generations and other's one:\n%s", strings.Join(wantKeys, "\n"))
	}
	for _, d := range []string{a, b, a2, b2} {
		for _, role := range []string{"helper-a", "helper-b", "other-b"} {
			if id, ok := w.opensOwnCell(role, d); !ok || id == ([32]byte{}) {
				t.Fatalf("%s has no cell on %s after the reconcile", role, filepath.Base(d))
			}
			if out, errOut, code := w.run(role, "log", d, "--json"); code != 0 {
				t.Fatalf("%s does not open %s after the reconcile: exit %d\n%s%s", role, filepath.Base(d), code, out, errOut)
			}
		}
		for _, role := range []string{"helper", "other"} {
			if !w.opensNothing(role, d) {
				t.Fatalf("the first generation of %s, taken back, opens %s", role, filepath.Base(d))
			}
		}
	}

	// The owner chooses by command: with --merge the heads are joined on
	// this side, and the answer no longer says they stay apart; the key stays
	// concurrent, and the answer still says so.
	out := w.must("owner", "reconcile", a, "--merge", b)
	if !strings.Contains(out, "merge ") || !strings.Contains(out, ": recorded") || strings.Contains(out, "heads stay apart") ||
		!strings.Contains(out, "nothing was chosen: a concurrent key stays so") {
		t.Fatalf("an asked merge:\n%s", out)
	}
	if got := strings.Fields(w.headsOf(a)); len(got) != 1 {
		t.Fatalf("after the asked merge a has heads %v", got)
	}
}

// Step 4 (contract R5, R1; acceptance B5), through the command line: a
// reconcile one of whose halves cannot be recorded, because that seed's
// vessel has no free slab for what it is to take, is a failure in either
// order. The answer names both halves: the recorded one with its count, the
// other not recorded, with its reason and no count; the exit code is 3; the
// vessel that could not take the union is not changed by a byte; no key cell
// is installed and no heads are shown as if the whole had succeeded; and the
// recorded half stays recorded. Two different anchors are refused with exit 3
// before anything moves, and neither side changes.
func TestAReconcileWhoseOtherHalfFailsIsNoSuccess(t *testing.T) {
	w := newSeedWorld(t)
	src, big, small, other := w.dir("src"), w.dir("big"), w.dir("small"), w.dir("other")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.must("owner", "seed", src, "--size", "32M", "--slab", "256K", big)
	w.must("owner", "seed", src, "--size", "4M", "--slab", "256K", small)
	data := make([]byte, 5<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	w.bring(big, "work/archive", data)
	big2, small2 := w.dir("big2"), w.dir("small2")
	copyVessel(t, big, big2)
	copyVessel(t, small, small2)
	for _, c := range []struct{ local, remote, full, took, fails, holds string }{
		{big, small, small, big, "remote", "local"},
		{small2, big2, small2, big2, "local", "remote"},
	} {
		files := w.digests(c.full)
		out, errOut, code := w.run("owner", "reconcile", c.local, c.remote)
		if code != 3 || !strings.Contains(out, c.holds+" recorded (+") || !strings.Contains(out, c.fails+" not-recorded: ") ||
			strings.Contains(out, c.fails+" not-recorded (+") || !strings.Contains(out, c.fails+" not-recorded: vessel: full") {
			t.Fatalf("reconcile %s %s, one half of which does not fit: exit %d\n%s%s", filepath.Base(c.local), filepath.Base(c.remote), code, out, errOut)
		}
		if strings.Contains(out, "key cell") || strings.Contains(out, "heads ") {
			t.Fatalf("a failed reconcile went on as if the whole had succeeded:\n%s", out)
		}
		if !reflect.DeepEqual(w.digests(c.full), files) {
			t.Fatalf("the half that was not recorded wrote into %s", filepath.Base(c.full))
		}
		if takes := w.takesIn(c.took); len(takes) != 2 {
			t.Fatalf("the recorded half did not bring %s the other seed's take: %d takes", filepath.Base(c.took), len(takes))
		}
	}
	w.must("owner", "init", other, "--message", "another synthetic genesis", "--size", "16M", "--slab", "256K")
	fb, fo := w.digests(big), w.digests(other)
	out, errOut, code := w.run("owner", "reconcile", big, other)
	if code != 3 || !strings.Contains(out+errOut, "different anchors") || !strings.Contains(out, "local not-recorded: ") || strings.Contains(out, "(+") {
		t.Fatalf("two anchors: exit %d\n%s%s", code, out, errOut)
	}
	if !reflect.DeepEqual(w.digests(big), fb) || !reflect.DeepEqual(w.digests(other), fo) {
		t.Fatal("a refused reconcile of two anchors wrote")
	}
}

// Step 2, inside a side's own scopes: a slice of journal, reconciled with its
// root, installs the cells of the keys added on the root since the seed that
// touch journal (a reader of journal, a writer in journal) and not of a key
// that reads only work; the root, which holds them all, installs nothing.
func TestASliceLearnsTheCellsOfTheKeysThatTouchItOnly(t *testing.T) {
	w := newSeedWorld(t)
	src, slice := w.dir("src"), w.dir("slice")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "seed", src, "--scope", "journal", "--size", "16M", "--slab", "256K", slice)
	for _, k := range [][]string{
		{"journalist", "--reads", "journal"},
		{"worker", "--reads", "work"},
		{"scribe", "--write", "--scope", "journal"},
	} {
		w.must("owner", append([]string{"key", "add", src, "--name", k[0], "--key-passphrase-file", w.pass(k[0])}, k[1:]...)...)
	}
	w.must("owner", "write", src, "--address", "journal/x", "--message", "JOURNAL: written after the slice")
	w.must("owner", "write", src, "--address", "work/y", "--message", "WORK: written after the slice")
	out := w.must("owner", "reconcile", slice, src)
	if !strings.Contains(out, "local: 2 key cells installed") || strings.Contains(out, "remote: 1 key cell") || strings.Contains(out, "remote: key cells") ||
		regexp.MustCompile(`(?m)^remote: \d+ key cells installed$`).MatchString(out) {
		t.Fatalf("reconcile of the slice with its root: %q", out)
	}
	if got := bodies(w.log("journalist", slice)); len(got) != 1 || got["journal/x"] == "" {
		t.Fatalf("the journalist's view of the slice: %v", got)
	}
	if _, ok := w.opensOwnCell("scribe", slice); !ok {
		t.Fatal("the writer in journal has no cell on the slice")
	}
	if _, ok := w.opensOwnCell("worker", slice); ok || !w.opensNothing("worker", slice) {
		t.Fatal("a key that reads only work opens the slice of journal")
	}
}

// Step 2: a side whose vessel has no free cell for the keys it learns is a
// failure of the whole, named with its reason, and the union stays recorded
// on both sides: one seed holds the owner's cell and 31 keys' cells, and the
// other seed one key of its own; neither has room for what the other added,
// and neither takes a part of it.
func TestAReconcileWhoseCellsDoNotFitIsAFailure(t *testing.T) {
	w := newSeedWorld(t)
	src, a, b := w.dir("src"), w.dir("a"), w.dir("b")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", a)
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", b)
	for i := 1; i <= 31; i++ {
		name := fmt.Sprintf("k%02d", i)
		w.must("owner", "key", "add", a, "--name", name, "--reads", "journal", "--key-passphrase-file", w.pass(name))
	}
	w.must("owner", "key", "add", b, "--name", "late", "--reads", "journal", "--key-passphrase-file", w.pass("late"))
	out, errOut, code := w.run("owner", "reconcile", a, b)
	if code != 3 || !strings.Contains(out, "local recorded (+") || !strings.Contains(out, "remote recorded (+") ||
		!strings.Contains(out, "local: key cells not installed") || !strings.Contains(out, "remote: key cells not installed") ||
		!strings.Contains(errOut, "the union is recorded on both sides") {
		t.Fatalf("a reconcile whose cells do not fit: exit %d\n%s%s", code, out, errOut)
	}
	if !sameIDs(w.log("owner", a), w.log("owner", b)) {
		t.Fatal("the union is not recorded on both sides")
	}
	if _, ok := w.opensOwnCell("late", a); ok {
		t.Fatal("a cell was installed where there was no room for all")
	}
}
