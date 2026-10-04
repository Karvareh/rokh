// Measures a note: one event, signed by its issuer, that carries one or more
// tokens and names no holder, as a container would hold it. Copy into
// source/rokh/oracle and run:
//
//	go test -count=1 -run TestNoteSize -v ./oracle
//
// It changes nothing in the tree and asserts nothing; it reports.
package oracle

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"testing"

	"rokh/event"
	"rokh/frame"
)

func TestNoteSize(t *testing.T) {
	_, issuer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, issuer, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	anchor := gen.ID
	// A token is a 16-byte serial and a 4-byte value; the note's payload is
	// a kind byte, the id of the issuer's standing offer it answers to, and
	// its tokens.
	for _, k := range []int{1, 10, 25, 100, 200} {
		payload := []byte{0x02}
		offer := frame.Hash([]byte("the issuer's standing offer"))
		payload = append(payload, offer[:]...)
		for i := 0; i < k; i++ {
			var tok [20]byte
			if _, err := rand.Read(tok[:16]); err != nil {
				t.Fatal(err)
			}
			binary.BigEndian.PutUint32(tok[16:], 100)
			payload = append(payload, tok[:]...)
		}
		note, err := event.SignFrom(event.Event{Carrier: &anchor, Parents: []frame.ID{gen.ID},
			Address: "n/0123456789abcdef", Verb: "n", Payload: payload}, issuer, rand.Reader)
		if err != nil {
			t.Logf("a note of %3d tokens: refused: %v", k, err)
			continue
		}
		t.Logf("a note of %3d tokens       %5d bytes", k, len(note.Raw))
	}
}
