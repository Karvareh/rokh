package daemon

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"rokh/canon"
	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/harness"
	"rokh/key"
	"rokh/ledger"
	"rokh/turn"
	"rokh/vessel"
)

// Protocol names what this door speaks. The second version added typed
// recording answers, attempts, a precondition on the heads and the
// capabilities op; the third adds a cursor and filters on log, the wait op,
// the door attestation, the key and authority beside each event, and the
// ancestry_pending code. Everything the earlier versions said, it still says
// in the same words.
const Protocol = "rokh.daemon/3"

// The three answers to "was it recorded?".
//
// "unknown" is not a softer "not-recorded". It is the ending nobody could read
// back, said out loud; a caller that meets it asks the attempt op, and does not
// send again.
//
//	— T8.5, T10.4, T12.3
const (
	Recorded    = "recorded"
	NotRecorded = "not-recorded"
	Unknown     = "unknown"
)

// MaxAttempt bounds the length of an attempt's name.
const MaxAttempt = 128

// attemptOracle names the attestation an attempt lives in. It is the recording
// program's testimony about its own call — which name its caller gave this
// recording — and, like every attestation, it proves the signer said so and
// nothing about the world.
const attemptOracle = "attempt"

// turnPatience is how long a request waits for another writer to finish.
const turnPatience = 15 * time.Second

// writingOps are the ops that can put an event on the carrier. Each of them
// takes the writing turn and says in its answer what it recorded.
var writingOps = map[string]bool{"write": true, "append": true, "intent": true, "outcome": true}

// coded is an error with a stable code for programs beside the sentence for
// people.
type coded struct {
	code string
	err  error
}

func (c coded) Error() string { return c.err.Error() }
func (c coded) Unwrap() error { return c.err }

func withCode(code string, err error) error { return coded{code: code, err: err} }

func codeOf(err error, otherwise string) string {
	var c coded
	if errors.As(err, &c) {
		return c.code
	}
	return otherwise
}

// refusal is a failure with its code.
func refusal(code string, err error) map[string]any {
	return map[string]any{"ok": false, "error": err.Error(), "code": code}
}

// notRecorded is a writing request's refusal: it stopped before anything
// reached the carrier, or the carrier was read back and holds nothing of it.
func notRecorded(code string, err error) map[string]any {
	r := refusal(code, err)
	r["record"] = NotRecorded
	return r
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// takeTurn takes the carrier's writing turn for a request that can record, and
// the shared turn for the attempt query, so that a "not recorded" is never read
// while a recording is in flight somewhere else. With no carrier directory
// there is no turn to take, and the server's own mutex is all there is.
//
//	— T8.5, T4.4
func (s *Server) takeTurn(op string) (func() error, map[string]any) {
	if s.opts.Dir == "" {
		return nil, nil
	}
	take := turn.Acquire
	switch {
	case writingOps[op]:
	case op == "attempt":
		take = turn.AcquireShared
	default:
		return nil, nil
	}
	lk, err := take(s.opts.Dir, turnPatience)
	release := func() error { return nil }
	if err == nil {
		if writingOps[op] {
			s.lock = lk
		}
		release = func() error {
			if s.lock == lk {
				s.lock = nil
			}
			return lk.Release()
		}
	}
	if err != nil {
		code := "carrier_unreadable"
		if errors.Is(err, turn.ErrBusy) {
			code = "turn_busy"
		}
		if writingOps[op] {
			return nil, notRecorded(code, err)
		}
		return nil, refusal(code, err)
	}
	return release, nil
}

// commit takes one signed event through the commit point.
//
// The ledger judges first, because a rejected event never reaches the carrier.
// Then the object, then the reference — and the reference is the commit point:
// what it reaches is recorded and what it does not is not, whatever a step
// returned. So when a step fails, the answer is not taken from the step. The
// view is rebuilt from the references and the event is looked for there, and
// the live answers never again hold an event the carrier does not.
//
// It returns nil when the event is recorded, and the typed answer otherwise.
//
//	— T8.5, T4.4, T3.3
func (s *Server) commit(raw []byte, pin func(*carrier.Recording, frame.ID) error) (frame.ID, map[string]any) {
	var id frame.ID
	if h, _, err := frame.ParseHead(raw); err == nil {
		id = h.ID
	} else {
		return id, notRecorded("not_accepted", fmt.Errorf("not an event: %w", err))
	}
	own := s.holder()
	if own == nil {
		return id, notRecorded("turn_unavailable", errors.New("this door holds no writer's turn on the vessel; nothing was recorded"))
	}
	// The session the event is sealed with, at its own point: its parents,
	// as this view holds them now (E3, E4). It is found before the view
	// takes the event: a point whose owner cannot be given seals nothing,
	// and nothing is recorded.
	// An event whose parents this view does not hold accepted is judged
	// below and not recorded; there is no point to seal it at.
	var sealer carrier.Sealer
	if pe, err := event.Parse(raw); err == nil && s.opts.SealerAt != nil && s.settledAt(pe.Event.Parents) {
		sl, err := s.opts.SealerAt(s.led, pe.Event.Parents)
		if err != nil {
			code := "ancestry_unproven"
			if errors.Is(err, key.ErrKeyNotLive) {
				code = "key_revoked"
			}
			return id, notRecorded(code, fmt.Errorf("event %s cannot be sealed at its own point: %w; nothing was recorded", id.Short(), err))
		}
		sealer = sl
	}
	st, err := s.led.Add(raw)
	if st == ledger.Rejected {
		if over := s.led.Overturned(); len(over) > 0 && over[0] == id {
			return id, s.overturn(raw, over, err)
		}
	}
	if err != nil || st != ledger.Accepted {
		// The verdict is about bytes that will not be stored. The view lets
		// go of them at once, so nothing is answered from a judgement of an
		// event that never was, and a stream of such offers does not grow
		// the view. Not arrived is not the same as not existing, and the two
		// get two codes.
		//   — T3.3, T12.3
		code := "not_accepted"
		if err == nil {
			if st == ledger.Pending {
				// B8: what cannot be proven is pending and nothing is
				// recorded (C8).
				code = "ancestry_unproven"
				why, _ := s.led.Why(id)
				if why == "" {
					why = "its ancestry is not proven here"
				}
				err = fmt.Errorf("event %s: %s; not stored", st, why)
			} else if why, said := s.led.Why(id); said {
				err = fmt.Errorf("event %s: %s; not stored", st, why)
			} else {
				err = fmt.Errorf("event %s; not stored", st)
			}
		}
		s.led.Forget(id)
		return id, notRecorded(code, err)
	}
	signed, _ := s.led.Get(id)
	// One recording is one vessel commit: the event and the reference that
	// makes it one go together, or nothing does (contract 2.6).
	rec, err := s.car.Begin(own)
	if err != nil {
		return id, s.settle(id, err)
	}
	if sealer != nil {
		rec.SealWith(sealer)
	}
	if err := rec.Event(id, signed.Head, signed.Body, signed.Event.Address); err != nil {
		rec.Abandon()
		return id, s.settle(id, err)
	}
	if err := pin(rec, id); err != nil {
		rec.Abandon()
		return id, s.settle(id, err)
	}
	out, err := rec.Commit()
	if out != vessel.Recorded {
		if err == nil {
			err = fmt.Errorf("the vessel answered %s", out)
		}
		return id, s.settle(id, err)
	}
	s.index(id)
	s.moved()
	return id, nil
}

// settledAt says whether this view holds every parent accepted: the point of
// an event signed on them is known here.
func (s *Server) settledAt(parents []frame.ID) bool {
	if len(parents) == 0 {
		return false
	}
	for _, p := range parents {
		if s.led.State(p) != ledger.Accepted {
			return false
		}
	}
	return true
}

// settle answers a recording whose step failed, by reading the carrier back.
// If the carrier cannot be read, the answer is "unknown", and the view is
// marked so that nothing is answered from it until it has been rebuilt.
//
//	— T8.5, T4.4
func (s *Server) settle(id frame.ID, cause error) map[string]any {
	if err := s.reload(); err != nil {
		s.unsettled = true
		r := refusal("storage_failed", fmt.Errorf("%v; and the carrier could not be read back to learn whether the event was recorded: %v", cause, err))
		r["record"] = Unknown
		r["id"] = id.String()
		return r
	}
	if s.led.State(id) == ledger.Accepted {
		return nil
	}
	return notRecorded("storage_failed", cause)
}

// reload rebuilds the view from the references, whatever the view held: the
// references the carrier holds now, read afresh (refresh).
func (s *Server) reload() error {
	if _, err := s.car.Refresh(); err != nil {
		return err
	}
	refs, err := s.car.Refs()
	if err != nil {
		return err
	}
	heads := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		heads = append(heads, id)
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].Compare(heads[j]) < 0 })
	raw, err := s.car.Get(s.car.Anchor())
	if err != nil {
		return fmt.Errorf("genesis unreadable: %w", err)
	}
	led, err := ledger.Load(raw, s.car.Get, heads)
	if err != nil {
		return err
	}
	s.led = led
	s.unsettled = false
	s.reindex()
	s.moved()
	return nil
}

// precondition holds a request to the heads it was prepared against. It is
// read under the writing turn, from a view just read from the carrier, so no
// other writer can move the heads between this check and the commit point.
//
//	— T8.5, T4.4
func (s *Server) precondition(expect *[]string) map[string]any {
	if expect == nil {
		return nil
	}
	want := append([]string(nil), (*expect)...)
	sort.Strings(want)
	have := s.headNames()
	if strings.Join(want, ",") == strings.Join(have, ",") {
		return nil
	}
	r := notRecorded("precondition_failed", errors.New("the ledger's heads are no longer the ones this request was prepared against; nothing was written"))
	r["heads"] = have
	return r
}

func (s *Server) headNames() []string {
	have := []string{}
	for _, h := range s.led.Heads() {
		have = append(have, h.String())
	}
	sort.Strings(have)
	return have
}

// validAttempt keeps an attempt's name a name: short, readable text.
func validAttempt(name string) error {
	if name == "" || len(name) > MaxAttempt {
		return withCode("attempt_invalid", fmt.Errorf("an attempt is named with 1 to %d bytes; this one has %d", MaxAttempt, len(name)))
	}
	if !utf8.ValidString(name) {
		return withCode("attempt_invalid", errors.New("an attempt's name is not valid UTF-8"))
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return withCode("attempt_invalid", errors.New("an attempt's name holds a control character"))
		}
	}
	return nil
}

// joined gives each part its length, so no two different lists of parts join
// into the same bytes.
func joined(domain string, parts ...[]byte) []byte {
	out := append([]byte(domain), 0)
	var n [4]byte
	for _, p := range parts {
		binary.BigEndian.PutUint32(n[:], uint32(len(p)))
		out = append(out, n[:]...)
		out = append(out, p...)
	}
	return out
}

// attemptWho is the first half of an attempt's claim: the name, bound to this
// ledger and to the key that signs.
func attemptWho(anchor frame.ID, author ed25519.PublicKey, name string) []byte {
	h := frame.Hash(joined("rokh/attempt/name/1", anchor[:], author, []byte(name)))
	return h[:]
}

// attemptWhat is the second half: the exact request the name was given to, in
// its one canonical encoding.
func attemptWhat(op string, fields map[string]any) ([]byte, error) {
	fields["op"] = op
	b, err := canon.Marshal(fields)
	if err != nil {
		return nil, err
	}
	h := frame.Hash(joined("rokh/attempt/request/1", b))
	return h[:], nil
}

// findAttempt looks through the accepted events for one this signer recorded
// under this name. The binding is inside the signed bytes, so nothing is kept
// beside the ledger, and a carrier read afresh gives the same answer. An event
// by any other author does not count, whatever claim it carries.
//
//	— T8.5, T3.6
func (s *Server) findAttempt(who []byte, author ed25519.PublicKey) (frame.ID, []byte, bool) {
	var key [32]byte
	copy(key[:], who)
	id, found := s.attempts[key]
	if !found {
		return frame.Zero, nil, false
	}
	e, held := s.led.Get(id)
	if !held || !bytes.Equal(e.Event.Author, author) {
		return frame.Zero, nil, false
	}
	for _, a := range e.Event.Attest {
		if a.Oracle == attemptOracle && len(a.Claim) == 64 && bytes.Equal(a.Claim[:32], who) {
			return id, a.Claim[32:], true
		}
	}
	return frame.Zero, nil, false
}

// attemptFor settles an attempt before anything is signed. The same request
// under the same name is the event already recorded, and answers as it did; a
// different request under the name is refused. Otherwise it returns the claim
// to sign into the new event.
//
//	— T8.5, T3.6, T10.1
func (s *Server) attemptFor(name string, author ed25519.PublicKey, op string, fields map[string]any, answer func(frame.ID) map[string]any) ([]byte, map[string]any) {
	if name == "" {
		return nil, nil
	}
	if err := validAttempt(name); err != nil {
		return nil, notRecorded("attempt_invalid", err)
	}
	what, err := attemptWhat(op, fields)
	if err != nil {
		return nil, notRecorded("bad_request", err)
	}
	who := attemptWho(s.car.Anchor(), author, name)
	if id, held, found := s.findAttempt(who, author); found {
		if !bytes.Equal(held, what) {
			return nil, notRecorded("attempt_conflict", fmt.Errorf("the attempt %q already names a different request (event %s); nothing was written", name, id.Short()))
		}
		r := answer(id)
		r["record"] = Recorded
		r["already"] = true
		r["attempt"] = name
		return nil, r
	}
	return append(append([]byte(nil), who...), what...), nil
}

// attempt answers, reading only, whether a recording was made under a name. A
// caller whose answer went missing asks this, and does not send again.
//
//	— T8.5, T10.4
func (s *Server) attempt(req request) map[string]any {
	if err := validAttempt(req.Attempt); err != nil {
		return refusal("attempt_invalid", err)
	}
	var author ed25519.PublicKey
	if req.Author != "" {
		b, err := hex.DecodeString(req.Author)
		if err != nil || len(b) != ed25519.PublicKeySize {
			return refusal("bad_request", errors.New("author is a public key in 64 hex digits"))
		}
		author = ed25519.PublicKey(b)
	} else {
		priv, _, err := s.key(req.Key)
		if err != nil {
			return fail(err)
		}
		author = priv.Public().(ed25519.PublicKey)
		wipe(priv)
	}
	id, what, found := s.findAttempt(attemptWho(s.car.Anchor(), author, req.Attempt), author)
	if !found {
		return ok(map[string]any{"attempt": req.Attempt, "record": NotRecorded})
	}
	e, _ := s.led.Get(id)
	return ok(map[string]any{"attempt": req.Attempt, "record": Recorded, "id": id.String(),
		"request": hex.EncodeToString(what), "address": e.Event.Address, "verb": e.Event.Verb})
}

// capabilities says what this door is: the protocol, the generations it reads
// and writes, its limits and modes, and which names sign here with which public
// key under which authority. Nothing in it is secret — names and public keys,
// never a private byte.
//
//	— T8, N4.6
func (s *Server) capabilities() map[string]any {
	rootHeld := s.opts.Root != nil
	signers := []map[string]any{{
		"name": keyRoot, "public": hex.EncodeToString(s.led.Root()),
		"authority": nil, "held": rootHeld,
		"signs_here": rootHeld && s.opts.AllowSign && !s.opts.NoRoot && !s.opts.ReadOnly,
	}}
	standing := map[string]bool{}
	for _, g := range s.led.ActiveGrants() {
		standing[g.String()] = true
	}
	type held struct {
		name string
		pub  ed25519.PublicKey
		auth string
	}
	var keys []held
	if s.opts.Keys != nil {
		names := append([]string(nil), s.opts.Keys.Names()...)
		sort.Strings(names)
		for _, n := range names {
			priv, authority, found := s.opts.Keys.Key(n)
			if !found || len(priv) != ed25519.PrivateKeySize {
				continue
			}
			k := held{name: n, pub: priv.Public().(ed25519.PublicKey)}
			wipe(priv)
			if authority != nil {
				k.auth = authority.String()
			}
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		alias, pub, auth := k.name, k.pub, []byte(k.auth)
		signers = append(signers, map[string]any{
			"name": alias, "public": hex.EncodeToString(pub), "held": true,
			"authority": string(auth), "standing": standing[string(auth)],
			"signs_here": s.opts.AllowSign && !s.opts.ReadOnly,
		})
	}
	allOps := append([]string(nil), ops...)
	sort.Strings(allOps)
	scope := "process"
	if s.opts.Dir != "" {
		scope = "carrier"
	}
	release := s.opts.Release
	if release == "" {
		release = "unnamed"
	}
	return ok(map[string]any{
		"protocol": Protocol, "release": release,
		"anchor":      s.led.Genesis().String(),
		"generations": map[string]any{"reads": []string{frame.Magic}, "writes": frame.Magic},
		"limits": map[string]any{"line": MaxLine, "frame": frame.MaxFrame,
			"payload": event.MaxPayload, "attempt": MaxAttempt},
		"read_only": s.opts.ReadOnly, "allow_sign": s.opts.AllowSign, "no_root": s.opts.NoRoot,
		"ops":           allOps,
		"record_states": []string{Recorded, NotRecorded, Unknown},
		"preconditions": []string{"expect_heads"},
		"attempts": map[string]any{"ops": []string{"write", "intent", "outcome"},
			"query": "attempt", "bound_to": []string{"signer", "ledger", "request"}},
		"writer_turn": scope,
		"door":        s.opts.Door,
		"log":         map[string]any{"cursor": "after", "filters": []string{"address", "verb"}, "wait": "wait"},
		"signers":     signers,
	})
}

// declarationName is the name of a harness's eight declarations as bound: the
// hash of their canonical bytes. The same meaning declared with its verbs in
// any order has one name, and a program can carry it into what it witnesses.
// It is a name for the declaration, not an authority: binding grants nothing.
//
//	— T11.10, T10.7
func declarationName(b harness.Bound) string {
	can := append([]string(nil), b.Can...)
	sort.Strings(can)
	enc, err := canon.Marshal(map[string]any{
		"namespace": b.Namespace, "version": b.Version, "can": can,
		"unknown": string(b.Unknown), "repeat": string(b.Effects.Repeat),
		"retry": string(b.Effects.Retry), "compensate": string(b.Effects.Compensate),
		"ending": string(b.Effects.Ending),
	})
	if err != nil {
		return ""
	}
	return frame.Hash(joined("rokh/harness/declaration/1", enc)).String()
}

// overturn answers an offer whose body overturned a head this door held (R1):
// the event and what rested on it are refused. The body is kept on the
// carrier as sealed rejection evidence, never served, so a door opened later
// on the same carrier judges the whole event and reaches the same refusals
// instead of accepting the head alone again (ruling D1). Every view beside
// the ledger is rebuilt before the answer.
func (s *Server) overturn(raw []byte, over []frame.ID, cause error) map[string]any {
	if e, err := event.Parse(raw); err == nil && !e.HeadOnly {
		// The body is recorded as sealed rejection evidence: one more
		// envelope for the same id (E2), never served, never authorizing.
		// It is sealed at its own point where the door seals so.
		var sealer carrier.Sealer
		if s.opts.SealerAt != nil {
			sealer, _ = s.opts.SealerAt(s.led, e.Event.Parents)
		}
		if own := s.holder(); own != nil && (s.opts.SealerAt == nil || sealer != nil) {
			if rec, err := s.car.Begin(own); err == nil {
				if sealer != nil {
					rec.SealWith(sealer)
				}
				if rec.Event(e.ID, e.Head, e.Body, e.Event.Address) == nil {
					if out, _ := rec.Commit(); out != vessel.Recorded {
						s.unsettled = true
					}
				} else {
					rec.Abandon()
				}
			}
		}
	}
	s.reindex()
	s.moved()
	ids := make([]string, 0, len(over))
	for _, id := range over {
		ids = append(ids, id.String())
	}
	if cause == nil {
		cause = fmt.Errorf("its body overturned its head")
	}
	r := notRecorded("not_accepted", cause)
	r["overturned"] = ids
	r["evidence"] = "the body is kept sealed as rejection evidence and is never served"
	return r
}

// holder is this server's hold on the vessel for the request in hand: the
// turn it took, or the host's owner for a door without a folder.
func (s *Server) holder() vessel.Owner {
	if s.lock != nil {
		return s.lock
	}
	return s.opts.Owner
}
