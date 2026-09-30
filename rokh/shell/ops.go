// Executing the sentences that read and write one ledger.
//
// Nothing here runs on its own: every event is the direct result of one typed
// sentence, and reading sentences change nothing. Accepted is not true;
// verified is not honest; entrusted is not consent; opened-to is not sent.
package shell

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"rokh/content"
	"rokh/covenant"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
	"rokh/oracle"
	"rokh/working"
)

// execute runs one parsed sentence and returns the reply for stdout.
func (s *session) execute(c command) (string, error) {
	switch c.Op {
	case opOpen:
		if err := s.openByName(c.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf(tplOpened, eventCount(s.current.led), countOf(len(s.current.led.Heads()), "head")), nil

	case opOpenNew:
		// A carrier made by rokh init is one ledger; a second is made in a
		// folder of ledgers, not inside this one.
		if s.carrier != "" {
			return "", fmt.Errorf("%s is one ledger, made by rokh init; a new ledger is made in a folder of ledgers: open an empty folder with rokh FOLDER and make it there", s.carrier)
		}
		l, err := s.initLedger(c.Name, c.Sentence)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(tplOpenedNew, l.led.Genesis().String()), nil

	case opClose:
		if err := s.closeAll(); err != nil {
			return "", err
		}
		return tplClosed, nil

	case opWriteClose:
		return s.doWriteClose(c)
	case opCancel:
		return s.doCancel(c)
	case opWrite:
		return s.doWrite(c)
	case opRead:
		return s.doRead(c)
	case opSeeLedger:
		return s.doSeeLedger()
	case opSeeLedgers:
		return s.doSeeLedgers()
	case opSeeGrants:
		return s.doSeeGrants()
	case opEntrustWrite:
		return s.doEntrustWrite(c)
	case opEntrustRead:
		return s.doEntrustRead(c)
	case opTakeBack:
		return s.doTakeBack(c)
	case opImport:
		return s.doImport(c)
	case opReunite:
		return s.doReunite(c)
	case opReconcile:
		return s.doReconcile()
	case opCarryLedger:
		return s.doCarryLedger(c)
	case opCarryAddress:
		return s.doCarryAddress(c)
	case opCarryBundle:
		return s.doCarryBundle(c)
	}
	return "", fmt.Errorf("unhandled op %q", c.Op)
}

// doWrite is "write at": the sentence goes into the working state and the form of
// its effect is shown. Nothing is recorded here. What is on the screen is a
// prediction of the effect, not the effect — it carries no name, because a
// name is the hash of bytes that have not been made.
//
//	— T4.4, T9.2, T8.2, N4.7
func (s *session) doWrite(c command) (string, error) {
	l, err := s.requireOwn()
	if err != nil {
		return "", err
	}
	// The authority is resolved now, so the form can say under what right the
	// writing would happen — and so a sentence that could never be recorded
	// is refused before it is drafted.
	if _, _, err := l.signer(); err != nil {
		return "", err
	}
	d := working.Draft{Address: c.Address, Verb: "note", Payload: []byte(c.Text)}
	// One sentence waits per address. Saying it again for the same address
	// revises it — the working state is not final and changes as much as
	// one wants (T4.4) — while a sentence for another address waits beside
	// it. Each is recorded or let go by its own act.
	at := -1
	for i, w := range s.drafts {
		if prev, err := s.work.Read(w.h); err == nil && prev.Address == c.Address {
			at = i
		}
	}
	var h working.Handle
	if at >= 0 {
		h = s.drafts[at].h
		if err := s.work.Revise(h, d); err != nil {
			return "", err
		}
		s.drafts[at].line = c.Sentence
	} else {
		var err error
		if h, err = s.work.Write(d); err != nil {
			return "", err
		}
		s.drafts = append(s.drafts, waiting{h: h, line: c.Sentence})
		at = len(s.drafts) - 1
	}
	key, right := l.signerName()
	f, err := s.work.Preview(h, l.branchHead(), right)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(tplDrafted, ordinalOf(at+1, len(s.drafts)), f.Address, digits(f.PayloadBytes), key, right), nil
}

// ordinalOf names a waiting sentence by its place: "2 of 3", or just "1"
// when it is alone.
func ordinalOf(n, of int) string {
	if of == 1 {
		return digits(n)
	}
	return digits(n) + " of " + digits(of)
}

// chosen picks the waiting sentence a closing or cancelling sentence names:
// the n-th from the oldest, or the newest when none is named.
func (s *session) chosen(n int) (int, error) {
	if len(s.drafts) == 0 {
		return 0, refusal{code: "nothing_waiting", err: errors.New(`no sentence is waiting; "write at {address}: {text}" puts one there first`)}
	}
	if n == 0 {
		return len(s.drafts) - 1, nil
	}
	if n > len(s.drafts) {
		return 0, refusal{code: "nothing_waiting", err: fmt.Errorf("only %s waiting; there is no sentence %d", countOf(len(s.drafts), "sentence"), n)}
	}
	return n - 1, nil
}

// doWriteClose is the bare "write": the one explicit act that carries a
// waiting sentence across the boundary. It is not a ninth verb — it is the
// same writing, finished — and it is the only path here that records
// anything. With several waiting, the newest goes unless one is named.
//
//	— T4.4, T4.5, T9.2, N4.7
func (s *session) doWriteClose(c command) (string, error) {
	i, err := s.chosen(c.N)
	if err != nil {
		return "", err
	}
	l, err := s.requireOwn()
	if err != nil {
		return "", err
	}
	id, err := s.work.Close(s.drafts[i].h, recorder{l: l})
	if err != nil {
		return "", err // the crossing failed; the sentence is still waiting
	}
	s.drafts = append(s.drafts[:i:i], s.drafts[i+1:]...)
	key, right := l.signerName()
	return fmt.Sprintf(tplWritten, id.String(), key, right, doorName), nil
}

// doCancel lets a waiting sentence go. Nothing was recorded, so nothing is
// undone: the sentence leaves working state and that is all.
//
//	— T4.4
func (s *session) doCancel(c command) (string, error) {
	i, err := s.chosen(c.N)
	if err != nil {
		return "", err
	}
	if err := s.work.Discard(s.drafts[i].h); err != nil {
		return "", err
	}
	s.drafts = append(s.drafts[:i:i], s.drafts[i+1:]...)
	return fmt.Sprintf(tplCancelled, digits(i+1)), nil
}

// doorName is the mark this surface leaves on what it records: the sentence
// surface, as against the command line or a home's doors.
const doorName = "rokh-shell"

// recorder is the far side of the boundary: signing, and the ledger. The
// working state calls it from exactly one place.
//
//	— T4.1
type recorder struct{ l *openLedger }

func (r recorder) Record(d working.Draft) (frame.ID, error) {
	priv, authority, err := r.l.signer()
	if err != nil {
		return frame.ID{}, err
	}
	att, _ := oracle.Observe(oracle.Default())
	att = append(att, event.Attestation{Oracle: "door", Claim: []byte(doorName)})
	anchor := r.l.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Authority: authority, Parents: r.l.branchHead(),
		Address: d.Address, Verb: d.Verb, Payload: d.Payload, Attest: att,
	}, priv)
	if err != nil {
		return frame.ID{}, err
	}
	if err := r.l.commit(e); err != nil {
		return frame.ID{}, err
	}
	return e.ID, nil
}

// doRead is "read": the events at that address and beneath it, in the
// ledger's deterministic order. A content descriptor is resolved from the
// library and hash-verified before anything is shown; a payload is escaped so
// stored bytes can never command the terminal. Reading changes nothing.
func (s *session) doRead(c command) (string, error) {
	l, err := s.require()
	if err != nil {
		return "", err
	}
	var lines []string
	n := 0
	for _, id := range l.led.Order() {
		e, _ := l.led.Get(id)
		if !event.ScopeCovers(c.Address, e.Event.Address) {
			continue
		}
		n++
		lines = append(lines, s.renderEvent(l, id, e))
	}
	if n == 0 {
		return fmt.Sprintf("there is nothing at %s.", c.Address), nil
	}
	out := ""
	for _, ln := range lines {
		out += ln + "\n"
	}
	return out + fmt.Sprintf("%s at %s.", countOf(n, "event"), c.Address), nil
}

// renderEvent is one event on one line. A system event carries the mark
// system in the same line format as any event (contract 4.1).
func (s *session) renderEvent(l *openLedger, id frame.ID, e event.Signed) string {
	if e.System {
		return fmt.Sprintf("— system %s (%s): ", e.Event.Address, id.Short()) + s.describePayload(l, e)
	}
	return fmt.Sprintf("— %s (%s): ", e.Event.Address, id.Short()) + s.describePayload(l, e)
}

// describePayload says what an event carries, in words safe to print: a
// note's text with its control bytes escaped, or what a content descriptor
// points at and whether the library still holds and verifies it. It is the
// one description both surfaces use.
func (s *session) describePayload(l *openLedger, e event.Signed) string {
	head := ""
	if e.HeadOnly {
		return "(sealed; this passphrase does not open what it says)"
	}
	if e.System {
		return systemLine(e)
	}
	if e.Event.Verb == content.VerbPut {
		d, err := content.Decode(e.Event.Payload)
		if err != nil {
			return head + "a payload that cannot be read"
		}
		var buf bytes.Buffer
		if err := content.Fetch(l.car, d, &buf); err != nil {
			return head + fmt.Sprintf("a file, %s bytes %s; not readable here (%s)", digits(int(d.Size)), d.Type, plainText(err))
		}
		body := buf.Bytes()
		if err := d.Verify(body); err != nil {
			return head + "the file is there but does not match its witness — I will not show it"
		}
		if d.Type == content.MediaTypeTree {
			if t, err := content.DecodeTree(body); err == nil {
				return head + fmt.Sprintf("a tree of %s; checked", countOf(len(t.Entries), "file"))
			}
		}
		if d.Size <= 4096 {
			return head + escapePayload(body) + "  (checked)"
		}
		return head + fmt.Sprintf("a file, %s bytes %s; checked", digits(int(d.Size)), d.Type)
	}
	if len(e.Event.Payload) == 0 {
		return head + "(" + e.Event.Verb + ", with no payload)"
	}
	return head + escapePayload(e.Event.Payload)
}

// genesisLine is the first event as one line: the sentence that made the
// ledger, and a word for what is under it, never a line break in the middle.
func genesisLine(payload []byte) string {
	first, rest, more := strings.Cut(string(payload), "\n")
	out := escapePayload([]byte(first))
	switch {
	case !more:
	case strings.HasPrefix(strings.TrimSpace(rest), reserveLine):
		out += "  (and the room set aside)"
	default:
		out += "  (and more lines)"
	}
	return out
}

// plainText is the plain sentence for an error, without its code: for a
// line that names what could not be opened among others that could.
func plainText(err error) string {
	var p plainErr
	if errors.As(Plain(err), &p) {
		return p.text
	}
	return tidy(err.Error())
}

// doSeeLedger is "see the ledger": status and verification in one answer.
func (s *session) doSeeLedger() (string, error) {
	l, err := s.require()
	if err != nil {
		return "", err
	}
	_, r, p := l.led.Tally()
	if r > 0 {
		return "", fmt.Errorf("%s in the ledger rejected; the verify did not pass", countOf(r, "event"))
	}
	if p > 0 {
		fmt.Fprintf(s.notes, "(%s waiting on ancestors — not arrived, not absent)\n", countOf(p, "event"))
	}
	// The three layers, as an aside where auxiliary detail goes: the screen
	// has a view of its own for them, and no sentence is added for them.
	if !s.onScreen {
		sayLayers(s.notes, layersOf(l.led))
	}
	return fmt.Sprintf(tplSeen, eventCount(l.led), countOf(len(l.led.Heads()), "head")), nil
}

// doSeeLedgers is "see the ledgers": the vault's ledgers grouped by anchor, and
// every seat with its three declarations. Custody is stated plainly: warm
// means the root key travels with the carrier; cold means it does not.
func (s *session) doSeeLedgers() (string, error) {
	names := s.ledgerNames()
	if len(names) == 0 && s.mount == "" {
		return "there is no ledger.", nil
	}
	out := ""
	for _, name := range names {
		dir := s.ledgerDir(name)
		c, sec, err := s.openCarrier(dir, s.pass)
		if err != nil {
			out += fmt.Sprintf("— %s: shut; %s\n", name, plainText(err))
			continue
		}
		led, err := replay(c)
		if err != nil {
			out += fmt.Sprintf("— %s: unreadable\n", name)
			continue
		}
		custody := custodyOf(sec)
		out += fmt.Sprintf("— %s · anchor %s · %s · %s · %s\n",
			name, led.Genesis().Short(), eventCount(led),
			countOf(len(led.Heads()), "head"), custody)
	}
	for _, st := range s.readSeats() {
		kind := st.kind
		switch kind {
		case "berth":
			kind = "berth"
		case "mirror":
			kind = "mirror"
		case "":
			kind = "undeclared"
		}
		ruling := st.ruling
		if ruling == "" {
			ruling = "—"
		}
		anchor := st.anchor
		if len(anchor) > 8 {
			anchor = anchor[:8]
		}
		out += fmt.Sprintf("— seat %s · anchor %s · %s · ruling: %s\n", st.name, anchor, kind, escapePayload([]byte(ruling)))
		if st.declErr != "" {
			out += fmt.Sprintf("   (%s)\n", st.declErr)
		}
	}
	return out + "the names belong to this vault; a ledger is known by its anchor.", nil
}

// doSeeGrants is "see the grants": live write grants and open disclosures at
// the heads. Entrusted is not consent; opened-to is not sent.
func (s *session) doSeeGrants() (string, error) {
	l, err := s.require()
	if err != nil {
		return "", err
	}
	out := ""
	n := 0
	for _, id := range l.led.ActiveGrants() {
		e, ok := l.led.Get(id)
		if !ok {
			continue
		}
		g, err := event.DecodeGrant(e.Event.Payload)
		if err != nil {
			continue
		}
		scope := g.Scope
		if scope == "" {
			scope = "(the whole ledger)"
		}
		out += fmt.Sprintf("— a grant to write %s · to %x… · at %s\n", id.Short(), g.Subject[:4], scope)
		n++
	}
	for _, cv := range covenant.ActiveAt(l.led) {
		scope := cv.Scope
		if scope == "" {
			scope = "(the whole ledger)"
		}
		out += fmt.Sprintf("— opened to %x… %s · at %s\n", cv.Subject[:4], cv.ID.Short(), scope)
		n++
	}
	for _, tb := range takenBack(l.led) {
		out += fmt.Sprintf("— taken back: grant %s, by %s\n", tb.grant.Short(), tb.revoke.Short())
	}
	if n == 0 {
		return out + "nothing entrusted, nothing opened to anyone.", nil
	}
	return out + fmt.Sprintf("%s alive.", countOf(n, "covenant")), nil
}

// revoked is one grant that was taken back, and the event that took it.
type revoked struct{ grant, revoke frame.ID }

// takenBack lists the grants taken back in the accepted history, so a person
// sees what closed as well as what stands: taking back closes the future and
// erases nothing, and the screen must not make it look erased.
//
//	— T6.3
func takenBack(led *ledger.Ledger) []revoked {
	var out []revoked
	for _, id := range led.Order() {
		e, ok := led.Get(id)
		if !ok || e.Event.Verb != event.VerbRevoke {
			continue
		}
		if rv, err := event.DecodeRevoke(e.Event.Payload); err == nil {
			out = append(out, revoked{grant: rv.Target, revoke: id})
		}
	}
	return out
}

// doEntrustWrite is "entrust writing": a grant, scope-bound, verbs=note, signed by
// the owner alone. If {who} is a name, a new key is made and kept in the
// keyring; if it is a public key, no key material is stored at all.
func (s *session) doEntrustWrite(c command) (string, error) {
	l, err := s.requireOwn()
	if err != nil {
		return "", err
	}
	root, err := l.rootSigner()
	if err != nil {
		return "", err
	}
	// In v1 a key is made by the key layer and recorded in the keyring
	// (`rokh key add`); the sentence names the public key it entrusts.
	subject, err := l.keyFor(c.Who)
	if err != nil {
		return "", fmt.Errorf("%w. A key is entrusted by its public key; a new key is made on the command line with rokh key add", err)
	}
	payload, err := event.Grant{Subject: subject, Scope: c.Address, Verbs: []string{"note"}}.Encode()
	if err != nil {
		return "", err
	}
	att, _ := oracle.Observe(oracle.Default())
	anchor := l.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Parents: l.branchHead(),
		Address: event.AddressRoot, Verb: event.VerbGrant, Payload: payload, Attest: att,
	}, root)
	if err != nil {
		return "", err
	}
	if err := l.commit(e); err != nil {
		return "", err
	}
	return fmt.Sprintf(tplGranted, e.ID.String()), nil
}

// doEntrustRead is "entrust reading": a share covenant, disclosure not authorship,
// owner-signed and evaluated outside the core. It permits; it does not send.
func (s *session) doEntrustRead(c command) (string, error) {
	l, err := s.requireOwn()
	if err != nil {
		return "", err
	}
	root, err := l.rootSigner()
	if err != nil {
		return "", err
	}
	subject, err := l.keyFor(c.Who)
	if err != nil {
		return "", err
	}
	payload, err := covenant.Share{Subject: subject, Scope: c.Address}.Encode()
	if err != nil {
		return "", err
	}
	att, _ := oracle.Observe(oracle.Default())
	anchor := l.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Parents: l.branchHead(),
		Address: covenant.Address, Verb: covenant.VerbShare, Payload: payload, Attest: att,
	}, root)
	if err != nil {
		return "", err
	}
	if err := l.commit(e); err != nil {
		return "", err
	}
	return fmt.Sprintf(tplShared, e.ID.String()), nil
}

// doTakeBack is "take back": the object is always the recorded id, never "his
// right". A grant id revokes; a share id unshares. From here on, not the past.
func (s *session) doTakeBack(c command) (string, error) {
	l, err := s.require()
	if err != nil {
		return "", err
	}
	id, err := frame.ParseID(c.ID)
	if err != nil {
		return "", errors.New("the id must be whole; a shortened one is not accepted")
	}
	target, ok := l.led.Get(id)
	if !ok || l.led.State(id) != ledger.Accepted {
		return "", errors.New("there is no such event in this ledger")
	}
	root, err := l.rootSigner()
	if err != nil {
		return "", err
	}
	var payload []byte
	var addr, verb string
	switch target.Event.Verb {
	case event.VerbGrant:
		payload, err = event.Revoke{Target: id}.Encode()
		addr, verb = event.AddressRoot, event.VerbRevoke
	case covenant.VerbShare:
		payload, err = covenant.Unshare{Target: id}.Encode()
		addr, verb = covenant.Address, covenant.VerbUnshare
	default:
		return "", errors.New("this is neither a grant nor an opening; there is nothing to take back")
	}
	if err != nil {
		return "", err
	}
	att, _ := oracle.Observe(oracle.Default())
	anchor := l.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Parents: l.branchHead(),
		Address: addr, Verb: verb, Payload: payload, Attest: att,
	}, root)
	if err != nil {
		return "", err
	}
	if err := l.commit(e); err != nil {
		return "", err
	}
	return tplRevoked, nil
}
