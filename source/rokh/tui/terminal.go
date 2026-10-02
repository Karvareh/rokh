package tui

import (
	"os"
)

// The terminal's own settings — raw mode, and the window's size — are reached
// through one small seam, and only there: term_unix.go on the systems that
// have stty, term_other.go on the rest. Go's standard library has no portable
// way to put a terminal in raw mode or to ask its size, and this module takes
// no dependencies and spells out no system call by hand for each platform.
// Where the seam has nothing, raw mode cannot be set, the screen is not
// started, and the line surface is, and says why.

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// detect measures the terminal and decides on colour. NO_COLOR and TERM=dumb
// are honoured because they are how a person says no; COLORTERM and TERM
// say how many colours the terminal draws (colourDepth).
func detect(tty, out *os.File) Options {
	cols, rows, ok := size(tty)
	if !ok {
		cols, rows = 80, 24
	}
	colour, depth := colourDepth(os.Getenv, isTerminal(out))
	return Options{Columns: cols, Rows: rows, Color: colour, Depth: depth}
}
