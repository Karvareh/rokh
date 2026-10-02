package gate

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"rokh-home/authority"
	"rokh-home/home"
	"rokh-home/native"
)

// nativeNamespace is where a publication's receipts are written: a namespace
// of its own, named by the engine manifest, never the home's own address and
// never the ledger root. A program needs an explicit ledger grant over it, and
// that grant is a separate thing from the share and bytes it needs over the
// item.
func (s *Server) nativeNamespace() string {
	if m := s.nativeManager(); m != nil {
		return m.Manifest().Namespace
	}
	return ""
}

// EnableNative gives this gate a runtime manager over the host's engine. A
// gate without one answers every native op with "no engine here", which is a
// named answer and not a failure.
func (s *Server) EnableNative(bin native.Binaries) {
	m := native.New(s.home.Sealed(), bin)
	// A conductor is counted against the same host offer a program is, and the
	// manager asks before it makes, opens or writes anything. The ceiling lives
	// here because the offer does; the manager holds none of its own.
	m.UnderCeiling(native.Ceiling{
		Take: func(profile string) (func(), func(), error) { return s.reserveConductor(profile) },
		Give: func(profile string) { s.releaseConductor(profile) },
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.native = m
}

// nativeAnswer turns the manager's named errors into the gate's typed codes,
// so a caller can tell "no engine", "already live" and "stale" apart from a
// plain failure without reading prose.
func nativeAnswer(out map[string]any, err error) map[string]any {
	if err == nil {
		return okWith(out)
	}
	code := "failed"
	switch {
	case errors.Is(err, native.ErrNoEngine):
		code = "engine_absent"
	case errors.Is(err, native.ErrIncompatible):
		code = "engine_incompatible"
	case errors.Is(err, native.ErrLive):
		code = "already_live"
	case errors.Is(err, native.ErrNotLive):
		code = "not_live"
	case errors.Is(err, native.ErrStale):
		code = "stale_generation"
	case errors.Is(err, native.ErrNoSnapshot):
		code = "no_checkpoint"
	case errors.Is(err, native.ErrNotQuiesced):
		code = "not_quiesced"
	case errors.Is(err, native.ErrUnknownEnding):
		code = "ending_unknown"
	case errors.Is(err, native.ErrClosing):
		code = "closing"
	case errors.Is(err, native.ErrStopping):
		code = "stopping"
	case errors.Is(err, native.ErrNoConfirmation):
		code = "not_confirmed"
	}
	r := map[string]any{"ok": false, "code": code, "error": err.Error()}
	for k, v := range out {
		r[k] = v
	}
	return r
}

// nativeOwnerOp answers the owner's native ops. A program never reaches these:
// they name runtime folders, ports and checkpoints, which are the broker's.
func (s *Server) nativeOwnerOp(op string, req map[string]any) (map[string]any, bool) {
	m := s.nativeManager()
	if m == nil {
		if !isNativeOp(op) {
			return nil, false
		}
		return map[string]any{"ok": false, "code": "engine_absent",
			"error": "gate: this gate was not given a native engine; start it with --native-dir"}, true
	}
	// A reading mirror carries content and provenance, and deliberately not the
	// writer's identity or its application state. Every native op that would
	// make either of those on a mirror is refused here, not left to fail later
	// somewhere less legible.
	if s.home.Replica().Kind != home.WriterReplica && nativeWrites[op] {
		return map[string]any{"ok": false, "code": "denied", "reason": "read_only_replica",
			"error": "gate: a reading mirror does not make or hold a native identity"}, true
	}
	switch op {
	case "native.offer":
		e := m.Offer()
		return okWith(map[string]any{"engine": e, "namespace": s.nativeNamespace(),
			"data_authority": "the owner's separate share, bytes and ledger grants"}), true
	case "native.profile.create":
		p, err := m.Create(native.CreateInput{Label: str(req, "label"),
			Bootstrap: str(req, "bootstrap"), Relay: str(req, "relay")})
		if err != nil {
			return nativeAnswer(nil, err), true
		}
		return okWith(map[string]any{"profile": p.View()}), true
	case "native.profile.list":
		list, err := m.List()
		if err != nil {
			return nativeAnswer(nil, err), true
		}
		return okWith(map[string]any{"profiles": list}), true
	case "native.start":
		// The ceiling is inside m.Start, which asks this gate before it makes a
		// folder, takes a lock or runs a process. A host that was withdrawn, has
		// expired, offers no network or has no slot left answers here, and the
		// runtime path named in the request is untouched.
		out, err := m.Start(native.StartInput{Profile: str(req, "profile"),
			RuntimeDir: str(req, "runtime_dir"), Origin: str(req, "origin"),
			WaitSec: int(num(req, "wait_s")), Bootstrap: str(req, "bootstrap"), Relay: str(req, "relay")})
		if err != nil {
			var hosting *HostingError
			if errors.As(err, &hosting) {
				r := answerErr(err)
				r["hosting"] = s.Hosting()
				r["before_any_effect"] = true
				return r, true
			}
		}
		return nativeAnswer(out, err), true
	case "native.status":
		out, err := m.Status(str(req, "profile"))
		return nativeAnswer(out, err), true
	case "native.stop":
		out, err := m.Stop(str(req, "profile"))
		return nativeAnswer(out, err), true
	case "native.hosting":
		return okWith(map[string]any{"hosting": s.Hosting()}), true
	case "native.checkpoint":
		out, err := m.Checkpoint(str(req, "profile"), str(req, "attempt"))
		return nativeAnswer(out, err), true
	case "native.restore":
		out, err := m.Restore(str(req, "profile"), str(req, "runtime_dir"))
		return nativeAnswer(out, err), true
	case "native.observe":
		var payload any
		if raw, ok := req["payload"]; ok {
			payload = raw
		}
		out, err := m.Observe(str(req, "profile"), str(req, "fn"), payload)
		if err != nil {
			return nativeAnswer(nil, err), true
		}
		return okWith(map[string]any{"result": out}), true
	case "native.assess":
		strings := func(key string) []string {
			var out []string
			if raw, ok := req[key].([]any); ok {
				for _, n := range raw {
					if s, ok := n.(string); ok {
						out = append(out, s)
					}
				}
			}
			return out
		}
		needles, controls, absent := strings("needles"), strings("controls"), strings("absent_controls")
		probe := req["database_probe"] == true
		if dir := str(req, "runtime_dir"); dir != "" {
			a, err := native.AssessDir(dir, needles, controls, absent, probe)
			return nativeAnswer(map[string]any{"assessment": a}, err), true
		}
		a, err := m.Assess(str(req, "profile"), needles, controls, absent, probe)
		if err != nil {
			return nativeAnswer(nil, err), true
		}
		return okWith(map[string]any{"assessment": a}), true
	case "native.confirm":
		// One decision, not two: what leaves the home and which identity signs
		// it. The identity is read from the profile now and sealed with the
		// disclosure's hash; the program named in it is the one that prepared
		// the preview, and no other may spend it.
		hash := str(req, "hash")
		d, err := s.home.ConfirmDisclosure(home.OwnerActor, hash)
		if err != nil {
			return answerErr(err), true
		}
		c, err := m.Confirm(hash, str(req, "profile"), d.By, d.Path, d.Version)
		if err != nil {
			return nativeAnswer(map[string]any{"disclosure": d}, err), true
		}
		return okWith(map[string]any{"confirmation": c, "disclosure": map[string]any{
			"hash": d.Hash, "path": d.Path, "version": d.Version, "state": d.State,
			"body_sha256": d.BodySHA256, "excerpt_sha256": d.ExcerptSHA,
			"recipient": d.Recipient, "network": d.Network}}), true
	case "native.quiesce":
		// A put-away that did not put the engine away is not an ok answer. Round
		// three wrapped the report in okWith whatever it said, so an owner who
		// asked to quiesce and lost a conductor's last state was told yes, and
		// the CLI exited zero on it.
		report := m.Quiesce(str(req, "attempt"))
		s.mu.Lock()
		s.lastQuiesce = report
		s.mu.Unlock()
		return quiesceAnswer(report), true
	case "native.peer":
		// The acts a reviewing home performs on its own chain about content a
		// peer disclosed to it: the standing word about which claims it will
		// take, the deliberate lending of one it holds, the fetch that presents
		// it, and the review it writes. All of them are the owner's, never a
		// program's, and each is named here rather than being a door onto the
		// whole zome.
		var payload any
		if raw, ok := req["payload"]; ok {
			payload = raw
		}
		out, err := m.Peer(str(req, "profile"), str(req, "fn"), payload)
		if err != nil {
			return nativeAnswer(map[string]any{"fn": str(req, "fn")}, err), true
		}
		return okWith(map[string]any{"result": out, "fn": str(req, "fn")}), true
	case "native.bridge.kill":
		out, err := m.KillBridge(str(req, "profile"))
		return nativeAnswer(out, err), true
	case "native.bridge.reopen":
		out, err := m.Reopen(str(req, "profile"), req["reauthorize"] == true)
		return nativeAnswer(out, err), true
	case "native.capability":
		var payload any
		if raw, ok := req["payload"]; ok {
			payload = raw
		}
		out, err := m.Capability(str(req, "profile"), str(req, "fn"), payload)
		if err != nil {
			return nativeAnswer(nil, err), true
		}
		return okWith(map[string]any{"result": out}), true
	case "native.admin":
		var args []string
		if raw, ok := req["args"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					args = append(args, s)
				}
			}
		}
		out, err := m.Admin(str(req, "profile"), args)
		if err != nil {
			return nativeAnswer(map[string]any{"stdout": out}, err), true
		}
		return okWith(map[string]any{"stdout": out, "args": args}), true
	}
	return nil, false
}

func isNativeOp(op string) bool {
	return len(op) > 7 && op[:7] == "native."
}

// nativeWrites are the ops that would make or move a native identity or its
// state. A reading mirror answers none of them.
var nativeWrites = map[string]bool{
	"native.profile.create": true, "native.start": true, "native.checkpoint": true,
	"native.restore": true, "native.capability": true, "native.confirm": true,
	"native.bridge.reopen": true, "native.publish": true, "native.quiesce": true,
	"native.peer": true,
}

// nativeProgramOp answers the two native ops a program may speak. Everything
// else under native. is the owner's.
//
// Publishing is decided twice over and by two different rights: the home
// decides share and bytes on the item, again at the moment the body is handed
// over, and the ledger decides the program's grant over this namespace. A
// capability the conductor holds is neither of those, and grants nothing here.
func (s *Server) nativeProgramOp(a home.Actor, op string, req map[string]any) (map[string]any, bool) {
	if op != "native.publish" && op != "native.attempt" {
		if isNativeOp(op) {
			return map[string]any{"ok": false, "code": "denied", "reason": "owner_only",
				"error": fmt.Sprintf("gate: %q is not an op a program speaks", op)}, true
		}
		return nil, false
	}
	m := s.nativeManager()
	if m == nil {
		return map[string]any{"ok": false, "code": "engine_absent", "record": "not-recorded",
			"error": "gate: this gate was not given a native engine"}, true
	}
	namespace := str(req, "namespace")
	if namespace == "" {
		namespace = s.nativeNamespace()
	}
	// The owner's binding decides both the identity and which program may spend
	// it. Neither is taken from the request: a program that could name either
	// could publish an approved preview under another identity, or spend
	// somebody else's confirmation.
	hash := str(req, "hash")
	confirmed, err := m.Confirmation(hash)
	if err != nil {
		return nativeAnswer(map[string]any{"record": "not-recorded", "stage": "confirmation"}, err), true
	}
	caller := a.Consumer
	if a.Owner {
		caller = home.OwnerID
	}
	if confirmed.Consumer != caller {
		return map[string]any{"ok": false, "code": "denied", "reason": "not_your_confirmation",
			"record": "not-recorded", "stage": "confirmation",
			"error": "gate: this confirmation belongs to another program"}, true
	}
	if named := str(req, "profile"); named != "" && named != confirmed.Profile {
		return map[string]any{"ok": false, "code": "denied", "reason": "identity_not_confirmed",
			"record": "not-recorded", "stage": "confirmation",
			"error": fmt.Sprintf("gate: the owner confirmed this disclosure for %s, not %s",
				confirmed.Profile, named)}, true
	}
	if err := m.CheckIdentity(confirmed); err != nil {
		return nativeAnswer(map[string]any{"record": "not-recorded", "stage": "confirmation"}, err), true
	}
	profile := confirmed.Profile
	// The attempt's name carries the asking program, so no program can name —
	// or ask about — another's attempt. The suffix is the program's own way to
	// make a second attempt of its own.
	attempt := native.AttemptFor(hash, caller, str(req, "suffix"))

	if op == "native.attempt" {
		// The read-only question a lost answer is settled with. It needs the
		// same ledger standing as the receipt it belongs to.
		if r := s.home.LedgerOp(a, map[string]any{"op": "receipts", "address": namespace}); r["ok"] != true {
			return r, true
		}
		record, found, err := m.FindAttempt(profile, attempt)
		if err != nil {
			return nativeAnswer(map[string]any{"record": "unknown", "attempt": attempt}, err), true
		}
		return okWith(map[string]any{"record": record, "found": found, "attempt": attempt}), true
	}

	// 1. The disclosure, decided now. A program whose bytes or share was
	//    withdrawn between the preview and this call collects nothing.
	d, body, err := s.home.ConfirmedDisclosure(a, hash)
	if err != nil {
		r := answerErr(err)
		r["record"] = "not-recorded"
		r["stage"] = "disclosure"
		return r, true
	}

	// 2. The intent, in the program's own namespace, before anything is sent.
	//
	// Every field an attempt is named by has to be the same on a replay. The
	// state a receipt shows is therefore the state of the thing being disclosed
	// — the version and the hash of its bytes — and not the conductor's chain
	// head, which moves with the first publication and would make the second
	// attempt a different request under the same name.
	witness := map[string]any{
		"origin":    fmt.Sprintf("%s@%d", d.Path, d.Version),
		"authority": "rokh:disclosure:" + d.Hash,
		"audience":  d.Recipient + " on " + d.Network,
		"state":     fmt.Sprintf("body sha256 %s, excerpt sha256 %s", d.BodySHA256, d.ExcerptSHA),
		"wayBack":   "none: a published action is not withdrawn; revoking closes the future only",
	}
	intent := s.home.LedgerOp(a, map[string]any{"op": "intent", "address": namespace,
		"doing":   "disclose an excerpt to a Holochain peer",
		"attempt": "intent:" + attempt,
		"witness": witness})
	if intent["ok"] != true {
		intent["stage"] = "intent"
		if _, ok := intent["record"]; !ok {
			intent["record"] = "not-recorded"
		}
		return intent, true
	}
	intentID := fmt.Sprint(intent["id"])

	// 3. The App API, with only what that preview disclosed.
	result, err := m.Publish(native.PublishInput{Profile: profile, ItemID: d.Item,
		Title: d.Path, Version: d.BodySHA256, Body: body, Excerpt: d.Excerpt,
		ExcerptSHA256: d.ExcerptSHA, Attempt: attempt, RokhIntent: intentID})
	if err != nil {
		result.Record, result.Error = "unknown", err.Error()
	}

	// 4. The outcome, whichever it was. An ending that was never learned is
	//    recorded as that, not left open and not called a success.
	saying := result.Error
	ending := "done"
	switch result.Record {
	case "recorded":
		saying = fmt.Sprintf("published %s at %s", d.ExcerptSHA[:16], result.Manuscript)
	case "unknown":
		ending = "unknown"
	default:
		ending = "failed"
	}
	outcome := s.home.LedgerOp(a, nativeOutcomeRequest(namespace, intentID, attempt, ending, saying, witness))

	answer := map[string]any{
		"record": result.Record, "native": result, "intent": intentID,
		"chain_head": s.nativeHead(m, profile),
		"outcome":    map[string]any{"record": outcome["record"], "id": outcome["id"], "ok": outcome["ok"]},
		"disclosure": map[string]any{"hash": d.Hash, "path": d.Path, "version": d.Version,
			"body_sha256": d.BodySHA256, "excerpt_sha256": d.ExcerptSHA, "recipient": d.Recipient,
			"network": d.Network, "private": d.Private, "public": d.Public},
		"attempt": attempt,
		"confirmation": map[string]any{"profile": confirmed.Profile, "agent_key": confirmed.AgentKey,
			"dna_hash": confirmed.DNAHash, "consumer": confirmed.Consumer,
			"confirmed_utc": confirmed.ConfirmedUTC},
		"authority": map[string]any{
			"home":             []string{string(authority.Share), string(authority.Bytes)},
			"ledger_namespace": namespace,
			"note":             "the conductor's own capability is not one of these and grants nothing here",
		},
	}
	return nativeCompletion(answer, result, outcome), true
}

// nativeHead reports where the source chain stands, for the answer. It is not
// part of any receipt: a witness a replay has to reproduce cannot be a value
// that moves. When the App API path is cut it says so, rather than nothing.
func (s *Server) nativeHead(m *native.Manager, profile string) string {
	out, err := m.Observe(profile, m.Manifest().Functions.ChainHead, nil)
	if err != nil {
		return "unread: " + err.Error()
	}
	head, ok := out.(map[string]any)
	if !ok {
		return "unread"
	}
	b, _ := json.Marshal(head["seq"])
	return "chain_head seq " + string(b)
}

func (s *Server) nativeManager() *native.Manager {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.native
}

// quiesceNative is how the gate puts the engine away: no new native work, then
// the work in flight, then a consistent stop, then the last state sealed —
// before any key of this home is zeroed.
//
// It answers with the whole report, including a failure. A close that could not
// put the engine away is not a close, and saying so is the point.
func (s *Server) quiesceNative(attempt string) map[string]any {
	m := s.nativeManager()
	if m == nil {
		return nil
	}
	return m.Quiesce(attempt)
}

// quiesceAnswer turns a put-away report into the gate's answer.
//
// A report that says the engine was not put away is not an ok answer, whatever
// else is in it. This is the one place that decides, so the owner op and the
// close op cannot drift apart, and so the CLI's exit code — which is read off
// this ok — says the same thing the report does.
func quiesceAnswer(report map[string]any) map[string]any {
	if report != nil && report["ok"] != true {
		return map[string]any{"ok": false, "code": "not_quiesced", "quiesce": report,
			"error": "gate: the engine was not put away; the runtime folders are kept so this can be looked at and tried again"}
	}
	return okWith(map[string]any{"quiesce": report})
}

// closeNative is the last resort behind quiesceNative: stop, seal nothing.
func (s *Server) closeNative() []map[string]any {
	m := s.nativeManager()
	if m == nil {
		return nil
	}
	return m.CloseAll()
}

var _ = base64.StdEncoding
