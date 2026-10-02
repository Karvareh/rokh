package native

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh-home/store"
)

// sealed makes a real sealed store to run against: the guards below are about
// what actually reaches disk, so none of them is tested against a stub.
func sealed(t *testing.T) *store.Home {
	t.Helper()
	root := t.TempDir()
	h, err := store.Create(root, "synthetic-test-passphrase", 1000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

func manager(t *testing.T) *Manager {
	t.Helper()
	return New(sealed(t), Binaries{Manifest: testManifest(t)})
}

// testManifest is the engine manifest the tests run under: the file named by
// ROKH_ENGINE_MANIFEST, or the first one in testdata. A second manifest that
// names another release runs the same tests with no change of code.
func testManifest(t *testing.T) Manifest {
	t.Helper()
	path := os.Getenv("ROKH_ENGINE_MANIFEST")
	if path == "" {
		path = filepath.Join("testdata", "engine-holochain-0.7.0.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func configFiles(m Manifest) map[string]bool {
	out := map[string]bool{}
	for _, f := range m.ReleaseConfigFiles {
		out[f] = true
	}
	return out
}

// fakeEngine writes executables that answer --version the way the wanted
// release does, so a guard that runs before anything is started can be tested
// without a conductor on the machine.
func fakeEngine(t *testing.T) Binaries {
	t.Helper()
	dir := t.TempDir()
	write := func(name, out string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho '"+out+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	man := testManifest(t)
	happ := filepath.Join(dir, man.App.File)
	if err := os.WriteFile(happ, []byte("not a real bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Binaries{
		Conductor: write(man.Conductor.File, man.Conductor.Version),
		CLI:       write(man.CLI.File, man.CLI.Version),
		Bridge:    write(man.Bridge.File, "bridge 0.1.0"),
		HApp:      happ,
		Manifest:  man,
	}
}

func newProfile(t *testing.T, m *Manager) *Profile {
	t.Helper()
	p, err := m.Create(CreateInput{Label: "synthetic", Bootstrap: "http://127.0.0.1:9", Relay: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeRuntime(t *testing.T, p *Profile) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"data/conductor.sqlite3":       "SQLite format 3\x00 synthetic body bytes",
		"ks/lair-keystore-config.yaml": "storeFile: " + dir + "/ks/store\n",
		"conductor.yaml":               `{"data_root_path":"` + dir + `/data"}`,
		"conductor.log":                "this log is not part of the state",
		"runtime.lock":                 "",
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeStamp(dir, stamp{Format: RuntimeFormat, Profile: p.ID, Generation: p.Generation,
		AppID: p.AppID, WrittenUTC: nowUTC()}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProfileViewHasNoSecrets(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	if len(p.ConductorPassphrase) == 0 {
		t.Fatal("a profile should hold a generated passphrase")
	}
	b, err := json.Marshal(p.View())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), string(p.ConductorPassphrase)) {
		t.Fatal("the passphrase reached a view meant for answers and evidence")
	}
	if !strings.Contains(string(b), "not recorded") {
		t.Fatal("the view should say a secret is held and not recorded")
	}
}

func TestCheckpointRefusesALiveConductor(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	p.RuntimeDir, p.Quiesced, p.HeadRead = fakeRuntime(t, p), true, true
	p.Live = &Live{PID: os.Getpid(), RuntimeDir: p.RuntimeDir, Generation: p.Generation}
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "attempt-1"); !errors.Is(err, ErrNotQuiesced) {
		t.Fatalf("want ErrNotQuiesced, got %v", err)
	}
}

func TestCheckpointRefusesAnUnclearEnding(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	p.RuntimeDir, p.Quiesced = fakeRuntime(t, p), false
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "attempt-1"); !errors.Is(err, ErrNotQuiesced) {
		t.Fatalf("an ending that was never learned must not be checkpointed: %v", err)
	}
}

func TestCheckpointThenRestore(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	source := fakeRuntime(t, p)
	p.RuntimeDir, p.Quiesced, p.HeadRead = source, true, true
	p.LastHead = ChainHead{Seq: 12, Action: "uhCkkTEST"}
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	out, err := m.Checkpoint(p.ID, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	snap := out["snapshot"].(*Snapshot)
	if !snap.Complete || snap.Generation != 2 || snap.Head.Seq != 12 {
		t.Fatalf("unexpected snapshot %+v", snap)
	}
	// The log and the lock are this process's, not the conductor's state.
	for _, e := range snap.Files {
		if e.Path == "conductor.log" || e.Path == lockFile {
			t.Fatalf("%s should not be in a checkpoint", e.Path)
		}
	}
	// The same attempt again is the checkpoint already taken.
	again, err := m.Checkpoint(p.ID, "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	if again["already"] != true {
		t.Fatal("repeating an attempt should return the checkpoint already taken")
	}

	fresh := filepath.Join(t.TempDir(), "root-b")
	restored, err := m.Restore(p.ID, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if restored["files"].(int) != len(snap.Files) {
		t.Fatalf("restored %v of %d files", restored["files"], len(snap.Files))
	}
	body, err := os.ReadFile(filepath.Join(fresh, "data/conductor.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "synthetic body bytes") {
		t.Fatal("the restored database is not the one that was sealed")
	}
	// Nothing in the fresh folder may still name the folder it came from.
	ks, err := os.ReadFile(filepath.Join(fresh, "ks/lair-keystore-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ks), source) {
		t.Fatal("the restored keystore configuration still names the retired folder")
	}
	st, found, err := readStamp(fresh)
	if err != nil || !found {
		t.Fatalf("the restored folder has no stamp: %v", err)
	}
	if st.Generation != 2 {
		t.Fatalf("the restored folder is stamped at generation %d", st.Generation)
	}
}

func TestRestoreRefusesASnapshotThatIsNotTheCurrentOne(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	p.RuntimeDir, p.Quiesced, p.HeadRead = fakeRuntime(t, p), true, true
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "attempt-1"); err != nil {
		t.Fatal(err)
	}
	// A later checkpoint moved the identity on; the older one is behind work
	// the peers have already seen.
	p, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.Generation++
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(p.ID, filepath.Join(t.TempDir(), "root-c")); !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
}

func TestStartRefusesARetiredRuntimeFolder(t *testing.T) {
	m := New(sealed(t), fakeEngine(t))
	p := newProfile(t, m)
	dir := fakeRuntime(t, p)
	p.RuntimeDir, p.Quiesced, p.HeadRead = dir, true, true
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "attempt-1"); err != nil {
		t.Fatal(err)
	}
	// The folder is stamped at generation 1 and the identity is at 2. The guard
	// is in Start, before any process is launched.
	_, err := m.Start(StartInput{Profile: p.ID, RuntimeDir: dir})
	if !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
}

func TestStartRefusesASecondConductorForOneIdentity(t *testing.T) {
	m := New(sealed(t), fakeEngine(t))
	p := newProfile(t, m)
	dir := fakeRuntime(t, p)
	p.Live = &Live{PID: os.Getpid(), RuntimeDir: dir, Generation: p.Generation}
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(StartInput{Profile: p.ID, RuntimeDir: dir}); !errors.Is(err, ErrLive) {
		t.Fatalf("want ErrLive, got %v", err)
	}
}

func TestOfferNamesAMissingEngineRatherThanFailing(t *testing.T) {
	m := New(sealed(t), Binaries{Conductor: "/nonexistent/holochain", Manifest: testManifest(t)})
	e := m.Offer()
	if e.Present || e.Compatible || e.Reason == "" {
		t.Fatalf("a missing engine should be reported, got %+v", e)
	}
	if e.CPUs < 1 || e.Concurrency < 1 {
		t.Fatal("the host's capacity should be reported")
	}
}

func TestOfferNamesAVersionMismatch(t *testing.T) {
	bin := fakeEngine(t)
	if err := os.WriteFile(bin.Conductor, []byte("#!/bin/sh\necho 'holochain 0.6.0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	e := New(sealed(t), bin).Offer()
	if !e.Present || e.Compatible || !strings.Contains(e.Reason, "0.6.0") {
		t.Fatalf("a mismatched engine should be named, got %+v", e)
	}
}

func TestAssessReportsWhatIsReadableOnDisk(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	p.RuntimeDir = fakeRuntime(t, p)
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	a, err := m.Assess(p.ID, []string{"synthetic body bytes", "a string that is not there"}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.SQLiteFiles != 1 {
		t.Fatalf("expected one SQLite file, got %d", a.SQLiteFiles)
	}
	if !a.ContentReadable {
		t.Fatal("a database holding the needle verbatim is readable content")
	}
	if !a.WholeFilesRead {
		t.Fatal("every file of this small runtime should have been read whole")
	}
	if len(a.ExposedFiles) != 1 || a.ExposedFiles[0] != "data/conductor.sqlite3" {
		t.Fatalf("the exposed file should be named exactly once: %v", a.ExposedFiles)
	}
	found := false
	for _, f := range a.Findings {
		if f.Path == "data/conductor.sqlite3" {
			if len(f.Plaintext) != 1 || f.Plaintext[0] != "synthetic body bytes" {
				t.Fatalf("unexpected finding %+v", f)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("the database was not assessed")
	}
}

// A search that finds nothing at all has proved nothing; a control says so.
func TestAssessSaysWhenItsSearchProvedNothing(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	p.RuntimeDir = fakeRuntime(t, p)
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	a, err := m.Assess(p.ID, []string{"absent"}, []string{"also absent"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.SearchTrustworthy || !strings.Contains(a.Conclusion, "cannot be relied on") {
		t.Fatalf("a search with no control hit must not claim anything: %+v", a.Conclusion)
	}
	b, err := m.Assess(p.ID, []string{"absent"}, []string{"synthetic body bytes"}, []string{"definitely-not-here-4f9a"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !b.SearchTrustworthy || b.ContentReadable || len(b.ControlsFound) != 1 || len(b.AbsentFound) != 0 {
		t.Fatalf("with the control found and nothing else, the answer should say so: %+v", b)
	}
}

func TestObserveRefusesAWritingFunction(t *testing.T) {
	m := manager(t)
	if _, err := m.Observe("whatever", "publish_manuscript", nil); err == nil ||
		!strings.Contains(err.Error(), "not a reading function") {
		t.Fatalf("observe must refuse a writing function, got %v", err)
	}
}

func TestPublishNeedsANamedAttempt(t *testing.T) {
	m := manager(t)
	if _, err := m.Publish(PublishInput{Profile: "x"}); err == nil {
		t.Fatal("a publication without an attempt should be refused")
	}
}
