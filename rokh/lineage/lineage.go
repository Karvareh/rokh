// Package lineage is how a rokh spreads and meets itself: seeds and
// reconcile (contract 4.7, 4.8, 3.3).
//
// It is portable core (C1). Reconcile is a union of records both ways,
// inside each side's scopes. It records no event of its own (C6): only
// `reconcile --merge` adds one rokh.merge, and that is the caller's
// recording, not this package's. The answer always names both halves, and
// the first half is never reported as the whole (R5).
package lineage

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

// Side is one carrier taking part, with the host's owner of its vessel.
//
// Judge and Covers are the session's own checks (contract R4, E3), given by
// the key layer. Judge answers, for an event of this side, "full",
// "lineage", "pending" or "refused": a session transfers only what it can
// judge, never pending. Covers answers whether an envelope names the owner's
// live readers. A side without them is refused (ancestry_unproven): a union
// of records nobody judged is not offered. Unjudged is for
// tests only: every record counts as judged and every envelope as covering.
type Side struct {
	C        *carrier.Carrier
	Own      vessel.Owner
	Judge    func(id frame.ID) string
	Covers   func(id frame.ID, env []byte) bool
	Unjudged bool
}

// ready refuses a side that gives no judge or no owner check.
func (s Side) ready() error {
	if s.Unjudged || (s.Judge != nil && s.Covers != nil) {
		return nil
	}
	return ErrAncestryUnproven
}

// judged reports whether a side may transfer an event.
func (s Side) judged(id frame.ID) bool {
	if s.Judge == nil {
		return true
	}
	switch s.Judge(id) {
	case "full", "lineage":
		return true
	}
	return false
}

// covers reports whether an envelope names the owner (E3).
// The id is the record's own (an event's, or the content's whose signed
// descriptor gives the historical point): the owners live at that point are
// the ones an envelope must name.
func (s Side) covers(id frame.ID, env []byte) bool {
	return s.Covers == nil || s.Covers(id, env)
}

// Half is one side's ending.
type Half struct {
	Outcome vessel.Outcome
	Added   int // records added to this side
	Held    int // records withheld from this side: pending, refused, or not naming the owner
	Err     error
}

// Result names both halves (R5).
type Result struct {
	Local, Remote Half
}

// OK is true only when both halves are recorded.
func (r Result) OK() bool {
	return r.Local.Outcome == vessel.Recorded && r.Remote.Outcome == vessel.Recorded
}

// ExitCode is 0 when both halves are recorded, 4 when either is unknown,
// and 3 for any other failure (R5).
func (r Result) ExitCode() int {
	switch {
	case r.OK():
		return 0
	case r.Local.Outcome == vessel.Unknown || r.Remote.Outcome == vessel.Unknown:
		return 4
	default:
		return 3
	}
}

// String names both halves (R5). A count of records added is given only for
// a half that was recorded: a half that was not recorded added nothing, and a
// half whose ending is unknown is given no number it cannot vouch for. What
// was withheld from a half is said either way, and so is the reason a half
// failed.
func (r Result) String() string {
	half := func(h Half) string {
		s := string(h.Outcome)
		switch {
		case h.Outcome == vessel.Recorded:
			s = fmt.Sprintf("%s (+%d, %d withheld)", h.Outcome, h.Added, h.Held)
		case h.Held > 0:
			s = fmt.Sprintf("%s (%d withheld)", h.Outcome, h.Held)
		}
		if h.Err != nil {
			s += fmt.Sprintf(": %v", h.Err)
		}
		return s
	}
	return fmt.Sprintf("local %s; remote %s", half(r.Local), half(r.Remote))
}

// Errors with stable codes.
var (
	ErrAnchorDiffers = &codeError{"anchor_differs", "the two sides have different anchors; they do not meet"}
	ErrSeedRefused   = &codeError{"seed_refused", "seed refused"}
	// ErrAncestryUnproven refuses a union whose side gives no judge or no
	// owner check (contract C8, R4).
	ErrAncestryUnproven = &codeError{"ancestry_unproven", "a side gives no judge or no owner check; nothing is moved that nobody judged"}
)

type codeError struct{ code, msg string }

func (e *codeError) Error() string { return "lineage: " + e.msg }

// Code returns an error's stable code.
func Code(err error) string {
	var ce *codeError
	if errors.As(err, &ce) {
		return ce.code
	}
	return carrier.Code(err)
}

// HeadOnly is the envelope of an event whose body did not travel.
var HeadOnly = carrier.HeadOnly

// record is one record as it travels: its header and its sealed envelope,
// unopened.
type record struct {
	h   vessel.Header
	env []byte
}

// key names one record as it travels. Envelopes are distinct (E2): one id
// may sit in several envelopes, and each of them is a record of its own.
func key(h vessel.Header, env []byte) string {
	sum := sha256.Sum256(env)
	switch h.Type {
	case vessel.RecEvent:
		return "e" + string(h.ID[:]) + string(sum[:])
	case vessel.RecContent:
		return fmt.Sprintf("c%s/%d/%s", h.ID[:], h.Chunk, sum[:])
	case vessel.RecObject:
		return fmt.Sprintf("o%c%s/%d/%s", h.Space, h.Name[:], h.Chunk, sum[:])
	}
	return ""
}

// holding is what one side holds: every record by key, and every event id
// it holds in any form, whole or as a head only.
type holding struct {
	have   map[string]bool
	events map[frame.ID]bool
}

// holdings lists what a side holds.
func holdings(c *carrier.Carrier) (holding, error) {
	h := holding{have: map[string]bool{}, events: map[frame.ID]bool{}}
	err := c.Vessel().Scan(func(hd vessel.Header, r vessel.Ref) error {
		if hd.Type == vessel.RecEvent {
			h.events[hd.ID] = true
			if carrier.IsHeadOnly(r) {
				return nil
			}
		}
		if hd.Type != vessel.RecEvent && hd.Type != vessel.RecContent && hd.Type != vessel.RecObject {
			return nil
		}
		env, err := c.Vessel().Body(r)
		if err != nil {
			return err
		}
		h.have[key(hd, env)] = true
		return nil
	})
	return h, err
}

// Address reads the address field (tag 0x0005) of an RKH3 body.
func Address(body []byte) (string, bool) {
	fs, err := frame.DecodeFields(body)
	if err != nil {
		return "", false
	}
	v, ok := fs.Get(0x0005)
	return string(v), ok
}

// System reports whether an RKH3 head carries the system mark (tag 0x000B).
func System(head []byte) bool {
	if len(head) < 9+64 {
		return false
	}
	n := int(head[5])<<24 | int(head[6])<<16 | int(head[7])<<8 | int(head[8])
	if 9+n+64 != len(head) {
		return false
	}
	fs, err := frame.DecodeFields(head[9 : 9+n])
	if err != nil {
		return false
	}
	v, ok := fs.Get(0x000B)
	return ok && len(v) == 1 && v[0] == 0x01
}

// Within reports whether addr lies in one of scopes; no scopes means whole.
func Within(addr string, scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if s == "" || addr == s || strings.HasPrefix(addr, s+"/") {
			return true
		}
	}
	return false
}

// offer lists what from holds that to lacks, inside to's scopes. An event
// outside the scopes, or whose body from's session cannot open to judge,
// travels as a head only when to lacks it; system events travel whole.
func offer(src Side, to *carrier.Carrier, toHas holding) ([]record, []frame.ID, int, error) {
	from := src.C
	held := 0
	scopes := to.Vessel().Info().Scopes
	inScope := map[frame.ID]bool{} // content ids named by events that travel whole
	var all []carrier.EventRecord
	whole := map[frame.ID]bool{}    // what the receiver opens: in scope, or system
	unplaced := map[frame.ID]bool{} // a scoped receiver's event this session cannot open
	parents := map[frame.ID][]frame.ID{}
	err := from.Events(func(e carrier.EventRecord) error {
		if !src.judged(e.ID) {
			held++ // pending or refused: it does not travel (C8, R4)
			return nil
		}
		all = append(all, e)
		parents[e.ID] = Parents(e.Head)
		if len(e.Refs) == 0 {
			return nil
		}
		// A receiver with no scopes holds the whole rokh: every judged event
		// travels with every envelope it lacks, whether or not this session
		// can open it; the address is not needed.
		if len(scopes) == 0 || System(e.Head) {
			whole[e.ID] = true
			return nil
		}
		body, err := from.Body(e)
		if err != nil {
			unplaced[e.ID] = true // outside what this session can place
			return nil
		}
		if addr, ok := Address(body); ok && Within(addr, scopes) {
			whole[e.ID] = true
			for _, id := range contentNamed(body) {
				inScope[id] = true
			}
		}
		return nil
	}, nil)
	if err != nil {
		return nil, nil, held, err
	}
	// The necessary lineage (3.3): the signed head of every ancestor of what
	// the receiver opens, and nothing else. An event that is no ancestor of
	// anything in scope does not travel at all.
	travel := map[frame.ID]bool{}
	stack := make([]frame.ID, 0, len(whole))
	for id := range whole {
		stack = append(stack, id)
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if travel[id] {
			continue
		}
		travel[id] = true
		stack = append(stack, parents[id]...)
	}
	var out []record
	for _, e := range all {
		if !travel[e.ID] {
			if unplaced[e.ID] {
				held++ // the session cannot say where it belongs, and it is no ancestor
			}
			continue
		}
		h := vessel.Header{Type: vessel.RecEvent, ID: e.ID, Head: e.Head}
		if !whole[e.ID] {
			if !toHas.events[e.ID] {
				out = append(out, record{h, HeadOnly})
			}
			continue
		}
		// Every envelope of the event that the other side lacks travels.
		for _, r := range e.Refs {
			env, err := from.Vessel().Body(r)
			if err != nil {
				return nil, nil, held, err
			}
			if toHas.have[key(h, env)] {
				continue
			}
			if !src.covers(e.ID, env) {
				held++ // an envelope that does not name the owner does not travel (E3)
				continue
			}
			out = append(out, record{h, env})
		}
	}
	err = from.Vessel().Scan(func(h vessel.Header, r vessel.Ref) error {
		if h.Type != vessel.RecContent {
			return nil
		}
		if len(scopes) > 0 && !inScope[h.ID] {
			return nil
		}
		env, err := from.Vessel().Body(r)
		if err != nil {
			return err
		}
		if toHas.have[key(h, env)] {
			return nil
		}
		if !src.covers(h.ID, env) {
			held++
			return nil
		}
		out = append(out, record{h, env})
		return nil
	})
	// The tips of what travels: no event that travels names them as a parent.
	isParent := map[frame.ID]bool{}
	for id := range travel {
		for _, p := range parents[id] {
			isParent[p] = true
		}
	}
	var tips []frame.ID
	for _, e := range all {
		if travel[e.ID] && !isParent[e.ID] {
			tips = append(tips, e.ID)
		}
	}
	return out, tips, held, err
}

// Parents reads the parents field (tag 0x0004) of an RKH3 head.
func Parents(head []byte) []frame.ID {
	if len(head) < 9+64 {
		return nil
	}
	n := int(head[5])<<24 | int(head[6])<<16 | int(head[7])<<8 | int(head[8])
	if 9+n+64 != len(head) {
		return nil
	}
	fs, err := frame.DecodeFields(head[9 : 9+n])
	if err != nil {
		return nil
	}
	v, _ := fs.Get(0x0004)
	var out []frame.ID
	for i := 0; i+32 <= len(v); i += 32 {
		var id frame.ID
		copy(id[:], v[i:i+32])
		out = append(out, id)
	}
	return out
}

// VerbContentPut is the conventional verb of an event that brings content
// into the vessel; its payload is a content descriptor whose field 0x0001 is
// the content id.
const VerbContentPut = "content.put"

// contentNamed finds the content ids an event names: the descriptor of a
// content.put event, and any 32-byte value following the marker "content:"
// in another payload (tag 0x0007).
func contentNamed(body []byte) []frame.ID {
	fs, err := frame.DecodeFields(body)
	if err != nil {
		return nil
	}
	p, _ := fs.Get(0x0007)
	var out []frame.ID
	if verb, _ := fs.Get(0x0006); string(verb) == VerbContentPut {
		if d, err := frame.DecodeFields(p); err == nil {
			if h, ok := d.Get(0x0001); ok && len(h) == frame.IDSize {
				var id frame.ID
				copy(id[:], h)
				out = append(out, id)
			}
		}
		return out
	}
	for {
		i := bytes.Index(p, []byte("content:"))
		if i < 0 || len(p) < i+8+32 {
			return out
		}
		var id frame.ID
		copy(id[:], p[i+8:i+40])
		out = append(out, id)
		p = p[i+40:]
	}
}

// pins names, on the receiving side, the tips of what travelled, so that its
// walk from references reaches every event that arrived; a tip the side
// already names needs nothing. The first unnamed tip takes "main" when the
// side has none; the others are named beside it by their short id.
func pins(to *carrier.Carrier, tips []frame.ID) (map[string]frame.ID, error) {
	return pinsPreferring(to, tips, frame.Zero)
}

// pinsPreferring is pins where the tip prefer, when it is among the tips,
// takes "main" before any other: a seed's branch goes on from its source's.
func pinsPreferring(to *carrier.Carrier, tips []frame.ID, prefer frame.ID) (map[string]frame.ID, error) {
	refs, err := to.Refs()
	if err != nil {
		return nil, err
	}
	named := map[frame.ID]bool{}
	for _, id := range refs {
		named[id] = true
	}
	ordered := make([]frame.ID, 0, len(tips))
	for _, t := range tips {
		if t == prefer {
			ordered = append([]frame.ID{t}, ordered...)
		} else {
			ordered = append(ordered, t)
		}
	}
	out := map[string]frame.ID{}
	for _, t := range ordered {
		if named[t] {
			continue
		}
		name := "main"
		if _, taken := refs[name]; taken {
			name = pinName(t)
		} else if _, taken := out[name]; taken {
			name = pinName(t)
		}
		out[name] = t
	}
	return out, nil
}

// pinName is the name a tip is pinned under beside "main".
func pinName(t frame.ID) string { return "main~" + t.String()[:16] }

// apply commits the records and pins into one side, as one commit. With
// nothing to add, nothing is written and the half is recorded.
func apply(s Side, recs []record, refs map[string]frame.ID) Half {
	if len(recs) == 0 && len(refs) == 0 {
		return Half{Outcome: vessel.Recorded}
	}
	r, err := s.C.Begin(s.Own)
	if err != nil {
		return Half{Outcome: vessel.NotRecorded, Err: err}
	}
	for _, x := range recs {
		var err error
		switch x.h.Type {
		case vessel.RecEvent:
			err = r.EventEnvelope(x.h.ID, x.h.Head, x.env)
		default:
			err = r.ContentEnvelope(x.h, x.env)
		}
		if err != nil {
			r.Abandon()
			return Half{Outcome: vessel.NotRecorded, Err: err}
		}
	}
	names := make([]string, 0, len(refs))
	for n := range refs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := r.SetRef(n, refs[n]); err != nil {
			r.Abandon()
			return Half{Outcome: vessel.NotRecorded, Err: err}
		}
	}
	out, err := r.Commit()
	return Half{Outcome: out, Added: len(recs), Err: err}
}

// Reconcile is the union of records both ways (R1..R5). It refuses two
// different anchors before anything is read. Both halves are always
// attempted and always named; it records no event.
func Reconcile(local, remote Side) (Result, error) {
	if local.C.Anchor() != remote.C.Anchor() {
		return Result{
			Local:  Half{Outcome: vessel.NotRecorded, Err: ErrAnchorDiffers},
			Remote: Half{Outcome: vessel.NotRecorded, Err: ErrAnchorDiffers},
		}, ErrAnchorDiffers
	}
	for _, s := range []Side{local, remote} {
		if err := s.ready(); err != nil {
			return failed(err), err
		}
	}
	lHas, err := holdings(local.C)
	if err != nil {
		return failed(err), err
	}
	rHas, err := holdings(remote.C)
	if err != nil {
		return failed(err), err
	}
	toLocal, lTips, lHeld, err := offer(remote, local.C, lHas)
	if err != nil {
		return failed(err), err
	}
	toRemote, rTips, rHeld, err := offer(local, remote.C, rHas)
	if err != nil {
		return failed(err), err
	}
	lPins, err := pins(local.C, lTips)
	if err != nil {
		return failed(err), err
	}
	rPins, err := pins(remote.C, rTips)
	if err != nil {
		return failed(err), err
	}
	var res Result
	res.Local = apply(local, toLocal, lPins)
	res.Remote = apply(remote, toRemote, rPins)
	res.Local.Held, res.Remote.Held = lHeld, rHeld
	if !res.OK() {
		return res, fmt.Errorf("lineage: reconcile did not complete: %s", res)
	}
	return res, nil
}

func failed(err error) Result {
	return Result{Local: Half{Outcome: vessel.NotRecorded, Err: err}, Remote: Half{Outcome: vessel.NotRecorded, Err: err}}
}
