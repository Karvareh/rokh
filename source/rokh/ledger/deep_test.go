package ledger_test

import (
	"crypto/ed25519"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"testing"

	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// A history is as deep as it is long, and reading it must not cost a call per
// generation of ancestors. Load walked parents by recursion, about 1.9 KiB of
// stack for each event on the branch it climbed: a single branch of some
// 550,000 events outgrew the goroutine's default stack of 1 GiB, and the
// process ended with a fatal stack overflow instead of an answer, so a
// ledger past that length could not be opened at all.
//
// The same walk is made here under a stack small enough that the old walk
// overflowed it at a few hundred events: a chain of 3,000 events loads whole,
// every event accepted and the tip a head.
func TestALongChainLoadsWithoutGrowingTheStack(t *testing.T) {
	const depth = 3000
	root := ed25519.NewKeyFromSeed(make([]byte, 32))
	rnd := rand.NewChaCha8([32]byte{7})
	g, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, rnd)
	if err != nil {
		t.Fatal(err)
	}
	stored := map[frame.ID][]byte{g.ID: g.Raw}
	tip := g.ID
	for i := 0; i < depth; i++ {
		e, err := event.SignFrom(event.Event{Carrier: &g.ID, Parents: []frame.ID{tip},
			Address: "home/journal", Verb: "note", Payload: []byte(fmt.Sprint(i))}, root, rnd)
		if err != nil {
			t.Fatal(err)
		}
		stored[e.ID] = e.Raw
		tip = e.ID
	}
	get := func(id frame.ID) ([]byte, error) {
		b, ok := stored[id]
		if !ok {
			return nil, fmt.Errorf("not held: %s", id.Short())
		}
		return b, nil
	}

	defer debug.SetMaxStack(debug.SetMaxStack(512 << 10))
	l, err := ledger.Load(g.Raw, get, []frame.ID{tip})
	if err != nil {
		t.Fatal(err)
	}
	if acc, rej, pend := l.Tally(); acc != depth+1 || rej != 0 || pend != 0 {
		t.Fatalf("accepted %d, rejected %d, pending %d; want %d accepted", acc, rej, pend, depth+1)
	}
	if heads := l.Heads(); len(heads) != 1 || heads[0] != tip {
		t.Fatalf("heads %v, want the tip %s", heads, tip.Short())
	}
}
