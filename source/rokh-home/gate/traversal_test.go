package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGrantedPathsCanBeWalkedWithoutOpeningSiblings(t *testing.T) {
	f := newGate(t)
	a, _ := f.program("path-walker", nil, nil)
	base := t.TempDir()
	allowed := filepath.Join(base, "one", "two", "allowed")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(allowed, "file"), []byte("synthetic permitted bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(base, "one", "private-sibling")
	if err := os.WriteFile(sibling, []byte("synthetic forbidden sibling"), 0o600); err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	script := `import os,sys
p=os.path.realpath(sys.argv[1])
fd=os.open("/",os.O_RDONLY|os.O_DIRECTORY)
for part in p.strip("/").split("/"):
 n=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=fd)
 os.close(fd);fd=n
n=os.open("file",os.O_RDONLY|os.O_NOFOLLOW,dir_fd=fd)
assert os.read(n,100)==b"synthetic permitted bytes"
os.close(n);os.close(fd)
try:
 open(sys.argv[2],"rb").read()
 raise SystemExit(31)
except (PermissionError,FileNotFoundError):pass
print("safe traversal worked; sibling refused")
`
	r, err := f.s.run(RunSpec{Consumer: a, Argv: []string{python, "-c", script, allowed, sibling}, Dir: allowed, ReadOnly: []string{allowed}, Writable: []string{}, Wait: true})
	if err != nil || r["exit"] != 0 || r["stdout"] != "safe traversal worked; sibling refused\n" {
		t.Fatalf("descriptor traversal: %+v %v", r, err)
	}
}
