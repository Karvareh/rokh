//go:build legacy09

// This test pins the 0.9 carrier API (secrets, Put, FS, Store) removed by the
// v1 vessel carrier; it is excluded until rewritten for v1.

package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
)

// expectNotRecorded checks the typed shape of a writing request that recorded
// nothing: a refusal, a stable code, the word "not-recorded", and still a
// sentence for a person.
func expectNotRecorded(t *testing.T, r map[string]any, code string) {
	t.Helper()
	if ok, _ := r["ok"].(bool); ok {
		t.Fatalf("expected a refusal with code %q, got %v", code, r)
	}
	if r["code"] != code {
		t.Fatalf("code = %v, want %q (%v)", r["code"], code, r["error"])
	}
	if r["record"] != "not-recorded" {
		t.Fatalf("record = %v, want not-recorded (%v)", r["record"], r)
	}
	if s, _ := r["error"].(string); s == "" {
		t.Fatalf("a refusal still owes a sentence: %v", r)
	}
}

func newKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func witness() map[string]any {
	return map[string]any{"origin": "synthetic test", "authority": "test owner",
		"audience": "test", "state": "before", "wayBack": "discard the fixture"}
}

func line(t *testing.T, req map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// durableAccepted opens the carrier afresh and counts what its references
// reach: the recorded truth, whatever any live view says.
func durableAccepted(t *testing.T, dir string) int {
	t.Helper()
	c, err := carrier.Open(carrier.FS{Root: dir}, "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	acc, _, _ := auditReload(t, c).Tally()
	return acc
}

func liveAccepted(t *testing.T, s *Server) int {
	t.Helper()
	return int(num(t, mustOK(t, ask(t, s, map[string]any{"op": "status"}), "status")["accepted"]))
}

// A writing request says in one word whether it was recorded, and when it
// was not, a stable code says why. The sentence stays for people.
//
//	— T8.5, T4.4
func TestAWriteSaysWhetherItWasRecorded(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true, Dir: f.dir})
	r := mustOK(t, ask(t, s, map[string]any{"op": "write", "address": "home/note", "message": "synthetic"}), "write")
	if r["record"] != "recorded" {
		t.Fatalf("record = %v, want recorded", r["record"])
	}

	ro := New(f.car, f.led, Options{ReadOnly: true, Dir: f.dir})
	expectNotRecorded(t, ask(t, ro, map[string]any{"op": "write", "address": "home/note", "message": "x"}), "read_only")
	expectNotRecorded(t, ask(t, ro, map[string]any{"op": "intent", "address": "home", "doing": "x", "witness": witness()}), "read_only")

	quiet := New(f.car, f.led, Options{AllowSign: false, Dir: f.dir})
	expectNotRecorded(t, ask(t, quiet, map[string]any{"op": "write", "address": "home/note", "message": "x"}), "signing_disabled")
	expectNotRecorded(t, ask(t, quiet, map[string]any{"op": "intent", "address": "home", "doing": "x", "witness": witness()}), "signing_disabled")

	expectNotRecorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/note", "message": "x", "key": "nobody"}), "key_unknown")
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "append", "raw": "not hex"}), "bad_request")

	// A validly signed event by a key with no authority here is refused by the
	// ledger, and never stored.
	_, stranger, _ := newKey()
	anchor := f.gen.ID
	e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{f.gen.ID},
		Address: "home/x", Verb: "note", Payload: []byte("synthetic")}, stranger)
	if err != nil {
		t.Fatal(err)
	}
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "append", "raw": hex.EncodeToString(e.Raw)}), "not_accepted")

	own, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{f.gen.ID},
		Address: "home/y", Verb: "note", Payload: []byte("synthetic")}, f.root)
	if err != nil {
		t.Fatal(err)
	}
	r = mustOK(t, ask(t, s, map[string]any{"op": "append", "raw": hex.EncodeToString(own.Raw)}), "append")
	if r["record"] != "recorded" {
		t.Fatalf("append record = %v", r["record"])
	}
	if got, want := durableAccepted(t, f.dir), 3; got != want {
		t.Fatalf("durable accepted = %d, want %d", got, want)
	}

	for _, c := range []struct {
		req  map[string]any
		code string
	}{
		{map[string]any{"op": "get", "id": strings.Repeat("0", 64)}, "not_found"},
		{map[string]any{"op": "no-such-op"}, "unknown_op"},
	} {
		r := ask(t, s, c.req)
		if r["ok"] != false || r["code"] != c.code {
			t.Fatalf("%v: got %v", c.req["op"], r)
		}
	}
	if r := s.Handle([]byte("{")); r["code"] != "bad_request" {
		t.Fatalf("a broken line: %v", r)
	}
}

// failAfterStore performs a write and then reports it as failed, or refuses
// to list the references for a while — the two ways a recording's ending can
// be misreported by the ground under it.
type failAfterStore struct {
	carrier.Store
	mu            sync.Mutex
	writtenButErr bool // the next reference write happens, then errs
	refuseRef     bool // the next reference write errs without happening
	blindLists    int  // this many reference listings fail
	// blindAfterRefusal is how many listings fail once a refused reference
	// write has happened, so the blindness lands on the reading-back and not
	// on the reading before the write.
	blindAfterRefusal int
}

func (s *failAfterStore) Write(name string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.Contains(name, "/refs/") {
		if s.refuseRef {
			s.refuseRef = false
			s.blindLists = s.blindAfterRefusal
			return errors.New("synthetic: reference write refused")
		}
		if s.writtenButErr {
			s.writtenButErr = false
			if err := s.Store.Write(name, b); err != nil {
				return err
			}
			return errors.New("synthetic: written, then reported as failed")
		}
	}
	return s.Store.Write(name, b)
}

func (s *failAfterStore) List(prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.Contains(prefix, "refs") && s.blindLists > 0 {
		s.blindLists--
		return nil, errors.New("synthetic: references unreadable")
	}
	return s.Store.List(prefix)
}

// When the ground misreports the ending, the answer follows what is recorded,
// not what the call returned: a write that landed is "recorded", a write that
// did not is "not-recorded", and a daemon that cannot re-read the carrier to
// find out says "unknown" — and does not answer from a view it cannot vouch
// for afterwards.
//
//	— T8.5, T4.4
func TestAMisreportedEndingIsSettledByWhatIsRecorded(t *testing.T) {
	t.Run("written-then-error", func(t *testing.T) {
		f := newFixture(t)
		st := &failAfterStore{Store: carrier.FS{Root: f.dir}}
		c, err := carrier.Open(st, "pass")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		s := New(c, auditReload(t, c), Options{AllowSign: true, Dir: f.dir})
		st.writtenButErr = true
		r := ask(t, s, map[string]any{"op": "write", "address": "home/a", "message": "synthetic"})
		if r["record"] != "recorded" {
			t.Fatalf("the write landed, and the answer says %v: %v", r["record"], r)
		}
		if live, durable := liveAccepted(t, s), durableAccepted(t, f.dir); live != durable || durable != 2 {
			t.Fatalf("live %d durable %d", live, durable)
		}
	})
	t.Run("refused-reference", func(t *testing.T) {
		f := newFixture(t)
		st := &failAfterStore{Store: carrier.FS{Root: f.dir}}
		c, err := carrier.Open(st, "pass")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		s := New(c, auditReload(t, c), Options{AllowSign: true, Dir: f.dir})
		st.refuseRef = true
		expectNotRecorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/a", "message": "synthetic"}), "storage_failed")
		if live, durable := liveAccepted(t, s), durableAccepted(t, f.dir); live != durable || durable != 1 {
			t.Fatalf("live %d durable %d", live, durable)
		}
	})
	t.Run("cannot-reread", func(t *testing.T) {
		f := newFixture(t)
		st := &failAfterStore{Store: carrier.FS{Root: f.dir}}
		c, err := carrier.Open(st, "pass")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		s := New(c, auditReload(t, c), Options{AllowSign: true, Dir: f.dir})
		st.refuseRef = true
		st.blindAfterRefusal = 1
		r := ask(t, s, map[string]any{"op": "write", "address": "home/a", "message": "synthetic"})
		if r["ok"] != false || r["record"] != "unknown" || r["code"] != "storage_failed" {
			t.Fatalf("an ending nobody could read back must be unknown: %v", r)
		}
		if live, durable := liveAccepted(t, s), durableAccepted(t, f.dir); live != durable {
			t.Fatalf("after the carrier became readable again: live %d durable %d", live, durable)
		}
	})
}

// A precondition on the heads is checked at the point of recording: a write
// that names heads the ledger has moved past records nothing and says where
// the heads now are.
//
//	— T8.5, T4.4
func TestAStaleHeadsPreconditionRecordsNothing(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true, Dir: f.dir})
	heads := []string{f.gen.ID.String()}
	mustOK(t, ask(t, s, map[string]any{"op": "write", "address": "home/a", "message": "one", "expect_heads": heads}), "first")
	r := ask(t, s, map[string]any{"op": "write", "address": "home/b", "message": "two", "expect_heads": heads})
	expectNotRecorded(t, r, "precondition_failed")
	if hs, _ := r["heads"].([]string); len(hs) != 1 || hs[0] == heads[0] {
		t.Fatalf("the refusal should name the current heads: %v", r["heads"])
	}
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "intent", "address": "home", "doing": "x",
		"witness": witness(), "expect_heads": heads}), "precondition_failed")
	if got := durableAccepted(t, f.dir); got != 2 {
		t.Fatalf("durable accepted = %d, want 2", got)
	}
}

// Two doors on one carrier, each with its own ledger view, race on the same
// heads. Exactly one records; the other is told the heads moved. Neither
// orphans the other's event.
//
//	— T8.5, T4.4
func TestTwoDoorsOneCarrierOneWinner(t *testing.T) {
	f := newFixture(t)
	open := func() *Server {
		c, err := carrier.Open(carrier.FS{Root: f.dir}, "pass")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return New(c, auditReload(t, c), Options{AllowSign: true, Dir: f.dir})
	}
	doors := []*Server{open(), open()}
	heads := []string{f.gen.ID.String()}
	lines := [][]byte{
		line(t, map[string]any{"op": "write", "address": "home/race", "message": "writer 0", "expect_heads": heads}),
		line(t, map[string]any{"op": "write", "address": "home/race", "message": "writer 1", "expect_heads": heads}),
	}
	for round := 0; round < 20; round++ {
		if round > 0 {
			st := mustOK(t, ask(t, doors[0], map[string]any{"op": "status"}), "status")
			heads = st["heads"].([]string)
			for i := range lines {
				lines[i] = line(t, map[string]any{"op": "write", "address": "home/race",
					"message": fmt.Sprintf("round %d writer %d", round, i), "expect_heads": heads})
			}
		}
		results := make([]map[string]any, 2)
		var wg sync.WaitGroup
		for i := range doors {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i] = doors[i].Handle(lines[i])
			}(i)
		}
		wg.Wait()
		recorded := 0
		for _, r := range results {
			switch r["record"] {
			case "recorded":
				recorded++
			case "not-recorded":
				if r["code"] != "precondition_failed" {
					t.Fatalf("round %d: the loser should be told the heads moved: %v", round, r)
				}
			default:
				t.Fatalf("round %d: untyped answer %v", round, r)
			}
		}
		if recorded != 1 {
			t.Fatalf("round %d: %d writers recorded against the same heads", round, recorded)
		}
		if got, want := durableAccepted(t, f.dir), round+2; got != want {
			t.Fatalf("round %d: durable accepted %d, want %d (an event was orphaned)", round, got, want)
		}
	}
}

// An attempt is the caller's own name for one recording, bound to who signs,
// which ledger, and the exact request. The same attempt again returns the
// same event and records nothing new; a different request under the same name
// is refused; and after a lost answer, a read-only query says what happened.
//
//	— T8.5, T3.6, T10.1
func TestAnAttemptIsBoundToActorLedgerAndBytes(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true, Dir: f.dir})
	req := map[string]any{"op": "write", "address": "home/a", "message": "one", "attempt": "gateway:plan-1"}
	first := mustOK(t, ask(t, s, req), "first")
	id := first["id"]
	if first["record"] != "recorded" || first["already"] != false {
		t.Fatalf("first: %v", first)
	}
	again := mustOK(t, ask(t, s, req), "again")
	if again["id"] != id || again["already"] != true || again["record"] != "recorded" {
		t.Fatalf("the same attempt again should be the same event: %v", again)
	}
	if got := durableAccepted(t, f.dir); got != 2 {
		t.Fatalf("a repeated attempt recorded twice: %d", got)
	}
	other := map[string]any{"op": "write", "address": "home/a", "message": "changed", "attempt": "gateway:plan-1"}
	expectNotRecorded(t, ask(t, s, other), "attempt_conflict")

	q := mustOK(t, ask(t, s, map[string]any{"op": "attempt", "attempt": "gateway:plan-1"}), "query")
	if q["record"] != "recorded" || q["id"] != id {
		t.Fatalf("query: %v", q)
	}
	none := mustOK(t, ask(t, s, map[string]any{"op": "attempt", "attempt": "never-made"}), "query none")
	if none["record"] != "not-recorded" {
		t.Fatalf("an attempt nobody made: %v", none)
	}
	pub, _, _ := newKey()
	elsewhere := mustOK(t, ask(t, s, map[string]any{"op": "attempt", "attempt": "gateway:plan-1",
		"author": hex.EncodeToString(pub)}), "query other actor")
	if elsewhere["record"] != "not-recorded" {
		t.Fatalf("an attempt belongs to its signer: %v", elsewhere)
	}
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/a", "message": "x",
		"attempt": strings.Repeat("x", 129)}), "attempt_invalid")

	// The binding lives in the signed bytes, so a fresh reading of the carrier
	// finds it without anything kept beside the ledger.
	c, err := carrier.Open(carrier.FS{Root: f.dir}, "pass")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fresh := New(c, auditReload(t, c), Options{Dir: f.dir})
	q2 := mustOK(t, ask(t, fresh, map[string]any{"op": "attempt", "attempt": "gateway:plan-1"}), "query after reopening")
	if q2["record"] != "recorded" || q2["id"] != id {
		t.Fatalf("after reopening: %v", q2)
	}

	// Receipts take attempts too.
	in := map[string]any{"op": "intent", "address": "home", "doing": "synthetic step",
		"witness": witness(), "attempt": "gateway:intent-1"}
	i1 := mustOK(t, ask(t, s, in), "intent")
	i2 := mustOK(t, ask(t, s, in), "intent again")
	if i1["id"] != i2["id"] || i2["already"] != true {
		t.Fatalf("intent attempt: %v then %v", i1, i2)
	}
	in["doing"] = "another step"
	expectNotRecorded(t, ask(t, s, in), "attempt_conflict")
	out := map[string]any{"op": "outcome", "address": "home", "intent": i1["id"], "outcome": "done",
		"saying": "synthetic", "witness": witness(), "attempt": "gateway:outcome-1"}
	o1 := mustOK(t, ask(t, s, out), "outcome")
	o2 := mustOK(t, ask(t, s, out), "outcome again")
	if o1["id"] != o2["id"] || o2["already"] != true {
		t.Fatalf("outcome attempt: %v then %v", o1, o2)
	}
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "append", "raw": "00", "attempt": "x"}), "attempt_unsupported")
}

// The capabilities op tells a program what this door is — protocol,
// generations, limits, whether it signs, which names sign with which public
// key under which authority — and nothing secret.
//
//	— T8, N4.6
func TestCapabilitiesSayWhatThisDoorIsWithoutASecret(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true, Dir: f.dir, Release: "synthetic"})
	r := mustOK(t, ask(t, s, map[string]any{"op": "capabilities"}), "capabilities")
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["protocol"] != Protocol || m["release"] != "synthetic" {
		t.Fatalf("protocol/release: %v %v", m["protocol"], m["release"])
	}
	gens, _ := m["generations"].(map[string]any)
	if fmt.Sprint(gens["reads"]) != "[RKH1 RKH2]" || gens["writes"] != "RKH2" {
		t.Fatalf("generations: %v", gens)
	}
	if m["read_only"] != false || m["allow_sign"] != true || m["no_root"] != false {
		t.Fatalf("modes: %v", m)
	}
	lim, _ := m["limits"].(map[string]any)
	if num(t, lim["line"]) != MaxLine || num(t, lim["payload"]) != event.MaxPayload ||
		num(t, lim["frame"]) != frame.MaxFrame || num(t, lim["attempt"]) != 128 {
		t.Fatalf("limits: %v", lim)
	}
	for _, op := range []string{"attempt", "capabilities", "write", "intent", "outcome"} {
		if !strings.Contains(fmt.Sprint(m["ops"]), op) {
			t.Fatalf("ops does not list %s: %v", op, m["ops"])
		}
	}
	if fmt.Sprint(m["record_states"]) != "[recorded not-recorded unknown]" {
		t.Fatalf("record states: %v", m["record_states"])
	}
	signers, _ := m["signers"].([]any)
	if len(signers) != 1 {
		t.Fatalf("signers: %v", m["signers"])
	}
	root, _ := signers[0].(map[string]any)
	pub := f.root.Public().(ed25519.PublicKey)
	if root["name"] != "root" || root["public"] != hex.EncodeToString(pub) {
		t.Fatalf("root signer: %v", root)
	}
	text := string(raw)
	for _, secret := range []string{hex.EncodeToString(f.root), hex.EncodeToString(f.root.Seed()),
		base64.StdEncoding.EncodeToString(f.root), base64.StdEncoding.EncodeToString(f.root.Seed())} {
		if strings.Contains(text, secret) {
			t.Fatal("the capabilities answer carries a private key")
		}
	}
}

// Declaring the same meaning twice is one declaration, whatever order the verbs
// were typed in; declaring a different one in a space that is taken is still
// refused. The declaration has a name, so a program can carry it into what it
// witnesses.
//
//	— T11.10, T13.5
func TestTheSameBindingTwiceIsOneBinding(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true, Dir: f.dir})
	decl := func(version string, can ...any) map[string]any {
		return map[string]any{"op": "bind", "namespace": "gateway", "version": version, "can": can,
			"unknown": "refuse", "repeat": "duplicates", "retry": "never-retry",
			"compensate": "irreversible", "ending": "ask-a-person"}
	}
	r1 := mustOK(t, ask(t, s, decl("0.3.0", "gateway.plan.propose", "gateway.engine.inspect")), "bind")
	d, _ := r1["declaration"].(string)
	if r1["already"] != false || len(d) != 64 {
		t.Fatalf("first bind: %v", r1)
	}
	r2 := mustOK(t, ask(t, s, decl("0.3.0", "gateway.engine.inspect", "gateway.plan.propose")), "bind again")
	if r2["already"] != true || r2["declaration"] != d {
		t.Fatalf("the same declaration again: %v", r2)
	}
	r3 := ask(t, s, decl("0.4.0", "gateway.engine.inspect"))
	if r3["ok"] != false || r3["code"] != "namespace_taken" {
		t.Fatalf("a different declaration in a taken space: %v", r3)
	}
	b := mustOK(t, ask(t, s, map[string]any{"op": "bound"}), "bound")
	hs := b["harnesses"].([]map[string]any)
	if len(hs) != 1 || hs[0]["declaration"] != d {
		t.Fatalf("bound: %v", hs)
	}
}

// A program's door never signs with the root key — not when a request names
// it, and not when a request names nothing.
//
//	— T11.10, N4.8
func TestAProgramDoorNeverFallsBackToRoot(t *testing.T) {
	f := newFixture(t)
	s := New(f.car, f.led, Options{AllowSign: true, NoRoot: true, Dir: f.dir})
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/a", "message": "x"}), "root_refused")
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "write", "address": "home/a", "message": "x", "key": "root"}), "root_refused")
	expectNotRecorded(t, ask(t, s, map[string]any{"op": "intent", "address": "home", "doing": "x", "witness": witness()}), "root_refused")
	if got := durableAccepted(t, f.dir); got != 1 {
		t.Fatalf("durable accepted = %d", got)
	}
}
