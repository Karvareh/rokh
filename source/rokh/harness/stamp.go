package harness

import (
	"encoding/json"
	"fmt"
	"strings"

	"rokh/canon"
	"rokh/event"
)

// Stamp is the header a harness writes at the front of its own payloads: what
// this payload is, and which version of the harness wrote it.
//
// It goes inside the payload and nowhere else, and that placement is the whole
// point. The payload is inside the bytes the signature covers and inside the
// bytes the name is the hash of, so the version is bound to the event itself —
// not to a table somewhere that says which version was running that week, and
// not to the harness's current belief about itself. A reader years later, with
// only the event, can still tell which version's meaning to read it under.
//
// Address and verb say whose event it is. The stamp says what it is. Neither
// substitutes for the other: a verb is a name for an act, and two versions of a
// harness may keep the verb and change what the payload beneath it holds.
//
//	— T11.10, T10.6, T3.2
type Stamp struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}

// StampOf reads only the header out of a payload, without decoding the rest.
// Recognising an event must not require parsing all of it: the point of the
// stamp is to decide whether this payload is yours before you are willing to
// read it as yours.
//
//	— T10.6
func StampOf(payload []byte) Stamp {
	// The exact keys, not Go's case-insensitive match, and no decoding of the
	// rest: a reader decides whether a payload is its business before it is
	// willing to read it as its own, and the peek has to be as exact as the
	// full read would be.
	//   — T10.6, N4.1
	t, ok := canon.Peek(payload, "type")
	if !ok {
		return Stamp{}
	}
	v, ok := canon.Peek(payload, "version")
	if !ok {
		return Stamp{}
	}
	return Stamp{Type: t, Version: v}
}

// Stamps writes the header onto a value about to become a payload. It returns
// the bytes; the caller records them.
func (c Covenant) Stamps(kind string, v any) ([]byte, error) {
	if !strings.HasPrefix(kind, c.Namespace+".") {
		return nil, fmt.Errorf("%w: %q is not under %q", ErrOutsideNamespace, kind, c.Namespace)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("harness: a stamped payload must be an object: %w", err)
	}
	m["type"], _ = json.Marshal(kind)
	m["version"], _ = json.Marshal(c.Version)
	// The one byte order, because a stamped payload goes inside the bytes the
	// signature covers and the name is the hash of.
	//   — N4.1, T3.2
	return canon.Marshal(m)
}

// Reads is the covenant applied to an event with its payload examined, and it
// is the stricter of the two readings.
//
// Speaks and Read decide by address and verb. That is enough to know whose
// event it is and whether the verb was declared, but not enough to know the
// harness can act on it: a payload stamped by a different version may mean
// something this version does not implement, and a payload stamped as another
// type is not this act at all.
//
// A stamp that does not match is answered exactly as an unknown verb is —
// refuse or ignore, whichever the harness declared. It is never guessed at, and
// it is never rejected: rejection belongs to the ledger and means the event
// never was.
//
//	— T11.10, T10.6, T12.3, T3.3
func (c Covenant) Reads(e event.Event, kind string) Reading {
	r := c.Read(e)
	if r != Known {
		return r
	}
	s := StampOf(e.Payload)
	if s.Type != kind || s.Version != c.Version {
		if c.Unknown == Ignore {
			return Ignored
		}
		return Refused
	}
	return Known
}
