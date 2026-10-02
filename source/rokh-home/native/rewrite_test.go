package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A restore rewrites this release's own configuration files and nothing else.
//
// The fixture is the point: a data file whose name ends in .json and another
// whose name ends in .yaml, both holding the retired path as bytes. Round three
// rewrote anything under the keystore with a configuration suffix, which would
// have edited exactly these. They must come back byte for byte, and be named in
// the answer so an operator sees them.
func TestOnlyThisReleasesConfigurationIsRewritten(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	old := fakeRuntime(t, p)

	// Two data files with configuration suffixes, and one real configuration
	// file. All three name the folder they were written in.
	fixtures := map[string]string{
		"ks/store.json":         "\x00\x01lair store bytes, not JSON: " + old + "/ks\x00\x02",
		"data/cache.yaml":       "sqlite page holding " + old + "/data as a literal\n",
		"data/conductor.sqlite": "SQLite format 3\x00 " + old + " \x00",
	}
	for name, body := range fixtures {
		full := filepath.Join(old, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	p.RuntimeDir, p.Quiesced, p.HeadRead = old, true, true
	p.LastHead = ChainHead{Seq: 1, Action: "uhCkkSYNTHETIC"}
	p.AgentKey, p.DNAHash = "uhCAkAGENT", "uhC0kDNA"
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "before-the-move"); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "runtime-b")
	out, err := m.Restore(p.ID, fresh)
	if err != nil {
		t.Fatal(err)
	}

	rewritten := list(out["paths_rewritten"])
	for _, name := range rewritten {
		if !configFiles(testManifest(t))[name] {
			t.Fatalf("%s is not one of this release's configuration files and must not have been rewritten", name)
		}
	}
	if !has(rewritten, "ks/lair-keystore-config.yaml") {
		t.Fatalf("the keystore's own configuration should have been rewritten: %v", rewritten)
	}

	// The data files come back exactly as they were sealed.
	for name, body := range fixtures {
		got, err := os.ReadFile(filepath.Join(fresh, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Fatalf("%s was changed by the restore:\n want %q\n  got %q", name, body, string(got))
		}
	}

	// And they are named, so nobody has to discover them by a failed start.
	still := list(out["paths_still_named"])
	for name := range fixtures {
		if !has(still, name) {
			t.Fatalf("%s still names the retired folder and should have been reported: %v", name, still)
		}
	}
}

// A file that does not name a retired folder is neither rewritten nor reported.
func TestAFileThatNamesNothingIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "conductor.yaml"), []byte("nothing retired here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rewritten, still, _, err := rewritePaths(dir, []string{"/private/tmp/some-older-runtime"}, configFiles(testManifest(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rewritten) != 0 || len(still) != 0 {
		t.Fatalf("nothing to do, got %v and %v", rewritten, still)
	}
}

// The streamed search finds a name that straddles a chunk boundary: a file
// looked at in pieces must not be a file half looked at.
func TestAStraddlingNameIsStillFound(t *testing.T) {
	dir := t.TempDir()
	old := "/private/tmp/rokh-retired-runtime-abcdef"
	body := make([]byte, (1<<20)+len(old)+7)
	for i := range body {
		body[i] = 'x'
	}
	copy(body[(1<<20)-len(old)/2:], old)
	path := filepath.Join(dir, "data", "big.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := fileContainsAny(path, []string{old})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("a name that crosses a chunk boundary is still a name in the file")
	}
	if found, err := fileContainsAny(path, []string{old + "-and-more"}); err != nil || found {
		t.Fatalf("a name that is not there must not be found: %v %v", found, err)
	}
}

func list(v any) []string {
	out, _ := v.([]string)
	return out
}

func has(list []string, want string) bool {
	for _, v := range list {
		if v == want || strings.TrimPrefix(v, "./") == want {
			return true
		}
	}
	return false
}

// A new folder whose path extends a retired one cannot be told apart from it by
// looking at bytes. The rewrite is still exact; the report says so rather than
// naming files that only carry the new path.
func TestAFolderThatExtendsARetiredOneIsSaidSo(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "runtime")
	fresh := filepath.Join(root, "runtime-b")
	if err := os.MkdirAll(filepath.Join(fresh, "ks"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(fresh, "ks", "lair-keystore-config.yaml")
	if err := os.WriteFile(cfg, []byte("storeFile: "+old+"/ks/store\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A data file that names only the new folder. Searching it for the old one
	// would find it, because the new path contains it.
	data := filepath.Join(fresh, "ks", "store")
	if err := os.WriteFile(data, []byte("lair store bytes naming "+fresh+"/ks"), 0o600); err != nil {
		t.Fatal(err)
	}
	rewritten, still, ambiguous, err := rewritePaths(fresh, []string{old}, configFiles(testManifest(t)))
	if err != nil {
		t.Fatal(err)
	}
	if !has(rewritten, "ks/lair-keystore-config.yaml") {
		t.Fatalf("the configuration is still rewritten: %v", rewritten)
	}
	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "storeFile: "+fresh+"/ks/store\n" {
		t.Fatalf("the rewrite must be exact even here: %q", string(got))
	}
	if len(still) != 0 {
		t.Fatalf("a file that names only the new folder must not be reported: %v", still)
	}
	if len(ambiguous) != 1 || ambiguous[0] != old {
		t.Fatalf("the limit has to be said out loud: %v", ambiguous)
	}
}
