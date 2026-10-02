package event

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"rokh/frame"
)

// A signed byte string is never rewritten, so a change of format is a new
// generation, with its own name and its own verifier. v1 reads one generation,
// RKH3 (contract C4 and 3.4); the generations before it, RKH1 and RKH2, are
// neither read nor converted (contract section 7).
//
// What this build does with their bytes is refuse them by name: "this is RKH1"
// and not "this is malformed". An unread event is not a refuted one, and
// keeping the two apart is the difference between a ledger that can grow and
// one that has to burn its past to change. The refusal touches nothing: the
// bytes are exactly what they were and still hash to the name they had. And
// it is the generation that is named, not every doubtful magic: bytes that
// are no generation at all are refused without being given a name.
//
//	— T3.7, T12.3
func TestEarlierGenerationsAreRefusedByNameNotCalledMalformed(t *testing.T) {
	pub, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID
	e, err := SignFrom(Event{Carrier: &anchor, Author: pub, Parents: []frame.ID{g.ID},
		Address: "home/journal", Verb: "note", Payload: []byte("written in a generation of its own")},
		priv, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Superseded) == 0 {
		t.Fatal("no earlier generation is named; this test watches nothing")
	}
	for _, old := range frame.Superseded {
		raw := append([]byte(nil), e.Raw...)
		copy(raw, old)
		kept := append([]byte(nil), raw...)
		name := frame.Hash(raw)

		_, err := Parse(raw)
		if err == nil {
			t.Fatalf("%s: an event of an earlier generation parsed under %s", old, frame.Magic)
		}
		if !errors.Is(err, frame.ErrMagic) || errors.Is(err, ErrShape) || errors.Is(err, ErrSig) {
			t.Fatalf("%s: refused for the wrong reason (%v); the reason must be the generation, not the shape", old, err)
		}
		if !strings.Contains(err.Error(), old) || !strings.Contains(err.Error(), "earlier generation") {
			t.Fatalf("%s: the refusal does not name the generation it found: %v", old, err)
		}
		if !bytes.Equal(raw, kept) || frame.Hash(raw) != name {
			t.Fatalf("%s: refusing the bytes changed them", old)
		}
	}

	// The generation v1 holds still reads: the refusal is by name, not by
	// suspicion of every event.
	if _, err := Parse(e.Raw); err != nil {
		t.Fatalf("the event of this generation stopped parsing: %v", err)
	}
	// Bytes of no generation are refused and are not called an earlier one.
	stranger := append([]byte(nil), e.Raw...)
	copy(stranger, "XYZ9")
	_, err = Parse(stranger)
	if !errors.Is(err, frame.ErrMagic) || strings.Contains(err.Error(), "earlier generation") {
		t.Fatalf("an unknown magic was answered as %v", err)
	}
}

// Freshness is what separates "twice, separately" from "once, repeated". The
// same content written twice, as two acts, is two events with two names; and
// written once and replayed, it is one. Every event carries its freshness in
// its signed head (tag 0x0009, FreshSize bytes; contract 3.4), and a head
// without it is not an event with a default: it is refused. The one way to
// write a new act draws the freshness from the host's randomness, refuses
// when there is none rather than guess, and refuses to re-sign somebody
// else's draw.
//
//	— T3.6
func TestTheSameContentTwiceIsTwoEventsWhenItIsTwoActs(t *testing.T) {
	pub, priv := key(t)
	g := genesis(t, priv)
	anchor := g.ID
	words := Event{Carrier: &anchor, Author: pub, Parents: []frame.ID{g.ID},
		Address: "home/journal", Verb: "note", Payload: []byte("the very same words")}

	// Two acts: two draws.
	first, err := SignFrom(words, priv, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SignFrom(words, priv, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if first.Event.Fresh == second.Event.Fresh {
		t.Fatal("two draws came out identical; the source is not what the ruling asks for")
	}
	if first.ID == second.ID {
		t.Fatal("two separate acts with the same words became one event")
	}
	// And the same act, replayed, is the same event, not a third one.
	replay := words
	replay.Fresh, replay.Salt = first.Event.Fresh, first.Event.Salt
	again, err := Sign(replay, priv)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || !bytes.Equal(again.Raw, first.Raw) {
		t.Fatal("replaying one act produced a different event")
	}

	// Freshness is exactly FreshSize bytes, in the signed head.
	h, _, err := frame.ParseHead(first.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := h.Fields.Get(TagFresh); !ok || len(v) != FreshSize {
		t.Fatalf("the signed head carries freshness of %d bytes, want %d", len(v), FreshSize)
	}
	// Strip it and sign the head again: what comes out is not an event.
	var without frame.Fields
	for _, fl := range h.Fields {
		if fl.Tag != TagFresh {
			without = append(without, fl)
		}
	}
	signed, err := frame.BuildHead(frame.KindEvent, without)
	if err != nil {
		t.Fatal(err)
	}
	head, err := frame.SealHead(signed, ed25519.Sign(priv, signed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(append(append([]byte(nil), head...), first.Body...)); err == nil {
		t.Fatal("an event with no freshness was accepted")
	}
	if _, err := Parse(head); err == nil {
		t.Fatal("a head with no freshness was accepted as a head only")
	}

	// The way to write a new act does not re-sign somebody else's draw.
	if _, err := SignFrom(replay, priv, rand.Reader); err == nil {
		t.Fatal("an event that already carries freshness was signed as a new act")
	}
	// With no randomness from the host it refuses rather than guess; with it,
	// every call is a new act.
	saved := Entropy
	defer func() { Entropy = saved }()
	Entropy = nil
	if _, err := SignFresh(words, priv); err == nil {
		t.Fatal("a new act was signed with no randomness to draw its freshness from")
	}
	Entropy = rand.Reader
	a, err := SignFresh(words, priv)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SignFresh(words, priv)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("two calls for two acts made one event")
	}
}
