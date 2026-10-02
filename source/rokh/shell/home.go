// The same sentences, spoken to a home's gate.
//
// A Rokh home is a layer above the ledger, and a program never holds it open:
// the gate does. What a program is handed is one connection, already carrying
// that program's identity, on a descriptor the gate named in ROKH_GATE_FD. So
// this file is not a second surface. It is the same closed set of sentences
// with a second back end behind it, and the difference between the two is only
// who answers.
//
// Three rules shape everything here.
//
// The first is that identity is the gate's. This surface never sends a name, a
// process number or a credential of its own: the connection it inherited is
// already somebody, and "hello" only asks the gate who that is and what it may
// do. The answer is printed, because a person typing sentences into a program
// that is not them should be able to read whose hands they are in.
//
// The second is that a sentence the gate does not speak is refused, by name,
// and nothing else is tried. There is no carrier here, no passphrase, no root
// key and no vault folder — and there must be no path by which a refused
// sentence quietly finds one. "entrust writing" is the owner's word over the
// owner's channel; a program that answered it by opening a carrier of its own
// would be answering a question nobody asked.
//
// The third is that the ledger core stays independent of the home, and that
// this surface still opens nothing. Nothing here imports rokh-home: the wire is
// NDJSON, one JSON object per line, and the standard library carries it. And
// nothing here imports a transport either — the shell has no listener, no
// dialler and no endpoint to name. What it has is a descriptor that was already
// connected when this program started, which it reads and writes as the stream
// it is. The architecture's rule that the sentence surface holds no socket of
// its own is kept as written: it cannot make one, and it cannot choose who is
// at the other end of the one it was handed.
//
//	— T1.4, T4.1, T8.2
package shell

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// GateFD is the variable the gate sets on a program it starts: the number of
// the descriptor the program's connection was handed on.
const GateFD = "ROKH_GATE_FD"

// gateProtocol is the protocol this surface knows: the contract's one
// protocol of both booths, rokh.booth/1 (T2). The gate says its own in the
// answer to hello, and a different one is said aloud rather than assumed
// compatible.
const gateProtocol = "rokh.booth/1"

// maxGateLine bounds one answer, as the gate bounds one request.
const maxGateLine = 8 << 20

// readChunk is how much of a file is asked for at a time. The gate hands back
// at most a megabyte per answer; asking in smaller pieces keeps one sentence's
// memory bounded and lets the reply say exactly how much of the whole arrived.
const readChunk = 64 << 10

// showBytes is how much of a file's text is put on the screen. Beyond it the
// reply says the size and the hash instead of scrolling a home past somebody.
const showBytes = 4096

// ---------- the wire ----------

// gateConn is one connection to a gate: JSON objects, one per line, in both
// directions, over a stream this program did not open. It is deliberately not a
// client for the gate's whole protocol — it is a line and a JSON object, and
// every op this surface speaks is written out where the sentence that needs it
// is.
type gateConn struct {
	rw     io.ReadWriteCloser
	r      *bufio.Reader
	broken error
	seq    int
}

func newGateConn(rw io.ReadWriteCloser) *gateConn {
	return &gateConn{rw: rw, r: bufio.NewReaderSize(rw, 64<<10)}
}

func (g *gateConn) close() error { return g.rw.Close() }

func (g *gateConn) fail(err error) error {
	g.broken = err
	g.rw.Close()
	return err
}

// line reads one answer, refusing a line longer than the protocol allows
// rather than growing to hold it.
func (g *gateConn) line() ([]byte, error) {
	var out []byte
	for {
		chunk, err := g.r.ReadSlice('\n')
		out = append(out, chunk...)
		if len(out) > maxGateLine {
			return nil, errors.New("the gate's answer is over the protocol's limit")
		}
		switch {
		case err == nil:
			return bytes.TrimRight(out, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(out) > 0:
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}

// call sends one request and reads its answer. A refusal is an answer, not a
// failure: it comes back as a gateRefusal, which the surface prints and goes
// on from. Only a broken connection is an error of the other kind.
func (g *gateConn) call(op string, req map[string]any) (map[string]any, error) {
	if g.broken != nil {
		return nil, g.broken
	}
	if req == nil {
		req = map[string]any{}
	}
	// One rokh.booth/1 message: the protocol, an id of its own, the op. The
	// id of the thing a request is about (a draft) travels as "ref" (B2).
	if ref, ok := req["id"]; ok {
		req["ref"] = ref
	}
	g.seq++
	id := strconv.Itoa(g.seq)
	req["v"], req["id"], req["op"] = gateProtocol, id, op
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := g.rw.Write(append(b, '\n')); err != nil {
		return nil, g.fail(fmt.Errorf("the gate did not take the request: %w", err))
	}
	raw, err := g.line()
	if err != nil {
		return nil, g.fail(fmt.Errorf("the gate gave no answer: %w", err))
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, g.fail(fmt.Errorf("the gate's answer is not a JSON object: %w", err))
	}
	if got, _ := resp["id"].(string); got != id {
		return nil, g.fail(fmt.Errorf("the gate's answer names request %v, not %s", resp["id"], id))
	}
	ok, valid := resp["ok"].(bool)
	if !valid {
		return nil, g.fail(errors.New("the gate's answer has no boolean outcome"))
	}
	if !ok {
		return resp, refusalOf(op, resp)
	}
	return resp, nil
}

// gateRefusal is the gate saying no. It keeps the gate's own code and reason
// so the surface can say which no it was: a right that was never given reads
// differently from one that was withdrawn, and a person deserves to be told
// which.
type gateRefusal struct {
	Op     string
	Code   string
	Reason string
	Msg    string
}

func (r *gateRefusal) Error() string {
	what := r.Code
	if r.Reason != "" && r.Reason != r.Code {
		what += " (" + r.Reason + ")"
	}
	if r.Msg == "" {
		return fmt.Sprintf("the gate refused %s: %s", r.Op, what)
	}
	return fmt.Sprintf("the gate refused %s — %s: %s", r.Op, what, r.Msg)
}

func refusalOf(op string, resp map[string]any) error {
	r := &gateRefusal{Op: op, Code: text(resp, "code"), Reason: text(resp, "reason"), Msg: text(resp, "error")}
	if r.Code == "" {
		r.Code = "refused"
	}
	return r
}

func text(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func whole(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func obj(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func list(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

// ---------- the session ----------

// homeGrant is one right this program holds, as the gate stated it.
type homeGrant struct {
	Action string
	Scope  string
}

// homeDraft is the sentence waiting to be closed. It lives in the home, not
// here: what is kept here is only its name and the revision last seen, so the
// closing sentence can name the same draft and the gate can refuse a stale one.
type homeDraft struct {
	id       string
	path     string
	base     int
	revision int
	sha      string
	size     int64
	line     string
}

// homeSession is a session on a home, in place of a session on a vault.
type homeSession struct {
	g        *gateConn
	consumer string
	name     string
	protocol string
	version  int
	grants   []homeGrant
	draft    *homeDraft
	notes    io.Writer
	out      io.Writer
}

// greetGate asks the gate who this program is. On a connection the gate handed
// out, no credential is sent and none would help: the connection is already
// bound to one program, and a credential belonging to another ends it.
func greetGate(g *gateConn, notes, out io.Writer) (*homeSession, error) {
	// Bound as every rokh.booth/1 session is: hello, then prove, with the
	// empty credential that names the program this connection was handed to;
	// then the gate says who that is and what it holds.
	hello, err := g.call("hello", map[string]any{"versions": []string{gateProtocol}, "auth": "credential"})
	if err != nil {
		return nil, err
	}
	if _, err := g.call("prove", map[string]any{"credential": ""}); err != nil {
		return nil, err
	}
	resp, err := g.call("whoami", nil)
	if err != nil {
		return nil, err
	}
	if p := text(hello, "protocol"); p != "" {
		resp["protocol"] = p
	}
	hs := &homeSession{g: g, consumer: text(resp, "consumer"), name: text(resp, "name"),
		protocol: text(resp, "protocol"), version: whole(resp, "authority_version"),
		notes: notes, out: out}
	for _, raw := range list(resp, "grants") {
		if m, ok := raw.(map[string]any); ok {
			hs.grants = append(hs.grants, homeGrant{Action: text(m, "action"), Scope: text(m, "scope")})
		}
	}
	sort.Slice(hs.grants, func(i, j int) bool {
		if hs.grants[i].Action != hs.grants[j].Action {
			return hs.grants[i].Action < hs.grants[j].Action
		}
		return hs.grants[i].Scope < hs.grants[j].Scope
	})
	if hs.protocol != "" && hs.protocol != gateProtocol {
		fmt.Fprintf(notes, "(this gate speaks %s and I speak %s; what follows may not line up)\n",
			hs.protocol, gateProtocol)
	}
	return hs, nil
}

// standing is the first thing a person sees: whose hands they are in, and what
// those hands may do. A program that did not say this would be asking somebody
// to type into a boundary they cannot see.
func (hs *homeSession) standing() string {
	var b strings.Builder
	name := hs.name
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Fprintf(&b, "a home, through its gate. I am %s — %s, authority %d.\n",
		escapePayload([]byte(name)), hs.consumer, hs.version)
	if len(hs.grants) == 0 {
		b.WriteString("I hold nothing here: no right was given to this program.")
		return b.String()
	}
	for _, g := range hs.grants {
		fmt.Fprintf(&b, "— may %s at %s\n", g.Action, escapePayload([]byte(g.Scope)))
	}
	b.WriteString("that is the whole of it; anything else is refused at the gate, not here.")
	return b.String()
}

// ---------- sentences ----------

// Two sentences exist only on this surface, because the home has two acts the
// carrier surface has no word for: a search across everything a program may
// read, and the abandoning of a draft that was never closed. They are added
// here rather than in the shared grammar so that the sentences a carrier knows
// stay exactly the sentences a carrier knows.
const (
	opFind   = "find"   // find {text}
	opCancel = "cancel" // cancel
)

// parseHome recognizes the home's two sentences and hands everything else to
// the one parser both surfaces share.
func parseHome(line string) (command, error) {
	raw := strings.Trim(strings.TrimRight(line, "\r\n"), " \t")
	ts := words(raw)
	switch {
	case len(ts) == 1 && is(ts[0], "cancel"):
		return command{Op: opCancel, Sentence: line}, nil
	case len(ts) >= 2 && is(ts[0], "find"):
		return command{Op: opFind, Text: strings.TrimRight(raw[ts[1].beg:], " \t"), Sentence: line}, nil
	}
	return parse(line)
}

// execute runs one parsed sentence against the gate.
func (hs *homeSession) execute(c command) (string, error) {
	switch c.Op {
	case opRead:
		return hs.doRead(c)
	case opFind:
		return hs.doFind(c)
	case opSeeLedgers:
		return hs.doList()
	case opSeeLedger:
		return hs.doStanding()
	case opSeeGrants:
		return hs.doGrants()
	case opWrite:
		return hs.doWrite(c)
	case opWriteClose:
		return hs.doWriteClose()
	case opCancel:
		return hs.doCancel()
	case opClose:
		return hs.doClose()
	}
	return "", hs.refuse(c)
}

// whyNot says, for each sentence this surface will not carry out, what would
// have to happen instead and who would have to do it. A refusal that does not
// say that is only a wall.
var whyNot = map[string]string{
	opOpen:         "a program is handed one home and does not choose another; the owner opens a home, and the gate hands out the connection",
	opOpenNew:      "making a home is the owner's act at the owner's channel, with the owner's passphrase",
	opImport:       "bringing a file into a home is the owner's import, which names the source and runs it",
	opReunite:      "berths and mirrors are the carrier surface's; a home has no seats folder",
	opReconcile:    "a home's versions do not fork into two heads, so there is nothing here to reconcile",
	opEntrustWrite: "only the owner grants, over the owner's channel; a program cannot widen its own authority",
	opEntrustRead:  "only the owner opens a home to somebody, over the owner's channel",
	opTakeBack:     "only the owner takes a right back, over the owner's channel",
	opCarryLedger:  "a home is not carried out whole; a version is handed out with export, into this program's own handover folder",
	opCarryAddress: "handing bytes out is export, into this program's own handover folder, and this surface does not do it",
	opCarryBundle:  "a bundle is the carrier surface's; a home hands out with export",
}

// refuse is what a sentence outside this surface gets: its own name, the
// reason, and the plain statement that nothing was attempted. The last part is
// the point. A surface that fell back to a carrier, a passphrase or a root key
// when the gate said no would be a way around the gate, and the gate is the
// whole of the boundary.
func (hs *homeSession) refuse(c command) error {
	why := whyNot[c.Op]
	if why == "" {
		why = "the gate speaks no op for it"
	}
	return fmt.Errorf("a program speaking to a home's gate cannot do that — %s. "+
		"Nothing was attempted: this surface holds no carrier, no passphrase and no key, "+
		"and does not reach for one when the gate says no", why)
}

// homePath checks a path the way the home does, so a sentence that could never
// be a path is answered here instead of spending a round trip to be told the
// same thing less clearly.
func homePath(p string) (string, error) {
	switch {
	case p == "":
		return "", errors.New("name the path")
	case p == "/":
		return "", errors.New(`"/" is the whole home, not a thing in it; say "see the ledgers" to see what is in it`)
	case strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/"):
		return "", fmt.Errorf("%q: a home's path has no leading or trailing slash", p)
	case !utf8.ValidString(p) || strings.ContainsRune(p, 0):
		return "", errors.New("that is not a path")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%q: a home's path has no %q segment", p, seg)
		}
	}
	return p, nil
}

// doRead is the reading sentence: exactly what is at that path, and nothing
// inferred. It asks the gate what the item is, then asks for its bytes in
// pieces until as many have arrived as the gate said there were, and hashes
// what arrived. The reply states the version, who registered it, what the home
// says about its authorship, how many bytes came and the hash of those bytes —
// so that "I read it" is a checkable claim and not a manner of speaking.
func (hs *homeSession) doRead(c command) (string, error) {
	path, err := homePath(c.Address)
	if err != nil {
		return "", err
	}
	st, err := hs.g.call("stat", map[string]any{"path": path})
	if err != nil {
		return "", err
	}
	item := obj(st, "item")
	versions := list(item, "versions")
	if len(versions) == 0 {
		return "", fmt.Errorf("%s has no version", path)
	}
	latest := versions[len(versions)-1].(map[string]any)
	n := whole(latest, "n")
	kind := text(latest, "kind")
	size := int64(whole(latest, "size"))

	var b strings.Builder
	fmt.Fprintf(&b, "— %s · version %s of %s · %s · %s bytes\n",
		escapePayload([]byte(path)), digits(n), digits(len(versions)), kind, digits(int(size)))
	fmt.Fprintf(&b, "  registered by %s · the home says authorship is %s\n",
		escapePayload([]byte(orUnsaid(text(latest, "by")))), escapePayload([]byte(orUnsaid(text(latest, "authorship")))))

	if kind != "file" {
		fmt.Fprintf(&b, "  a %s is read one file at a time; this surface reads files.\n", kind)
		return b.String() + fmt.Sprintf("%s at %s.", countOf(len(versions), "version"), path), nil
	}

	body, total, err := hs.bytesOf(path, n)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	fmt.Fprintf(&b, "  %s of %s bytes read · sha256 %s\n",
		digits(len(body)), digits(int(total)), hex.EncodeToString(sum[:]))
	if int64(len(body)) != total {
		fmt.Fprintf(&b, "  the gate stopped short of the whole; what is above is the hash of what arrived, not of the file.\n")
	}
	switch {
	case !utf8.Valid(body):
		b.WriteString("  the bytes are not text; they are not put on a screen.\n")
	case len(body) > showBytes:
		fmt.Fprintf(&b, "  the first %s bytes:\n", digits(showBytes))
		b.WriteString(escapePayload(body[:showBytes]) + "\n")
	default:
		b.WriteString(escapePayload(body) + "\n")
	}
	return b.String() + fmt.Sprintf("read %s at version %s.", path, digits(n)), nil
}

// bytesOf asks for a version's bytes until the gate has handed over as many as
// it said there were, or hands over none — which is where it stops, because a
// loop that asks again for nothing is a loop that does not end.
func (hs *homeSession) bytesOf(path string, version int) ([]byte, int64, error) {
	var out []byte
	var total int64
	for {
		resp, err := hs.g.call("bytes", map[string]any{"path": path, "version": version,
			"offset": len(out), "length": readChunk})
		if err != nil {
			return nil, 0, err
		}
		total = int64(whole(resp, "total"))
		chunk, err := base64.StdEncoding.DecodeString(text(resp, "data"))
		if err != nil {
			return nil, total, fmt.Errorf("the gate's bytes are not base64: %w", err)
		}
		out = append(out, chunk...)
		if len(chunk) == 0 || int64(len(out)) >= total {
			return out, total, nil
		}
	}
}

func orUnsaid(s string) string {
	if s == "" {
		return "(not said)"
	}
	return s
}

// doFind is the searching sentence. It asks twice and says which answer is
// which: what matched, and then the passages themselves with the exact byte
// range each one is. Both come from the home and neither is this program's
// wording — a search that blurred that line would be the first step toward a
// machine's sentence wearing somebody's name.
func (hs *homeSession) doFind(c command) (string, error) {
	query := strings.TrimSpace(c.Text)
	if query == "" {
		return "", errors.New("say what to look for")
	}
	found, err := hs.g.call("search", map[string]any{"query": query, "limit": 8})
	if err != nil {
		return "", err
	}
	hits := list(found, "hits")
	var b strings.Builder
	for _, raw := range hits {
		h, _ := raw.(map[string]any)
		if h == nil {
			continue
		}
		where := text(h, "path")
		if sub := text(h, "sub"); sub != "" {
			where += "/" + sub
		}
		fmt.Fprintf(&b, "— %s · version %s", escapePayload([]byte(where)), digits(whole(h, "version")))
		if a := text(h, "authorship"); a != "" {
			fmt.Fprintf(&b, " · authorship %s", escapePayload([]byte(a)))
		}
		b.WriteString("\n")
		if s := text(h, "snippet"); s != "" {
			fmt.Fprintf(&b, "   %s\n", escapePayload([]byte(s)))
		}
	}
	gathered, err := hs.g.call("context", map[string]any{"query": query, "max": 2048})
	if err != nil {
		return "", err
	}
	passages := list(gathered, "passages")
	for _, raw := range passages {
		p, _ := raw.(map[string]any)
		if p == nil {
			continue
		}
		at := obj(p, "at")
		fmt.Fprintf(&b, "· the home's own bytes at %s, %s–%s:\n%s\n",
			escapePayload([]byte(text(p, "path"))), digits(whole(at, "start")), digits(whole(at, "end")),
			escapePayload([]byte(text(p, "text"))))
	}
	if len(hits) == 0 && len(passages) == 0 {
		return fmt.Sprintf("nothing I may look at matches %q.", query), nil
	}
	return b.String() + fmt.Sprintf("%s, %s.", countOf(len(hits), "match"), countOf(len(passages), "passage")), nil
}

// doList is what a program may see of a home: every item under the whole home
// that its own rights reach. What it may not read is not listed and not
// counted, and this surface adds nothing to that.
func (hs *homeSession) doList() (string, error) {
	resp, err := hs.g.call("list", map[string]any{"scope": "/"})
	if err != nil {
		return "", err
	}
	items := list(resp, "items")
	if len(items) == 0 {
		return "there is nothing here I may see.", nil
	}
	var b strings.Builder
	for _, raw := range items {
		it, _ := raw.(map[string]any)
		if it == nil {
			continue
		}
		versions := list(it, "versions")
		line := fmt.Sprintf("— %s · %s", escapePayload([]byte(text(it, "path"))), countOf(len(versions), "version"))
		if len(versions) > 0 {
			v, _ := versions[len(versions)-1].(map[string]any)
			line += fmt.Sprintf(" · %s · %s bytes · authorship %s",
				text(v, "kind"), digits(whole(v, "size")), orUnsaid(text(v, "authorship")))
		}
		b.WriteString(line + "\n")
	}
	return b.String() + fmt.Sprintf("%s I may see.", countOf(len(items), "item")), nil
}

// doStanding re-reads identity from the gate rather than repeating what was
// said at hello: authority can move under a running program, and the honest
// answer to "where am I" is the one the gate gives now.
func (hs *homeSession) doStanding() (string, error) {
	resp, err := hs.g.call("whoami", nil)
	if err != nil {
		return "", err
	}
	hs.consumer, hs.name = text(resp, "consumer"), text(resp, "name")
	hs.version = whole(resp, "authority_version")
	hs.grants = hs.grants[:0]
	for _, raw := range list(resp, "grants") {
		if m, ok := raw.(map[string]any); ok {
			hs.grants = append(hs.grants, homeGrant{Action: text(m, "action"), Scope: text(m, "scope")})
		}
	}
	sort.Slice(hs.grants, func(i, j int) bool {
		if hs.grants[i].Action != hs.grants[j].Action {
			return hs.grants[i].Action < hs.grants[j].Action
		}
		return hs.grants[i].Scope < hs.grants[j].Scope
	})
	if v := whole(resp, "session_version"); v != hs.version {
		fmt.Fprintf(hs.notes, "(this session was opened under authority %d and the gate is now at %d)\n", v, hs.version)
	}
	return hs.standing(), nil
}

func (hs *homeSession) doGrants() (string, error) {
	if _, err := hs.doStanding(); err != nil {
		return "", err
	}
	if len(hs.grants) == 0 {
		return "nothing is entrusted to this program.", nil
	}
	var b strings.Builder
	for _, g := range hs.grants {
		fmt.Fprintf(&b, "— %s at %s\n", g.Action, escapePayload([]byte(g.Scope)))
	}
	return b.String() + fmt.Sprintf("%s, and no other.", countOf(len(hs.grants), "right")), nil
}

// doWrite is the drafting sentence, and it records nothing. The draft is
// opened in the home, its bytes are put there, and what comes back is the form
// of the effect: the path, the version it would follow, the size and the hash.
// The closing sentence is a second sentence, as it is on a carrier, because
// there is no way back from a recorded version either.
func (hs *homeSession) doWrite(c command) (string, error) {
	path, err := homePath(c.Address)
	if err != nil {
		return "", err
	}
	if hs.draft != nil {
		fmt.Fprintf(hs.notes, "(the sentence waiting at %s is set aside, unrecorded, as draft %s)\n",
			hs.draft.path, hs.draft.id)
		hs.draft = nil
	}
	opened, err := hs.g.call("draft.open", map[string]any{"path": path})
	if err != nil {
		return "", err
	}
	view := obj(opened, "draft")
	d := &homeDraft{id: text(view, "id"), path: text(view, "path"), base: whole(view, "base"),
		revision: whole(view, "revision"), line: c.Sentence}
	written, err := hs.g.call("draft.write", map[string]any{"id": d.id,
		"data": base64.StdEncoding.EncodeToString([]byte(c.Text)), "revision": d.revision})
	if err != nil {
		// The draft is open in the home and holds whatever it held before. Say
		// so rather than leaving a name nobody was told.
		fmt.Fprintf(hs.notes, "(draft %s is open at %s; the new bytes were not confirmed — inspect that draft)\n", d.id, d.path)
		return "", err
	}
	view = obj(written, "draft")
	d.revision, d.sha, d.size = whole(view, "revision"), text(view, "sha256"), int64(whole(view, "size"))
	hs.draft = d
	base := "nothing"
	if d.base > 0 {
		base = "version " + digits(d.base)
	}
	return fmt.Sprintf("at %s, %s bytes over %s — draft %s, revision %s, sha256 %s. "+
		"Nothing is recorded yet. \"write\" closes it, \"cancel\" leaves it.",
		escapePayload([]byte(d.path)), digits(int(d.size)), base, d.id, digits(d.revision), d.sha), nil
}

// doWriteClose closes the waiting sentence: one recorded version, signed by
// this program's own key, with this program named as its registrar. The reply
// says that plainly. A program is not a person, and the home does not pretend
// otherwise: authorship stays whatever the home says it is until the owner
// declares it, and a registrar's signature is never that declaration.
func (hs *homeSession) doWriteClose() (string, error) {
	if hs.draft == nil {
		return "", errors.New("no sentence is waiting to be closed")
	}
	d := hs.draft
	resp, err := hs.g.call("draft.record", map[string]any{"id": d.id})
	if err != nil {
		// The draft is still waiting; a failed crossing is not a discarded one.
		var refused *gateRefusal
		if !errors.As(err, &refused) {
			return "", &gateRefusal{Op: "draft.record", Code: "record_unknown",
				Msg: fmt.Sprintf("the ending of draft %s is unknown: %v", d.id, err)}
		}
		return "", err
	}
	res := obj(resp, "result")
	if text(res, "record") != "recorded" || text(res, "event") == "" ||
		whole(res, "version") < 1 || text(res, "path") != d.path {
		code := "record_unknown"
		if text(res, "record") == "not-recorded" {
			code = "record_not_recorded"
		}
		return "", &gateRefusal{Op: "draft.record", Code: code,
			Msg: fmt.Sprintf("draft %s has no confirmed recorded version; it remains available for inspection", d.id)}
	}
	hs.draft = nil
	name := hs.name
	if name == "" {
		name = hs.consumer
	}
	return fmt.Sprintf("written. %s at %s is version %s, event %s — registered by %s, "+
		"which is this program and not a person. The home's word on authorship is the owner's to give.",
		text(res, "record"), escapePayload([]byte(text(res, "path"))), digits(whole(res, "version")),
		text(res, "event"), escapePayload([]byte(name))), nil
}

// doCancel abandons the waiting sentence. The draft's bytes stay in the home,
// because a program may open and write a draft and has no op that unmakes one;
// what the reply must not do is call that "gone". It says where it was left and
// that no version was recorded, and it checks with the gate before saying so.
func (hs *homeSession) doCancel() (string, error) {
	if hs.draft == nil {
		return "", errors.New("no sentence is waiting")
	}
	d := hs.draft
	state := "open"
	if resp, err := hs.g.call("draft.read", map[string]any{"id": d.id}); err == nil {
		state = text(obj(resp, "draft"), "state")
		if obj(resp, "draft")["recorded"] == true {
			hs.draft = nil
			return "", fmt.Errorf("draft %s at %s was already recorded; there is nothing to cancel", d.id, d.path)
		}
	} else {
		fmt.Fprintf(hs.notes, "(the gate would not say what became of draft %s: %v)\n", d.id, err)
	}
	hs.draft = nil
	return fmt.Sprintf("let go. Nothing was recorded at %s; draft %s stays in the home, %s, "+
		"holding %s bytes nobody signed.", escapePayload([]byte(d.path)), d.id, state, digits(int(d.size))), nil
}

// doClose ends the conversation. A draft left waiting is named, because it was
// never recorded and the person should not learn that from its absence.
func (hs *homeSession) doClose() (string, error) {
	if hs.draft != nil {
		fmt.Fprintf(hs.notes, "(draft %s at %s was left unrecorded)\n", hs.draft.id, hs.draft.path)
		hs.draft = nil
	}
	return tplHomeClosed, nil
}

// ---------- the surface ----------

// runHome is the line surface over a gate: sentences in, one answer each. It
// is the same loop the vault surface runs, and deliberately so.
func homeErrorCode(err error) int {
	var refused *gateRefusal
	if errors.As(err, &refused) && (refused.Code == "record_unknown" || refused.Code == "durability_unknown") {
		return 4
	}
	return 1
}

func runHome(stream io.ReadWriteCloser, oneShot string, in io.Reader, out, notes io.Writer) int {
	g := newGateConn(stream)
	defer g.close()
	hs, err := greetGate(g, notes, out)
	if err != nil {
		fmt.Fprintln(notes, "no —", err)
		return 1
	}
	// A person who arrives through a gate arrives at the same program as a
	// person who opens a vault, and until now could not tell: no greeting, no
	// prompt, and an exit that said nothing. The loop below is the vault's
	// loop, so its surface is the vault's surface too. Only a person at a
	// terminal is shown any of it — a driver's bytes are what they were.
	person := oneShot == "" && personThere(in, notes)
	if person {
		greetHome(notes)
	}
	fmt.Fprintln(out, hs.standing())

	if oneShot != "" {
		if err := hs.runLine(oneShot); err != nil {
			fmt.Fprintln(notes, "no —", err)
			return homeErrorCode(err)
		}
		if hs.draft != nil {
			fmt.Fprintln(notes, "the sentence was left waiting to be closed, and ended with this command.")
			fmt.Fprintf(notes, "draft %s at %s holds it; nothing was recorded.\n", hs.draft.id, hs.draft.path)
			return 1
		}
		return 0
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	code := 0
	for {
		if person {
			fmt.Fprint(notes, "? ")
		}
		if !sc.Scan() {
			break
		}
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if asking(line) {
			showHomeSentences(notes)
			continue
		}
		if err := hs.runLine(line); err != nil {
			fmt.Fprintln(notes, "no —", err)
			code = max(code, homeErrorCode(err))
			if g.broken != nil {
				break
			}
			continue
		}
		if c, perr := parseHome(line); perr == nil && c.Op == opClose {
			return code
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(notes, "no —", err)
		return max(code, 1)
	}
	if hs.draft != nil {
		fmt.Fprintf(notes, "(draft %s at %s remains available; inspect its recording status)\n", hs.draft.id, hs.draft.path)
	}
	// End of input closes like "go" and says so the same way — otherwise the
	// prompt just written is left hanging and the next thing on that line is
	// somebody else's shell. What became of a draft is the gate's word, not
	// this line's, so the note above is left exactly as it was. A gate that
	// broke under us ended nothing tidily and is not told otherwise.
	if person && g.broken == nil {
		fmt.Fprintln(out, tplHomeClosed)
	}
	return code
}

func (hs *homeSession) runLine(line string) error {
	c, err := parseHome(line)
	if err != nil {
		if errors.Is(err, errNotASentence) {
			return errUnknownSentence
		}
		return err
	}
	reply, err := hs.execute(c)
	if err != nil {
		return err
	}
	fmt.Fprintln(hs.out, reply)
	return nil
}

// homeFD is the descriptor to speak to a gate on: the one named on the command
// line, or the one the gate named in the environment, or none.
func homeFD(named int) (int, bool, error) {
	if named >= 0 {
		return named, true, nil
	}
	v := strings.TrimSpace(os.Getenv(GateFD))
	if v == "" {
		return 0, false, nil
	}
	fd, err := strconv.Atoi(v)
	if err != nil || fd < 0 {
		return 0, false, fmt.Errorf("%s is %q, which is not a descriptor", GateFD, v)
	}
	return fd, true, nil
}

func showHomeSentences(w io.Writer) {
	fmt.Fprint(w, `the sentences this surface speaks to a home's gate:

  read {path}                   what is at that path: version, registrar, bytes, hash
  find {text}                   what a search may reach, and the home's own passages
  see the ledgers               the items this program may see
  see the ledger                who this program is and what it holds
  see the grants                the rights the gate says it has
  write at {path}: {text}       draught a version; nothing is recorded
  write                         close it — one recorded version
  cancel                        let the draught go; nothing is recorded
  go                            end the conversation

Everything else in the carrier surface's grammar is refused here by name:
a program does not open a home, does not grant, does not take back and does
not carry. Those are the owner's, at the owner's channel.
`)
}
