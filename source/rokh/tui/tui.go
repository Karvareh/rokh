// Package tui is the screen surface of Rokh: the same sentences as the line
// surface, drawn on a terminal that is measured, coloured by meaning, and
// redrawn as the ledger changes.
//
// It is an adapter, like the sentence surface it sits beside, and it imports
// nothing of Rokh's. What it draws arrives as a Snapshot the shell fills from
// a verified session; what a person types goes back to the shell as the
// sentence they typed, byte for byte, to be parsed and judged there. This
// package creates no event, opens no carrier, holds no key and knows no
// socket. Delete it and Rokh loses a way of looking, and nothing else.
//
// Colour here is meaning, not decoration. Six roles, six colours, and each
// colour carries exactly one of Rokh's own distinctions — place, working
// state, settled history, the recording boundary, authority, lineage — so a
// reader who knows the palette can read a screen before reading its words.
// Colour never replaces a word: every screen reads the same with colour off.
package tui

import "strings"

// Role is what a piece of text means, and therefore what colour it takes.
type Role int

const (
	RoleDefault   Role = iota // neutral prose, payloads, ordinary grammar
	RolePlace                 // vault, ledger, address, current view
	RoleWorking               // draft, preview, pending ancestry — not settled
	RoleSettled               // accepted events, verification that passed
	RoleBoundary              // irreversible recording, rejection, refusal
	RoleAuthority             // owner, key, grant, covenant, custody
	RoleLineage               // anchor, event id, parent, head, berth, reunion
	RoleMuted                 // frame lines, hints, the parts that recede
)

// RGB is a colour as a terminal is told it.
type RGB struct{ R, G, B uint8 }

// Palette is the owner's six colours, one per meaning. They are the owner's
// and are not adjusted here; a reader who knows them from anywhere else the
// owner uses them knows them on this screen. Each is named below by its
// meaning and by the colour it is.
var Palette = map[Role]RGB{
	RolePlace:     {0x2E, 0x5A, 0xAC}, // place
	RoleWorking:   {0xD9, 0x98, 0x00}, // working state
	RoleSettled:   {0x00, 0x9B, 0x95}, // settled history
	RoleBoundary:  {0xC4, 0x46, 0x2D}, // the recording boundary
	RoleAuthority: {0x5B, 0x7F, 0x2C}, // authority
	RoleLineage:   {0x8B, 0x58, 0xB8}, // lineage
}

// Screens. A screen is where the eye rests; every sentence may be typed on
// any of them.
const (
	ScreenLedger = iota
	ScreenSentence
	ScreenAuthority
	ScreenLayers
)

// ScreenNames, in the order the tabs show them.
var ScreenNames = [...]string{"ledger", "sentence", "authority", "layers"}

// Sentence is one canonical sentence: what to say, what it does in plain
// words and exactly, the meaning
// it belongs to, the screen it belongs on, and whether saying it records an
// event. Records is held to what the sentence does by a test that says each
// one on a ledger and counts the events it made.
type Sentence struct {
	Say     string
	Plain   string // what it does, in everyday words, shown first
	Does    string // what it does exactly, shown under the plain words
	Role    Role
	Screen  int
	Records bool
}

// Sentences is the whole human surface, in the order a person meets it: open
// something, say something, look at something, entrust something, carry
// something, leave. It is the one list. The line surface prints from it, the
// palette searches it, and the shell's parser is the only judge of whether a
// typed line is one of them.
//
// A slot in braces is the person's to fill. The palette places the sentence
// up to its first slot and leaves the rest to be typed, because a template
// with the braces left in is not a sentence anybody means.
//
// Each says, in the same words on both surfaces, whether it records: the
// seven that do say "records one event" and take the boundary's colour, the
// ones that only look say "reads only", and "write at" says it waits.
var Sentences = []Sentence{
	{"open a new ledger named {name}", "start a new ledger beside this one", "records one event: makes a new ledger, and this sentence is its genesis", RoleBoundary, ScreenLedger, true},
	{"open the ledger {name}", "go to a ledger that is already here", "reads only: verifies its carrier and stands at its head", RolePlace, ScreenLedger, false},
	{"write at {address}: {text}", "put a note down; it waits until you say write", "waits: puts a sentence in working state, beside any already waiting; nothing is recorded yet", RoleWorking, ScreenSentence, false},
	{"write", "keep the waiting note for good; there is no undo", "records one event: the newest waiting sentence (write {n}: the n-th); there is no way back", RoleBoundary, ScreenSentence, true},
	{"cancel", "let the waiting note go", "lets the newest waiting sentence go (cancel {n}: the n-th); nothing is recorded", RoleWorking, ScreenSentence, false},
	{"read {address}", "show what is written there", "reads only: what was written at that address and beneath it", RoleSettled, ScreenLedger, false},
	{"see the ledger", "check the ledger and count what is in it", "reads only: how many events, how many heads, all verified", RoleSettled, ScreenLedger, false},
	{"see the ledgers", "list the ledgers in this folder", "reads only: the ledgers in this vault, their anchors, custody and seats", RolePlace, ScreenLedger, false},
	{"see the grants", "list who may write or read here", "reads only: the writing you have entrusted and the reading you have opened", RoleAuthority, ScreenAuthority, false},
	{"bring {thing} to {address}", "keep a file or a folder in the ledger", "records one event: hashes and checks a file or folder, then records it with its content", RoleBoundary, ScreenSentence, true},
	{"bring the returned ledger", "take in a copy that went out and came back", "joins a same-anchor copy that went out and came back, both ways; records no event of its own", RoleLineage, ScreenLedger, false},
	{"entrust writing at {place} to {who}", "let one key write in one place", "records one event: gives one key a narrow right to write there", RoleBoundary, ScreenAuthority, true},
	{"entrust reading {place} to {who}", "let one key read one place; nothing is sent", "records one event: opens one scope to one reader; nothing is sent", RoleBoundary, ScreenAuthority, true},
	{"take back the grant {id}", "stop a key writing from now on; the past stays", "records one event: closes that authority from here on; the past is not erased", RoleBoundary, ScreenAuthority, true},
	{"carry the ledger to {path}", "a whole copy is made on the command line", "records nothing: a whole copy is a seed, made on the command line", RoleLineage, ScreenLedger, false},
	{"carry {address} to {path}", "copy what is written somewhere out as files", "reads only: puts checked content out as files; moves no event", RoleLineage, ScreenLedger, false},
	{"carry the bundle for {who}", "pack what one reader may have, to hand over", "reads only: writes a file of what one live disclosure permits", RoleLineage, ScreenAuthority, false},
	{"reconcile", "make two versions of the ledger one", "records one event: joins two clean heads of one ledger; erases and crowns neither", RoleBoundary, ScreenLedger, true},
	{"leave", "close the ledger", "closes the ledger and leaves; records nothing", RolePlace, ScreenLedger, false},
}

// Asking reports whether a line asks for the sentences rather than being one
// of them: "?", "/", "sentences" or "help", in any ASCII case. Both surfaces
// answer it the same way, with the list, and neither records anything for it.
func Asking(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "?", "/", "sentences", "help":
		return true
	}
	return false
}

// Stem is the part of a sentence a person does not have to type: everything
// before its first slot. A sentence with no slot is its own stem.
func Stem(say string) string {
	for i := 0; i < len(say); i++ {
		if say[i] == '{' {
			return say[:i]
		}
	}
	return say
}

// EventItem is one event as the ledger screen shows it. Every field is a
// string the shell has already made safe to say; this package escapes what it
// draws, but decides nothing about what an event means.
type EventItem struct {
	ID        string `json:"id,omitempty"`      // the short name
	Verdict   string `json:"verdict,omitempty"` // accepted, pending, rejected
	Verb      string `json:"verb,omitempty"`
	Address   string `json:"address,omitempty"`
	Payload   string `json:"payload,omitempty"`   // the shell's description of the payload, not raw bytes
	Parent    string `json:"parent,omitempty"`    // the short name of the first parent; empty for genesis
	Signer    string `json:"signer,omitempty"`    // the key that signed, by name where it has one
	Authority string `json:"authority,omitempty"` // the grant it was signed under, or the owner's own right
	Door      string `json:"door,omitempty"`      // the door that recorded it, when it left its mark
	// System marks the ledger's own events (its making, keys, grants,
	// seeds): the same line, with the mark system, and Payload is the
	// shell's sentence for what the event did, not the person's words.
	System bool `json:"system,omitempty"`
}

// Draft is the sentence waiting in working state, if one is.
type Draft struct {
	Index        int    `json:"index,omitempty"` // its place among the waiting sentences, from 1
	Count        int    `json:"count,omitempty"` // how many wait
	Sentence     string `json:"sentence,omitempty"`
	Address      string `json:"address,omitempty"`
	Verb         string `json:"verb,omitempty"`
	PayloadBytes int    `json:"payload_bytes,omitempty"`
	Signer       string `json:"signer,omitempty"`
	Authority    string `json:"authority,omitempty"`
	Parent       string `json:"parent,omitempty"`
}

// AuthorityItem is one live grant or one live disclosure.
type AuthorityItem struct {
	ID      string `json:"id,omitempty"`
	Subject string `json:"subject,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// Snapshot is everything this surface knows, and the seam between it and the
// shell. The shell fills one from a session it has already verified; this
// package draws it and never reaches past it. An empty Ledger means no ledger
// is open, and the screen says so instead of drawing zeros as if they were
// facts.
type Snapshot struct {
	Vault   string `json:"vault,omitempty"`
	Ledger  string `json:"ledger,omitempty"`
	Anchor  string `json:"anchor,omitempty"`
	Head    string `json:"head,omitempty"`
	Heads   int    `json:"heads,omitempty"`
	Custody string `json:"custody,omitempty"` // warm or cold
	// Room is the size of the Rokh as its vessel holds it, Free what can
	// be written into it now, Growth whether it grows by itself and how far,
	// all written out; Full says nothing more can be written until it grows.
	// Empty when the shell does not know them.
	Room       string          `json:"room,omitempty"`
	Free       string          `json:"free,omitempty"`
	Growth     string          `json:"growth,omitempty"`
	Full       bool            `json:"full,omitempty"`
	Verified   bool            `json:"verified,omitempty"` // the open ledger passed verification when it was opened
	Accepted   int             `json:"accepted,omitempty"` // the person's accepted events
	System     int             `json:"system,omitempty"`   // the ledger's own accepted events, counted apart
	Pending    int             `json:"pending,omitempty"`
	Rejected   int             `json:"rejected,omitempty"`
	Events     []EventItem     `json:"events,omitempty"` // newest first, accepted only, as many as fit
	Drafts     []Draft         `json:"drafts,omitempty"` // the sentences waiting in working state, oldest first
	Layers     Layers          `json:"layers,omitempty"`
	Writing    []AuthorityItem `json:"writing,omitempty"`
	Disclosure []AuthorityItem `json:"disclosure,omitempty"`
	TakenBack  []AuthorityItem `json:"taken_back,omitempty"` // grants closed by a revocation; ID is the grant, Subject the revoking event
}

// Layers is the only part of a ledger's layout the screen shows, and all of
// it (contract section 6): the Star is the rokh itself; a Planet is the
// first component of an address; a Moon is the second. Deeper components
// are listed inside their Moon. The shell fills it from the events it may
// read; nothing here reads a file.
type Layers struct {
	Star    Star     `json:"star,omitempty"`
	Planets []Planet `json:"planets,omitempty"`
}

// Star is the rokh itself: its anchor, its own events, its keys, its seeds.
type Star struct {
	Anchor string `json:"anchor,omitempty"`
	System int    `json:"system,omitempty"` // the ledger's own events
	Keys   int    `json:"keys,omitempty"`   // keys added besides the owner's and not taken back
	Seeds  int    `json:"seeds,omitempty"`  // seeds given
}

// Planet is a first address component and what is written beneath it.
type Planet struct {
	Name   string `json:"name,omitempty"`
	Events int    `json:"events,omitempty"` // events at the Planet and beneath it
	Moons  []Moon `json:"moons,omitempty"`
}

// Moon is a second address component; Deeper are the components beneath it,
// each written out from the Moon down.
type Moon struct {
	Name   string   `json:"name,omitempty"`
	Events int      `json:"events,omitempty"`
	Deeper []string `json:"deeper,omitempty"`
}

// View is the state that belongs to the screen and to nothing else: which
// screen, whether the palette is open, what is being typed, what was last
// answered. None of it is in the ledger and none of it survives the session.
type View struct {
	Screen   int
	MenuOpen bool
	Selected int
	Query    string // the palette's filter
	Input    string // the sentence being typed, byte for byte
	// Back is where the cursor stands, as the number of bytes of Input after
	// it: zero is the end of the line, where typing adds.
	Back int
	// History is the sentences said in this session, oldest first. It is
	// kept here, in memory, and nowhere else; it ends with the session.
	History []string
	// Recalled is how far back in History the prompt stands, from 1 for the
	// last sentence; zero when it holds what is being typed. Typed is that
	// line, kept while the person walks back.
	Recalled int
	Typed    string
	// Pasting is true between the two marks a terminal puts around pasted
	// text: a line break inside a paste is text, never Enter.
	Pasting bool
	Reply   []string // what the shell last answered, one line each
	Failed  bool     // whether that answer was a refusal
	// Reading is true while a long answer takes the view's place so the
	// person can page through it; AnswerAt is the first of its rows shown.
	// Overflow and Page are what the last layout found: that the answer is
	// longer than its box, and how many of its rows a page shows.
	Reading  bool
	AnswerAt int
	Overflow bool
	Page     int
}

// readingKeys says whether the arrows page through the answer rather than
// walk the history: an answer longer than its box is showing, or being read,
// and nothing is typed or recalled.
func (v View) readingKeys() bool {
	return len(v.Reply) > 0 && (v.Overflow || v.Reading) && v.Input == "" && v.Recalled == 0 && !v.MenuOpen
}

// Options is the terminal as measured.
type Options struct {
	Columns int
	Rows    int
	Color   bool
	Depth   int // DepthTrue, Depth256 or Depth16, when Color is on
}
