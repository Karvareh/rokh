// Package booth is the one message protocol of Rokh's booths, rokh.booth/1
// (contract section 5). The daemon is the booth of a carrier and the gate is
// the booth of a home; both speak this, and so does every program and every
// engine.
//
// A message is one UTF-8 JSON object on one line, at most MaxLine bytes. The
// protocol needs an ordered byte stream and nothing else: this package reads
// and writes an io.ReadWriter and never opens a socket. Listeners and dialers
// are host adapters in package transport (B1).
//
// A session begins with hello and prove and is bound to one key before
// anything else is answered (B3). Nothing about the transport is an identity:
// no path, no port, no process id.
package booth

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"rokh/event"
	"rokh/frame"
	"rokh/key"
)

// Protocol is the name and version of the protocol.
const Protocol = "rokh.booth/1"

// MaxLine bounds one message (B1).
const MaxLine = 8 << 20

// Codes of B7, and the few the session itself needs.
const (
	CodeViewDenied        = "view_denied"
	CodeKeyRevoked        = "key_revoked"
	CodeVesselFull        = "vessel_full"
	CodeVesselCorrupt     = "vessel_corrupt"
	CodeTurnLost          = "turn_lost"
	CodeTurnUnavailable   = "turn_unavailable"
	CodeSeedRefused       = "seed_refused"
	CodeAnchorDiffers     = "anchor_differs"
	CodeAncestryUnproven  = "ancestry_unproven"
	CodeContentConflict   = "content_conflict"
	CodeContentIncomplete = "content_incomplete"

	CodeHelloFirst  = "hello_first"
	CodeBadRequest  = "bad_request"
	CodeUnknownKey  = "unknown_key"
	CodeProof       = "proof_failed"
	CodeVersion     = "version_unsupported"
	CodeUnknownOp   = "unknown_op"
	CodeOwnerOnly   = "owner_only"
	CodeNotRecorded = "not-recorded"
)

// ChallengeInfo is the seal info of a challenge sealed to a key's reader.
const ChallengeInfo = "rokh.booth/1/challenge"

// ReaderInfo is the seal info of a key's reader given to a booth: its private
// half sealed to the one-time public half the hello offered (seal_to), so it
// never crosses a transport in the clear (B4, T2).
const ReaderInfo = "rokh.booth/1/reader"

// Identity is what a session is bound to: one generation of one key, or a
// credential the booth itself issued (the gate's programs).
type Identity struct {
	Key    [32]byte
	Gen    uint32
	Name   string
	Owner  bool
	Signer ed25519.PublicKey // nil when the key cannot write
	Reader []byte            // X25519 public half; nil for a credential
	Reads  []string          // nil reads nothing; [""] reads everything
	// Credential names a booth-issued identity; empty for a key.
	Credential string
	// Program is the booth's own name for a credential's holder.
	Program string
	// Guest is a stranger without a key of this rokh (B5): it proved the root
	// of a genesis it presented, reads the read-open addresses, and appends
	// only under an open grant.
	Guest bool
	// Judged is how far what this session is served is judged, when the
	// booth says: "lineage" for a view restricted to a key's (3.3). Empty
	// means the owner's "full" for the owner and "lineage" for every other.
	Judged string
}

// Covers reports whether this identity reads an address.
func (id Identity) Covers(addr string) bool {
	if id.Owner {
		return true
	}
	for _, p := range id.Reads {
		if p == "" || addr == p || strings.HasPrefix(addr, p+"/") {
			return true
		}
	}
	return false
}

// Authority is what a booth knows of the rokh it serves: its anchor, the live
// keys of its keyring, and the credentials it issued.
type Authority interface {
	Anchor() frame.ID
	KeyByID(id [32]byte) (Identity, bool)
	Credential(token string) (Identity, bool)
}

// ReadOpener is an Authority that can list the read-open addresses a guest is
// served (B5). A booth whose authority cannot serves a guest nothing to read.
type ReadOpener interface {
	ReadOpen() []string
}

// Handler answers one request of a bound session. The answer's id and ok are
// set by the session.
type Handler interface {
	Serve(s *Session, req map[string]any) map[string]any
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(s *Session, req map[string]any) map[string]any

func (f HandlerFunc) Serve(s *Session, req map[string]any) map[string]any { return f(s, req) }

// Session is one bound conversation.
type Session struct {
	Who   Identity
	Bound bool
	// Values lets a booth keep its own state for the session.
	Values map[string]any

	auth      Authority
	challenge []byte
	pending   *Identity
	proof     proofKind // the proof the pending hello asked for
	rnd       io.Reader
	// sealTo is the one-time reader the pending hello offered a key to seal
	// its own reader to; reader is that key's reader, given at prove and
	// held for as long as the session stays bound to it (B4).
	sealTo *key.Reader
	reader *key.Reader
}

// Reader is the reader the bound key gave this session at prove, sealed to
// the booth's one-time half: the booth opens envelopes for the session with
// it (B4). A session that gave none has none.
func (s *Session) Reader() (key.Reader, bool) {
	if !s.Bound || s.reader == nil {
		return key.Reader{}, false
	}
	return *s.reader, true
}

// Unbind ends the session's binding and forgets the reader it gave: a key
// that is no longer live is served nothing more, and may say hello again
// only while it stands (B7).
func (s *Session) Unbind() { s.forget() }

// Ender is a Handler that is told when a session ends, so that it forgets
// whatever it kept for the session.
type Ender interface {
	End(s *Session)
}

// proofKind is the one proof a hello asks for; prove accepts that kind and
// no other (B3).
type proofKind byte

const (
	proofNone       proofKind = iota
	proofSignature            // a signature over the challenge: a key that signs, or a guest's root
	proofOpened               // the challenge sealed to a key's reader, opened
	proofCredential           // a credential string, empty only where the stream names its program
)

// ChallengeSize is the length of every challenge a hello issues.
const ChallengeSize = 32

// forget ends a hello that did not bind: nothing pending, nothing bound.
func (s *Session) forget() {
	s.Bound, s.pending, s.challenge, s.proof = false, nil, nil, proofNone
	s.sealTo, s.reader = nil, nil
}

// Refuse builds a failed answer.
func Refuse(code string, err error) map[string]any {
	return map[string]any{"ok": false, "code": code, "error": err.Error()}
}

// Serve runs one session over an ordered byte stream until it ends.
func Serve(rw io.ReadWriter, a Authority, h Handler, rnd io.Reader) error {
	if rnd == nil {
		rnd = rand.Reader
	}
	s := &Session{auth: a, rnd: rnd, Values: map[string]any{}}
	// When the stream ends the session ends: the booth forgets the reader it
	// was given, and a handler forgets what it kept for the session.
	defer func() {
		if e, ok := h.(Ender); ok {
			e.End(s)
		}
		s.forget()
		s.Values = map[string]any{}
	}()
	in := bufio.NewReaderSize(rw, 64<<10)
	w := bufio.NewWriter(rw)
	var mu sync.Mutex
	write := func(m map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		b, err := json.Marshal(m)
		if err != nil {
			b, _ = json.Marshal(Refuse(CodeBadRequest, err))
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
		return w.Flush()
	}
	for {
		line, err := readLine(in)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			_ = write(Refuse(CodeBadRequest, err))
			return err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil || req == nil {
			if err == nil {
				err = errors.New("a message is one JSON object")
			}
			if werr := write(Refuse(CodeBadRequest, err)); werr != nil {
				return werr
			}
			continue
		}
		ans := s.answer(req, h)
		if id, ok := req["id"]; ok {
			ans["id"] = id
		}
		ans["v"] = Protocol
		if err := write(ans); err != nil {
			return err
		}
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, err := r.ReadSlice('\n')
		out = append(out, chunk...)
		if len(out) > MaxLine {
			return nil, fmt.Errorf("a message is over %d bytes", MaxLine)
		}
		switch {
		case err == nil:
			return bytes.TrimRight(out, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(out) > 0:
			return bytes.TrimRight(out, "\r\n"), nil
		default:
			return nil, err
		}
	}
}

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

func (s *Session) answer(req map[string]any, h Handler) map[string]any {
	if v := str(req, "v"); v != "" && v != Protocol {
		return Refuse(CodeVersion, fmt.Errorf("this booth speaks %s, not %s", Protocol, v))
	}
	switch str(req, "op") {
	case "hello":
		return s.hello(req)
	case "prove":
		return s.prove(req)
	}
	if !s.Bound {
		return Refuse(CodeHelloFirst, errors.New("a session is bound to a key before anything else is answered: hello, then prove"))
	}
	if str(req, "op") == "whoami" {
		// The session says who it is; a booth may add what it alone knows,
		// such as a program's grants, but cannot change the identity. It
		// asks the booth first, which may find the key no longer live and
		// unbind the session (B7).
		extra := h.Serve(s, req)
		if !s.Bound {
			return extra
		}
		ans := s.whoami()
		if extra != nil && extra["ok"] == true {
			for k, v := range extra {
				if _, set := ans[k]; !set {
					ans[k] = v
				}
			}
		}
		return ans
	}
	ans := h.Serve(s, req)
	if ans == nil {
		ans = Refuse(CodeUnknownOp, fmt.Errorf("unknown op %q", str(req, "op")))
	}
	if _, ok := ans["ok"]; !ok {
		ans["ok"] = true
	}
	return ans
}

func (s *Session) hello(req map[string]any) map[string]any {
	if vs, ok := req["versions"].([]any); ok {
		found := false
		for _, v := range vs {
			if v == Protocol {
				found = true
			}
		}
		if !found {
			return Refuse(CodeVersion, fmt.Errorf("this booth speaks %s only", Protocol))
		}
	}
	s.forget()
	switch str(req, "auth") {
	case "key":
		raw, err := hex.DecodeString(str(req, "key"))
		if err != nil || len(raw) != 32 {
			return Refuse(CodeBadRequest, errors.New("hello names a key id: 64 hex digits"))
		}
		var kid [32]byte
		copy(kid[:], raw)
		who, ok := s.auth.KeyByID(kid)
		if !ok {
			return Refuse(CodeUnknownKey, errors.New("no live key of this rokh has that id"))
		}
		if who.Signer == nil && len(who.Reader) == 0 {
			// Neither half: nothing this key could show proves it.
			return Refuse(CodeProof, errors.New("this key has neither a signing nor a reading half, so it cannot prove itself"))
		}
		ch := make([]byte, ChallengeSize)
		if _, err := io.ReadFull(s.rnd, ch); err != nil {
			return Refuse(CodeBadRequest, err)
		}
		ans := map[string]any{"ok": true, "protocol": Protocol, "anchor": s.auth.Anchor().String()}
		if !who.Owner && len(who.Reader) > 0 {
			// A key may give its reader, so that the booth opens envelopes
			// for it with that reader only (B4): sealed to this one-time
			// half, which lives until prove.
			eph, err := key.NewReader(s.rnd)
			if err != nil {
				return Refuse(CodeBadRequest, err)
			}
			ans["seal_to"] = hex.EncodeToString(eph.Public())
			s.sealTo = &eph
		}
		if who.Signer == nil {
			// A key that only reads proves itself by opening the challenge
			// sealed to its reader; the challenge is never sent in the clear,
			// or repeating it would be the whole proof (R1).
			sealed, err := key.SealTo(who.Reader, ch, ChallengeInfo, s.rnd)
			if err != nil {
				return Refuse(CodeBadRequest, err)
			}
			ans["sealed"] = hex.EncodeToString(sealed)
			s.proof = proofOpened
		} else {
			// A key that signs proves itself by its signature over the
			// challenge, which may travel in the clear.
			ans["challenge"] = hex.EncodeToString(ch)
			s.proof = proofSignature
		}
		s.pending, s.challenge = &who, ch
		return ans
	case "credential":
		// The credential itself is the proof; there is no identity until the
		// authority names one for it.
		s.pending, s.proof = &Identity{}, proofCredential
		return map[string]any{"ok": true, "protocol": Protocol, "anchor": s.auth.Anchor().String()}
	case "guest":
		// B5: a guest presents a genesis of its own and proves its root key.
		raw, err := hex.DecodeString(str(req, "genesis"))
		if err != nil {
			return Refuse(CodeBadRequest, errors.New("a guest presents its genesis in hex"))
		}
		g, err := event.Parse(raw)
		if err != nil || !g.IsGenesis() || g.HeadOnly {
			return Refuse(CodeBadRequest, errors.New("a guest presents a whole genesis event of its own"))
		}
		ch := make([]byte, ChallengeSize)
		if _, err := io.ReadFull(s.rnd, ch); err != nil {
			return Refuse(CodeBadRequest, err)
		}
		who := Identity{Name: "guest", Guest: true, Signer: g.Event.Author, Reads: []string{}}
		if ro, ok := s.auth.(ReadOpener); ok {
			who.Reads = ro.ReadOpen()
		}
		copy(who.Key[:], g.ID[:])
		s.pending, s.challenge, s.proof = &who, ch, proofSignature
		return map[string]any{"ok": true, "protocol": Protocol, "anchor": s.auth.Anchor().String(),
			"challenge": hex.EncodeToString(ch)}
	}
	return Refuse(CodeBadRequest, errors.New("hello offers auth \"key\", \"credential\" or \"guest\""))
}

// ProofMessage is what a key signs to prove itself: the protocol, the
// challenge and the anchor.
func ProofMessage(challenge []byte, anchor frame.ID) []byte {
	out := append([]byte(Protocol), challenge...)
	return append(out, anchor[:]...)
}

// prove answers the pending hello with the one proof that hello asked for.
// Whatever fails leaves the session unbound and with nothing pending, so the
// next try starts with a new hello (B3).
func (s *Session) prove(req map[string]any) map[string]any {
	pending, kind, challenge, sealTo := s.pending, s.proof, s.challenge, s.sealTo
	s.forget()
	if pending == nil || kind == proofNone {
		return Refuse(CodeHelloFirst, errors.New("prove answers a hello"))
	}
	fail := func(why string) map[string]any { return Refuse(CodeProof, errors.New(why)) }
	switch kind {
	case proofCredential:
		// A credential the booth issued, or on a stream the booth itself
		// bound to one program, the empty credential naming that program.
		// Given, and a string: a missing or other value is no proof.
		tok, given := req["credential"].(string)
		if !given {
			return fail("this hello asked for a credential, and prove gave none")
		}
		who, ok := s.auth.Credential(tok)
		if !ok {
			return fail("no program holds that credential")
		}
		s.Who, s.Bound = who, true
		return s.whoami()
	case proofSignature:
		sig, err := hex.DecodeString(str(req, "sig"))
		if pending.Signer == nil || len(challenge) != ChallengeSize || err != nil ||
			!ed25519.Verify(pending.Signer, ProofMessage(challenge, s.auth.Anchor()), sig) {
			return fail("the proof does not hold")
		}
	case proofOpened:
		got, err := hex.DecodeString(str(req, "challenge"))
		if len(pending.Reader) == 0 || len(challenge) != ChallengeSize || err != nil ||
			len(got) != ChallengeSize || subtle.ConstantTimeCompare(got, challenge) != 1 {
			return fail("the challenge was not opened")
		}
	default:
		return fail("this hello asked for no proof prove knows")
	}
	var given *key.Reader
	if sealed, ok := req["reader"].(string); ok && sealed != "" {
		// The key's reader, sealed to the one-time half this hello offered.
		// It must be the reader the keyring names for this key, or nothing
		// binds.
		if sealTo == nil || pending.Owner || len(pending.Reader) == 0 {
			return fail("this hello offered no seal for a reader")
		}
		raw, err := hex.DecodeString(sealed)
		if err != nil {
			return fail("the reader given is not hex")
		}
		priv, err := key.OpenFrom(*sealTo, raw, ReaderInfo)
		if err != nil {
			return fail("the reader given does not open under the seal this hello offered")
		}
		r, err := key.ReaderFrom(priv)
		if err != nil || subtle.ConstantTimeCompare(r.Public(), pending.Reader) != 1 {
			return fail("the reader given is not this key's reader")
		}
		given = &r
	}
	s.Who, s.Bound, s.reader = *pending, true, given
	return s.whoami()
}

func (s *Session) whoami() map[string]any {
	w := s.Who
	reads := w.Reads
	if reads == nil {
		reads = []string{}
	}
	judged := "lineage"
	if w.Owner {
		judged = "full"
	}
	if w.Judged != "" {
		judged = w.Judged
	}
	return map[string]any{"ok": true, "protocol": Protocol, "key": hex.EncodeToString(w.Key[:]),
		"name": w.Name, "generation": w.Gen, "owner": w.Owner, "reads": reads,
		"writes": w.Signer != nil || w.Credential != "", "program": w.Program, "judged": judged, "guest": w.Guest,
		"reader_held": s.reader != nil}
}

// Client speaks rokh.booth/1 over an ordered byte stream.
type Client struct {
	mu  sync.Mutex
	w   io.Writer
	r   *bufio.Reader
	seq int
}

// NewClient speaks over a stream a program already holds.
func NewClient(rw io.ReadWriter) *Client {
	return &Client{w: rw, r: bufio.NewReaderSize(rw, 64<<10)}
}

// Call sends one request and reads its answer.
func (c *Client) Call(op string, fields map[string]any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	req := map[string]any{"v": Protocol, "id": fmt.Sprint(c.seq), "op": op}
	for k, v := range fields {
		if k == "v" || k == "id" || k == "op" {
			continue
		}
		req[k] = v
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.w.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := readLine(c.r)
	if err != nil {
		return nil, err
	}
	var ans map[string]any
	if err := json.Unmarshal(line, &ans); err != nil {
		return nil, err
	}
	if ans["id"] != req["id"] {
		return nil, fmt.Errorf("booth: the answer names request %v, not %v", ans["id"], req["id"])
	}
	return ans, nil
}

// ProveKey binds the session to a key that can sign.
func (c *Client) ProveKey(kid [32]byte, signer ed25519.PrivateKey) (map[string]any, error) {
	h, err := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hex.EncodeToString(kid[:])})
	if err != nil {
		return nil, err
	}
	if h["ok"] != true {
		return h, fmt.Errorf("booth: hello refused: %v", h["error"])
	}
	ch, _ := hex.DecodeString(str(h, "challenge"))
	anchor, err := frame.ParseID(str(h, "anchor"))
	if err != nil {
		return nil, err
	}
	return c.Call("prove", map[string]any{"sig": hex.EncodeToString(ed25519.Sign(signer, ProofMessage(ch, anchor)))})
}

// ProveReader binds the session to a key that can only read, by opening the
// challenge sealed to its reader, and gives the booth that reader, sealed to
// the one-time half the hello offered: the booth opens envelopes for this
// session with it and no other (B4), and forgets it when the session ends.
func (c *Client) ProveReader(kid [32]byte, r key.Reader) (map[string]any, error) {
	h, err := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hex.EncodeToString(kid[:])})
	if err != nil {
		return nil, err
	}
	sealed, _ := hex.DecodeString(str(h, "sealed"))
	ch, err := key.OpenFrom(r, sealed, ChallengeInfo)
	if err != nil {
		return nil, err
	}
	req := map[string]any{"challenge": hex.EncodeToString(ch)}
	if err := giveReader(h, r, req); err != nil {
		return nil, err
	}
	return c.Call("prove", req)
}

// ProveKeyWithReader binds the session to a key that signs, and gives the
// booth its reader as ProveReader does.
func (c *Client) ProveKeyWithReader(kid [32]byte, signer ed25519.PrivateKey, r key.Reader) (map[string]any, error) {
	h, err := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "key", "key": hex.EncodeToString(kid[:])})
	if err != nil {
		return nil, err
	}
	if h["ok"] != true {
		return h, fmt.Errorf("booth: hello refused: %v", h["error"])
	}
	ch, _ := hex.DecodeString(str(h, "challenge"))
	anchor, err := frame.ParseID(str(h, "anchor"))
	if err != nil {
		return nil, err
	}
	req := map[string]any{"sig": hex.EncodeToString(ed25519.Sign(signer, ProofMessage(ch, anchor)))}
	if err := giveReader(h, r, req); err != nil {
		return nil, err
	}
	return c.Call("prove", req)
}

// giveReader seals a key's reader to the one-time half a hello offered and
// puts it in the prove request. A hello that offered none is given nothing.
func giveReader(hello map[string]any, r key.Reader, prove map[string]any) error {
	to, err := hex.DecodeString(str(hello, "seal_to"))
	if err != nil || len(to) == 0 {
		return nil
	}
	sealed, err := key.SealTo(to, r.Bytes(), ReaderInfo, rand.Reader)
	if err != nil {
		return err
	}
	prove["reader"] = hex.EncodeToString(sealed)
	return nil
}

// ProveCredential binds the session to a booth-issued credential.
func (c *Client) ProveCredential(token string) (map[string]any, error) {
	if _, err := c.Call("hello", map[string]any{"versions": []string{Protocol}, "auth": "credential"}); err != nil {
		return nil, err
	}
	return c.Call("prove", map[string]any{"credential": token})
}
