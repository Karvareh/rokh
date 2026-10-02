package gate

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"rokh-home/archive"
	"rokh-home/authority"
	"rokh-home/home"
	"rokh-home/native"
	"rokh-home/store"
)

// Server is the gate of one opened home.
type Server struct {
	home        *home.Home
	ownerKey    []byte
	runDir      string
	ownerLn     net.Listener
	gateLn      net.Listener
	mu          sync.Mutex
	conns       map[net.Conn]bool
	children    map[int]*exec.Cmd
	closed      bool
	offer       HostOffer
	hostRevoked string
	starting    int
	// conductors are the native identities whose conductor this gate is
	// carrying against the host's ceiling, and conductorStarting the ones on
	// their way up. A conductor costs the host a process for as long as it runs,
	// exactly as a program does, and is counted the same way.
	conductors        map[string]bool
	conductorStarting int
	handlers          sync.WaitGroup
	done              chan struct{}
	// native is the broker's runtime manager for the host's Holochain engine,
	// or nil on a gate that was given none. The home itself never reaches it.
	native      *native.Manager
	lastQuiesce map[string]any
	// Launch starts a program in its enclosure. It is set per platform.
	Launch func(spec RunSpec) (*exec.Cmd, error)
}

// Start opens the gate's two sockets in runDir, which must be a private folder
// of this user.
func Start(h *home.Home, runDir string) (*Server, error) {
	return StartWithHost(h, runDir, LocalHostOffer())
}

func StartWithHost(h *home.Home, runDir string, offer HostOffer) (*Server, error) {
	if err := offer.validate(); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(runDir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("gate: %s must be a folder only this user can enter (0700)", runDir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("gate: %s belongs to another user", runDir)
	}
	s := &Server{home: h, ownerKey: h.OwnerProofKey(), runDir: runDir, conns: map[net.Conn]bool{},
		children: map[int]*exec.Cmd{}, conductors: map[string]bool{},
		done: make(chan struct{}), Launch: platformLaunch, offer: offer}
	if s.ownerLn, err = listen(filepath.Join(runDir, "owner.sock")); err != nil {
		return nil, err
	}
	if s.gateLn, err = listen(filepath.Join(runDir, "gate.sock")); err != nil {
		s.ownerLn.Close()
		return nil, err
	}
	go s.accept(s.ownerLn, s.serveOwner)
	go s.accept(s.gateLn, func(c net.Conn) { s.serveProgram(c, "") })
	if offer.ExpiresAt != 0 {
		go func() {
			timer := time.NewTimer(time.Until(time.Unix(offer.ExpiresAt, 0)))
			defer timer.Stop()
			select {
			case <-s.done:
				return
			case <-timer.C:
				s.RevokeHosting("expired")
			}
		}()
	}
	return s, nil
}

func listen(path string) (net.Listener, error) {
	if len(path) >= 104 {
		return nil, fmt.Errorf("gate: socket path is %d bytes; use a shorter folder", len(path))
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("gate: %s exists; a gate does not take over another's socket", path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func (s *Server) accept(ln net.Listener, serve func(net.Conn)) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.Close()
			return
		}
		s.conns[c] = ln == s.ownerLn
		s.handlers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.handlers.Done()
			defer func() {
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
				c.Close()
			}()
			serve(c)
		}()
	}
}

// Done is closed when the gate has shut.
func (s *Server) Done() <-chan struct{} { return s.done }

// Close shuts the gate. Whatever the engine did on the way out is in
// LastQuiesce; Close itself cannot report it, so a caller that needs to know
// calls CloseReporting instead.
func (s *Server) Close() { s.CloseReporting("home:close") }

// LastQuiesce is what the engine did the last time this gate put it away.
func (s *Server) LastQuiesce() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastQuiesce
}

// CloseReporting shuts the gate and answers with what the engine did.
//
// The order is the whole point: the conductor stops taking work, finishes what
// it has, stops consistently and has its last state sealed into this home —
// and only then does the home close and its keys go. A failure anywhere in that
// is in the answer, and the runtime folders are left where they are so it can
// be looked at.
func (s *Server) CloseReporting(attempt string) map[string]any {
	report := s.quiesceNative(attempt)
	s.mu.Lock()
	s.lastQuiesce = report
	s.mu.Unlock()
	s.shut()
	return report
}

func (s *Server) shut() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		<-s.done
		return
	}
	s.closed = true
	for c := range s.conns {
		c.Close()
	}
	for _, cmd := range s.children {
		stopProgram(cmd)
	}
	s.mu.Unlock()
	s.ownerLn.Close()
	s.gateLn.Close()
	// Whatever is still running after the quiesce above — a conductor a failed
	// quiesce left, or one started by a path that never quiesced — does not
	// outlive the home that opened it.
	s.closeNative()
	s.home.CancelWork()
	s.handlers.Wait()
	os.Remove(filepath.Join(s.runDir, "owner.sock"))
	os.Remove(filepath.Join(s.runDir, "gate.sock"))
	close(s.done)
}

func (s *Server) serveOwner(c net.Conn) {
	sc, err := ownerAccept(newLineConn(c), s.ownerKey)
	if err != nil {
		return
	}
	for {
		var req map[string]any
		if err := sc.read(&req); err != nil {
			return
		}
		// Each frame holds one rokh.booth/1 message; its answer names the
		// same id.
		var resp map[string]any
		if v, _ := req["v"].(string); v != Protocol {
			resp = refusal("version_unsupported", "this gate speaks "+Protocol+"; every message names it in \"v\"")
		} else {
			inner := map[string]any{}
			for k, v := range req {
				if k != "v" && k != "id" {
					inner[k] = v
				}
			}
			// The thing a request is about travels as "ref" or "event"
			// (B2), as on a program's connection.
			for _, alias := range []string{"ref", "event"} {
				if v, ok := req[alias]; ok {
					inner["id"] = v
					delete(inner, alias)
				}
			}
			resp = s.dispatch(home.OwnerActor, inner, true)
		}
		resp["v"] = Protocol
		if id, ok := req["id"]; ok {
			resp["id"] = id
		}
		if err := sc.write(resp); err != nil {
			return
		}
		if req["op"] == "close" && resp["ok"] == true {
			go func() {
				time.Sleep(50 * time.Millisecond)
				s.Close()
			}()
			return
		}
	}
}

// Bind makes a connection for a program before the program runs: one end the
// gate serves as that program, the other end for the program to inherit.
func (s *Server) Bind(consumer string) (*os.File, error) {
	if _, ok := s.home.Consumer(consumer); !ok {
		return nil, errors.New("gate: no such program")
	}
	// A socket pair is born without close-on-exec, and marking it is a second
	// step. A program started in that window inherits both ends of a connection
	// the gate made for someone else. ForkLock is held for reading across the
	// two steps, as the standard library does for every descriptor it makes, so
	// no exec can fall between them.
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fds[0])
		syscall.CloseOnExec(fds[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	mine := os.NewFile(uintptr(fds[0]), "gate-bound")
	conn, err := net.FileConn(mine)
	mine.Close()
	if err != nil {
		syscall.Close(fds[1])
		return nil, err
	}
	s.mu.Lock()
	if err := s.hostErrorLocked(); err != nil {
		s.mu.Unlock()
		conn.Close()
		syscall.Close(fds[1])
		return nil, err
	}
	s.conns[conn] = false
	s.handlers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.handlers.Done()
		defer func() {
			s.mu.Lock()
			delete(s.conns, conn)
			s.mu.Unlock()
			conn.Close()
		}()
		s.serveProgram(conn, consumer)
	}()
	return os.NewFile(uintptr(fds[1]), "gate-for-program"), nil
}

func (s *Server) serveProgram(c net.Conn, bound string) {
	first, replay, err := firstLine(c)
	if err != nil {
		return
	}
	if !speaksBooth(first) {
		// The gate speaks rokh.booth/1 only (contract section 5, T2): a
		// program that speaks another protocol is told so, in this one, and
		// the connection ends.
		var req map[string]any
		_ = json.Unmarshal(first, &req)
		ans := refusal("version_unsupported", "this gate speaks "+Protocol+": every message carries \"v\": \""+Protocol+
			"\" and an \"id\"; a session begins with hello (auth \"credential\") and prove")
		ans["v"] = Protocol
		if id, ok := req["id"]; ok {
			ans["id"] = id
		}
		b, _ := json.Marshal(ans)
		c.Write(append(b, '\n'))
		return
	}
	s.serveBooth(struct {
		io.Reader
		io.Writer
	}{replay, c}, bound)
}

func refusal(code, msg string) map[string]any {
	return map[string]any{"ok": false, "code": code, "error": msg}
}

func answerErr(err error) map[string]any {
	if errors.Is(err, store.ErrDurability) {
		return map[string]any{"ok": false, "code": "durability_unknown", "error": err.Error(), "reopen_required": true}
	}
	var host *HostingError
	if errors.As(err, &host) {
		return map[string]any{"ok": false, "code": "host_unavailable", "reason": host.Reason, "error": host.Error()}
	}
	var d *home.Denied
	switch {
	case errors.As(err, &d):
		return map[string]any{"ok": false, "code": "denied", "reason": d.Decision.Reason, "decision": d.Decision, "error": err.Error()}
	case errors.Is(err, home.ErrNotFound):
		return refusal("not_found", err.Error())
	case errors.Is(err, archive.ErrRefused):
		return refusal("refused", err.Error())
	case errors.Is(err, archive.ErrChanged):
		return refusal("source_changed", err.Error())
	case errors.Is(err, archive.ErrStopped):
		return refusal("stopped", err.Error())
	case errors.Is(err, archive.ErrNoSpace):
		return refusal("no_space", err.Error())
	}
	return refusal("failed", err.Error())
}

func str(req map[string]any, key string) string {
	v, _ := req[key].(string)
	return v
}

func num(req map[string]any, key string) int64 {
	switch v := req[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

func okWith(kv map[string]any) map[string]any {
	kv["ok"] = true
	return kv
}

func (s *Server) dispatch(a home.Actor, req map[string]any, owner bool) map[string]any {
	h := s.home
	op := str(req, "op")
	if err := h.Health(); err != nil && !(owner && (op == "close" || op == "home.status")) {
		return answerErr(err)
	}
	if !owner {
		if err := s.hostError(); err != nil {
			return answerErr(err)
		}
	}
	switch op {
	case "whoami":
		if owner {
			return okWith(map[string]any{"owner": true, "protocol": Protocol})
		}
		c, _ := h.Consumer(a.Consumer)
		return okWith(map[string]any{"consumer": c.ID, "name": c.Name, "session_version": a.Session,
			"authority_version": c.Version, "grants": c.Grants, "protocol": Protocol})
	case "list":
		scope := str(req, "scope")
		if scope == "" {
			scope = authority.WholeHome
		}
		items, err := h.List(a, scope)
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"items": items})
	case "stat":
		item, err := h.Stat(a, str(req, "path"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"item": item})
	case "sheet":
		sheets, err := h.Sheet(a, str(req, "path"), int(num(req, "version")))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"sheets": sheets})
	case "bytes":
		b, total, contentHash, err := h.BytesWithHash(a, str(req, "path"), int(num(req, "version")), str(req, "sub"), num(req, "offset"), num(req, "length"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"data": base64.StdEncoding.EncodeToString(b), "total": total,
			"offset": num(req, "offset"), "content_sha256": contentHash})
	case "search":
		hits, err := h.Search(a, str(req, "query"), int(num(req, "limit")))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"hits": hits})
	case "context":
		ps, err := h.Context(a, str(req, "query"), int(num(req, "max")))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"passages": ps})
	case "draft.open":
		d, err := h.DraftOpen(a, str(req, "path"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"draft": d})
	case "draft.write":
		body, err := base64.StdEncoding.DecodeString(str(req, "data"))
		if err != nil {
			return refusal("bad_request", "data is base64")
		}
		d, err := h.DraftWrite(a, str(req, "id"), body, int(num(req, "revision")))
		if err != nil {
			r := answerErr(err)
			r["draft"] = d
			return r
		}
		return okWith(map[string]any{"draft": d})
	case "draft.read":
		b, d, err := h.DraftRead(a, str(req, "id"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"draft": d, "data": base64.StdEncoding.EncodeToString(b)})
	case "draft.record":
		res, err := h.DraftRecord(a, str(req, "id"))
		if err != nil {
			r := answerErr(err)
			r["result"] = res
			return r
		}
		return okWith(map[string]any{"result": res})
	case "import.preview":
		p, id, err := h.ImportPreview(a, str(req, "source"), str(req, "path"))
		if err != nil {
			r := answerErr(err)
			if p != nil {
				r["preview"] = p
			}
			return r
		}
		return okWith(map[string]any{"import": id, "preview": p})
	case "import.run":
		res, err := h.ImportRun(a, str(req, "id"))
		if err != nil {
			r := answerErr(err)
			r["result"] = res
			return r
		}
		return okWith(map[string]any{"result": res})
	case "import.stop":
		if err := h.ImportStop(a, str(req, "id")); err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{})
	case "import.status":
		st, err := h.ImportStatus(a, str(req, "id"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"status": st})
	case "export":
		opts := archive.ExportOptions{AllowIncomplete: req["allow_incomplete"] == true, KeepTimes: true,
			AllowOutsideLinks: owner && req["allow_outside_links"] == true}
		rep, err := h.Export(a, str(req, "path"), int(num(req, "version")), str(req, "dest"), opts)
		if err != nil {
			r := answerErr(err)
			r["report"] = rep
			return r
		}
		return okWith(map[string]any{"report": rep})
	case "export.history":
		reps, err := h.ExportHistory(a, str(req, "path"), str(req, "dest"))
		if err != nil {
			r := answerErr(err)
			r["reports"] = reps
			return r
		}
		return okWith(map[string]any{"reports": reps})
	case "disclose.preview":
		d, err := h.DisclosePreview(a, str(req, "path"), int(num(req, "version")), num(req, "start"), num(req, "end"), str(req, "recipient"), str(req, "network"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"disclosure": d})
	case "disclose.get":
		d, body, err := h.ConfirmedDisclosure(a, str(req, "hash"))
		if err != nil {
			r := answerErr(err)
			if d.Hash != "" {
				r["disclosure"] = d
			}
			return r
		}
		return okWith(map[string]any{"disclosure": d, "body": base64.StdEncoding.EncodeToString(body)})
	case "ledger":
		inner, _ := req["request"].(map[string]any)
		if inner == nil {
			return refusal("bad_request", "ledger carries a request")
		}
		var ans map[string]any
		if owner {
			ans = h.OwnerLedger(inner)
		} else {
			ans = h.LedgerOp(a, inner)
		}
		// B2: a message's own "id" is the message's; an event the answer
		// names is "event", as the daemon's booth names it.
		if ev, ok := ans["id"]; ok {
			ans["event"] = ev
			delete(ans, "id")
		}
		return ans
	// Tasks: the owner hands work to a program as a signed event on the
	// program's shelf; the program receives it, takes it with an intent,
	// acts within what it holds, and finishes it with an outcome.
	case "task.give":
		input, _ := req["input"].(map[string]any)
		tv, err := h.GiveTask(a, str(req, "consumer"), str(req, "doing"), input)
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"task": tv})
	case "task.list":
		tasks, err := h.Tasks(a, str(req, "consumer"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"tasks": tasks})
	case "task.wait":
		r, err := h.WaitTasks(a, str(req, "after"), int(num(req, "timeout")))
		if err != nil {
			if r != nil {
				return r
			}
			return answerErr(err)
		}
		return okWith(r)
	case "task.take":
		r, err := h.TakeTask(a, str(req, "task"), str(req, "doing"))
		if err != nil {
			if r != nil {
				r["error"] = err.Error()
				return r
			}
			return answerErr(err)
		}
		return okWith(r)
	case "task.done":
		r, err := h.FinishTask(a, str(req, "task"), str(req, "intent"), str(req, "outcome"), str(req, "saying"),
			str(req, "once"), str(req, "state"), str(req, "way_back"))
		if err != nil {
			if r != nil {
				r["error"] = err.Error()
				return r
			}
			return answerErr(err)
		}
		return okWith(r)
	}
	if !owner {
		// A program speaks two native ops and no others; the rest are the
		// broker's, and name runtime folders, ports and checkpoints.
		if r, handled := s.nativeProgramOp(a, op, req); handled {
			return r
		}
		return map[string]any{"ok": false, "code": "denied", "reason": "owner_only",
			"error": fmt.Sprintf("gate: %q is not an op a program speaks", op)}
	}
	if r, handled := s.nativeOwnerOp(op, req); handled {
		return r
	}
	if r, handled := s.nativeProgramOp(a, op, req); handled {
		return r
	}
	switch op {
	case "home.status":
		sum := h.Summary()
		if err := h.Health(); err != nil {
			sum["storage"] = answerErr(err)
		}
		sum["replica"] = h.Replica()
		sum["hosting"] = s.Hosting()
		if m := s.nativeManager(); m != nil {
			sum["native"] = map[string]any{"engine": m.Offer(), "namespace": s.nativeNamespace()}
		} else {
			sum["native"] = map[string]any{"engine": map[string]any{"present": false,
				"reason": "this gate was given no native engine"}}
		}
		sum["protocol"], sum["pid"], sum["run"] = Protocol, os.Getpid(), s.runDir
		return okWith(sum)
	case "replica.mirror":
		info, err := h.Mirror(a, str(req, "destination"), str(req, "passphrase"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"replica": info, "destination": str(req, "destination")})
	case "consumer.add":
		cv, cred, err := h.AddConsumer(a, str(req, "name"), str(req, "kind"), home.Places{Intake: str(req, "intake"), Handover: str(req, "handover")})
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"consumer": cv, "credential": hex.EncodeToString(cred)})
	case "consumer.list":
		list, err := h.Consumers(a)
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"consumers": list})
	case "grant":
		g, err := h.Grant(a, str(req, "consumer"), authority.Action(str(req, "action")), str(req, "scope"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"grant": g})
	case "ungrant":
		if err := h.Ungrant(a, str(req, "consumer"), str(req, "grant")); err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{})
	case "revoke":
		if err := h.RevokeConsumer(a, str(req, "consumer")); err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{})
	case "declare":
		var in home.DeclareInput
		b, _ := json.Marshal(req["input"])
		if err := json.Unmarshal(b, &in); err != nil {
			return refusal("bad_request", err.Error())
		}
		sheetID, evt, err := h.DeclareAuthor(a, str(req, "path"), in)
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"sheet": sheetID, "event": evt})
	case "disclose.confirm":
		d, err := h.ConfirmDisclosure(a, str(req, "hash"))
		if err != nil {
			return answerErr(err)
		}
		return okWith(map[string]any{"disclosure": d})
	case "run":
		var spec RunSpec
		b, _ := json.Marshal(req["spec"])
		if err := json.Unmarshal(b, &spec); err != nil {
			return refusal("bad_request", err.Error())
		}
		out, err := s.run(spec)
		if err != nil {
			r := answerErr(err)
			for k, v := range out {
				r[k] = v
			}
			return r
		}
		return okWith(out)
	case "close":
		// The engine is put away before the answer, not after it: an owner who
		// is told "closing" and then loses a conductor's last state has been
		// told something untrue. A failure keeps this channel open, so the
		// owner can look and ask again.
		attempt := str(req, "attempt")
		if attempt == "" {
			attempt = "home:close"
		}
		report := s.quiesceNative(attempt)
		s.mu.Lock()
		s.lastQuiesce = report
		s.mu.Unlock()
		answer := quiesceAnswer(report)
		if answer["ok"] != true {
			answer["gate_open"] = true
			answer["error"] = "gate: the engine was not put away; this home stays open so it can be"
			return answer
		}
		answer["closing"] = true
		return answer
	}
	return refusal("unknown_op", fmt.Sprintf("gate: unknown op %q", op))
}

// RunSpec is how the owner starts a program in its enclosure.
type RunSpec struct {
	Consumer    string            `json:"consumer"`
	Argv        []string          `json:"argv"`
	Dir         string            `json:"dir"`
	Env         map[string]string `json:"env,omitempty"`
	Net         string            `json:"net,omitempty"` // none | host
	ReadOnly    []string          `json:"ro,omitempty"`
	Writable    []string          `json:"rw,omitempty"`
	UnixSockets []string          `json:"unix_sockets,omitempty"`
	Wait        bool              `json:"wait"`
	Timeout     int               `json:"timeout_s,omitempty"`
	Stdin       string            `json:"stdin,omitempty"`
}

func (s *Server) run(spec RunSpec) (map[string]any, error) {
	out := map[string]any{}
	c, ok := s.home.Consumer(spec.Consumer)
	if !ok || c.Revoked {
		return out, errors.New("gate: no program in standing by that identifier")
	}
	if len(spec.Argv) == 0 || spec.Dir == "" {
		return out, errors.New("gate: a program needs a command and its own folder")
	}
	if s.Launch == nil {
		return out, errors.New("gate: this platform has no enclosure")
	}
	releaseSlot, err := s.reserveRun(spec)
	if err != nil {
		return out, err
	}
	defer releaseSlot()
	if spec.Writable == nil {
		spec.Writable = []string{spec.Dir}
	}
	cmd, err := s.Launch(spec)
	if err != nil {
		return out, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	conn, err := s.Bind(spec.Consumer)
	if err != nil {
		return out, err
	}
	// A program may print private home bytes. Keep bounded output in the
	// broker's memory, never plaintext files outside the encrypted store.
	stdout, stderr := &outputTail{}, &outputTail{}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if spec.Wait {
		cmd.Stdout, cmd.Stderr = stdout, stderr
	}
	// A descendant holding a pipe open must not indefinitely block the gate
	// after the process has exited. Such truncation is reported to the owner.
	cmd.WaitDelay = time.Second
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	cmd.ExtraFiles = []*os.File{conn}
	env := []string{}
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+spec.Env[k])
	}
	cmd.Env = append(env, "ROKH_GATE_FD=3")
	if err := cmd.Start(); err != nil {
		conn.Close()
		return out, err
	}
	conn.Close()
	out["pid"] = cmd.Process.Pid
	s.mu.Lock()
	if err := s.hostErrorLocked(); err != nil {
		s.mu.Unlock()
		stopProgram(cmd)
		cmd.Wait()
		return out, err
	}
	s.children[cmd.Process.Pid] = cmd
	s.mu.Unlock()
	releaseSlot()
	if !spec.Wait {
		out["output_capture"] = "discarded"
		go func() {
			cmd.Wait()
			s.mu.Lock()
			delete(s.children, cmd.Process.Pid)
			s.mu.Unlock()
		}()
		return out, nil
	}
	timeout := time.Duration(spec.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-waited:
	case <-time.After(timeout):
		stopProgram(cmd)
		waitErr = <-waited
		out["timed_out"] = true
	}
	s.mu.Lock()
	delete(s.children, cmd.Process.Pid)
	s.mu.Unlock()
	out["exit"] = cmd.ProcessState.ExitCode()
	out["output_capture"] = "memory_tail"
	out["stdout"], out["stdout_truncated"] = stdout.snapshot()
	out["stderr"], out["stderr_truncated"] = stderr.snapshot()
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		out["output_incomplete"] = true
	}
	return out, nil
}
