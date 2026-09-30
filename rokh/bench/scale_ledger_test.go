package bench

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	mrand "math/rand/v2"
	"testing"
	"time"

	"rokh/bond"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/vessel"
)

// stamp is the testimony a door leaves on an ordinary note: a clock and a
// chance, as oracle.Default gives them, at their usual sizes.
func stamp(i int) []event.Attestation {
	clock := make([]byte, 8)
	binary.BigEndian.PutUint64(clock, uint64(1_700_000_000+i))
	chance := make([]byte, 16)
	binary.BigEndian.PutUint64(chance, uint64(i))
	return []event.Attestation{{Oracle: "chance", Claim: chance}, {Oracle: "clock", Claim: clock}}
}

// chain signs a history of n notes on one branch, each naming the one before.
func chain(tb testing.TB, n int) (g event.Signed, stored map[frame.ID][]byte, order []frame.ID) {
	root := ed25519.NewKeyFromSeed(make([]byte, 32))
	rnd := mrand.NewChaCha8([32]byte{11})
	g, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, rnd)
	if err != nil {
		tb.Fatal(err)
	}
	stored = make(map[frame.ID][]byte, n+1)
	stored[g.ID] = g.Raw
	order = make([]frame.ID, 0, n+1)
	order = append(order, g.ID)
	tip := g.ID
	for i := 0; i < n; i++ {
		e, err := event.SignFrom(event.Event{Carrier: &g.ID, Parents: []frame.ID{tip},
			Address: "home/journal", Verb: "note", Payload: []byte(fmt.Sprintf("a short note, number %d", i)),
			Attest: stamp(i)}, root, rnd)
		if err != nil {
			tb.Fatal(err)
		}
		stored[e.ID] = e.Raw
		order = append(order, e.ID)
		tip = e.ID
	}
	return g, stored, order
}

// BenchmarkScaleChain measures one history as it grows longer on one branch:
// what judging one more event costs, what an event holds in memory, how long
// reading the whole history back takes (the walk a door makes when it opens),
// and how long it takes from the newest event to reach the first one — by its
// parents, and as the ledger's causal past. Run it with -benchtime 1x.
func BenchmarkScaleChain(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("events=%d", n), func(b *testing.B) {
			start := time.Now()
			g, stored, order := chain(b, n)
			signed := time.Since(start)
			var raw int64
			for _, r := range stored {
				raw += int64(len(r))
			}
			// Judging one event at a time, as a door does when it records.
			heap0 := liveHeap()
			l, err := ledger.New(g.Raw)
			if err != nil {
				b.Fatal(err)
			}
			start = time.Now()
			for _, id := range order[1:] {
				if st, err := l.Add(stored[id]); st != ledger.Accepted {
					b.Fatalf("%s: %v %v", id.Short(), st, err)
				}
			}
			added := time.Since(start)
			held := int64(liveHeap()) - int64(heap0)
			tip := order[len(order)-1]
			// From the newest event back to the first, by the parents each names.
			start = time.Now()
			steps, at := 0, tip
			for at != g.ID {
				e, ok := l.Get(at)
				if !ok {
					b.Fatalf("%s is not held", at.Short())
				}
				at = e.Event.Parents[0]
				steps++
			}
			walked := time.Since(start)
			start = time.Now()
			past := l.CausalPast(tip)
			pastTook := time.Since(start)
			start = time.Now()
			l.Order()
			ordered := time.Since(start)
			l = nil
			// Reading the whole history back from its bytes, as opening does.
			get := func(id frame.ID) ([]byte, error) {
				if r, ok := stored[id]; ok {
					return r, nil
				}
				return nil, fmt.Errorf("not held: %s", id.Short())
			}
			start = time.Now()
			l2, err := ledger.Load(g.Raw, get, []frame.ID{tip})
			if err != nil {
				b.Fatal(err)
			}
			loaded := time.Since(start)
			if acc, _, _ := l2.Tally(); acc != n+1 || len(past) != n+1 || steps != n {
				b.Fatalf("accepted %d, past %d, steps %d", acc, len(past), steps)
			}
			per := float64(n)
			b.ReportMetric(signed.Seconds()*1e6/per, "sign-us/event")
			b.ReportMetric(added.Seconds()*1e6/per, "judge-us/event")
			b.ReportMetric(loaded.Seconds()*1e6/per, "load-us/event")
			b.ReportMetric(loaded.Seconds(), "load-s")
			b.ReportMetric(float64(held)/per, "heap-B/event")
			b.ReportMetric(float64(raw)/per, "bytes/event")
			b.ReportMetric(walked.Seconds()*1000, "walk-to-genesis-ms")
			b.ReportMetric(pastTook.Seconds()*1000, "causal-past-ms")
			b.ReportMetric(ordered.Seconds()*1000, "order-ms")
			b.ReportMetric(float64(peakRSS())/(1<<30), "peak-rss-GiB")
		})
	}
}

// BenchmarkScaleAuthority measures what a ledger holds and costs as the
// people it names grow: grants to that many keys, then taking every one of
// them back, and keyring generations for that many keys. Each authority
// event is judged in the causal past, and the sets of grants, revocations
// and keyring changes that each event carries are what grows. Run it with
// -benchtime 1x.
func BenchmarkScaleAuthority(b *testing.B) {
	for _, kind := range []string{"grants", "revokes", "keyring"} {
		for _, n := range []int{250, 500, 1000, 2000, 4000, 8000} {
			b.Run(fmt.Sprintf("%s=%d", kind, n), func(b *testing.B) {
				root := ed25519.NewKeyFromSeed(make([]byte, 32))
				rnd := mrand.NewChaCha8([32]byte{13})
				g, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, rnd)
				if err != nil {
					b.Fatal(err)
				}
				heap0 := liveHeap()
				l, err := ledger.New(g.Raw)
				if err != nil {
					b.Fatal(err)
				}
				tip := g.ID
				sign := func(verb string, payload []byte) event.Signed {
					e, err := event.SignFrom(event.Event{Carrier: &g.ID, Parents: []frame.ID{tip},
						Address: event.AddressRoot, Verb: verb, Payload: payload}, root, rnd)
					if err != nil {
						b.Fatal(err)
					}
					return e
				}
				add := func(e event.Signed) {
					if st, err := l.Add(e.Raw); st != ledger.Accepted {
						b.Fatalf("%s: %v %v", e.Event.Verb, st, err)
					}
					tip = e.ID
				}
				var judged time.Duration
				var grants []frame.ID
				subject := func(i int) ed25519.PublicKey {
					var seed [32]byte
					binary.BigEndian.PutUint64(seed[:], uint64(i+1))
					return ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
				}
				switch kind {
				case "grants", "revokes":
					for i := 0; i < n; i++ {
						p, _ := event.Grant{Subject: subject(i), Scope: fmt.Sprintf("people/%d", i)}.Encode()
						e := sign(event.VerbGrant, p)
						start := time.Now()
						add(e)
						judged += time.Since(start)
						grants = append(grants, e.ID)
					}
					if kind == "revokes" {
						judged = 0
						heap0 = liveHeap()
						// Taken back newest first: a set of revocations
						// that equalled a set of grants (the first k of
						// each) would be kept once by the ledger's pool,
						// and the measurement would show sharing that
						// revocations in no particular order do not get.
						for i := len(grants) - 1; i >= 0; i-- {
							id := grants[i]
							p, _ := event.Revoke{Target: id}.Encode()
							e := sign(event.VerbRevoke, p)
							start := time.Now()
							add(e)
							judged += time.Since(start)
						}
					}
				case "keyring":
					for i := 0; i < n; i++ {
						var kid [32]byte
						binary.BigEndian.PutUint64(kid[:], uint64(i+1))
						reader := make([]byte, 32)
						rnd.Read(reader)
						system := make([]byte, event.SealedKeySize)
						rnd.Read(system)
						p, err := event.Keyring{Op: event.KeyringAdd, Key: kid, Gen: 1, Name: fmt.Sprintf("k%d", i),
							Reader: reader, Reads: []string{fmt.Sprintf("people/%d", i)}, System: system}.Encode()
						if err != nil {
							b.Fatal(err)
						}
						e := sign(event.VerbKeyring, p)
						start := time.Now()
						add(e)
						judged += time.Since(start)
					}
				}
				held := int64(liveHeap()) - int64(heap0)
				// One more event after them all, by the last person named.
				start := time.Now()
				adds, _ := l.Keyring()
				live := l.ActiveGrants()
				folded := time.Since(start)
				b.ReportMetric(judged.Seconds()*1e6/float64(n), "judge-us/event")
				b.ReportMetric(mib(held), "heap-MiB")
				b.ReportMetric(float64(held)/float64(n), "heap-B/person")
				b.ReportMetric(folded.Seconds()*1000, "fold-ms")
				b.ReportMetric(float64(len(adds)+len(live)), "live")
			})
		}
	}
}

// BenchmarkScaleSeedLedger follows seeds further than the command line can
// make them in a measurement (BenchmarkScaleSeeds, in cmd/rokh, makes each
// seed as a person does, in about a second). A seed adds four events to a
// lineage, as newGiving and the take make them in cmd/rokh/v1_seedplan.go: the
// keyring add of the seed's key, a grant to its signer, the give, and the take
// the seed's key signs under that grant. Here one ledger judges them. In a
// line each seed is given by the one before it, so the last seed holds every
// seed event before it; in a fan one root gives every seed, and the root that
// has reconciled them all holds them all. Every keyring event and every seed
// event joins the system set that each later event carries, and a take reads
// that set to find its signer. Run it with -benchtime 1x.
func BenchmarkScaleSeedLedger(b *testing.B) {
	for _, shape := range []string{"line", "fan"} {
		for _, n := range []int{250, 500, 1000, 2000} {
			b.Run(fmt.Sprintf("%s/seeds=%d", shape, n), func(b *testing.B) {
				root := ed25519.NewKeyFromSeed(make([]byte, 32))
				rnd := mrand.NewChaCha8([32]byte{23})
				g, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, rnd)
				if err != nil {
					b.Fatal(err)
				}
				heap0 := liveHeap()
				l, err := ledger.New(g.Raw)
				if err != nil {
					b.Fatal(err)
				}
				stored := map[frame.ID][]byte{g.ID: g.Raw}
				sign := func(by ed25519.PrivateKey, authority *frame.ID, parent frame.ID, verb string, payload []byte) event.Signed {
					e, err := event.SignFrom(event.Event{Carrier: &g.ID, Authority: authority, Parents: []frame.ID{parent},
						Address: event.AddressRoot, Verb: verb, Payload: payload}, by, rnd)
					if err != nil {
						b.Fatal(err)
					}
					return e
				}
				var judged, lastTake time.Duration
				add := func(e event.Signed) time.Duration {
					start := time.Now()
					if st, err := l.Add(e.Raw); st != ledger.Accepted {
						b.Fatalf("%s: %v %v", e.Event.Verb, st, err)
					}
					took := time.Since(start)
					judged += took
					stored[e.ID] = e.Raw
					return took
				}
				tip := g.ID
				var takes []frame.ID
				for i := 0; i < n; i++ {
					var seed, kid, vessel [32]byte
					binary.BigEndian.PutUint64(seed[:], uint64(i+1))
					kid, vessel = seed, seed
					seed[31], kid[31], vessel[31] = 1, 2, 3
					signer := ed25519.NewKeyFromSeed(seed[:])
					reader := make([]byte, 32)
					rnd.Read(reader)
					system := make([]byte, event.SealedKeySize)
					rnd.Read(system)
					kp, err := event.Keyring{Op: event.KeyringAdd, Key: kid, Gen: 1, Name: fmt.Sprintf("seed %d", i),
						Reader: reader, Signer: signer.Public().(ed25519.PublicKey), System: system}.Encode()
					if err != nil {
						b.Fatal(err)
					}
					ka := sign(root, nil, tip, event.VerbKeyring, kp)
					add(ka)
					gp, _ := event.Grant{Subject: signer.Public().(ed25519.PublicKey), Scope: ""}.Encode()
					gr := sign(root, nil, ka.ID, event.VerbGrant, gp)
					add(gr)
					sp, err := event.Seed{Op: event.SeedGive, Seed: seed, Key: kid}.Encode()
					if err != nil {
						b.Fatal(err)
					}
					gv := sign(root, nil, gr.ID, event.VerbSeed, sp)
					add(gv)
					tp, err := event.Seed{Op: event.SeedTake, Seed: seed, Key: kid, Give: gv.ID, Vessel: vessel}.Encode()
					if err != nil {
						b.Fatal(err)
					}
					tk := sign(signer, &gr.ID, gv.ID, event.VerbSeed, tp)
					lastTake = add(tk)
					takes = append(takes, tk.ID)
					if shape == "line" {
						tip = tk.ID // the next seed is given by this one
					} else {
						tip = gv.ID // the root gives on; the take stays in its seed
					}
				}
				held := int64(liveHeap()) - int64(heap0)
				last := takes[len(takes)-1]
				// From the last take back to the first event, by the parents.
				depth, at := 0, last
				for at != g.ID {
					e, ok := l.Get(at)
					if !ok {
						b.Fatalf("%s is not held", at.Short())
					}
					at = e.Event.Parents[0]
					depth++
				}
				acc, _, _ := l.Tally()
				heads := []frame.ID{last}
				if shape == "fan" {
					heads = takes // the root that reconciled every seed
				}
				l = nil
				get := func(id frame.ID) ([]byte, error) {
					if r, ok := stored[id]; ok {
						return r, nil
					}
					return nil, fmt.Errorf("not held: %s", id.Short())
				}
				start := time.Now()
				if _, err := ledger.Load(g.Raw, get, heads); err != nil {
					b.Fatal(err)
				}
				loaded := time.Since(start)
				b.ReportMetric(float64(acc), "events")
				b.ReportMetric(float64(depth), "depth")
				b.ReportMetric(mib(held), "heap-MiB")
				b.ReportMetric(float64(held)/float64(n), "heap-B/seed")
				b.ReportMetric(judged.Seconds()*1e6/float64(4*n), "judge-us/event")
				b.ReportMetric(lastTake.Seconds()*1e6, "last-take-us")
				b.ReportMetric(loaded.Seconds(), "load-s")
				b.ReportMetric(float64(peakRSS())/(1<<30), "peak-rss-GiB")
			})
		}
	}
}

// BenchmarkScaleEnvelope measures an envelope as the readers of one address
// grow: its size, sealing it, and opening it by the last reader it names. The
// format names at most key.MaxReaders; the thirty-third is refused.
func BenchmarkScaleEnvelope(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 33} {
		b.Run(fmt.Sprintf("readers=%d", n), func(b *testing.B) {
			var readers [][]byte
			var last key.Reader
			for i := 0; i < n; i++ {
				r, err := key.NewReader(rand.Reader)
				if err != nil {
					b.Fatal(err)
				}
				readers = append(readers, r.Public())
				last = r
			}
			var id frame.ID
			rand.Read(id[:])
			body := make([]byte, 300)
			var env []byte
			var err error
			b.ResetTimer()
			start := time.Now()
			for i := 0; i < b.N; i++ {
				env, err = key.SealReaders(key.TypeEvent, id, nil, readers, body, rand.Reader)
			}
			sealed := time.Since(start)
			b.StopTimer()
			if n > key.MaxReaders {
				if err == nil {
					b.Fatalf("an envelope named %d readers", n)
				}
				b.ReportMetric(0, "refused")
				return
			}
			if err != nil {
				b.Fatal(err)
			}
			start = time.Now()
			if _, err := key.OpenReaders(key.TypeEvent, id, nil, env, last); err != nil {
				b.Fatal(err)
			}
			opened := time.Since(start)
			b.ReportMetric(float64(len(env)), "envelope-B")
			b.ReportMetric(float64(len(env)-len(body)), "overhead-B")
			b.ReportMetric(sealed.Seconds()*1e6/float64(b.N), "seal-us")
			b.ReportMetric(opened.Seconds()*1e6, "open-us")
		})
	}
}

// BenchmarkScalePassphrase is the floor under every opening: the owner's
// passphrase at the default cost, tried against every slot cell of a vessel.
func BenchmarkScalePassphrase(b *testing.B) {
	salt := make([]byte, 32)
	rand.Read(salt)
	start := time.Now()
	if _, err := key.PassKey("a passphrase for a measurement", salt, vessel.DefaultIter); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(time.Since(start).Seconds()*1000, "pbkdf2-ms")
	cells := make([][]byte, vessel.SlotCells)
	for i := range cells {
		cells[i] = make([]byte, vessel.CellSize)
		rand.Read(cells[i])
	}
	kk, _ := key.PassKey("a passphrase for a measurement", salt, 1)
	start = time.Now()
	for _, c := range cells {
		key.OpenCell(kk, c)
	}
	b.ReportMetric(time.Since(start).Seconds()*1e6, "try-32-cells-us")
}

// BenchmarkScaleLeaf measures the foundation leaf of a bond as the founders it
// names grow, from a couple to a country: its bytes, making its name, and
// reading it back. A bond is how many people hold something together without
// sharing one ledger: each accepts the leaf's name in their own.
func BenchmarkScaleLeaf(b *testing.B) {
	for _, n := range []int{2, 100, 10_000, 1_000_000} {
		b.Run(fmt.Sprintf("founders=%d", n), func(b *testing.B) {
			fs := make([]frame.ID, n)
			for i := range fs {
				binary.BigEndian.PutUint64(fs[i][:], uint64(i+1))
				fs[i][31] = 1
			}
			leaf := bond.Leaf{Kind: bond.Founding, Founders: fs, Doing: "keep one thing together"}
			start := time.Now()
			raw, err := leaf.Encode()
			if err != nil {
				b.Fatal(err)
			}
			name := bond.Name(raw)
			made := time.Since(start)
			start = time.Now()
			if _, err := bond.Parse(raw); err != nil {
				b.Fatal(err)
			}
			read := time.Since(start)
			start = time.Now()
			missing, closed := bond.Standing(leaf, fs[:n-1])
			stood := time.Since(start)
			if closed || len(missing) != 1 || name.IsZero() {
				b.Fatalf("standing: %d missing, closed %v", len(missing), closed)
			}
			b.ReportMetric(float64(len(raw)), "leaf-B")
			b.ReportMetric(made.Seconds()*1000, "encode-ms")
			b.ReportMetric(read.Seconds()*1000, "parse-ms")
			b.ReportMetric(stood.Seconds()*1000, "standing-ms")
		})
	}
}

// BenchmarkScaleOpenAddress measures the one way a ledger takes many people
// without naming each: an open address (contract 4.5), where any key writes
// under one grant the root made. Each person is a key of their own that
// writes one note there; nobody is granted, added to the keyring or named in
// an envelope. Run it with -benchtime 1x.
func BenchmarkScaleOpenAddress(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("people=%d", n), func(b *testing.B) {
			root := ed25519.NewKeyFromSeed(make([]byte, 32))
			rnd := mrand.NewChaCha8([32]byte{19})
			g, err := event.SignFrom(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis}, root, rnd)
			if err != nil {
				b.Fatal(err)
			}
			p, _ := event.Grant{Open: true, Read: true, Scope: "commons"}.Encode()
			og, err := event.SignFrom(event.Event{Carrier: &g.ID, Parents: []frame.ID{g.ID}, Address: event.AddressRoot,
				Verb: event.VerbGrant, Payload: p}, root, rnd)
			if err != nil {
				b.Fatal(err)
			}
			heap0 := liveHeap()
			l, err := ledger.New(g.Raw)
			if err != nil {
				b.Fatal(err)
			}
			if st, err := l.Add(og.Raw); st != ledger.Accepted {
				b.Fatalf("open grant: %v %v", st, err)
			}
			tip := og.ID
			var judged, signed time.Duration
			for i := 0; i < n; i++ {
				var seed [32]byte
				binary.BigEndian.PutUint64(seed[:], uint64(i+1))
				person := ed25519.NewKeyFromSeed(seed[:])
				start := time.Now()
				e, err := event.SignFrom(event.Event{Carrier: &g.ID, Authority: &og.ID, Parents: []frame.ID{tip},
					Address: fmt.Sprintf("commons/%d", i), Verb: "note", Payload: []byte("here"), Attest: stamp(i)}, person, rnd)
				if err != nil {
					b.Fatal(err)
				}
				signed += time.Since(start)
				start = time.Now()
				if st, err := l.Add(e.Raw); st != ledger.Accepted {
					b.Fatalf("person %d: %v %v", i, st, err)
				}
				judged += time.Since(start)
				tip = e.ID
			}
			held := int64(liveHeap()) - int64(heap0)
			b.ReportMetric(judged.Seconds()*1e6/float64(n), "judge-us/event")
			b.ReportMetric(signed.Seconds()*1e6/float64(n), "sign-us/event")
			b.ReportMetric(float64(held)/float64(n), "heap-B/person")
			b.ReportMetric(float64(len(l.ActiveGrants())), "grants")
			b.ReportMetric(float64(peakRSS())/(1<<30), "peak-rss-GiB")
		})
	}
}
