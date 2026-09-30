//go:build !unix

package tui

import (
	"errors"
	"os"
	"strconv"
)

// The seam on the systems without stty: nothing sets raw mode here, so the
// screen is not started and the line surface is, and says why. The size is
// what COLUMNS and LINES say, for drawing a picture or the demo.

var errNoSeam = errors.New("this system gives the screen no way to set the terminal's mode")

// Interactive reports whether f is a terminal a person may be at.
func Interactive(f *os.File) bool { return isTerminal(f) }

func raw(*os.File) (func(), error) { return nil, errNoSeam }

func size(*os.File) (cols, rows int, ok bool) {
	c, e1 := strconv.Atoi(os.Getenv("COLUMNS"))
	r, e2 := strconv.Atoi(os.Getenv("LINES"))
	return c, r, e1 == nil && e2 == nil && c > 0 && r > 0
}
