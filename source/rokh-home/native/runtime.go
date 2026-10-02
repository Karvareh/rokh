package native

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CreateInput names a fresh synthetic profile and the test network it joins.
type CreateInput struct {
	Label     string `json:"label"`
	Bootstrap string `json:"bootstrap"`
	Relay     string `json:"relay"`
}

// Create makes a new synthetic identity for this home: a random conductor
// passphrase generated here and sealed at once, and nothing on disk yet. The
// agent key itself is the conductor's to make, on the first start.
func (m *Manager) Create(in CreateInput) (*Profile, error) {
	if err := m.seal.Health(); err != nil {
		return nil, err
	}
	if in.Bootstrap == "" || in.Relay == "" {
		return nil, errors.New("native: name the test network's bootstrap and relay explicitly")
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	pass := make([]byte, 32)
	if _, err := rand.Read(pass); err != nil {
		return nil, err
	}
	p := &Profile{
		Format: ProfileFormat, ID: fmt.Sprintf("%x", id), Label: in.Label, AppID: m.bin.Manifest.App.ID,
		CreatedUTC: nowUTC(), Generation: 1,
		ConductorPassphrase: []byte(base64.StdEncoding.EncodeToString(pass)),
		Bootstrap:           in.Bootstrap, Relay: in.Relay,
	}
	if err := m.saveProfile(p); err != nil {
		return nil, err
	}
	return p, m.addToIndex(p.ID)
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// session is one live conductor this process owns.
//
// calls, draining and stopping are read and written under the manager's lock
// alone, and every change to them broadcasts on its condition. That is what
// makes a drain mean something: work that arrives while a stop is running finds
// draining set and is turned away, instead of being counted after the wait has
// already passed.
type session struct {
	cmd    *exec.Cmd
	log    *os.File
	lock   *os.File
	bridge *bridge
	dir    string
	port   int
	origin string

	// calls is the number of zome calls out on this session now.
	calls int
	// draining is set before a stop waits for those calls. No new work is taken
	// once it is set.
	draining bool
	// stopping is set by the one call that is stopping this session, so a second
	// stop waits for it rather than waiting on the same process twice and
	// closing the same files twice.
	stopping bool
	// release gives back the host slot this conductor holds.
	release func()
}

// StartInput asks for a conductor on a runtime path this call owns.
type StartInput struct {
	Profile    string `json:"profile"`
	RuntimeDir string `json:"runtime_dir"`
	Origin     string `json:"origin"`
	WaitSec    int    `json:"wait_s"`
	// Bootstrap and Relay override the profile's test network for this run
	// only. The identity is the same one; what changes is whether there is a
	// network for it to reach, which is how the absent case is exercised.
	Bootstrap string `json:"bootstrap,omitempty"`
	Relay     string `json:"relay,omitempty"`
}

// Start brings a profile's conductor up on a runtime path.
//
// The guards are here, in the path the gate actually takes, not in a caller's
// bookkeeping: a live conductor for this identity, or a runtime folder a later
// checkpoint retired, refuses before anything is started.
func (m *Manager) Start(in StartInput) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.seal.Health(); err != nil {
		return nil, err
	}
	if m.closing {
		return nil, ErrClosing
	}
	// The host's ceiling is asked before anything is made, opened or written.
	// A host that offers no network, or has no slot left, refuses a conductor
	// here — where the refusal costs nothing — rather than after a runtime
	// folder, a lock and a process exist.
	commit, releaseSlot := func() {}, func() {}
	if m.ceiling.Take != nil {
		c, r, err := m.ceiling.Take(in.Profile)
		if err != nil {
			return nil, err
		}
		commit, releaseSlot = c, r
	}
	slotHeld := true
	dropSlot := func() {
		if slotHeld {
			slotHeld = false
			releaseSlot()
		}
	}
	defer dropSlot()
	engine := m.Offer()
	if !engine.Present {
		return map[string]any{"engine": engine}, ErrNoEngine
	}
	if !engine.Compatible {
		return map[string]any{"engine": engine}, fmt.Errorf("%w: %s", ErrIncompatible, engine.Reason)
	}
	p, err := m.loadProfile(in.Profile)
	if err != nil {
		return nil, err
	}
	if s := m.live[p.ID]; s != nil && s.cmd.ProcessState == nil {
		return map[string]any{"live": p.Live}, ErrLive
	}
	if p.Live != nil && processAlive(p.Live.PID) {
		return map[string]any{"live": p.Live}, fmt.Errorf("%w: pid %d is still running", ErrLive, p.Live.PID)
	}
	dir, err := filepath.Abs(in.RuntimeDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	// A runtime folder carries the generation it belongs to. A folder left over
	// from before a checkpoint names an older generation, and resuming it would
	// fork one identity across two chains.
	st, found, err := readStamp(dir)
	if err != nil {
		return nil, err
	}
	if found {
		if st.Profile != p.ID {
			return map[string]any{"stamp": st}, fmt.Errorf("native: %s belongs to profile %s", dir, st.Profile)
		}
		if st.Generation != p.Generation {
			return map[string]any{"stamp": st, "generation": p.Generation},
				fmt.Errorf("%w: the folder is at generation %d and this identity is at %d",
					ErrStale, st.Generation, p.Generation)
		}
	}
	// A lock file on the runtime folder catches a second broker that never saw
	// this home's sealed record at all.
	lock, err := holdRuntime(dir)
	if err != nil {
		return map[string]any{"runtime_dir": dir}, fmt.Errorf("%w: %v", ErrLive, err)
	}
	release := func() { lock.Close() }

	port, err := freePort()
	if err != nil {
		release()
		return nil, err
	}
	origin := in.Origin
	if origin == "" {
		origin = "rokh-native-" + p.ID
	}
	network := p
	if in.Bootstrap != "" || in.Relay != "" {
		copyOf := *p
		if in.Bootstrap != "" {
			copyOf.Bootstrap = in.Bootstrap
		}
		if in.Relay != "" {
			copyOf.Relay = in.Relay
		}
		network = &copyOf
	}
	if err := writeConductorConfig(dir, network, port, origin); err != nil {
		release()
		return nil, err
	}
	if err := writeStamp(dir, stamp{Format: RuntimeFormat, Profile: p.ID, Generation: p.Generation,
		AppID: p.AppID, WrittenUTC: nowUTC()}); err != nil {
		release()
		return nil, err
	}

	logPath := filepath.Join(dir, "conductor.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		release()
		return nil, err
	}
	cmd := exec.Command(m.bin.Conductor, "--config-path", filepath.Join(dir, "conductor.yaml"), "--piped")
	// The passphrase goes down a pipe. It is not an argument and not in the
	// environment, and the environment this child gets is emptied of both.
	cmd.Stdin = bytes.NewReader(append(append([]byte(nil), p.ConductorPassphrase...), '\n'))
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf.Close()
		release()
		return nil, err
	}
	s := &session{cmd: cmd, log: logf, lock: lock, dir: dir, port: port, origin: origin}
	wait := time.Duration(in.WaitSec) * time.Second
	if wait <= 0 {
		wait = 180 * time.Second
	}
	ready, probe := m.waitReady(cmd, port, origin, dir, wait)
	if !ready {
		m.tearDown(s)
		return map[string]any{"admin_port": port, "log_tail": tailFile(logPath, 4000), "probe": probe},
			errors.New("native: the conductor did not answer its admin interface")
	}

	installed, err := m.ensureApp(dir, port, origin, p)
	if err != nil {
		m.tearDown(s)
		return map[string]any{"admin_port": port, "log_tail": tailFile(logPath, 4000)}, err
	}

	br, opened, err := m.openBridge(p, port, origin, dir)
	if err != nil {
		m.tearDown(s)
		return map[string]any{"admin_port": port, "install": installed}, err
	}
	s.bridge = br

	p.Live = &Live{PID: cmd.Process.Pid, RuntimeDir: dir, Generation: p.Generation,
		AdminPort: port, StartedUTC: nowUTC(), Origin: origin}
	p.RuntimeDir, p.Quiesced = dir, false
	p.Engine = engine.ConductorVersion
	if err := m.saveProfile(p); err != nil {
		m.tearDown(s)
		return nil, err
	}
	// The conductor is up and this profile owns it: the slot it reserved is now
	// held for as long as it runs, and given back by the stop that ends it.
	slotHeld = false
	commit()
	profileID := p.ID
	s.release = func() {
		if m.ceiling.Give != nil {
			m.ceiling.Give(profileID)
		}
	}
	m.live[p.ID] = s
	out := map[string]any{"profile": p.View(), "engine": engine, "install": installed,
		"admin_port": port, "app_port": opened["app_port"], "pid": cmd.Process.Pid,
		"runtime_dir": dir, "credentials_new": opened["credentials_new"],
		"network":      map[string]any{"bootstrap": network.Bootstrap, "relay": network.Relay},
		"secrets_path": "conductor passphrase on a pipe; signing credentials sealed in the home"}
	return out, nil
}

func (m *Manager) waitReady(cmd *exec.Cmd, port int, origin, dir string, within time.Duration) (bool, map[string]any) {
	deadline := time.Now().Add(within)
	var last map[string]any
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil {
			return false, map[string]any{"exited": true}
		}
		if portOpen(port) {
			out, err := m.admin(dir, port, origin, "list-apps")
			last = map[string]any{"list_apps_exit": exitOf(err), "stdout": trim(out, 400)}
			if err == nil {
				return true, last
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	return false, last
}

// admin runs one `hc client call` admin request. Admin requests carry no
// secret: they name ports, app identifiers and file paths only.
func (m *Manager) admin(dir string, port int, origin string, args ...string) (string, error) {
	argv := append([]string{"client", "call", "--port", strconv.Itoa(port), "--origin", origin}, args...)
	cmd := exec.Command(m.bin.CLI, argv...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("hc %s: %v: %s", strings.Join(args, " "), err, trim(errb.String(), 400))
	}
	return out.String(), nil
}

// ensureApp installs and enables the shipped hApp if this runtime has not got
// it yet, and reads back the identity the conductor made.
func (m *Manager) ensureApp(dir string, port int, origin string, p *Profile) (map[string]any, error) {
	out := map[string]any{}
	known := p.AgentKey
	listed, err := m.admin(dir, port, origin, "list-apps")
	if err != nil {
		return out, err
	}
	if !strings.Contains(listed, p.AppID) {
		installed, err := m.admin(dir, port, origin, "install-app", "--app-id", p.AppID, m.bin.HApp)
		if err != nil {
			return out, err
		}
		out["installed"] = true
		if agent, dna, ok := readInstall(installed); ok {
			p.AgentKey, p.DNAHash = agent, dna
		}
		if _, err := m.admin(dir, port, origin, "enable-app", p.AppID); err != nil {
			return out, err
		}
		out["enabled"] = true
		p.HAppSHA256, _ = fileSHA(m.bin.HApp)
	} else {
		out["installed"] = false
		// A restored runtime lists the app, and the app may be listed
		// disabled: its cell then answers CellDisabled. It is enabled as it
		// is, with its own identity and chain; nothing is installed again.
		status := appStatus(listed, p.AppID)
		out["status"] = status
		if needsEnabling(status) {
			if _, err := m.admin(dir, port, origin, "enable-app", p.AppID); err != nil {
				return out, err
			}
			out["enabled"] = true
		}
	}
	if p.AgentKey == "" || p.DNAHash == "" {
		listed, err = m.admin(dir, port, origin, "list-apps")
		if err != nil {
			return out, err
		}
		if agent, dna, ok := readInstall(listed); ok {
			p.AgentKey, p.DNAHash = agent, dna
		}
	}
	if p.AgentKey == "" || p.DNAHash == "" {
		return out, errors.New("native: the conductor did not report this app's cell")
	}
	// An empty runtime folder would have the conductor make a brand new agent,
	// which is a different identity wearing this profile's name. That is the
	// one thing a start must never do quietly.
	if known != "" && known != p.AgentKey {
		return map[string]any{"expected_agent": known, "found_agent": p.AgentKey},
			fmt.Errorf("native: %s holds identity %s, and this profile is %s; restore the checkpoint instead",
				dir, p.AgentKey, known)
	}
	out["agent_key"], out["dna_hash"] = p.AgentKey, p.DNAHash
	return out, nil
}

// appStatus reads one app's status out of a list-apps answer: the "status"
// of the entry whose installed_app_id is the app, as a word ("running",
// "enabled", "disabled", "paused"), or "" when the answer does not say.
func appStatus(jsonText, appID string) string {
	var v any
	if err := json.NewDecoder(strings.NewReader(stripBanner(jsonText))).Decode(&v); err != nil {
		return ""
	}
	found := ""
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if n["installed_app_id"] == appID && found == "" {
				switch s := n["status"].(type) {
				case string:
					found = strings.ToLower(s)
				case map[string]any:
					if t, ok := s["type"].(string); ok {
						found = strings.ToLower(t)
					} else {
						for k := range s { // {"Disabled": {...}}
							found = strings.ToLower(k)
						}
					}
				}
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(v)
	return found
}

// needsEnabling says whether a listed app must be enabled before its cell
// answers: anything but running or enabled, an unknown status included.
func needsEnabling(status string) bool {
	return status != "running" && status != "enabled"
}

// readInstall digs the agent key and DNA hash out of an install-app or
// list-apps answer without depending on the exact shape of the rest.
func readInstall(jsonText string) (string, string, bool) {
	var v any
	dec := json.NewDecoder(strings.NewReader(stripBanner(jsonText)))
	if err := dec.Decode(&v); err != nil {
		return "", "", false
	}
	agent, dna := "", ""
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if a, ok := n["agent_pub_key"].(string); ok && agent == "" {
				agent = a
			}
			if cell, ok := n["cell_id"].(map[string]any); ok {
				if d, ok := cell["dna_hash"].(string); ok && dna == "" {
					dna = d
				}
				if a, ok := cell["agent_pub_key"].(string); ok && agent == "" {
					agent = a
				}
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(v)
	return agent, dna, agent != "" && dna != ""
}

// The CLI prints a logging banner before its JSON.
func stripBanner(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "Initialising log") {
			continue
		}
		idx := strings.Index(s, line)
		return s[idx:]
	}
	return s
}

// Status is what the broker knows about a profile now, with no secrets in it.
func (m *Manager) Status(id string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.loadProfile(id)
	if err != nil {
		return nil, err
	}
	out := p.View()
	out["engine"] = m.Offer()
	running := false
	if p.Live != nil {
		running = processAlive(p.Live.PID)
		out["pid_alive"] = running
	}
	out["running"] = running
	if s := m.live[id]; s != nil {
		out["owned_by_this_broker"] = true
		out["bridge"] = s.bridge != nil
	}
	if p.Snapshot != nil {
		out["restorable"] = p.Snapshot.Complete && p.Snapshot.Generation == p.Generation
	}
	return out, nil
}

// Stop quiesces a conductor: no new work, then the work already out, then an
// interrupt and its own exit. The keys are not dropped here — a checkpoint
// still has to be able to read this profile.
func (m *Manager) Stop(id string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked(id)
}

// stopLocked is Stop with the manager's lock already held. The lock is released
// only where this function says so, and every release happens with draining and
// stopping already set, so nothing can arrive behind the drain.
func (m *Manager) stopLocked(id string) (map[string]any, error) {
	// A stop already under way is waited for, not run a second time: two stops
	// on one session would wait on one process twice and close its files twice.
	// When the first finishes, this one finds the session gone and says so.
	for {
		s := m.live[id]
		if s == nil || !s.stopping {
			break
		}
		m.cond.Wait()
	}
	p, err := m.loadProfile(id)
	if err != nil {
		return nil, err
	}
	s := m.live[id]
	out := map[string]any{"profile": id}
	if s == nil {
		if p.Live == nil || !processAlive(p.Live.PID) {
			p.Live = nil
			if err := m.saveProfile(p); err != nil {
				return out, err
			}
			out["already_stopped"] = true
			return out, nil
		}
		return map[string]any{"live": p.Live}, fmt.Errorf(
			"native: pid %d is live but was not started by this broker; stop it where it was started", p.Live.PID)
	}
	// From here on this session takes no new work and this call owns the stop.
	// Both are set under the lock before anything waits, which is the whole
	// difference: a drain that does not first close the door is only a pause.
	s.draining, s.stopping = true, true
	m.cond.Broadcast()
	defer func() {
		s.stopping = false
		m.cond.Broadcast()
	}()
	// Everything that has left this process is waited for. Reading the head
	// before that would name a head that a call still in flight is about to
	// move, and a checkpoint taken from it would be a checkpoint of a chain that
	// had already gone further.
	for s.calls > 0 {
		m.cond.Wait()
	}
	out["drained"] = true
	// Read the chain head while the conductor is still answering. A checkpoint
	// taken afterwards has to name the head it is a checkpoint of, and after the
	// interrupt there is nothing left to ask.
	//
	// The flag is cleared first. Leaving the last stop's answer standing would
	// let a checkpoint name a head this conductor was never asked for.
	p.HeadRead = false
	if br := s.bridge; br != nil {
		m.mu.Unlock()
		answer, callErr := br.call(map[string]any{"op": "zome", "zome": m.bin.Manifest.App.Zome,
			"fn": m.bin.Manifest.Functions.ChainHead, "payload": nil}, 60*time.Second)
		m.mu.Lock()
		if callErr != nil {
			// A head that could not be read is said so. A checkpoint that
			// cannot name its head is worth less, and pretending is worse.
			out["head_error"] = callErr.Error()
		} else if head, seq, action, ok := readHead(answer); !ok {
			out["head_error"] = "the conductor's answer carried no seq and action to read a head from"
			out["head_answer_shape"] = head
		} else {
			p.LastHead = ChainHead{Seq: seq, Action: action}
			p.HeadRead = true
			out["head"] = p.LastHead
		}
		br.close()
		s.bridge = nil
	}
	out["head_read"] = p.HeadRead
	start := time.Now()
	out["signal"] = "SIGINT"
	_ = s.cmd.Process.Signal(syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		out["signal"] = "SIGINT, then SIGKILL after 120s"
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
	out["exit"] = s.cmd.ProcessState.ExitCode()
	out["seconds"] = time.Since(start).Seconds()
	out["pid_alive_after"] = processAlive(s.cmd.Process.Pid)
	s.log.Close()
	s.lock.Close()
	if s.release != nil {
		// The host slot this conductor held goes back now, whether or not the
		// ending was a clean one: the process is gone either way.
		s.release()
		s.release = nil
	}
	delete(m.live, id)
	m.cond.Broadcast()
	// The ending is only consistent when the conductor exited by itself after
	// the interrupt. Anything else is reported as that, not as a clean stop.
	consistent := out["exit"] == 0 && out["pid_alive_after"] == false && out["signal"] == "SIGINT"
	out["consistent"] = consistent
	out["runtime_dir"] = s.dir
	p.Live = nil
	p.Quiesced = consistent
	p.LastStop = map[string]any{"exit": out["exit"], "signal": out["signal"],
		"pid_alive_after": out["pid_alive_after"], "consistent": consistent, "at": nowUTC()}
	if err := m.saveProfile(p); err != nil {
		return out, err
	}
	if !consistent {
		return out, ErrUnknownEnding
	}
	return out, nil
}

// readHead takes a chain head out of a bridge answer, and says plainly when
// there is none to take.
//
// A partial or misshapen answer is not a head. Round three read whatever was
// there and left the previous head standing when it found nothing, which meant
// a checkpoint could name a head that this conductor was never asked for and
// nobody ever saw.
func readHead(answer map[string]any) (shape any, seq uint32, action string, ok bool) {
	result := answer["result"]
	head, isMap := result.(map[string]any)
	if !isMap {
		return result, 0, "", false
	}
	raw, hasSeq := head["seq"].(float64)
	act := holoB64(head["action"])
	if !hasSeq || math.IsNaN(raw) || raw < 0 || raw > math.MaxUint32 || math.Trunc(raw) != raw || act == "" {
		return head, 0, "", false
	}
	return head, uint32(raw), act, true
}

// holoB64 turns the byte array a zome answer carries into the 'u…' form the
// conductor's own logs and admin answers use.
func holoB64(v any) string {
	items, ok := v.([]any)
	if !ok || len(items) != 39 {
		return ""
	}
	b := make([]byte, 0, len(items))
	for _, it := range items {
		f, ok := it.(float64)
		if !ok || math.IsNaN(f) || f < 0 || f > 255 || math.Trunc(f) != f {
			return ""
		}
		b = append(b, byte(int(f)))
	}
	return "u" + base64.RawURLEncoding.EncodeToString(b)
}

// tearDown stops a session that never became usable.
func (m *Manager) tearDown(s *session) {
	if s.bridge != nil {
		s.bridge.close()
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGINT)
		done := make(chan struct{})
		go func() { s.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}
	s.log.Close()
	s.lock.Close()
}

// Quiesce is how a home lets go of the engine: no new native work, then the
// work already in flight, then a consistent stop, then the last state sealed —
// in that order, and the whole of it before any key is zeroed.
//
// It reports what happened for every profile and says plainly whether all of it
// succeeded. A stop that was not consistent, or a checkpoint that did not seal,
// is not a closed home: the caller keeps the channel and the runtime folder, so
// the owner can look and try again.
func (m *Manager) Quiesce(attempt string) map[string]any {
	m.mu.Lock()
	if q := m.quiescing; q != nil {
		// Somebody is already putting the engine away. Joining it is the only
		// honest answer: returning an empty success here is how a second close
		// could shut a home while the first was still stopping a conductor, or
		// had already found that it could not be sealed.
		m.mu.Unlock()
		<-q.done
		joined := map[string]any{"joined": true}
		for k, v := range q.report {
			joined[k] = v
		}
		return joined
	}
	q := &putAway{done: make(chan struct{})}
	m.quiescing = q
	// Set before the sessions are read, so a start that is still deciding
	// refuses rather than appearing after this list was taken.
	m.closing = true
	m.mu.Unlock()

	report := m.putEngineAway(attempt)

	m.mu.Lock()
	q.report = report
	m.quiescing = nil
	if report["ok"] != true {
		// A failed put-away must not leave the manager refusing everything: the
		// owner still has to be able to stop, checkpoint or restart by hand.
		m.closing = false
	}
	m.mu.Unlock()
	close(q.done)
	return report
}

// putEngineAway is the body of one Quiesce. Only one runs at a time.
func (m *Manager) putEngineAway(attempt string) map[string]any {
	report := map[string]any{"format": "rokh.native-quiesce/1", "attempt": attempt, "started_utc": nowUTC()}
	rows := []map[string]any{}
	ids, scanErr := m.toPutAway()
	allOK := scanErr == nil
	if scanErr != nil {
		report["scan_error"] = scanErr.Error()
	}
	for _, id := range ids {
		row := map[string]any{"profile": id}
		stop, err := m.Stop(id)
		row["stop"] = stop
		if err != nil {
			row["stop_error"], row["ok"] = err.Error(), false
			allOK = false
			rows = append(rows, row)
			continue
		}
		if attempt == "" {
			row["checkpoint"] = "not asked for"
			row["ok"] = true
			rows = append(rows, row)
			continue
		}
		p, loadErr := m.profile(id)
		if loadErr != nil {
			row["checkpoint_error"], row["ok"] = loadErr.Error(), false
			allOK = false
			rows = append(rows, row)
			continue
		}
		if !pendingSeal(p) {
			// Nothing on disk that the sealed snapshot does not already stand
			// for. Sealing again would be a second name for one state.
			row["checkpoint"] = map[string]any{"already_sealed": true,
				"object": snapField(p.Snapshot, "object"), "head": snapHead(p.Snapshot)}
			row["ok"] = true
			rows = append(rows, row)
			continue
		}
		// The attempt names the state it seals: the generation it was taken
		// from and the head it stopped at. A second try after a failure that
		// reached the same head reuses the same name and therefore the same
		// object; a different head is honestly a different attempt.
		head := p.LastHead.Action
		if len(head) > 16 {
			head = head[:16]
		}
		named := fmt.Sprintf("%s:g%d:%s", attempt, p.Generation, head)
		row["attempt"] = named
		cp, err := m.Checkpoint(id, named)
		if err != nil {
			row["checkpoint_error"], row["ok"] = err.Error(), false
			if cp != nil {
				row["checkpoint"] = cp
			}
			allOK = false
			rows = append(rows, row)
			continue
		}
		snap, _ := cp["snapshot"].(*Snapshot)
		row["checkpoint"] = map[string]any{"generation": cp["generation"], "already": cp["already"],
			"object": snapField(snap, "object"), "sha256": snapField(snap, "sha256"),
			"head": snapHead(snap), "retired_runtime": cp["retired_runtime"]}
		row["ok"] = true
		rows = append(rows, row)
	}
	report["profiles"], report["ok"], report["finished_utc"] = rows, allOK, nowUTC()
	if len(rows) == 0 && allOK {
		report["nothing_to_put_away"] = true
	}
	if !allOK {
		report["note"] = "the engine was not put away cleanly; the runtime folders are kept so this can be looked at and tried again"
	}
	return report
}

// toPutAway names every identity this put-away has to deal with: the conductors
// this process is running, and every identity whose state on disk the sealed
// snapshot does not stand for — including ones an earlier attempt stopped and
// failed to seal, and ones left behind by a broker that has since been reopened.
func (m *Manager) toPutAway() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for id := range m.live {
		seen[id] = true
	}
	ids, err := m.index()
	if err == nil {
		for _, id := range ids {
			if seen[id] {
				continue
			}
			p, loadErr := m.loadProfile(id)
			if loadErr != nil {
				// An unreadable profile still needs attention. Let Stop name the
				// failure instead of silently treating it as sealed.
				seen[id] = true
				continue
			}
			if pendingSeal(p) {
				seen[id] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, err
}

// profile reads one sealed profile record under the manager's lock.
func (m *Manager) profile(id string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadProfile(id)
}

func snapField(s *Snapshot, field string) any {
	if s == nil {
		return nil
	}
	switch field {
	case "object":
		return s.Object
	case "sha256":
		return s.SHA256
	}
	return nil
}

func snapHead(s *Snapshot) any {
	if s == nil {
		return nil
	}
	return s.Head
}

// CloseAll stops every conductor this broker started, without sealing anything.
// Quiesce is the way a home closes; this is the last resort behind it.
func (m *Manager) CloseAll() []map[string]any {
	m.mu.Lock()
	m.closing = true
	ids := make([]string, 0, len(m.live))
	for id := range m.live {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	sort.Strings(ids)
	out := []map[string]any{}
	for _, id := range ids {
		r, err := m.Stop(id)
		if err != nil {
			if r == nil {
				r = map[string]any{"profile": id}
			}
			r["error"] = err.Error()
			// The last resort must also work when the sealed profile is
			// unreadable. We can stop only our own process; no checkpoint or
			// clean ending is claimed for this fallback.
			m.mu.Lock()
			for s := m.live[id]; s != nil && s.stopping; s = m.live[id] {
				m.cond.Wait()
			}
			if s := m.live[id]; s != nil {
				s.draining = true
				for s.calls > 0 {
					m.cond.Wait()
				}
				m.tearDown(s)
				if s.release != nil {
					s.release()
					s.release = nil
				}
				delete(m.live, id)
				r["fallback_stop"] = true
				r["pid_alive_after"] = processAlive(s.cmd.Process.Pid)
				r["consistent"] = false
				m.cond.Broadcast()
			}
			m.mu.Unlock()
		}
		out = append(out, r)
	}
	return out
}

// ---------- host bits ----------

func privateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("native: %s is not a folder", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("native: %s belongs to another user", dir)
	}
	return nil
}

func holdRuntime(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another process holds %s", filepath.Join(dir, lockFile))
	}
	return f, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func portOpen(port int) bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func exitOf(err error) any {
	if err == nil {
		return 0
	}
	return err.Error()
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func tailFile(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	if fi.Size() > n {
		if _, err := f.Seek(fi.Size()-n, 0); err != nil {
			return ""
		}
	}
	b, _ := os.ReadFile(path)
	if int64(len(b)) > n {
		b = b[int64(len(b))-n:]
	}
	return string(b)
}

// writeConductorConfig writes the conductor's own configuration. It holds
// ports, paths and the test network's explicit addresses — and no secret.
func writeConductorConfig(dir string, p *Profile, port int, origin string) error {
	cfg := map[string]any{
		"data_root_path": filepath.Join(dir, "data"),
		"keystore": map[string]any{
			"type":      "lair_server_in_proc",
			"lair_root": filepath.Join(dir, "ks"),
		},
		"admin_interfaces": []any{map[string]any{
			"driver": map[string]any{"type": "websocket", "port": port,
				"allowed_origins": origin},
		}},
		"network": map[string]any{
			"bootstrap_url":     p.Bootstrap,
			"relay_url":         p.Relay,
			"request_timeout_s": 30,
			"report":            "none",
			"advanced": map[string]any{
				"irohTransport": map[string]any{"relayAllowPlainText": true},
				"k2Gossip": map[string]any{"initialInitiateIntervalMs": 2000, "initiateIntervalMs": 5000,
					"minInitiateIntervalMs": 1000, "initiateJitterMs": 500},
				"coreBootstrap": map[string]any{"backoffMinMs": 1000, "backoffMaxMs": 5000},
			},
		},
	}
	b, err := json.MarshalIndent(cfg, "", " ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "conductor.yaml"), append(b, '\n'))
}
