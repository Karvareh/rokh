package gate

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"rokh-home/home"
	"rokh-home/store"
)

const pass = "synthetic gate passphrase"

type gateFixture struct {
	t    *testing.T
	s    *Server
	h    *home.Home
	run  string
	root string
	oc   *OwnerClient
}

func newGate(t *testing.T) *gateFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := home.Create(root, pass, "synthetic genesis of a gate test", 1000); err != nil {
		t.Fatal(err)
	}
	h, err := home.Open(root, pass, "test")
	if err != nil {
		t.Fatal(err)
	}
	run, err := shortTemp(t, "rkg")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(h, run)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.OwnerKey(root, pass)
	if err != nil {
		t.Fatal(err)
	}
	oc, err := DialOwner(filepath.Join(run, "owner.sock"), key)
	if err != nil {
		t.Fatal(err)
	}
	f := &gateFixture{t: t, s: s, h: h, run: run, root: root, oc: oc}
	t.Cleanup(func() {
		oc.Close()
		s.Close()
		h.Close()
		os.RemoveAll(run)
	})
	return f
}

func (f *gateFixture) owner(req map[string]any) map[string]any {
	f.t.Helper()
	r, err := f.oc.Call(req)
	if err != nil {
		f.t.Fatal(err)
	}
	if r["ok"] != true {
		f.t.Fatalf("owner %v: %v", req["op"], r)
	}
	return r
}

func (f *gateFixture) importText(name, body, path string) {
	f.t.Helper()
	src := filepath.Join(f.t.TempDir(), name)
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
	r := f.owner(map[string]any{"op": "import.preview", "source": src, "path": path})
	f.owner(map[string]any{"op": "import.run", "id": r["import"]})
}

func (f *gateFixture) program(name string, grants map[string]string, places map[string]string) (string, string) {
	f.t.Helper()
	req := map[string]any{"op": "consumer.add", "name": name, "kind": "test"}
	for k, v := range places {
		req[k] = v
	}
	r := f.owner(req)
	id := r["consumer"].(map[string]any)["id"].(string)
	for action, scope := range grants {
		f.owner(map[string]any{"op": "grant", "consumer": id, "action": action, "scope": scope})
	}
	return id, r["credential"].(string)
}

func call(t *testing.T, c *Client, req map[string]any) map[string]any {
	t.Helper()
	r, err := c.Call(req)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheOwnersChannelNeedsTheOwnersKey(t *testing.T) {
	f := newGate(t)
	if r := f.owner(map[string]any{"op": "home.status"}); r["items"] != float64(0) {
		t.Fatalf("status: %v", r)
	}
	wrong, _ := store.OwnerKey(f.root, "not the passphrase")
	if _, err := DialOwner(filepath.Join(f.run, "owner.sock"), wrong); !errors.Is(err, ErrProof) {
		t.Fatalf("a wrong key opened the owner's channel: %v", err)
	}
	key, _ := store.OwnerKey(f.root, pass)
	oc, err := DialOwner(filepath.Join(f.run, "owner.sock"), key)
	if err != nil {
		t.Fatal(err)
	}
	oc.sc.c.Write([]byte(base64.StdEncoding.EncodeToString([]byte("a frame nobody sealed")) + "\n"))
	if _, err := oc.Call(map[string]any{"op": "home.status"}); err == nil {
		t.Fatal("the channel went on after an unsealed frame")
	}
	c, err := Dial(filepath.Join(f.run, "gate.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if r := call(t, c, map[string]any{"op": "consumer.add", "name": "x"}); r["code"] != "hello_first" {
		t.Fatalf("the programs' socket answered an owner's op: %v", r)
	}
}

func TestAProgramIsWhatItsCredentialSays(t *testing.T) {
	f := newGate(t)
	f.importText("یادداشت.md", "متن ساختگی پژوهشی دربارهٔ کتاب‌خانه.\n", "research/یادداشت.md")
	f.importText("راز.md", "متن ساختگی خصوصی.\n", "private/راز.md")
	id, cred := f.program("reader", map[string]string{"bytes": "research", "search": "research"}, nil)
	sock := filepath.Join(f.run, "gate.sock")

	// rokh.booth/1 (T2): nothing is answered before hello and prove, and a
	// credential nobody holds binds nothing.
	c, _ := Dial(sock)
	if r := call(t, c, map[string]any{"op": "search", "query": "کتابخانه"}); r["code"] != "hello_first" {
		t.Fatalf("a request before hello: %v", r)
	}
	if r, _ := c.Bind(strings.Repeat("00", 32)); r["ok"] == true || r["code"] != "proof_failed" {
		t.Fatalf("a made-up credential: %v", r)
	}
	if r := call(t, c, map[string]any{"op": "whoami"}); r["code"] != "hello_first" {
		t.Fatalf("a made-up credential bound the session: %v", r)
	}
	c.Close()

	c, _ = Dial(sock)
	defer c.Close()
	if r, err := c.Bind(cred); err != nil || r["ok"] != true {
		t.Fatalf("bind: %v %v", r, err)
	}
	if r := call(t, c, map[string]any{"op": "whoami"}); r["consumer"] != id {
		t.Fatalf("the credential's program: %v", r)
	}
	r := call(t, c, map[string]any{"op": "bytes", "path": "research/یادداشت.md"})
	if data, _ := base64.StdEncoding.DecodeString(r["data"].(string)); string(data) != "متن ساختگی پژوهشی دربارهٔ کتاب‌خانه.\n" {
		t.Fatalf("bytes in scope: %v", r)
	}
	for _, req := range []map[string]any{
		{"op": "bytes", "path": "private/راز.md"},
		{"op": "bytes", "path": "private/راز.md", "consumer": "owner", "as": "owner", "actor": "root"},
		{"op": "consumer.add", "name": "sneaky"},
		{"op": "ledger", "request": map[string]any{"op": "log"}},
		{"op": "run", "spec": map[string]any{"consumer": id, "argv": []string{"/bin/sh"}}},
	} {
		if r := call(t, c, req); r["ok"] != false || r["code"] != "denied" {
			t.Fatalf("%v was not refused: %v", req, r)
		}
	}
	if hits := call(t, c, map[string]any{"op": "search", "query": "متن ساختگی"})["hits"].([]any); len(hits) != 1 {
		t.Fatalf("search reached outside scope: %v", hits)
	}

	f.owner(map[string]any{"op": "grant", "consumer": id, "action": "read", "scope": "research"})
	if r := call(t, c, map[string]any{"op": "bytes", "path": "research/یادداشت.md"}); r["reason"] != "stale_session" {
		t.Fatalf("a stale session: %v", r)
	}
	if r, err := c.Bind(cred); err != nil || r["ok"] != true {
		t.Fatalf("hello again: %v %v", r, err)
	}
	if r := call(t, c, map[string]any{"op": "bytes", "path": "research/یادداشت.md"}); r["ok"] != true {
		t.Fatalf("after hello again: %v", r)
	}

	f.owner(map[string]any{"op": "revoke", "consumer": id})
	if r := call(t, c, map[string]any{"op": "bytes", "path": "research/یادداشت.md"}); r["ok"] != false {
		t.Fatalf("a revoked program read: %v", r)
	}
	c2, _ := Dial(sock)
	defer c2.Close()
	if r, _ := c2.Bind(cred); r["ok"] == true || r["code"] != "proof_failed" {
		t.Fatalf("a revoked credential: %v", r)
	}
}

func TestABoundConnectionCannotBecomeAnotherProgram(t *testing.T) {
	f := newGate(t)
	a, _ := f.program("a", map[string]string{"read": "a"}, nil)
	_, credB := f.program("b", map[string]string{"read": "b"}, nil)
	file, err := f.s.Bind(a)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.FileConn(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(conn)
	defer c.Close()
	// rokh.booth/1 (T2): the connection is bound to its program by the empty
	// credential, and to no other: another program's credential binds
	// nothing on it, and a new binding is its own program again.
	if r, err := c.Bind(""); err != nil || r["ok"] != true {
		t.Fatalf("a bound connection did not bind as its program: %v %v", r, err)
	}
	if r := call(t, c, map[string]any{"op": "whoami"}); r["consumer"] != a {
		t.Fatalf("a bound connection is not its program: %v", r)
	}
	if r, _ := c.Bind(credB); r["ok"] == true || r["code"] != "proof_failed" {
		t.Fatalf("another program's credential on a bound connection: %v", r)
	}
	if r := call(t, c, map[string]any{"op": "whoami"}); r["code"] != "hello_first" {
		t.Fatalf("the connection went on as someone after presenting another program's credential: %v", r)
	}
	if r, err := c.Bind(""); err != nil || r["ok"] != true {
		t.Fatalf("a bound connection did not bind as its program again: %v %v", r, err)
	}
	if r := call(t, c, map[string]any{"op": "whoami"}); r["consumer"] != a {
		t.Fatalf("a bound connection became another program: %v", r)
	}
}

const memoryProbePython = `
import json, os, sys
def memory_handle(pid):
    if sys.platform == "darwin":
        import ctypes
        lib = ctypes.CDLL("/usr/lib/libSystem.B.dylib")
        me = ctypes.c_uint.in_dll(lib, "mach_task_self_").value
        task = ctypes.c_uint()
        lib.task_for_pid.argtypes = [ctypes.c_uint, ctypes.c_int, ctypes.POINTER(ctypes.c_uint)]
        code = lib.task_for_pid(me, int(pid), ctypes.byref(task))
        if code != 0:
            raise PermissionError("task_for_pid refused with code %d" % code)
        lib.mach_port_deallocate(me, task)
        return "task port obtained"
    fd = os.open("/proc/%s/mem" % pid, os.O_RDONLY)
    os.close(fd)
    return "memory descriptor obtained"
`

const probe = memoryProbePython + `
import socket
home_json, run_dir, personal, gate_pid = sys.argv[1:5]
out = {}
def attempt(name, fn):
    try:
        out[name] = {"ok": True, "value": fn()}
    except Exception as e:
        out[name] = {"ok": False, "error": type(e).__name__, "detail": str(e)}
fds = []
for fd in range(0, 256):
    try:
        os.fstat(fd)
        fds.append(fd)
    except OSError:
        pass
out["fds"] = fds
attempt("read_home_descriptor", lambda: open(home_json, "rb").read(16).hex())
attempt("list_home", lambda: os.listdir(os.path.dirname(home_json)))
attempt("list_run_dir", lambda: os.listdir(run_dir))
def dial(name):
    s = socket.socket(socket.AF_UNIX)
    s.settimeout(2)
    s.connect(os.path.join(run_dir, name))
    return "connected"
attempt("dial_owner_socket", lambda: dial("owner.sock"))
attempt("dial_gate_socket", lambda: dial("gate.sock"))
attempt("read_personal_standin", lambda: open(personal, "rb").read())
attempt("read_gate_memory", lambda: memory_handle(gate_pid))
gate = socket.socket(fileno=3)
f = gate.makefile("rw")
seq = [0]
def ask(req):
    # rokh.booth/1: every message names the protocol and an id of its own.
    seq[0] += 1
    req = dict(req, v="rokh.booth/1", id=str(seq[0]))
    f.write(json.dumps(req) + "\n"); f.flush()
    return json.loads(f.readline())
# Bound on the connection the gate handed it: hello, then prove with the
# empty credential that names this program.
out["hello"] = ask({"op": "hello", "auth": "credential"})
out["prove"] = ask({"op": "prove", "credential": ""})
out["whoami"] = ask({"op": "whoami"})
out["bytes_in_scope"] = ask({"op": "bytes", "path": "research/یادداشت.md"})
out["bytes_outside"] = ask({"op": "bytes", "path": "private/راز.md"})
out["claims_owner"] = ask({"op": "consumer.list", "as": "owner"})
print(json.dumps(out))
`

func TestALaunchedProgramReachesOnlyItsConnection(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	// Use the exact same native probe outside the enclosure as its positive
	// control. Seatbelt may refuse task_for_pid even for a sandbox's own PID.
	positive, err := exec.Command(python, "-c", memoryProbePython+"\nprint(memory_handle(os.getpid()))\n").CombinedOutput()
	if err != nil {
		t.Fatalf("native memory probe positive control: %v: %s", err, positive)
	}
	t.Logf("native memory probe positive control: %s", positive)
	f := newGate(t)
	f.importText("یادداشت.md", "متن ساختگی پژوهشی.\n", "research/یادداشت.md")
	f.importText("راز.md", "متن ساختگی خصوصی.\n", "private/راز.md")
	id, _ := f.program("probe", map[string]string{"bytes": "research"}, nil)
	work := t.TempDir()
	dir := filepath.Join(work, "consumer")
	os.Mkdir(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "probe.py"), []byte(probe), 0o600)
	personal := filepath.Join(t.TempDir(), "Documents-synthetic-standin")
	os.Mkdir(personal, 0o700)
	os.WriteFile(filepath.Join(personal, "note.txt"), []byte("synthetic stand-in for a personal file"), 0o600)
	spec := map[string]any{"consumer": id, "argv": []string{python, filepath.Join(dir, "probe.py"),
		filepath.Join(f.root, "home.json"), f.run, filepath.Join(personal, "note.txt"), itoa(os.Getpid())},
		"dir": dir, "rw": []string{dir}, "env": map[string]string{"PYTHONDONTWRITEBYTECODE": "1", "HOME": dir,
			"PATH": "/usr/bin:/bin:/opt/homebrew/bin", "LANG": "C.UTF-8"}, "wait": true, "timeout_s": 60}
	r := f.owner(map[string]any{"op": "run", "spec": spec})
	if r["exit"] != float64(0) {
		t.Fatalf("the probe exited %v\nstdout: %v\nstderr: %v", r["exit"], r["stdout"], r["stderr"])
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(r["stdout"].(string))), &out); err != nil {
		t.Fatalf("probe output: %v\n%v", err, r["stdout"])
	}
	t.Logf("enclosure %s on %s: %s", Enclosure, runtime.GOOS, r["stdout"])
	for _, name := range []string{"read_home_descriptor", "list_home", "list_run_dir", "dial_owner_socket",
		"dial_gate_socket", "read_personal_standin", "read_gate_memory"} {
		if res := out[name].(map[string]any); res["ok"] == true {
			t.Errorf("the enclosed program could %s: %v", name, res)
		}
	}
	fds := out["fds"].([]any)
	for _, fd := range fds {
		if fd.(float64) > 3 {
			t.Errorf("the enclosed program inherited descriptor %v (all: %v)", fd, fds)
		}
	}
	if w := out["whoami"].(map[string]any); w["consumer"] != id {
		t.Fatalf("whoami: %v", w)
	}
	if b := out["bytes_in_scope"].(map[string]any); b["ok"] != true {
		t.Fatalf("bytes in scope: %v", b)
	}
	for _, name := range []string{"bytes_outside", "claims_owner"} {
		if b := out[name].(map[string]any); b["ok"] != false || b["code"] != "denied" {
			t.Fatalf("%s: %v", name, b)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// shortTemp is a folder for sockets with a short path, inside the work tree's
// short folder (ROKH_SHORT_TMP, or the work tree's .t); never a literal /tmp.
func shortTemp(t *testing.T, prefix string) (string, error) {
	t.Helper()
	base := os.Getenv("ROKH_SHORT_TMP")
	if base == "" {
		base = filepath.Join("..", "..", "..", "..", "..", "..", ".t")
	}
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		t.Skip("no short socket folder in this work tree")
	}
	// Absolute: an enclosed program runs in its own folder, and a grant names
	// an absolute path.
	abs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(abs, prefix)
}
