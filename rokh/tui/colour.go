package tui

import (
	"strconv"
	"strings"
)

// Colour depths. A terminal that says it draws 24-bit colour is given the
// owner's own values; one that draws 256 colours is given the nearest of
// those; any other is given one of the sixteen, chosen by meaning. The six
// meanings stay six and apart at every depth, and no seventh appears.
const (
	DepthTrue = 0   // 24-bit: the owner's own values
	Depth256  = 256 // the nearest of the 256 colours of xterm
	Depth16   = 16  // one of the sixteen, by meaning
)

// basic is each meaning's colour among the sixteen: place is blue, working
// state is yellow, settled history is cyan, the recording boundary is red,
// authority is green, lineage is magenta.
var basic = map[Role]int{
	RolePlace: 34, RoleWorking: 33, RoleSettled: 36,
	RoleBoundary: 31, RoleAuthority: 32, RoleLineage: 35,
}

// colourDepth reads what the environment says of the terminal: NO_COLOR, a
// TERM that is dumb or missing, or an output that is not a terminal mean no
// colour; COLORTERM says whether 24-bit colour is drawn; a TERM that names
// 256 colours is given those.
func colourDepth(getenv func(string) string, terminal bool) (colour bool, depth int) {
	term := getenv("TERM")
	if getenv("NO_COLOR") != "" || term == "" || term == "dumb" || !terminal {
		return false, DepthTrue
	}
	switch strings.ToLower(getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return true, DepthTrue
	}
	if strings.Contains(term, "256") {
		return true, Depth256
	}
	return true, Depth16
}

// colourCode is the terminal's code for a meaning at a depth, or nothing
// for the roles that carry no colour.
func colourCode(r Role, depth int) string {
	rgb, ok := Palette[r]
	if !ok {
		return ""
	}
	switch depth {
	case Depth256:
		return "38;5;" + strconv.Itoa(xterm256(rgb))
	case Depth16:
		return strconv.Itoa(basic[r])
	}
	return "38;2;" + strconv.Itoa(int(rgb.R)) + ";" + strconv.Itoa(int(rgb.G)) + ";" + strconv.Itoa(int(rgb.B))
}

// xterm256 is the nearest of the 256 colours to c: the 6x6x6 cube or the
// grey ramp, whichever is closer.
func xterm256(c RGB) int {
	levels := [6]int{0, 95, 135, 175, 215, 255}
	nearest := func(v int) int {
		best := 0
		for i, l := range levels {
			if abs(l-v) < abs(levels[best]-v) {
				best = i
			}
		}
		return best
	}
	r, g, b := int(c.R), int(c.G), int(c.B)
	ri, gi, bi := nearest(r), nearest(g), nearest(b)
	cube := 16 + 36*ri + 6*gi + bi
	cubeDist := sq(levels[ri]-r) + sq(levels[gi]-g) + sq(levels[bi]-b)
	grey := min(23, max(0, ((r+g+b)/3-8+5)/10))
	gv := 8 + 10*grey
	if sq(gv-r)+sq(gv-g)+sq(gv-b) < cubeDist {
		return 232 + grey
	}
	return cube
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sq(v int) int { return v * v }
