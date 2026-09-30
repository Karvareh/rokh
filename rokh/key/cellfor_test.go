package key

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"rokh/event"
)

// A cell made from a keyring add's slot blob and reader, for another vessel's
// key, opens under the key's own passphrase to the key's own secret and to
// that vessel's key, as the cell made with the secret does; another
// passphrase opens nothing, and a blob or reader of the wrong size is refused.
func TestACellForAKeyOpensWithItsOwnPassphraseOnly(t *testing.T) {
	rnd := &stream{seed: 7}
	salt := bytes.Repeat([]byte{3}, 32)
	r, err := NewReader(rnd)
	if err != nil {
		t.Fatal(err)
	}
	s := Secret{Key: [32]byte{9}, Gen: 2}
	copy(s.Reader[:], r.Bytes())
	kk, err := PassKey("synthetic key passphrase", salt, 1)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := Blob(kk, s, rnd)
	if err != nil {
		t.Fatal(err)
	}
	vk := bytes.Repeat([]byte{5}, 32)
	cell, err := CellFor(blob, r.Public(), vk, rnd)
	if err != nil || len(cell) != CellSize || !bytes.Equal(cell[:BlobSize], blob) {
		t.Fatalf("the cell: %v, %d bytes", err, len(cell))
	}
	got, gotVK, err := OpenCell(kk, cell)
	if err != nil || got != s || !bytes.Equal(gotVK, vk) {
		t.Fatalf("the key's passphrase does not open its cell to its secret and the vessel key: %v", err)
	}
	other, err := PassKey("another synthetic passphrase", salt, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenCell(other, cell); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("another passphrase opened the cell: %v", err)
	}
	if _, err := CellFor(blob[1:], r.Public(), vk, rnd); err == nil {
		t.Fatal("a short blob made a cell")
	}
	if _, err := CellFor(blob, r.Public()[1:], vk, rnd); err == nil {
		t.Fatal("a short reader made a cell")
	}
}

// Step 2 (R2; a ruling of the design): which live generations
// a vessel takes the cell of. A vessel of the whole rokh takes every key
// whose add carries a slot; a slice takes one whose reads, or the scope of a
// live grant to its signer, touch one of its scopes, one inside the other at
// a component's boundary; never the owner's generation, never a key without a
// slot, never a revoke.
func TestAVesselTakesTheCellsOfTheKeysThatTouchIt(t *testing.T) {
	slot := bytes.Repeat([]byte{1}, BlobSize)
	reader := bytes.Repeat([]byte{2}, event.ReaderSize)
	signer := ed25519.PublicKey(bytes.Repeat([]byte{3}, ed25519.PublicKeySize))
	mk := func(reads []string, writes bool) event.Keyring {
		k := event.Keyring{Op: event.KeyringAdd, Key: [32]byte{7}, Gen: 1, Name: "synthetic", Reader: reader, Reads: reads, Slot: slot}
		if writes {
			k.Signer = signer
		}
		return k
	}
	journal := []string{"journal"}
	for _, c := range []struct {
		name            string
		k               event.Keyring
		granted, scopes []string
		want            bool
	}{
		{"a reader of journal, the whole rokh", mk(journal, false), nil, nil, true},
		{"a key of no right, the whole rokh", mk(nil, false), nil, nil, true},
		{"a reader of journal, a slice of journal", mk(journal, false), nil, journal, true},
		{"a reader of journal/day, a slice of journal", mk([]string{"journal/day"}, false), nil, journal, true},
		{"a reader of journal, a slice of journal/day", mk(journal, false), nil, []string{"journal/day"}, true},
		{"a reader of everything, a slice of journal", mk([]string{""}, false), nil, journal, true},
		{"a reader of work, a slice of journal", mk([]string{"work"}, false), nil, journal, false},
		{"a reader of journalism, a slice of journal", mk([]string{"journalism"}, false), nil, journal, false},
		{"a key of no right, a slice of journal", mk(nil, false), nil, journal, false},
		{"a writer in journal reading nothing, a slice of journal", mk(nil, true), journal, journal, true},
		{"a writer in work reading nothing, a slice of journal", mk(nil, true), []string{"work"}, journal, false},
		{"a grant's scope with no signer, a slice of journal", mk(nil, false), journal, journal, false},
	} {
		if got := Takes(c.k, c.granted, c.scopes); got != c.want {
			t.Errorf("%s: takes %v, want %v", c.name, got, c.want)
		}
	}
	owner := mk([]string{""}, true)
	owner.Key = [32]byte{}
	noSlot := mk([]string{""}, false)
	noSlot.Slot = nil
	revoke := mk(journal, false)
	revoke.Op = event.KeyringRevoke
	for name, k := range map[string]event.Keyring{"the owner": owner, "a key without a slot": noSlot, "a revoke": revoke} {
		if Takes(k, nil, nil) || Takes(k, journal, journal) {
			t.Errorf("a vessel takes the cell of %s", name)
		}
	}
}

// Step 2 (R2, contract 4.6): Install places each new cell where nobody's
// cell is. The owner's cell and a live key's cell stay where they are; the
// cell of a generation taken back and the vessel's random filling are free; a
// cell already there is not placed again, so installing twice changes
// nothing; the slots given are not changed in place; and when the free cells
// do not hold them all, none is placed.
func TestInstallPlacesCellsWhereNobodysCellIs(t *testing.T) {
	rnd := &stream{seed: 11}
	vk := bytes.Repeat([]byte{5}, 32)
	keyCell := func() ([]byte, []byte) {
		r, err := NewReader(rnd)
		if err != nil {
			t.Fatal(err)
		}
		blob, err := read(rnd, BlobSize)
		if err != nil {
			t.Fatal(err)
		}
		cell, err := CellFor(blob, r.Public(), vk, rnd)
		if err != nil {
			t.Fatal(err)
		}
		return cell, blob
	}
	owner, err := NewReader(rnd)
	if err != nil {
		t.Fatal(err)
	}
	ownerBlob, err := read(rnd, BlobSize)
	if err != nil {
		t.Fatal(err)
	}
	ownerCell, err := CellFor(ownerBlob, owner.Public(), vk, rnd)
	if err != nil {
		t.Fatal(err)
	}
	slots := make([][]byte, 32)
	slots[0] = ownerCell
	liveCell, liveBlob := keyCell()
	slots[1] = liveCell
	goneCell, _ := keyCell() // a generation taken back: its blob is not held
	slots[2] = goneCell
	for i := 3; i < len(slots); i++ {
		if slots[i], err = read(rnd, CellSize); err != nil {
			t.Fatal(err)
		}
	}
	before := make([][]byte, len(slots))
	for i, s := range slots {
		before[i] = append([]byte(nil), s...)
	}
	held := map[string]bool{string(liveBlob): true}
	a, _ := keyCell()
	b, _ := keyCell()
	out, n, err := Install(slots, owner, held, [][]byte{a, liveCell, b, a})
	if err != nil || n != 2 {
		t.Fatalf("installed %d cells: %v", n, err)
	}
	if !bytes.Equal(out[0], ownerCell) || !bytes.Equal(out[1], liveCell) || !bytes.Equal(out[2], a) || !bytes.Equal(out[3], b) {
		t.Fatal("the cells did not go where nobody's cell is, in order")
	}
	for i := 4; i < len(out); i++ {
		if !bytes.Equal(out[i], before[i]) {
			t.Fatalf("slot %d changed", i)
		}
	}
	for i := range slots {
		if !bytes.Equal(slots[i], before[i]) {
			t.Fatalf("the slots given were changed in place at %d", i)
		}
	}
	again, n, err := Install(out, owner, held, [][]byte{a, b, liveCell})
	if err != nil || n != 0 {
		t.Fatalf("installing again placed %d cells: %v", n, err)
	}
	for i := range out {
		if !bytes.Equal(again[i], out[i]) {
			t.Fatalf("installing again changed slot %d", i)
		}
	}

	full := make([][]byte, 32)
	full[0] = ownerCell
	heldAll := map[string]bool{}
	for i := 1; i < len(full); i++ {
		var blob []byte
		full[i], blob = keyCell()
		heldAll[string(blob)] = true
	}
	c, _ := keyCell()
	got, n, err := Install(full, owner, heldAll, [][]byte{c})
	if err == nil || n != 0 {
		t.Fatalf("a vessel with no free cell took a cell: %d, %v", n, err)
	}
	for i := range full {
		if !bytes.Equal(got[i], full[i]) {
			t.Fatalf("a refused install changed slot %d", i)
		}
	}
	if _, _, err := Install(slots, owner, held, [][]byte{a[1:]}); err == nil {
		t.Fatal("a short cell was placed")
	}
}
