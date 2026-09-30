package main

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"rokh/booth"
	"rokh/frame"
	"rokh/key"
	"rokh/transport"
)

// T2, steps 2 and 3, through real processes: `rokh daemon` is the booth of a
// carrier, and every session is answered from the view it may hold, opened
// with its own key's reader and nothing wider (contract B4, 3.3).

// holder is one key of the fixture as its holder has it: the id, the reader
// and the signer its passphrase opens from its own cell.
type holder struct {
	name     string
	passFile string
	kid      [32]byte
	reader   key.Reader
	signer   ed25519.PrivateKey // nil for a key that only reads
}

type boothsFixture struct {
	processFixture
	root ed25519.PrivateKey
}

func newBoothsFixture(t *testing.T) *boothsFixture {
	t.Helper()
	f := &boothsFixture{processFixture: newProcessFixture(t)}
	s, err := openSession(f.dir, "synthetic test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	f.root = ownerRoot(s.sec)
	s.Close()
	return f
}

// addKey adds a key through the command line and opens its own cell with
// its own passphrase, as the key's holder would.
func (f *boothsFixture) addKey(t *testing.T, name, reads, scope string) *holder {
	t.Helper()
	pass := "synthetic passphrase of " + name
	h := &holder{name: name, passFile: filepath.Join(t.TempDir(), name+".pass")}
	if err := os.WriteFile(h.passFile, []byte(pass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"key", "add", f.dir, "--name", name, "--key-passphrase-file", h.passFile}
	if reads != "" {
		args = append(args, "--reads", reads)
	}
	if scope != "" {
		args = append(args, "--write", "--scope", scope)
	}
	if out, code := f.run(args...); code != 0 {
		t.Fatalf("key add %s: exit %d\n%s", name, code, out)
	}
	c := openForTest(t, f.dir, pass)
	info := c.Vessel().Info()
	sec, _, err := key.Try(pass, c.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil {
		t.Fatal(err)
	}
	h.kid = sec.Key
	if h.reader, err = key.ReaderFrom(sec.Reader[:]); err != nil {
		t.Fatal(err)
	}
	h.signer = sec.Signer()
	return h
}

// write records a note as the owner, through the command line.
func (f *boothsFixture) write(t *testing.T, address, message string) frame.ID {
	t.Helper()
	out, code := f.run("write", f.dir, "--address", address, "--message", message, "--json")
	a := answerIn(out)
	if code != 0 || a["record"] != "recorded" {
		t.Fatalf("write %s: exit %d %s", address, code, out)
	}
	id, err := frame.ParseID(fmt.Sprint(a["id"]))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// runningBooth is a `rokh daemon` process listening on a socket.
type runningBooth struct {
	cmd              *exec.Cmd
	network, address string
}

var listeningLine = regexp.MustCompile(`rokh\.booth/1 on (unix|tcp):(\S+)`)

// startBooth starts `rokh daemon DIR --listen WHERE` with a passphrase file
// and waits for the line that says where it listens.
func startBooth(t *testing.T, bin, dir, passFile, where string) *runningBooth {
	t.Helper()
	cmd := exec.Command(bin, "daemon", dir, "--listen", where, "--passphrase-file", passFile)
	errs, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	b := &runningBooth{cmd: cmd}
	t.Cleanup(func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() })
	found := make(chan []string, 1)
	go func() {
		sc := bufio.NewScanner(errs)
		sent := false
		for sc.Scan() {
			if m := listeningLine.FindStringSubmatch(sc.Text()); m != nil && !sent {
				found <- m
				sent = true
			}
		}
		if !sent {
			close(found)
		}
	}()
	select {
	case m, ok := <-found:
		if !ok {
			t.Fatal("the daemon ended without listening")
		}
		b.network, b.address = m[1], m[2]
	case <-time.After(60 * time.Second):
		t.Fatal("the daemon never said where it listens")
	}
	return b
}

func (b *runningBooth) dial(t *testing.T) *booth.Client {
	t.Helper()
	c, err := transport.Dial(b.network, b.address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return booth.NewClient(c)
}

// stdioSession starts `rokh daemon DIR --stdio`, which serves one session on
// its standard input and output, and speaks to it.
func stdioSession(t *testing.T, bin, dir, passFile string) *booth.Client {
	t.Helper()
	cmd := exec.Command(bin, "daemon", dir, "--stdio", "--passphrase-file", passFile)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); cmd.Wait() })
	return booth.NewClient(struct {
		io.Reader
		io.Writer
	}{out, in})
}

// rowsOf asks log and gives its rows by id.
func rowsOf(t *testing.T, c *booth.Client, fields map[string]any) map[string]map[string]any {
	t.Helper()
	r, err := c.Call("log", fields)
	if err != nil || r["ok"] != true {
		t.Fatalf("log %v: %v %v", fields, r, err)
	}
	out := map[string]map[string]any{}
	list, _ := r["events"].([]any)
	for _, x := range list {
		m := x.(map[string]any)
		out[fmt.Sprint(m["id"])] = m
	}
	return out
}

// isWhole says whether a row shows its event whole, with the address given.
func isWhole(row map[string]any, address string) bool {
	return row != nil && row["address"] == address && row["head_only"] != true
}

// isHead says whether a row shows its event by its head alone.
func isHead(row map[string]any) bool {
	if row == nil || row["head_only"] != true {
		return false
	}
	for _, k := range []string{"address", "verb", "payload", "attest", "door", "key"} {
		if _, ok := row[k]; ok {
			return false
		}
	}
	return true
}

// allLineage says whether every row of an answer is judged lineage.
func allLineage(rows map[string]map[string]any) bool {
	for _, r := range rows {
		if j, ok := r["judged"]; ok && j != "lineage" {
			return false
		}
	}
	return true
}

// carries says whether any string in an answer holds text, as it is or
// decoded from base64 or hex.
func carries(ans any, text string) bool {
	b, _ := json.Marshal(ans)
	if strings.Contains(string(b), text) {
		return true
	}
	found := false
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case string:
			if d, err := base64.StdEncoding.DecodeString(x); err == nil && strings.Contains(string(d), text) {
				found = true
			}
			if d, err := hex.DecodeString(x); err == nil && strings.Contains(string(d), text) {
				found = true
			}
		}
	}
	walk(ans)
	return found
}

// Step 2: a booth opened with a key's passphrase serves that key's view and
// nothing else to every session, opened with that key's own reader, and it
// never holds the root: over a Unix socket, over TCP on 127.0.0.1, and over
// standard input and output.
func TestABoothOpenedWithAKeysPassphraseServesThatKeysViewToEverySession(t *testing.T) {
	t.Parallel()
	f := newBoothsFixture(t)
	k := f.addKey(t, "journal-reader", "journal", "")
	wide := f.addKey(t, "wide-reader", "journal,notes", "")
	ja := f.write(t, "journal/a", "a journal line, in both views")
	nb := f.write(t, "notes/b", "a notes line, in the wide view only")
	pc := f.write(t, "private/c", "a private line, in no key's view")

	sockDir, err := shortTemp(t, "rkb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	unixBooth := startBooth(t, f.bin, f.dir, k.passFile, "unix:"+filepath.Join(sockDir, "k.sock"))
	tcpBooth := startBooth(t, f.bin, f.dir, k.passFile, "tcp:127.0.0.1:0")
	transports := map[string]func() *booth.Client{
		"unix socket":      func() *booth.Client { return unixBooth.dial(t) },
		"tcp 127.0.0.1":    func() *booth.Client { return tcpBooth.dial(t) },
		"standard streams": func() *booth.Client { return stdioSession(t, f.bin, f.dir, k.passFile) },
	}
	for name, open := range transports {
		t.Run(name, func(t *testing.T) {
			// The key the booth was opened with.
			c := open()
			who, err := c.ProveReader(k.kid, k.reader)
			if err != nil || who["ok"] != true || who["owner"] == true || who["judged"] != "lineage" {
				t.Fatalf("the booth's own key did not bind: %v %v", who, err)
			}
			rows := rowsOf(t, c, nil)
			if !isWhole(rows[ja.String()], "journal/a") || !isHead(rows[nb.String()]) || !isHead(rows[pc.String()]) || !allLineage(rows) {
				t.Errorf("the booth's own key is not served its view: journal %v, notes %v, private %v", rows[ja.String()], rows[nb.String()], rows[pc.String()])
			}

			// The owner: served the key's view and nothing more, judged
			// lineage, and nothing is signed with the root, which this booth
			// does not hold.
			o := open()
			who, err = o.ProveKey([32]byte{}, f.root)
			if err != nil || who["ok"] != true || who["owner"] != true || who["judged"] != "lineage" {
				t.Fatalf("the owner did not bind as the owner of a restricted view: %v %v", who, err)
			}
			rows = rowsOf(t, o, nil)
			if !isWhole(rows[ja.String()], "journal/a") || !isHead(rows[nb.String()]) || !isHead(rows[pc.String()]) || !allLineage(rows) {
				t.Errorf("the owner on a key's booth was served more than the key's view: notes %v, private %v", rows[nb.String()], rows[pc.String()])
			}
			view, _ := o.Call("view", nil)
			if view["judged"] != "lineage" || carries(view, "notes") || carries(view, "private") {
				t.Errorf("the owner's view on a key's booth: %v", view)
			}
			w, _ := o.Call("write", map[string]any{"address": "journal/x", "verb": "note", "message": "signed by the root?"})
			if w["record"] == "recorded" {
				t.Errorf("a key's booth signed with the root: %v", w)
			}
			caps, _ := o.Call("capabilities", nil)
			signers, _ := caps["signers"].([]any)
			for _, s := range signers {
				if m := s.(map[string]any); m["name"] == "root" && m["held"] != false {
					t.Errorf("a key's booth says it holds the root: %v", m)
				}
			}

			// Another key, wider than the booth's: at most the booth's view.
			s := open()
			if who, err := s.ProveReader(wide.kid, wide.reader); err != nil || who["ok"] != true || who["reader_held"] != true {
				t.Fatalf("the wide key did not bind with its reader: %v %v", who, err)
			}
			rows = rowsOf(t, s, nil)
			if !isWhole(rows[ja.String()], "journal/a") || !isHead(rows[nb.String()]) || !isHead(rows[pc.String()]) || !allLineage(rows) {
				t.Errorf("a wider key on a key's booth was served beyond the booth's view: notes %v", rows[nb.String()])
			}
			if got := rowsOf(t, s, map[string]any{"address": "notes"}); len(got) != 0 {
				t.Errorf("a question for notes on a booth that does not read notes answered %d rows", len(got))
			}
			for _, answer := range []any{rows} {
				if carries(answer, "wide view only") || carries(answer, "no key's view") {
					t.Error("a body outside the booth's view reached a session")
				}
			}
		})
	}
}

// Step 3: a session bound to a limited key on a booth the owner opened is
// served only what that session's own reader opens: a body written before
// the key existed stays a head; a question for an address outside the view
// answers as one for an address that does not exist; a key that gives no
// reader is served heads only; and the reader one session gave is not kept
// for another.
func TestAKeysSessionOnTheOwnersBoothIsServedWhatItsOwnReaderOpens(t *testing.T) {
	t.Parallel()
	f := newBoothsFixture(t)
	early := f.write(t, "journal/early", "written before the reader key existed")
	k := f.addKey(t, "journal-reader", "journal", "")
	w := f.addKey(t, "journal-writer", "journal", "journal")
	late := f.write(t, "journal/late", "written after the reader key was added")
	priv := f.write(t, "private/x", "outside every key's view")

	sockDir, err := shortTemp(t, "rkc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	b := startBooth(t, f.bin, f.dir, f.passFile, "unix:"+filepath.Join(sockDir, "o.sock"))

	// The reader gives its reader: its own view.
	c := b.dial(t)
	who, err := c.ProveReader(k.kid, k.reader)
	if err != nil || who["ok"] != true || who["reader_held"] != true || who["judged"] != "lineage" {
		t.Fatalf("the reader did not bind with its reader: %v %v", who, err)
	}
	rows := rowsOf(t, c, nil)
	if !isWhole(rows[late.String()], "journal/late") {
		t.Errorf("the reader is not served what its own reader opens: %v", rows[late.String()])
	}
	if !isHead(rows[early.String()]) {
		t.Errorf("a body written before the key existed was served to it: %v", rows[early.String()])
	}
	if !isHead(rows[priv.String()]) || !allLineage(rows) {
		t.Errorf("outside the view: %v", rows[priv.String()])
	}
	got, _ := c.Call("get", map[string]any{"event": early.String()})
	if got["head_only"] != true || carries(got, "before the reader key existed") {
		t.Errorf("get of a record sealed before the key existed: %v", got)
	}
	count := func(fields map[string]any) int { return len(rowsOf(t, c, fields)) }
	nowhere := count(map[string]any{"address": "e3-no-such-place"})
	if n := count(map[string]any{"address": "private"}); n != nowhere {
		t.Errorf("a question for private answered %d rows, one for a place that does not exist %d", n, nowhere)
	}
	if n := count(map[string]any{"address": "journal"}); n != 1 {
		t.Errorf("a question for journal answered %d rows; the reader's own reader opens one there", n)
	}
	if n := count(map[string]any{"verb": "note"}); n != 1 {
		t.Errorf("a question by verb answered %d rows; the reader's own reader opens one note", n)
	}
	if n := count(map[string]any{"after": late.String(), "address": "private"}); n != 0 {
		t.Errorf("a question by cursor for private answered %d rows", n)
	}
	wt, _ := c.Call("wait", map[string]any{"after": priv.String(), "address": "private", "timeout": 1})
	if evs, _ := wt["events"].([]any); len(evs) != 0 || wt["timed_out"] != true {
		t.Errorf("a wait on private answered %v", wt)
	}
	for _, text := range []string{"before the reader key existed", "outside every key's view"} {
		if carries(rowsOf(t, c, nil), text) {
			t.Errorf("a body the reader's own reader does not open reached it: %q", text)
		}
	}

	// A key that proves by its signature and gives no reader: heads only.
	h := b.dial(t)
	who, err = h.ProveKey(w.kid, w.signer)
	if err != nil || who["ok"] != true || who["reader_held"] != false {
		t.Fatalf("the writer did not bind without its reader: %v %v", who, err)
	}
	rows = rowsOf(t, h, nil)
	for id, r := range rows {
		if !isHead(r) {
			t.Errorf("a session that gave no reader was served more than a head: %s %v", id[:12], r)
		}
	}
	if !allLineage(rows) {
		t.Error("a heads-only view was not judged lineage")
	}
	if n := len(rowsOf(t, h, map[string]any{"address": "journal"})); n != 0 {
		t.Errorf("a heads-only session's question for an address answered %d rows", n)
	}
	if v, _ := h.Call("view", nil); v["planets"] != nil && len(v["planets"].([]any)) != 0 {
		t.Errorf("a heads-only session was shown planets: %v", v["planets"])
	}

	// The same key, giving its reader: its own view.
	g := b.dial(t)
	if who, err := g.ProveKeyWithReader(w.kid, w.signer, w.reader); err != nil || who["reader_held"] != true {
		t.Fatalf("the writer did not bind with its reader: %v %v", who, err)
	}
	if rows := rowsOf(t, g, nil); !isWhole(rows[late.String()], "journal/late") || !isHead(rows[priv.String()]) {
		t.Errorf("the writer's own view: late %v, private %v", rows[late.String()], rows[priv.String()])
	}

	// The reader one session gave is the session's: a new session of the
	// reader that gives none is served heads only.
	n := b.dial(t)
	hello, err := n.Call("hello", map[string]any{"versions": []string{booth.Protocol}, "auth": "key", "key": hex.EncodeToString(k.kid[:])})
	if err != nil || hello["ok"] != true {
		t.Fatalf("hello: %v %v", hello, err)
	}
	sealed, _ := hex.DecodeString(fmt.Sprint(hello["sealed"]))
	ch, err := key.OpenFrom(k.reader, sealed, booth.ChallengeInfo)
	if err != nil {
		t.Fatal(err)
	}
	if who, err := n.Call("prove", map[string]any{"challenge": hex.EncodeToString(ch)}); err != nil || who["reader_held"] != false {
		t.Fatalf("the reader did not bind without its reader: %v %v", who, err)
	}
	if rows := rowsOf(t, n, nil); !isHead(rows[late.String()]) {
		t.Errorf("a session that gave no reader was served what another session's reader opened: %v", rows[late.String()])
	}
}

// Step 4: a person's booth. The owner makes a key for each of two persons and
// sets a booth for each (`rokh daemon` opened with that key's passphrase, on a
// socket of its own); each person comes by that path. At the same time, each
// sees their own view as it is and is refused outside it, and neither sees
// the other's; the owner revokes one, who is then served nothing more, on the
// session bound before and on any new one; the other is served as before.
func TestTwoPersonsOnTwoBoothsOfOneCarrierSeeTheirOwnViewsOnly(t *testing.T) {
	t.Parallel()
	f := newBoothsFixture(t)
	alice := f.addKey(t, "alice", "letters/alice", "")
	bob := f.addKey(t, "bob", "letters/bob", "letters/bob")
	toAlice := f.write(t, "letters/alice/1", "a letter only alice reads")
	toBob := f.write(t, "letters/bob/1", "a letter only bob reads")
	own := f.write(t, "diary/1", "the owner's own page")

	sockDir, err := shortTemp(t, "rkp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	boothA := startBooth(t, f.bin, f.dir, alice.passFile, "unix:"+filepath.Join(sockDir, "alice.sock"))
	boothB := startBooth(t, f.bin, f.dir, bob.passFile, "unix:"+filepath.Join(sockDir, "bob.sock"))

	a := boothA.dial(t)
	if who, err := a.ProveReader(alice.kid, alice.reader); err != nil || who["ok"] != true || who["name"] != "alice" {
		t.Fatalf("alice did not bind on her booth: %v %v", who, err)
	}
	b := boothB.dial(t)
	if who, err := b.ProveKeyWithReader(bob.kid, bob.signer, bob.reader); err != nil || who["ok"] != true || who["name"] != "bob" {
		t.Fatalf("bob did not bind on his booth: %v %v", who, err)
	}
	// At the same time: each their own letters whole, the other's and the
	// owner's pages as heads, and asking for the other's address answers
	// nothing.
	ra, rb := rowsOf(t, a, nil), rowsOf(t, b, nil)
	if !isWhole(ra[toAlice.String()], "letters/alice/1") || !isHead(ra[toBob.String()]) || !isHead(ra[own.String()]) {
		t.Errorf("alice's view: hers %v, bob's %v, owner's %v", ra[toAlice.String()], ra[toBob.String()], ra[own.String()])
	}
	if !isWhole(rb[toBob.String()], "letters/bob/1") || !isHead(rb[toAlice.String()]) || !isHead(rb[own.String()]) {
		t.Errorf("bob's view: his %v, alice's %v, owner's %v", rb[toBob.String()], rb[toAlice.String()], rb[own.String()])
	}
	if carries(ra, "only bob reads") || carries(rb, "only alice reads") || carries(ra, "owner's own page") || carries(rb, "owner's own page") {
		t.Error("a person was served a body of another's view")
	}
	if n := len(rowsOf(t, a, map[string]any{"address": "letters/bob"})); n != 0 {
		t.Errorf("alice asked for bob's letters and was answered %d rows", n)
	}
	if n := len(rowsOf(t, b, map[string]any{"address": "letters/alice"})); n != 0 {
		t.Errorf("bob asked for alice's letters and was answered %d rows", n)
	}
	// Refused outside it: an op that is the owner's, and a write outside
	// bob's grant.
	if kl, _ := a.Call("key.list", nil); kl["ok"] == true || kl["code"] != "owner_only" {
		t.Errorf("alice was answered the owner's key.list: %v", kl)
	}
	// One person on the other's booth: at most that booth's view, and bob's
	// reader opens nothing of alice's.
	x := boothA.dial(t)
	if who, err := x.ProveKeyWithReader(bob.kid, bob.signer, bob.reader); err != nil || who["ok"] != true {
		t.Fatalf("bob did not bind on alice's booth: %v %v", who, err)
	}
	rx := rowsOf(t, x, nil)
	if !isHead(rx[toAlice.String()]) || !isHead(rx[toBob.String()]) || carries(rx, "only alice reads") || carries(rx, "only bob reads") {
		t.Errorf("bob on alice's booth: alice's %v, his own %v", rx[toAlice.String()], rx[toBob.String()])
	}

	// The owner revokes alice: served nothing more, on the session bound before
	// and on a new one; bob is served as before.
	if out, code := f.run("key", "revoke", f.dir, "--key", "alice"); code != 0 {
		t.Fatalf("revoke alice: exit %d\n%s", code, out)
	}
	later := f.write(t, "letters/alice/2", "written after alice was revoked")
	for _, op := range []string{"log", "status", "view"} {
		r, _ := a.Call(op, nil)
		if r["ok"] == true || carries(r, "only alice reads") || carries(r, "after alice was revoked") {
			t.Errorf("revoked alice was served %s on her session: %v", op, r)
		}
	}
	n := boothA.dial(t)
	if who, _ := n.ProveReader(alice.kid, alice.reader); who != nil && who["ok"] == true {
		t.Errorf("revoked alice bound a new session: %v", who)
	}
	if rb := rowsOf(t, b, nil); !isWhole(rb[toBob.String()], "letters/bob/1") || !isHead(rb[later.String()]) {
		t.Errorf("bob after alice's revocation: his %v, the later letter %v", rb[toBob.String()], rb[later.String()])
	}
}
