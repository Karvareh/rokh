package shell

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"strings"

	"rokh/carrier"
	"rokh/covenant"
	"rokh/event"
	"rokh/ledger"
	"rokh/tui"
)

// The screen surface is the same session drawn on a terminal. Everything
// below hands the tui package a picture of the session and takes back the
// sentence a person typed; the parsing, the judging and the recording stay
// here, where they were. The tui imports nothing of Rokh's, and this file is
// the only place the two meet.
//
//	— T9, T9.1, T4.1

// say runs one line as the surface would, and returns the answer instead of
// printing it. done is true when the line was the leaving sentence.
func (s *session) say(line string) (reply string, done bool, err error) {
	c, err := parse(line)
	if err != nil {
		return "", false, nearMiss(line)
	}
	reply, err = s.execute(c)
	if err != nil {
		refused := s.plain(err)
		s.unsure = s.unsure || exitFor(refused) == 4
		return "", false, refused
	}
	return reply, c.Op == opClose, nil
}

// runScreen drives the tui with this session. The asides the session would
// have written to stderr — an opened ledger, a seat that would not declare
// itself — are gathered and shown under the reply, so nothing is printed over
// the screen and nothing is lost.
func runScreen(s *session) error {
	var notes bytes.Buffer
	s.notes, s.onScreen = &notes, true
	defer func() { s.notes, s.onScreen = os.Stderr, false }()
	return tui.Run(os.Stdin, os.Stdout, tui.Hooks{
		Say: func(line string) (string, bool, error) {
			notes.Reset()
			reply, done, err := s.say(line)
			if aside := strings.TrimSpace(notes.String()); aside != "" {
				if reply != "" {
					reply += "\n"
				}
				reply += aside
			}
			return reply, done, err
		},
		Snapshot: s.snapshot,
		Signals:  TerminalSignals,
	})
}

// snapshot is the session as the screen may know it: the open ledger, its
// tally and heads, the newest events in causal order, the sentence waiting
// in working state, and the live grants and disclosures. Nothing here reads
// past what the line surface already says in "see the ledger", "read" and
// "see the grants"; it is those answers, arranged.
func (s *session) snapshot() tui.Snapshot {
	st := tui.Snapshot{Vault: s.vault}
	l := s.current
	if l == nil {
		return st
	}
	led := l.led
	st.Ledger = l.name
	st.Anchor = led.Genesis().Short()
	heads := led.Heads()
	st.Heads = len(heads)
	if len(heads) > 0 {
		st.Head = heads[0].Short()
	}
	st.Custody = custodyOf(l.sec)
	st.Verified = true // opening a ledger verifies every event on its carrier
	// The room is the vessel's own, as it holds it now: its size, what the
	// next recording may write, and whether it grows by itself.
	st.Room, st.Free, st.Growth, st.Full = roomOfLedger(l)
	st.Accepted, st.Rejected, st.Pending = led.Tally()
	// The tally counts what the person recorded, and the ledger's own
	// events beside it, the way "see the ledger" counts them.
	st.System = systemCount(led)
	st.Accepted -= st.System
	st.Layers = layersOf(led)

	names := keyNamesOf(l.car, led)
	order := led.Order()
	for i := len(order) - 1; i >= 0 && len(st.Events) < 40; i-- {
		e, ok := led.Get(order[i])
		if !ok {
			continue
		}
		// Three questions, three fields: who signed (the key), under what
		// (the grant), and through which door. None of them is who composed
		// the words, which the surface does not claim to know.
		//   — T12.5
		item := tui.EventItem{
			ID:        order[i].Short(),
			Verdict:   "accepted",
			Verb:      e.Event.Verb,
			Address:   e.Event.Address,
			Payload:   s.describePayload(l, e),
			Signer:    names.of(e.Event.Author),
			Authority: "the owner's own right",
			System:    e.System,
		}
		if len(e.Event.Parents) > 0 {
			item.Parent = e.Event.Parents[0].Short()
		}
		if e.Event.Authority != nil {
			item.Authority = "grant " + e.Event.Authority.Short()
		}
		for _, a := range e.Event.Attest {
			if a.Oracle == "door" {
				item.Door = string(a.Claim)
			}
		}
		st.Events = append(st.Events, item)
	}

	key, right := l.signerName()
	for i, w := range s.drafts {
		if f, err := s.work.Preview(w.h, l.branchHead(), right); err == nil {
			d := tui.Draft{Index: i + 1, Count: len(s.drafts), Sentence: w.line, Address: f.Address, Verb: f.Verb,
				PayloadBytes: f.PayloadBytes, Signer: key, Authority: right}
			if len(f.Parents) > 0 {
				d.Parent = f.Parents[0].Short()
			}
			st.Drafts = append(st.Drafts, d)
		}
	}

	for _, id := range led.ActiveGrants() {
		e, ok := led.Get(id)
		if !ok {
			continue
		}
		g, err := event.DecodeGrant(e.Event.Payload)
		if err != nil {
			continue
		}
		st.Writing = append(st.Writing, tui.AuthorityItem{
			ID: id.Short(), Subject: shortKey(g.Subject), Scope: scopeName(g.Scope),
			Detail: "may " + strings.Join(g.Verbs, ", "),
		})
	}
	for _, cv := range covenant.ActiveAt(led) {
		st.Disclosure = append(st.Disclosure, tui.AuthorityItem{
			ID: cv.ID.Short(), Subject: shortKey(cv.Subject), Scope: scopeName(cv.Scope), Detail: "may receive",
		})
	}
	for _, tb := range takenBack(led) {
		st.TakenBack = append(st.TakenBack, tui.AuthorityItem{ID: tb.grant.Short(), Subject: tb.revoke.Short(), Detail: "taken back by"})
	}
	return st
}

// keyNames maps the public keys this carrier can name to their names: root
// for the ledger's root, else the keyring's own name for the key.
type keyNames map[string]string

func keyNamesOf(c *carrier.Carrier, led *ledger.Ledger) keyNames {
	// In v1 the other keys' names live in the keyring events, which the key
	// layer folds; the screen names the root and shows other keys by prefix.
	return keyNames{string(led.Root()): "root"}
}

func (k keyNames) of(pub ed25519.PublicKey) string {
	if n, ok := k[string(pub)]; ok {
		return n
	}
	return shortKey(pub)
}

func shortKey(k []byte) string {
	if len(k) < 4 {
		return hex.EncodeToString(k)
	}
	return hex.EncodeToString(k[:4]) + "…"
}

func scopeName(scope string) string {
	if scope == "" {
		return "(the whole ledger)"
	}
	return scope
}
