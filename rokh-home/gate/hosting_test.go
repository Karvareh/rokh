package gate

import (
	"errors"
	"testing"
	"time"

	"rokh-home/authority"
	"rokh-home/home"
)

func TestAnIncompatibleHostIsRefusedBeforeOpeningSockets(t *testing.T) {
	f := newGate(t)
	f.s.Close()
	for _, tc := range []struct {
		reason string
		change func(*HostOffer)
	}{
		{"protocol_mismatch", func(o *HostOffer) { o.Protocol = "rokh.gate/999" }},
		{"enclosure_mismatch", func(o *HostOffer) { o.Enclosure = "none" }},
		{"insufficient_program_slots", func(o *HostOffer) { o.MaxPrograms = 0 }},
		{"offer_expired", func(o *HostOffer) { o.ExpiresAt = time.Now().Add(-time.Hour).Unix() }},
	} {
		o := LocalHostOffer()
		tc.change(&o)
		s, err := StartWithHost(f.h, f.run, o)
		if err == nil {
			s.Close()
			t.Fatalf("accepted incompatible host: %s", tc.reason)
		}
		var host *HostingError
		if !errors.As(err, &host) || host.Reason != tc.reason {
			t.Fatalf("wrong host refusal: %v", err)
		}
	}
}

func TestHostResourcesAndGuestAuthorityAreSeparateAndRevocable(t *testing.T) {
	f := newGate(t)
	f.importText("note.txt", "synthetic retained content", "notes/test")
	a, _ := f.program("guest", map[string]string{"bytes": "notes"}, nil)
	f.s.Close()
	o := LocalHostOffer()
	o.MaxPrograms = 1
	o.AllowHostNetwork = false
	s, err := StartWithHost(f.h, f.run, o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, _ := f.h.Consumer(a)
	actor := home.Actor{Consumer: a, Session: c.Version}
	if r := s.dispatch(actor, map[string]any{"op": "bytes", "path": "notes/test"}, false); r["ok"] != true {
		t.Fatalf("authorized reading: %+v", r)
	}
	if r := s.dispatch(actor, map[string]any{"op": "draft.open", "path": "notes/test"}, false); r["ok"] != false {
		t.Fatal("host execution supply gave guest ungranted writing")
	}
	spec := RunSpec{Consumer: a, Argv: []string{"/bin/sleep", "60"}, Dir: t.TempDir(), Wait: false}
	network := spec
	network.Net = "host"
	if _, err := s.run(network); err == nil {
		t.Fatal("owner run exceeded host network supply")
	}
	if _, err := s.run(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := s.run(spec); err == nil {
		t.Fatal("host program ceiling was exceeded")
	}
	if err := f.h.RevokeConsumer(home.OwnerActor, a); err != nil {
		t.Fatal(err)
	}
	if r := s.dispatch(actor, map[string]any{"op": "bytes", "path": "notes/test"}, false); r["ok"] != false {
		t.Fatal("guest revocation did not stop reading")
	}
	// A fresh guest with fresh scope still cannot use a withdrawn host.
	cv, _, err := f.h.AddConsumer(home.OwnerActor, "new-guest", "test", home.Places{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.Grant(home.OwnerActor, cv.ID, authority.Bytes, "notes"); err != nil {
		t.Fatal(err)
	}
	c, _ = f.h.Consumer(cv.ID)
	s.RevokeHosting("synthetic host withdrawal")
	r := s.dispatch(home.Actor{Consumer: c.ID, Session: c.Version}, map[string]any{"op": "bytes", "path": "notes/test"}, false)
	if r["code"] != "host_unavailable" || r["reason"] != "offer_revoked" {
		t.Fatalf("host revocation: %+v", r)
	}
	if _, err := s.run(spec); err == nil {
		t.Fatal("revoked hosting still launches")
	}
	until := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.children)
		s.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("host withdrawal left its program running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _, err := f.h.Bytes(home.OwnerActor, "notes/test", 1, "", 0, -1)
	if err != nil || string(b) != "synthetic retained content" {
		t.Fatal("withdrawal damaged data")
	}
}
