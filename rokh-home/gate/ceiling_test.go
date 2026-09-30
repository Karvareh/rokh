package gate

import (
	"net"
	"os/exec"
	"sync"
	"testing"
)

// bare is a server with only the fields a ceiling needs. The host offer is the
// thing under test, and nothing here opens a socket or a home.
func bare(offer HostOffer) *Server {
	return &Server{children: map[int]*exec.Cmd{}, conductors: map[string]bool{},
		conns: map[net.Conn]bool{}, offer: offer, done: make(chan struct{})}
}

// A host that offers no network cannot supply a conductor, and says so before
// the start has made anything.
func TestAHostThatOffersNoNetworkSuppliesNoConductor(t *testing.T) {
	s := bare(HostOffer{Format: HostOfferFormat, Host: "h", Protocol: Protocol, Enclosure: Enclosure,
		MaxPrograms: 8, AllowHostNetwork: false})
	_, _, err := s.reserveConductor("profile-a")
	hosting, ok := err.(*HostingError)
	if !ok || hosting.Reason != "host_network_not_offered" {
		t.Fatalf("want host_network_not_offered, got %v", err)
	}
	if s.Hosting()["conductors"] != 0 {
		t.Fatal("a refusal must count nothing")
	}
}

// A conductor and a program are the same host's processes and share one
// ceiling. Round three counted only programs, so a full host would still have
// started a conductor.
func TestConductorsAndProgramsShareOneCeiling(t *testing.T) {
	s := bare(HostOffer{Format: HostOfferFormat, Host: "h", Protocol: Protocol, Enclosure: Enclosure,
		MaxPrograms: 2, AllowHostNetwork: true, AllowUnixSockets: true})
	commitA, _, err := s.reserveConductor("profile-a")
	if err != nil {
		t.Fatal(err)
	}
	commitA()
	// One slot left, and a program takes it.
	releaseRun, err := s.reserveRun(RunSpec{Consumer: "c", Net: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.reserveConductor("profile-b"); err == nil {
		t.Fatal("the host is full and a second conductor must be refused")
	} else if hosting, ok := err.(*HostingError); !ok || hosting.Reason != "program_slots_exhausted" {
		t.Fatalf("want program_slots_exhausted, got %v", err)
	}
	// The program finishes and the slot comes back.
	releaseRun()
	commitB, _, err := s.reserveConductor("profile-b")
	if err != nil {
		t.Fatalf("the freed slot should be usable: %v", err)
	}
	commitB()
	if got := s.Hosting()["conductors"]; got != 2 {
		t.Fatalf("two conductors are held, got %v", got)
	}
	// And a stop gives it back.
	s.releaseConductor("profile-a")
	if got := s.Hosting()["conductors"]; got != 1 {
		t.Fatalf("one conductor left, got %v", got)
	}
}

// Two starts racing for one slot: exactly one gets it. The reservation is what
// the start asks first, so this is the decision the live path makes.
func TestTwoStartsRacingTakeOneSlot(t *testing.T) {
	s := bare(HostOffer{Format: HostOfferFormat, Host: "h", Protocol: Protocol, Enclosure: Enclosure,
		MaxPrograms: 1, AllowHostNetwork: true})
	var wg sync.WaitGroup
	got := make(chan error, 2)
	begin := make(chan struct{})
	for _, profile := range []string{"profile-a", "profile-b"} {
		wg.Add(1)
		go func(profile string) {
			defer wg.Done()
			<-begin
			commit, _, err := s.reserveConductor(profile)
			if err == nil {
				commit()
			}
			got <- err
		}(profile)
	}
	close(begin)
	wg.Wait()
	close(got)
	won, refused := 0, 0
	for err := range got {
		if err == nil {
			won++
		} else {
			refused++
		}
	}
	if won != 1 || refused != 1 {
		t.Fatalf("one slot, one winner: won %d, refused %d", won, refused)
	}
	if s.Hosting()["conductors"] != 1 {
		t.Fatalf("exactly one conductor is held: %v", s.Hosting())
	}
}

// A withdrawn offer supplies no conductor either.
func TestAWithdrawnOfferSuppliesNoConductor(t *testing.T) {
	s := bare(HostOffer{Format: HostOfferFormat, Host: "h", Protocol: Protocol, Enclosure: Enclosure,
		MaxPrograms: 4, AllowHostNetwork: true})
	s.hostRevoked = "the supervisor withdrew execution"
	if _, _, err := s.reserveConductor("profile-a"); err == nil {
		t.Fatal("a withdrawn offer starts nothing")
	} else if hosting, ok := err.(*HostingError); !ok || hosting.Reason != "offer_revoked" {
		t.Fatalf("want offer_revoked, got %v", err)
	}
}

// A put-away that did not put the engine away is not an ok answer, and the exit
// code the CLI reads is taken from exactly this ok.
func TestAFailedPutAwayIsNotAnOkAnswer(t *testing.T) {
	failed := map[string]any{"format": "rokh.native-quiesce/1", "ok": false,
		"profiles": []map[string]any{{"profile": "p", "ok": false, "checkpoint_error": "not sealed"}}}
	answer := quiesceAnswer(failed)
	if answer["ok"] != false || answer["code"] != "not_quiesced" {
		t.Fatalf("a failed put-away must answer no: %v", answer)
	}
	if answer["quiesce"] == nil {
		t.Fatal("the report has to travel with the refusal, or there is nothing to look at")
	}
	if ok := quiesceAnswer(map[string]any{"ok": true})["ok"]; ok != true {
		t.Fatalf("a clean put-away answers yes: %v", ok)
	}
	if ok := quiesceAnswer(nil)["ok"]; ok != true {
		t.Fatalf("a gate with no engine has nothing to put away: %v", ok)
	}
}
