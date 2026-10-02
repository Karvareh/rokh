package gate

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"

	"rokh/booth"
)

// A strict consumer's bridge speaks these lines, in this order, on
// the pipe the gate bound to its program: hello offering a credential, prove
// with the explicitly empty credential, whoami, then its reading ops. Every
// message carries the protocol and an id of its own, which the answer echoes.
func TestTheBridgeSpeaksTheBoothOnItsBoundPipe(t *testing.T) {
	f := newGate(t)
	f.importText("note.md", "synthetic research text about a library.\n", "research/note.md")
	id, _ := f.program("reader", map[string]string{"bytes": "research", "search": "research"}, nil)
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	go f.s.serveBooth(b, id)
	in := bufio.NewReader(a)
	seq := 0
	send := func(op string, fields map[string]any) map[string]any {
		t.Helper()
		seq++
		m := map[string]any{"v": booth.Protocol, "id": seq, "op": op}
		for k, v := range fields {
			m[k] = v
		}
		line, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
		back, err := in.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var ans map[string]any
		if err := json.Unmarshal(back, &ans); err != nil {
			t.Fatal(err)
		}
		if ans["id"] != float64(seq) || ans["v"] != booth.Protocol {
			t.Fatalf("%s: the answer is not to message %d: %v", op, seq, ans)
		}
		return ans
	}
	if r := send("search", map[string]any{"query": "synthetic"}); r["code"] != booth.CodeHelloFirst {
		t.Fatalf("answered before hello: %v", r)
	}
	if h := send("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "credential"}); h["ok"] != true {
		t.Fatalf("hello: %v", h)
	}
	if p := send("prove", map[string]any{"credential": ""}); p["ok"] != true || p["program"] != id {
		t.Fatalf("the empty credential on the pipe bound to %s did not bind it: %v", id, p)
	}
	who := send("whoami", nil)
	if who["ok"] != true || who["consumer"] != id || who["name"] != "reader" {
		t.Fatalf("whoami: %v", who)
	}
	if r := send("search", map[string]any{"query": "synthetic"}); r["ok"] != true {
		t.Fatalf("a reading op inside the grant: %v", r)
	}
	if r := send("bytes", map[string]any{"path": "research/note.md"}); r["ok"] != true {
		t.Fatalf("bytes inside the grant: %v", r)
	}
	// A prove that gives no credential at all binds nothing, on this pipe too.
	send("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "credential"})
	if p := send("prove", map[string]any{}); p["ok"] == true {
		t.Fatalf("a prove with no credential bound the pipe: %v", p)
	}
	if r := send("search", map[string]any{"query": "synthetic"}); r["code"] != booth.CodeHelloFirst {
		t.Fatalf("answered after a failed prove: %v", r)
	}
}
