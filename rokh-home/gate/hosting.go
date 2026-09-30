package gate

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const HostOfferFormat = "rokh.host-offer/1"

// HostOffer is the host's ceiling on program execution. It confers no right
// to home content: the owner's consumer registry decides those separately.
// Its identity is a label supplied by the trusted host, not remote attestation.
type HostOffer struct {
	Format           string `json:"format"`
	Host             string `json:"host"`
	Protocol         string `json:"protocol"`
	Enclosure        string `json:"enclosure"`
	MaxPrograms      int    `json:"max_programs"`
	AllowHostNetwork bool   `json:"allow_host_network"`
	AllowUnixSockets bool   `json:"allow_unix_sockets"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
}

type HostingError struct{ Reason string }

func (e *HostingError) Error() string { return "gate: host cannot supply this session: " + e.Reason }

func LocalHostOffer() HostOffer {
	host, _ := os.Hostname()
	return HostOffer{Format: HostOfferFormat, Host: host, Protocol: Protocol, Enclosure: Enclosure, MaxPrograms: 32, AllowHostNetwork: true, AllowUnixSockets: true}
}

func (o HostOffer) validate() error {
	if o.Format != HostOfferFormat || o.Host == "" {
		return &HostingError{"invalid_offer"}
	}
	if o.Protocol != Protocol {
		return &HostingError{"protocol_mismatch"}
	}
	if o.Enclosure != Enclosure {
		return &HostingError{"enclosure_mismatch"}
	}
	if o.MaxPrograms < 1 || o.MaxPrograms > 4096 {
		return &HostingError{"insufficient_program_slots"}
	}
	if o.ExpiresAt != 0 && o.ExpiresAt <= time.Now().Unix() {
		return &HostingError{"offer_expired"}
	}
	if _, err := exec.LookPath(Enclosure); err != nil {
		return &HostingError{"enclosure_unavailable"}
	}
	return nil
}

func (s *Server) hostErrorLocked() error {
	if s.closed {
		return &HostingError{"gate_closed"}
	}
	if s.hostRevoked != "" {
		return &HostingError{"offer_revoked"}
	}
	if s.offer.ExpiresAt != 0 && s.offer.ExpiresAt <= time.Now().Unix() {
		return &HostingError{"offer_expired"}
	}
	return nil
}

func (s *Server) hostError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hostErrorLocked()
}

// Reserve before starting a child so concurrent starts cannot exceed the host
// ceiling. Reservation is released even when program launch fails.
func (s *Server) reserveRun(spec RunSpec) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.hostErrorLocked(); err != nil {
		return nil, err
	}
	if spec.Net != "" && spec.Net != "none" && spec.Net != "host" {
		return nil, fmt.Errorf("gate: unknown network mode %q", spec.Net)
	}
	if spec.Net == "host" && !s.offer.AllowHostNetwork {
		return nil, &HostingError{"host_network_not_offered"}
	}
	if len(spec.UnixSockets) > 0 && !s.offer.AllowUnixSockets {
		return nil, &HostingError{"unix_sockets_not_offered"}
	}
	if s.heldLocked() >= s.offer.MaxPrograms {
		return nil, &HostingError{"program_slots_exhausted"}
	}
	s.starting++
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); s.starting--; s.mu.Unlock() }) }, nil
}

// reserveConductor takes one slot of the host's ceiling for a conductor.
//
// A conductor is a process this host carries for as long as it runs, exactly as
// a program is, and it is counted against the same offer. Round three checked
// only whether the offer had been withdrawn, so a host that offers no network
// at all, or whose slots were already full, would still have had a conductor
// started on it — and the report said otherwise. The check is here, before the
// start does anything: the request that cannot be supplied is refused while a
// refusal still costs nothing.
//
// commit keeps the slot for the conductor's life; release gives it back when
// the start failed. Whichever is called, calling it twice changes nothing.
func (s *Server) reserveConductor(profile string) (commit func(), release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.hostErrorLocked(); err != nil {
		return nil, nil, err
	}
	// A conductor joins a test network over this host's own network. A host that
	// does not offer that cannot supply one, and saying so before it is started
	// is the whole of the difference.
	if !s.offer.AllowHostNetwork {
		return nil, nil, &HostingError{"host_network_not_offered"}
	}
	if s.conductors[profile] {
		return nil, nil, &HostingError{"conductor_already_counted"}
	}
	if s.heldLocked() >= s.offer.MaxPrograms {
		return nil, nil, &HostingError{"program_slots_exhausted"}
	}
	s.conductorStarting++
	var once sync.Once
	drop := func() { s.conductorStarting-- }
	commit = func() {
		once.Do(func() {
			s.mu.Lock()
			drop()
			s.conductors[profile] = true
			s.mu.Unlock()
		})
	}
	release = func() {
		once.Do(func() {
			s.mu.Lock()
			drop()
			s.mu.Unlock()
		})
	}
	return commit, release, nil
}

// releaseConductor gives back the slot a conductor held.
func (s *Server) releaseConductor(profile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conductors, profile)
}

// heldLocked is everything this host is carrying or about to carry.
func (s *Server) heldLocked() int {
	return len(s.children) + s.starting + len(s.conductors) + s.conductorStarting
}

func (s *Server) Hosting() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := "active"
	if err := s.hostErrorLocked(); err != nil {
		state = err.(*HostingError).Reason
	}
	return map[string]any{"offer": s.offer, "state": state, "programs": len(s.children), "starting": s.starting,
		"conductors": len(s.conductors), "conductors_starting": s.conductorStarting,
		"held":           s.heldLocked(),
		"data_authority": "the owner's separate consumer grants"}
}

func stopProgram(cmd *exec.Cmd) {
	if cmd.Process != nil {
		// Every launch has a private process group. On Linux bubblewrap also
		// destroys its PID namespace when its supervisor dies.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	}
}

// RevokeHosting is called by the trusted host supervisor (SIGHUP in the CLI),
// never a consumer request. The owner channel remains available for inspection
// and exit; no ledger history or stored consumer grants are rewritten.
func (s *Server) RevokeHosting(reason string) {
	s.mu.Lock()
	if s.hostRevoked != "" || s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	// Withdrawing execution takes the conductor with it, in the order a close
	// uses: stop taking work, finish what is in flight, stop consistently, seal
	// the last state. Killing the programs first and the conductor never would
	// leave the engine's state where the home cannot reach it.
	if report := s.quiesceNative("host:revoked"); report != nil {
		s.mu.Lock()
		s.lastQuiesce = report
		s.mu.Unlock()
	}
	s.mu.Lock()
	if s.hostRevoked != "" || s.closed {
		s.mu.Unlock()
		return
	}
	s.hostRevoked = reason
	for c, owner := range s.conns {
		if !owner {
			c.Close()
		}
	}
	for _, cmd := range s.children {
		stopProgram(cmd)
	}
	s.mu.Unlock()
	s.home.CancelWork()
}
