// Sentence parsing.
//
// The human surface of Rokh is a closed set of canonical sentences. The
// grammar is small enough to parse by hand, and it is: no dependency, no
// regexp.
//
// Two different texts live in one line and get two different treatments:
//
//   - the FRAME — the verb and the role words — is folded before matching:
//     case is not significant, because upper and lower case are the same word
//     said twice. For this closed keyword set, that folding is all that is
//     needed.
//
//   - every SLOT is taken from the raw bytes at its position and preserved
//     exactly as typed. The payload of "write" in particular is never folded,
//     never normalized, and never trimmed beyond the one frame space after
//     the colon.
//
// English puts the verb first, so the verb is what the parser branches on,
// and the role words anchor the slots after it: "at" (ledger, address,
// scope), "to" (destination, recipient), "for" (audience), "named" (a new
// ledger's name), and the colon introduces the payload.
//
// There is no sentence-final period. In Persian the period was frame and was
// stripped; here a path may legitimately end in one ("carry the ledger to .")
// and a payload may legitimately end in one, so nothing is stripped and what
// a person types is what is recorded.
package shell

import (
	"fmt"
	"strings"

	"rokh/working"
)

// Operations. The identifier and the sentence are both English now; the
// mapping is kept in one place so the sentence can change without the meaning
// moving.
const (
	opOpen       = "open"        // open the ledger {name}
	opOpenNew    = "open_new"    // open a new ledger named {name}
	opClose      = "close"       // leave
	opWrite      = "write"       // write at {address}: {text}
	opWriteClose = "write_close" // write, or write {n} — closing a sentence already written
	// opCancel — cancel, or cancel {n} — is declared with the home's sentences
	// in home.go; the vault surface speaks it too, for a waiting sentence.
	opRead         = "read"          // read {address}
	opSeeLedger    = "see_ledger"    // see the ledger
	opSeeLedgers   = "see_ledgers"   // see the ledgers
	opSeeGrants    = "see_grants"    // see the grants
	opImport       = "import"        // bring {thing} to {address}
	opReunite      = "reunite"       // bring the returned ledger
	opEntrustWrite = "entrust_write" // entrust writing at {place} to {who}
	opEntrustRead  = "entrust_read"  // entrust reading {place} to {who}
	opTakeBack     = "take_back"     // take back the grant {id}
	opCarryLedger  = "carry_ledger"  // carry the ledger to {path}
	opCarryAddress = "carry_address" // carry {address} to {path}
	opCarryBundle  = "carry_bundle"  // carry the bundle for {who}
	opReconcile    = "reconcile"     // reconcile
)

// ordinal reads a small positive number, or 0 when the word is not one.
func ordinal(w string) int {
	n := 0
	for _, r := range w {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > working.MaxDrafts {
			return 0
		}
	}
	return n
}

// command is one parsed sentence.
type command struct {
	// N is the waiting sentence a closing or cancelling sentence names,
	// counted from the oldest; 0 means the newest.
	N        int
	Op       string
	Name     string // {name}
	Address  string // {address} or {place}
	Text     string // {text} — byte-exact
	Path     string // {thing} or {path} — raw slice, outer frame space removed
	Who      string // {who}
	ID       string // {id}
	Sentence string // the raw sentence as typed
}

// foldFrame folds a FRAME word only. Never applied to a slot.
//
// Case is the whole of it. A person who types "Open" means open, and a
// keyword set this small has no other spelling to forgive. ASCII case is
// folded by hand rather than through strings.ToLower, because ToLower also
// maps letters outside ASCII — and a frame word here is never outside ASCII,
// so anything that is would be a slot in a frame position and must not be
// quietly rewritten before it is refused.
func foldFrame(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// word is a whitespace-delimited run with its byte offsets in the raw line,
// so multi-word slots can be recovered exactly.
type word struct {
	raw      string
	beg, end int
}

func words(line string) []word {
	var out []word
	i := 0
	for i < len(line) {
		if line[i] == ' ' || line[i] == '\t' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] != ' ' && line[j] != '\t' {
			j++
		}
		out = append(out, word{raw: line[i:j], beg: i, end: j})
		i = j
	}
	return out
}

func is(t word, keyword string) bool { return foldFrame(t.raw) == foldFrame(keyword) }

var errNotASentence = fmt.Errorf("not a sentence")

// parse recognizes one sentence. The error is diagnostic; the caller turns an
// unrecognized line into a reply in register.
//
// The shell of Rokh is a language, not a command set: eight verbs, and a
// sentence you can read aloud. The meaning of the verbs does not belong to
// this parser — the syntax may change or be translated, and the eight
// meanings do not. This file is the proof of that claim: the syntax was
// translated, and not one meaning moved.
//
//	— T9, T9.1, T9.4
func parse(line string) (command, error) {
	c := command{Sentence: line}
	raw := strings.TrimRight(line, "\r\n")
	raw = strings.Trim(raw, " \t")
	if raw == "" {
		return c, errNotASentence
	}

	// The write sentence is split at the FIRST colon: the frame owns the
	// colon and one following space if present. Everything after is the
	// payload, byte for byte — the trailing period included, because in this
	// grammar a period is not frame.
	if idx := strings.IndexByte(raw, ':'); idx >= 0 {
		frame := words(raw[:idx])
		if len(frame) == 3 && is(frame[0], "write") && is(frame[1], "at") {
			payload := raw[idx+1:]
			if strings.HasPrefix(payload, " ") {
				payload = payload[1:] // exactly one frame space
			}
			c.Op, c.Address, c.Text = opWrite, frame[2].raw, payload
			return c, nil
		}
	}

	ts := words(raw)
	if len(ts) == 0 {
		return c, errNotASentence
	}

	switch {
	case len(ts) == 1 && is(ts[0], "leave"):
		c.Op = opClose
		return c, nil

	// The bare verb closes the sentence already written. It is not a ninth
	// verb: it is the same writing, finished. You write the sentence, you see
	// the form of its effect, then you close it.
	//   — T9.2, T9.1, T4.4
	case len(ts) == 1 && is(ts[0], "write"):
		c.Op = opWriteClose
		return c, nil
	// write {n} closes the n-th waiting sentence, counted from the oldest,
	// when several wait at once.
	case len(ts) == 2 && is(ts[0], "write") && ordinal(ts[1].raw) > 0:
		c.Op, c.N = opWriteClose, ordinal(ts[1].raw)
		return c, nil

	// cancel lets the newest waiting sentence go; cancel {n} the n-th. It
	// records nothing: what was never recorded needs no undoing.
	//   — T4.4
	case len(ts) == 1 && is(ts[0], "cancel"):
		c.Op = opCancel
		return c, nil
	case len(ts) == 2 && is(ts[0], "cancel") && ordinal(ts[1].raw) > 0:
		c.Op, c.N = opCancel, ordinal(ts[1].raw)
		return c, nil

	case len(ts) == 1 && is(ts[0], "reconcile"):
		c.Op = opReconcile
		return c, nil

	case is(ts[0], "open"):
		// open a new ledger named {name}
		if len(ts) == 6 && is(ts[1], "a") && is(ts[2], "new") &&
			is(ts[3], "ledger") && is(ts[4], "named") {
			c.Op, c.Name = opOpenNew, ts[5].raw
			return c, nil
		}
		// open the ledger {name}
		if len(ts) == 4 && is(ts[1], "the") && is(ts[2], "ledger") {
			c.Op, c.Name = opOpen, ts[3].raw
			return c, nil
		}

	case is(ts[0], "read"):
		// read {address}
		if len(ts) == 2 {
			c.Op, c.Address = opRead, ts[1].raw
			return c, nil
		}

	case is(ts[0], "see"):
		if len(ts) == 3 && is(ts[1], "the") {
			switch {
			case is(ts[2], "ledger"):
				c.Op = opSeeLedger
				return c, nil
			case is(ts[2], "ledgers"):
				c.Op = opSeeLedgers
				return c, nil
			case is(ts[2], "grants"):
				c.Op = opSeeGrants
				return c, nil
			}
		}

	case is(ts[0], "bring"):
		// bring the returned ledger
		if len(ts) == 4 && is(ts[1], "the") && is(ts[2], "returned") && is(ts[3], "ledger") {
			c.Op = opReunite
			return c, nil
		}
		// bring {thing} to {address} — thing may hold spaces; anchor from the right.
		if len(ts) >= 4 && is(ts[len(ts)-2], "to") {
			c.Op = opImport
			c.Address = ts[len(ts)-1].raw
			c.Path = strings.TrimRight(raw[ts[1].beg:ts[len(ts)-2].beg], " \t")
			return c, nil
		}

	case is(ts[0], "entrust"):
		// entrust writing at {place} to {who}
		if len(ts) == 6 && is(ts[1], "writing") && is(ts[2], "at") && is(ts[4], "to") {
			c.Op, c.Address, c.Who = opEntrustWrite, ts[3].raw, ts[5].raw
			return c, nil
		}
		// entrust reading {place} to {who}
		if len(ts) == 5 && is(ts[1], "reading") && is(ts[3], "to") {
			c.Op, c.Address, c.Who = opEntrustRead, ts[2].raw, ts[4].raw
			return c, nil
		}

	case is(ts[0], "take"):
		// take back the grant {id}
		if len(ts) == 5 && is(ts[1], "back") && is(ts[2], "the") && is(ts[3], "grant") {
			c.Op, c.ID = opTakeBack, ts[4].raw
			return c, nil
		}

	case is(ts[0], "carry"):
		// carry the bundle for {who}
		if len(ts) == 5 && is(ts[1], "the") && is(ts[2], "bundle") && is(ts[3], "for") {
			c.Op, c.Who = opCarryBundle, ts[4].raw
			return c, nil
		}
		// carry the ledger to {path} — path may hold spaces and runs to the end.
		if len(ts) >= 5 && is(ts[1], "the") && is(ts[2], "ledger") && is(ts[3], "to") {
			c.Op = opCarryLedger
			c.Path = strings.TrimRight(raw[ts[4].beg:], " \t")
			return c, nil
		}
		// carry {address} to {path}
		if len(ts) >= 4 && is(ts[2], "to") {
			c.Op = opCarryAddress
			c.Address = ts[1].raw
			c.Path = strings.TrimRight(raw[ts[3].beg:], " \t")
			return c, nil
		}
	}

	return c, errNotASentence
}
