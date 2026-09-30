package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rokh/carrier"
	"rokh/content"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/lineage"
	"rokh/medium"
	"rokh/turn"
	"rokh/vessel"
)

// seedWorld runs the real rokh binary, built from this package, on synthetic
// carriers in a folder of the test's own under the work tree's short folder
// (ROKH_SHORT_TMP; the system's temporary folder where none is named). The
// folder is removed when the test ends. Every passphrase is synthetic and
// sits in a file of its own.
type seedWorld struct {
	t      *testing.T
	bin    string
	base   string
	files  map[string]string // role: passphrase file
	phrase map[string]string // role: the passphrase itself
}

func newSeedWorld(t *testing.T) *seedWorld {
	t.Helper()
	short := os.Getenv("ROKH_SHORT_TMP")
	if short == "" {
		short = os.TempDir()
	}
	base, err := os.MkdirTemp(short, "seed-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	bin := filepath.Join(base, "rokh")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return &seedWorld{t: t, bin: bin, base: base, files: map[string]string{}, phrase: map[string]string{}}
}

// dir names a folder of this world. It is not made.
func (w *seedWorld) dir(name string) string { return filepath.Join(w.base, name) }

// pass is the file of a role's synthetic passphrase, made on first use.
func (w *seedWorld) pass(role string) string {
	w.t.Helper()
	if f, ok := w.files[role]; ok {
		return f
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		w.t.Fatal(err)
	}
	p := "synthetic " + role + " " + hex.EncodeToString(b)
	f := filepath.Join(w.base, role+".pass")
	if err := os.WriteFile(f, []byte(p+"\n"), 0o600); err != nil {
		w.t.Fatal(err)
	}
	w.files[role], w.phrase[role] = f, p
	return f
}

// run runs one command with a role's passphrase file, placed right after the
// first folder named, since flags come before a second folder. It returns the
// two streams and the exit status.
func (w *seedWorld) run(role string, args ...string) (string, string, int) {
	w.t.Helper()
	var full []string
	placed := false
	for _, a := range args {
		full = append(full, a)
		if !placed && filepath.IsAbs(a) {
			full = append(full, "--passphrase-file", w.pass(role))
			placed = true
		}
	}
	cmd := exec.Command(w.bin, full...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ROKH_PASSPHRASE=") && !strings.HasPrefix(kv, "ROKH_PASSPHRASE_FILE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			w.t.Fatalf("rokh %s: %v", strings.Join(args, " "), err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errOut.String(), code
}

// must runs a command that must succeed and returns what it printed.
func (w *seedWorld) must(role string, args ...string) string {
	w.t.Helper()
	out, errOut, code := w.run(role, args...)
	if code != 0 {
		w.t.Fatalf("rokh %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

// seedRow is one line of `rokh log --json`.
type seedRow struct {
	ID        string `json:"id"`
	Address   string `json:"address"`
	Verb      string `json:"verb"`
	Author    string `json:"author"`
	Key       string `json:"key"`
	Authority string `json:"authority"`
	Payload   string `json:"payload"`
}

func (r seedRow) payload(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(r.Payload)
	if err != nil {
		t.Fatalf("payload of %s: %v", r.ID, err)
	}
	return b
}

// log reads a carrier's ledger back through the real command, in its order.
func (w *seedWorld) log(role, dir string) []seedRow {
	w.t.Helper()
	var rows []seedRow
	for _, line := range strings.Split(w.must(role, "log", dir, "--json"), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var r seedRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			w.t.Fatalf("log line %q: %v", line, err)
		}
		rows = append(rows, r)
	}
	return rows
}

// open opens a carrier in this process the way the reading commands do,
// with a role's passphrase. It takes no turn and writes nothing.
func (w *seedWorld) open(role, dir string) *carrier.Carrier {
	w.t.Helper()
	w.pass(role)
	c, _, _, err := openCarrier(dir, w.phrase[role], false)
	if err != nil {
		w.t.Fatalf("open %s as %s: %v", dir, role, err)
	}
	return c
}

func idsOf(rows []seedRow) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.ID] = true
	}
	return out
}

// Step 1 (contract 4.7 S1, S2): rokh seed records the give on the source in
// one commit (the keyring add of the seed's key, one grant to its signer for
// the whole rokh, and the give, each by the root), and makes the new folder a
// vessel of the same rokh with its own key, its own id and the inherited
// salt. The new vessel holds every event of the source and records the take,
// signed by the seed's key under that grant and naming the new vessel. The
// owner's passphrase opens and verifies it; a key's passphrase does not.
func TestASeedIsGivenOnTheSourceAndTakenInTheNewFolder(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "reader", "--reads", "journal", "--key-passphrase-file", w.pass("reader"))
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	before := w.log("owner", src)
	out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	if !strings.Contains(out, "give recorded") || !strings.Contains(out, "copy recorded") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the seed does not name its three steps: %q", out)
	}
	after := w.log("owner", src)
	if len(after) != len(before)+3 {
		t.Fatalf("the give is %d events on the source, not 3", len(after)-len(before))
	}
	for i, r := range before {
		if after[i].ID != r.ID {
			t.Fatalf("the source's earlier events moved: %d %s %s", i, after[i].ID, r.ID)
		}
	}
	root := before[0].Author
	add, grant, give := after[len(before)], after[len(before)+1], after[len(before)+2]
	for _, r := range []seedRow{add, grant, give} {
		if r.Address != event.AddressRoot || r.Author != root || r.Authority != "" {
			t.Fatalf("a give event is not a system event by the root: %+v", r)
		}
	}
	k, err := event.DecodeKeyring(add.payload(t))
	if err != nil || add.Verb != event.VerbKeyring || k.Op != event.KeyringAdd || k.IsOwner() {
		t.Fatalf("the first event of the give is not a keyring add of a key: %v %+v", err, add)
	}
	// The seed's key reads nothing (Step 1): it is named in no envelope.
	if !strings.HasPrefix(k.Name, "seed-") || k.Reads != nil || k.Signer == nil || k.Slot != nil || len(k.System) == 0 {
		t.Fatalf("the seed's key: name %q, reads %q, signer %v, slot %d bytes, system %d bytes", k.Name, k.Reads, k.Signer != nil, len(k.Slot), len(k.System))
	}
	g, err := event.DecodeGrant(grant.payload(t))
	if err != nil || grant.Verb != event.VerbGrant || !bytes.Equal(g.Subject, k.Signer) || g.Scope != "" || g.Open || g.CanDelegate || g.Verbs != nil {
		t.Fatalf("the grant of a whole seed: %v %+v", err, g)
	}
	s, err := event.DecodeSeed(give.payload(t))
	if err != nil || give.Verb != event.VerbSeed || s.Op != event.SeedGive || s.Key != k.Key || s.Scopes != nil || s.Source != ([32]byte{}) {
		t.Fatalf("the give: %v %+v", err, s)
	}

	// The new folder verifies with the owner's passphrase.
	if v := w.must("owner", "verify", dst); !strings.Contains(v, "0 rejected, 0 pending") || !strings.Contains(v, "ok ") {
		t.Fatalf("verify of the seed:\n%s", v)
	}
	seeded := w.log("owner", dst)
	have := idsOf(seeded)
	for _, r := range after {
		if !have[r.ID] {
			t.Fatalf("the seed lacks %s %s", r.ID, r.Verb)
		}
	}
	if len(seeded) != len(after)+1 {
		t.Fatalf("the seed holds %d events; the source %d and the take", len(seeded), len(after))
	}
	take := seeded[len(seeded)-1]
	ts, err := event.DecodeSeed(take.payload(t))
	if err != nil || take.Verb != event.VerbSeed || ts.Op != event.SeedTake {
		t.Fatalf("the seed's last event is not the take: %v %+v", err, take)
	}
	if take.Author != hex.EncodeToString(k.Signer) || take.Authority != grant.ID || ts.Seed != s.Seed || ts.Key != s.Key {
		t.Fatalf("the take is not the seed key's, under its grant, of this give: %+v %+v", take, ts)
	}
	if give.ID != frame.ID(ts.Give).String() {
		t.Fatalf("the take names give %x, not %s", ts.Give, give.ID)
	}

	// The new vessel: the same anchor, its own id and key, the inherited
	// salt and cost; the take names it.
	sc, dc := w.open("owner", src), w.open("owner", dst)
	si, di := sc.Vessel().Info(), dc.Vessel().Info()
	if di.Anchor != si.Anchor || di.Seed != frame.ID(s.Seed) || di.Vessel == si.Vessel || len(di.Scopes) != 0 {
		t.Fatalf("the seed's identity: anchor %v, seed %v, own vessel %v, scopes %q",
			di.Anchor == si.Anchor, di.Seed == frame.ID(s.Seed), di.Vessel != si.Vessel, di.Scopes)
	}
	if !bytes.Equal(di.Salt, si.Salt) || di.Iter != si.Iter || bytes.Equal(dc.Vessel().VK(), sc.Vessel().VK()) {
		t.Fatal("the seed does not inherit the salt and cost, or it shares the source's vessel key")
	}
	if frame.ID(ts.Vessel) != di.Vessel {
		t.Fatal("the take does not name the new vessel")
	}
	// Its cells: the owner's passphrase opens the owner's cell, which holds
	// this rokh's root; the reader's own passphrase opens the reader's own
	// cell, installed from its keyring add.
	sec, _, err := key.Try(w.phrase["owner"], dc.Vessel().Slots(), di.Salt, di.Iter)
	if err != nil || sec.Key != ([32]byte{}) {
		t.Fatalf("the owner's passphrase does not open the owner's cell of the seed: %v", err)
	}
	if !bytes.Equal(sec.Signer().Public().(ed25519.PublicKey), mustHex(t, root)) {
		t.Fatal("the seed's owner cell does not hold this rokh's root")
	}
	rs, _, err := key.Try(w.phrase["reader"], dc.Vessel().Slots(), di.Salt, di.Iter)
	if err != nil || rs.Key == ([32]byte{}) || rs.Key == k.Key {
		t.Fatalf("the reader's passphrase does not open the reader's own cell of the seed: %v", err)
	}
}

// S1: only the owner gives. A key's passphrase is refused before anything is
// recorded: the source is unchanged and no folder is made.
func TestOnlyTheOwnerGivesASeed(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "writer", "--reads", "journal", "--write", "--scope", "journal", "--key-passphrase-file", w.pass("writer"))
	before := w.log("owner", src)
	out, errOut, code := w.run("writer", "seed", src, "--size", "16M", "--slab", "256K", dst)
	if code == 0 || !strings.Contains(errOut, "only the owner gives a seed") {
		t.Fatalf("a key's passphrase gave a seed: exit %d\n%s%s", code, out, errOut)
	}
	if after := w.log("owner", src); len(after) != len(before) {
		t.Fatalf("a refused seed recorded %d events on the source", len(after)-len(before))
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused seed made its folder: %v", err)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// digests reads every file of a carrier's vessel folder: equal digests mean
// nothing there was written.
func (w *seedWorld) digests(dir string) map[string]string {
	w.t.Helper()
	out := map[string]string{}
	root := filepath.Join(dir, "rokh")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		sum := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

// bring puts synthetic content into a carrier the way the home does, since
// the command line has no bring: its chunks and the content.put event naming
// them, signed by the root, in one commit under the owner's passphrase.
func (w *seedWorld) bring(dir, addr string, data []byte) frame.ID {
	w.t.Helper()
	w.pass("owner")
	c, lock, _, err := openCarrier(dir, w.phrase["owner"], true)
	if err != nil {
		w.t.Fatal(err)
	}
	defer lock.Release()
	info := c.Vessel().Info()
	sec, _, err := key.Try(w.phrase["owner"], c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		w.t.Fatal(err)
	}
	l, err := sourceLedger(c)
	if err != nil {
		w.t.Fatal(err)
	}
	r, err := c.Begin(lock)
	if err != nil {
		w.t.Fatal(err)
	}
	d, payload, err := content.BringBytes(r, addr, data, "application/octet-stream")
	if err != nil {
		w.t.Fatal(err)
	}
	anchor := c.Anchor()
	e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: l.Heads(), Address: addr, Verb: content.VerbPut, Payload: payload}, sec.Signer())
	if err != nil {
		w.t.Fatal(err)
	}
	if err := r.Event(e.ID, e.Head, e.Body, addr); err != nil {
		w.t.Fatal(err)
	}
	if err := r.SetRef(defaultBranch, e.ID); err != nil {
		w.t.Fatal(err)
	}
	if out, err := r.Commit(); out != vessel.Recorded {
		w.t.Fatalf("bring: %s %v", out, err)
	}
	return d.Hash
}

// takesIn lists the takes a carrier's ledger holds, read by the command.
func (w *seedWorld) takesIn(dir string) []event.Seed {
	w.t.Helper()
	var out []event.Seed
	for _, r := range w.log("owner", dir) {
		if r.Verb != event.VerbSeed {
			continue
		}
		s, err := event.DecodeSeed(r.payload(w.t))
		if err != nil {
			w.t.Fatal(err)
		}
		if s.Op == event.SeedTake {
			out = append(out, s)
		}
	}
	return out
}

func sameIDs(a, b []seedRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

// Step 2 (S5, goal 7.41): repeating the command for a folder that took the
// give records nothing and says so. Not one byte of either vessel changes,
// with the same size or another.
func TestRepeatingASeedRecordsNothing(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	srcFiles, dstFiles, srcLog := w.digests(src), w.digests(dst), w.log("owner", src)
	for _, size := range []string{"16M", "32M"} {
		out := w.must("owner", "seed", src, "--size", size, "--slab", "256K", dst)
		if !strings.Contains(out, "already took this seed; nothing was recorded") {
			t.Fatalf("a repeated seed (--size %s) does not say it records nothing: %q", size, out)
		}
		if !reflect.DeepEqual(w.digests(src), srcFiles) || !reflect.DeepEqual(w.digests(dst), dstFiles) {
			t.Fatalf("a repeated seed (--size %s) wrote into a vessel", size)
		}
	}
	if !sameIDs(w.log("owner", src), srcLog) || len(w.takesIn(dst)) != 1 {
		t.Fatal("a repeated seed changed a ledger")
	}
}

// Step 2 (goal 7.41), through the real command: a seed whose copy does not
// fit is cut after its give. The same command again uses that give and its
// keys, and records no second one; with a larger --size it grows the seed and
// goes on from where it stopped, to one take of the one give.
func TestAGiveRecordedAndNeverTakenIsUsedAgain(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	data := make([]byte, 5<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	archive := w.bring(src, "work/archive", data)
	before := w.log("owner", src)
	out, errOut, code := w.run("owner", "seed", src, "--size", "4M", "--slab", "256K", dst)
	if code != 3 || !strings.Contains(out, "give recorded") || !strings.Contains(out, "copy not-recorded") || !strings.Contains(errOut, "larger --size") {
		t.Fatalf("a seed that does not fit: exit %d\n%s%s", code, out, errOut)
	}
	given := w.log("owner", src)
	if len(given) != len(before)+3 {
		t.Fatalf("the cut seed's give is %d events", len(given)-len(before))
	}
	give := given[len(given)-1]
	out, errOut, code = w.run("owner", "seed", src, "--size", "4M", "--slab", "256K", dst)
	if code != 3 || !strings.Contains(out, "used again") {
		t.Fatalf("the same seed again, still too small: exit %d\n%s%s", code, out, errOut)
	}
	if !sameIDs(w.log("owner", src), given) {
		t.Fatal("repeating a cut seed recorded a second give")
	}
	out = w.must("owner", "seed", src, "--size", "32M", "--slab", "256K", dst)
	for _, want := range []string{"grown to 128 slabs", "used again", "copy recorded", "take recorded"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the seed going on does not say %q: %q", want, out)
		}
	}
	if !sameIDs(w.log("owner", src), given) {
		t.Fatal("the seed going on recorded on the source")
	}
	if v := w.must("owner", "verify", dst); !strings.Contains(v, "0 rejected, 0 pending") {
		t.Fatalf("verify of the seed:\n%s", v)
	}
	takes := w.takesIn(dst)
	if len(takes) != 1 || takes[0].Give.String() != give.ID {
		t.Fatalf("the seed holds %d takes, not one of give %s", len(takes), give.ID)
	}
	var got bytes.Buffer
	if _, err := w.open("owner", dst).ContentTo(archive, &got); err != nil || !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("the content did not arrive whole: %v", err)
	}
}

// Step 2: a seed cut after its copy and before its take (as a power cut
// would leave it) is taken by the real command, once, with nothing copied
// twice and no second give.
func TestASeedCutBeforeItsTakeIsTakenOnce(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.pass("owner")
	cutBeforeTake(t, src, dst, w.phrase["owner"])
	given := w.log("owner", src)
	if len(w.takesIn(src)) != 0 {
		t.Fatal("the source holds a take")
	}
	out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)
	for _, want := range []string{"used again", "copy recorded (+0", "take recorded"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the cut seed going on does not say %q: %q", want, out)
		}
	}
	if !sameIDs(w.log("owner", src), given) {
		t.Fatal("the cut seed going on recorded on the source")
	}
	if len(w.takesIn(dst)) != 1 {
		t.Fatal("the seed does not hold one take")
	}
	w.must("owner", "verify", dst)
}

// cutBeforeTake runs the command's own steps in this process and cuts them
// after the copy: the give is recorded on src, dst is made and filled, and
// no take is recorded.
func cutBeforeTake(t *testing.T, src, dst, pass string) {
	t.Helper()
	c, lock, _, err := openCarrier(src, pass, true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	plan, keys, err := v1SeedPlan(c, pass, SeedAsk{})
	if err != nil {
		t.Fatal(err)
	}
	if h := recordOn(c, lock, plan.Give, plan.Branch); h.Outcome != vessel.Recorded {
		t.Fatalf("the give: %s %v", h.Outcome, h.Err)
	}
	side, err := judgedSide(c, lock, pass)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	var dl *turn.Lock
	target := lineage.SeedTarget{M: medium.Dir{Root: dst}, Params: vessel.Params{SlabLog2: 18, Slabs: 64, Rand: rand.Reader},
		VK: keys.VK, Slots: keys.Slots, Sealer: keys.Sealer, Vessel: keys.Vessel,
		Owner: func() (vessel.Owner, error) {
			l, err := turn.Acquire(dst, v1Patience)
			if err != nil {
				return nil, err
			}
			dl = l
			return l, nil
		}}
	plan.Take = func(frame.ID) (lineage.Event, error) { return lineage.Event{}, errors.New("cut before the take") }
	res, err := lineage.Seed(side, target, plan)
	if dl != nil {
		dl.Release()
	}
	if err == nil || res.Copy.Outcome != vessel.Recorded || res.Take.Outcome == vessel.Recorded {
		t.Fatalf("the cut: %v %+v", err, res)
	}
}

// Step 2 (contract section 6): --attempt names the act. The same name again
// finds the same give even where the folder was lost, and makes the seed anew
// with that give; another name is another seed, and a folder that holds one
// attempt's seed is refused to another.
func TestTheSameAttemptFindsTheSameGive(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.must("owner", "seed", src, "--attempt", "first", "--size", "16M", "--slab", "256K", dst)
	given := w.log("owner", src)
	give := given[len(given)-1]
	if out := w.must("owner", "seed", src, "--attempt", "first", "--size", "16M", "--slab", "256K", dst); !strings.Contains(out, "nothing was recorded") {
		t.Fatalf("the same attempt again: %q", out)
	}
	// The folder is lost: the same attempt finds its give and makes it anew.
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	out := w.must("owner", "seed", src, "--attempt", "first", "--size", "16M", "--slab", "256K", dst)
	if !strings.Contains(out, "used again") || !strings.Contains(out, "take recorded") {
		t.Fatalf("the same attempt on a lost folder: %q", out)
	}
	if !sameIDs(w.log("owner", src), given) {
		t.Fatal("the same attempt recorded a second give")
	}
	if takes := w.takesIn(dst); len(takes) != 1 || takes[0].Give.String() != give.ID {
		t.Fatal("the seed made anew does not take the attempt's one give")
	}
	w.must("owner", "verify", dst)
	// Another attempt's name does not take this folder.
	out, errOut, code := w.run("owner", "seed", src, "--attempt", "second", "--size", "16M", "--slab", "256K", dst)
	if code != 3 || !strings.Contains(errOut, "not the seed of attempt") || !sameIDs(w.log("owner", src), given) {
		t.Fatalf("another attempt on this folder: exit %d\n%s%s", code, out, errOut)
	}
	// Another name on another folder is another seed.
	w.must("owner", "seed", src, "--attempt", "second", "--size", "16M", "--slab", "256K", w.dir("other"))
	if n := len(w.log("owner", src)); n != len(given)+3 {
		t.Fatalf("another attempt recorded %d events, not a give of 3", n-len(given))
	}
}

// Step 2: a folder that holds anything but this seed is refused before
// anything is recorded, and nothing there is touched: a vessel of another
// rokh (anchor_differs), the source itself, a seed of other scopes, and a
// vessel folder that holds no vessel.
func TestAFolderThatIsNotThisSeedIsRefused(t *testing.T) {
	w := newSeedWorld(t)
	src, other, slice, junk := w.dir("src"), w.dir("other"), w.dir("slice"), w.dir("junk")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "init", other, "--message", "another synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "seed", src, "--scope", "journal", "--size", "16M", "--slab", "256K", slice)
	if err := os.MkdirAll(filepath.Join(junk, "rokh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junk, "rokh", "note.txt"), []byte("not a vessel"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcLog := w.log("owner", src)
	for _, c := range []struct {
		dst, why string
		args     []string
	}{
		{other, "another rokh", nil},
		{src, "a folder of its own", nil},
		{slice, "holds a seed of journal", nil},
		{junk, "does not open as a whole vessel", nil},
		{slice, "holds a seed of journal", []string{"--scope", "work"}},
	} {
		files := w.digests(c.dst)
		args := append(append([]string{"seed", src}, c.args...), "--size", "16M", "--slab", "256K", c.dst)
		out, errOut, code := w.run("owner", args...)
		if code == 0 || !strings.Contains(errOut, c.why) {
			t.Fatalf("seed into %s: exit %d, want %q\n%s%s", c.dst, code, c.why, out, errOut)
		}
		if !reflect.DeepEqual(w.digests(c.dst), files) {
			t.Fatalf("a refused seed wrote into %s", c.dst)
		}
		if !sameIDs(w.log("owner", src), srcLog) {
			t.Fatalf("a refused seed into %s recorded on the source", c.dst)
		}
	}
	if b, err := os.ReadFile(filepath.Join(junk, "rokh", "note.txt")); err != nil || string(b) != "not a vessel" {
		t.Fatal("a refused seed touched a foreign file")
	}
}

// seedRecord is one record of a vessel as the owner reads it: an event whole
// or as a head only, or one chunk of content.
type seedRecord struct {
	kind     byte
	id       frame.ID
	headOnly bool
	head     []byte
	body     []byte // an event's opened body
	plain    []byte // a chunk's opened bytes
}

// records opens every record a carrier holds with the owner's passphrase:
// what a slice holds, byte by byte.
func (w *seedWorld) records(dir string) []seedRecord {
	w.t.Helper()
	c := w.open("owner", dir)
	info := c.Vessel().Info()
	sec, _, err := key.Try(w.phrase["owner"], c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		w.t.Fatal(err)
	}
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		w.t.Fatal(err)
	}
	max := vessel.MaxChunk(info.SlabLog2)
	var out []seedRecord
	err = c.Vessel().Scan(func(h vessel.Header, ref vessel.Ref) error {
		switch h.Type {
		case vessel.RecEvent:
			r := seedRecord{kind: h.Type, id: h.ID, head: append([]byte(nil), h.Head...), headOnly: carrier.IsHeadOnly(ref)}
			if !r.headOnly {
				env, err := c.Vessel().Body(ref)
				if err != nil {
					return err
				}
				if r.body, err = key.OpenReaders(key.TypeEvent, h.ID, nil, env, owner); err != nil {
					return fmt.Errorf("event %s does not open for the owner: %v", h.ID.Short(), err)
				}
			}
			out = append(out, r)
		case vessel.RecContent:
			env, err := c.Vessel().Body(ref)
			if err != nil {
				return err
			}
			l := uint32(max)
			if h.Chunk+1 == h.Chunks {
				l = uint32(h.Size - uint64(h.Chunk)*uint64(max))
			}
			plain, err := key.OpenReaders(key.TypeContent, h.ID, carrier.At(h.Chunk, h.Chunks, h.Size, l), env, owner)
			if err != nil {
				return fmt.Errorf("content %s does not open for the owner: %v", h.ID.Short(), err)
			}
			out = append(out, seedRecord{kind: h.Type, id: h.ID, plain: plain})
		}
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

// Step 3 (contract 4.7 S3, 3.3; B6): rokh seed --scope a,b makes a seed that
// holds its scopes and their necessary lineage, and nothing else. Every
// record of the slice is opened with the owner's passphrase and read: each
// whole event lies in a scope or is a system event; every other event of the
// source is there as a head only, an ancestor of what the slice holds whole;
// content outside the scopes is not there; and no address, verb, payload or
// content byte from outside is found in anything the slice holds. The whole
// seed holds everything whole. Later, reconcile brings the slice what lands
// in its scopes, the head of what lies beneath it, and nothing after it.
func TestASlicedSeedHoldsItsScopesAndTheirLineageOnly(t *testing.T) {
	w := newSeedWorld(t)
	src, slice, whole := w.dir("src"), w.dir("slice"), w.dir("whole")
	scopes := []string{"journal", "work"}
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "reader", "--reads", "journal", "--key-passphrase-file", w.pass("reader"))
	w.must("owner", "write", src, "--address", "private/diary", "--message", "OUTSIDE-diary-7f3a")
	w.must("owner", "write", src, "--address", "journal/day", "--verb", "entry", "--message", "INSIDE-journal")
	w.must("owner", "write", src, "--address", "notes/idea", "--verb", "outside-verb", "--message", "OUTSIDE-notes-91c2")
	w.must("owner", "write", src, "--address", "work/plan", "--message", "INSIDE-work")
	inData := bytes.Repeat([]byte("INSIDE-work-file "), 40000)
	outData := bytes.Repeat([]byte("OUTSIDE-private-file "), 40000)
	inID, outID := w.bring(src, "work/files", inData), w.bring(src, "private/files", outData)
	w.must("owner", "seed", src, "--scope", "work,journal", "--size", "16M", "--slab", "256K", slice)
	srcRows := w.log("owner", src)
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", whole)
	if v := w.must("owner", "verify", slice); !strings.Contains(v, "0 rejected, 0 pending") {
		t.Fatalf("verify of the slice:\n%s", v)
	}
	if got := w.open("owner", slice).Vessel().Info().Scopes; !reflect.DeepEqual(got, scopes) {
		t.Fatalf("the slice's vessel holds scopes %q", got)
	}
	l, err := sourceLedger(w.open("owner", src))
	if err != nil {
		t.Fatal(err)
	}
	outside := [][]byte{[]byte("OUTSIDE-"), []byte("private/"), []byte("notes/"), []byte("outside-verb")}
	check := func(recs []seedRecord) (map[frame.ID]bool, map[frame.ID]bool, map[frame.ID]bool) {
		t.Helper()
		wholeIDs, headIDs, contentIDs := map[frame.ID]bool{}, map[frame.ID]bool{}, map[frame.ID]bool{}
		for _, r := range recs {
			for _, b := range [][]byte{r.head, r.body, r.plain} {
				for _, o := range outside {
					if bytes.Contains(b, o) {
						t.Fatalf("the slice holds %q in a record of %s", o, r.id.Short())
					}
				}
			}
			switch {
			case r.kind == vessel.RecContent:
				contentIDs[r.id] = true
			case r.headOnly:
				headIDs[r.id] = true
			default:
				wholeIDs[r.id] = true
				if addr, _ := carrier.BodyAddress(r.body); addr != event.AddressRoot && !lineage.Within(addr, scopes) {
					t.Fatalf("an event at %q is whole in the slice", addr)
				}
			}
		}
		var held []frame.ID
		for id := range wholeIDs {
			held = append(held, id)
		}
		beneath := map[frame.ID]bool{}
		for _, id := range l.CausalPast(held...) {
			beneath[id] = true
		}
		for id := range headIDs {
			if !beneath[id] {
				t.Fatalf("the head of %s is in the slice and is no ancestor of what it holds", id.Short())
			}
		}
		return wholeIDs, headIDs, contentIDs
	}
	wholeIDs, headIDs, contentIDs := check(w.records(slice))
	for _, r := range srcRows {
		id, err := frame.ParseID(r.ID)
		if err != nil {
			t.Fatal(err)
		}
		inside := r.Address == event.AddressRoot || lineage.Within(r.Address, scopes)
		if inside && !wholeIDs[id] {
			t.Fatalf("%s at %q is inside and not whole in the slice", r.ID, r.Address)
		}
		if !inside && (wholeIDs[id] || !headIDs[id]) {
			t.Fatalf("%s at %q is outside and not a head only in the slice", r.ID, r.Address)
		}
	}
	if !contentIDs[inID] || contentIDs[outID] {
		t.Fatalf("content in the slice: inside %v, outside %v", contentIDs[inID], contentIDs[outID])
	}
	var got bytes.Buffer
	if _, err := w.open("owner", slice).ContentTo(inID, &got); err != nil || !bytes.Equal(got.Bytes(), inData) {
		t.Fatalf("the content inside is not whole in the slice: %v", err)
	}
	// The whole seed holds every event whole and every content.
	for _, r := range w.records(whole) {
		if r.kind == vessel.RecEvent && r.headOnly {
			t.Fatalf("the whole seed holds %s as a head only", r.id.Short())
		}
	}
	for _, id := range []frame.ID{inID, outID} {
		if _, err := w.open("owner", whole).ContentTo(id, io.Discard); err != nil {
			t.Fatalf("the whole seed lacks content %s: %v", id.Short(), err)
		}
	}

	// Later: outside, then inside on it, then outside again. The slice takes
	// the inside one whole, the one beneath it as a head only, and nothing of
	// the last.
	w.must("owner", "write", src, "--address", "private/later", "--message", "OUTSIDE-later")
	w.must("owner", "write", src, "--address", "work/later", "--message", "INSIDE-later")
	w.must("owner", "write", src, "--address", "private/last", "--message", "OUTSIDE-last")
	later := map[string]frame.ID{}
	for _, r := range w.log("owner", src) {
		if strings.HasSuffix(r.Address, "/later") || strings.HasSuffix(r.Address, "/last") {
			later[r.Address], _ = frame.ParseID(r.ID)
		}
	}
	w.must("owner", "reconcile", slice, src)
	if l, err = sourceLedger(w.open("owner", src)); err != nil {
		t.Fatal(err)
	}
	wholeIDs, headIDs, _ = check(w.records(slice))
	if !wholeIDs[later["work/later"]] || !headIDs[later["private/later"]] || wholeIDs[later["private/later"]] {
		t.Fatal("reconcile did not bring the slice what lands in its scope with the head beneath it")
	}
	if wholeIDs[later["private/last"]] || headIDs[later["private/last"]] {
		t.Fatal("reconcile brought the slice an event outside it that nothing inside rests on")
	}
}

// Step 3 (S1): a seed of a slice lies inside it. The whole rokh and a scope
// outside the slice are refused before anything is recorded; a scope inside
// it is given, from the slice, and names the slice as its source.
func TestASeedOfASliceLiesInsideIt(t *testing.T) {
	w := newSeedWorld(t)
	src, slice, sub := w.dir("src"), w.dir("slice"), w.dir("sub")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "write", src, "--address", "work/files/one", "--message", "INSIDE-one")
	w.must("owner", "write", src, "--address", "work/plan", "--message", "INSIDE-two")
	w.must("owner", "seed", src, "--scope", "work", "--size", "16M", "--slab", "256K", slice)
	sliceLog := w.log("owner", slice)
	for _, bad := range [][]string{nil, {"--scope", "private"}, {"--scope", "notes,work"}} {
		args := append(append([]string{"seed", slice}, bad...), "--size", "16M", "--slab", "256K", sub)
		out, errOut, code := w.run("owner", args...)
		if code == 0 || !strings.Contains(errOut, "inside") {
			t.Fatalf("seed %v of a slice: exit %d\n%s%s", bad, code, out, errOut)
		}
		if !sameIDs(w.log("owner", slice), sliceLog) {
			t.Fatalf("a refused seed %v of a slice recorded", bad)
		}
		if _, err := os.Stat(sub); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a refused seed %v of a slice made its folder", bad)
		}
	}
	w.must("owner", "seed", slice, "--scope", "work/files", "--size", "16M", "--slab", "256K", sub)
	w.must("owner", "verify", sub)
	si, ui := w.open("owner", slice).Vessel().Info(), w.open("owner", sub).Vessel().Info()
	if !reflect.DeepEqual(ui.Scopes, []string{"work/files"}) {
		t.Fatalf("the seed of the slice holds %q", ui.Scopes)
	}
	rows := w.log("owner", slice)
	give, err := event.DecodeSeed(rows[len(rows)-1].payload(t))
	if err != nil || give.Op != event.SeedGive || frame.ID(give.Source) != si.Seed || frame.ID(give.Seed) != ui.Seed {
		t.Fatalf("the give on the slice does not name the slice as its source: %v %+v", err, give)
	}
}

// bodies is a view's application bodies, as the command shows them: the
// payload of every whole event outside the system address, by address.
func bodies(rows []seedRow) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		if r.Address != event.AddressRoot && r.Payload != "" {
			out[r.Address] = r.Payload
		}
	}
	return out
}

// opensOwnCell says which cell of a vessel a role's passphrase opens: the
// key it holds, or false when it opens none.
func (w *seedWorld) opensOwnCell(role, dir string) ([32]byte, bool) {
	w.t.Helper()
	w.pass(role)
	v := w.open("owner", dir).Vessel()
	info := v.Info()
	sec, _, err := key.Try(w.phrase[role], v.Slots(), info.Salt, info.Iter)
	if errors.Is(err, key.ErrPassphrase) {
		return [32]byte{}, false
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return sec.Key, true
}

// Step 3, by a ruling of the design (axiom 19, contract 4.6): a
// whole seed takes the cell of every live key generation whose add carries a
// slot. A reader's own passphrase opens the seed and shows its view there as
// on the source, byte for byte, and nothing outside it; a key revoked on the
// source before the give opens nothing on the seed; and repeating the seed
// still records nothing and changes no byte.
func TestAWholeSeedTakesTheCellsOfItsLiveKeys(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	w.must("owner", "key", "add", src, "--name", "reader", "--reads", "journal", "--key-passphrase-file", w.pass("reader"))
	w.must("owner", "key", "add", src, "--name", "gone", "--reads", "journal", "--key-passphrase-file", w.pass("gone"))
	w.must("owner", "write", src, "--address", "journal/day", "--message", "INSIDE: a synthetic journal line\nwith a second line")
	w.must("owner", "write", src, "--address", "private/diary", "--message", "OUTSIDE: a synthetic private line")
	w.must("owner", "key", "revoke", src, "--key", "gone")
	w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst)

	onSource, onSeed := bodies(w.log("reader", src)), bodies(w.log("reader", dst))
	if len(onSeed) != 1 || onSeed["journal/day"] == "" || !reflect.DeepEqual(onSeed, onSource) {
		t.Fatalf("the reader's view on the seed is not its view on the source:\n seed   %v\n source %v", onSeed, onSource)
	}
	if got, _ := base64.StdEncoding.DecodeString(onSeed["journal/day"]); string(got) != "INSIDE: a synthetic journal line\nwith a second line" {
		t.Fatalf("the reader reads other bytes on the seed: %q", got)
	}
	if id, ok := w.opensOwnCell("reader", dst); !ok || id == ([32]byte{}) {
		t.Fatal("the reader's passphrase does not open its own cell of the seed")
	}
	if _, ok := w.opensOwnCell("gone", dst); ok {
		t.Fatal("a key revoked before the give has a cell on the seed")
	}
	if out, errOut, code := w.run("gone", "log", dst, "--json"); code == 0 {
		t.Fatalf("a key revoked before the give opened the seed:\n%s%s", out, errOut)
	}
	files, srcFiles := w.digests(dst), w.digests(src)
	if out := w.must("owner", "seed", src, "--size", "16M", "--slab", "256K", dst); !strings.Contains(out, "nothing was recorded") {
		t.Fatalf("the repeated seed: %q", out)
	}
	if !reflect.DeepEqual(w.digests(dst), files) || !reflect.DeepEqual(w.digests(src), srcFiles) {
		t.Fatal("repeating the seed changed a byte")
	}
}

// Step 3, by the same decision: a slice takes the cell of a key only when
// its reads, or a live grant to its signer, touch the slice's scope. A key
// that reads the sliced scope opens the slice and reads it there; a key that
// reads only another scope does not open it; a key that writes in the scope
// and reads nothing, and a key that reads everything, take their cells too.
func TestASliceTakesTheCellsOfTheKeysThatTouchIt(t *testing.T) {
	w := newSeedWorld(t)
	src, slice := w.dir("src"), w.dir("slice")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	for _, k := range [][]string{
		{"journalist", "--reads", "journal"},
		{"worker", "--reads", "work"},
		{"scribe", "--write", "--scope", "journal"},
		{"all", "--reads", "*"},
	} {
		args := append([]string{"key", "add", src, "--name", k[0], "--key-passphrase-file", w.pass(k[0])}, k[1:]...)
		w.must("owner", args...)
	}
	w.must("owner", "write", src, "--address", "journal/day", "--message", "INSIDE: a synthetic journal line")
	w.must("owner", "write", src, "--address", "work/plan", "--message", "OUTSIDE: a synthetic work line")
	w.must("owner", "seed", src, "--scope", "journal", "--size", "16M", "--slab", "256K", slice)

	if got := bodies(w.log("journalist", slice)); len(got) != 1 || got["journal/day"] == "" {
		t.Fatalf("the journalist's view of the slice: %v", got)
	}
	if _, ok := w.opensOwnCell("worker", slice); ok {
		t.Fatal("a key that reads only another scope has a cell on the slice")
	}
	if out, errOut, code := w.run("worker", "log", slice, "--json"); code == 0 {
		t.Fatalf("a key that reads only another scope opened the slice:\n%s%s", out, errOut)
	}
	for _, role := range []string{"journalist", "scribe", "all"} {
		if id, ok := w.opensOwnCell(role, slice); !ok || id == ([32]byte{}) {
			t.Fatalf("%s's passphrase does not open its own cell of the slice", role)
		}
	}
	if got := bodies(w.log("all", slice)); len(got) != 1 || got["journal/day"] == "" {
		t.Fatalf("a key that reads everything reads outside the slice's scope there: %v", got)
	}
}

// Step 3, by the same decision: a give used again may be older than the
// source's present. The seed made anew with it takes the cell of a key live
// at the give and still live now, and no cell of a key taken back since the
// give, nor of one added after it.
func TestAGiveUsedAgainGivesNoCellToAKeyTakenBackSince(t *testing.T) {
	w := newSeedWorld(t)
	src, dst := w.dir("src"), w.dir("seed")
	w.must("owner", "init", src, "--message", "synthetic genesis", "--size", "16M", "--slab", "256K")
	for _, name := range []string{"reader", "later-gone"} {
		w.must("owner", "key", "add", src, "--name", name, "--reads", "journal", "--key-passphrase-file", w.pass(name))
	}
	w.must("owner", "write", src, "--address", "journal/day", "--message", "a synthetic line")
	w.must("owner", "seed", src, "--attempt", "first", "--size", "16M", "--slab", "256K", dst)
	for _, role := range []string{"reader", "later-gone"} {
		if _, ok := w.opensOwnCell(role, dst); !ok {
			t.Fatalf("%s has no cell on the first seed", role)
		}
	}
	w.must("owner", "key", "revoke", src, "--key", "later-gone")
	w.must("owner", "key", "add", src, "--name", "newcomer", "--reads", "journal", "--key-passphrase-file", w.pass("newcomer"))
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	if out := w.must("owner", "seed", src, "--attempt", "first", "--size", "16M", "--slab", "256K", dst); !strings.Contains(out, "used again") {
		t.Fatalf("the seed made anew: %q", out)
	}
	if _, ok := w.opensOwnCell("reader", dst); !ok {
		t.Fatal("a key live at the give and now has no cell on the seed made anew")
	}
	if _, ok := w.opensOwnCell("later-gone", dst); ok {
		t.Fatal("a key taken back after the give has a cell on the seed made anew")
	}
	if _, ok := w.opensOwnCell("newcomer", dst); ok {
		t.Fatal("a key added after the give has a cell on the seed made anew")
	}
}
