package shell

import (
	"errors"
	"strings"
)

// Near misses.
//
// A line that begins like one of the sentences and does not have its shape
// is answered with that sentence's shape and an example, chosen by its first
// word or two. The parser is not touched and does not guess: the line is
// still refused, with the same code as any line that is not a sentence, and
// nothing is recorded. It only says which sentence the person was reaching
// for and how it goes.

// nearMiss is the refusal for a line that is not a sentence: the shape of
// the one it begins like, or the general answer.
func nearMiss(line string) error {
	if hint := shapeOf(line); hint != "" {
		return refusal{code: "unknown_sentence", err: errors.New(hint)}
	}
	return errUnknownSentence
}

// shapeOf names the shape of the sentence a line begins like, with an
// example, or nothing.
func shapeOf(line string) string {
	ts := words(strings.TrimSpace(line))
	if len(ts) == 0 {
		return ""
	}
	first := foldFrame(ts[0].raw)
	second := ""
	if len(ts) > 1 {
		second = foldFrame(ts[1].raw)
	}
	switch first {
	case "write":
		if second != "at" {
			return `"write" alone records the newest waiting sentence, and "write 2" the second; a new one begins "write at": write at home/journal: my first note`
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			return `"write at" needs an address, a colon and your text: write at home/journal: my first note`
		}
		if len(words(line[:colon])) > 3 {
			return `the address before the colon is one word, with no spaces: write at home/my-notes: my first note`
		}
		return `"write at" needs an address before the colon: write at home/journal: my first note`
	case "read":
		return `"read" needs one address, with no spaces: read home/journal`
	case "open":
		if second == "a" || second == "new" {
			return `"open a new ledger named" needs a name, one word: open a new ledger named work`
		}
		return `"open the ledger" needs the name of a ledger that is here, one word: open the ledger home`
	case "see":
		return `"see" goes with the ledger, the ledgers or the grants: see the ledger`
	case "entrust":
		if second == "reading" {
			return `"entrust reading" needs a place and a public key: entrust reading home/journal to {64 hex digits}`
		}
		return `"entrust writing at" needs a place and a public key: entrust writing at home/journal to {64 hex digits}`
	case "take":
		return `"take back the grant" needs the grant's whole id, which "see the grants" shows: take back the grant {id}`
	case "bring":
		return `"bring" needs a file or folder and an address: bring notes.txt to home/files`
	case "carry":
		return `"carry" goes with the ledger, an address or the bundle: carry home/journal to /path/to/folder`
	case "cancel":
		return `"cancel" alone lets the newest waiting sentence go, and "cancel 2" the second`
	case "leave", "reconcile":
		return `"` + first + `" is said alone`
	}
	return ""
}
