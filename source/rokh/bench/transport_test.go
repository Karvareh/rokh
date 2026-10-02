package bench

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"rokh/bundle"
	"rokh/covenant"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// Transport: how long it takes to select what one peer may receive from a
// ledger of N events, and how fast a receiving door ingests pre-signed events
// through append (the courier's path), with every event judged and pinned.
func TestTransportBench(t *testing.T) {
	if os.Getenv("ROKH_BENCH") == "" {
		t.Skip("set ROKH_BENCH=1 (and N) to run the transport measurement")
	}
	n := 10000
	if v := os.Getenv("N"); v != "" {
		fmt.Sscanf(v, "%d", &n)
	}
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g, _ := event.SignFresh(event.Event{Author: pub, Address: "rokh", Verb: event.VerbGenesis}, priv)
	anchor := g.ID
	// A peer, and a covenant opening home/journal to them.
	peerPub, _, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := covenant.Share{Subject: peerPub, Scope: "home/journal"}.Encode()
	share, _ := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{g.ID}, Address: covenant.Address, Verb: covenant.VerbShare, Payload: sp}, priv)
	c := createV1(t, dir, "pass", priv, g.ID, g, share)
	l, _ := ledger.New(g.Raw)
	l.Add(share.Raw)
	// N root-signed events, built in memory (signing cost) and committed
	// through the daemon path.
	s := daemon.New(c, l, daemon.Options{AllowSign: true, Dir: dir, Root: own(priv)})
	raws := make([][]byte, 0, n)
	start := time.Now()
	for i := 0; i < n; i++ {
		r := s.Handle([]byte(fmt.Sprintf(`{"op":"write","address":"home/journal","verb":"note","message":"entry %d"}`, i)))
		if r["ok"] != true {
			t.Fatalf("write %d: %v", i, r)
		}
		id, _ := frame.ParseID(r["id"].(string))
		e, _ := l.Get(id)
		raws = append(raws, e.Raw)
	}
	t.Logf("wrote %d events: %v (%.1f/s)", n, time.Since(start), float64(n)/time.Since(start).Seconds())

	start = time.Now()
	sel, err := bundle.Disclosable(l, peerPub, g.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bundle.Disclosable over %d events: %v, selected %d, missing %d", l.Len(), time.Since(start), len(sel.IDs), len(sel.Missing))
	start = time.Now()
	sel2, _ := bundle.Disclosable(l, peerPub, g.ID, "", true)
	t.Logf("bundle.Disclosable with ancestry: %v, selected %d", time.Since(start), len(sel2.IDs))

	// A receiving door: a fresh carrier with the same genesis, no root key
	// (cold), ingesting pre-signed events through append.
	dir2 := t.TempDir()
	c2 := createV1(t, dir2, "pass", nil, g.ID, g)
	l2, _ := ledger.New(g.Raw)
	s2 := daemon.New(c2, l2, daemon.Options{Dir: dir2})
	// The covenant is the parent of everything that follows; it travels first.
	if r := s2.Handle([]byte(fmt.Sprintf(`{"op":"append","raw":"%s"}`, hex.EncodeToString(share.Raw)))); r["record"] != "recorded" {
		t.Fatalf("share: %v", r)
	}
	m := 2000
	if m > len(raws) {
		m = len(raws)
	}
	start = time.Now()
	for i := 0; i < m; i++ {
		r := s2.Handle([]byte(fmt.Sprintf(`{"op":"append","raw":"%s"}`, hex.EncodeToString(raws[i]))))
		if r["record"] != "recorded" {
			t.Fatalf("append %d: %v", i, r)
		}
	}
	t.Logf("append (courier ingest) %d events: %v (%.1f/s)", m, time.Since(start), float64(m)/time.Since(start).Seconds())
	// Reverse order: everything pending until its parent arrives; the door
	// must not pay a full reload per pending offer.
	dir3 := t.TempDir()
	c3 := createV1(t, dir3, "pass", nil, g.ID, g)
	l3, _ := ledger.New(g.Raw)
	s3 := daemon.New(c3, l3, daemon.Options{Dir: dir3})
	s3.Handle([]byte(fmt.Sprintf(`{"op":"append","raw":"%s"}`, hex.EncodeToString(share.Raw))))
	k := 500
	if k > len(raws) {
		k = len(raws)
	}
	start = time.Now()
	pending := 0
	for i := k - 1; i >= 0; i-- {
		r := s3.Handle([]byte(fmt.Sprintf(`{"op":"append","raw":"%s"}`, hex.EncodeToString(raws[i]))))
		if r["code"] == "ancestry_pending" {
			pending++
		}
	}
	t.Logf("reverse-order offers of %d events: %v, %d answered ancestry_pending, ledger holds %d", k, time.Since(start), pending, l3.Len())
}
