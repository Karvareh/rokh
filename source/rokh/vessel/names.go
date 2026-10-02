// Package vessel is the keeping layer of a rokh: one directory of equal-sized,
// encrypted slab files and four head files, with one commit point and
// recovery (contract sections 1 and 2).
//
// The package is portable core. It holds no file call, no clock and no lock:
// files come through an injected Medium, the writer's lock through an injected
// Owner, and every random byte through an injected io.Reader. With the same
// inputs and the same random stream it writes the same bytes on every
// platform (C5).
//
// At every instant, also in the middle of a write, the vessel is exactly
// N + 4 files of N*S + 4*65,536 bytes. The format has no temporary file, no
// lock file and no rename (V6).
package vessel

import (
	"errors"
	"fmt"
)

// Format names and layout constants (contract section 1).
const (
	Format    = "rokh.vessel/1"
	Magic     = "RKV1"
	Dir       = "rokh" // the whole footprint, inside the carrier folder
	HeadSlots = 4      // head0.rkh .. head3.rkh
	HeadSize  = 65536
	SlotCells = 32
	CellSize  = 256
	Retention = 3 // R: a slab live in any of the last R generations is never written

	MinSlabLog2     = 18
	MaxSlabLog2     = 26
	DefaultSlabLog2 = 20
	MinSlabs        = 16
	MaxSlabs        = 1 << 22
	DefaultSlabs    = 64
	DefaultIter     = 600000
	MaxIter         = 10000000

	SegmentEntries = 4096 // slabs covered by one inventory segment
	EntrySize      = 46
	SlabOverhead   = 48 // salt(32) + tag(16)
	SlabHeader     = 8  // kind(1) zero(3) used(4)
	ChunkMargin    = 4096

	KDFPBKDF2 = 0x01

	slotsOff   = 64
	rootBoxOff = slotsOff + SlotCells*CellSize // 8256
	perDir     = 1024
	maxDirs    = 4096
)

// Slab kinds.
const (
	KindSegment byte = 0x01
	KindPack    byte = 0x02
)

// Record types (contract 2.5).
const (
	RecEvent   byte = 0x01
	RecContent byte = 0x02
	RecPointer byte = 0x03
	RecObject  byte = 0x04
)

// Spaces of pointers and objects.
const (
	SpaceRefs     byte = 0x01 // ledger references (branches)
	SpaceHomeLow  byte = 0x10 // the home's objects, pointers, journal
	SpaceHomeHigh byte = 0x1F
)

// Entry states of the inventory.
const (
	stateNever byte = 0x00
	stateLive  byte = 0x01
	stateFree  byte = 0x02
)

// Footprint is everything a vessel puts in a carrier folder.
func Footprint() []string { return []string{Dir} }

// HeadName is the name of head file i (0..3).
func HeadName(i int) string { return fmt.Sprintf("%s/head%d.rkh", Dir, i) }

// SlabDirName is the directory of slab index.
func SlabDirName(index uint32) string { return fmt.Sprintf("%s/%03x", Dir, index>>10) }

// SlabName is the file of slab index: rokh/<ddd>/<nnnnnnnn>.rkh.
func SlabName(index uint32) string {
	return fmt.Sprintf("%s/%03x/%08x.rkh", Dir, index>>10, index)
}

// ValidName reports whether a path is inside the vessel grammar: lowercase
// a-z 0-9, 8.3 shape with extension rkh, directories of three hex digits.
func ValidName(name string) bool {
	switch name {
	case HeadName(0), HeadName(1), HeadName(2), HeadName(3):
		return true
	}
	var d, n uint32
	var ext string
	if len(name) != len("rokh/000/00000000.rkh") {
		return false
	}
	if _, err := fmt.Sscanf(name, "rokh/%03x/%08x.%s", &d, &n, &ext); err != nil {
		return false
	}
	return ext == "rkh" && SlabName(n) == name && n>>10 == d
}

// codeError carries the stable code a booth or a command answers with.
type codeError struct {
	code string
	msg  string
}

func (e *codeError) Error() string { return "vessel: " + e.msg }

// Errors with stable codes (contract B7).
var (
	ErrFull            error = &codeError{"vessel_full", "full: the free slabs do not hold this commit"}
	ErrCorrupt         error = &codeError{"vessel_corrupt", "corrupt: a slab or head did not verify"}
	ErrTurnLost        error = &codeError{"turn_lost", "the writer no longer holds this vessel; nothing was committed"}
	ErrTurnUnavailable error = &codeError{"turn_unavailable", "no owner is held; this vessel is open for reading only"}
	ErrNotAVessel      error = &codeError{"not_a_vessel", "not a rokh vessel"}
)

// Plain errors of the Medium boundary.
var (
	ErrExists   = errors.New("vessel: already exists")
	ErrNotFound = errors.New("vessel: not found")
	ErrLocked   = errors.New("vessel: no key opens this vessel")
	ErrTooLarge = errors.New("vessel: over limit")
	ErrStale    = errors.New("vessel: stale reference; scan again")
)

// Code returns the stable code of an error, or "" when it has none.
func Code(err error) string {
	var ce *codeError
	if errors.As(err, &ce) {
		return ce.code
	}
	return ""
}

// wrap keeps a code while adding detail.
func wrap(base error, format string, a ...any) error {
	return fmt.Errorf("%w: %s", base, fmt.Sprintf(format, a...))
}
