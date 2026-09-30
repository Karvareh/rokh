package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
)

// T4c: a seed resumed after the source moved on, and repeated.

// slabsReadOnly makes the slab files of a folder's vessel read-only, or
// writable again: a commit there then fails before its commit point, as a
// full or pulled medium makes it fail. The head files are left as they are.
func slabsReadOnly(t *testing.T, dir string, ro bool) {
	t.Helper()
	mode := os.FileMode(0o644)
	if ro {
		mode = 0o444
	}
	root := filepath.Join(dir, "rokh")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Dir(p) == root {
			return err
		}
		return os.Chmod(p, mode)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// accepted is the count of accepted events `rokh verify` gives for a carrier;
// it fails the test unless the carrier verifies with none rejected or pending.
func (w *seedWorld) accepted(dir string) int {
	w.t.Helper()
	out := w.must("owner", "verify", dir)
	if !strings.Contains(out, " 0 rejected, 0 pending") || !strings.Contains(out, "ok ") {
		w.t.Fatalf("verify of %s:\n%s", filepath.Base(dir), out)
	}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 2 && f[0] == "events" {
			n, err := strconv.Atoi(f[1])
			if err != nil {
				w.t.Fatal(err)
			}
			return n
		}
	}
	w.t.Fatalf("verify of %s gives no count", filepath.Base(dir))
	return 0
}

// takeParents reads, with the owner's passphrase, the parents of the one take
// a seed's ledger holds.
func (w *seedWorld) takeParents(dir string) []frame.ID {
	w.t.Helper()
	l, err := sourceLedger(w.open("owner", dir))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []frame.ID
	takes := 0
	for _, id := range l.Order() {
		e, _ := l.Get(id)
		if e.HeadOnly || e.Event.Verb != event.VerbSeed {
			continue
		}
		if s, err := event.DecodeSeed(e.Event.Payload); err == nil && s.Op == event.SeedTake && frame.ID(s.Seed) == w.vesselOf(dir).Info().Seed {
			out = e.Event.Parents
			takes++
		}
	}
	if takes != 1 {
		w.t.Fatalf("%s holds %d takes of its own seed", filepath.Base(dir), takes)
	}
	return out
}

// branchTip is the event a carrier's main branch names.
func (w *seedWorld) branchTip(dir string) frame.ID {
	w.t.Helper()
	id, ok, err := w.open("owner", dir).Ref(defaultBranch)
	if err != nil || !ok {
		w.t.Fatalf("%s names no %s: %v", filepath.Base(dir), defaultBranch, err)
	}
	return id
}

// holdsEvery fails the test unless a seed's own ledger, read through the
// command, holds every event of the rows given.
func (w *seedWorld) holdsEvery(dir string, rows []seedRow) []seedRow {
	w.t.Helper()
	got := w.log("owner", dir)
	have := idsOf(got)
	for _, r := range rows {
		if !have[r.ID] {
			w.t.Fatalf("the seed's own ledger lacks %s (%s at %s)", r.ID[:8], r.Verb, r.Address)
		}
	}
	return got
}

// Step 1: a seed whose give was recorded and whose command was cut
// before anything reached the folder is finished after the source moved on:
// two writes and a key added. The same command, without --attempt, uses the
// give again, and the seed's own ledger names every event the copy brought:
// the source's log, event by event, and the take. Verify of the seed and of
// the source agree on those events; the take goes on from the source's
// branch, so the seed has one head, the take. The resume writes nothing on
// the source (its file digests are unchanged), and the same command once
// more changes no byte of either.
func TestAResumedSeedHoldsWhatTheCopyBrought(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/before", "--message", "BEFORE the give")
	w.pass("owner")
	seed := cutSeedAt(t, src, dst, w.phrase["owner"], true)
	w.must("owner", "write", src, "--address", "journal/later", "--message", "LATER: written on the source after the give")
	w.must("owner", "key", "add", src, "--name", "late", "--reads", "journal", "--key-passphrase-file", w.pass("late"))
	w.must("owner", "write", src, "--address", "work/plan", "--message", "LATER two")
	srcRows, srcFiles := w.log("owner", src), w.digests(src)
	srcTip := w.branchTip(src)

	out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	for _, want := range []string{"give recorded (recorded before; used again)", "copy recorded", "take recorded"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the resumed seed does not say %q: %q", want, out)
		}
	}
	if !reflect.DeepEqual(w.digests(src), srcFiles) {
		t.Fatal("the resumed seed wrote on the source")
	}
	seedRows := w.holdsEvery(dst, srcRows)
	if len(seedRows) != len(srcRows)+1 {
		t.Fatalf("the seed holds %d events; the source %d and the take", len(seedRows), len(srcRows))
	}
	if s, n := w.accepted(src), w.accepted(dst); n != s+1 {
		t.Fatalf("verify: the source accepts %d events, the seed %d", s, n)
	}
	if got := w.takeParents(dst); len(got) != 1 || got[0] != srcTip {
		t.Fatalf("the take is signed on %v, not on the source's branch %s", got, srcTip.Short())
	}
	if named, _ := w.folderSeed(dst); named != seed {
		t.Fatalf("the folder names %s, not the seed cut", named.Short())
	}
	if heads := strings.Fields(w.headsOf(dst)); len(heads) != 1 {
		t.Fatalf("the resumed seed holds heads %v; one, its take, is the source's branch gone on", heads)
	}
	files := w.digests(dst)
	if out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst); !strings.Contains(out, "already took this seed; nothing was recorded") {
		t.Fatalf("the seed once more: %q", out)
	}
	if !reflect.DeepEqual(w.digests(src), srcFiles) || !reflect.DeepEqual(w.digests(dst), files) {
		t.Fatal("the seed once more changed a byte")
	}
}

// Step 1, the same through --attempt on a lost folder: a whole seed made
// under an attempt's name is lost; the source writes on; the same attempt
// makes the folder anew with the give it recorded before, and the new seed's
// own ledger holds the source's later event.
func TestAnAttemptOnALostFolderHoldsWhatTheCopyBrought(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.must("owner", "seed", src, "--attempt", "first", "--size", "16M", "--slab", "256K", dst)
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	w.must("owner", "write", src, "--address", "journal/later", "--message", "LATER: after the folder was lost")
	srcRows := w.log("owner", src)
	out := w.must("owner", "seed", src, "--attempt", "first", "--size", "16M", "--slab", "256K", dst)
	if !strings.Contains(out, "used again") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the same attempt on a lost folder: %q", out)
	}
	if !sameIDs(w.log("owner", src), srcRows) {
		t.Fatal("the same attempt recorded on the source")
	}
	if rows := w.holdsEvery(dst, srcRows); len(rows) != len(srcRows)+1 {
		t.Fatalf("the seed made anew holds %d events; the source %d and the take", len(rows), len(srcRows))
	}
	if s, n := w.accepted(src), w.accepted(dst); n != s+1 {
		t.Fatalf("verify: the source accepts %d events, the seed %d", s, n)
	}
	if heads := strings.Fields(w.headsOf(dst)); len(heads) != 1 {
		t.Fatalf("the seed made anew holds heads %v", heads)
	}
}

// Step 1, with heads the source holds apart: after the give the source
// meets another seed that wrote, and holds two heads. The resumed seed names
// both: the take goes on from the source's branch, the other head stays apart
// and named, and no event of the source is missing from the seed's ledger.
// The resume joins nothing: the seed holds as many heads as the source.
func TestAResumedSeedKeepsTheSourcesHeadsApart(t *testing.T) {
	w := newSeedWorld(t)
	src, other, dst := w.dir("src"), w.dir("other"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", other)
	w.pass("owner")
	cutSeedAt(t, src, dst, w.phrase["owner"], true)
	w.must("owner", "write", other, "--address", "journal/other", "--message", "written on the other seed")
	w.must("owner", "write", src, "--address", "journal/src", "--message", "written on the source after the give")
	w.must("owner", "reconcile", src, other)
	srcHeads := strings.Fields(w.headsOf(src))
	if len(srcHeads) != 2 {
		t.Fatalf("the source holds heads %v, not two", srcHeads)
	}
	srcRows := w.log("owner", src)
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	w.holdsEvery(dst, srcRows)
	if s, n := w.accepted(src), w.accepted(dst); n != s+1 {
		t.Fatalf("verify: the source accepts %d events, the seed %d", s, n)
	}
	seedHeads := strings.Fields(w.headsOf(dst))
	if len(seedHeads) != 2 {
		t.Fatalf("the resumed seed holds heads %v; the source holds %v", seedHeads, srcHeads)
	}
	srcTip := w.branchTip(src)
	if got := w.takeParents(dst); len(got) != 1 || got[0] != srcTip {
		t.Fatalf("the take is signed on %v, not on the source's branch %s", got, srcTip.Short())
	}
	apart := ""
	for _, h := range srcHeads {
		if h != srcTip.Short() {
			apart = h
		}
	}
	found := false
	for _, h := range seedHeads {
		found = found || h == apart
	}
	if !found {
		t.Fatalf("the head the source holds apart (%s) is not a head of the seed: %v", apart, seedHeads)
	}
}

// filesHolding lists the files of a folder's vessel whose bytes hold b
// anywhere: the four head files, whose cells lie in the clear, and the slabs.
func (w *seedWorld) filesHolding(dir string, b []byte) []string {
	w.t.Helper()
	var out []string
	root := filepath.Join(dir, "rokh")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, b) {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

// Step 2, a resumed seed and a revoked key,
// as a test of the tree, through the real command line, with the cuts it
// made: a key kk reads journal; a seed is cut before its give (the source's
// slabs read-only: the folder is made, with kk's cell, and no give is
// recorded), then after its give (the folder's slabs read-only: the give is
// recorded, the copy is not). The owner takes kk back. The same command then
// finishes the seed, and at that moment the cells are judged again by the
// source's keyring as it is: kk has no cell on the seed, in no head file and
// in no other file of the folder (the bytes of kk's slot blob are nowhere);
// the seed's own ledger holds the revoke and lists kk as no live key; kk's
// passphrase opens nothing there. What the owner, and a live key that writes,
// record on the seed afterwards is sealed to neither kk nor anybody taken
// back, and a live reader reads it.
func TestAResumedSeedGivesNoCellToAKeyTakenBackSince(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "kk", "--reads", "journal", "--key-passphrase-file", w.pass("kk"))
	w.must("owner", "key", "add", src, "--name", "scribe", "--reads", "journal", "--write", "--scope", "journal", "--key-passphrase-file", w.pass("scribe"))
	w.must("owner", "write", src, "--address", "journal/before", "--message", "BEFORE the give")

	slabsReadOnly(t, src, true)
	out, errOut, code := w.run("owner", "seed", src, "--size", "8M", "--slab", "256K", dst)
	slabsReadOnly(t, src, false)
	if code != 3 || !strings.Contains(out, "names this seed and holds no event") {
		t.Fatalf("the cut before the give: exit %d\n%s%s", code, out, errOut)
	}
	if !w.hasCell("kk", dst) {
		t.Fatal("the folder made before the give holds no cell of kk, live then")
	}
	slabsReadOnly(t, dst, true)
	out, errOut, code = w.run("owner", "seed", src, "--size", "8M", "--slab", "256K", dst)
	slabsReadOnly(t, dst, false)
	if code != 3 || !strings.Contains(out, "give recorded") || !strings.Contains(out, "copy not-recorded") {
		t.Fatalf("the cut after the give: exit %d\n%s%s", code, out, errOut)
	}
	w.must("owner", "key", "revoke", src, "--key", "kk")
	kk := w.keyringOf(src)["kk"]
	if len(kk) != 1 || len(kk[0].Slot) != key.BlobSize {
		t.Fatalf("kk's add on the source: %+v", kk)
	}
	srcRows := w.log("owner", src)

	out = w.must("owner", "seed", src, "--size", "8M", "--slab", "256K", dst)
	if !strings.Contains(out, "used again") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the resumed seed: %q", out)
	}
	if got := w.filesHolding(dst, kk[0].Slot); len(got) != 0 {
		t.Fatalf("kk's cell is still in the folder, in %v", got)
	}
	if w.hasCell("kk", dst) || !w.opensNothing("kk", dst) {
		t.Fatal("kk's passphrase opens the resumed seed")
	}
	w.holdsEvery(dst, srcRows)
	if listed := w.must("owner", "key", "list", dst); strings.Contains(listed, " kk ") || !strings.Contains(listed, " scribe ") {
		t.Fatalf("the resumed seed's own ledger lists:\n%s", listed)
	}
	if !w.hasCell("scribe", dst) {
		t.Fatal("scribe, live at the give and now, has no cell on the resumed seed")
	}

	kkKid := fmt.Sprintf("%x", key.Kid(kk[0].Reader))
	w.must("owner", "write", dst, "--address", "journal/after", "--message", "AFTER: written by the owner on the resumed seed")
	w.must("scribe", "write", dst, "--address", "journal/scribe", "--message", "AFTER: written by scribe on the resumed seed")
	for _, r := range w.log("owner", dst) {
		if r.Address != "journal/after" && r.Address != "journal/scribe" {
			continue
		}
		for _, k := range w.kidsOf(dst, r.ID) {
			if k == kkKid {
				t.Fatalf("DISCLOSURE: %s, written on the resumed seed after kk was taken back, is sealed to kk", r.Address)
			}
		}
	}
	if got := bodies(w.log("scribe", dst)); got["journal/after"] == "" || got["journal/scribe"] == "" {
		t.Fatalf("a live reader does not read what was written on the resumed seed: %v", got)
	}
}
