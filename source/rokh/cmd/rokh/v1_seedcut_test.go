package main

import (
	"crypto/rand"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/vessel"
)

// cutSeedAt runs the seed command's own steps in this process, in the
// command's order, on src and a folder dst that holds nothing yet, with the
// owner's passphrase, and cuts them where a power cut could: after the folder
// is made (give false), or right after the give's commit on src (give true).
// Nothing is copied and nothing is taken. It returns the seed the folder
// names.
func cutSeedAt(t *testing.T, src, dst, pass string, give bool) frame.ID {
	t.Helper()
	c, lock, _, err := openCarrier(src, pass, true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if held, err := seedHeldAt(dst, pass, c); err != nil || held != nil {
		t.Fatalf("the folder before the cut: %+v %v", held, err)
	}
	plan, keys, err := v1SeedPlan(c, pass, SeedAsk{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := makeSeedFolder(dst, c, plan, keys, 18, 64); err != nil {
		t.Fatal(err)
	}
	if give {
		if h := recordOn(c, lock, plan.Give, plan.Branch); h.Outcome != vessel.Recorded {
			t.Fatalf("the give: %s %v", h.Outcome, h.Err)
		}
	}
	return plan.Seed
}

// folderSeed opens a folder's vessel with the owner's passphrase, below the
// key layer, and says which seed it names and how many event records it
// holds. It writes nothing.
func (w *seedWorld) folderSeed(dir string) (frame.ID, int) {
	w.t.Helper()
	v := w.vesselOf(dir)
	events := 0
	if err := v.Scan(func(h vessel.Header, _ vessel.Ref) error {
		if h.Type == vessel.RecEvent {
			events++
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return v.Info().Seed, events
}

// vesselOf opens a folder's vessel with the owner's passphrase, below the key
// layer: a folder that holds no event opens there too.
func (w *seedWorld) vesselOf(dir string) *vessel.Vessel {
	w.t.Helper()
	w.pass("owner")
	c, _, err := carrier.Open(medium.Dir{Root: dir}, v1Unlock(w.phrase["owner"]), rand.Reader, nil)
	if err != nil {
		w.t.Fatalf("open %s: %v", dir, err)
	}
	return c.Vessel()
}

// hasCell says whether a role's passphrase opens a cell of a folder's vessel
// other than the owner's, read below the key layer.
func (w *seedWorld) hasCell(role, dir string) bool {
	w.t.Helper()
	w.pass(role)
	v := w.vesselOf(dir)
	info := v.Info()
	sec, _, err := key.Try(w.phrase[role], v.Slots(), info.Salt, info.Iter)
	if errors.Is(err, key.ErrPassphrase) {
		return false
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return sec.Key != ([32]byte{})
}

// takesOf lists the takes of one seed a carrier's ledger holds: a seed of a
// seed holds its source's take too.
func (w *seedWorld) takesOf(dir string, seed frame.ID) int {
	w.t.Helper()
	n := 0
	for _, s := range w.takesIn(dir) {
		if frame.ID(s.Seed) == seed {
			n++
		}
	}
	return n
}

// Step 5 (goal 7.41; contract S5), cut at the point the earlier order left
// open: right after the give's commit on the source, before anything reached
// the new folder. The folder was made first and names the seed, so the same
// command again, without --attempt, finds that give and uses it: no second
// give is recorded, the copy and the take go on, and the seed holds one take
// of the one give. The command once more records nothing.
func TestASeedCutRightAfterItsGiveRecordsNoSecondGive(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	before := w.log("owner", src)
	w.pass("owner")
	seed := cutSeedAt(t, src, dst, w.phrase["owner"], true)
	given := w.log("owner", src)
	if len(given) != len(before)+3 {
		t.Fatalf("the cut give is %d events", len(given)-len(before))
	}
	if named, events := w.folderSeed(dst); named != seed || events != 0 {
		t.Fatalf("after the cut the folder names %s and holds %d events", named.Short(), events)
	}
	out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	for _, want := range []string{"give recorded (recorded before; used again)", "copy recorded", "take recorded"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the seed going on does not say %q: %q", want, out)
		}
	}
	if !sameIDs(w.log("owner", src), given) {
		t.Fatal("the seed going on recorded a second give")
	}
	takes := w.takesIn(dst)
	if len(takes) != 1 || takes[0].Give.String() != given[len(given)-1].ID || frame.ID(takes[0].Seed) != seed {
		t.Fatalf("the seed holds %d takes, not one of the cut give", len(takes))
	}
	if v := w.must("owner", "verify", dst); !strings.Contains(v, "0 rejected, 0 pending") {
		t.Fatalf("verify of the seed:\n%s", v)
	}
	if out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst); !strings.Contains(out, "nothing was recorded") {
		t.Fatalf("the seed once more: %q", out)
	}
	if !sameIDs(w.log("owner", src), given) || len(w.takesIn(dst)) != 1 {
		t.Fatal("the seed once more recorded")
	}
}

// Step 5, cut before the give: the folder is made and names a seed planned on
// this source, and no give is recorded. Meanwhile the owner takes one key
// back and adds another. The same command again, without --attempt, gives
// that very seed now (one give, not "used again"), copies and takes it; the
// folder's cells are made again at this give: the key added since opens the
// seed, the key taken back since has no cell there.
func TestASeedCutBeforeItsGiveIsGivenOnce(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "early", "--reads", "journal", "--key-passphrase-file", w.pass("early"))
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.pass("owner")
	seed := cutSeedAt(t, src, dst, w.phrase["owner"], false)
	if !w.hasCell("early", dst) {
		t.Fatal("the folder made before the cut has no cell of the key live then")
	}
	w.must("owner", "key", "revoke", src, "--key", "early")
	w.must("owner", "key", "add", src, "--name", "late", "--reads", "journal", "--key-passphrase-file", w.pass("late"))
	before := w.log("owner", src)
	if len(w.takesIn(src)) != 0 || len(before) == 0 {
		t.Fatal("the source holds a take")
	}
	for _, r := range before {
		if r.Verb == "rokh.seed" {
			t.Fatal("a give was recorded before the cut")
		}
	}
	out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	if !strings.Contains(out, "give recorded, copy recorded") || strings.Contains(out, "used again") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the seed going on: %q", out)
	}
	after := w.log("owner", src)
	if len(after) != len(before)+3 {
		t.Fatalf("the seed going on recorded %d events on the source, not one give of 3", len(after)-len(before))
	}
	if named, _ := w.folderSeed(dst); named != seed {
		t.Fatalf("the folder names %s after it went on, not %s", named.Short(), seed.Short())
	}
	if takes := w.takesIn(dst); len(takes) != 1 || takes[0].Give.String() != after[len(after)-1].ID {
		t.Fatalf("the seed holds %d takes, not one of the one give", len(takes))
	}
	if id, ok := w.opensOwnCell("late", dst); !ok || id == ([32]byte{}) {
		t.Fatal("the key added since the cut has no cell on the seed")
	}
	if _, ok := w.opensOwnCell("early", dst); ok || !w.opensNothing("early", dst) {
		t.Fatal("the key taken back since the cut keeps its cell on the seed")
	}
	if got := bodies(w.log("late", dst)); len(got) != 0 {
		t.Fatalf("the key added after the line was written reads it: %v", got)
	}
}

// Step 5: a folder that names a seed planned on another source and holds no
// event is not given from this one: the command is refused, nothing is
// recorded and no byte of the folder changes. The source it was planned on
// goes on with it.
func TestAFolderPlannedOnAnotherSourceIsNotGivenHere(t *testing.T) {
	w := newSeedWorld(t)
	src, other, dst := w.dir("src"), w.dir("other"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", other)
	w.pass("owner")
	seed := cutSeedAt(t, other, dst, w.phrase["owner"], false)
	srcLog, files := w.log("owner", src), w.digests(dst)
	out, errOut, code := w.run("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	if code != 3 || !strings.Contains(errOut, "not planned on this source") {
		t.Fatalf("a folder planned on another source: exit %d\n%s%s", code, out, errOut)
	}
	if !sameIDs(w.log("owner", src), srcLog) || !reflect.DeepEqual(w.digests(dst), files) {
		t.Fatal("a refused seed recorded or wrote")
	}
	if out := w.must("owner", "seed", other, "--size", "16M", "--slab", "256K", dst); !strings.Contains(out, "give recorded, copy recorded") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the source it was planned on does not go on with it: %q", out)
	}
	if w.takesOf(dst, seed) != 1 {
		t.Fatal("the seed does not hold one take of its own seed")
	}
}

// Step 6 (goal 7.41): a seed cut before any event arrived opens with every
// command that opens through the key layer. The seed is cut right after its
// give: its folder names it and holds no event. Grow and shrink open it and
// change its size (grow was seen failing: carrier: not found: event
// <genesis>). Reconcile opens it and, in either order, refuses before
// anything moves to fill a seed that took nothing, and names the way on. A
// seed given from it is refused: it has no lineage to give. The commands of
// the ledger, and a key's passphrase, say that it holds no event yet, never
// that its genesis is not found, and write nothing. Then the same seed goes
// on, and is a whole seed.
func TestASeedCutBeforeAnyEventArrivedOpens(t *testing.T) {
	w := newSeedWorld(t)
	src, dst, sub := w.dir("src"), w.dir("seed"), w.dir("sub")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "reader", "--reads", "journal", "--key-passphrase-file", w.pass("reader"))
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.pass("owner")
	cutSeedAt(t, src, dst, w.phrase["owner"], true)
	if _, events := w.folderSeed(dst); events != 0 {
		t.Fatalf("the cut seed holds %d events", events)
	}
	if out := w.must("owner", "grow", dst, "--to", "32M"); !strings.Contains(out, "128 slabs") {
		t.Fatalf("grow of the cut seed: %q", out)
	}
	if out := w.must("owner", "shrink", dst, "--to", "16M", "--finish"); !strings.Contains(out, "64 slabs") {
		t.Fatalf("shrink of the cut seed: %q", out)
	}
	fs, fd := w.digests(src), w.digests(dst)
	for _, pair := range [][2]string{{dst, src}, {src, dst}} {
		out, errOut, code := w.run("owner", "reconcile", pair[0], pair[1])
		if code != 3 || !strings.Contains(errOut, "holds no event yet") || strings.Contains(out+errOut, "not found") {
			t.Fatalf("reconcile with the cut seed: exit %d\n%s%s", code, out, errOut)
		}
	}
	out, errOut, code := w.run("owner", "seed", dst, "--size", "16M", "--slab", "256K", sub)
	if code == 0 || !strings.Contains(errOut, "holds no event yet") || strings.Contains(errOut, "not found") {
		t.Fatalf("a seed given from the cut seed: exit %d\n%s%s", code, out, errOut)
	}
	if _, err := os.Stat(sub); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused seed made its folder: %v", err)
	}
	for _, c := range []struct {
		role string
		args []string
	}{
		{"owner", []string{"log", dst, "--json"}},
		{"owner", []string{"verify", dst}},
		{"owner", []string{"key", "list", dst}},
		{"owner", []string{"view", dst}},
		{"owner", []string{"write", dst, "--address", "journal/x", "--message", "synthetic"}},
		{"reader", []string{"log", dst, "--json"}},
	} {
		out, errOut, code := w.run(c.role, c.args...)
		if code == 0 || !strings.Contains(errOut, "holds no event yet") || strings.Contains(errOut, "not found") {
			t.Fatalf("rokh %s on the cut seed as %s: exit %d\n%s%s", c.args[0], c.role, code, out, errOut)
		}
	}
	if !reflect.DeepEqual(w.digests(src), fs) || !reflect.DeepEqual(w.digests(dst), fd) {
		t.Fatal("a command refused on the cut seed wrote")
	}
	out = w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	if !strings.Contains(out, "used again") || !strings.Contains(out, "copy recorded") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the cut seed going on: %q", out)
	}
	if v := w.must("owner", "verify", dst); !strings.Contains(v, "0 rejected, 0 pending") {
		t.Fatalf("verify of the seed:\n%s", v)
	}
	if got := bodies(w.log("reader", dst)); len(got) != 1 || got["journal/day"] == "" {
		t.Fatalf("the reader's view of the seed: %v", got)
	}
}

// Step 5 through the command line alone: a seed whose give does not fit in
// the source (a fixed vessel with no free slab left for it) leaves a folder
// that names the seed and holds no event, and says the same command goes on
// from there. Again while the source is full, nothing is recorded and no
// byte of the folder changes. Once the source is grown, the same command
// gives that seed, once, and copies and takes it.
func TestASeedWhoseGiveDoesNotFitGoesOnFromItsFolder(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "4M", "--slab", "256K")
	data := make([]byte, 3000000)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	w.bring(src, "work/archive", data)
	before := w.log("owner", src)
	out, errOut, code := w.run("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	if code != 3 || !strings.Contains(out, "give not-recorded") || !strings.Contains(out, "names this seed and holds no event") {
		t.Fatalf("a seed whose give does not fit: exit %d\n%s%s", code, out, errOut)
	}
	if !sameIDs(w.log("owner", src), before) {
		t.Fatal("a give that did not fit was recorded")
	}
	seed, events := w.folderSeed(dst)
	if seed.IsZero() || events != 0 {
		t.Fatalf("the folder names %s and holds %d events", seed.Short(), events)
	}
	files := w.digests(dst)
	if _, _, code := w.run("owner", "seed", src, "--size", "16M", "--slab", "256K", dst); code != 3 {
		t.Fatalf("the same seed while the source is full: exit %d", code)
	}
	if !sameIDs(w.log("owner", src), before) || !reflect.DeepEqual(w.digests(dst), files) {
		t.Fatal("the same seed while the source is full recorded or wrote")
	}
	w.must("owner", "grow", src, "--to", "8M")
	out = w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	if !strings.Contains(out, "give recorded, copy recorded") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the seed going on after the source grew: %q", out)
	}
	if after := w.log("owner", src); len(after) != len(before)+3 {
		t.Fatalf("the seed recorded %d events on the source, not one give of 3", len(after)-len(before))
	}
	if named, _ := w.folderSeed(dst); named != seed || len(w.takesIn(dst)) != 1 {
		t.Fatal("the seed made is not the one the folder named, taken once")
	}
}
