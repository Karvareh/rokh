package event

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"strings"

	"rokh/frame"
)

// Grant payload fields.
const (
	gTagSubject     frame.Tag = 0x0001 // 32 bytes, the key receiving the right
	gTagScope       frame.Tag = 0x0002 // UTF-8 address prefix. Absent iff whole ledger
	gTagVerbs       frame.Tag = 0x0003 // verb list. Absent iff any non-reserved verb
	gTagCanDelegate frame.Tag = 0x0004 // 0x01. Absent iff false
	gTagOpen        frame.Tag = 0x0005 // 0x01: an open address; no subject, no delegation (contract 4.5)
	gTagRead        frame.Tag = 0x0006 // 0x01: the open address is read-open too
)

// MaxVerbsPerGrant bounds the verb list in one grant.
const MaxVerbsPerGrant = 32

// Grant confers a right that is bounded, explicit and revocable.
//
// Every default is the narrowest one, never the widest: an unwritten scope is
// not "the whole ledger" by accident, an unwritten verb list never includes
// reserved verbs, and sub-delegation does not exist until written.
// Granting is an event and so is revoking. A grant has an address, and its
// bound is an event — never a clock.
//
// A delegate's power is not in its claws; it is in the covenant. Whatever it
// can do, it can do because it was entrusted — and every entrustment is
// taken back.
//
//	— T6, N7.6
type Grant struct {
	Subject     ed25519.PublicKey
	Scope       string
	Verbs       []string
	CanDelegate bool
	// Open makes this grant an open address: any key may write under it,
	// inside its scope and verbs, and never a reserved verb. An open grant
	// has no subject and cannot delegate. Only the root writes one.
	Open bool
	// Read makes an open address read-open: what is written there is sealed
	// to the system reader too.
	Read bool
}

// Revoke withdraws a grant.
//
// Invariant: revocation applies from here on. Events already written in the
// causal past stay valid, because their authority was settled at signing
// time.
type Revoke struct {
	Target frame.ID
}

func normVerbs(vs []string) ([]string, error) {
	if len(vs) == 0 {
		return nil, nil
	}
	cp := append([]string(nil), vs...)
	sort.Strings(cp)
	for i, v := range cp {
		if err := ValidVerb(v); err != nil {
			return nil, err
		}
		// Reserved verbs never appear in a list: granting is governed by
		// CanDelegate, and revocation by "everyone may withdraw their own".
		if IsReserved(v) {
			return nil, fmt.Errorf("%w: reserved verb in grant list (%s)", ErrShape, v)
		}
		if i > 0 && cp[i-1] == v {
			return nil, fmt.Errorf("%w: duplicate verb in grant", ErrShape)
		}
	}
	return cp, nil
}

func encodeVerbs(vs []string) []byte {
	var out []byte
	for _, v := range vs {
		out = append(out, byte(len(v)))
		out = append(out, v...)
	}
	return out
}

func decodeVerbs(b []byte) ([]string, error) {
	var out []string
	for len(b) > 0 {
		n := int(b[0])
		b = b[1:]
		if n == 0 || len(b) < n {
			return nil, fmt.Errorf("%w: truncated verb list", ErrShape)
		}
		v := string(b[:n])
		b = b[n:]
		if err := ValidVerb(v); err != nil {
			return nil, err
		}
		if len(out) > 0 && out[len(out)-1] >= v {
			return nil, fmt.Errorf("%w: verbs not ordered and distinct", ErrShape)
		}
		out = append(out, v)
		if len(out) > MaxVerbsPerGrant {
			return nil, fmt.Errorf("%w: too many verbs in grant", ErrShape)
		}
	}
	return out, nil
}

// Encode writes a grant payload in canonical form.
func (g Grant) Encode() ([]byte, error) {
	if err := g.shape(); err != nil {
		return nil, err
	}
	if g.Scope != "" {
		if err := ValidAddress(g.Scope); err != nil {
			return nil, err
		}
	}
	vs, err := normVerbs(g.Verbs)
	if err != nil {
		return nil, err
	}
	var fs frame.Fields
	if !g.Open {
		fs = append(fs, frame.Field{Tag: gTagSubject, Value: append([]byte(nil), g.Subject...)})
	}
	if g.Open {
		fs = append(fs, frame.Field{Tag: gTagOpen, Value: []byte{0x01}})
	}
	if g.Read {
		fs = append(fs, frame.Field{Tag: gTagRead, Value: []byte{0x01}})
	}
	if g.Scope != "" {
		fs = append(fs, frame.Field{Tag: gTagScope, Value: []byte(g.Scope)})
	}
	if len(vs) > 0 {
		fs = append(fs, frame.Field{Tag: gTagVerbs, Value: encodeVerbs(vs)})
	}
	if g.CanDelegate {
		fs = append(fs, frame.Field{Tag: gTagCanDelegate, Value: []byte{0x01}})
	}
	return frame.EncodeFields(fs)
}

// DecodeGrant reads a grant payload and enforces the canonical form.
func DecodeGrant(b []byte) (Grant, error) {
	var g Grant
	fs, err := frame.DecodeFields(b)
	if err != nil {
		return g, err
	}
	for _, f := range fs {
		switch f.Tag {
		case gTagSubject:
			if len(f.Value) != ed25519.PublicKeySize {
				return g, fmt.Errorf("%w: subject key size", ErrShape)
			}
			g.Subject = ed25519.PublicKey(append([]byte(nil), f.Value...))
		case gTagScope:
			g.Scope = string(f.Value)
			if err := ValidAddress(g.Scope); err != nil {
				return g, err
			}
		case gTagVerbs:
			if g.Verbs, err = decodeVerbs(f.Value); err != nil {
				return g, err
			}
		case gTagCanDelegate:
			if len(f.Value) != 1 || f.Value[0] != 0x01 {
				return g, fmt.Errorf("%w: can-delegate is 0x01 or absent", ErrShape)
			}
			g.CanDelegate = true
		case gTagOpen:
			if len(f.Value) != 1 || f.Value[0] != 0x01 {
				return g, fmt.Errorf("%w: open is 0x01 or absent", ErrShape)
			}
			g.Open = true
		case gTagRead:
			if len(f.Value) != 1 || f.Value[0] != 0x01 {
				return g, fmt.Errorf("%w: read is 0x01 or absent", ErrShape)
			}
			g.Read = true
		default:
			return g, fmt.Errorf("%w in grant (tag=%d)", ErrUnknown, f.Tag)
		}
	}
	if err := g.shape(); err != nil {
		return g, err
	}
	return g, nil
}

// shape holds the two kinds of grant apart: a grant to a key names its
// subject; an open grant names none and cannot delegate (contract 4.5).
func (g Grant) shape() error {
	if g.Open {
		if g.Subject != nil {
			return fmt.Errorf("%w: an open grant names no subject", ErrShape)
		}
		if g.CanDelegate {
			return fmt.Errorf("%w: an open grant cannot delegate", ErrShape)
		}
		return nil
	}
	if g.Read {
		return fmt.Errorf("%w: only an open address is read-open", ErrShape)
	}
	if len(g.Subject) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: grant without a subject, or a subject of the wrong size", ErrShape)
	}
	return nil
}

// Encode writes a revoke payload.
func (r Revoke) Encode() ([]byte, error) {
	return frame.EncodeFields(frame.Fields{
		{Tag: gTagSubject, Value: append([]byte(nil), r.Target[:]...)},
	})
}

// DecodeRevoke reads a revoke payload.
func DecodeRevoke(b []byte) (Revoke, error) {
	var r Revoke
	fs, err := frame.DecodeFields(b)
	if err != nil {
		return r, err
	}
	for _, f := range fs {
		switch f.Tag {
		case gTagSubject:
			if len(f.Value) != frame.IDSize {
				return r, fmt.Errorf("%w: revoke target size", ErrShape)
			}
			copy(r.Target[:], f.Value)
		default:
			return r, fmt.Errorf("%w in revoke (tag=%d)", ErrUnknown, f.Tag)
		}
	}
	if r.Target.IsZero() {
		return r, fmt.Errorf("%w: revoke without a target", ErrShape)
	}
	return r, nil
}

// ScopeCovers reports whether a scope covers an address. An empty scope means
// the whole ledger. The boundary is the component separator, not a string
// prefix: scope "a" does not cover "ab".
func ScopeCovers(scope, addr string) bool {
	if scope == "" {
		return true
	}
	return addr == scope || strings.HasPrefix(addr, scope+"/")
}

// ScopeWithin reports whether the inner scope is contained in the outer one.
// A scope only ever narrows. A writing delegate cannot mint a delegate
// wider than the one it holds, and this is the whole of that rule.
//
//	— T6.1, N4.4
func ScopeWithin(inner, outer string) bool {
	if outer == "" {
		return true
	}
	if inner == "" {
		return false // "whole ledger" is inside no bounded scope
	}
	return ScopeCovers(outer, inner)
}

// allowsVerb reports whether this grant permits a non-reserved verb.
// Reserved verbs never pass through here; their rules live in the core.
func (g Grant) allowsVerb(verb string) bool {
	if IsReserved(verb) {
		return false
	}
	if len(g.Verbs) == 0 {
		return true
	}
	for _, v := range g.Verbs {
		if v == verb {
			return true
		}
	}
	return false
}

// Allows reports whether this grant permits an action.
func (g Grant) Allows(addr, verb string) bool {
	return ScopeCovers(g.Scope, addr) && g.allowsVerb(verb)
}

// VerbsWithin reports whether the inner verb list is contained in the outer.
func VerbsWithin(inner, outer []string) bool {
	if len(outer) == 0 {
		// The outer grant means "any non-reserved verb".
		if len(inner) == 0 {
			return true
		}
		for _, v := range inner {
			if IsReserved(v) {
				return false
			}
		}
		return true
	}
	if len(inner) == 0 {
		return false // "any non-reserved verb" is wider than a fixed list
	}
	set := make(map[string]bool, len(outer))
	for _, v := range outer {
		set[v] = true
	}
	for _, v := range inner {
		if !set[v] {
			return false
		}
	}
	return true
}
