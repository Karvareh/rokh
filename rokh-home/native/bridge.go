package native

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// bridge is the one process that speaks the App API, over pipes.
//
// Requests and answers are JSON lines on the child's own stdin and stdout, so
// a private body, a capability secret and a signing key are never a process
// argument or an environment variable. The child writes no files.
type bridge struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	logf *os.File
	next uint64
	dead bool
	// closed says this bridge has already been put away, so a second close is
	// not a second wait on one process.
	closed bool
}

func (m *Manager) openBridge(p *Profile, port int, origin, dir string) (*bridge, map[string]any, error) {
	cmd := exec.Command(m.bin.Bridge)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "bridge.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		return nil, nil, err
	}
	b := &bridge{cmd: cmd, in: stdin, out: bufio.NewReaderSize(stdout, 1<<20), logf: logf}

	req := map[string]any{"op": "open", "admin_port": port, "origin": origin,
		"app_id": p.AppID, "dna_hash": p.DNAHash, "agent_key": p.AgentKey}
	if len(p.Credentials) > 0 {
		var creds any
		if err := json.Unmarshal(p.Credentials, &creds); err == nil {
			req["credentials"] = creds
		}
	}
	answer, err := b.call(req, 120*time.Second)
	if err != nil {
		b.close()
		return nil, nil, err
	}
	// Credentials the bridge authorised for the first time are sealed by the
	// caller; the bridge keeps no copy of its own.
	if creds, ok := answer["credentials"]; ok {
		raw, err := json.Marshal(creds)
		if err != nil {
			b.close()
			return nil, nil, err
		}
		p.Credentials = raw
	}
	return b, answer, nil
}

// doorError is a refusal the conductor gave, relayed by the bridge under the
// conductor's own code. It is not a lost answer — the bridge was there and it
// answered — but it is not evidence that the chain is untouched either: the
// request had already crossed the pipe.
type doorError struct {
	Code    string
	Message string
}

func (e *doorError) Error() string { return "native: " + e.Code + ": " + e.Message }

// call writes one request line and reads the answer that belongs to it.
//
// Every request carries an id and every answer is matched against it. Anything
// else on that pipe — an answer to a request this call did not make, a line
// that is not an answer at all, a reply with no id — ends the line rather than
// being taken for this call's result: an answer attributed to the wrong request
// is worse than no answer, because the caller would act on it.
func (b *bridge) call(req map[string]any, timeout time.Duration) (map[string]any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dead {
		// Nothing left this process, so the chain is certainly untouched.
		return nil, fmt.Errorf("%w: the bridge is gone", ErrNotSent)
	}
	b.next++
	id := b.next
	req["id"] = id
	line, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	if _, err := b.in.Write(append(line, '\n')); err != nil {
		// A write that failed part-way may still have been read at the other
		// end, so this is not "not sent".
		b.dead = true
		return nil, fmt.Errorf("native: the bridge closed while a request was going out: %w", err)
	}
	type result struct {
		m   map[string]any
		err error
	}
	done := make(chan result, 1)
	go func() {
		raw, err := b.out.ReadString('\n')
		if err != nil {
			done <- result{nil, err}
			return
		}
		var answer map[string]any
		if err := json.Unmarshal([]byte(raw), &answer); err != nil {
			done <- result{nil, fmt.Errorf("the bridge wrote a line that is not an answer: %w", err)}
			return
		}
		done <- result{answer, nil}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			b.dead = true
			return nil, fmt.Errorf("native: the bridge gave no answer: %w", r.err)
		}
		got, ok := r.m["id"].(float64)
		if !ok || uint64(got) != id {
			b.dead = true
			return nil, fmt.Errorf("native: the bridge answered request %v while %d was outstanding; the line is closed",
				r.m["id"], id)
		}
		answered, present := r.m["ok"].(bool)
		if !present {
			b.dead = true
			return nil, fmt.Errorf("native: the bridge's answer to %d says neither yes nor no; the line is closed", id)
		}
		if !answered {
			return r.m, &doorError{Code: fmt.Sprint(r.m["code"]), Message: fmt.Sprint(r.m["error"])}
		}
		return r.m, nil
	case <-time.After(timeout):
		// A bridge that stopped answering is not asked again on this session:
		// the caller has to look at what the chain says before deciding.
		b.dead = true
		return nil, fmt.Errorf("native: the bridge did not answer within %s", timeout)
	}
}

func (b *bridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	if !b.dead && b.in != nil {
		_, _ = b.in.Write([]byte(`{"op":"close"}` + "\n"))
	}
	b.dead = true
	if b.in != nil {
		b.in.Close()
	}
	// A bridge whose process never started has nothing to wait for. It still has
	// its pipes closed above, which is what a caller asked for.
	if b.cmd != nil && b.cmd.Process != nil {
		done := make(chan struct{})
		go func() { b.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}
	if b.logf != nil {
		b.logf.Close()
	}
}

// Zome makes one zome call on a live profile.
//
// Whether this call may begin at all is decided under the manager's lock, and
// so is the count that a stop drains: a home that is closing, or a conductor
// that is being stopped, takes no new work, and a call that gets past that
// decision is counted before the lock is let go. The bridge it will speak to is
// taken there too, so a stop clearing it cannot be read half-way.
func (m *Manager) Zome(profile, fn string, payload any, timeout time.Duration) (any, error) {
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return nil, ErrClosing
	}
	s := m.live[profile]
	if s == nil || s.bridge == nil {
		m.mu.Unlock()
		return nil, ErrNotLive
	}
	if s.draining {
		m.mu.Unlock()
		return nil, ErrStopping
	}
	br := s.bridge
	s.calls++
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		s.calls--
		m.cond.Broadcast()
		m.mu.Unlock()
	}()
	// A zome function that takes no parameters expects msgpack nil, not an
	// empty map: `{}` would fail to deserialize into `()`.
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	answer, err := br.call(map[string]any{"op": "zome", "zome": m.bin.Manifest.App.Zome, "fn": fn, "payload": payload}, timeout)
	if err != nil {
		return nil, err
	}
	return answer["result"], nil
}

// KillBridge ends the bridge process without touching the conductor. It is how
// a lost answer is produced on purpose, to see what the chain says afterwards.
func (m *Manager) KillBridge(profile string) (map[string]any, error) {
	m.mu.Lock()
	s := m.live[profile]
	var br *bridge
	if s != nil {
		br = s.bridge
	}
	m.mu.Unlock()
	if br == nil {
		return nil, ErrNotLive
	}
	pid := 0
	if br.cmd.Process != nil {
		pid = br.cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	br.mu.Lock()
	br.dead = true
	br.mu.Unlock()
	return map[string]any{"bridge_pid": pid, "killed": true}, nil
}

// Reopen brings the bridge back after it was lost, on the same conductor. The
// credentials are the sealed ones; nothing is authorised a second time unless
// reauthorize is asked for, which is what a revoked capability needs.
func (m *Manager) Reopen(profile string, reauthorize bool) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return nil, ErrClosing
	}
	s := m.live[profile]
	if s == nil {
		return nil, ErrNotLive
	}
	if s.draining {
		return nil, ErrStopping
	}
	p, err := m.loadProfile(profile)
	if err != nil {
		return nil, err
	}
	if s.bridge != nil {
		s.bridge.close()
		s.bridge = nil
	}
	if reauthorize {
		p.Credentials = nil
	}
	br, answer, err := m.openBridge(p, s.port, s.origin, s.dir)
	if err != nil {
		return nil, err
	}
	s.bridge = br
	if err := m.saveProfile(p); err != nil {
		return nil, err
	}
	return map[string]any{"reopened": true, "credentials_new": answer["credentials_new"]}, nil
}
