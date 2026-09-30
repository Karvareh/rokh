package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layer ranks packages. A package may import its own layer and anything below
// it, never anything above.
var layer = map[string]int{
	// canon and seal import nothing of Rokh's: one is the byte order of a
	// JSON value, the other is ciphers. They sit under everything, so
	// anything may use them and they can reach nothing.
	"canon": 0,
	// size is how a person writes a number of bytes, in one place so that a
	// chest's size and a ledger's reservation cannot disagree about "2G".
	"size":  0,
	"frame": 0, "event": 0, "ledger": 0, "carrier": 0,
	// The v1 core beside them (contract C1): the vessel keeps, the key opens,
	// the lineage seeds and reconciles.
	"vessel": 0, "key": 0, "lineage": 0,
	// Host adapters: files and the writer's lock, and the booth transports.
	// They are the only places per-OS code may live (contract 2.9, C11).
	"medium": 1, "transport": 1,
	// The one passphrase rule of every command and the surface: it reads a
	// file, the environment or the terminal, and knows nothing of Rokh.
	"passphrase": 1,
	// The booth protocol rides on the core and knows no transport (B1).
	"booth":      2,
	"announce":   1,
	"oracle":     1,
	"working":    1,
	"generation": 1,
	"seal":       1,
	// The writing turn: the one lock every writer on a machine takes before it
	// records, so a precondition on the heads means the same in the daemon, the
	// command line and the sentence surface. It imports nothing of Rokh's.
	"turn":     1,
	"covenant": 2, "content": 2, "bundle": 2, "receipt": 2,
	"harness": 2, "bond": 2, "answer": 2, "selective": 2,
	// The key layer over an opened carrier: which session a passphrase
	// opens, the view it holds, and the owner at a record's own point. It
	// reads the carrier through its public door, as the surface does.
	"keyview": 2,
	// The sentence surface. It is a package rather than a command so the one
	// binary a person installs can be it, and it sits above the layers it
	// speaks for while reaching no socket at all.
	"shell": 3,
	// The screen surface: the same sentences drawn on a measured terminal.
	// It imports nothing of Rokh's — the shell hands it a snapshot and takes
	// back the sentence typed — so it sits beside the shell, not under it.
	"tui": 3,
	// The other carrier profile: one file with an encrypted filesystem in it.
	// It knows nothing of ledgers and no ledger knows of it; it makes a
	// folder appear and go away again.
	"chest": 3,
}

func importsOf(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	var out []string
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, imp := range f.Imports {
				out = append(out, strings.Trim(imp.Path.Value, `"`))
			}
		}
	}
	return out
}

// The core must never import a layer above it. If this breaks, either the
// import is wrong or the layering has changed and docs/07 needs a ruling.
func TestLayersOnlyPointDownwards(t *testing.T) {
	root := ".."
	for pkg, rank := range layer {
		dir := filepath.Join(root, pkg)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("package %s is missing; every v1 package is required", pkg)
			continue
		}
		for _, imp := range importsOf(t, dir) {
			if !strings.HasPrefix(imp, "rokh/") {
				continue
			}
			other := strings.TrimPrefix(imp, "rokh/")
			otherRank, known := layer[other]
			if !known {
				t.Errorf("%s imports %s, which is not placed in any layer", pkg, imp)
				continue
			}
			if otherRank > rank {
				t.Errorf("%s (layer %d) imports %s (layer %d): layers may only point downwards",
					pkg, rank, imp, otherRank)
			}
		}
	}
}

// The core must not know what a peer, a covenant, a want or a file is.
func TestCoreKnowsNothingOfPeering(t *testing.T) {
	for _, pkg := range []string{"frame", "event", "ledger", "carrier"} {
		for _, imp := range importsOf(t, filepath.Join("..", pkg)) {
			switch imp {
			case "rokh/covenant", "rokh/content", "rokh/bundle", "rokh/announce", "rokh/daemon":
				t.Errorf("core package %s imports %s", pkg, imp)
			}
		}
	}
}

// Nothing outside the daemon may open a socket, and the daemon opens only a
// Unix one. Rokh has no network listener.
// For something to have happened, no observer is needed. There is not one
// code path in Rokh that opens a network socket, and an offline ledger is
// not an incomplete one.
//
//	— T1.4, N-Axiom5
func TestNoNetworkListenerOutsideTheDaemon(t *testing.T) {
	for pkg := range layer {
		if pkg == "transport" {
			continue // the booth transports are the listeners (contract B1)
		}
		body := codeOnly(t, filepath.Join("..", pkg))
		for _, bad := range []string{"net.Listen", `"tcp"`, "net/http"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s contains %q; only the daemon may hold a socket, and only a Unix one", pkg, bad)
			}
		}
	}
	d := readAll(t, filepath.Join("..", "daemon"))
	if strings.Contains(d, `"tcp"`) || strings.Contains(d, "net/http") {
		t.Error("the daemon opened a network listener; it must stay Unix-domain only")
	}
	tr := ""
	if _, err := os.Stat(filepath.Join("..", "transport")); err == nil {
		tr = readAll(t, filepath.Join("..", "transport"))
	}
	if !strings.Contains(d, `net.Listen("unix"`) && !strings.Contains(tr, `"unix"`) {
		t.Error("neither the daemon nor the transports listen on a Unix socket")
	}
}

// No database anywhere: committed content is plain files, and an index, if one
// is ever needed, is a separate ruling.
func TestNoDatabase(t *testing.T) {
	dirs := []string{"canon", "frame", "event", "ledger", "carrier", "announce",
		"oracle", "working", "generation", "covenant", "content", "bundle",
		"receipt", "seal", "harness", "bond", "answer", "selective", "daemon",
		"shell", "tui", "chest", "size", "turn"}
	for _, d := range dirs {
		body := strings.ToLower(codeOnly(t, filepath.Join("..", d)))
		for _, bad := range []string{"database/sql", "sqlite", "postgres", "mysql"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s mentions %q; a database needs its own ruling", d, bad)
			}
		}
	}
}

func readAll(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
	}
	return sb.String()
}

// The sentence shell is an adapter: no socket of any kind, no announce or
// daemon reach, and the ledger only through its public door. These checks
// hold the directives that placed it.
func TestSentenceShellBoundaries(t *testing.T) {
	dir := filepath.Join("..", "shell")
	body := readAll(t, dir)

	// No listener, no dialer, no network package at all: the shell works on
	// carriers directly and never speaks to a socket.
	for _, bad := range []string{`"net"`, "net.Listen", "net.Dial", "net/http", `"tcp"`} {
		if strings.Contains(body, bad) {
			t.Errorf("the shell contains %q; it holds no socket of any kind", bad)
		}
	}

	// Announce and hello are machine affairs behind the bridge; they must not
	// leak into the human surface. The daemon is the bridge itself.
	for _, imp := range importsOf(t, dir) {
		switch imp {
		case "rokh/announce", "rokh/daemon":
			t.Errorf("the shell imports %s; that layer must not leak into the sentence surface", imp)
		}
	}

	// No database, same rule as everywhere else.
	low := strings.ToLower(body)
	for _, bad := range []string{"database/sql", "sqlite", "postgres", "mysql"} {
		if strings.Contains(low, bad) {
			t.Errorf("rokh-shell mentions %q; a database needs its own ruling", bad)
		}
	}
}
