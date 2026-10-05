package shell

import (
	"strings"
	"testing"
)

// A line that begins like a sentence and has the wrong shape is answered
// with that sentence's shape and an example, still refused with the code of
// a line that is not a sentence; a line that begins like none of them gets
// the general answer.
func TestANearMissIsToldTheShape(t *testing.T) {
	for line, want := range map[string]string{
		"write at":              `"write at" needs an address, a colon and your text`,
		"write at x":            `"write at" needs an address, a colon and your text`,
		"write at my notes: hi": "one word, with no spaces",
		"read":                  `"read" needs one address`,
		"read my notes":         `"read" needs one address`,
		"open the ledger":       `"open the ledger" needs the name`,
		"open a new ledger":     `"open a new ledger named" needs a name`,
		"entrust writing":       `"entrust writing at" needs a place and a public key`,
		"entrust reading home":  `"entrust reading" needs a place and a public key`,
		"take back":             `"take back the grant" needs the grant's whole id`,
		"see everything":        `"see" goes with the ledger`,
		"write something":       `a new one begins "write at"`,
		"LEAVE now":             `"leave" is said alone`,
	} {
		if _, err := parse(line); err == nil {
			t.Fatalf("%q parsed; it is no near miss", line)
		}
		got := nearMiss(line).Error()
		if !strings.Contains(got, want) || !strings.HasSuffix(got, "[unknown_sentence]") {
			t.Errorf("%q was answered %q; want %q in it", line, got, want)
		}
	}
	if got := nearMiss("sing a song"); got != errUnknownSentence {
		t.Errorf("a line like no sentence was answered %v", got)
	}
}
