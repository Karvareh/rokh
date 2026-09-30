package shell

import (
	"crypto/ed25519"
	"crypto/rand"
	"regexp"
	"strings"
	"testing"

	"rokh/event"
	"rokh/frame"
)

// escapedByte is what a payload printed as bytes looks like.
var escapedByte = regexp.MustCompile(`\\x[0-9a-fA-F]{2}|\\u[0-9a-fA-F]{4}`)

// A ledger with keys added and taken back and a grant given and taken back:
// "read rokh" answers every one of its own events on one line each, marked
// system, as a sentence, and never as bytes; the screen draws them the same
// way; and "see the ledger" counts the person's events and the ledger's own
// apart, the way the screen does.
func TestSystemEventsReadAsSentences(t *testing.T) {
	kv := makeKeyedVault(t, keyedSpecs)
	kv.revoke(t, "blind")
	s := kv.session(testPass)
	defer s.closeAll()
	run(t, s, "see the ledger")
	grants := s.current.led.ActiveGrants()
	if len(grants) == 0 {
		t.Fatal("the fixture holds no grant")
	}
	run(t, s, "take back the grant "+grants[0].String())

	out := run(t, s, "read rokh")
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "— ") {
			lines = append(lines, ln)
		}
	}
	if len(lines) < 6 {
		t.Fatalf("read rokh answered %d event lines:\n%s", len(lines), out)
	}
	for _, ln := range lines {
		if !strings.HasPrefix(ln, "— system rokh (") || escapedByte.MatchString(ln) {
			t.Errorf("a system event line: %q", ln)
		}
	}
	for _, want := range []string{"made: keyed fixture genesis", `key "reader" added, generation 1, reads journal`,
		`key "writer" added, generation 1, reads nothing, may sign`, "taken back (it was added in",
		"writing entrusted to", "taken back, from here on"} {
		if !strings.Contains(out, want) {
			t.Errorf("read rokh does not say %q:\n%s", want, out)
		}
	}

	st := s.snapshot()
	system := 0
	for _, e := range st.Events {
		if e.Address == event.AddressRoot {
			system++
			if !e.System || escapedByte.MatchString(e.Payload) || strings.Contains(e.Payload, "“") {
				t.Errorf("the screen draws a system event as %+v", e)
			}
		}
	}
	if system == 0 || st.System != system {
		t.Fatalf("the screen counts %d system events and shows %d", st.System, system)
	}
	a, _, _ := s.current.led.Tally()
	if st.Accepted+st.System != a {
		t.Fatalf("the screen counts %d and %d of %d accepted events", st.Accepted, st.System, a)
	}
	seen := run(t, s, "see the ledger")
	if want := countOf(st.Accepted, "event") + " and " + countOf(st.System, "system event"); !strings.HasPrefix(seen, want) {
		t.Fatalf("see the ledger says %q; the screen counts %q", seen, want)
	}
}

// Every kind of system event is said in words, whatever its payload holds,
// and a payload that cannot be read is said to be so, not printed.
func TestEveryKindOfSystemEventIsSaidInWords(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	enc := func(b []byte, err error) []byte {
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var seed [32]byte
	seed[0] = 7
	cases := map[string]event.Event{
		"made: open a new ledger named home  (and the room set aside)": {Verb: event.VerbGenesis, Payload: []byte("open a new ledger named home\nreserving 2 GB")},
		"two heads met here":                    {Verb: event.VerbMerge},
		"the owner's key added, generation 1":   {Verb: event.VerbKeyring, Payload: enc(event.Keyring{Op: event.KeyringAdd, Gen: 1, Name: "owner", Reader: make([]byte, 32), Reads: []string{""}}.Encode())},
		"writing entrusted to":                  {Verb: event.VerbGrant, Payload: enc(event.Grant{Subject: pub, Scope: "home", Verbs: []string{"note"}}.Encode())},
		"an open address: any key may write at": {Verb: event.VerbGrant, Payload: enc(event.Grant{Open: true, Read: true, Scope: "public", Verbs: []string{"note"}}.Encode())},
		"taken back, from here on":              {Verb: event.VerbRevoke, Payload: enc(event.Revoke{Target: frame.ID{1}}.Encode())},
		"seed given for the whole ledger":       {Verb: event.VerbSeed, Payload: enc(event.Seed{Op: event.SeedGive, Seed: seed, Key: seed}.Encode())},
		"cannot be read here":                   {Verb: event.VerbKeyring, Payload: []byte{0, 1, 2, 3, 0xff}},
		"a system event, rokh.future":           {Verb: "rokh.future", Payload: []byte{0, 1}},
	}
	for want, ev := range cases {
		got := systemLine(event.Signed{System: true, Event: ev})
		if !strings.Contains(got, want) || escapedByte.MatchString(got) || strings.ContainsAny(got, "\x00\n") {
			t.Errorf("%s: said %q, want %q in it", ev.Verb, got, want)
		}
	}
}
