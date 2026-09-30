package bundle

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"rokh/covenant"
	"rokh/frame"
	"rokh/seal"
)

// shareSealed grants a peer a scope and names the reading key their bundles
// are to be closed for.
func (w *world) shareSealed(peer ed25519.PublicKey, scope string, reading []byte) frame.ID {
	w.t.Helper()
	p, err := covenant.Share{Subject: peer, Scope: scope, SealTo: reading}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.write(covenant.Address, covenant.VerbShare, p)
}

// Disclosure is decided when the bundle is sealed, not when it is sent, and
// this is where that becomes bytes rather than intention. What leaves is
// already closed for one reader: the courier carries it, cannot read it, and
// no third party can either.
//
//	— T13.1, T7.4, N4.9
func TestWhatLeavesIsAlreadyClosedForOneReader(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)

	reading, err := seal.NewReading()
	if err != nil {
		t.Fatal(err)
	}
	w.shareSealed(peer, "home/journal", reading.PublicKey().Bytes())
	one := w.write("home/journal/one", "note", []byte("a private line"))

	sel, err := Disclosable(w.led, peer, frame.Zero, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !has(sel.IDs, one) {
		t.Fatal("the event in scope was not selected")
	}

	covs := activeFor(w, peer)
	key, ok := ReadingKeyFor(covs, "home/journal/one")
	if !ok {
		t.Fatal("the covenant names a reading key and it was not found")
	}
	boxes, err := SealFor(w.led, sel, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != len(sel.IDs) {
		t.Fatalf("sealed %d of %d selected events", len(boxes), len(sel.IDs))
	}

	// Nothing readable travels.
	for i, b := range boxes {
		s, _ := w.led.Get(sel.IDs[i])
		if bytes.Contains(b.Box, s.Raw) {
			t.Fatal("an event travelled in the clear inside its own seal")
		}
		if bytes.Contains(b.Box, []byte("a private line")) {
			t.Fatal("the payload travelled in the clear")
		}
	}

	// The named reader opens exactly what was sent, each under its own name.
	for i, b := range boxes {
		got, err := seal.Open(reading, b, sel.IDs[i][:])
		if err != nil {
			t.Fatalf("the named reader could not open box %d: %v", i, err)
		}
		s, _ := w.led.Get(sel.IDs[i])
		if !bytes.Equal(got, s.Raw) {
			t.Fatalf("box %d opened to something other than its event", i)
		}
	}

	// Anybody else — the courier included, who holds no key at all — gets
	// nothing.
	stranger, err := seal.NewReading()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seal.Open(stranger, boxes[0], sel.IDs[0][:]); !errors.Is(err, seal.ErrNotForYou) {
		t.Fatal("a stranger opened a sealed bundle")
	}
	// And a box cannot be offered as though it were a different event.
	if len(boxes) > 1 {
		if _, err := seal.Open(reading, boxes[0], sel.IDs[1][:]); err == nil {
			t.Fatal("a sealed event opened under another event's name")
		}
	}
}

// A covenant that names no reading key seals nothing, and says so — rather
// than quietly handing over the plain bytes because there was nowhere to put
// them.
//
//	— T13.1, T7.4
func TestWithoutAReadingKeyNothingIsSealedAndItSaysSo(t *testing.T) {
	w := newWorld(t)
	peer := newKey(t)
	w.share(peer, "home/journal") // no SealTo
	w.write("home/journal/one", "note", []byte("x"))

	sel, err := Disclosable(w.led, peer, frame.Zero, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadingKeyFor(activeFor(w, peer), "home/journal/one"); ok {
		t.Fatal("a covenant with no reading key reported one")
	}
	if _, err := SealFor(w.led, sel, nil); err == nil {
		t.Fatal("sealing to nobody succeeded")
	}
}

func activeFor(w *world, peer ed25519.PublicKey) []covenant.Covenant {
	var out []covenant.Covenant
	for _, c := range covenant.ActiveAt(w.led) {
		if c.For(peer) {
			out = append(out, c)
		}
	}
	return out
}
