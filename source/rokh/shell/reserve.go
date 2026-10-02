package shell

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"rokh/size"
	"rokh/vessel"
)

// The room: how large a person makes their Rokh.
//
// A Rokh has a size (axiom 5): its vessel is a number of equal slabs, set
// when it is made, and changed on purpose (rokh grow, rokh shrink) or, when
// the person asks for it, grown by itself as it fills. The gate asks for that
// size, checks it against the room the disk has, and makes the vessel that
// size in whole slabs. The first event says nothing of it: the vessel is
// where the size lives, and a size written in the first event could not
// follow it when it changes.

const (
	// minRoom is the smallest vessel: sixteen slabs of 256 KiB.
	minRoom = int64(vessel.MinSlabs) << vessel.MinSlabLog2
	// defaultRoom is the vessel's own default: 64 slabs of 1 MiB.
	defaultRoom = int64(vessel.DefaultSlabs) << vessel.DefaultSlabLog2
)

// reserveLine is how a room was once written under the first sentence of a
// ledger's first event. It is read only to describe such an event.
const reserveLine = "reserving "

// roomParams is a vessel of at least room bytes, in whole slabs: slabs of 1
// MiB, or of 256 KiB for a room under 16 MiB, or larger for a room past the
// most slabs a vessel counts.
func roomParams(room int64) (vessel.Params, error) {
	if room < minRoom {
		return vessel.Params{}, fmt.Errorf("the smallest Rokh is %s", size.Write(minRoom))
	}
	k := vessel.DefaultSlabLog2
	if room < int64(vessel.MinSlabs)<<k {
		k = vessel.MinSlabLog2
	}
	slabs := func(k int) int64 { return (room + (int64(1) << k) - 1) >> k }
	for k < vessel.MaxSlabLog2 && slabs(k) > vessel.MaxSlabs {
		k++
	}
	n := slabs(k)
	if n > vessel.MaxSlabs {
		return vessel.Params{}, fmt.Errorf("the largest Rokh is %s", size.Write(int64(vessel.MaxSlabs)<<vessel.MaxSlabLog2))
	}
	return vessel.Params{SlabLog2: k, Slabs: int(max(n, vessel.MinSlabs))}, nil
}

// roomOfLedger is the room of an open ledger as its vessel holds it now:
// its size, what the next recording may write, and its growth, written out,
// and whether it is full.
func roomOfLedger(l *openLedger) (room, free, growth string, full bool) {
	info := l.car.Vessel().Info()
	slab := int64(info.SlabSize)
	growth = "fixed"
	if info.Growth.Auto {
		growth = "by itself, up to " + size.Write(int64(info.Growth.Max)*slab)
	}
	return size.Write(int64(info.Slabs) * slab), size.Write(int64(info.Free) * slab), growth, info.Free == 0
}

// roomOf is the size of a vessel made with p.
func roomOf(p vessel.Params) int64 { return int64(p.Slabs) << p.SlabLog2 }

// growing is p, grown by itself as it fills: a quarter of its size at a
// time, up to the room the disk had free when it was made, or as far as a
// vessel goes when the disk would not say.
func growing(p vessel.Params, free int64, known bool) vessel.Params {
	most := int64(vessel.MaxSlabs)
	if known {
		most = min(most, free>>p.SlabLog2)
	}
	p.Growth = vessel.Growth{Auto: true, Step: max(1, p.Slabs/4), Max: int(max(most, int64(p.Slabs)))}
	return p
}

// askRoom asks how large the Rokh should be and whether it grows by itself,
// and checks the size against the room the disk has. An empty answer is the
// vessel's own default.
func askRoom(w io.Writer, vault string) (vessel.Params, error) {
	free, known := freeOn(vault)
	question := fmt.Sprintf("how much room should your Rokh have? (for example 500M or 2G; enter for %s): ", size.Write(defaultRoom))
	if known {
		question = fmt.Sprintf("how much room should your Rokh have? (for example 500M or 2G; enter for %s; %s free here): ",
			size.Write(defaultRoom), size.Write(free))
	}
	for {
		answer, err := askVisible(question)
		if err != nil {
			return vessel.Params{}, err
		}
		room := defaultRoom
		if strings.TrimSpace(answer) != "" {
			if room, err = size.Parse(answer); err != nil {
				fmt.Fprintln(w, err)
				continue
			}
		}
		p, err := roomParams(room)
		if err != nil {
			fmt.Fprintf(w, "%v; name a size of at least that.\n", err)
			continue
		}
		if known && roomOf(p) > free {
			fmt.Fprintf(w, "this disk has %s free, and a Rokh of %s would not fit. Name a smaller size.\n",
				size.Write(free), size.Write(roomOf(p)))
			continue
		}
		if !known {
			fmt.Fprintln(w, "I could not ask this disk how much room it has, so that size is your word for it.")
		}
		grow, err := askVisible("should it grow by itself when it fills? [y/N] ")
		if err != nil {
			return vessel.Params{}, err
		}
		if yes(grow) {
			p = growing(p, free, known)
		}
		return p, nil
	}
}

// rooms hands the size a person chose at the gate to the making of the
// carrier, which happens a few calls further down, keyed by the ledger's
// folder. The gate sets it only for the one call that makes that folder.
var rooms = struct {
	sync.Mutex
	m map[string]vessel.Params
}{m: map[string]vessel.Params{}}

// withRoom makes the carrier in dir, while f runs, with the size p.
func withRoom(dir string, p vessel.Params, f func() error) error {
	rooms.Lock()
	rooms.m[dir] = p
	rooms.Unlock()
	defer func() {
		rooms.Lock()
		delete(rooms.m, dir)
		rooms.Unlock()
	}()
	return f()
}

// roomFor is the size chosen for the carrier about to be made in dir.
func roomFor(dir string) (vessel.Params, bool) {
	rooms.Lock()
	defer rooms.Unlock()
	p, ok := rooms.m[dir]
	return p, ok
}

// freeOn is how many bytes the disk holding a folder has free, and whether it
// would say. It asks df, the way the passphrase prompt asks stty: the standard
// library has no portable way to ask, and this project spells out no system
// call per platform. A machine without df is not an error — the size is then
// the person's word, and the gate says so rather than pretending it checked.
func freeOn(dir string) (int64, bool) {
	for at := dir; ; {
		if _, err := os.Stat(at); err == nil {
			dir = at
			break
		}
		parent := filepath.Dir(at)
		if parent == at {
			return 0, false
		}
		at = parent
	}
	out, err := exec.Command("df", "-Pk", dir).Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, false
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, false
	}
	blocks, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, false
	}
	return blocks * 1024, true
}
