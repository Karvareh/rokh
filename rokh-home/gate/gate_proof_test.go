package gate

// Probes: the gate's booth on a home whose ledger is a v1
// vessel, with the fixture of gate_test.go.

import (
	"net"
	"path/filepath"
	"testing"

	"rokh/booth"
)

// B-2 at the gate: what a program that proved nothing is answered, and what
// a program with a credential is answered outside its grants.
func TestTheGateAnswersNothingWithoutAProof(t *testing.T) {
	f := newGate(t)
	f.importText("note.md", "synthetic research text about a library.\n", "research/note.md")
	f.importText("secret.md", "synthetic private text.\n", "private/secret.md")
	id, cred := f.program("reader", map[string]string{"bytes": "research", "search": "research"}, nil)
	sock := filepath.Join(f.run, "gate.sock")

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := booth.NewClient(conn)
	if r, err := c.Call("search", map[string]any{"query": "synthetic"}); err != nil || r["code"] != booth.CodeHelloFirst {
		t.Fatalf("a request before hello: %v %v", r, err)
	}
	c.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "credential"})
	p, _ := c.Call("prove", map[string]any{})
	t.Logf("prove with nothing: ok=%v key=%v program=%q", p["ok"], p["key"], p["program"])
	if p["ok"] == true {
		t.Errorf("DEFECT B-2: the gate's booth bound a session that proved nothing")
	}
	for _, req := range []struct {
		op     string
		fields map[string]any
	}{
		{"whoami", nil},
		{"search", map[string]any{"query": "synthetic"}},
		{"bytes", map[string]any{"path": "research/note.md"}},
		{"bytes", map[string]any{"path": "private/secret.md"}},
	} {
		r, err := c.Call(req.op, req.fields)
		if err != nil {
			t.Fatalf("%s: %v", req.op, err)
		}
		t.Logf("%-8s %v: ok=%v code=%v", req.op, req.fields, r["ok"], r["code"])
		if r["ok"] == true && req.op != "whoami" {
			t.Errorf("DEFECT B-2: the gate served %q to a session that proved nothing", req.op)
		}
		if r["ok"] == true && req.op == "whoami" {
			t.Errorf("DEFECT B-2: whoami was answered to a session that proved nothing (B3)")
		}
	}

	// The same stream, now with the program's credential.
	who, err := c.ProveCredential(cred)
	if err != nil || who["ok"] != true || who["program"] != id {
		t.Fatalf("the program did not bind through rokh.booth/1: %v %v", who, err)
	}
	if r, _ := c.Call("bytes", map[string]any{"path": "research/note.md"}); r["ok"] != true {
		t.Errorf("a program was refused inside its grant: %v", r)
	}
	if r, _ := c.Call("bytes", map[string]any{"path": "private/secret.md"}); r["ok"] == true {
		t.Errorf("DEFECT: a program read outside its grant through rokh.booth/1")
	}
	// After the owner withdraws it, the bound session is answered nothing.
	f.owner(map[string]any{"op": "revoke", "consumer": id})
	if r, _ := c.Call("bytes", map[string]any{"path": "research/note.md"}); r["ok"] == true {
		t.Errorf("DEFECT: a withdrawn program still reads through its bound session")
	}
}
