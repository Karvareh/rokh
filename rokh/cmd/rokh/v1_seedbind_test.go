package main

import (
	"crypto/rand"
	"os"
	"reflect"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// T4c: which seed a folder, a name or a source may go on with.

// givesBySeed counts, through the command, the gives a carrier's ledger holds
// of each seed id.
func (w *seedWorld) givesBySeed(dir string) map[frame.ID]int {
	w.t.Helper()
	out := map[frame.ID]int{}
	for _, r := range w.log("owner", dir) {
		if r.Verb != event.VerbSeed {
			continue
		}
		s, err := event.DecodeSeed(r.payload(w.t))
		if err != nil {
			w.t.Fatal(err)
		}
		if s.Op == event.SeedGive {
			out[frame.ID(s.Seed)]++
		}
	}
	return out
}

// doubledSeeds are the seed ids a carrier holds two or more gives of.
func (w *seedWorld) doubledSeeds(dir string) []frame.ID {
	w.t.Helper()
	var out []frame.ID
	for s, n := range w.givesBySeed(dir) {
		if n > 1 {
			out = append(out, s)
		}
	}
	return out
}

// recordGiveOf records on a carrier, in this process, a give of a seed id
// another vessel of the rokh already gave: the keyring add of the key the
// root derives for that seed, its grant and the give, as two vessels did
// when they gave one attempt's seed before attempts were bound to their
// source.
func (w *seedWorld) recordGiveOf(dir string, seed frame.ID) {
	w.t.Helper()
	w.pass("owner")
	c, lock, _, err := openCarrier(dir, w.phrase["owner"], true)
	if err != nil {
		w.t.Fatal(err)
	}
	defer lock.Release()
	v := c.Vessel()
	info := v.Info()
	sec, _, err := key.Try(w.phrase["owner"], v.Slots(), info.Salt, info.Iter)
	if err != nil {
		w.t.Fatal(err)
	}
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		w.t.Fatal(err)
	}
	l, err := sourceLedger(c)
	if err != nil {
		w.t.Fatal(err)
	}
	sk, err := deriveSeedKey(sec.Signer(), c.Anchor(), seed)
	if err != nil {
		w.t.Fatal(err)
	}
	g, err := newGiving(l, sec.Signer(), sk, systemReaderOf(l, owner), event.Seed{Op: event.SeedGive, Seed: seed, Key: sk.id, Source: info.Seed}, l.Heads())
	if err != nil {
		w.t.Fatal(err)
	}
	if h := recordOn(c, lock, g.events, defaultBranch); h.Outcome != vessel.Recorded {
		w.t.Fatalf("the second give: %s %v", h.Outcome, h.Err)
	}
}

// Step 3: one --attempt name on two vessels of one rokh names two seeds.
// Two whole seeds of a root each give a seed under the same name: the two
// folders name two seed ids, the two sources reconcile, and neither, nor the
// root after it meets them, holds two gives of one seed id; every kind of
// seed is still given from them. The same name on a folder another source
// was cut on (its give recorded, its copy did not fit) is refused on the
// second source, which records nothing and changes no byte of the folder;
// the first source goes on with it.
func TestOneAttemptNameOnTwoSourcesNamesTwoSeeds(t *testing.T) {
	w := newSeedWorld(t)
	root, a, b := w.dir("root"), w.dir("a"), w.dir("b")
	w.must("owner", "init", root, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", root, "--address", "journal/x", "--message", "a synthetic line")
	w.must("owner", "seed", root, "--size", "16M", "--slab", "256K", a)
	w.must("owner", "seed", root, "--size", "16M", "--slab", "256K", b)
	da, db := w.dir("da"), w.dir("db")
	w.must("owner", "seed", a, "--attempt", "backup-1", "--size", "8M", "--slab", "256K", da)
	w.must("owner", "seed", b, "--attempt", "backup-1", "--size", "8M", "--slab", "256K", db)
	sa, _ := w.folderSeed(da)
	sb, _ := w.folderSeed(db)
	if sa == sb {
		t.Fatalf("one attempt name on two sources named one seed, %s", sa.Short())
	}
	w.must("owner", "reconcile", a, b)
	if d := w.doubledSeeds(a); len(d) != 0 {
		t.Fatalf("after the reconcile a holds two gives of %v", d)
	}
	w.must("owner", "seed", a, "--size", "8M", "--slab", "256K", w.dir("dn"))
	w.must("owner", "seed", a, "--scope", "journal", "--size", "8M", "--slab", "256K", w.dir("dn2"))
	w.must("owner", "seed", a, "--attempt", "other-name", "--size", "8M", "--slab", "256K", w.dir("dn3"))
	w.must("owner", "reconcile", root, a)
	if d := w.doubledSeeds(root); len(d) != 0 {
		t.Fatalf("the root holds two gives of %v", d)
	}
	w.must("owner", "seed", root, "--size", "8M", "--slab", "256K", w.dir("dn4"))

	// One folder, one name, two sources.
	data := make([]byte, 5<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	w.bring(a, "work/archive", data)
	w.must("owner", "reconcile", a, b)
	dx := w.dir("dx")
	out, errOut, code := w.run("owner", "seed", a, "--attempt", "x", "--size", "4M", "--slab", "256K", dx)
	if code != 3 || !strings.Contains(out, "give recorded") || !strings.Contains(out, "copy not-recorded") {
		t.Fatalf("the cut on a: exit %d\n%s%s", code, out, errOut)
	}
	bLog, files := w.log("owner", b), w.digests(dx)
	out, errOut, code = w.run("owner", "seed", b, "--attempt", "x", "--size", "32M", "--slab", "256K", dx)
	if code != 3 || !strings.Contains(errOut, `not the seed of attempt "x" on this source`) {
		t.Fatalf("the same name on b for the folder a was cut on: exit %d\n%s%s", code, out, errOut)
	}
	if !sameIDs(w.log("owner", b), bLog) || !reflect.DeepEqual(w.digests(dx), files) {
		t.Fatal("the refused seed recorded on b or wrote in the folder")
	}
	out = w.must("owner", "seed", a, "--attempt", "x", "--size", "32M", "--slab", "256K", dx)
	if !strings.Contains(out, "used again") || !strings.Contains(out, "take recorded") {
		t.Fatalf("a going on with its own folder: %q", out)
	}
	w.must("owner", "reconcile", a, b)
	if d := w.doubledSeeds(b); len(d) != 0 {
		t.Fatalf("b holds two gives of %v", d)
	}
}

// Step 3, a state made before this step: two vessels that each hold a
// give of one seed id meet, and now both hold two gives of it. Nothing removes
// an event, so the rule is: the two gives are shown, the seed they name is
// never gone on with, and new seeds are given. A new seed from either, and
// from the root after it meets them, is given and its answer names the two
// gives; the folder of that seed is refused, with the reason, and nothing is
// recorded and no byte of it changes.
func TestTwoGivesOfOneSeedAreShownAndNeverGoneOn(t *testing.T) {
	w := newSeedWorld(t)
	root, a, b, da := w.dir("root"), w.dir("a"), w.dir("b"), w.dir("da")
	w.must("owner", "init", root, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", root, "--address", "journal/x", "--message", "a synthetic line")
	w.must("owner", "seed", root, "--size", "16M", "--slab", "256K", a)
	w.must("owner", "seed", root, "--size", "16M", "--slab", "256K", b)
	w.must("owner", "seed", a, "--size", "8M", "--slab", "256K", da)
	seed, _ := w.folderSeed(da)
	w.recordGiveOf(b, seed)
	w.must("owner", "reconcile", a, b)
	for _, dir := range []string{a, b} {
		if n := w.givesBySeed(dir)[seed]; n != 2 {
			t.Fatalf("%s holds %d gives of the seed", dir, n)
		}
	}
	want := "note: the source holds 2 gives of seed " + seed.Short()
	for i, src := range []string{a, b} {
		dn := w.dir("new" + string(rune('a'+i)))
		out := w.must("owner", "seed", src, "--size", "8M", "--slab", "256K", dn)
		if !strings.Contains(out, want) || !strings.Contains(out, "take recorded") {
			t.Fatalf("a new seed from a source that holds two gives: %q", out)
		}
		if got, _ := w.folderSeed(dn); got == seed {
			t.Fatal("the new seed is the doubled one")
		}
	}
	srcLog, files := w.log("owner", a), w.digests(da)
	out, errOut, code := w.run("owner", "seed", a, "--size", "8M", "--slab", "256K", da)
	if code != 3 || !strings.Contains(errOut, "never gone on with") || !strings.Contains(errOut, seed.Short()) {
		t.Fatalf("the doubled seed's folder: exit %d\n%s%s", code, out, errOut)
	}
	if !sameIDs(w.log("owner", a), srcLog) || !reflect.DeepEqual(w.digests(da), files) {
		t.Fatal("the refused seed recorded or wrote")
	}
	w.must("owner", "reconcile", root, a)
	if out := w.must("owner", "seed", root, "--size", "8M", "--slab", "256K", w.dir("fromroot")); !strings.Contains(out, want) {
		t.Fatalf("a new seed from the root: %q", out)
	}
}

// rewriteMarker rewrites, as the holder of a key's cell of a folder (its
// vessel key, and no root), the seed the folder's vessel names.
func (w *seedWorld) rewriteMarker(role, dir string, seed frame.ID) {
	w.t.Helper()
	w.pass(role)
	c, _, err := carrier.Open(medium.Dir{Root: dir}, v1Unlock(w.phrase[role]), rand.Reader, nil)
	if err != nil {
		w.t.Fatalf("%s cannot open the folder: %v", role, err)
	}
	lock, err := turn.Acquire(dir, v1Patience)
	if err != nil {
		w.t.Fatal(err)
	}
	defer lock.Release()
	tx, err := c.Vessel().Begin(lock)
	if err != nil {
		w.t.Fatal(err)
	}
	tx.SetRoot(func(r *vessel.Root) { r.Seed = seed })
	if out, err := tx.Commit(); out != vessel.Recorded {
		w.t.Fatalf("the marker was not rewritten: %s %v", out, err)
	}
	if got, _ := w.folderSeed(dir); got != seed {
		w.t.Fatalf("the folder names %s after the rewrite", got.Short())
	}
}

// Step 4: a folder whose marker names a seed already given and taken is
// refused. A forged marker, as a test of the
// tree: a whole seed old is given and taken; a second seed is cut before its
// give (the source's slabs read-only), so its folder names a seed planned on
// this source and holds no event; the holder of a key's cell of that folder
// rewrites its marker to old's seed. The owner's seed command is refused,
// with the reason, before anything is recorded: the source's log and every
// byte of the folder are unchanged, and old's seed keeps one vessel that took
// it. A folder cut the same way and left alone goes on (the control), and a
// repeated seed of old still records nothing (S5).
func TestAMarkerNamingASeedGivenAndTakenIsRefused(t *testing.T) {
	w := newSeedWorld(t)
	src, old, forged, control := w.dir("src"), w.dir("old"), w.dir("forged"), w.dir("control")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "holder", "--reads", "journal", "--key-passphrase-file", w.pass("holder"))
	w.must("owner", "write", src, "--address", "journal/x", "--message", "a synthetic line")
	w.must("owner", "seed", src, "--size", "8M", "--slab", "256K", old)
	oldSeed, _ := w.folderSeed(old)
	for _, d := range []string{forged, control} {
		slabsReadOnly(t, src, true)
		_, _, code := w.run("owner", "seed", src, "--size", "8M", "--slab", "256K", d)
		slabsReadOnly(t, src, false)
		if code != 3 {
			t.Fatalf("the cut did not leave a folder: exit %d", code)
		}
	}
	w.rewriteMarker("holder", forged, oldSeed)
	srcLog, files := w.log("owner", src), w.digests(forged)
	out, errOut, code := w.run("owner", "seed", src, "--size", "8M", "--slab", "256K", forged)
	if code != 3 || !strings.Contains(errOut, "not the one made for that seed") || !strings.Contains(errOut, oldSeed.Short()) {
		t.Fatalf("a folder whose marker names a seed given and taken: exit %d\n%s%s", code, out, errOut)
	}
	if !sameIDs(w.log("owner", src), srcLog) || !reflect.DeepEqual(w.digests(forged), files) {
		t.Fatal("the refused seed recorded on the source or wrote in the folder")
	}
	if _, events := w.folderSeed(forged); events != 0 || w.takesOf(old, oldSeed) != 1 {
		t.Fatal("old's seed was taken a second time")
	}
	if out := w.must("owner", "seed", src, "--size", "8M", "--slab", "256K", control); !strings.Contains(out, "take recorded") {
		t.Fatalf("the folder left alone: %q", out)
	}
	if out := w.must("owner", "seed", src, "--size", "8M", "--slab", "256K", old); !strings.Contains(out, "already took this seed; nothing was recorded") {
		t.Fatalf("old once more: %q", out)
	}
}

// Step 5 (F12): a folder lost after its give. The seed is cut right after its
// give; the folder is lost. The same command, without --attempt, cannot tell
// which folder the give was for: it gives a new seed (a second give; the
// first stays on the source, as nothing removes an event), and its answer
// says so in one line: the earlier seed, its give, that it has no take on the
// source, and the command that takes its key back if its folder is lost for
// good. That command works, and the key is no longer live. Once the source
// holds the take of the seed it gave since, a further seed's answer names no
// earlier give.
func TestALostFolderIsANewSeedAndItsAnswerNamesTheEarlierGive(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/x", "--message", "a synthetic line")
	w.pass("owner")
	first := cutSeedAt(t, src, dst, w.phrase["owner"], true)
	firstGive := w.log("owner", src)
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	out := w.must("owner", "seed", src, "--size", "8M", "--slab", "256K", dst)
	var notes []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "note:") {
			notes = append(notes, line)
		}
	}
	revoke := "rokh key revoke " + src + " --key " + seedKeyName(first)
	if len(notes) != 1 || !strings.Contains(notes[0], "seed "+first.Short()) || !strings.Contains(notes[0], firstGive[len(firstGive)-1].ID[:8]) ||
		!strings.Contains(notes[0], "has no take on it") || !strings.Contains(notes[0], revoke) {
		t.Fatalf("the answer of the repeat does not name the earlier give in one line: %q", out)
	}
	second, _ := w.folderSeed(dst)
	if second == first || w.givesBySeed(src)[first] != 1 || w.givesBySeed(src)[second] != 1 {
		t.Fatalf("the lost folder's repeat: gives %v", w.givesBySeed(src))
	}
	if listed := w.must("owner", "key", "list", src); !strings.Contains(listed, seedKeyName(first)) {
		t.Fatalf("the earlier seed's key is not live before it is taken back:\n%s", listed)
	}
	w.must("owner", "key", "revoke", src, "--key", seedKeyName(first))
	if listed := w.must("owner", "key", "list", src); strings.Contains(listed, seedKeyName(first)) {
		t.Fatalf("the earlier seed's key is live after it was taken back:\n%s", listed)
	}
	w.must("owner", "reconcile", src, dst)
	if out := w.must("owner", "seed", src, "--size", "8M", "--slab", "256K", w.dir("third")); strings.Contains(out, "has no take on it") {
		t.Fatalf("a seed after both earlier gives were settled names one: %q", out)
	}
}

// Step 6 (F13): seed does not join heads without a word. Two seeds of a root
// write apart and meet; a holds two heads, as the owner left them. A seed
// given from a names only the head of a's main branch: a still holds two
// heads, no rokh.merge and no event naming both was recorded, and the
// answer says the heads stay apart until the owner merges them. The new seed
// holds them apart as well. A key added on b, which reached a by the
// reconcile beside a's main branch, still takes its cell on the new seed.
func TestASeedJoinsNoHeadsTheOwnerLeftApart(t *testing.T) {
	w := newSeedWorld(t)
	root, a, b, c := w.dir("root"), w.dir("a"), w.dir("b"), w.dir("c")
	w.must("owner", "init", root, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "seed", root, "--size", "8M", "--slab", "256K", a)
	w.must("owner", "seed", root, "--size", "8M", "--slab", "256K", b)
	w.must("owner", "key", "add", b, "--name", "onb", "--reads", "journal", "--key-passphrase-file", w.pass("onb"))
	w.must("owner", "write", a, "--address", "journal/a", "--message", "on a")
	w.must("owner", "write", b, "--address", "journal/b", "--message", "on b")
	w.must("owner", "reconcile", a, b)
	before := strings.Fields(w.headsOf(a))
	if len(before) != 2 {
		t.Fatalf("a holds heads %v, not two", before)
	}
	tip := w.branchTip(a)
	out := w.must("owner", "seed", a, "--size", "8M", "--slab", "256K", c)
	want := "note: the source holds 2 heads; the give names the head of its main branch, " + tip.Short() + ", and leaves 1 apart"
	if !strings.Contains(out, want) || !strings.Contains(out, "rokh reconcile --merge") {
		t.Fatalf("the answer does not say the heads stay apart: %q", out)
	}
	after := strings.Fields(w.headsOf(a))
	if len(after) != 2 {
		t.Fatalf("after the seed a holds heads %v", after)
	}
	other := before[0]
	if other == tip.Short() {
		other = before[1]
	}
	if !strings.Contains(w.headsOf(a), other) {
		t.Fatalf("the head a held apart, %s, is no head after the seed: %v", other, after)
	}
	for _, r := range w.log("owner", a) {
		if r.Verb == event.VerbMerge {
			t.Fatal("a merge was recorded")
		}
	}
	if heads := strings.Fields(w.headsOf(c)); len(heads) != 2 || !strings.Contains(w.headsOf(c), other) {
		t.Fatalf("the new seed holds heads %v", heads)
	}
	if id, ok := w.opensOwnCell("onb", c); !ok || id == ([32]byte{}) {
		t.Fatal("a key live on a beside its main branch has no cell on the new seed")
	}
}
