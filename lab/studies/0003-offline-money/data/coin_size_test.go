// Measures the bytes of one coin of private money on the RKH3 event: its
// issue, and its transfers from holder to holder. Copy into
// source/rokh/oracle and run:
//
//	go test -count=1 -run TestCoinSize -v ./oracle
//
// It changes nothing in the tree and asserts nothing; it reports.
package oracle

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

func TestCoinSize(t *testing.T) {
	key := func() (ed25519.PrivateKey, ed25519.PublicKey) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return priv, pub
	}
	issuer, _ := key()

	// The issuer's ledger: a genesis, and one open grant over the coins, so
	// that any holder's key may write a transfer there (contract 4.5).
	gen, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, issuer, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	anchor := gen.ID
	og, err := event.Grant{Scope: "c", Verbs: []string{"t"}, Open: true}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	open, err := event.SignFrom(event.Event{Carrier: &anchor, Parents: []frame.ID{gen.ID},
		Address: event.AddressRoot, Verb: event.VerbGrant, Payload: og}, issuer, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	openID := open.ID

	// The issue: one coin, a 16-byte serial, its denomination, and the key
	// that holds it first, written by the issuer.
	_, first := key()
	issuePayload := append([]byte{0x01, 0x00, 0x00, 0x00, 0x64}, first...) // kind 1, value 100, holder
	issue, err := event.SignFrom(event.Event{Carrier: &anchor, Parents: []frame.ID{open.ID},
		Address: "c/0123456789abcdef", Verb: "i", Payload: issuePayload}, issuer, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%-28s %4d bytes (head %d, body %d)", "issue", len(issue.Raw), len(issue.Head), len(issue.Body))
	t.Logf("%-28s %4d bytes", "issuer genesis", len(gen.Raw))
	t.Logf("%-28s %4d bytes", "open grant over the coins", len(open.Raw))

	// Transfers: each holder signs the coin over to the next key, under the
	// open grant, after the previous step of the same coin.
	raws := [][]byte{open.Raw, issue.Raw}
	prev := issue.ID
	total := len(issue.Raw)
	cur, _ := key()
	for hop := 1; hop <= 3; hop++ {
		_, next := key()
		tr, err := event.SignFrom(event.Event{Carrier: &anchor, Authority: &openID, Parents: []frame.ID{prev},
			Address: "c/0123456789abcdef", Verb: "t", Payload: next}, cur, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		total += len(tr.Raw)
		raws = append(raws, tr.Raw)
		if hop == 1 {
			ho := tr.WithoutBody()
			t.Logf("%-28s %4d bytes (head %d, body %d)", "transfer", len(tr.Raw), len(tr.Head), len(tr.Body))
			t.Logf("%-28s %4d bytes", "transfer, head only", len(ho.Raw))
		}
		t.Logf("issue and %d transfer(s)      %4d bytes", hop, total)
		prev = tr.ID
		cur, _ = key()
	}

	// What the core says of them: the issuer's ledger takes them in.
	l, err := ledger.New(gen.Raw)
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range raws {
		st, err := l.Add(raw)
		t.Logf("event %d of the coin's ledger: %s %v", i+1, st, err)
	}

	// The holder of the last step signs the coin over twice, from the same
	// step: both are accepted, side by side, and the ledger has two heads.
	_, a := key()
	_, b := key()
	for _, to := range []ed25519.PublicKey{a, b} {
		tr, err := event.SignFrom(event.Event{Carrier: &anchor, Authority: &openID, Parents: []frame.ID{prev},
			Address: "c/0123456789abcdef", Verb: "t", Payload: to}, cur, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		st, err := l.Add(tr.Raw)
		t.Logf("a second hand-over of the same step: %s %v", st, err)
	}
	t.Logf("heads after the double hand-over: %d", len(l.Heads()))

	// A key that never held the coin writes a hand-over too: the core takes
	// it, for the open grant covers it; who held the coin is not its question.
	stranger, _ := key()
	_, c := key()
	tr, err := event.SignFrom(event.Event{Carrier: &anchor, Authority: &openID, Parents: []frame.ID{issue.ID},
		Address: "c/0123456789abcdef", Verb: "t", Payload: c}, stranger, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	st, err := l.Add(tr.Raw)
	t.Logf("a hand-over by a key that never held the coin: %s %v", st, err)
}
