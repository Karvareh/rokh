package lineage

import (
	"bytes"
	"testing"

	"rokh/carrier"
	"rokh/frame"
	"rokh/vessel"
)

func sysEvent(t *testing.T, s Side, verb, payload string, parents []frame.ID) Event {
	t.Helper()
	bd := body("rokh", verb, payload)
	h := head(1, parents, bd, true)
	return Event{ID: frame.Hash(h), Head: h, Body: bd, Address: "rokh"}
}

// seedPayload is the contract's rokh.seed payload (4.7): op, seed, and for a
// take the give and the new vessel.
func seedPayload(op byte, seed frame.ID, give, v *frame.ID) string {
	fs := frame.Fields{{Tag: 1, Value: []byte{op}}, {Tag: 2, Value: seed[:]}}
	if give != nil {
		fs = append(fs, frame.Field{Tag: 6, Value: give[:]})
	}
	if v != nil {
		fs = append(fs, frame.Field{Tag: 7, Value: v[:]})
	}
	b, _ := frame.EncodeFields(fs)
	return string(b)
}

func plan(t *testing.T, src Side, seedID frame.ID, scopes []string) SeedPlan {
	heads, _ := src.C.Heads()
	give := sysEvent(t, src, "rokh.seed", seedPayload(0x01, seedID, nil, nil), heads)
	return SeedPlan{
		Seed: seedID, Scopes: scopes,
		Give: []Event{give},
		Take: func(v frame.ID) (Event, error) {
			bd := body("rokh", "rokh.seed", seedPayload(0x02, seedID, &give.ID, &v))
			h := head(3, []frame.ID{give.ID}, bd, true)
			return Event{ID: frame.Hash(h), Head: h, Body: bd, Address: "rokh"}, nil
		},
	}
}

func target(seed byte) (SeedTarget, *vessel.Memory) {
	m := vessel.NewMemory()
	return SeedTarget{
		M: m, VK: bytes.Repeat([]byte{seed}, 32),
		Params: vessel.Params{SlabLog2: 18, Slabs: 16, Rand: stream(seed)},
		Sealer: kSealer{k: sharedK, rnd: stream(seed + 1)},
		Owner:  func() (vessel.Owner, error) { return &holder{}, nil },
	}, m
}

// S2, S3, S4, S5: a whole seed and a sliced seed.
func TestSeedWholeAndSliced(t *testing.T) {
	src, _ := newSide(t, 1, nil, true)
	work := write(t, src, 1, "work/plan", "in scope")
	home := write(t, src, 1, "home/diary", "out of scope")
	for _, tc := range []struct {
		name   string
		scopes []string
	}{{"whole", nil}, {"slice", []string{"work"}}} {
		dst, _ := target(40)
		seedID := frame.Hash([]byte("seed " + tc.name))
		p := plan(t, src, seedID, tc.scopes)
		res, err := Seed(src, dst, p)
		if err != nil || !res.OK() || res.Repeated {
			t.Fatalf("%s: %v %+v", tc.name, err, res)
		}
		d, _, err := carrier.Open(dst.M, func([][]byte, []byte, int) ([]byte, error) { return dst.VK, nil }, stream(50), dst.Sealer)
		if err != nil {
			t.Fatal(err)
		}
		di, si := d.Vessel().Info(), src.C.Vessel().Info()
		if di.Anchor != si.Anchor || di.Seed != seedID || !bytes.Equal(di.Salt, si.Salt) || di.Vessel == si.Vessel {
			t.Fatalf("%s: the seed's identity is wrong: %+v", tc.name, di)
		}
		if bytes.Equal(d.Vessel().VK(), src.C.Vessel().VK()) {
			t.Fatalf("%s: the seed shares the source's VK", tc.name)
		}
		got := map[frame.ID]string{}
		d.Events(func(e carrier.EventRecord) error {
			got[e.ID] = map[bool]string{true: "head", false: "whole"}[carrier.IsHeadOnly(e.Ref)]
			return nil
		}, nil)
		wantHome := "whole"
		if tc.scopes != nil {
			wantHome = "head"
		}
		if got[anchor] != "whole" || got[p.Give[0].ID] != "whole" || got[work] != "whole" || got[home] != wantHome {
			t.Fatalf("%s: the seed holds %v", tc.name, got)
		}
		takes := 0
		d.Events(func(e carrier.EventRecord) error {
			if tookOne(d, e) {
				takes++
			}
			return nil
		}, nil)
		if takes != 1 {
			t.Fatalf("%s: %d take events on the seed", tc.name, takes)
		}
		// Repeating the seed for this target changes nothing.
		gs, gd := src.C.Vessel().Info().Generation, d.Vessel().Info().Generation
		res, err = Seed(src, dst, p)
		if err != nil || !res.Repeated || !res.OK() {
			t.Fatalf("%s: repeated seed: %v %+v", tc.name, err, res)
		}
		d2, _, _ := carrier.Open(dst.M, func([][]byte, []byte, int) ([]byte, error) { return dst.VK, nil }, stream(51), dst.Sealer)
		if src.C.Vessel().Info().Generation != gs || d2.Vessel().Info().Generation != gd {
			t.Fatalf("%s: a repeated seed wrote", tc.name)
		}
	}
}

// A seed's scopes must lie inside the giver's own.
func TestASeedWiderThanItsGiverIsRefused(t *testing.T) {
	src, _ := newSide(t, 1, []string{"work"}, true)
	dst, m := target(41)
	_, err := Seed(src, dst, plan(t, src, frame.Hash([]byte("s")), []string{"home"}))
	if Code(err) != "seed_refused" {
		t.Fatalf("a wider seed: %v", err)
	}
	if len(m.FileNames()) != 0 {
		t.Fatal("a refused seed created files")
	}
}

// tookOne reports whether one event is a take.
func tookOne(c *carrier.Carrier, e carrier.EventRecord) bool {
	b, err := c.Body(e)
	if err != nil {
		return false
	}
	fs, _ := frame.DecodeFields(b)
	p, _ := fs.Get(7)
	pf, _ := frame.DecodeFields(p)
	op, _ := pf.Get(1)
	return len(op) == 1 && op[0] == 0x02
}
