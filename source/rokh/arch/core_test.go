package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The portable core (contract C1, C11, acceptance S4): these packages import
// the standard library only, never an operating-system, clock, lock, socket or
// process package, and carry no build tag and no per-OS file. Files, locks,
// clocks, sockets and processes reach them only through injected interfaces.
var corePackages = []string{"frame", "event", "ledger", "carrier", "vessel", "key", "lineage"}

var forbiddenInCore = []string{
	"os", "path/filepath", "time", "syscall", "os/exec", "os/user", "os/signal",
	"net", "unsafe", "runtime",
}

// Entropy is a host resource too: the core takes every random byte from an
// injected io.Reader (C5), and draws none of its own. Test files may use a
// fixed stream.
var entropyInCore = []string{"crypto/rand", "math/rand", "math/rand/v2"}

// Packages whose tests run on a Medium in memory with no file system (S4).
// The rest of the core carries older tests that still read testdata files.
var memoryTested = map[string]bool{"vessel": true, "carrier": true, "lineage": true}

var perOS = []string{"_darwin", "_linux", "_windows", "_android", "_unix", "_ios",
	"_freebsd", "_openbsd", "_netbsd", "_plan9", "_js", "_wasip1", "_solaris"}

func TestThePortableCoreReachesNoOperatingSystem(t *testing.T) {
	for _, pkg := range corePackages {
		dir := filepath.Join("..", pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("core package %s is missing; every v1 core package is required: %v", pkg, err)
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") {
				continue
			}
			isTest := strings.HasSuffix(name, "_test.go")
			base := strings.TrimSuffix(strings.TrimSuffix(name, ".go"), "_test")
			for _, suf := range perOS {
				if strings.HasSuffix(base, suf) {
					t.Errorf("%s/%s is a per-OS file in the core", pkg, name)
				}
			}
			p := filepath.Join(dir, name)
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(src), "//go:build") || strings.Contains(string(src), "// +build") {
				t.Errorf("%s/%s carries a build tag in the core", pkg, name)
			}
			f, err := parser.ParseFile(token.NewFileSet(), p, src, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if !isTest {
					for _, bad := range entropyInCore {
						if path == bad {
							t.Errorf("%s/%s imports %q; the core draws no entropy of its own (C5)", pkg, name, path)
						}
					}
				}
			}
			if isTest && !memoryTested[pkg] {
				continue
			}
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, bad := range forbiddenInCore {
					if path == bad {
						t.Errorf("%s/%s imports %q; the core reaches the host only through injected interfaces", pkg, name, path)
					}
				}
			}
		}
	}
}
