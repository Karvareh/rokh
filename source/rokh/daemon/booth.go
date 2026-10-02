package daemon

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sort"
	"strings"

	"rokh/booth"
	"rokh/event"
	"rokh/frame"
	"rokh/transport"
)

// The daemon is the booth of a carrier (contract section 5, axiom 32): it
// serves rokh.booth/1 on any ordered byte stream, binds every session to one
// key of the carrier's keyring, and answers inside that key's view.

// Authority is what this booth knows of its carrier: the anchor and the live
// keys of the keyring. The owner is key id zero and signs with the root.
func (s *Server) Authority() booth.Authority { return authority{s} }

type authority struct{ s *Server }

func (a authority) Anchor() frame.ID {
	a.s.mu.RLock()
	defer a.s.mu.RUnlock()
	return a.s.led.Genesis()
}

func (a authority) KeyByID(id [32]byte) (booth.Identity, bool) {
	if id != ([32]byte{}) {
		// A key added or revoked through another door counts at hello too.
		// A view that cannot be brought up binds no key.
		if err := a.s.freshen(); err != nil {
			return booth.Identity{}, false
		}
	}
	a.s.mu.RLock()
	defer a.s.mu.RUnlock()
	if id == ([32]byte{}) {
		who := booth.Identity{Key: id, Gen: 1, Name: "owner", Owner: true, Signer: a.s.led.Root(), Reads: []string{""}}
		if a.s.restricted() {
			// A door opened with a key's passphrase serves the owner that
			// key's view, and says so (B4, 3.3).
			who.Judged = "lineage"
		}
		return who, true
	}
	adds, _ := a.s.led.Keyring()
	var best *booth.Identity
	for _, eid := range adds {
		e, ok := a.s.led.Get(eid)
		if !ok {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || k.Key != id {
			continue
		}
		if best != nil && best.Gen > k.Gen {
			continue
		}
		best = &booth.Identity{Key: k.Key, Gen: k.Gen, Name: k.Name, Signer: k.Signer, Reader: k.Reader, Reads: k.Reads}
	}
	if best == nil {
		return booth.Identity{}, false
	}
	return *best, true
}

func (a authority) Credential(string) (booth.Identity, bool) { return booth.Identity{}, false }

// ReadOpen lists the scopes of the live open grants that are read-open (B5),
// as the carrier holds them now. A view that cannot be brought up lists none.
func (a authority) ReadOpen() []string {
	if err := a.s.freshen(); err != nil {
		return []string{}
	}
	a.s.mu.RLock()
	defer a.s.mu.RUnlock()
	out := []string{}
	for _, id := range a.s.led.OpenGrants() {
		e, _ := a.s.led.Get(id)
		if g, err := event.DecodeGrant(e.Event.Payload); err == nil && g.Read {
			out = append(out, g.Scope)
		}
	}
	return out
}

// ServeBooth runs one rokh.booth/1 session on a stream. When the stream ends
// the booth forgets the reader the session gave and the view it read with it.
func (s *Server) ServeBooth(rw io.ReadWriter) error {
	return booth.Serve(rw, s.Authority(), boothHandler{s}, nil)
}

// ListenBooth serves rokh.booth/1 on a listener from package transport.
func (s *Server) ListenBooth(ln net.Listener) error {
	return transport.Serve(ln, func(c io.ReadWriteCloser) { _ = s.ServeBooth(c) })
}

// readingOps are the ops that read; writing ops are named where they are
// answered.
var readingOps = map[string]bool{"status": true, "log": true, "wait": true, "get": true,
	"attempt": true, "receipts": true, "capabilities": true, "bound": true}

// narrowedOps are the reading ops a session is answered from its own view.
// Every other reading op is the owner's alone (B4).
var narrowedOps = map[string]bool{"status": true, "log": true, "wait": true, "get": true, "capabilities": true}

// current looks a bound identity up again before an answer (B7): a key by
// the generation it proved, live now or not at all; a guest by the read-open
// addresses of now. The owner is always the owner.
func (s *Server) current(who booth.Identity) (booth.Identity, bool) {
	switch {
	case who.Owner:
		return who, true
	case who.Guest:
		who.Reads = authority{s}.ReadOpen()
		return who, true
	case who.Credential != "" || who.Program != "":
		// This booth binds no credential; another booth's is its own to judge.
		return who, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	adds, _ := s.led.Keyring()
	for _, eid := range adds {
		e, ok := s.led.Get(eid)
		if !ok {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || k.Key != who.Key || k.Gen != who.Gen {
			continue
		}
		if !bytes.Equal(k.Signer, who.Signer) || !bytes.Equal(k.Reader, who.Reader) {
			continue
		}
		return booth.Identity{Key: k.Key, Gen: k.Gen, Name: k.Name, Signer: k.Signer, Reader: k.Reader, Reads: k.Reads}, true
	}
	return booth.Identity{}, false
}

// freshen brings the view up to what the carrier holds now before an identity
// is looked up or anything is answered, so a recording or a revocation made
// through another door counts: another door on this carrier, or another
// opening of its folder in another process. A view it cannot bring up is not
// answered from as if it were the present one; the error says why.
func (s *Server) freshen() error {
	s.mu.RLock()
	stale, err := s.stale()
	s.mu.RUnlock()
	if err == nil && !stale {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refresh()
}

// unreadable answers a request whose view could not be brought up to the
// carrier: nothing of the old view is shown, and nothing is recorded.
func unreadable(op string, err error) map[string]any {
	r := booth.Refuse("carrier_unreadable", err)
	if writingOps[op] {
		r["record"] = NotRecorded
	}
	return r
}

// anchorOnly is what a guest is told of this rokh as a whole: which it is,
// and nothing of its keys, seeds, grants or counts (B5).
func (s *Server) anchorOnly() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{"ok": true, "anchor": s.led.Genesis().String(), "judged": "lineage"}
}

// BoothAnswer answers one request of a bound booth session, from the view
// that session may hold (views.go).
func (s *Server) BoothAnswer(sess *booth.Session, req map[string]any) map[string]any {
	op, _ := req["op"].(string)
	if err := s.freshen(); err != nil {
		return unreadable(op, err)
	}
	who, live := s.current(sess.Who)
	if !live {
		// A key the owner revoked is served nothing more, on this session or
		// any it bound before; it may say hello again only if it is live.
		sess.Unbind()
		return booth.Refuse(booth.CodeKeyRevoked, errors.New("this key is no longer live in the keyring; the session is unbound"))
	}
	sess.Who = who
	kind := s.kindOf(sess, who)
	// The door a reading question is answered by: this door for its own
	// view, a door over the session's own view, or over the system layer.
	answering := func() (*Server, map[string]any) {
		var (
			srv *Server
			err error
		)
		switch kind {
		case viewDoor:
			return s, nil
		case viewOwn:
			srv, err = s.ownView(sess, who)
		default:
			srv, err = s.systemView()
			if kind == viewGuest && errors.Is(err, errNoSystemView) {
				// A carrier with no system reader: a guest is answered from
				// this door's view narrowed to the read-open addresses of
				// now, as before T2 (B5). No key's session is.
				return s, nil
			}
		}
		if err != nil {
			return nil, booth.Refuse(booth.CodeViewDenied, err)
		}
		return srv, nil
	}
	switch op {
	case "view":
		switch kind {
		case viewGuest:
			return s.anchorOnly()
		case viewDoor:
			return s.viewOf(func(frame.ID) bool { return true }, s.judgedFor(who))
		}
		srv, bad := answering()
		if bad != nil {
			return bad
		}
		if kind == viewHeads {
			return srv.viewOf(func(frame.ID) bool { return false }, "lineage")
		}
		if s.restricted() {
			return srv.viewOf(func(id frame.ID) bool { return s.wholeInDoor(id.String()) }, "lineage")
		}
		return srv.viewOf(func(frame.ID) bool { return true }, "lineage")
	case "seed":
		if who.Guest {
			return s.anchorOnly()
		}
		s.mu.RLock()
		defer s.mu.RUnlock()
		acc, _, _ := s.led.Tally()
		return map[string]any{"ok": true, "anchor": s.led.Genesis().String(), "scopes": who.Reads,
			"generation": acc, "vessel": nil, "seed": nil}
	case "key.list":
		if !who.Owner {
			return booth.Refuse(booth.CodeOwnerOnly, errors.New("the keyring is listed to the owner"))
		}
		return s.keyList()
	}
	// The request's own id is the message's (B2); an event is named by
	// "event".
	inner := map[string]any{}
	for k, v := range req {
		if k != "v" && k != "id" {
			inner[k] = v
		}
	}
	if ev, ok := req["event"]; ok {
		inner["id"] = ev
		delete(inner, "event")
	}
	srv := s
	switch op {
	case "append":
		if !who.Owner {
			raw, err := hex.DecodeString(strOf(inner, "raw"))
			if err != nil {
				return notRecorded("bad_request", err)
			}
			e, err := event.Parse(raw)
			if err != nil {
				return notRecorded("bad_request", err)
			}
			if who.Signer == nil || !bytes.Equal(e.Event.Author, who.Signer) {
				return notRecorded(booth.CodeViewDenied, errors.New("a session appends only what its own key signed"))
			}
		}
	case "write", "intent", "outcome", "bind":
		if !who.Owner {
			return notRecorded(booth.CodeOwnerOnly, errors.New("this booth signs only for the owner; a key appends what it signed itself"))
		}
	default:
		if !readingOps[op] {
			return booth.Refuse(booth.CodeUnknownOp, errors.New("unknown op "+op))
		}
		if !who.Owner && !narrowedOps[op] {
			return booth.Refuse(booth.CodeOwnerOnly, errors.New("the answer of "+op+" is not narrowed to a view, so it is the owner's alone"))
		}
		var bad map[string]any
		if srv, bad = answering(); bad != nil {
			return bad
		}
	}
	// A question by address or by verb, on a view that is not the door's
	// own, is answered from that view alone; outside it the question is
	// asked of an address that holds nothing.
	_, byAddress := inner["address"].(string)
	_, byVerb := inner["verb"].(string)
	filtered := readingOps[op] && (byAddress || byVerb)
	switch {
	case !readingOps[op]:
	case kind == viewHeads && filtered:
		inner["address"] = nowhere
	case kind == viewGuest && byAddress && !who.Covers(strOf(inner, "address")):
		inner["address"] = nowhere
	}
	line, err := json.Marshal(inner)
	if err != nil {
		return booth.Refuse(booth.CodeBadRequest, err)
	}
	ans := srv.Handle(line)
	if !who.Owner && readingOps[op] {
		// Authority is judged again immediately before anything is shown: a
		// wait may have slept while the owner revoked this key, or withdrew
		// the open grant a guest reads by, and what arrived since is not this
		// session's. A view that cannot be brought up cannot say
		// whether the key still stands, and nothing is shown.
		if err := s.freshen(); err != nil {
			return unreadable(op, err)
		}
		now, live := s.current(sess.Who)
		if !live {
			sess.Unbind()
			return booth.Refuse(booth.CodeKeyRevoked, errors.New("this key was revoked before the answer was given; nothing of it is shown, and the session is unbound"))
		}
		sess.Who, who = now, now
	}
	if code, _ := ans["code"].(string); code == "ancestry_pending" {
		ans["code"] = booth.CodeAncestryUnproven
	}
	if ev, ok := ans["id"]; ok {
		ans["event"] = ev
		delete(ans, "id")
	}
	if !readingOps[op] {
		return ans
	}
	switch kind {
	case viewDoor:
		if s.restricted() {
			lineageOnly(ans)
		}
	case viewOwn:
		if s.restricted() {
			// A door opened with a key serves at most that key's view: what
			// the door's own view holds whole, and what this session's own
			// reader opened.
			narrow(ans, filtered, func(id string, _ bool, _ string) bool { return s.wholeInDoor(id) })
		}
		lineageOnly(ans)
	case viewHeads:
		narrow(ans, filtered, func(string, bool, string) bool { return false })
		lineageOnly(ans)
	case viewGuest:
		if op == "status" && ans["ok"] == true {
			return s.anchorOnly()
		}
		// A guest sees the read-open addresses of now and nothing of the
		// system layer, which names this rokh's keys (B5).
		narrow(ans, filtered, func(_ string, system bool, addr string) bool { return !system && who.Covers(addr) })
		lineageOnly(ans)
	}
	return ans
}

// judgedFor is how far the door's own view is judged for a session of it:
// "full" for the owner on the owner's door, "lineage" on a key's.
func (s *Server) judgedFor(who booth.Identity) string {
	if who.Owner && !s.restricted() {
		return "full"
	}
	return "lineage"
}

func strOf(m map[string]any, k string) string { v, _ := m[k].(string); return v }

// viewOf is the three layers of contract section 6 inside this door's view:
// Star is the rokh itself, Planet the first address component, Moon the
// second. keep says which events whole in this view are the session's.
func (s *Server) viewOf(keep func(frame.ID) bool, judged string) map[string]any {
	if err := s.freshen(); err != nil {
		return unreadable("view", err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	planets := map[string]map[string]int{}
	system := 0
	for _, id := range s.led.Order() {
		e, _ := s.led.Get(id)
		if e.System {
			system++
			continue
		}
		if e.HeadOnly || !keep(id) {
			continue
		}
		parts := strings.SplitN(e.Event.Address, "/", 3)
		moons := planets[parts[0]]
		if moons == nil {
			moons = map[string]int{}
			planets[parts[0]] = moons
		}
		moon := ""
		if len(parts) > 1 {
			moon = parts[1]
		}
		moons[moon]++
	}
	names := make([]string, 0, len(planets))
	for p := range planets {
		names = append(names, p)
	}
	sort.Strings(names)
	var ps []map[string]any
	for _, p := range names {
		var ms []map[string]any
		mnames := make([]string, 0, len(planets[p]))
		for m := range planets[p] {
			mnames = append(mnames, m)
		}
		sort.Strings(mnames)
		for _, m := range mnames {
			ms = append(ms, map[string]any{"moon": m, "events": planets[p][m]})
		}
		ps = append(ps, map[string]any{"planet": p, "moons": ms})
	}
	adds, seeds := s.led.Keyring()
	return map[string]any{"ok": true, "judged": judged,
		"star":    map[string]any{"anchor": s.led.Genesis().String(), "system_events": system, "keys": len(adds), "seeds": len(seeds)},
		"planets": ps}
}

func (s *Server) keyList() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	adds, _ := s.led.Keyring()
	count := map[[32]byte]int{}
	var keys []map[string]any
	for _, id := range adds {
		e, _ := s.led.Get(id)
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil {
			continue
		}
		count[k.Key]++
		keys = append(keys, map[string]any{"key": hex.EncodeToString(k.Key[:]), "gen": k.Gen, "name": k.Name,
			"reads": k.Reads, "writes": k.Signer != nil, "event": id.String()})
	}
	for _, k := range keys {
		raw, _ := hex.DecodeString(k["key"].(string))
		var id [32]byte
		copy(id[:], raw)
		k["concurrent"] = count[id] > 1
	}
	return map[string]any{"ok": true, "keys": keys}
}
