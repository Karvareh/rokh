// Package daemon serves one opened carrier over a Unix socket.
//
// # Shape
//
// Newline-delimited JSON, one request per line, one response per line. No
// framework, no code generation, no schema compiler. Any language that can
// open a Unix socket and encode JSON can read from and write to Rokh.
//
//	{"op":"status"}
//	{"ok":true,"anchor":"...","accepted":6,"heads":["..."]}
//
// The socket is Unix-domain only and created with mode 0600. There is no TCP
// listener anywhere in Rokh and no code path that opens one. A daemon that
// cannot be reached from the network cannot be reached from the network by
// mistake either.
//
// # No automatic events, ever
//
// This package has no timer, no watcher, no poller and no background
// goroutine that writes. The only thing that can put an event into the ledger
// is an explicit request on the socket, which is itself the result of an
// explicit human command. An idle daemon writes nothing, and there is a test
// that says so.
//
// That is the whole point: an event is a person's decision to record a
// change. Until someone records it, a change is temporary and outside the
// ledger.
//
// # Signing
//
// Two write paths, deliberately separate:
//
//   - append takes bytes that are already signed. No key is involved, so the
//     daemon is not a signing oracle. This is the generic path any other
//     program should prefer.
//   - write builds and signs with a named key from the carrier keyring. It is
//     a convenience for the local CLI. Pass AllowSign=false to remove it, and
//     it is gone.
package daemon

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"rokh/announce"
	"rokh/answer"
	"rokh/booth"
	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/harness"
	"rokh/keyview"
	"rokh/ledger"
	"rokh/oracle"
	"rokh/turn"
	"rokh/vessel"
)

// MaxLine bounds one request line. A whole event in hex is twice
// frame.MaxFrame; the rest is envelope.
const MaxLine = 64 << 10

// Options configure a server.
type Options struct {
	ReadOnly  bool // refuse append and write
	AllowSign bool // enable the write op, which uses keyring keys
	// NoRoot makes this a program's door: nothing here signs with the root
	// key, whether a request names it or names nothing.
	NoRoot bool
	// Dir is the carrier's directory. When it is set, every recording takes
	// the carrier's writing turn, which every other writer on this machine
	// takes too; without it only this process is kept in line.
	Dir string
	// Release names the build, for the capabilities op. Empty says so.
	Release string
	// Door names this door in every event it records, as an attestation the
	// signer makes about where it signed — "rokh-home/program" beside a
	// program's key, "rokh" beside the command line's. It is the registrar's
	// mark, kept apart from the author (the key) and the authority (the
	// grant): three questions, three fields. Empty leaves no mark.
	//   — T10.2, T12.5
	Door string
	// Keys, when set, is where every named key other than the root comes from,
	// instead of the carrier's keyring: a program that embeds this door keeps
	// the keys of the programs it serves in its own sealed storage and hands
	// out one by name. The keyring is not consulted for those names then.
	Keys KeySource
	// Named, when set, names public keys beside whatever Keys or the keyring
	// name, for display only: a door that never signs with a program's key
	// can still say, on a listing, that a program's key signed an event.
	// Nothing is ever signed through it.
	Named KeySource
	// Root is the owner's signing key, held by whoever opened the carrier with
	// the owner's passphrase; nil in cold custody and for a key's session.
	Root ed25519.PrivateKey
	// Owner is the host's hold on the vessel for a door without a Dir: every
	// recording is one vessel commit made while the owner holds (C9).
	Owner vessel.Owner
	// SealerAt, when set, is the session an event this door records is
	// sealed with: the key layer's session at the event's own point, its
	// parents, judged in the door's ledger (contract E3, E4, 3.3). The door
	// asks it for every recording, so a keyring that changed since the door
	// opened is the keyring the record is sealed by. Without it the
	// carrier's own sealer seals.
	SealerAt func(l *ledger.Ledger, parents []frame.ID) (carrier.Sealer, error)
	// Layer is the key layer the door's own view was read through: the
	// owner's, or the key's whose passphrase opened the door. A door opened
	// with a key serves every session at most that key's view (B4); the
	// layer's system reader is what a session that gave no reader, and a
	// guest, are answered from (keyview.SystemView).
	Layer *keyview.Layer
	// ReadOn, when set, brings the door's ledger up to what the carrier
	// holds now in place of the walk through the carrier's sealer: the key
	// layer's reading of the same ledger, every event at its own point
	// (keyview.Layer.ReadOn). A booth's per-session views read through it.
	ReadOn func() ([]frame.ID, error)
	// Live, when set, says whether the key a view is read for is still live;
	// a wait on that view ends when it is not.
	Live func() bool
}

// keyRoot is the name the owner's signing key is asked for by.
const keyRoot = "root"

// KeySource hands out signing keys by name. Key returns a copy the caller may
// wipe, and the grant the key signs under; Names lists what it holds.
type KeySource interface {
	Key(name string) (ed25519.PrivateKey, *frame.ID, bool)
	Names() []string
}

// Server holds the opened carrier and the in-memory ledger.
//
// Requests that read share the server; requests that record have it alone.
// Several programs asking at once are answered at once, and a recording
// waits for no reader longer than a reader takes.
type Server struct {
	mu   sync.RWMutex
	car  *carrier.Carrier
	led  *ledger.Ledger
	opts Options
	// attempts is the index of every recorded attempt, from the name half
	// of its claim to the event that carries it. It is a view of the ledger
	// and nothing more: rebuilt whenever the ledger is, brought up whenever
	// it is extended, so the question "was this name recorded?" is one
	// lookup rather than a walk.
	attempts map[[32]byte]frame.ID
	// changed is closed and replaced whenever the view gains an event, so a
	// wait op can sleep until then rather than ask again and again.
	changed chan struct{}
	// unsettled marks a view nobody could vouch for: a recording failed and the
	// carrier could not be read back, so the view is rebuilt before anything is
	// answered from it.
	unsettled bool
	// bound is the harnesses that have declared themselves here. The core
	// weighs bytes, signature and authority; it does not know what a verb
	// means and does not guess. A harness that wants Rokh to carry its
	// meaning says four things first, and this is where they are kept.
	//   — T11.10, T13.5
	bound *harness.Register
	// lock is the writer's turn this server holds for the request in hand.
	lock *turn.Lock
	// viewMu guards sysView, the door over what the system reader opens,
	// made at its first question (views.go).
	viewMu  sync.Mutex
	sysView *Server
}

// New builds a server over an already-opened carrier and its ledger.
func New(car *carrier.Carrier, led *ledger.Ledger, opts Options) *Server {
	s := &Server{car: car, led: led, opts: opts, bound: harness.NewRegister(),
		changed: make(chan struct{})}
	s.reindex()
	return s
}

// reindex rebuilds the attempt index from the whole ledger.
func (s *Server) reindex() {
	s.attempts = map[[32]byte]frame.ID{}
	for _, id := range s.led.Order() {
		s.index(id)
	}
}

// index notes the attempt an accepted event carries, if it carries one.
func (s *Server) index(id frame.ID) {
	e, found := s.led.Get(id)
	if !found {
		return
	}
	for _, a := range e.Event.Attest {
		if a.Oracle == attemptOracle && len(a.Claim) == 64 {
			var who [32]byte
			copy(who[:], a.Claim[:32])
			s.attempts[who] = id
		}
	}
}

// moved tells every waiter that the view gained an event.
func (s *Server) moved() {
	close(s.changed)
	s.changed = make(chan struct{})
}

type request struct {
	Op      string `json:"op"`
	ID      string `json:"id,omitempty"`
	Raw     string `json:"raw,omitempty"`
	Address string `json:"address,omitempty"`
	Verb    string `json:"verb,omitempty"`
	Message string `json:"message,omitempty"`
	Payload string `json:"payload,omitempty"` // base64, wins over message
	Key     string `json:"key,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	// Attempt is the caller's own name for this recording; ExpectHeads, when
	// present, holds the request to the heads it was prepared against; Author
	// names whose attempt the attempt op asks about.
	//   — T8.5, T4.4
	Attempt     string    `json:"attempt,omitempty"`
	ExpectHeads *[]string `json:"expect_heads,omitempty"`
	Author      string    `json:"author,omitempty"`
	// After is a cursor for log and wait: the id of the last event the
	// caller has read, so it is handed what came after and nothing twice.
	// Timeout bounds a wait, in seconds.
	After   string `json:"after,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
	// The four clauses of a harness's covenant, for the bind op.
	//   — T11.10
	Namespace string   `json:"namespace,omitempty"`
	Version   string   `json:"version,omitempty"`
	Can       []string `json:"can,omitempty"`
	Unknown   string   `json:"unknown,omitempty"`
	// And the four things it declares about its effects, which land somewhere
	// Rokh does not rule.
	//   — T10.7
	Repeat     string `json:"repeat,omitempty"`
	Retry      string `json:"retry,omitempty"`
	Compensate string `json:"compensate,omitempty"`
	Ending     string `json:"ending,omitempty"`
	// What a harness hands back, for the answer op: whose ground it was built
	// on, what it says and where each saying comes from, what it did and under
	// which authority.
	//   — T2, T2.1, T2.2, T2.3
	Anchor string    `json:"anchor,omitempty"`
	Claims []claimIn `json:"claims,omitempty"`
	Acts   []actIn   `json:"acts,omitempty"`
}

type claimIn struct {
	Saying string   `json:"saying"`
	From   []string `json:"from,omitempty"`
}

type actIn struct {
	Doing     string `json:"doing"`
	Authority string `json:"authority,omitempty"`
	Receipt   string `json:"receipt,omitempty"`
}

func fail(err error) map[string]any {
	return refusal(codeOf(err, "refused"), err)
}

func ok(kv map[string]any) map[string]any {
	if kv == nil {
		kv = map[string]any{}
	}
	kv["ok"] = true
	return kv
}

// Handle answers one request. It is exported so the protocol can be tested
// without a socket.
//
// A request that can put an event on the carrier takes the carrier's writing
// turn first and holds it to the end, and reads the view from the carrier under
// it: the heads a precondition names, the attempt a request repeats and the
// parent a new event takes are all read where no other writer can move them
// before the commit point. Its answer always says, in one word, what it
// recorded.
//
//	— T8.5, T4.4
func (s *Server) Handle(line []byte) map[string]any {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return refusal("bad_request", fmt.Errorf("bad request: %w", err))
	}
	writing := writingOps[req.Op]
	if !writing && !exclusiveOps[req.Op] {
		// A reader shares the server with every other reader. It checks
		// that the view is current under the shared lock; if the carrier
		// moved, it steps out and comes back through the door below, where
		// the view can be rebuilt.
		s.mu.RLock()
		release, bad := s.takeTurn(req.Op)
		if bad != nil {
			s.mu.RUnlock()
			return bad
		}
		stale, err := s.stale()
		if err == nil && !stale {
			r := s.dispatch(req, line)
			if release != nil {
				release()
			}
			s.mu.RUnlock()
			return r
		}
		if release != nil {
			release()
		}
		s.mu.RUnlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, bad := s.takeTurn(req.Op)
	if bad != nil {
		return bad
	}
	if release != nil {
		defer release()
	}
	if err := s.refresh(); err != nil {
		r := refusal("carrier_unreadable", err)
		if writing {
			r["record"] = NotRecorded
		}
		return r
	}
	r := s.dispatch(req, line)
	if writing {
		if _, said := r["record"]; !said {
			// Every path that reached the carrier said what it recorded; a
			// path that did not reach it recorded nothing.
			r["record"] = NotRecorded
		}
	}
	return r
}

// exclusiveOps record nothing but change what the server holds — or, for
// wait, let go of it and take it again — and so have it alone like a
// recording does.
var exclusiveOps = map[string]bool{"bind": true, "wait": true}

func (s *Server) dispatch(req request, line []byte) map[string]any {
	switch req.Op {
	case "status":
		return s.status()
	case "capabilities":
		return s.capabilities()
	case "log":
		return s.log(req)
	case "get":
		return s.get(req.ID)
	case "announce":
		return s.announce()
	case "bind":
		return s.bind(req)
	case "bound":
		return s.listBound()
	case "answer":
		return s.answer(req)
	case "attempt":
		return s.attempt(req)
	// A receipt is two signed events, so intent and outcome refuse under
	// ReadOnly and under !AllowSign. The guard is mayRecord, in daemon/receipt.go,
	// because all three share the decode below it. receipts signs nothing and
	// stores nothing, so it stands with status and log.
	//   — T10, T10.1
	case "intent":
		return s.intent(line)
	case "outcome":
		return s.outcome(line)
	case "receipts":
		return s.receipts(line)
	case "wait":
		return s.wait(req)
	case "append":
		if s.opts.ReadOnly {
			return notRecorded("read_only", errors.New("read-only"))
		}
		return s.append(req)
	case "write":
		if s.opts.ReadOnly {
			return notRecorded("read_only", errors.New("read-only"))
		}
		if !s.opts.AllowSign {
			return notRecorded("signing_disabled", errors.New("signing disabled; use append with pre-signed bytes"))
		}
		return s.write(req)
	default:
		// The ops are named here rather than left to be guessed. A socket
		// whose vocabulary exists only in this switch is a socket only its
		// author can speak to, and an error that withholds the one list it
		// holds is the place that happens.
		return refusal("unknown_op", fmt.Errorf("unknown op %q; the ops are %s",
			req.Op, strings.Join(ops, ", ")))
	}
}

// ops is every op this switch answers, for the refusal above.
var ops = []string{"status", "capabilities", "log", "get", "announce", "bind", "bound",
	"answer", "attempt", "intent", "outcome", "receipts", "wait", "append", "write"}

// refresh rebuilds the ledger when a reference on the carrier names an event
// the ledger has never heard of.
//
// The carrier is the ledger's truth and the ledger is a view of it, held in
// memory for as long as this process runs. Anything else may write to that
// carrier — the lock file is human discipline, and the CLI works directly on
// the carrier by design, so a person jotting one line while a daemon is up is
// the ordinary case rather than a strange one.
//
// It used to wedge, permanently and silently. branchHead reads the reference
// from disk, so a write after that arrived carrying a parent this ledger did
// not hold, and every write from then on came back "event pending; not
// stored" — which named the wrong cause. Nothing was pending: the ancestry had
// arrived and was sitting on the carrier unread. The verdict never reopens, so
// nothing short of a restart recovered, and nothing said to restart.
//
// Re-reading is not automation. No event is created here and none could be: it
// follows the references and re-accepts what they reach, which is the same
// walk opening the carrier does, and the same walk that decides the commit
// point. An idle daemon still writes nothing.
//
// The references are the carrier's present ones. An opened carrier keeps what
// it read in memory, and another writer — another door on its own opening of
// the folder, the command line, another process — commits to the folder, not
// to this memory; so the carrier reads its present generation first. Under
// the writing turn that is what the carrier holds until this door's own
// commit point, so a precondition is judged, an attempt looked for and a
// parent taken on what was committed, never on an older reading that a
// commit would then move a branch over.
//
//	— T8.5, T4, T4.4, T12.3
func (s *Server) refresh() error {
	if _, err := s.car.Refresh(); err != nil {
		return fmt.Errorf("the carrier may have moved under this door, and what it holds now could not be read: %w", err)
	}
	stale, err := s.behind()
	if err != nil {
		return err
	}
	if !stale {
		return nil
	}
	if s.opts.ReadOn != nil {
		// The key layer reads this very ledger on, every event at its own
		// point, with the view's own readers.
		added, err := s.opts.ReadOn()
		if err != nil {
			return fmt.Errorf("the carrier moved under this view and reading it on failed: %w", err)
		}
		for _, id := range added {
			s.index(id)
		}
		if len(added) > 0 {
			s.moved()
		}
		return nil
	}
	if s.unsettled {
		// A recording whose ending could not be read back left a view nobody
		// can vouch for, and it is rebuilt whole before anything is answered
		// from it.
		//   — T8.5
		if err := s.reload(); err != nil {
			s.unsettled = true
			return fmt.Errorf("the carrier moved under this daemon and re-reading it failed: %w", err)
		}
		return nil
	}
	// The references moved forward: read only what they reach that the view
	// does not hold. It is the walk opening a carrier makes, cut short at
	// every event already held, and it creates nothing.
	refs, err := s.car.Refs()
	if err != nil {
		return fmt.Errorf("cannot read the carrier's references: %w", err)
	}
	heads := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		heads = append(heads, id)
	}
	added, err := s.led.Extend(s.car.Get, heads)
	if err != nil {
		// The short walk found something it could not read. The whole walk
		// is the fallback, and if that fails too the view is not vouched for.
		if rerr := s.reload(); rerr != nil {
			s.unsettled = true
			return fmt.Errorf("the carrier moved under this daemon and re-reading it failed: %w", rerr)
		}
		return nil
	}
	for _, id := range added {
		s.index(id)
	}
	if len(added) > 0 {
		s.moved()
	}
	return nil
}

// stale reports whether this view may not be the carrier's present one:
// another writer committed since this door's carrier last read the folder,
// the references reach an event the view does not hold, or the view is one
// nobody vouched for. It reads and changes nothing, so a reader may ask under
// the shared lock; a stale view is brought up by refresh, under the lock held
// alone, before anything is answered from it.
func (s *Server) stale() (bool, error) {
	if s.car.Moved() {
		return true, nil
	}
	return s.behind()
}

// behind reports whether the references, as this door's carrier last read
// them, reach an event this view does not hold, or the view is one nobody
// vouched for. It changes nothing.
func (s *Server) behind() (bool, error) {
	refs, err := s.car.Refs()
	if err != nil {
		// A carrier whose references cannot be read is not a reason to answer
		// out of a view that may be stale. Say so.
		return false, fmt.Errorf("cannot read the carrier's references: %w", err)
	}
	if s.unsettled {
		return true, nil
	}
	for _, id := range refs {
		if !s.led.Has(id) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) status() map[string]any {
	acc, rej, pend := s.led.Tally()
	heads := []string{}
	for _, h := range s.led.Heads() {
		heads = append(heads, h.String())
	}
	grants := []string{}
	for _, g := range s.led.ActiveGrants() {
		grants = append(grants, g.String())
	}
	// A branch reference that names an event the ledger does not hold as
	// accepted — refused when its body overturned its head, or never proven —
	// is not served as a head (R1.3); it is named apart with its state.
	branches := map[string]string{}
	unheld := map[string]string{}
	if refs, err := s.car.Refs(); err == nil {
		for name, id := range refs {
			if s.led.State(id) == ledger.Accepted {
				branches[name] = id.String()
			} else {
				unheld[name] = id.String() + " " + s.led.State(id).String()
			}
		}
	}
	// What the references reached and could not be proven (C8, R4): damage
	// to the vessel shows here, not only as a few pending events.
	unproven := s.led.Unproven()
	if unproven == nil {
		unproven = []string{}
	}
	r := ok(map[string]any{
		"anchor":   s.led.Genesis().String(),
		"root":     hex.EncodeToString(s.led.Root()),
		"accepted": acc,
		"rejected": rej,
		"pending":  pend,
		"heads":    heads,
		"branches": branches,
		"grants":   grants,
		"unproven": unproven,
	})
	if len(unheld) > 0 {
		r["branches_not_accepted"] = unheld
	}
	return r
}

// log lists accepted events in the ledger's deterministic order.
//
// Without a cursor it is the last limit events of the whole order, as it
// always was. With after — the id of the last event the caller has read — it
// is what comes after that event, at most limit of them, so a program that
// keeps its cursor reads each event once. address narrows the listing to one
// place and what lies under it (the aperture of T10.6: a harness reads its
// own shelf and its own space); verb narrows it to one verb. Each row names
// the key that signed, the grant it signed under, and the door that
// recorded, where these are known — three answers to three questions, kept
// apart.
//
//	— T10.6, T12.5
func (s *Server) log(req request) map[string]any {
	var order []frame.ID
	if req.After != "" {
		after, err := frame.ParseID(req.After)
		if err != nil {
			return refusal("bad_request", fmt.Errorf("after: %w", err))
		}
		// The zero id is "from the beginning": a cursor a program can hold
		// before it has read anything.
		if _, known := s.led.Position(after); !known && !after.IsZero() {
			return refusal("not_found", fmt.Errorf("after: %s is not an accepted event here", after.Short()))
		}
		if req.Address == "" && req.Verb == "" {
			order = s.led.After(after, req.Limit)
		} else {
			order = s.led.After(after, 0)
		}
	} else {
		order = s.led.Order()
	}
	filtered := order[:0:0]
	for _, id := range order {
		e, _ := s.led.Get(id)
		if req.Address != "" && !event.ScopeCovers(req.Address, e.Event.Address) {
			continue
		}
		if req.Verb != "" && e.Event.Verb != req.Verb {
			continue
		}
		filtered = append(filtered, id)
	}
	order = filtered
	if req.Limit > 0 && req.Limit < len(order) {
		if req.After != "" {
			order = order[:req.Limit]
		} else {
			order = order[len(order)-req.Limit:]
		}
	}
	aliases := s.aliases()
	events := make([]map[string]any, 0, len(order))
	for _, id := range order {
		e, _ := s.led.Get(id)
		events = append(events, s.row(id, e, aliases))
	}
	r := ok(map[string]any{"events": events})
	if len(order) > 0 {
		r["last"] = order[len(order)-1].String()
	}
	return r
}

// row is one event as log shows it. An event this view holds as its head
// alone is shown so: its id, author, grant, parents and system mark, and
// nothing of a body it does not hold (contract 3.3).
func (s *Server) row(id frame.ID, e event.Signed, aliases map[string][]alias) map[string]any {
	if e.HeadOnly {
		parents := []string{}
		for _, p := range e.Event.Parents {
			parents = append(parents, p.String())
		}
		head := map[string]any{"id": id.String(), "author": hex.EncodeToString(e.Event.Author),
			"system": e.System, "parents": parents, "head_only": true}
		if j := s.led.Judged(id).String(); j != "" {
			head["judged"] = j
		}
		if e.Event.Authority != nil {
			head["authority"] = e.Event.Authority.String()
		}
		return head
	}
	row := map[string]any{
		"id":      id.String(),
		"address": e.Event.Address,
		"verb":    e.Event.Verb,
		"author":  hex.EncodeToString(e.Event.Author),
		// The system mark and how far the event was judged, in the same line
		// as every other event (contract 4.1, 3.3).
		"system": e.System,
	}
	if j := s.led.Judged(id).String(); j != "" {
		row["judged"] = j
	}
	if e.Event.Authority != nil {
		row["authority"] = e.Event.Authority.String()
	}
	// One key may be held under several names, one per grant; the grant the
	// event signed under says which name signed it.
	if names := aliases[string(e.Event.Author)]; len(names) > 0 {
		row["key"] = names[0].name
		for _, n := range names {
			if e.Event.Authority != nil && n.authority == e.Event.Authority.String() {
				row["key"] = n.name
			}
		}
	}
	if len(e.Event.Payload) > 0 {
		row["payload"] = base64.StdEncoding.EncodeToString(e.Event.Payload)
	}
	parents := []string{}
	for _, p := range e.Event.Parents {
		parents = append(parents, p.String())
	}
	row["parents"] = parents
	if len(e.Event.Attest) > 0 {
		att := make([]map[string]string, 0, len(e.Event.Attest))
		for _, a := range e.Event.Attest {
			if a.Oracle == doorOracle {
				row["door"] = string(a.Claim)
			}
			att = append(att, map[string]string{
				"oracle": a.Oracle,
				"claim":  base64.StdEncoding.EncodeToString(a.Claim),
			})
		}
		row["attest"] = att
	}
	return row
}

// alias is one name a public key is held under, and the grant it signs under
// there.
type alias struct{ name, authority string }

// aliases maps each public key this door can name to the names it is held
// under: "root" for the ledger's root, each keyring or key-source name, and
// each name the door was given for display. Names only; no private byte
// leaves it.
func (s *Server) aliases() map[string][]alias {
	out := map[string][]alias{string(s.led.Root()): {{name: keyRoot}}}
	add := func(src KeySource) {
		if src == nil {
			return
		}
		names := append([]string(nil), src.Names()...)
		sort.Strings(names)
		for _, n := range names {
			priv, authority, found := src.Key(n)
			if !found || len(priv) != ed25519.PrivateKeySize {
				continue
			}
			a := alias{name: n}
			if authority != nil {
				a.authority = authority.String()
			}
			pub := string(priv.Public().(ed25519.PublicKey))
			wipe(priv)
			out[pub] = append(out[pub], a)
		}
	}
	if s.opts.Keys != nil {
		add(s.opts.Keys)
	}
	add(s.opts.Named)
	return out
}

// wait answers like log with a cursor, but if nothing has come after the
// cursor yet it holds the answer until something does, or until timeout
// seconds have passed — whichever is first. A program that has work to
// receive sleeps here instead of asking every second.
//
// It is a watcher, and a watcher writes nothing: it wakes when a recording
// made elsewhere reaches the references, and reports it. Between wakings it
// looks at the references itself now and then, since a writer on the command
// line does not knock.
//
//	— T4.1, T10.6
func (s *Server) wait(req request) map[string]any {
	if req.After == "" {
		return refusal("bad_request", errors.New("wait takes after: the last event read"))
	}
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		r := s.log(req)
		if r["ok"] != true {
			return r
		}
		if evs, _ := r["events"].([]map[string]any); len(evs) > 0 {
			return r
		}
		if !time.Now().Before(deadline) {
			r["timed_out"] = true
			return r
		}
		// Let go of the server while sleeping, so recordings can happen.
		changed := s.changed
		s.mu.Unlock()
		poll := time.NewTimer(500 * time.Millisecond)
		select {
		case <-changed:
		case <-poll.C:
		}
		poll.Stop()
		s.mu.Lock()
		if err := s.refresh(); err != nil {
			return refusal("carrier_unreadable", err)
		}
		if s.opts.Live != nil && !s.opts.Live() {
			return refusal(booth.CodeKeyRevoked, errors.New("the key this view is read for is no longer live"))
		}
	}
}

func (s *Server) get(idHex string) map[string]any {
	id, err := frame.ParseID(idHex)
	if err != nil {
		return refusal("bad_request", err)
	}
	e, found := s.led.Get(id)
	if !found {
		return refusal("not_found", errors.New("not found"))
	}
	r := ok(map[string]any{
		"id":    id.String(),
		"raw":   hex.EncodeToString(e.Raw),
		"state": s.led.State(id).String(),
	})
	if e.HeadOnly {
		r["head_only"] = true
	}
	if j := s.led.Judged(id).String(); j != "" {
		r["judged"] = j
	}
	return r
}

func (s *Server) announce() map[string]any {
	heads := s.led.Heads()
	if len(heads) == 0 {
		return fail(errors.New("no heads"))
	}
	acc, _, _ := s.led.Tally()
	head := heads[0]
	e, _ := s.led.Get(head)
	a := announce.New(s.led.Genesis(), head, acc, e.Event.Address)
	b, err := a.Encode()
	if err != nil {
		return fail(err)
	}
	return ok(map[string]any{
		"announcement": hex.EncodeToString(b),
		"bytes":        len(b),
		"text":         a.String(),
	})
}

// append takes bytes that are already signed. This is the generic write path,
// and the one a courier has.
//
// It ends by moving the references, and that is not tidying up: the commit
// point is the reference, not the object store. Opening a carrier walks back
// from the heads, so an object that no reference reaches is an object the next
// open will not find — the bytes sit there and the ledger has never heard of
// them.
//
// It was doing Put and stopping. Every event a courier delivered was reported
// accepted, written to disk, and lost on the next open, with no shipped
// command able to name it again: a branch may only point at an accepted event,
// and it was not accepted, because nothing reached it. The daemon's own write
// op had always moved the ref; this one had not.
//
// An event that the ledger rejects is never stored on the carrier.
//
//	— T8.5, T3.3
func (s *Server) append(req request) map[string]any {
	if req.Attempt != "" {
		// Bytes already signed carry their own name, and asking get with that
		// name is how a caller learns whether they were recorded.
		return notRecorded("attempt_unsupported", errors.New("append takes bytes already signed; their own name is the attempt, so ask get with it"))
	}
	raw, err := hex.DecodeString(req.Raw)
	if err != nil {
		return notRecorded("bad_request", fmt.Errorf("raw: %w", err))
	}
	if r := s.precondition(req.ExpectHeads); r != nil {
		return r
	}
	id, bad := s.commit(raw, s.commitHeads)
	if bad != nil {
		return bad
	}
	return ok(map[string]any{"id": id.String(), "state": ledger.Accepted.String(), "record": Recorded})
}

// commitHeads makes the carrier's references reach every head the ledger has.
//
// Delivered events arrive without a branch of their own — a courier carries
// somebody's bytes, not their branch names — so the heads are what has to be
// pinned.
//
// They are pinned under a small fixed set of names rather than one name per
// delivery, because a carrier's store has no delete: it can write a reference
// and it cannot take one away, deliberately. Fixed names are overwritten, so
// a thousand deliveries leave as many references as there are concurrent
// heads. Heads come back in a deterministic order, so a name keeps meaning
// roughly the same place; and a name left pointing at what is now an ancestor
// is harmless, since the walk reaches everything from the newer head anyway.
//
//	— T8.5, T8
func (s *Server) commitHeads(rec *carrier.Recording, _ frame.ID) error {
	for i, h := range s.led.Heads() {
		name := deliveredRef
		if i > 0 {
			name = fmt.Sprintf("%s-%d", deliveredRef, i+1)
		}
		if err := rec.SetRef(name, h); err != nil {
			return err
		}
	}
	return nil
}

// deliveredRef is the branch name the daemon keeps delivered events under. It
// is not a person's branch and does not pretend to be one: whoever sent these
// events had their own names for them, and those names are theirs.
const deliveredRef = "delivered"

// write builds and signs with a keyring key. Convenience only; append is the
// path other programs should use.
func (s *Server) write(req request) map[string]any {
	priv, authority, err := s.key(req.Key)
	if err != nil {
		return notRecorded(codeOf(err, "key_unknown"), err)
	}
	payload := []byte(req.Message)
	if req.Payload != "" {
		payload, err = base64.StdEncoding.DecodeString(req.Payload)
		if err != nil {
			return notRecorded("bad_request", fmt.Errorf("payload: %w", err))
		}
	}
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}
	verb := req.Verb
	if verb == "" {
		verb = "note"
	}
	// If this address falls in a bound harness's space, the harness's own
	// covenant decides. A verb it did not declare gets the answer it
	// declared, and nothing here guesses what an unknown verb might mean.
	//   — T11.10, T13.5
	probe := event.Event{Address: req.Address, Verb: verb}
	if c, found := s.bound.For(probe); found {
		switch c.Read(probe) {
		case harness.Shelf:
			// The ritual's words at the harness's root are written through
			// the ritual — intent and outcome, which check the witnesses —
			// never through a plain write, which would put a receipt's name
			// on an event that is not one.
			//   — T11.11, T10.2
			return notRecorded("harness_refused", fmt.Errorf("%q at the root of %s is the receipt ritual's; write it with the intent or outcome op, not write",
				verb, c.Namespace))
		case harness.Refused:
			if c.AtRoot(probe) {
				// The root is the harness itself; its verbs act beneath it.
				//   — T11.11
				return notRecorded("harness_refused", fmt.Errorf("%q is the root of %s, which is the harness itself; its verbs act under it, as at %q",
					req.Address, c.Namespace, c.Namespace+"/…"))
			}
			return notRecorded("harness_refused", fmt.Errorf("%q is not a verb %s %s can do", verb,
				c.Namespace, c.Version))
		case harness.Ignored:
			return ok(map[string]any{"ignored": true, "namespace": c.Namespace,
				"version": c.Version, "record": NotRecorded})
		}
	}

	author := priv.Public().(ed25519.PublicKey)
	answer := func(id frame.ID) map[string]any {
		return ok(map[string]any{"id": id.String(), "state": ledger.Accepted.String(), "branch": branch})
	}
	claim, done := s.attemptFor(req.Attempt, author, "write", map[string]any{
		"address": req.Address, "verb": verb, "branch": branch,
		"payload": base64.StdEncoding.EncodeToString(payload),
	}, answer)
	if done != nil {
		return done
	}
	if r := s.precondition(req.ExpectHeads); r != nil {
		return r
	}
	parents, err := s.branchHead(branch)
	if err != nil {
		return notRecorded("carrier_unreadable", err)
	}
	anchor := s.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Authority: authority, Parents: parents,
		Address: req.Address, Verb: verb, Payload: payload, Attest: s.stamp(claim),
	}, priv)
	if err != nil {
		return notRecorded("bad_request", err)
	}
	if _, bad := s.commit(e.Raw, func(rec *carrier.Recording, id frame.ID) error { return rec.SetRef(branch, id) }); bad != nil {
		return bad
	}
	r := answer(e.ID)
	r["record"] = Recorded
	if req.Attempt != "" {
		r["attempt"] = req.Attempt
		r["already"] = false
	}
	return r
}

func (s *Server) branchHead(name string) ([]frame.ID, error) {
	id, found, err := s.car.Ref(name)
	if err != nil {
		return nil, err
	}
	if !found {
		return []frame.ID{s.led.Genesis()}, nil
	}
	return []frame.ID{id}, nil
}

func (s *Server) key(name string) (ed25519.PrivateKey, *frame.ID, error) {
	if name == "" {
		name = keyRoot
	}
	if name == keyRoot {
		// A program's door names its own delegated key or signs nothing: no
		// request falls back to the root, named or unnamed.
		//   — T11.10, N4.8
		if s.opts.NoRoot {
			return nil, nil, withCode("root_refused", errors.New("this door does not sign with the root key; name the delegated key this program was granted"))
		}
		if s.opts.Root == nil {
			return nil, nil, withCode("key_unknown", errors.New("the owner's signing key is not open here (cold custody); use append with a pre-signed event"))
		}
		return append(ed25519.PrivateKey(nil), s.opts.Root...), nil, nil
	}
	if s.opts.Keys != nil {
		priv, authority, found := s.opts.Keys.Key(name)
		if !found {
			return nil, nil, withCode("key_unknown", fmt.Errorf("key %q is not held for this door", name))
		}
		if authority == nil {
			return nil, nil, withCode("authority_unknown", fmt.Errorf("no authority recorded for key %q", name))
		}
		return priv, authority, nil
	}
	return nil, nil, withCode("key_unknown", fmt.Errorf("key %q is not held for this door", name))
}

// doorOracle names the attestation a door leaves about itself.
const doorOracle = "door"

// stamp is what a door attests on an event it records: the default oracles,
// its own name when it has one, and the attempt claim when the caller named
// one. Attestations are the signer's testimony and prove nothing about the
// world; the door's is that it is the door that signed here.
//
//	— T10.2, T12.5
func (s *Server) stamp(claim []byte) []event.Attestation {
	att, _ := oracle.Observe(oracle.Default())
	if s.opts.Door != "" {
		att = append(att, event.Attestation{Oracle: doorOracle, Claim: []byte(s.opts.Door)})
	}
	if claim != nil {
		att = append(att, event.Attestation{Oracle: attemptOracle, Claim: claim})
	}
	return att
}

// ---------- socket ----------

// Listen prepares a Unix socket at path with mode 0600.
//
// A stale socket left by a crash is removed, but only after checking that
// nothing is listening on it, and only if the path really is a socket. A
// regular file at that path is refused rather than deleted.
func Listen(path string) (net.Listener, error) {
	// Unix socket paths are limited by sun_path: 104 bytes on macOS and BSD,
	// 108 on Linux. Past that the kernel returns a bare "invalid argument",
	// so say what is actually wrong.
	if len(path) >= 104 {
		return nil, fmt.Errorf("daemon: socket path is %d bytes; the limit is about 104. Use a shorter path", len(path))
	}
	if fi, err := os.Stat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("daemon: %s exists and is not a socket", path)
		}
		if c, err := net.Dial("unix", path); err == nil {
			c.Close()
			return nil, fmt.Errorf("daemon: another daemon is listening on %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("daemon: stale socket: %w", err)
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Serve accepts connections until the listener closes.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.session(conn)
	}
}

func (s *Server) session(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), MaxLine)
	w := bufio.NewWriter(conn)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		resp := s.Handle(line)
		b, err := json.Marshal(resp)
		if err != nil {
			b, _ = json.Marshal(fail(err))
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		b, _ := json.Marshal(fail(err))
		w.Write(append(b, '\n'))
		w.Flush()
	}
}

// bind takes a harness's covenant — its own space for addresses and verbs, its
// version, everything it can do, and what it does with a verb it has never
// heard — together with what it declares about its effects. Both are required,
// and two harnesses cannot share a space.
//
//	— T11.10, T13.5, T10.7
func (s *Server) bind(req request) map[string]any {
	c := harness.Covenant{
		Namespace: req.Namespace,
		Version:   req.Version,
		Can:       req.Can,
		Unknown:   harness.Answer(req.Unknown),
	}
	e := harness.Effects{
		Repeat:     harness.Repeat(req.Repeat),
		Retry:      harness.Retry(req.Retry),
		Compensate: harness.Compensate(req.Compensate),
		Ending:     harness.Ending(req.Ending),
	}
	// The very same declaration again is the binding already standing, not a
	// second harness; a different one in a taken space is refused as before.
	//   — T11.10, T13.5
	already, err := s.bound.BindSame(c, e)
	if err != nil {
		code := "bind_refused"
		if errors.Is(err, harness.ErrNamespaceTaken) {
			code = "namespace_taken"
		}
		return refusal(code, err)
	}
	// The list itself, not how long it is. Bind sorts and checks it, so what
	// comes back is what was actually bound rather than what was typed — and a
	// caller that wants to see that for itself needs the verbs to compare.
	//   — T11.10
	bound, _ := s.bound.For(event.Event{Address: c.Namespace})
	return ok(map[string]any{"namespace": c.Namespace, "version": c.Version,
		"can": bound.Can, "unknown": string(c.Unknown),
		"repeat": string(e.Repeat), "retry": string(e.Retry),
		"compensate": string(e.Compensate), "ending": string(e.Ending),
		"mayResend": e.MayResend(), "already": already,
		"declaration": declarationName(bound)})
}

// listBound names the harnesses that have declared themselves, so a reader
// can see which meanings are answered for here and by which version.
//
//	— T11.10
func (s *Server) listBound() map[string]any {
	out := []map[string]any{}
	for _, b := range s.bound.All() {
		out = append(out, map[string]any{
			"namespace": b.Namespace, "version": b.Version,
			"can": b.Can, "unknown": string(b.Unknown),
			"repeat": string(b.Effects.Repeat), "retry": string(b.Effects.Retry),
			"compensate":  string(b.Effects.Compensate),
			"ending":      string(b.Effects.Ending),
			"mayResend":   b.Effects.MayResend(),
			"declaration": declarationName(b),
		})
	}
	return ok(map[string]any{"harnesses": out})
}

// answer is the gate a harness's answer passes through, and it is a real gate:
// what comes back is either the answer marked sound, or every way it is not.
//
// A harness does not get to check itself. It hands over what it says, where
// each saying came from, what it did and under which authority — and the
// ledger, not the harness, decides whether those names mean anything. A
// citation this ledger does not hold, an authority that is not a standing
// grant, an act with no receipt, a ground that is someone else's: each is
// named, and all of them are named at once rather than the first one found.
//
// It records nothing. Checking an answer is not performing it, and a harness
// that wants the act recorded writes it like anything else — after this says
// it may.
//
//	— T2, T2.1, T2.2, T2.3, T4.1
func (s *Server) answer(req request) map[string]any {
	if req.Namespace != "" {
		probe := event.Event{Address: req.Namespace}
		if _, found := s.bound.For(probe); !found {
			return fail(fmt.Errorf("%q is not a harness bound here", req.Namespace))
		}
	}
	a := answer.Answer{}
	if req.Anchor != "" {
		id, err := frame.ParseID(req.Anchor)
		if err != nil {
			return fail(fmt.Errorf("anchor: %w", err))
		}
		a.Anchor = id
	}
	for i, c := range req.Claims {
		cl := answer.Claim{Saying: c.Saying}
		for _, f := range c.From {
			id, err := frame.ParseID(f)
			if err != nil {
				return fail(fmt.Errorf("claim %d: %w", i, err))
			}
			cl.From = append(cl.From, id)
		}
		a.Claims = append(a.Claims, cl)
	}
	for i, act := range req.Acts {
		ac := answer.Act{Doing: act.Doing}
		if act.Authority != "" {
			id, err := frame.ParseID(act.Authority)
			if err != nil {
				return fail(fmt.Errorf("act %d authority: %w", i, err))
			}
			ac.Authority = id
		}
		if act.Receipt != "" {
			id, err := frame.ParseID(act.Receipt)
			if err != nil {
				return fail(fmt.Errorf("act %d receipt: %w", i, err))
			}
			ac.Receipt = id
		}
		a.Acts = append(a.Acts, ac)
	}

	owed := a.Check(ground{l: s.led})
	if len(owed) == 0 {
		return ok(map[string]any{"sound": true, "anchor": s.led.Genesis().String()})
	}
	reasons := make([]string, 0, len(owed))
	for _, e := range owed {
		reasons = append(reasons, e.Error())
	}
	return ok(map[string]any{"sound": false, "owed": reasons,
		"anchor": s.led.Genesis().String()})
}
