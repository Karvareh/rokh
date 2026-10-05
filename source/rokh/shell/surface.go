// Package shell is the human surface of Rokh: a closed set of canonical
// sentences over the same doors every program uses.
//
// It is a package rather than a command so that the one binary a person
// installs can *be* it. Somebody who types "rokh" wants their ledger, not a
// second program's name to remember; rokh-shell stays as a command for anyone
// who wants only this and nothing else.
//
// It is an adapter, like rokh-forms: it adds no core semantics, opens no
// socket of any kind, and signs nothing the person did not ask for in a
// sentence. The subcommands and the daemon remain the bridge for programs;
// this is a front for a person.
//
// The vault is a plain folder holding ledgers/<local-name>/, each a normal
// carrier. Local names are aliases; identity is the anchor. The library - the
// star's content store - lives outside the vault, one layer below Rokh, and
// is passed explicitly. Seats (--mount) are neutral opening places that must
// declare themselves before anything is offered.
//
// One principle, recorded here as in the docs: every machine is governed by
// somebody's rokh. Operating on another person's star means being a guest
// under their covenants. Guest mode is named; nothing of it is built.
package shell

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"rokh/tui"
)

// Run is the sentence surface from a command line, and returns the exit code.
//
// It takes its own flag set rather than the global one, because more than one
// program calls it and a program that has already parsed its own flags must
// not find them redefined here.
func Run(name string, args []string) int {
	return RunWithHome(name, args, nil)
}

// RunWithHome lets the command supply an already-connected gate stream. The
// surface owns no socket API: opening/validating the inherited descriptor is
// the command's job, while sentences only read and write the supplied stream.
func RunWithHome(name string, args []string, openHome func(int) (io.ReadWriteCloser, error)) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var (
		library = fs.String("library", "", "the star's library; content lives at <library>/rokh/<anchor>/")
		mount   = fs.String("mount", "", "the seats folder; berths and mirrors declare themselves there")
		oneShot = fs.String("c", "", "one sentence, no REPL")
		plain   = fs.Bool("plain", false, "the line surface even on a terminal; no screen")
		gateFD  = fs.Int("home-fd", -1, "speak to a home's gate on this inherited descriptor")
		state   = fs.String("state", "", "draw the screen from this snapshot file; no Rokh is opened")
		demo    = fs.Bool("demo", false, "draw every screen from the sample ledger and leave; no Rokh is opened")
	)
	fs.Usage = func() { UsageFor(name) }
	// Go's flag package stops at the first argument that is not a flag, so
	// "rokh ~/vault -c ..." would parse the folder and then quietly ignore the
	// -c. A person writes the folder first because that is the thing they are
	// talking about, so the folder is lifted out before parsing rather than
	// the person being told to put their words in the machine's order.
	vault, rest := split(args)
	if err := fs.Parse(rest); err != nil {
		// Asking what the flags are is not an error, and ends like one that
		// was answered; a flag that does not exist is.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if extra := fs.Args(); len(extra) > 0 {
		fmt.Fprintf(os.Stderr, "no — I do not know %q\n", extra[0])
		return 2
	}
	// The art, without a ledger: every screen drawn from the sample, and
	// nothing else happens.
	if *demo {
		if vault != "" || *oneShot != "" || *gateFD >= 0 || *state != "" {
			fmt.Fprintln(os.Stderr, "no — -demo draws the sample and opens no Rokh; name nothing else with it [refused]")
			return 2
		}
		if err := tui.Demo(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "no —", Plain(err))
			return 1
		}
		return 0
	}
	// A snapshot file is drawn as the screen would draw a ledger, and nothing
	// else happens: no folder, no passphrase, no ledger, nothing recorded.
	if *state != "" {
		if vault != "" || *oneShot != "" || *gateFD >= 0 {
			fmt.Fprintln(os.Stderr, "no — -state draws a snapshot file and opens no Rokh; name no folder, sentence or gate with it [refused]")
			return 2
		}
		f, err := os.Open(*state)
		if err != nil {
			fmt.Fprintln(os.Stderr, "no —", Plain(err))
			return 1
		}
		st, err := tui.ReadSnapshot(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, "no —", Plain(err))
			return 1
		}
		if err := tui.Picture(os.Stdin, os.Stdout, st, TerminalSignals); err != nil {
			fmt.Fprintln(os.Stderr, "no —", Plain(err))
			return 1
		}
		return 0
	}
	// A gate that started this program handed it a connection and named the
	// descriptor. That connection is already an identity, so there is no vault
	// to choose, no passphrase to ask for and no folder to make: the second
	// back end takes over the whole surface, and the flags that belong to the
	// first one are refused rather than silently ignored.
	//   — T4.1, T8.2
	fd, onGate, err := homeFD(*gateFD)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no —", err)
		return 2
	}
	if onGate {
		switch {
		case vault != "":
			fmt.Fprintf(os.Stderr, "no — a home comes through its gate, and %q is a folder; name neither\n", vault)
			return 2
		case *library != "" || *mount != "":
			fmt.Fprintln(os.Stderr, "no — a library and a seats folder belong to a vault; a home has neither")
			return 2
		}
		if openHome == nil {
			fmt.Fprintln(os.Stderr, "no — this caller supplied no home connection")
			return 2
		}
		stream, err := openHome(fd)
		if err != nil {
			fmt.Fprintln(os.Stderr, "no —", err)
			return 2
		}
		return runHome(stream, *oneShot, os.Stdin, os.Stdout, os.Stderr)
	}
	// A person at a terminal goes through the gate: the folder first, chosen
	// by them; then what is on it; then, and only then, a passphrase. Nothing
	// is chosen for them and nothing is made without their saying so.
	//
	// This used to ask for a passphrase before anything, and once fell back to
	// the working directory as the vault, so somebody who typed "rokh" in their
	// home folder had their home folder opened as a ledger. Rokh's whole claim
	// is that nothing is recorded unless a person says so, and picking their
	// vault for them is the same fault one layer down.
	//   — T4.1, T1.2, T8.2
	onTerminal := interactive(os.Stdin) && interactive(os.Stdout)
	var s *session
	if onTerminal && *oneShot == "" {
		var err error
		if s, err = enter(os.Stdout, vault, *mount, *library); err != nil {
			if errors.Is(err, errNothingChosen) {
				fmt.Fprintln(os.Stderr, err)
				return 0
			}
			fmt.Fprintln(os.Stderr, "no —", Plain(err))
			return 1
		}
	} else {
		// A script, a pipe, or one sentence: there is nobody to ask, so the
		// folder is named on the command line or nothing happens.
		if vault == "" {
			firstRun(os.Stderr, name)
			return 2
		}
		abs, err := absolute(vault)
		if err != nil {
			fmt.Fprintln(os.Stderr, "no —", Plain(err))
			return 1
		}
		pass, err := Passphrase()
		if err != nil {
			fmt.Fprintln(os.Stderr, "no —", Plain(err))
			return 1
		}
		s = sessionOn(abs, seatsFor(abs, *mount), *library, pass)
	}
	defer s.closeAll()

	if *oneShot != "" {
		if err := runLine(s, *oneShot, os.Stdout); err != nil {
			refused := s.plain(err)
			fmt.Fprintln(os.Stderr, "no —", refused)
			return exitFor(refused)
		}
		// A draft does not outlive the process, and -c is one sentence and
		// then the end of it. So a -c that leaves a sentence waiting is a -c
		// that lost it, and the reply it just printed says to type "write" —
		// which is the one thing this form cannot go on to do. It says so
		// rather than exiting quietly with nothing recorded.
		//   — T4.4, T8.2
		if len(s.drafts) > 0 {
			fmt.Fprintln(os.Stderr, "a sentence was left waiting to be closed, and ended with this command.")
			fmt.Fprintln(os.Stderr, `"-c" takes one sentence and that is all; the draft does not survive from one command to the next.`)
			fmt.Fprintln(os.Stderr, "Give both sentences in one session:")
			fmt.Fprintln(os.Stderr, `  printf 'write at {address}: {text}\nwrite\n' | rokh-shell VAULT`)
			return 1
		}
		return 0
	}

	// A person at a terminal gets the screen: the same sentences, drawn and
	// redrawn, with the ledger in view while they type. A pipe, a script or
	// -plain gets the line surface, which reads one sentence at a time and
	// prints one answer. They are one surface; only the drawing differs.
	if !*plain && onTerminal {
		err := runScreen(s)
		var noScreen tui.NoScreen
		var signalled tui.Signalled
		switch {
		case errors.As(err, &noScreen):
			// A terminal that cannot be drawn on gets the same sentences one
			// line at a time, and is told why in one sentence.
			fmt.Fprintf(os.Stderr, "%s, so this is the line surface: type a sentence and press Enter; \"?\" lists them, \"leave\" ends the conversation.\n", noScreen.Error())
			return runLines(s, os.Stdin, os.Stdout, os.Stderr, true)
		case errors.As(err, &signalled):
			if n := len(s.drafts); n > 0 {
				fmt.Fprintln(os.Stderr, letGo(n, s.unsure))
			}
			s.closeAll()
			fmt.Fprintf(os.Stderr, "%s; the terminal is as it was, and nothing was recorded by stopping.\n", signalled.Error())
			return 1
		case err != nil:
			fmt.Fprintln(os.Stderr, "no —", s.plain(err))
			return 1
		}
		if err := s.closeAll(); err != nil {
			fmt.Fprintln(os.Stderr, "no —", s.plain(err))
			return 1
		}
		if n := len(s.drafts); n > 0 {
			fmt.Fprintln(os.Stderr, letGo(n, s.unsure))
		}
		fmt.Println(tplClosed)
		return 0
	}
	return runLines(s, os.Stdin, os.Stdout, os.Stderr, interactive(os.Stdin))
}

// runLines is the line surface: one sentence read, one answer printed, the
// answers on out and everything else on notes. person says whether somebody
// is typing, rather than a script feeding it.
//
// The exit says how it went, as the home's surface does and the documents
// say: 0 when every sentence was answered, 1 when any was refused, and 4 when
// the ending of any recording is unknown. The worst of them stands.
func runLines(s *session, in io.Reader, out, notes io.Writer, person bool) int {
	s.notes = notes
	if person {
		greet(notes, s.vault)
	}
	code := 0
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for {
		// The prompt is for a person; a script reading the other end gets the
		// answers and nothing else.
		if person {
			fmt.Fprint(notes, "? ")
		}
		if !sc.Scan() {
			break
		}
		line := sc.Text()
		if line == "" {
			continue
		}
		if asking(line) {
			showSentences(notes)
			continue
		}
		// "leave" ends the conversation, and a sentence still waiting is named
		// before the parting line: it was never recorded.
		c, perr := parse(line)
		leaving := perr == nil && c.Op == opClose
		if leaving && len(s.drafts) > 0 {
			fmt.Fprintln(notes, letGo(len(s.drafts), s.unsure))
		}
		if err := runLine(s, line, out); err != nil {
			refused := s.plain(err)
			fmt.Fprintln(notes, "no —", refused)
			code = max(code, exitFor(refused))
			s.unsure = s.unsure || exitFor(refused) == 4
			continue
		}
		if leaving {
			return code
		}
	}
	// EOF closes like "leave", and says so the same way.
	if err := s.closeAll(); err != nil {
		fmt.Fprintln(notes, "no —", s.plain(err))
		return max(code, 1)
	}
	if n := len(s.drafts); n > 0 {
		fmt.Fprintln(notes, letGo(n, s.unsure))
	}
	fmt.Fprintln(out, tplClosed)
	return code
}

// exitFor is the exit of a refused sentence: 4 when whether anything was
// recorded is unknown, 1 otherwise.
func exitFor(err error) int {
	var r refusal
	if errors.As(err, &r) && r.record == "unknown" {
		return 4
	}
	return 1
}

func runLine(s *session, line string, out io.Writer) error {
	c, err := parse(line)
	if err != nil {
		if errors.Is(err, errNotASentence) {
			return nearMiss(line)
		}
		return err
	}
	reply, err := s.execute(c)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, reply)
	return nil
}

// firstRun is what a script or a pipe gets when it names no folder: there is
// nobody at the other end to ask, so the folder has to be on the command
// line. A person at a terminal never sees this; the gate asks them.
func firstRun(w io.Writer, name string) {
	fmt.Fprintf(w, `rokh — your event ledger. A Rokh is a folder that keeps your notes and files
sealed under a passphrase.

On a terminal, "%[1]s" alone asks which folder is your Rokh and opens it, or
makes one in an empty folder you choose. There is no terminal here to ask on,
so name the folder:

`, name)
	rows := [][2]string{
		{name + " PATH", "open the Rokh in that folder"},
		{name + ` PATH -c "SENTENCE"`, "one sentence, no screen"},
		{name + " -h", "the flags and the sentences"},
	}
	// The commands underneath are the rokh command's; another program that
	// carries this surface does not hand out a line that is not its own.
	if name == "rokh" {
		rows = append(rows, [2]string{"rokh help", "the commands underneath this surface"})
	}
	wide := 0
	for _, r := range rows {
		wide = max(wide, len(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", wide, r[0], r[1])
	}
}

// Usage prints the sentences a person may type.
func Usage() { UsageFor("rokh-shell") }

// UsageFor is Usage under whichever name was typed. One surface is carried by
// two commands, and help that names the other one hands somebody a line that
// is not theirs to run.
func UsageFor(name string) {
	fmt.Fprintf(os.Stderr, `%[1]s - the sentence surface of Rokh

  %[1]s                       on a terminal: asks which folder is your Rokh
  %[1]s [flags] [VAULT]
  %[1]s [flags] VAULT -c "<sentence>"

The vault is a plain folder: VAULT/ledgers/<name>/ are carriers. On a
terminal the folder is chosen first and the passphrase asked after; in a
script the folder is named and the passphrase comes from ROKH_PASSPHRASE_FILE.

Flags:
  -library PATH   the star's library; content lives at PATH/rokh/<anchor>/
  -mount PATH     the seats folder (default: VAULT/Mount if it exists)
  -c "SENTENCE"   run one sentence and exit
  -plain          the line surface even on a terminal, no screen
  -state FILE     draw the screen from a snapshot file (tui/sample.json is
                  one); no Rokh is opened and nothing is recorded
  -demo           draw every screen from the sample ledger, in colour and
                  plain, at this terminal's size; nothing is opened
  -home-fd N      speak to a Rokh home's gate on descriptor N

A gate that starts this program sets ROKH_GATE_FD, and then the same
sentences go to that home instead of a vault: no folder is named, no
passphrase is asked for, and the sentences the gate does not speak are
refused by name. "?" at that prompt lists the ones it does.

The sentences (case does not matter); those marked * record an event:

`, name)
	for _, sn := range tui.Sentences {
		mark := " "
		if sn.Records {
			mark = "*"
		}
		fmt.Fprintf(os.Stderr, "  %s %s\n", mark, sn.Say)
	}
	fmt.Fprint(os.Stderr, `
"write at {address}: {text}" draughts a sentence; the bare "write" closes
it, and a closed sentence is an event. There is no way back from that,
which is why the two are two sentences and why this list names both. The
full list with what each one does is "?" at the prompt.

Exit: 0 when every sentence was answered, 1 when any was refused, 4 when
whether a recording happened is unknown.
`)
}

// split lifts the vault out of the arguments and returns the rest, so that the
// folder may be written before or after the flags.
//
// Nearly every flag this surface has takes a value, so a bare "-x" consumes
// the next argument unless it was written as "-x=value" or is one of the few
// that stand alone — "rokh -plain VAULT" is the second line of this surface's
// own help, and without that exception the folder is eaten as -plain's value
// and the person is told their folder is not a sentence. Anything else is the
// vault, and only the first one is: a second would be a mistake worth naming
// rather than silently ignoring, and Run says so.
func split(args []string) (vault string, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i+1:]...)
			return vault, rest
		case strings.HasPrefix(a, "-"):
			rest = append(rest, a)
			if !strings.Contains(a, "=") && !standsAlone(a) && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
		case vault == "":
			vault = a
		default:
			rest = append(rest, a)
		}
	}
	return vault, rest
}

// standsAlone reports the flags that take no value. They are named here
// rather than guessed at, so that adding one is a decision somebody makes on
// purpose: -plain, and -demo, which draws the sample.
func standsAlone(a string) bool {
	switch strings.TrimLeft(a, "-") {
	case "plain", "demo":
		return true
	}
	return false
}
