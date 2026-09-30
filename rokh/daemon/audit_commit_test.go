//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// This audit-only store injects an ordinary I/O failure while the process
// stays alive. Existing cut tests reopen the carrier after stopping writes.
type auditFailStore struct {
	carrier.Store
	left    int
	enabled bool
	fired   bool
}

func (s *auditFailStore) Write(name string, b []byte) error {
	if s.enabled {
		if s.left == 0 {
			s.fired = true
			return errors.New("audit: injected storage failure")
		}
		s.left--
	}
	return s.Store.Write(name, b)
}

func auditReload(t *testing.T, c *carrier.Carrier) *ledger.Ledger {
	t.Helper()
	refs, err := c.Refs()
	if err != nil {
		t.Fatal(err)
	}
	var heads []frame.ID
	for _, id := range refs {
		heads = append(heads, id)
	}
	raw, err := c.Get(c.Anchor())
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// A daemon must not present a failed, uncommitted write as an accepted event.
// This checks the public API against a fresh load of durable references.
// It exercises three public recording paths, each at both commit steps,
// plus an uncut control. — T8.5, T4.4
func TestAuditFailedRecordingIsAbsentFromLiveAnswers(t *testing.T) {
	for _, op := range []string{"write", "append", "intent"} {
		for cut := 0; cut <= 2; cut++ {
			t.Run(fmt.Sprintf("%s-cut-%d", op, cut), func(t *testing.T) {
				f := newFixture(t)
				fs := &auditFailStore{Store: carrier.FS{Root: f.dir}, left: cut}
				c, err := carrier.Open(fs, "pass")
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				s := New(c, f.led, Options{AllowSign: true})
				req := map[string]any{"op": op, "address": "audit", "message": "synthetic test"}
				if op == "append" {
					anchor := f.gen.ID
					e, err := event.SignFresh(event.Event{Carrier: &anchor,
						Parents: []frame.ID{f.gen.ID}, Address: "audit", Verb: "note",
						Payload: []byte("synthetic test")}, f.root)
					if err != nil {
						t.Fatal(err)
					}
					req["raw"] = hex.EncodeToString(e.Raw)
				}
				if op == "intent" {
					req["doing"] = "synthetic local effect"
					req["witness"] = map[string]any{"origin": "audit", "authority": "test owner",
						"audience": "audit", "state": "before", "wayBack": "discard test fixture"}
				}
				fs.enabled = true
				response := ask(t, s, req)
				fs.enabled = false
				succeeded, _ := response["ok"].(bool)
				if cut < 2 && (!fs.fired || succeeded) {
					t.Fatalf("fault not exercised: %v", response)
				}
				if cut == 2 && !succeeded {
					t.Fatalf("uncut control failed: %v", response)
				}
				live := mustOK(t, ask(t, s, map[string]any{"op": "status"}), "live status")
				reopened := auditReload(t, f.car)
				durable, _, _ := reopened.Tally()
				observed := int(num(t, live["accepted"]))
				t.Logf("response_ok=%v injected=%v live_accepted=%d durable_accepted=%d",
					succeeded, fs.fired, observed, durable)
				if observed != durable {
					t.Errorf("failed recording is exposed by the live API: accepted=%d; reopening sees %d", observed, durable)
				}
			})
		}
	}
}

// A bound namespace declares interpretation, not the client's authority.
// This characterization uses only a synthetic root and stores no real data.
func TestAuditBindingIsNotAnAuthorizationBoundary(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true})
	mustOK(t, ask(t, s, map[string]any{"op": "bind", "namespace": "clerk", "version": "1",
		"can": []string{"clerk.note"}, "unknown": "refuse", "repeat": "idempotent",
		"retry": "never-retry", "compensate": "compensable", "ending": "ask-a-person"}), "bind")
	response := mustOK(t, ask(t, s, map[string]any{"op": "write", "address": "outside/clerk",
		"verb": "note", "message": "synthetic outside-scope write"}), "write outside bound namespace")
	id, err := frame.ParseID(response["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	written, found := s.led.Get(id)
	if !found {
		t.Fatal("event missing")
	}
	t.Logf("bound_namespace=clerk actual_address=%s root_authored=%v authority_field_present=%v",
		written.Event.Address, written.Event.Authority == nil, written.Event.Authority != nil)
	if written.Event.Authority != nil {
		t.Fatal("expected root convenience path")
	}
}
