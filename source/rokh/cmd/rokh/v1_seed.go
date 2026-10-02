package main

// `rokh seed SRC DST [--scope a,b] [--size SIZE] [--slab SIZE] [--attempt NAME]`
// (contract 4.7, S1..S5; goal 7.41): one commit on SRC with the give, a new
// vessel at DST with its own VK and the inherited salt, the copy of what is
// in scope with its necessary lineage, and the take on DST.
//
// What DST already holds decides which seed this is. A folder that holds a
// seed of this rokh names its own seed, and the give SRC recorded for it is
// used again with its keys: when DST took it, nothing is recorded and the
// command says so (S5); when it did not, the copy and the take go on from
// where they stopped. --attempt names the act itself: the same name again
// finds the same give even where DST holds nothing yet.
//
// The folder is made, and names its seed, before the give is recorded on
// SRC (goal 7.41). A command cut after the give therefore leaves a give that
// the folder names, and the same command again uses it; a command cut before
// the give leaves a folder that names a seed planned on SRC and holds no
// event, and the same command again makes that seed's give then. Repeating
// the command, with or without --attempt, records no second give.

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/lineage"
	"rokh/medium"
	"rokh/size"
	"rokh/turn"
	"rokh/vessel"
)

func init() { v1Commands["seed"] = cmdSeed }

// v1SeedPlan is the key layer's part of a seed: the signed give events (a
// keyring add for the seed's key, one grant per scope, the give), the take
// signed once the new vessel's id is known, and the new vessel's key, cells
// and sealer. It is set by the key's command files (v1_seedplan.go).
var v1SeedPlan func(src *carrier.Carrier, pass string, ask SeedAsk) (lineage.SeedPlan, SeedKeys, error)

// SeedAsk is what the command asks of the key layer.
type SeedAsk struct {
	// Scopes are the seed's address prefixes, sorted, each once, none inside
	// another; nil is the whole rokh.
	Scopes []string
	// Attempt is the person's own name for this act: the same name again
	// finds the same give (contract section 6).
	Attempt string
	// Held is the seed of this rokh that DST already holds, or nil.
	Held *SeedHeld
}

// SeedHeld is a seed of this rokh that DST already holds: its seed id and
// scopes, the key and cells it opened with, and whether it holds no event at
// all yet (its folder was made and its command cut before the copy, or
// before the give).
type SeedHeld struct {
	Seed   frame.ID
	Scopes []string
	VK     []byte
	Slots  [][]byte
	Empty  bool
	// Vessel is the folder's own vessel id.
	Vessel frame.ID
	// Heads are the folder's head files as they are, byte for byte: each
	// keeps the cells of the generation it holds.
	Heads [][]byte
}

// SeedKeys is the new vessel's own key material, made by the key layer.
// Recell asks that the cells of a folder DST already holds be written again,
// as Slots, in every head file, before anything is recorded: the keys it
// takes are judged again when the command goes on with it.
type SeedKeys struct {
	VK     []byte
	Slots  [][]byte
	Sealer carrier.Sealer
	Recell bool
	// Notes are what the command says beside its answer: lines the owner
	// reads before anything is recorded.
	Notes []string
	// Vessel is the id a new folder's vessel is made with; zero draws one at
	// random.
	Vessel frame.ID
	// Refused, when set, refuses to go on with a folder DST holds: unless the
	// folder already took its seed (then nothing is recorded, S5), nothing is
	// recorded and the command says why.
	Refused error
	// Earlier is, for a new seed into a folder that holds nothing, the latest
	// earlier give from this source of the same scopes whose seed's key is
	// live and whose take this source does not hold; zero when there is none.
	// A folder lost after its give leaves such a give (F12).
	Earlier EarlierGive
}

// EarlierGive names a give by its seed and its event.
type EarlierGive struct {
	Seed, Give frame.ID
}

func cmdSeed(args []string) error {
	fs, src, err := newFlags("seed", args)
	if err != nil {
		return err
	}
	scope := fs.String("scope", "", "comma-separated address prefixes; empty means the whole rokh")
	sz := fs.String("size", "64M", "the new vessel's size")
	slab := fs.String("slab", "1M", "slab size, a power of two from 256K to 64M")
	attempt := fs.String("attempt", "", "your own name for this seed; the same name again finds the same give and records nothing new")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("give the new folder: rokh seed SRC DST")
	}
	dst := fs.Arg(0)
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if v1SeedPlan == nil {
		return errNoKeyLayer
	}
	// Everything the person typed is read before anything is recorded: a
	// give is never left behind by a mistyped size or scope.
	scopes, err := seedScopes(splitList(*scope))
	if err != nil {
		return err
	}
	k, err := slabLog2(*slab)
	if err != nil {
		return err
	}
	total, err := size.Parse(*sz)
	if err != nil {
		return err
	}
	slabs := int((total + (int64(1)<<k - 1)) >> k)
	if slabs < vessel.MinSlabs || slabs > vessel.MaxSlabs {
		return fmt.Errorf("--size %s in slabs of %s is %d slabs; a vessel holds %d to %d", *sz, *slab, slabs, vessel.MinSlabs, vessel.MaxSlabs)
	}
	if err := apartFrom(src, dst); err != nil {
		return err
	}
	c, lock, _, err := openCarrier(src, pass, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	held, err := seedHeldAt(dst, pass, c)
	if err != nil {
		return exitStatus{code: 3, err: err}
	}
	plan, keys, err := v1SeedPlan(c, pass, SeedAsk{Scopes: scopes, Attempt: *attempt, Held: held})
	if err != nil {
		return exitStatus{code: 3, err: fmt.Errorf("nothing was recorded: %w", err)}
	}
	id := plan.Seed.Short()
	for _, n := range keys.Notes {
		fmt.Printf("seed %s: note: %s\n", id, n)
	}
	if e := keys.Earlier; !e.Seed.IsZero() {
		// A folder lost after its give is a new act (the source cannot tell
		// which folder a give was for): the earlier give stays, and its key
		// is the owner's to take back (F12).
		fmt.Printf("seed %s: note: seed %s, given from %s before (give %s), has no take on it; if its folder is lost for good, rokh key revoke %s --key %s takes that seed's key back\n",
			id, e.Seed.Short(), src, e.Give.Short(), src, seedKeyName(e.Seed))
	}
	// The turn on DST, once taken, is held to the end: the copy and the take
	// are recorded under it.
	var dstLock *turn.Lock
	defer func() {
		if dstLock != nil {
			dstLock.Release()
		}
	}()
	if held != nil {
		// A seed DST holds and did not take goes on from where it stopped.
		// --size names a target, never a delta: a larger one grows it first;
		// and a folder whose give is made now takes the cells of that give.
		grown, l, err := goOnHeld(dst, keys, plan.Seed, total)
		dstLock = l
		if err != nil {
			if errors.Is(err, lineage.ErrSeedRefused) {
				return exitStatus{code: 3, err: fmt.Errorf("nothing was recorded: %w", err)}
			}
			return exitStatus{code: 3, err: fmt.Errorf("%s was not readied; nothing was recorded: %w", dst, err)}
		}
		if grown > 0 {
			fmt.Printf("seed %s: %s grown to %d slabs\n", id, dst, grown)
		}
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return fmt.Errorf("nothing was recorded: %w", err)
	}
	if held == nil {
		// The folder first: it names the seed before the give is recorded
		// (goal 7.41), so that a cut after the give leaves a give this folder
		// names. It holds no event yet; a cut here leaves a folder the same
		// command again gives to (a target without a take is
		// unfinished).
		if err := makeSeedFolder(dst, c, plan, keys, k, slabs); err != nil {
			return exitStatus{code: 3, err: fmt.Errorf("%s was not made; nothing was recorded: %w", dst, err)}
		}
	}
	// Then the give, on SRC, in one commit (S2). A give SRC holds already is
	// not recorded again.
	give := recordOn(c, lock, plan.Give, plan.Branch)
	if give.Outcome != vessel.Recorded {
		fmt.Printf("seed %s: give %s; %s names this seed and holds no event: the same command again goes on from here\n", id, give.Outcome, dst)
		return exitStatus{code: seedExit(give), err: fmt.Errorf("the give was not recorded on %s: %v", src, give.Err)}
	}
	// The judge reads SRC as it is now, its give included: what it cannot
	// judge does not travel (R4), and a judge read before the give would
	// hold the give back from the seed that names it.
	srcSide, err := judgedSide(c, lock, pass)
	if err != nil {
		fmt.Printf("seed %s: give %s; the copy did not start\n", id, give.Outcome)
		return exitStatus{code: 3, err: fmt.Errorf("the give is recorded on %s and nothing was copied to %s: %w", src, dst, err)}
	}
	target := lineage.SeedTarget{
		M:      medium.Dir{Root: dst},
		Params: vessel.Params{SlabLog2: k, Slabs: slabs, Rand: rand.Reader},
		VK:     keys.VK, Slots: keys.Slots, Sealer: keys.Sealer, Vessel: keys.Vessel,
		Owner: func() (vessel.Owner, error) {
			if dstLock != nil {
				return dstLock, nil
			}
			l, err := turn.Acquire(dst, v1Patience)
			if err != nil {
				return nil, err
			}
			dstLock = l
			return l, nil
		},
	}
	res, err := lineage.Seed(srcSide, target, plan)
	res.Give = give
	if res.Repeated {
		fmt.Printf("seed %s: %s already took this seed; nothing was recorded\n", id, dst)
		return nil
	}
	again := ""
	if give.Added == 0 {
		again = " (recorded before; used again)"
	}
	fmt.Printf("seed %s: give %s%s, copy %s, take %s\n", id, res.Give.Outcome, again, stepText(res.Copy), stepText(res.Take))
	if err != nil {
		if lineage.Code(res.Copy.Err) == "vessel_full" || lineage.Code(res.Take.Err) == "vessel_full" {
			err = fmt.Errorf("%w; %s is full: the same command with a larger --size grows it and goes on from here", err, dst)
		}
		return exitStatus{code: seedExit(res.Give, res.Copy, res.Take), err: err}
	}
	return nil
}

// goOnHeld readies a seed DST holds, and did not take, before anything is
// recorded on SRC: it grows it to the size asked for, and it writes the
// folder's cells again when they were judged again (keys.Recell), in four
// generations, so that no head file of the folder keeps a cell the seed no
// longer takes (contract 4.6, as for a changed passphrase). A seed that took
// its give is left as it is (S5). It opens the vessel with the key it already
// has and reads no ledger there: a seed cut before its copy holds no event
// yet. It returns the slab count it grew to (0 when it did not grow) and the
// turn it took on DST.
func goOnHeld(dst string, keys SeedKeys, seed frame.ID, total int64) (int, *turn.Lock, error) {
	vk := keys.VK
	d, _, err := carrier.Open(medium.Dir{Root: dst}, func([][]byte, []byte, int) ([]byte, error) { return vk, nil }, rand.Reader, keys.Sealer)
	if err != nil {
		return 0, nil, err
	}
	if lineage.Took(d, seed) {
		return 0, nil, nil
	}
	if keys.Refused != nil {
		return 0, nil, keys.Refused
	}
	v := d.Vessel()
	per := int64(v.Info().SlabSize)
	want := int((total + per - 1) / per)
	grow := want > v.Info().Slabs
	if !grow && !keys.Recell {
		return 0, nil, nil
	}
	lock, err := turn.Acquire(dst, v1Patience)
	if err != nil {
		return 0, nil, err
	}
	grown := 0
	if grow {
		if err := hold(v, lock); err != nil {
			return 0, lock, err
		}
		if err := v.Grow(want); err != nil {
			return 0, lock, err
		}
		grown = want
	}
	if keys.Recell {
		for i := 0; i < vessel.HeadSlots; i++ {
			r, err := d.Begin(lock)
			if err != nil {
				return grown, lock, err
			}
			r.Tx().SetSlots(keys.Slots)
			if out, err := r.Commit(); out != vessel.Recorded {
				return grown, lock, fmt.Errorf("its cells were not written again in every head file (%d of %d): the commit was %s: %v", i, vessel.HeadSlots, out, err)
			}
		}
	}
	return grown, lock, nil
}

// makeSeedFolder makes the new vessel in dst before its give is recorded
// (goal 7.41): its own key and cells, the inherited salt and cost, and the
// seed's id and scopes in its root record. It holds no event yet. From here
// on the folder names its seed. A folder that already holds a vessel is not
// touched: carrier.Create refuses it.
func makeSeedFolder(dst string, src *carrier.Carrier, plan lineage.SeedPlan, keys SeedKeys, slabLog2, slabs int) error {
	info := src.Vessel().Info()
	p := vessel.Params{SlabLog2: slabLog2, Slabs: slabs, Iter: info.Iter, Salt: info.Salt, Rand: rand.Reader}
	root := vessel.Root{Anchor: src.Anchor(), Vessel: keys.Vessel, Seed: plan.Seed, Scopes: plan.Scopes}
	_, err := carrier.CreateRoot(medium.Dir{Root: dst}, p, keys.VK, keys.Slots, root, keys.Sealer)
	return err
}

// stepText is a step's ending as a person reads it: a step never reached says
// so, and a count is given only for what was recorded.
func stepText(h lineage.Half) string {
	switch {
	case h.Outcome == "":
		return "not reached"
	case h.Outcome != vessel.Recorded:
		return string(h.Outcome)
	case h.Held > 0:
		return fmt.Sprintf("%s (+%d, %d withheld)", h.Outcome, h.Added, h.Held)
	}
	return fmt.Sprintf("%s (+%d)", h.Outcome, h.Added)
}

// seedExit is 4 when a step's ending could not be read back, and 3 for any
// other step that was not recorded.
func seedExit(steps ...lineage.Half) int {
	for _, h := range steps {
		if h.Outcome == vessel.Unknown {
			return exitUnknown
		}
	}
	return 3
}

// seedScopes gives the seed's scopes one spelling: each a valid address
// outside the system address, sorted, once, none inside another. None is the
// whole rokh.
func seedScopes(list []string) ([]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	sorted := append([]string(nil), list...)
	sort.Strings(sorted)
	var out []string
	for _, s := range sorted {
		if err := event.ValidAddress(s); err != nil {
			return nil, fmt.Errorf("--scope %q: %w", s, err)
		}
		if s == event.AddressRoot || strings.HasPrefix(s, event.AddressRoot+"/") {
			return nil, fmt.Errorf("--scope %q: the system address travels with every seed; it is no scope", s)
		}
		inside := false
		for _, o := range out {
			if event.ScopeCovers(o, s) {
				inside = true
			}
		}
		if !inside {
			out = append(out, s)
		}
	}
	if len(out) > event.MaxReads {
		return nil, fmt.Errorf("--scope names %d prefixes; a seed holds at most %d", len(out), event.MaxReads)
	}
	return out, nil
}

// apartFrom refuses a DST that is SRC's folder itself or lies inside SRC's
// vessel: a seed is a folder of its own.
func apartFrom(src, dst string) error {
	a, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	b, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	inner := filepath.Join(a, vessel.Dir)
	if b == a || b == inner || strings.HasPrefix(b, inner+string(filepath.Separator)) {
		return fmt.Errorf("%s is %s itself or lies inside its vessel; a seed is a folder of its own", dst, src)
	}
	return nil
}

// seedHeldAt reads what DST already holds, and writes nothing there. No
// vessel folder: nil. A seed of this rokh: its seed id, scopes, key and
// cells. Anything else is refused before anything is recorded: a vessel of
// another rokh (anchor_differs), this rokh's root or SRC itself, a vessel
// that no seed of this rokh made, or a vessel folder this passphrase does
// not open whole.
func seedHeldAt(dst, pass string, src *carrier.Carrier) (*SeedHeld, error) {
	if _, err := os.Stat(filepath.Join(dst, vessel.Dir)); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	d, _, err := carrier.Open(medium.Dir{Root: dst}, v1Unlock(pass), rand.Reader, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %s holds a %s folder that this passphrase does not open as a whole vessel (%v); nothing was recorded and nothing there was touched",
			lineage.ErrSeedRefused, dst, vessel.Dir, err)
	}
	v, s := d.Vessel().Info(), src.Vessel().Info()
	switch {
	case v.Anchor != s.Anchor:
		return nil, fmt.Errorf("%w: %s holds a vessel of another rokh; nothing was recorded", lineage.ErrAnchorDiffers, dst)
	case v.Vessel == s.Vessel:
		return nil, fmt.Errorf("%w: %s is the source's own vessel; nothing was recorded", lineage.ErrSeedRefused, dst)
	case v.Seed.IsZero():
		return nil, fmt.Errorf("%w: %s holds this rokh's root vessel, not a seed; nothing was recorded", lineage.ErrSeedRefused, dst)
	case !bytes.Equal(v.Salt, s.Salt) || v.Iter != s.Iter:
		return nil, fmt.Errorf("%w: %s holds a vessel of this anchor that no seed of this rokh made (its salt or cost differs); nothing was recorded", lineage.ErrSeedRefused, dst)
	}
	empty := true
	if err := d.Vessel().Scan(func(h vessel.Header, _ vessel.Ref) error {
		empty = empty && h.Type != vessel.RecEvent
		return nil
	}); err != nil {
		return nil, err
	}
	var heads [][]byte
	m := medium.Dir{Root: dst}
	for i := 0; i < vessel.HeadSlots; i++ {
		if b, err := m.Read(vessel.HeadName(i), vessel.HeadSize); err == nil {
			heads = append(heads, b)
		}
	}
	return &SeedHeld{Seed: v.Seed, Scopes: v.Scopes, VK: d.Vessel().VK(), Slots: d.Vessel().Slots(), Empty: empty, Vessel: v.Vessel, Heads: heads}, nil
}

// recordOn records, in one commit on c under own, the events c does not hold
// yet, and names the last of them on the branch. With nothing new nothing is
// written, and the step is recorded with nothing added.
func recordOn(c *carrier.Carrier, own vessel.Owner, evs []lineage.Event, branch string) lineage.Half {
	held := map[frame.ID]bool{}
	if err := c.Events(func(e carrier.EventRecord) error { held[e.ID] = true; return nil }, nil); err != nil {
		return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
	}
	var fresh []lineage.Event
	for _, e := range evs {
		if !held[e.ID] {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) == 0 {
		return lineage.Half{Outcome: vessel.Recorded}
	}
	if branch == "" {
		branch = defaultBranch
	}
	r, err := c.Begin(own)
	if err != nil {
		return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
	}
	for _, e := range fresh {
		if err := r.Event(e.ID, e.Head, e.Body, e.Address); err != nil {
			r.Abandon()
			return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
		}
	}
	if err := r.SetRef(branch, fresh[len(fresh)-1].ID); err != nil {
		r.Abandon()
		return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
	}
	out, err := r.Commit()
	return lineage.Half{Outcome: out, Added: len(fresh), Err: err}
}

// slabLog2 reads a slab size and returns its power of two.
func slabLog2(s string) (int, error) {
	n, err := size.Parse(s)
	if err != nil {
		return 0, err
	}
	for k := vessel.MinSlabLog2; k <= vessel.MaxSlabLog2; k++ {
		if int64(1)<<k == n {
			return k, nil
		}
	}
	return 0, fmt.Errorf("slab size %s is not a power of two from 256K to 64M", s)
}
