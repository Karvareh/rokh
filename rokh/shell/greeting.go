package shell

import (
	"fmt"
	"io"
	"os"
	"rokh/tui"
)

// Sentences is the whole human surface. It lives in the tui package so the
// two surfaces cannot drift apart: what the screen offers and what the line
// prints are one list, and the parser is the only judge of either.
var Sentences = tui.Sentences

// showSentences prints them. It touches nothing: asking what may be said is
// not one of the eight verbs and never becomes an event.
//
//	— T9.1, T4.1
func showSentences(w io.Writer) {
	fmt.Fprintln(w, "\nThe sentences I know:")
	for _, sn := range Sentences {
		fmt.Fprintf(w, "  %s\n      %s\n      %s\n", sn.Say, sn.Plain, sn.Does)
	}
	fmt.Fprintln(w, "\n\"?\" or \"/\" brings this list back.")
}

// greet is what a person sees when they arrive at the prompt with nothing to
// go on.
//
// Without it the whole surface is a question mark and an empty line, and
// somebody who has just installed this has no way to find out what may be
// said. It is printed only when a person is actually there — never into a
// pipe, where it would be noise in somebody's script.
func greet(w io.Writer, vault string) {
	fmt.Fprintf(w, "rokh — your event ledger.  vault: %s\n", vault)
	fmt.Fprintln(w, "\"?\" or \"/\" lists the sentences. \"go\" leaves.")
	fmt.Fprintln(w)
}

// greetHome is greet for the other back end. A home has no folder to name —
// the standing block that follows says whose hands the person is in — so this
// says what the program is and how to leave, which is the part the standing
// block does not carry. Both surfaces open the same way on purpose.
func greetHome(w io.Writer) {
	fmt.Fprintln(w, "rokh — your event ledger.  through a home's gate.")
	fmt.Fprintln(w, "\"?\" or \"/\" lists the sentences. \"go\" leaves.")
	fmt.Fprintln(w)
}

// interactive reports whether a person is at the other end, rather than a pipe
// or a file. Nothing is greeted into a script.
// It asks the screen's own seam, so the two agree on every system.
func interactive(f *os.File) bool { return tui.Interactive(f) }

// personThere is interactive for a surface that was handed its streams rather
// than reaching for os.Stdin: a buffer or a pipe is not a person, and only a
// real terminal on both the reading and the telling end is one.
func personThere(in io.Reader, notes io.Writer) bool {
	r, ok := in.(*os.File)
	if !ok {
		return false
	}
	w, ok := notes.(*os.File)
	if !ok {
		return false
	}
	return interactive(r) && interactive(w)
}

// asking reports whether a line is a request for the sentences rather than one
// of them. The screen answers the same words the same way (tui.Asking).
func asking(line string) bool { return tui.Asking(line) }

// letGo is what leaving says when sentences were still waiting: they were
// never recorded, and a person should not learn that from their absence.
// When a recording in this session ended unknown, the sentence it carried is
// still waiting and may be in the ledger all the same: that is said instead.
func letGo(n int, unsure bool) string {
	waiting := "1 sentence was waiting and was let go"
	if n != 1 {
		waiting = digits(n) + " sentences were waiting and were let go"
	}
	if unsure {
		return waiting + "; leaving records nothing, but a recording above ended unknown: read the ledger to see whether it holds that sentence."
	}
	return waiting + "; nothing was recorded."
}
