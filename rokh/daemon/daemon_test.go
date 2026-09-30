//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

const testIter = 2000

type fixture struct {
	dir  string
	car  *carrier.Carrier
	led  *ledger.Ledger
	root ed25519.PrivateKey
	gen  event.Signed
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	_, root, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.Sign(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis, Payload: []byte("genesis"),
	}, root)
	if err != nil {
		t.Fatal(err)
	}
	c, err := carrier.Create(carrier.FS{Root: dir}, "pass", gen.ID,
		map[string][]byte{carrier.KeyRoot: root}, testIter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put(gen.Raw); err != nil {
		t.Fatal(err)
	}
	if err := c.SetRef("main", gen.ID); err != nil {
		t.Fatal(err)
	}
	l, err := ledger.New(gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &fixture{dir: dir, car: c, led: l, root: root, gen: gen}
}

// shortDir gives a socket directory well inside the sun_path limit, which
// the usual test temp directory is not.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := shortTemp(t, "rk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func ask(t *testing.T, s *Server, req map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return s.Handle(b)
}

// num reads a numeric field. Handle returns live Go values (int), while a
// response that has been through JSON gives float64; the tests exercise both
// paths, so accept either.
func num(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	t.Fatalf("not a number: %T", v)
	return 0
}

func mustOK(t *testing.T, resp map[string]any, why string) map[string]any {
	t.Helper()
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("%s: %v", why, resp["error"])
	}
	return resp
}

func TestReadOps(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	st := mustOK(t, ask(t, s, map[string]any{"op": "status"}), "status")
	if st["anchor"] != f.gen.ID.String() {
		t.Fatal("status reported the wrong anchor")
	}
	if num(t, st["accepted"]) != 1 {
		t.Fatalf("expected one accepted event, got %v", st["accepted"])
	}

	lg := mustOK(t, ask(t, s, map[string]any{"op": "log"}), "log")
	if len(lg["events"].([]map[string]any)) != 1 {
		t.Fatal("log did not return the genesis event")
	}

	g := mustOK(t, ask(t, s, map[string]any{"op": "get", "id": f.gen.ID.String()}), "get")
	if g["raw"] != hex.EncodeToString(f.gen.Raw) {
		t.Fatal("get returned different bytes")
	}

	an := mustOK(t, ask(t, s, map[string]any{"op": "announce"}), "announce")
	if num(t, an["bytes"]) > 51 {
		t.Fatalf("announcement is %v bytes, over one SF12 frame", an["bytes"])
	}

	if ok, _ := ask(t, s, map[string]any{"op": "nonsense"})["ok"].(bool); ok {
		t.Fatal("unknown op accepted")
	}
	if ok, _ := s.Handle([]byte("not json"))["ok"].(bool); ok {
		t.Fatal("malformed request accepted")
	}
}

// append is the generic write path: pre-signed bytes, no key involved.
func TestAppendTakesPreSignedBytes(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{})

	anchor := f.gen.ID
	e, err := event.Sign(event.Event{
		Carrier: &anchor, Parents: []frame.ID{f.gen.ID},
		Address: "home/journal", Verb: "note", Payload: []byte("written elsewhere"),
	}, f.root)
	if err != nil {
		t.Fatal(err)
	}
	resp := mustOK(t, ask(t, s, map[string]any{
		"op": "append", "raw": hex.EncodeToString(e.Raw),
	}), "append")
	if resp["id"] != e.ID.String() || resp["state"] != "accepted" {
		t.Fatalf("append returned %v", resp)
	}
	if ok, _ := f.car.Has(e.ID); !ok {
		t.Fatal("the accepted event did not reach the carrier")
	}

	// Garbage, and a tampered event, must both be refused and stored nowhere.
	before := f.led.Len()
	for _, bad := range []string{"", "zz", hex.EncodeToString([]byte("not an event"))} {
		if ok, _ := ask(t, s, map[string]any{"op": "append", "raw": bad})["ok"].(bool); ok {
			t.Fatalf("append accepted %q", bad)
		}
	}
	tampered := append([]byte(nil), e.Raw...)
	tampered[len(tampered)/2] ^= 0xFF
	if ok, _ := ask(t, s, map[string]any{
		"op": "append", "raw": hex.EncodeToString(tampered),
	})["ok"].(bool); ok {
		t.Fatal("append accepted a tampered event")
	}
	if f.led.Len() != before {
		t.Fatal("bad input grew the ledger")
	}
}

func TestWriteSignsWithAKeyringKey(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	resp := mustOK(t, ask(t, s, map[string]any{
		"op": "write", "address": "home/journal/today", "verb": "note", "message": "hello",
	}), "write")
	if resp["state"] != "accepted" {
		t.Fatalf("write returned %v", resp)
	}
	id, err := frame.ParseID(resp["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if f.led.State(id) != ledger.Accepted {
		t.Fatal("the written event is not accepted in the ledger")
	}
}

func TestReadOnlyAndNoSign(t *testing.T) {
	f := newFixture(t)

	ro := New(f.car, f.led, Options{ReadOnly: true, AllowSign: true})
	for _, op := range []string{"append", "write"} {
		if ok, _ := ask(t, ro, map[string]any{"op": op})["ok"].(bool); ok {
			t.Fatalf("read-only daemon accepted %s", op)
		}
	}
	mustOK(t, ask(t, ro, map[string]any{"op": "status"}), "status on a read-only daemon")

	// With signing off, write is gone but append still works.
	ns := New(f.car, f.led, Options{AllowSign: false})
	if ok, _ := ask(t, ns, map[string]any{"op": "write", "address": "a"})["ok"].(bool); ok {
		t.Fatal("write accepted while signing is disabled")
	}
	anchor := f.gen.ID
	e, err := event.Sign(event.Event{
		Carrier: &anchor, Parents: []frame.ID{f.gen.ID},
		Address: "a", Verb: "note",
	}, f.root)
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, ask(t, ns, map[string]any{
		"op": "append", "raw": hex.EncodeToString(e.Raw),
	}), "append with signing disabled")
}

// The guarantee that matters most: an idle daemon creates nothing.
//
// No timer, no watcher, no poller. Until someone asks, nothing is recorded.
// An always-awake service may run; it may not author. A daemon left alone
// writes nothing at all.
//
//	— T4, T4.1
func TestIdleDaemonWritesNothing(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})

	snapshot := func() []string {
		var out []string
		filepath.Walk(f.dir, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			out = append(out, p)
			return nil
		})
		return out
	}
	before := snapshot()
	beforeLen := f.led.Len()

	ln, err := Listen(filepath.Join(shortDir(t), "sock"))
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(ln)
	time.Sleep(200 * time.Millisecond) // let any hypothetical background work run
	ln.Close()

	after := snapshot()
	if len(after) != len(before) {
		t.Fatalf("an idle daemon changed the carrier: %d files before, %d after", len(before), len(after))
	}
	if f.led.Len() != beforeLen {
		t.Fatal("an idle daemon added events to the ledger")
	}
}

func TestSocketRoundTripAndPermissions(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	path := filepath.Join(shortDir(t), "rokh.sock")

	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go s.Serve(ln)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode is %v, want 0600", fi.Mode().Perm())
	}

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"op":"status"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatal(err)
	}
	mustOK(t, resp, "status over the socket")
	if resp["anchor"] != f.gen.ID.String() {
		t.Fatal("wrong anchor over the socket")
	}

	// A second daemon on the same live socket must refuse rather than
	// silently take it over.
	if _, err := Listen(path); err == nil {
		t.Fatal("a second Listen on a live socket succeeded")
	}
}

// An over-long path must fail with a message that says why.
func TestListenRejectsOverlongPath(t *testing.T) {
	long := "/tmp/" + strings.Repeat("x", 120) + ".sock"
	_, err := Listen(long)
	if err == nil {
		t.Fatal("accepted an over-long socket path")
	}
	if !strings.Contains(err.Error(), "shorter path") {
		t.Fatalf("unhelpful error for an over-long path: %v", err)
	}
}

// Listen must not delete a regular file that happens to sit at that path.
func TestListenRefusesNonSocket(t *testing.T) {
	path := filepath.Join(shortDir(t), "not-a-socket")
	if err := os.WriteFile(path, []byte("important"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path); err == nil {
		t.Fatal("Listen accepted a regular file path")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "important" {
		t.Fatal("Listen destroyed a regular file")
	}
}

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
	return os.MkdirTemp(base, prefix)
}
