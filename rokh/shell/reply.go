// Replies.
//
// Every reply is a short sentence in the same register, from a fixed
// template; only numbers and ids vary. Every WRITE reply carries the full
// event id. The discipline of the docs is carried in the wording and never
// softened: accepted is not true, verified is not honest, entrusted is not
// consent, opened-to is not sent.
//
// Stdout carries the sentences and nothing else. Auxiliary detail - file
// paths, hints, per-side verdicts - goes to stderr.
package shell

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Templates, normative. Adjust only numbers and ids.
const (
	tplOpened     = "opened; I am standing at the head of the chain. %s, %s."
	tplOpenedNew  = "a new ledger is open; its anchor is %s."
	tplClosed     = "gone. The ledger is closed, everything where it was."
	tplHomeClosed = "gone. The home stays open behind its gate, everything where it was."
	tplWritten    = "written and recorded. %s — signed by %s under %s, through %s."
	tplDrafted    = "waiting as sentence %s: at %s, %s bytes, to be signed by %s under %s — nothing is recorded yet. \"write\" records it; \"cancel\" lets it go."
	tplCancelled  = "let go. Sentence %s left working state; nothing was recorded."
	tplSeen       = "%s, %s, all verified."
	tplImported   = "brought it, checked it, wrote it. %s"
	tplImportFail = "brought it but did not write it — %s. The source is untouched."
	tplReunited   = "%s new from them, %s from us; %s. Two open heads — say \"reconcile\" to make them one."
	tplMerged     = "they met; neither was erased and neither won. %s"
	tplGranted    = "entrusted. %s"
	tplShared     = "opened to them. %s"
	tplRevoked    = "taken back; from here on, not over what is past."
	tplCopied     = "carried; a whole copy at %s. The keys did not go — writing there needs a new entrusting."
	tplExported   = "carried; %s put out as files. Nothing in the ledger moved."
	tplBundled    = "bundled to carry: %s, under the same grant whose reading you entrusted to them. %s did not go."
)

// refusal is a refusal with the two words a program needs beside the
// sentence a person reads: a stable code, and whether anything was recorded.
// Every surface prints it the same way, so "no — …" is never a bare sentence
// a script has to parse.
//
//	— T10.3, T12.3
type refusal struct {
	code   string
	record string // "not recorded", "unknown", or empty when nothing could have been
	err    error
}

func (r refusal) Error() string {
	tail := r.code
	if r.record != "" {
		tail += "; " + r.record
	}
	return r.err.Error() + " [" + tail + "]"
}

func (r refusal) Unwrap() error { return r.err }

// errUnknownSentence is what both surfaces say to a line that is not one of
// the sentences: one text, and both keys that bring the list.
var errUnknownSentence = refusal{code: "unknown_sentence",
	err: errors.New(`I do not know that sentence — "?" or "/" lists the ones I do`)}

// countOf renders a number with the thing it counts, and agrees with it.
//
// "1 heads" is the sort of thing that tells a reader the sentence was
// assembled rather than written, and this surface's whole claim is that its
// replies are sentences. Only the regular plural is here: every noun this
// surface counts takes one.
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return digits(n) + " " + noun + "s"
}

// digits renders a count for a reply.
//
// It used to render Persian digits, to keep the replies in one register. The
// replies are in English now, so it renders the digits English writing uses —
// the same rule as before, applied to the language actually being spoken.
func digits(n int) string { return strconv.Itoa(n) }

// escapePayload makes stored content safe to display: control bytes and
// bidirectional overrides are escaped so content can never command the
// terminal or masquerade as something it is not. ZWNJ is ordinary writing in
// several scripts and stays literal — content is not the surface's language
// and is never folded to it. Newlines survive as newlines.
func escapePayload(b []byte) string {
	var sb strings.Builder
	for _, r := range string(b) {
		switch {
		case r == '\n':
			sb.WriteRune(r)
		case r == '\t':
			sb.WriteString("\\t")
		case r < 0x20 || r == 0x7F: // C0 and DEL
			fmt.Fprintf(&sb, "\\x%02x", r)
		case r >= 0x80 && r <= 0x9F: // C1
			fmt.Fprintf(&sb, "\\u%04x", r)
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069: // bidi controls
			fmt.Fprintf(&sb, "\\u%04x", r)
		case r == 0xFFFD:
			sb.WriteString("\\ufffd")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
