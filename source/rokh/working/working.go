// Package working is the boundary between doing and recording.
//
// What you see on screen and what you have done is the working state. It is
// not final and changes as often as you like. One explicit act turns it into
// an event, and from then on there is no way back.
//
// Whether the working state lives in memory or on disk has nothing to do with
// the architecture. What matters is that nothing crosses that boundary by
// itself. Both stores in this package are therefore interchangeable, and the
// tests run the same script through each and require the same outcome.
//
//	— T4.4, T4.5, N4.7
package working

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"rokh/event"
	"rokh/frame"
)

// MaxDrafts bounds how much unfinished work one working state may hold. It is
// a base-profile number, not a law: a ceiling exists so a runaway writer
// cannot fill a carrier with things that were never recorded.
const MaxDrafts = 1024

var (
	// ErrNoSuchDraft is returned for a handle that names nothing. A handle
	// that has been closed names nothing either: it became an event, and
	// events are not addressed from here.
	ErrNoSuchDraft = errors.New("working: no such draft")
	// ErrFull is returned when the working state is at its ceiling.
	ErrFull = errors.New("working: too many drafts")
)

// Handle names a draft *inside the working state*. It is deliberately not an
// event name: a draft has no name, because a name is the hash of bytes that
// do not exist yet.
//
//	— T3.2, T9.2
type Handle string

// Draft is a sentence written but not yet closed.
type Draft struct {
	Address string              `json:"address"`
	Verb    string              `json:"verb"`
	Payload []byte              `json:"payload,omitempty"`
	Attest  []event.Attestation `json:"attest,omitempty"`
}

// Store is where drafts wait. Memory and disk both satisfy it, and the design
// above the interface cannot tell which it has.
//
//	— T4.5
//
// Its methods speak in plain strings rather than Handle so that whoever holds
// the carrier key can satisfy it without importing this package. A durable
// working state must be sealed by the carrier: Rokh deliberately writes
// nothing outside it.
//
//	— T8
type Store interface {
	Put(name string, b []byte) error
	Get(name string) ([]byte, bool, error)
	Delete(name string) error
	List() ([]string, error)
}

// State is one open working state, raised when a carrier is opened. What is
// in it is not yet the ledger.
//
//	— T8.2
type State struct {
	store Store
	next  int
}

// Open raises a working state over a store. It resumes from whatever the
// store already holds — reopening is not a fresh start, and nothing waiting
// there is lost or recorded by the act of opening.
//
//	— T8.2
func Open(s Store) (*State, error) {
	hs, err := s.List()
	if err != nil {
		return nil, err
	}
	st := &State{store: s}
	for _, h := range hs {
		if n, err := strconv.Atoi(strings.TrimPrefix(h, "d")); err == nil && n >= st.next {
			st.next = n + 1
		}
	}
	return st, nil
}

// Write puts a draft into the working state and returns its handle. Nothing
// is recorded.
func (s *State) Write(d Draft) (Handle, error) {
	hs, err := s.store.List()
	if err != nil {
		return "", err
	}
	if len(hs) >= MaxDrafts {
		return "", ErrFull
	}
	h := Handle("d" + strconv.Itoa(s.next))
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	if err := s.store.Put(string(h), b); err != nil {
		return "", err
	}
	s.next++
	return h, nil
}

// Revise replaces a draft. The working state is not final and may be changed
// as many times as one likes.
//
//	— T4.4
func (s *State) Revise(h Handle, d Draft) error {
	if _, ok, err := s.store.Get(string(h)); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %s", ErrNoSuchDraft, h)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.store.Put(string(h), b)
}

// Discard throws a draft away. It leaves nothing behind, because it never was
// an event: only what crosses the boundary is permanent.
//
//	— T4.4
func (s *State) Discard(h Handle) error {
	if _, ok, err := s.store.Get(string(h)); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %s", ErrNoSuchDraft, h)
	}
	return s.store.Delete(string(h))
}

// Read returns a draft as it currently stands.
func (s *State) Read(h Handle) (Draft, error) {
	var d Draft
	b, ok, err := s.store.Get(string(h))
	if err != nil {
		return d, err
	}
	if !ok {
		return d, fmt.Errorf("%w: %s", ErrNoSuchDraft, h)
	}
	return d, json.Unmarshal(b, &d)
}

// List names every draft waiting, in a stable order.
func (s *State) List() ([]Handle, error) {
	names, err := s.store.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(names, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(names[i], "d"))
		b, _ := strconv.Atoi(strings.TrimPrefix(names[j], "d"))
		return a < b
	})
	out := make([]Handle, 0, len(names))
	for _, n := range names {
		out = append(out, Handle(n))
	}
	return out, nil
}

// Form is the form of the effect: what closing this draft would do.
//
// It carries no name, and that absence is the point. Before closing, what is
// shown is a prediction of the effect, not the effect. A name is the hash of
// bytes that have not been made yet, so showing one here would claim an event
// exists when it does not.
//
//	— T9.2
type Form struct {
	Address      string
	Verb         string
	PayloadBytes int
	Attestations int
	Parents      []frame.ID // the parents it would name, as the ledger stands now
	Authority    string     // how the writer would be entitled to write it
}

// Preview reports the form of the effect. It changes nothing: calling it a
// thousand times leaves the working state and the ledger exactly as they were.
//
//	— T9.2, T4.1
func (s *State) Preview(h Handle, parents []frame.ID, authority string) (Form, error) {
	d, err := s.Read(h)
	if err != nil {
		return Form{}, err
	}
	return Form{
		Address:      d.Address,
		Verb:         d.Verb,
		PayloadBytes: len(d.Payload),
		Attestations: len(d.Attest),
		Parents:      append([]frame.ID(nil), parents...),
		Authority:    authority,
	}, nil
}

// Recorder is whatever turns a draft into a recorded event — signing it,
// putting it to the ledger and moving the branch. This package neither signs
// nor stores; it only decides *when* that may happen, which is: on Close, and
// nowhere else.
//
//	— T4.1
type Recorder interface {
	Record(Draft) (frame.ID, error)
}

// Close is the explicit act, and the only way anything in this package
// reaches a Recorder. There is no timer, no flush, no commit-on-exit and no
// finalizer: nothing crosses this boundary by itself.
//
// After it succeeds the draft is gone from the working state, because it is
// no longer a draft — it is an event, and an event does not come back.
//
//	— T4.4, T4.5, N4.7, N-Axiom2
func (s *State) Close(h Handle, rec Recorder) (frame.ID, error) {
	var zero frame.ID
	d, err := s.Read(h)
	if err != nil {
		return zero, err
	}
	id, err := rec.Record(d)
	if err != nil {
		// The recording did not happen, so the draft stays exactly where it
		// was. A failed crossing leaves the working state untouched.
		return zero, err
	}
	if err := s.store.Delete(string(h)); err != nil {
		return id, err
	}
	return id, nil
}
