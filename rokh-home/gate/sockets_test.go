package gate

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAProgramReachesOnlyTheNamedUnixSocket(t *testing.T) {
	f := newGate(t)
	a, _ := f.program("local-client", nil, nil)
	base, err := shortTemp(t, "rku")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(base, "core.sock")
	other := filepath.Join(base, "other.sock")
	for _, p := range []string{allowed, other} {
		ln, err := net.Listen("unix", p)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				go func() { defer c.Close(); io.Copy(c, c) }()
			}
		}()
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("the real Unix socket test needs Python:", err)
	}
	script := `import socket,sys
for i,p in enumerate(sys.argv[1:]):
 s=socket.socket(socket.AF_UNIX);s.settimeout(2)
 try:
  s.connect(p);s.sendall(b"synthetic echo");data=s.recv(100)
  if i!=0 or data!=b"synthetic echo":sys.exit(31)
  print("named endpoint worked")
 except OSError:
  if i==0:raise
  print("unnamed endpoint refused")
 finally:s.close()
`
	spec := RunSpec{Consumer: a, Argv: []string{python, "-c", script, allowed, other}, Dir: t.TempDir(), Wait: true, UnixSockets: []string{allowed}}
	r, err := f.s.run(spec)
	if err != nil || r["exit"] != 0 || r["stdout"] != "named endpoint worked\nunnamed endpoint refused\n" {
		t.Fatalf("actual socket enclosure: %+v %v", r, err)
	}
	// A directory is not a socket grant, and cannot silently widen the rule.
	spec.UnixSockets = []string{base}
	if _, err := f.s.run(spec); err == nil {
		t.Fatal("a directory-wide socket grant was accepted")
	}
}
