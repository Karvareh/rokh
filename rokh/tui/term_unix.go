//go:build unix

package tui

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// The seam on the systems that have stty: the terminal is driven through it,
// the same way the passphrase prompt hushes the echo.

func stty(tty *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Interactive reports whether a person is at the other end of f: a
// terminal whose settings can be read, and not a pipe, a file, or
// /dev/null, which is a character device too.
func Interactive(f *os.File) bool {
	if !isTerminal(f) {
		return false
	}
	_, err := stty(f, "-g")
	return err == nil
}

// raw puts the terminal in raw mode — every key arrives as it is pressed,
// nothing echoes, and a read waits at most a tenth of a second — and returns
// the call that puts it back. The restore runs however the surface ends,
// because a terminal left raw is one the person has to repair by hand.
func raw(tty *os.File) (restore func(), err error) {
	saved, err := stty(tty, "-g")
	if err != nil {
		return nil, errors.New("could not read the terminal's settings: " + err.Error())
	}
	if _, err := stty(tty, "raw", "-echo", "min", "0", "time", "1"); err != nil {
		return nil, errors.New("could not put the terminal in raw mode: " + err.Error())
	}
	return func() { _, _ = stty(tty, saved) }, nil
}

// size asks the terminal how big it is. When it will not say, the answer
// comes from COLUMNS and LINES, and failing those, from nothing.
func size(tty *os.File) (cols, rows int, ok bool) {
	if tty != nil {
		if out, err := stty(tty, "size"); err == nil {
			if f := strings.Fields(out); len(f) == 2 {
				r, e1 := strconv.Atoi(f[0])
				c, e2 := strconv.Atoi(f[1])
				if e1 == nil && e2 == nil && r > 0 && c > 0 {
					return c, r, true
				}
			}
		}
	}
	c, e1 := strconv.Atoi(os.Getenv("COLUMNS"))
	r, e2 := strconv.Atoi(os.Getenv("LINES"))
	return c, r, e1 == nil && e2 == nil && c > 0 && r > 0
}
