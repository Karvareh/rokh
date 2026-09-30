package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// importsOfTree returns, for every package in the tree, everything it imports —
// including from its test files, because a dependency introduced by a test is
// still a dependency the package can be made to have.
func importsOfTree(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	err := filepath.Walk("..", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, _ := filepath.Rel("..", p)
		pkg := filepath.ToSlash(filepath.Dir(rel))
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		if out[pkg] == nil {
			out[pkg] = map[string]bool{}
		}
		for _, imp := range f.Imports {
			out[pkg][strings.Trim(imp.Path.Value, `"`)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The receipt is a custom the harnesses keep on Rokh, not a part of Rokh. The
// core knows events and nothing else — and this reads the import graph rather
// than the text, so a rename or a synonym cannot slip past it.
//
//	— T10.5, T10
func TestTheImportGraphKeepsTheCoreIgnorantOfReceipts(t *testing.T) {
	imports := importsOfTree(t)
	for _, pkg := range []string{"frame", "event", "ledger", "carrier", "working", "generation"} {
		for imp := range imports[pkg] {
			if strings.HasPrefix(imp, "rokh/receipt") || strings.HasPrefix(imp, "rokh/answer") ||
				strings.HasPrefix(imp, "rokh/harness") || strings.HasPrefix(imp, "rokh/bond") {
				t.Errorf("%s imports %s; the ledger knows events and nothing else", pkg, imp)
			}
		}
	}
}

// Rokh is upstream of meaning and lineage — not of cryptography and the
// network. It is not a network path, not a content store and not an identity
// system: it takes those into service from wheels that already turn, and
// rebuilds none of them. The import graph is where that is either true or not.
//
//	— T11, T11.1, T11.2, T11.3, N9.3, N9.4
func TestTheImportGraphShowsNoWheelRebuilt(t *testing.T) {
	imports := importsOfTree(t)
	// Everything the tree may reach for outside its own module.
	allowedPrefixes := []string{
		"rokh/", // its own layers, governed by TestLayersOnlyPointDownwards
		"crypto/", "encoding/", "errors", "fmt", "io", "os", "sort", "strings",
		"strconv", "bufio", "bytes", "path/filepath", "sync", "time", "unicode",
		"container/", "math", "reflect", "testing", "go/", "flag", "hash/",
		"net", // the daemon's Unix socket only; see the network test
		"regexp", "runtime", "slices", "maps", "iter", "context", "text/",
	}
	for pkg, imps := range imports {
		if strings.HasPrefix(pkg, "cmd/") {
			continue // the commands are allowed the standard library at large
		}
		for imp := range imps {
			if pkg == "turn" && (imp == "syscall" || imp == "unsafe") {
				continue // the writing turn is the kernel's lock; see TestOnlyTheTurnReachesTheKernel
			}
			if pkg == "medium" && imp == "syscall" {
				continue // its tests measure allocated blocks; see TestOnlyTheTurnReachesTheKernel
			}
			ok := false
			for _, a := range allowedPrefixes {
				if imp == a || strings.HasPrefix(imp, a) {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%s imports %q — a wheel Rokh should take into service, "+
					"not rebuild or vendor", pkg, imp)
			}
		}
	}
	// And nothing outside the module at all: no dependency is the strongest
	// form of "we did not rebuild it here".
	mod, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), "require") {
		t.Error("go.mod has a dependency; the base profile declares none")
	}
}

// The core layers may not reach for a network, and only the daemon may hold a
// socket at all — a Unix one. Read from the import graph, so a package that
// merely mentions the word is not accused, and one that quietly imports a
// transport cannot hide.
//
//	— T1.4, N-Axiom5
func TestNoCoreLayerImportsATransport(t *testing.T) {
	// Product files only: a test may join two ends of an in-memory stream
	// with net.Pipe, which is no transport and opens no socket.
	imports := importsOfTreeNonTest(t)
	for pkg := range layer {
		if pkg == "transport" {
			continue // it is the transport
		}
		for imp := range imports[pkg] {
			switch {
			case imp == "net", strings.HasPrefix(imp, "net/"):
				t.Errorf("%s imports %q; only the daemon may hold a socket", pkg, imp)
			}
		}
	}
	if !imports["daemon"]["net"] && !imports["transport"]["net"] {
		t.Error("neither the daemon nor the transports import net; either the gateway moved or this " +
			"test has stopped watching anything")
	}
}

// The writing turn is the kernel's advisory lock on the carrier's system
// directory, and taking it is the one thing in the tree that reaches past the
// standard library's portable surface into the kernel. Nothing else may: a
// package that wants the kernel for something else wants a ruling first.
//
//	— T8.5, T11.1
func TestOnlyTheTurnReachesTheKernel(t *testing.T) {
	imports := importsOfTree(t)
	for pkg, imps := range imports {
		if strings.HasPrefix(pkg, "cmd/") || pkg == "turn" {
			continue
		}
		if pkg == "medium" || pkg == "transport" {
			continue // host adapters, outside the core (contract 2.9)
		}
		if imps["syscall"] {
			t.Errorf("%s imports syscall; only the writing turn reaches the kernel", pkg)
		}
	}
	if !imports["turn"]["syscall"] {
		t.Error("the turn no longer takes the kernel's lock; either it moved or this test watches nothing")
	}
}

// importsOfTreeNonTest is importsOfTree without the test files.
func importsOfTreeNonTest(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	err := filepath.Walk("..", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel("..", p)
		pkg := filepath.ToSlash(filepath.Dir(rel))
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		if out[pkg] == nil {
			out[pkg] = map[string]bool{}
		}
		for _, imp := range f.Imports {
			out[pkg][strings.Trim(imp.Path.Value, `"`)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
