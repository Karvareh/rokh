//go:build darwin

package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A folder the owner names is the program's to write in whether or not it has
// been made yet, and a folder the owner did not name stays shut. On this system
// /tmp is a link into /private, so a rule written under the name as given would
// never match what the kernel sees.
func TestTheEnclosureGrantsTheFoldersTheOwnerNamed(t *testing.T) {
	f := newGate(t)
	id, _ := f.program("writer", nil, nil)

	base, err := shortTemp(t, "rkw")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	work := filepath.Join(base, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	later := filepath.Join(base, "later") // named, but not made yet
	shut := filepath.Join(base, "shut")   // made, but never named
	if err := os.Mkdir(shut, 0o700); err != nil {
		t.Fatal(err)
	}

	script := "mkdir -p " + later + " && echo made >" + filepath.Join(later, "f") +
		" && echo wrote-named; if echo x >" + filepath.Join(shut, "f") + " 2>/dev/null; then echo WROTE-UNNAMED; else echo unnamed-shut; fi"
	spec := map[string]any{"consumer": id, "argv": []string{"/bin/sh", "-c", script},
		"dir": work, "rw": []string{work, later}, "wait": true, "timeout_s": 60,
		"env": map[string]string{"PATH": "/usr/bin:/bin", "HOME": work}}
	r := f.owner(map[string]any{"op": "run", "spec": spec})
	out, _ := r["stdout"].(string)
	t.Logf("enclosure %s: exit=%v stdout=%q stderr=%q", Enclosure, r["exit"], out, r["stderr"])

	if !strings.Contains(out, "wrote-named") {
		t.Errorf("the program could not write in the folder the owner named but had not made")
	}
	if b, err := os.ReadFile(filepath.Join(later, "f")); err != nil || strings.TrimSpace(string(b)) != "made" {
		t.Errorf("the named folder holds nothing: %v", err)
	}
	if !strings.Contains(out, "unnamed-shut") || strings.Contains(out, "WROTE-UNNAMED") {
		t.Errorf("the program wrote into a folder the owner never named")
	}
	if _, err := os.Stat(filepath.Join(shut, "f")); err == nil {
		t.Errorf("a file appeared in the folder the owner never named")
	}
}
