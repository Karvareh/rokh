// The session: a temporary machine in RAM.
//
// Opening loads a carrier, verifies everything, and stands at the heads. The
// session lives in memory only; an idle session writes nothing, and there is
// a test in the same style as the daemon's that says so. "leave" closes it and
// confirms the carrier is exactly as the last accepted operation left it.
//
// One session may hold several ledgers open; the vault folder is the
// namespace and sentences address the currently opened ledger. Local names
// are aliases; identity is the anchor. Same anchor, same ledger. Different
// anchor, a different ledger, never mergeable.
//
// The design nudges toward one ledger per person: when nothing is open and
// the vault holds exactly one ledger, it becomes the subject of the sentence
// without ceremony. Creating a second ledger works but is never suggested.
package shell

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
	"rokh/ledger"
	"rokh/oracle"
	"rokh/turn"
	"rokh/vessel"
	"rokh/working"
)

const defaultBranch = "main"

// openLedger is one ledger held open in the session.
type openLedger struct {
	name string
	dir  string
	car  *carrier.Carrier
	led  *ledger.Ledger
	// sec is the key the passphrase holds in this carrier: the owner's slot
	// carries the root signing seed (warm) or none (cold).
	sec key.Secret
	// layer is the key layer the passphrase opened: it judges every record
	// at its own point and gives the session a record is sealed with.
	layer *keyLayer
	// mirror marks a ledger that is somebody else's. It is read and never
	// written: a ledger does not accept another anchor's events, so writing
	// here would not be forbidden so much as impossible, and saying so at the
	// door is kinder than letting every sentence fail at the end.
	//   — T13.4, T8.4
	mirror bool
}

// pendingMerge remembers a reunion that left two heads open, waiting for the
// exact reply that closes them. Nothing merges without it.
type pendingMerge struct {
	ledgerName string
	heads      []frame.ID
	berthDir   string
	berthPass  string
}

// session is the temporary machine.
type session struct {
	vault   string
	mount   string // seats; may be empty
	library string // the star's library; may be empty until a sentence needs it
	pass    string

	open    map[string]*openLedger
	current *openLedger
	pending *pendingMerge
	// work is the boundary between doing and recording. A sentence lands here
	// first; only the closing act carries it across.
	//   — T4.4, T8.2, N4.7
	work *working.State
	// drafts are the sentences waiting in working state, oldest first. Several
	// may wait at once: saying a second sentence sets nothing aside, and each
	// is recorded or let go by its own explicit act.
	drafts []waiting
	// notes is where the session puts its asides — the ledger it opened for
	// you, the seat that would not declare itself. On the line surface that
	// is stderr; on the screen surface it is gathered and shown.
	notes io.Writer
	// unsure is set once a recording in this session ended unknown: what
	// leaving says about the sentences still waiting depends on it.
	unsure bool
	// onScreen is true while the screen draws this session: it shows the
	// three layers as a view of their own, so "see the ledger" does not
	// repeat them as an aside.
	onScreen bool
	// carrier is the folder named when it is itself a carrier, as rokh init
	// makes one: the session's one ledger, named after its folder. Empty for
	// a folder of ledgers, as the gate makes one.
	carrier string
}

// sessionOn is a session on the folder a person named, of either shape:
// a carrier made by rokh init is the one ledger of the session; any other
// folder is a folder of ledgers.
func sessionOn(dir, mount, library, pass string) *session {
	s := newSession(dir, mount, library, pass)
	if isCarrier(dir) {
		s.carrier = dir
	}
	return s
}

// ledgerDir is the folder of the ledger with that name: the carrier itself
// when the session is on one, or the ledger's folder in the folder of
// ledgers.
func (s *session) ledgerDir(name string) string {
	if s.carrier != "" && name == filepath.Base(s.carrier) {
		return s.carrier
	}
	return filepath.Join(s.ledgersDir(), name)
}

func newSession(vault, mount, library, pass string) *session {
	// Opening raises a working state. What is in it is not yet the ledger,
	// and it stays in memory: whether it lives there or on a disk has nothing
	// to do with the architecture — what matters is that nothing crosses on
	// its own.
	//   — T8.2, T4.5
	w, _ := working.Open(working.NewMemory())
	return &session{vault: vault, mount: mount, library: library, pass: pass,
		open: map[string]*openLedger{}, work: w, notes: os.Stderr}
}

// waiting is one sentence in working state: its handle in the working state
// and the sentence as the person typed it, kept so the screen can show them
// their own words while the sentence is still theirs.
type waiting struct {
	h    working.Handle
	line string
}

// custodyOf says what key this carrier holds at hand: warm when the root key
// is here, delegated when only an entrusted key is, cold when none. One word,
// the same on every surface.
func custodyOf(sec key.Secret) string {
	switch {
	case sec.Signer() == nil:
		return "cold"
	case sec.Key == [32]byte{}:
		return "warm"
	}
	return "delegated"
}

// signerName says, for the replies and the screen, which key would sign and
// under what: "the root key" with no grant, or an entrusted key's name and
// the grant it writes under.
func (l *openLedger) signerName() (name, authority string) {
	switch custodyOf(l.sec) {
	case "warm":
		return "root", "the owner's own right"
	case "delegated":
		return hex.EncodeToString(l.sec.Key[:4]), "its own grant"
	}
	return "", "no key at hand"
}

// LedgersFolder is what the folder of ledgers is called inside a vault.
//
// The rule has not changed: it is named in the language the person is being
// spoken to in, because they open it with their own hands and a second name
// for one thing is a lie in the one place they are looking. The surface says
// "see the ledgers", so the folder is "ledgers".
const LedgersFolder = "ledgers"

// ledgersDir is the folder of ledgers inside the vault. A vault from before
// v1, under any other name, holds no v1 carrier and is answered as not a
// Rokh (contract section 7); nothing here looks for it.
func (s *session) ledgersDir() string {
	return filepath.Join(s.vault, LedgersFolder)
}

// contentRoot is the canonical per-anchor place in the star's library:
// <library>/rokh/<anchor>/. The library belongs to the machine, one layer
// below Rokh - it is never inside a carrier and never inside the vault's
// ledgers tree, and both are refused.
func (s *session) contentRoot(anchor frame.ID) (string, error) {
	if s.library == "" {
		return "", errors.New("you have named no library; say where it is with --library")
	}
	lib, err := filepath.Abs(s.library)
	if err != nil {
		return "", err
	}
	led, err := filepath.Abs(s.ledgersDir())
	if err != nil {
		return "", err
	}
	if lib == led || strings.HasPrefix(lib, led+string(filepath.Separator)) {
		return "", errors.New("the library does not sit inside the ledgers")
	}
	// Refuse a library inside any carrier: no vessel at the path or above.
	for p := lib; ; {
		if isCarrier(p) {
			return "", errors.New("the library does not sit inside a carrier")
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	root := filepath.Join(lib, "rokh", anchor.String())
	return root, os.MkdirAll(root, 0o700)
}

// ledgerNames lists the vault's ledgers, sorted.
func (s *session) ledgerNames() []string {
	if s.carrier != "" {
		return []string{filepath.Base(s.carrier)}
	}
	entries, err := os.ReadDir(s.ledgersDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// require returns the current ledger, opening the single home ledger without
// ceremony when nothing is open and exactly one exists.
func (s *session) require() (*openLedger, error) {
	if s.current != nil {
		return s.current, nil
	}
	names := s.ledgerNames()
	if len(names) == 1 {
		if err := s.openByName(names[0]); err != nil {
			return nil, err
		}
		fmt.Fprintf(s.notes, "(opened the ledger %s)\n", names[0])
		return s.current, nil
	}
	if len(names) == 0 {
		return nil, errors.New(`there is no ledger; first "open a new ledger named <name>"`)
	}
	return nil, errors.New(`there is more than one ledger; first "open the ledger <name>"`)
}

// openByName loads a carrier, replays and verifies every event, and stands at
// the heads. Opening writes nothing.
func (s *session) openByName(name string) error {
	if l, ok := s.open[name]; ok {
		s.current = l
		return nil
	}
	dir := s.ledgerDir(name)
	mirror := false
	if _, err := os.Stat(dir); err != nil {
		// Not in the vault. It may be a seat that declared itself a mirror —
		// somebody else's ledger, sitting read-only where they left it.
		//
		// Rokh makes a mirror possible and does not run one: whoever put it
		// there decided to, and this only opens what is already sitting in a
		// seat. A seat that has not declared itself is not opened, and a berth
		// is not opened this way either — a berth is your own copy and is
		// reached by uniting with it, not by being read as though it were
		// another person's.
		//   — T13.4
		seat, found := s.seatNamed(name)
		if !found {
			return fmt.Errorf("there is no ledger named %s, and no seat by that name either", name)
		}
		if seat.declErr != "" {
			return fmt.Errorf("seat %s: %s", name, seat.declErr)
		}
		switch seat.kind {
		case "mirror":
			dir, mirror = seat.dir, true
		case "berth":
			return fmt.Errorf(`seat %s is a berth of your own ledger, not a mirror; say "bring the returned ledger"`, name)
		default:
			return fmt.Errorf("seat %s has not declared itself; I will not touch it", name)
		}
	}
	c, sec, layer, err := s.openLayered(dir, s.pass)
	if err != nil {
		return err
	}
	led, err := replay(c)
	if err != nil {
		return err
	}
	l := &openLedger{name: name, dir: dir, car: c, led: led, sec: sec, layer: layer, mirror: mirror}
	if mirror && led.Genesis().String() != seatAnchorOf(s, name) {
		// The anchor is read off the carrier itself, so a seat that claimed a
		// different one has already been refused by readSeats. This is the
		// second look, because a mirror is the one place where whose ledger
		// this is decides everything.
		return fmt.Errorf("seat %s has a different anchor", name)
	}
	s.open[name] = l
	s.current = l
	return nil
}

// seatNamed finds a declared seat by name.
func (s *session) seatNamed(name string) (seat, bool) {
	for _, st := range s.readSeats() {
		if st.name == name {
			return st, true
		}
	}
	return seat{}, false
}

// seatAnchorOf is the anchor a seat declared, for the second look above.
func seatAnchorOf(s *session, name string) string {
	if st, ok := s.seatNamed(name); ok {
		return st.anchor
	}
	return ""
}

// replay rebuilds the ledger from what the carrier holds now, re-verifying
// everything. No derived state is ever stored.
//
// Another writer on this carrier (the command line, a booth, another
// surface) commits to the folder, not to this opening's memory, so the
// carrier reads its present generation first, as a door does since T10. Under
// the writing turn that is what the carrier holds until this surface's own
// commit point, so the recording judges its parent on what was committed and
// never moves a branch over another writer's event.
func replay(c *carrier.Carrier) (*ledger.Ledger, error) {
	if _, err := c.Refresh(); err != nil {
		return nil, fmt.Errorf("what the carrier holds now could not be read: %w", err)
	}
	return replayHeld(c)
}

// replayHeld rebuilds the ledger from what this opening of the carrier last
// read, without reading the folder again.
func replayHeld(c *carrier.Carrier) (*ledger.Ledger, error) {
	raw, err := c.Get(c.Anchor())
	if err != nil {
		return nil, fmt.Errorf("genesis unreadable: %w", err)
	}
	// The walk follows the references, not the object store: the reference is
	// the commit point, and bytes it does not reach were left behind by a
	// recording that stopped.
	//   — T8.5
	heads, err := carrierHeads(c)
	if err != nil {
		return nil, err
	}
	led, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		return nil, err
	}
	return led, nil
}

// initLedger creates a new ledger in the vault. The first page is the very
// sentence that created it: the human's own explicit act, recorded exactly.
func (s *session) initLedger(name, sentence string) (*openLedger, error) {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return nil, fmt.Errorf("%q cannot be a ledger name", name)
	}
	dir := filepath.Join(s.ledgersDir(), name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("a ledger named %s already exists; open that one", name)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	att, _ := oracle.Observe(oracle.Default())
	gen, err := event.SignFresh(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte(sentence), Attest: att,
	}, priv)
	if err != nil {
		return nil, err
	}
	c, sec, err := createCarrier(dir, s.pass, gen.ID, priv)
	if err != nil {
		return nil, err
	}
	lock, err := turn.Acquire(dir, turnPatience)
	if err != nil {
		return nil, err
	}
	// The first page is what `rokh init` records (T2): the genesis and the
	// owner's first keyring generation, whose add seals the system reader to
	// the owner. A key added here later is given the system reader and opens
	// its own view with its own passphrase, as on a carrier `rokh init` made.
	out, err := keyview.Bootstrap(c, lock, defaultBranch, gen, sec, priv)
	lock.Release()
	if out != vessel.Recorded {
		return nil, fmt.Errorf("the first page was %s: %v", out, err)
	}
	// From here the new ledger is read and sealed as every other one is:
	// through the key layer its owner's passphrase opens.
	sess, layer, err := layerFor(c, sec)
	if err != nil {
		return nil, err
	}
	c.SetSealer(sess)
	led, err := replay(c)
	if err != nil {
		return nil, err
	}
	l := &openLedger{name: name, dir: dir, car: c, led: led, sec: sec, layer: layer}
	s.open[name] = l
	s.current = l
	return l, nil
}

// closeAll ends the session: nothing new is flushed, and each carrier is
// re-read from disk and compared against the in-memory ledger, confirming it
// is exactly as the last accepted operation left it.
func (s *session) closeAll() error {
	var firstErr error
	for _, l := range s.open {
		// What this session's own operations left, as this opening read it:
		// another writer's later recordings are theirs, not a divergence.
		fresh, err := replayHeld(l.car)
		if err == nil {
			a1, r1, p1 := l.led.Tally()
			a2, r2, p2 := fresh.Tally()
			if a1 != a2 || r1 != r2 || p1 != p2 {
				err = fmt.Errorf("carrier %s diverged from the session", l.name)
			}
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.open = map[string]*openLedger{}
	s.current, s.pending = nil, nil
	return firstErr
}

// signer picks the key a sentence signs with: root when warm, else the
// device key, else the single delegated key. Cold ledgers refuse plainly.
func (l *openLedger) signer() (ed25519.PrivateKey, *frame.ID, error) {
	switch custodyOf(l.sec) {
	case "warm":
		return l.sec.Signer(), nil, nil
	case "delegated":
		return nil, nil, errors.New("this passphrase is an entrusted key's, and this surface signs only with the owner's key; write with the command line: rokh write")
	}
	return nil, nil, errors.New("no key is at hand; this ledger is cold")
}

// rootSigner is for sentences only the owner may speak: entrust, take back.
func (l *openLedger) rootSigner() (ed25519.PrivateKey, error) {
	if custodyOf(l.sec) != "warm" {
		return nil, errors.New("the root key is not in this ledger; a cold ledger cannot do this")
	}
	return l.sec.Signer(), nil
}

// turnPatience is how long a sentence waits for another writer to finish.
const turnPatience = 15 * time.Second

// commit stores an event only after the ledger accepts it, then moves the
// branch. A rejected event never reaches the carrier.
//
// It takes the carrier's writing turn first and reads what is recorded now,
// because the command line and the daemon write this carrier too. An event
// signed on a head that another writer has since moved past is not recorded:
// moving the branch onto it would leave the other writer's event behind no
// reference. The sentence says so, and the draft is still waiting.
//
// And when a step fails, the answer is read back from the carrier rather than
// taken from the step: recorded if the reference landed after all, not
// recorded if it did not.
//
//	— T8.5, T4.4
func (l *openLedger) commit(e event.Signed) error { return l.commitWith(e, nil) }

// commitWith is commit with more records in the same vessel commit, put in
// by extra before the event: content that the event names.
func (l *openLedger) commitWith(e event.Signed, extra func(*carrier.Recording) error) error {
	lock, err := turn.Acquire(l.dir, turnPatience)
	if err != nil {
		return l.failed(err, "not recorded")
	}
	defer lock.Release()
	fresh, err := replay(l.car)
	if err != nil {
		return l.failed(err, "not recorded")
	}
	l.led = fresh
	for _, h := range l.branchHead() {
		if !slices.Contains(e.Event.Parents, h) {
			return errors.New("the ledger moved while this was being written: another writer recorded first, and nothing was recorded; say it again")
		}
	}
	st, err := l.led.Add(e.Raw)
	if err != nil || st != ledger.Accepted {
		why, said := l.led.Why(e.ID)
		l.reread()
		if err != nil {
			return refusal{code: "not_accepted", record: "not recorded", err: err}
		}
		if st == ledger.Pending {
			return refusal{code: "ancestry_pending", record: "not recorded",
				err: errors.New("the event's ancestry is not on this carrier, so it did not settle; not arrived is not the same as not existing")}
		}
		if said {
			return refusal{code: "rejected", record: "not recorded", err: errors.New("the ledger refused it: " + why)}
		}
		return refusal{code: "rejected", record: "not recorded", err: fmt.Errorf("the event was %s and did not settle on the carrier", st)}
	}
	out, err := l.recordAtPoint(lock, e, extra)
	switch out {
	case vessel.Recorded:
		return nil
	case vessel.Unknown:
		return l.failed(err, "unknown")
	}
	if err == nil {
		err = errors.New("the vessel did not record it")
	}
	return l.afterFailure(e.ID, err)
}

// recordAtPoint records e, and what extra puts into the same commit, sealed
// at e's own point: its parents, as the ledger just read holds them, so each
// record is sealed to the readers of that point and not to a keyring read
// when this surface opened (contract E3, E4; T2).
func (l *openLedger) recordAtPoint(lock *turn.Lock, e event.Signed, extra func(*carrier.Recording) error) (vessel.Outcome, error) {
	var sealer carrier.Sealer
	if l.layer != nil {
		sl, err := l.layer.sealerAt(l.led, l.sec, l.car.Vessel().SharedKey(), e.Event.Parents)
		if err != nil {
			return vessel.NotRecorded, fmt.Errorf("it cannot be sealed at its own point: %w", err)
		}
		sealer = sl
	}
	return recordSealed(l.car, lock, e, defaultBranch, extra, sealer)
}

// reread takes the view back to what the references reach.
func (l *openLedger) reread() {
	if fresh, err := replay(l.car); err == nil {
		l.led = fresh
	}
}

// afterFailure answers a recording whose step failed by reading the carrier
// back. If the carrier cannot be read, it says the ending is unknown.
//
//	— T8.5
func (l *openLedger) afterFailure(id frame.ID, cause error) error {
	fresh, err := replay(l.car)
	if err != nil {
		return l.failed(cause, "unknown")
	}
	l.led = fresh
	if fresh.State(id) == ledger.Accepted {
		return nil
	}
	return l.failed(cause, "not recorded")
}

// failed is a recording that did not go through, said plainly: what the
// cause means to a person, whether anything was recorded, and the code the
// cause carries (a full Rokh is vessel_full), or storage_failed.
func (l *openLedger) failed(cause error, record string) error {
	w, known := wordsFor(cause, l)
	code, text := w.code, w.text
	if !known {
		text = tidy(cause.Error()) + "."
	}
	if code == "" || code == "passphrase_refused" || code == "not_a_vessel" || code == "already_exists" {
		code = "storage_failed"
	}
	switch record {
	case "unknown":
		// The plain sentence of a cause may say nothing was recorded; here
		// that is exactly what is not known, so it is not said.
		text = "the recording failed (" + strings.TrimSuffix(tidy(cause.Error()), ".") +
			"), and whether it was recorded is unknown: the ledger could not be read back. Read the ledger again before saying it again."
	default:
		if !strings.Contains(strings.ToLower(text), "nothing was recorded") {
			text += " Nothing was recorded."
		}
	}
	return refusal{code: code, record: record, err: plainErr{text: text, cause: cause}}
}

// branchHead is the head the next sentence rests on: the branch as the
// carrier holds it now, read afresh, because another writer may have
// recorded since this surface last read the folder. Under the writing turn
// the commit reads the carrier again (replay) and refuses a sentence that no
// longer rests on the branch's head; nothing is recorded over another
// writer's event.
func (l *openLedger) branchHead() []frame.ID {
	_, _ = l.car.Refresh() // a folder that cannot be read now is refused under the turn
	if id, ok, err := l.car.Ref(defaultBranch); err == nil && ok {
		return []frame.ID{id}
	}
	return []frame.ID{l.led.Genesis()}
}

// keyFor resolves {who}: a 64-hex public key, or a keyring name whose public
// key is derived from the stored private key.
func (l *openLedger) keyFor(who string) (ed25519.PublicKey, error) {
	if b, err := hex.DecodeString(who); err == nil && len(b) == ed25519.PublicKeySize {
		return ed25519.PublicKey(b), nil
	}
	return nil, fmt.Errorf("%q is neither a key at hand nor a well-formed public key", who)
}

// seat is one entry in the Mount folder: a neutral opening place that must
// declare itself before any operation is offered.
//
// Two kinds, and the difference is the whole of what Rokh owes the mirror
// ruling. A berth is a copy of your own ledger and may be united with. A
// mirror holds somebody else's, read-only, verified against its own anchor,
// and is never merged into the host — a ledger does not accept another
// anchor's events, and no seat makes it.
//
// Rokh makes a mirror possible; it does not run one. Whether anybody offers
// that service, and to whom, is not Rokh's to decide: inside one person's own
// machines a mirror is a backup, and beyond them it is somebody's choice.
//
// The anchor is read off the carrier's own public face and never off the
// seat's claim about itself, so a seat cannot lie about whose ledger it holds.
//
//	— T13.4, T8.4
type seat struct {
	name    string
	dir     string
	anchor  string // from the carrier's own rokh.json; the source of truth
	kind    string // "berth" | "mirror" | "" (undeclared)
	ruling  string // the recorded human ruling it rests on
	declErr string
}

// seatDecl is the declaration file beside the carrier. It is the owner's
// file, not Rokh's: the carrier footprint is unchanged by it.
type seatDecl struct {
	Anchor string `json:"anchor,omitempty"`
	Kind   string `json:"kind"`
	Ruling string `json:"ruling"`
}

const seatDeclName = "seat.json"

// readSeats lists the seats in the mount folder with their declarations.
func (s *session) readSeats() []seat {
	if s.mount == "" {
		return nil
	}
	entries, err := os.ReadDir(s.mount)
	if err != nil {
		return nil
	}
	var out []seat
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		st := seat{name: e.Name(), dir: filepath.Join(s.mount, e.Name())}
		// The anchor comes from the carrier itself, opened, never from the
		// claim: a v1 vessel shows no anchor without a key.
		if isCarrier(st.dir) {
			if c, _, err := s.openCarrier(st.dir, s.pass); err == nil {
				st.anchor = c.Anchor().String()
			}
		}
		if raw, err := os.ReadFile(filepath.Join(st.dir, seatDeclName)); err == nil {
			var d seatDecl
			if err := json.Unmarshal(raw, &d); err != nil {
				st.declErr = "seat.json unreadable"
			} else {
				st.kind, st.ruling = d.Kind, d.Ruling
				if d.Anchor != "" && st.anchor != "" && d.Anchor != st.anchor {
					st.declErr = "declared anchor does not match the carrier"
				}
			}
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// carrierHeads is every branch tip the carrier names — the commit points.
//
//	— T8.5
func carrierHeads(c *carrier.Carrier) ([]frame.ID, error) {
	refs, err := c.Refs()
	if err != nil {
		return nil, err
	}
	out := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out, nil
}

// requireOwn is require for a sentence that would write.
//
// A mirror is somebody else's ledger. Their events name their anchor, and a
// ledger does not accept another anchor's events — so writing here is not
// forbidden by policy, it is impossible by construction. Saying so at the door
// is kinder than signing something and watching it be refused at the end.
//
//	— T13.4, T8.4
func (s *session) requireOwn() (*openLedger, error) {
	l, err := s.require()
	if err != nil {
		return nil, err
	}
	if l.mirror {
		return nil, fmt.Errorf("%q is a mirror — somebody else's ledger. It can be read from, "+
			"not written to", l.name)
	}
	return l, nil
}
