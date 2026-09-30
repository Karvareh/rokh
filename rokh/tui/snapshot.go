package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// A snapshot as a file.
//
// The screen draws a Snapshot and nothing else, so a screen can be drawn
// from a snapshot written in a file, with no ledger opened, no passphrase
// asked and nothing recorded: to see what Rokh looks like before making
// anything, to show it, and to check a change to the drawing by eye. The
// source carries one, sample.json, the same ledger as Sample.

// ReadSnapshot reads a snapshot written as JSON. A field it does not know is
// refused rather than dropped, so a misspelt name is not silently a blank.
func ReadSnapshot(r io.Reader) (Snapshot, error) {
	var st Snapshot
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&st); err != nil {
		return Snapshot{}, fmt.Errorf("the snapshot could not be read: %w", err)
	}
	return st, nil
}

// WriteSnapshot writes a snapshot as JSON, indented to be read and edited.
func WriteSnapshot(w io.Writer, st Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(st)
}

// errPicture is what the screen answers when a sentence is said to a
// picture: there is no ledger behind it to hear it.
var errPicture = errors.New(`this screen is drawn from a snapshot file: no ledger is open and nothing is recorded. "go" leaves [picture]`)

// Picture draws a snapshot as the screen would draw a ledger, and runs it
// until the person leaves: the views, the list, the prompt and its keys all
// work, and every sentence is answered that there is no ledger behind the
// picture. Where no screen can be drawn it prints the picture once, at the
// size the terminal says or at 80x24, without colour, and returns.
func Picture(in, out *os.File, st Snapshot, signals []os.Signal) error {
	err := Run(in, out, Hooks{
		Say: func(line string) (string, bool, error) {
			if strings.EqualFold(strings.TrimSpace(line), "go") {
				return "", true, nil
			}
			return "", false, errPicture
		},
		Snapshot: func() Snapshot { return st },
		Signals:  signals,
	})
	var ns NoScreen
	if errors.As(err, &ns) {
		o := Measure(out)
		o.Color = false
		_, werr := fmt.Fprintln(out, Render(o, View{}, st))
		return werr
	}
	return err
}

// Measure is the terminal an output is, as the screen would draw on it: its
// size, and the colour it says it draws. An output that is not a terminal
// is 80 columns by 24 rows, unless COLUMNS and LINES say otherwise.
func Measure(out *os.File) Options {
	if isTerminal(out) {
		return detect(out, out)
	}
	o := Options{Columns: 80, Rows: 24}
	if c, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && c > 0 {
		o.Columns = c
	}
	if r, err := strconv.Atoi(os.Getenv("LINES")); err == nil && r > 0 {
		o.Rows = r
	}
	return o
}
