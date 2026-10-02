package shell

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// duplex is two pipes read as one stream, standing in for the descriptor a
// gate hands a program. It is made of pipes rather than a socket on purpose:
// this surface may not import a transport, and neither may its tests.
type duplex struct {
	r *os.File
	w *os.File
}

func (d duplex) Read(p []byte) (int, error)  { return d.r.Read(p) }
func (d duplex) Write(p []byte) (int, error) { return d.w.Write(p) }
func (d duplex) Close() error {
	err := d.r.Close()
	if werr := d.w.Close(); err == nil {
		err = werr
	}
	return err
}

// pair returns the two ends of one conversation.
func pair(t *testing.T) (io.ReadWriteCloser, io.ReadWriteCloser) {
	t.Helper()
	ar, bw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	br, aw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return duplex{r: ar, w: aw}, duplex{r: br, w: bw}
}

// A gate that answers from a script and remembers every op it was asked for.
// What it remembers is as much the point as what it answers: a surface that
// refuses a sentence must refuse it without asking anybody anything, and the
// only way to hold it to that is to watch the wire.
type scriptedGate struct {
	t      *testing.T
	conn   io.ReadWriteCloser
	mu     sync.Mutex
	asked  []string
	sent   []map[string]any
	answer func(op string, req map[string]any) map[string]any
	done   chan struct{}
}

// ops is what the gate was asked for, taken under the lock: the gate answers
// on its own goroutine and the test reads from another.
func (g *scriptedGate) ops() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.asked...)
}

func fakeGate(t *testing.T, answer func(op string, req map[string]any) map[string]any) (*gateConn, *scriptedGate) {
	t.Helper()
	mine, theirs := pair(t)
	g := &scriptedGate{t: t, conn: theirs, answer: answer, done: make(chan struct{})}
	go g.serve()
	t.Cleanup(func() {
		mine.Close()
		<-g.done
		theirs.Close()
	})
	return newGateConn(mine), g
}

// serve speaks rokh.booth/1 as the gate does (T2): every answer names the
// request's id. The session's binding (hello, prove, and the whoami that
// greets) is the gate's own and is answered here, from the script's answer
// to "hello"; it is noted as the one "hello" the surface asked.
func (g *scriptedGate) serve() {
	defer close(g.done)
	dec := json.NewDecoder(g.conn)
	enc := json.NewEncoder(g.conn)
	bound := false
	for {
		var req map[string]any
		if err := dec.Decode(&req); err != nil {
			return
		}
		op, _ := req["op"].(string)
		var resp map[string]any
		switch {
		case !bound && op == "hello":
			resp = map[string]any{"ok": true, "protocol": gateProtocol}
		case !bound && op == "prove":
			resp = map[string]any{"ok": true}
		case !bound && op == "whoami":
			bound = true
			g.mu.Lock()
			g.asked = append(g.asked, "hello")
			g.sent = append(g.sent, req)
			g.mu.Unlock()
			resp = g.answer("hello", req)
		default:
			g.mu.Lock()
			g.asked = append(g.asked, op)
			g.sent = append(g.sent, req)
			g.mu.Unlock()
			resp = g.answer(op, req)
		}
		if resp == nil {
			resp = map[string]any{"ok": false, "code": "unknown_op", "error": "no script for " + op}
		}
		answer := map[string]any{}
		for k, v := range resp {
			answer[k] = v
		}
		answer["v"], answer["id"] = gateProtocol, req["id"]
		if err := enc.Encode(answer); err != nil {
			return
		}
	}
}

func hello() map[string]any {
	return map[string]any{"ok": true, "protocol": gateProtocol, "consumer": "c0ffee", "name": "a test program",
		"authority_version": float64(3),
		"grants": []any{
			map[string]any{"id": "g1", "action": "read", "scope": "journal"},
			map[string]any{"id": "g2", "action": "bytes", "scope": "journal"},
		}}
}

func openSession(t *testing.T, answer func(op string, req map[string]any) map[string]any) (*homeSession, *scriptedGate, *strings.Builder, *strings.Builder) {
	t.Helper()
	g, script := fakeGate(t, func(op string, req map[string]any) map[string]any {
		if op == "hello" {
			return hello()
		}
		return answer(op, req)
	})
	out, notes := &strings.Builder{}, &strings.Builder{}
	hs, err := greetGate(g, notes, out)
	if err != nil {
		t.Fatal(err)
	}
	return hs, script, out, notes
}

func say(t *testing.T, hs *homeSession, line string) (string, error) {
	t.Helper()
	c, err := parseHome(line)
	if err != nil {
		t.Fatalf("parse %q: %v", line, err)
	}
	return hs.execute(c)
}

// The first thing the surface says is whose hands the person is in. It is the
// gate's answer, not a claim of this program's own.
func TestTheGateSaysWhoTheProgramIsAndWhatItHolds(t *testing.T) {
	hs, _, _, _ := openSession(t, func(string, map[string]any) map[string]any { return nil })
	standing := hs.standing()
	for _, want := range []string{"a test program", "c0ffee", "authority 3", "may read at journal", "may bytes at journal"} {
		if !strings.Contains(standing, want) {
			t.Fatalf("standing does not say %q:\n%s", want, standing)
		}
	}
}

// A program with nothing granted says so, rather than offering a surface that
// looks like it can do something.
func TestAProgramWithNoRightSaysSo(t *testing.T) {
	g, _ := fakeGate(t, func(op string, req map[string]any) map[string]any {
		return map[string]any{"ok": true, "protocol": gateProtocol, "consumer": "bare", "name": "a bare program",
			"authority_version": float64(1), "grants": []any{}}
	})
	hs, err := greetGate(g, &strings.Builder{}, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hs.standing(), "no right was given") {
		t.Fatalf("standing: %s", hs.standing())
	}
}

// Reading asks for every byte the gate says is there, and the reply hashes what
// actually arrived rather than repeating a number the gate gave.
func TestReadingAsksUntilTheWholeFileHasArrived(t *testing.T) {
	body := strings.Repeat("a page of somebody's evening. ", 4000) // > one chunk
	hs, script, _, _ := openSession(t, func(op string, req map[string]any) map[string]any {
		switch op {
		case "stat":
			return map[string]any{"ok": true, "item": map[string]any{"path": "journal/tuesday",
				"versions": []any{map[string]any{"n": float64(1), "kind": "file", "size": float64(len(body)),
					"by": "owner", "authorship": "unknown"}}}}
		case "bytes":
			off := whole(req, "offset")
			end := off + whole(req, "length")
			if end > len(body) {
				end = len(body)
			}
			return map[string]any{"ok": true, "total": float64(len(body)),
				"data": base64.StdEncoding.EncodeToString([]byte(body[off:end]))}
		}
		return nil
	})
	reply, err := say(t, hs, "read journal/tuesday")
	if err != nil {
		t.Fatal(err)
	}
	chunks := 0
	for _, op := range script.ops() {
		if op == "bytes" {
			chunks++
		}
	}
	if want := (len(body) + readChunk - 1) / readChunk; chunks != want {
		t.Fatalf("asked for bytes %d times, wanted %d", chunks, want)
	}
	if !strings.Contains(reply, digits(len(body))+" of "+digits(len(body))+" bytes read") {
		t.Fatalf("reply does not say how much arrived:\n%s", reply)
	}
}

// A gate that hands back fewer bytes than it said there were is not smoothed
// over: the reply says the hash is of what arrived and not of the file.
func TestAShortReadIsSaidToBeShort(t *testing.T) {
	hs, _, _, _ := openSession(t, func(op string, req map[string]any) map[string]any {
		switch op {
		case "stat":
			return map[string]any{"ok": true, "item": map[string]any{"path": "journal/x",
				"versions": []any{map[string]any{"n": float64(1), "kind": "file", "size": float64(100)}}}}
		case "bytes":
			if whole(req, "offset") > 0 {
				return map[string]any{"ok": true, "total": float64(100), "data": ""}
			}
			return map[string]any{"ok": true, "total": float64(100),
				"data": base64.StdEncoding.EncodeToString([]byte("only ten."))}
		}
		return nil
	})
	reply, err := say(t, hs, "read journal/x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "stopped short of the whole") {
		t.Fatalf("a short read was not said to be short:\n%s", reply)
	}
}

// The heart of it: a sentence this surface does not carry to a home is refused
// with nothing asked of anybody. No carrier is opened, no passphrase is sought,
// no other op is tried in its place — the wire stays silent after hello.
func TestARefusedSentenceAsksTheGateNothing(t *testing.T) {
	sentences := []string{
		"open the ledger other",
		"open a new ledger named other",
		"bring /etc/hosts to journal",
		"bring the returned ledger",
		"entrust writing at journal to somebody",
		"entrust reading journal to somebody",
		"take back the grant 0123",
		"carry the ledger to /tmp//x",
		"carry journal to /tmp/x",
		"carry the bundle for somebody",
		"reconcile",
	}
	for _, line := range sentences {
		hs, script, out, _ := openSession(t, func(op string, req map[string]any) map[string]any {
			t.Errorf("%q reached the gate as %q", line, op)
			return nil
		})
		reply, err := say(t, hs, line)
		if err == nil {
			t.Fatalf("%q was answered with %q instead of refused", line, reply)
		}
		if !strings.Contains(err.Error(), "Nothing was attempted") {
			t.Fatalf("%q: refusal does not say nothing was attempted: %v", line, err)
		}
		if !strings.Contains(err.Error(), "no carrier, no passphrase and no key") {
			t.Fatalf("%q: refusal does not rule out a fallback: %v", line, err)
		}
		if len(script.ops()) != 1 || script.ops()[0] != "hello" {
			t.Fatalf("%q asked the gate %v", line, script.ops())
		}
		if out.String() != "" {
			t.Fatalf("%q printed %q", line, out.String())
		}
	}
}

// A denial is the gate's, and the surface says so in the gate's own words. It
// does not translate a "no" into a second attempt at something else.
func TestADenialIsCarriedBackWholeAndNotWorkedAround(t *testing.T) {
	hs, script, _, _ := openSession(t, func(op string, req map[string]any) map[string]any {
		return map[string]any{"ok": false, "code": "denied", "reason": "no_grant",
			"error": `home: read is not allowed on "private/ledger"`}
	})
	if _, err := say(t, hs, "read private/ledger"); err == nil {
		t.Fatal("a denied read was not an error")
	} else if !strings.Contains(err.Error(), "no_grant") || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("the gate's own no did not come back: %v", err)
	}
	if len(script.ops()) != 2 || script.ops()[1] != "stat" {
		t.Fatalf("after a denial the surface asked %v", script.ops())
	}
}

// The path check happens here, so a sentence that could never be a home path
// costs no round trip and the person is told why in the surface's own words.
func TestAPathThatCouldNeverBeOneIsAnsweredHere(t *testing.T) {
	for _, line := range []string{"read /", "read /journal", "read journal/", "read a/../b"} {
		hs, script, _, _ := openSession(t, func(op string, req map[string]any) map[string]any {
			t.Errorf("%q reached the gate as %q", line, op)
			return nil
		})
		if _, err := say(t, hs, line); err == nil {
			t.Fatalf("%q was accepted", line)
		}
		if len(script.ops()) != 1 {
			t.Fatalf("%q asked %v", line, script.ops())
		}
	}
}

// Draughting is two sentences, as it is on a carrier, and the first one records
// nothing. "cancel" is the second half not happening: the wire never carries a
// draft.record, and the reply does not call the draft gone.
func TestCancellingADraughtRecordsNothing(t *testing.T) {
	hs, script, _, _ := openSession(t, draftGate("d1"))
	if _, err := say(t, hs, "write at journal/tuesday: the weather turned"); err != nil {
		t.Fatal(err)
	}
	if hs.draft == nil {
		t.Fatal("no draft is waiting")
	}
	reply, err := say(t, hs, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if hs.draft != nil {
		t.Fatal("a cancelled draft is still waiting")
	}
	for _, op := range script.ops() {
		if op == "draft.record" {
			t.Fatalf("cancel recorded: %v", script.ops())
		}
	}
	if !strings.Contains(reply, "Nothing was recorded") || !strings.Contains(reply, "stays in the home") {
		t.Fatalf("cancel does not say what became of the draught:\n%s", reply)
	}
}

// The bare verb is the only path to a recorded version, and the reply names the
// program as its registrar. A program is not a person and the reply says so.
func TestOnlyTheClosingSentenceRecords(t *testing.T) {
	hs, script, _, _ := openSession(t, draftGate("d2"))
	if _, err := say(t, hs, "write at journal/tuesday: the weather turned"); err != nil {
		t.Fatal(err)
	}
	for _, op := range script.ops() {
		if op == "draft.record" {
			t.Fatal("draughting recorded")
		}
	}
	reply, err := say(t, hs, "write")
	if err != nil {
		t.Fatal(err)
	}
	if script.ops()[len(script.ops())-1] != "draft.record" {
		t.Fatalf("closing did not record: %v", script.ops())
	}
	for _, want := range []string{"version 4", "registered by a test program", "not a person", "authorship is the owner's"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("reply does not say %q:\n%s", want, reply)
		}
	}
	if hs.draft != nil {
		t.Fatal("a recorded draft is still waiting")
	}
}

// A refused recording leaves the sentence waiting. The crossing failed; the
// draught did not become a version and did not become nothing either.
func TestARefusedRecordingLeavesTheSentenceWaiting(t *testing.T) {
	hs, _, _, _ := openSession(t, func(op string, req map[string]any) map[string]any {
		if op == "draft.record" {
			return map[string]any{"ok": false, "code": "denied", "reason": "no_grant",
				"error": "home: record is not allowed on \"journal/tuesday\""}
		}
		return draftGate("d3")(op, req)
	})
	if _, err := say(t, hs, "write at journal/tuesday: the weather turned"); err != nil {
		t.Fatal(err)
	}
	if _, err := say(t, hs, "write"); err == nil {
		t.Fatal("a refused recording was not an error")
	}
	if hs.draft == nil {
		t.Fatal("the sentence was lost when its recording was refused")
	}
}

func draftGate(id string) func(op string, req map[string]any) map[string]any {
	revision := 1
	return func(op string, req map[string]any) map[string]any {
		switch op {
		case "draft.open":
			return map[string]any{"ok": true, "draft": map[string]any{"id": id, "path": text(req, "path"),
				"base": float64(3), "revision": float64(revision), "sha256": "", "size": float64(0), "state": "open"}}
		case "draft.write":
			if whole(req, "revision") != revision {
				return map[string]any{"ok": false, "code": "failed", "error": "stale revision"}
			}
			revision++
			body, _ := base64.StdEncoding.DecodeString(text(req, "data"))
			return map[string]any{"ok": true, "draft": map[string]any{"id": id, "path": "journal/tuesday",
				"base": float64(3), "revision": float64(revision), "sha256": "abc", "size": float64(len(body)),
				"state": "open"}}
		case "draft.read":
			return map[string]any{"ok": true, "draft": map[string]any{"id": id, "state": "open", "recorded": false},
				"data": ""}
		case "draft.record":
			return map[string]any{"ok": true, "result": map[string]any{"record": "recorded", "path": "journal/tuesday",
				"version": float64(4), "event": "e1e1", "item": "i1", "sheet": "s1"}}
		}
		return nil
	}
}
