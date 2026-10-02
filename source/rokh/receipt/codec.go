package receipt

import (
	"sort"

	"rokh/canon"
)

// encode writes a receipt in the one byte order.
//
// It is not encoding/json, and the difference is not cosmetic. A receipt rides
// inside an event payload and an event's bytes are its name, so "one content,
// one encoding" is the law here — and Go's encoder escapes three characters no
// other language escapes, which would give a receipt saying "sent the invoice
// & the note" one name in Go and another everywhere else. The canon package
// carries the whole rule.
//
//	— T3.1, N4.1, T3.2
func encode(v any) ([]byte, error) { return canon.Marshal(v) }

// Read parses a receipt payload back. It never repairs: a payload that does
// not decode is not a receipt with a problem, it is not a receipt.
//
// And it insists these bytes are the one encoding of what they say. A payload
// spelled {"TYPE":…} decodes in Go exactly as {"type":…} does, so without this
// two different byte strings — two different event names — would both be read
// as the same receipt.
//
//	— N4.1, T3.1
func Read[T Intent | Result](b []byte) (T, error) {
	var out T
	err := canon.Strict(b, &out)
	return out, err
}

func sortStrings(s []string) { sort.Strings(s) }

// PayloadType reads only the declared type out of a payload and nothing else.
//
// It is deliberately narrow: recognising an event must not require decoding
// the whole of it, because a harness has to decide whether a payload is its
// business before it is willing to parse it as its own.
//
//	— T10.6
func PayloadType(b []byte) string {
	// The exact key, not Go's case-insensitive match: the peek has to be as
	// exact as the full read, or "TYPE" gets past the door "type" was checked
	// at.
	t, _ := canon.Peek(b, "type")
	return t
}
