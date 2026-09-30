package main

// The vessel commands of v1: grow, shrink, reconcile (contract 2.8, 4.8, 6).
// They are host code: they take the writer's turn from package turn, keep the
// vessel in files through package medium, and hand the owner to the core.
//
// The key layer is reached through the hooks below, which the key's command
// files set in their own init(). Until they are set these commands say so
// and write nothing.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"rokh/carrier"
	"rokh/frame"
	"rokh/lineage"
	"rokh/medium"
	"rokh/size"
	"rokh/turn"
	"rokh/vessel"
)

func init() {
	v1Commands["grow"] = cmdGrow
	v1Commands["shrink"] = cmdShrink
	v1Commands["reconcile"] = cmdReconcile
}

// Hooks of the key layer. Each is set by the key's command files.
var (
	// v1Unlock turns a passphrase into the vessel's unlock (key.Unlock).
	v1Unlock func(pass string) vessel.Unlock
	// v1Sealer is the session sealer of an opened carrier for a passphrase
	// (key.Session over the carrier's keyring).
	v1Sealer func(c *carrier.Carrier, pass string) (carrier.Sealer, error)
	// v1Merge signs one rokh.merge over the given heads for `reconcile --merge`.
	v1Merge func(c *carrier.Carrier, pass string, heads []frame.ID) (lineage.Event, error)
	// v1Judge gives, for an opened carrier and its passphrase, the key
	// layer's judge (an id to "full", "lineage", "pending" or "refused", read
	// from the ledger) and its owner check (NamesAll with the owner
	// generations of the record's point). While it is nil, reconcile and seed
	// refuse with ancestry_unproven.
	// covers takes the record's id with its envelope: the owners live at that
	// record's point are the ones it must name.
	v1Judge func(c *carrier.Carrier, pass string) (judge func(frame.ID) string, covers func(id frame.ID, env []byte) bool, err error)
	// v1Cells has one side of a reconcile, after the union, install the
	// cells of the live keys its ledger holds and its vessel lacks (R2). It
	// records no event.
	v1Cells func(c *carrier.Carrier, own vessel.Owner, pass string) (cellsLearned, error)
	// v1Boundary reads, for one side of a reconcile after the union, what the
	// owner decides on and Rokh does not choose (R7, B4): the side's heads
	// and its concurrent keys.
	v1Boundary func(c *carrier.Carrier, pass string) (sideBoundary, error)
)

// sideBoundary is what one side shows after a reconcile (R7): its heads,
// and its concurrent keys, one line each, every live generation of such a
// key named by its add.
type sideBoundary struct {
	heads      []frame.ID
	concurrent []string
}

// cellsLearned is what one side of a reconcile did with the cells of the
// live keys its ledger holds (R2).
type cellsLearned struct {
	installed int
	// byKey is set when the passphrase opens a key's cell: no cell is
	// installed then.
	byKey bool
	// outcome is that of the commit that installed them; empty when no
	// commit was made.
	outcome vessel.Outcome
}

// judgedSide is one side of a union with the key layer's judge and owner
// check; the carrier refuses an envelope without the owner at commit too.
func judgedSide(c *carrier.Carrier, own vessel.Owner, pass string) (lineage.Side, error) {
	if v1Judge == nil {
		return lineage.Side{}, fmt.Errorf("%w: the key layer gives no judge in this build", lineage.ErrAncestryUnproven)
	}
	judge, covers, err := v1Judge(c, pass)
	if err != nil {
		return lineage.Side{}, err
	}
	if judge == nil || covers == nil {
		return lineage.Side{}, lineage.ErrAncestryUnproven
	}
	// The owner check travels with the side and is applied by the sender to
	// every envelope it offers, against the ledger that holds the record
	// (lineage.offer). The receiving carrier is not given it for union: an
	// arriving event is not yet in its ledger, so it cannot place the point.
	return lineage.Side{C: c, Own: own, Judge: judge, Covers: covers}, nil
}

var errNoKeyLayer = errors.New("the key layer is not wired into this build; nothing was recorded")

const v1Patience = 15 * time.Second

// openCarrier opens the carrier in dir. With write it first takes the
// writer's turn, which the caller releases.
func openCarrier(dir, pass string, write bool) (*carrier.Carrier, *turn.Lock, vessel.Report, error) {
	if v1Unlock == nil {
		return nil, nil, vessel.Report{}, errNoKeyLayer
	}
	var lock *turn.Lock
	if write {
		l, err := turn.Acquire(dir, v1Patience)
		if err != nil {
			return nil, nil, vessel.Report{}, fmt.Errorf("nothing was recorded: %w", err)
		}
		lock = l
	}
	c, rep, err := carrier.Open(medium.Dir{Root: dir}, v1Unlock(pass), rand.Reader, nil)
	if err != nil {
		if lock != nil {
			lock.Release()
		}
		return nil, nil, rep, err
	}
	if v1Sealer != nil {
		s, err := v1Sealer(c, pass)
		if err != nil {
			if lock != nil {
				lock.Release()
			}
			return nil, nil, rep, err
		}
		c.SetSealer(s)
	}
	printFallback(rep)
	return c, lock, rep, nil
}

// printFallback says when the vessel opened an older generation than the
// highest it saw (2.7): the caller learns it before anything is written.
func printFallback(rep vessel.Report) {
	for _, f := range rep.FellBack {
		fmt.Fprintf(os.Stderr, "note: generation %d did not verify (%s); opened %d\n", f.From, f.Why, rep.Generation)
	}
	if len(rep.Beyond) > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slab files lie beyond the vessel's size and are ignored\n", len(rep.Beyond))
	}
	if len(rep.Missing) > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slab files are missing\n", len(rep.Missing))
	}
}

// slabsFor turns a size such as 256M into a slab count for this vessel.
func slabsFor(v *vessel.Vessel, sz string) (int, error) {
	if sz == "" {
		return 0, errors.New("--to SIZE is required, for example --to 256M; it names a target, never a delta")
	}
	n, err := size.Parse(sz)
	if err != nil {
		return 0, err
	}
	per := int64(v.Info().SlabSize)
	return int((n + per - 1) / per), nil
}

func describe(dir string, v *vessel.Vessel) {
	info := v.Info()
	fmt.Printf("vessel %s: %d slabs of %s, %s in all, generation %d\n", dir, info.Slabs,
		size.Write(int64(info.SlabSize)), size.Write(int64(info.Slabs)*int64(info.SlabSize)), info.Generation)
}

// hold registers the host's owner with the vessel for Grow, Shrink and
// Compact, which write under the owner of the last Begin.
func hold(v *vessel.Vessel, lock *turn.Lock) error {
	tx, err := v.Begin(lock)
	if err != nil {
		return err
	}
	return tx.Abandon()
}

func cmdGrow(args []string) error {
	fs, dir, err := newFlags("grow", args)
	if err != nil {
		return err
	}
	to := fs.String("to", "", "the vessel's new size, e.g. 256M; a target, never a delta")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	c, lock, _, err := openCarrier(dir, pass, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	v := c.Vessel()
	n, err := slabsFor(v, *to)
	if err != nil {
		return err
	}
	if err := hold(v, lock); err != nil {
		return err
	}
	if err := v.Grow(n); err != nil {
		return err
	}
	describe(dir, v)
	return nil
}

func cmdShrink(args []string) error {
	fs, dir, err := newFlags("shrink", args)
	if err != nil {
		return err
	}
	to := fs.String("to", "", "the vessel's new size, e.g. 64M; a target, never a delta")
	finish := fs.Bool("finish", false, "remove the files beyond the new size now")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	c, lock, _, err := openCarrier(dir, pass, true)
	if err != nil {
		return err
	}
	defer lock.Release()
	v := c.Vessel()
	n, err := slabsFor(v, *to)
	if err != nil {
		return err
	}
	if err := hold(v, lock); err != nil {
		return err
	}
	if err := v.Shrink(n, *finish); err != nil {
		if vessel.Code(err) != "" {
			return fmt.Errorf("%w (code %s)", err, vessel.Code(err))
		}
		return err
	}
	describe(dir, v)
	return nil
}

// cmdReconcile is `rokh reconcile DIR OTHER [--merge]` (4.8): the union of
// records both ways, then each side installs the cells of the live keys it
// holds (R2), and with --merge one rokh.merge on this side. The answer names
// both halves; anything but two recorded halves is exit 3, or 4 when a half
// is unknown. Two seeds become one only by this command (the owner's word of
// 2026-09-29): nothing reconciles without being asked, and R6 is not built.
func cmdReconcile(args []string) error {
	fs, dir, err := newFlags("reconcile", args)
	if err != nil {
		return err
	}
	merge := fs.Bool("merge", false, "record one rokh.merge after the union")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("give the other carrier: rokh reconcile DIR OTHER")
	}
	other := fs.Arg(0)
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	a, la, _, err := openCarrier(dir, pass, true)
	if err != nil {
		return err
	}
	defer la.Release()
	b, lb, _, err := openCarrier(other, pass, true)
	if err != nil {
		return err
	}
	defer lb.Release()
	// A seed cut before any event arrived is not filled by a union: it took
	// nothing, and its own seed command goes on with it (goal 7.41).
	for _, s := range []struct {
		dir string
		c   *carrier.Carrier
	}{{dir, a}, {other, b}} {
		held, err := holdsEvents(s.c)
		if err != nil {
			return exitStatus{code: 3, err: err}
		}
		if !held {
			return exitStatus{code: 3, err: fmt.Errorf("%s %w; nothing was moved", s.dir, errNoEventYet)}
		}
	}
	sa, err := judgedSide(a, la, pass)
	if err != nil {
		return exitStatus{code: 3, err: err}
	}
	sb, err := judgedSide(b, lb, pass)
	if err != nil {
		return exitStatus{code: 3, err: err}
	}
	res, err := lineage.Reconcile(sa, sb)
	fmt.Println(res)
	if err != nil {
		return exitStatus{code: res.ExitCode(), err: err}
	}
	sides := []reconcileSide{{"local", a, la}, {"remote", b, lb}}
	if err := reconcileCells(sides, pass); err != nil {
		return err
	}
	if err := showBoundary(sides, pass, *merge); err != nil {
		return err
	}
	if *merge {
		if v1Merge == nil {
			return exitStatus{code: 3, err: fmt.Errorf("the union is recorded on both sides; the merge was not: %w", errNoKeyLayer)}
		}
		heads, err := a.Heads()
		if err != nil {
			return err
		}
		e, err := v1Merge(a, pass, heads)
		if err != nil {
			return exitStatus{code: 3, err: err}
		}
		r, err := a.Begin(la)
		if err != nil {
			return exitStatus{code: 3, err: err}
		}
		if err := r.Event(e.ID, e.Head, e.Body, e.Address); err != nil {
			r.Abandon()
			return exitStatus{code: 3, err: err}
		}
		if err := r.SetRef("main", e.ID); err != nil {
			r.Abandon()
			return exitStatus{code: 3, err: err}
		}
		out, err := r.Commit()
		fmt.Printf("merge %s: %s\n", e.ID.Short(), out)
		switch out {
		case vessel.Recorded:
		case vessel.Unknown:
			return exitStatus{code: 4, err: fmt.Errorf("the merge's ending could not be read back: %v", err)}
		default:
			return exitStatus{code: 3, err: fmt.Errorf("the merge was not recorded: %v", err)}
		}
	}
	return nil
}

// reconcileSide is one side of a reconcile, as the answer names it.
type reconcileSide struct {
	name string
	c    *carrier.Carrier
	own  vessel.Owner
}

// reconcileCells is R2 after the union, on each side: the side installs the
// cells of the live keys its ledger holds and its vessel lacks, by the host's
// decision of 2026-09-29, and the answer says what each side did. A side
// whose cells were not installed is a failure of the whole, named with its
// reason: exit 3, or 4 when the ending of its commit is unknown. The union
// itself stays recorded on both sides.
func reconcileCells(sides []reconcileSide, pass string) error {
	if v1Cells == nil {
		return exitStatus{code: 3, err: fmt.Errorf("the union is recorded on both sides; no key cell was installed: %w", errNoKeyLayer)}
	}
	var failed []string
	code := 0
	for _, s := range sides {
		got, err := v1Cells(s.c, s.own, pass)
		switch {
		case err != nil:
			fmt.Printf("%s: key cells not installed: %v\n", s.name, err)
			failed = append(failed, s.name)
			if got.outcome == vessel.Unknown {
				code = exitUnknown
			} else if code == 0 {
				code = 3
			}
		case got.byKey:
			fmt.Printf("%s: no key cell installed: this passphrase opens a key's cell, and only the owner's tells a free cell from the owner's own\n", s.name)
		case got.installed == 1:
			fmt.Printf("%s: 1 key cell installed\n", s.name)
		case got.installed > 1:
			fmt.Printf("%s: %d key cells installed\n", s.name, got.installed)
		}
	}
	switch len(failed) {
	case 0:
		return nil
	case 1:
		return exitStatus{code: code, err: fmt.Errorf("the union is recorded on both sides; key cells were not installed on the %s side", failed[0])}
	}
	return exitStatus{code: code, err: fmt.Errorf("the union is recorded on both sides; key cells were not installed on the %s sides", strings.Join(failed, " and "))}
}

// showBoundary is R7 and B4 on the answer: after the union each side says
// its heads and its concurrent keys, as its own ledger reads them, and they
// are the same on both sides where both hold the same records. Rokh shows the
// boundary and chooses nothing: two heads stay two until the owner merges
// them, and a key with several live generations stays concurrent until the
// owner leaves it one (contract 4.4). With --merge the owner joins the heads
// in the same command, and only the keys are left to the owner. A side that
// cannot be read is a failure; the union stays recorded.
func showBoundary(sides []reconcileSide, pass string, merging bool) error {
	if v1Boundary == nil {
		return exitStatus{code: 3, err: fmt.Errorf("the union is recorded on both sides; the heads and keys cannot be read: %w", errNoKeyLayer)}
	}
	apart, concurrent := false, false
	for _, s := range sides {
		bd, err := v1Boundary(s.c, pass)
		if err != nil {
			return exitStatus{code: 3, err: fmt.Errorf("the union is recorded on both sides; the heads and keys of the %s side could not be read: %w", s.name, err)}
		}
		ids := make([]string, len(bd.heads))
		for i, h := range bd.heads {
			ids[i] = h.Short()
		}
		fmt.Printf("%s: heads %s\n", s.name, strings.Join(ids, " "))
		keys := "none"
		if len(bd.concurrent) > 0 {
			keys = strings.Join(bd.concurrent, "; ")
		}
		fmt.Printf("%s: concurrent keys: %s\n", s.name, keys)
		apart = apart || len(bd.heads) > 1
		concurrent = concurrent || len(bd.concurrent) > 0
	}
	var left []string
	if apart && !merging {
		left = append(left, "heads stay apart until the owner merges them (rokh reconcile --merge)")
	}
	if concurrent {
		left = append(left, "a concurrent key stays so until the owner leaves it one live generation (rokh key rotate)")
	}
	if len(left) > 0 {
		fmt.Println("nothing was chosen: " + strings.Join(left, ", and "))
	}
	return nil
}

// splitList reads a comma-separated list, dropping empty items.
func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
