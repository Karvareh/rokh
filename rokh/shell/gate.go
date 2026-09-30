package shell

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"rokh/key"
	"rokh/vessel"
)

// The gate: what happens between typing "rokh" and standing at a ledger.
//
// The order is the person's, not the machine's. First the folder — chosen by
// them, through the tool their system already has for choosing folders, or
// typed where there is none. Then a look at what is on it. Only then a
// passphrase, because a passphrase opens a Rokh, and until the folder is known
// there is nothing to open. A folder is known by what is on it and by nothing
// else: no path is guessed, no default is chosen, and a folder that holds no
// Rokh is not made into one without the person saying so, here, to that
// question.
//
// The look is quick and touches nothing: it reads names, and one small public
// file, and writes not a byte. A wrong passphrase is said to be wrong and asked
// for again; a folder that is neither a Rokh nor empty is named and the person
// chooses again. Nothing is recorded by any of this. The first event of a new
// Rokh is the one sentence that would have made it, and it is recorded only
// after the person has named the folder, chosen the passphrase twice and given
// the name.
//
//	— T4.1, T1.2, T8, T8.2

// folderKind is what a look at a folder finds.
type folderKind int

const (
	kindMissing    folderKind = iota // nothing is there
	kindNotAFolder                   // something is there and it is not a folder
	kindRokh                         // a vault: a ledgers folder with at least one carrier in it
	kindEmpty                        // a folder with nothing in it
	kindOther                        // a folder with other things in it, and no Rokh
)

// inspect looks at a folder and says what it is. It reads directory names and
// nothing else; it opens no carrier and writes nothing.
func inspect(dir string) folderKind {
	fi, err := os.Stat(dir)
	if err != nil {
		return kindMissing
	}
	if !fi.IsDir() {
		return kindNotAFolder
	}
	// Both shapes are a Rokh: a folder of ledgers, as the gate makes one,
	// and a carrier, as rokh init makes one.
	if isRokh(dir) || isCarrier(dir) {
		return kindRokh
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return kindOther
	}
	for _, e := range entries {
		// The Finder drops one of these into every folder it shows. A person
		// who made a new folder in the Finder to be their Rokh should not be
		// told it is not empty.
		if e.Name() == ".DS_Store" {
			continue
		}
		return kindOther
	}
	return kindEmpty
}

// isRokh is whether a folder is a vault: it has a ledgers folder holding at
// least one folder that carries a vessel. The vessel's head file is its
// public face and is meant to be recognized before any passphrase is known.
func isRokh(dir string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, LedgersFolder))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && isCarrier(filepath.Join(dir, LedgersFolder, e.Name())) {
			return true
		}
	}
	return false
}

// seatsFor is the seats folder: the one named, or the conventional Mount
// beside the vault if the person made one. Never created here.
func seatsFor(vault, mount string) string {
	if mount != "" {
		return mount
	}
	if fi, err := os.Stat(filepath.Join(vault, "Mount")); err == nil && fi.IsDir() {
		return filepath.Join(vault, "Mount")
	}
	return ""
}

// enter is the gate, from the first line a person sees to a session standing
// at their ledger. folder is the one named on the command line, or empty to
// ask. w is where the gate speaks, before any screen is drawn.
func enter(w io.Writer, folder, mount, library string) (*session, error) {
	named := folder != ""
	intro(w)
	for {
		if folder == "" {
			fmt.Fprintln(w, pickerPrompt+".")
			var err error
			if folder, err = chooseFolder(); err != nil {
				return nil, err
			}
		}
		abs, err := absolute(folder)
		if err != nil {
			return nil, err
		}
		switch inspect(abs) {
		case kindRokh:
			fmt.Fprintf(w, "%s — your Rokh.\n", abs)
			return unlock(w, abs, seatsFor(abs, mount), library)
		case kindEmpty:
			return offerToMake(w, abs, seatsFor(abs, mount), library, true)
		case kindMissing:
			return offerToMake(w, abs, seatsFor(abs, mount), library, false)
		case kindNotAFolder:
			return nil, fmt.Errorf("%s is not a folder", abs)
		}
		fmt.Fprintf(w, "%s is not a Rokh, and it is not empty.\n", abs)
		if named {
			return nil, errors.New("choose the folder that is your Rokh, or an empty one to make it in")
		}
		folder = ""
	}
}

// intro is said once, before the first question: what a Rokh is, that
// nothing is written until the last answer, and that a passphrase shows
// nothing while it is typed, so the terminal does not seem to have frozen.
func intro(w io.Writer) {
	fmt.Fprintln(w, "rokh — your event ledger.")
	fmt.Fprintln(w, "A Rokh is a folder that keeps your notes and files sealed under a passphrase.")
	fmt.Fprintln(w, "Nothing is written until the last question is answered, and nothing shows while you type a passphrase.")
}

// unlock asks for the passphrase of a Rokh that is there, and opens a session
// on it. A wrong passphrase is said to be wrong and asked for again, three
// times in all; a passphrase that came from the environment is tried once,
// because asking the environment again gets the same answer.
//
// When the vault holds one ledger it is opened and stood at, without a
// sentence: one ledger per person is the shape this surface nudges toward,
// and a person with one ledger has nothing to choose. With more than one, the
// passphrase is checked against the first and the choosing is left to them.
func unlock(w io.Writer, vault, mount, library string) (*session, error) {
	attempts := 3
	if os.Getenv("ROKH_PASSPHRASE_FILE") != "" || os.Getenv("ROKH_PASSPHRASE") != "" {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		if i == 0 && attempts > 1 {
			fmt.Fprintln(w, "(nothing shows while you type the passphrase)")
		}
		pass, err := Passphrase()
		if err != nil {
			return nil, err
		}
		s := sessionOn(vault, mount, library, pass)
		names := s.ledgerNames()
		if len(names) == 1 {
			err = s.openByName(names[0])
		} else {
			err = tryPass(s.ledgerDir(names[0]), pass)
		}
		if errors.Is(err, vessel.ErrLocked) || errors.Is(err, key.ErrPassphrase) {
			fmt.Fprintln(w, "that is not this Rokh's passphrase.")
			continue
		}
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	if attempts == 1 {
		// The passphrase came from a file or the environment and was tried
		// once; asking the environment again gets the same answer.
		return nil, Plain(vessel.ErrLocked)
	}
	return nil, errors.New("three times; nothing opened")
}

// tryPass opens a carrier only to learn whether the passphrase is its, and
// closes it again. Nothing is replayed and nothing is stood at.
func tryPass(dir, pass string) error {
	_, _, err := (&session{}).openCarrier(dir, pass)
	return err
}

// offerToMake asks whether to make a Rokh in an empty folder, or in a folder
// that is not there yet, and makes it when the answer is yes: a passphrase,
// the same passphrase again, and the name the Rokh will know the person by.
// Any other answer to the first question is no, and nothing is made.
func offerToMake(w io.Writer, vault, mount, library string, exists bool) (*session, error) {
	question := vault + " is empty. Make your Rokh here? [y/N] "
	if !exists {
		question = "there is no folder at " + vault + ". Make it, and your Rokh in it? [y/N] "
	}
	answer, err := askVisible(question)
	if err != nil {
		return nil, err
	}
	if !yes(answer) {
		return nil, errNothingChosen
	}
	room, err := askRoom(w, vault)
	if err != nil {
		return nil, err
	}
	pass, err := choosePassphrase(w)
	if err != nil {
		return nil, err
	}
	name, err := askName(w)
	if err != nil {
		return nil, err
	}
	s, err := makeRokhWith(vault, mount, library, pass, name, room)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(w, "made. Your Rokh is at %s; its first event is %s.\n", vault, s.current.led.Genesis().Short())
	// What "sealed" means here, said once, in what it actually is: the notes
	// and files are sealed and open to nobody without the passphrase; the
	// name given to the ledger is a folder's name, and the files' sizes are
	// there for anyone who looks at the folder. Nothing more is claimed.
	fmt.Fprintln(w, "Your notes and files in it are sealed; the name you gave it and the sizes of its files can be seen by anyone who sees the folder.")
	fmt.Fprintln(w, `Type "?" to see what you can say.`)
	return s, nil
}

// choosePassphrase asks for the new passphrase twice. Two that differ are
// asked for again, three times in all, because a slip of the finger is not a
// change of mind; the third mismatch makes nothing.
func choosePassphrase(w io.Writer) (string, error) {
	for try := 0; try < 3; try++ {
		pass, err := ask("choose a passphrase (nothing shows while you type; there is no recovery without it): ")
		if err != nil {
			if err.Error() == "no passphrase was given" {
				fmt.Fprintln(w, "a passphrase cannot be empty.")
				continue
			}
			return "", err
		}
		again, err := ask("the same passphrase again: ")
		if err != nil {
			return "", err
		}
		if pass == again {
			return pass, nil
		}
		fmt.Fprintln(w, "the two passphrases differ; type them again.")
	}
	return "", errors.New("the passphrases differed three times; nothing was made")
}

// askName asks for the ledger's name and holds it at once to the rule the
// sentences hold it to, so a name that could never be said again is refused
// before anything is made, with the reason, and asked for again.
func askName(w io.Writer) (string, error) {
	for {
		answer, err := askVisible("name this ledger, one word, no slash (it becomes a folder's name): ")
		if err != nil {
			return "", err
		}
		name := strings.TrimSpace(answer)
		why := nameProblem(name)
		if why == "" {
			return name, nil
		}
		fmt.Fprintln(w, why)
	}
}

// nameProblem says why a name cannot be a ledger's name, or nothing. A name
// is one word the parser takes back as that same name, and a folder's name
// that stays inside the ledgers folder.
func nameProblem(name string) string {
	switch {
	case name == "":
		return "a name is needed: one word, such as home."
	case strings.ContainsAny(name, " \t"):
		return "a name is one word: it cannot hold a space."
	case strings.ContainsAny(name, `/\`):
		return "a name cannot hold a slash: it becomes a folder's name."
	case strings.HasPrefix(name, "."):
		return "a name cannot begin with a dot."
	case len(name) > 64:
		return "a name is at most 64 bytes long."
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return "a name cannot hold a control character."
		}
	}
	if c, err := parse(newLedgerStem + name); err != nil || c.Op != opOpenNew || c.Name != name {
		return "that name cannot be said back in a sentence; choose one word of letters and digits."
	}
	return ""
}

// makeRokh makes a Rokh in a folder and returns a session standing at it:
// makeRokhWith with a vessel of room bytes, or of the vessel's own default
// when room is zero.
func makeRokh(vault, mount, library, pass, name string, room int64) (*session, error) {
	if room == 0 {
		room = defaultRoom
	}
	p, err := roomParams(room)
	if err != nil {
		return nil, err
	}
	return makeRokhWith(vault, mount, library, pass, name, p)
}

// makeRokhWith makes a Rokh in a folder, its vessel the size the person
// chose, and returns a session standing at it. The folder is made if it is
// not there. The ledger's first event is the one canonical sentence that
// would have made it at the prompt — the person's explicit act, in the
// surface's own words, with the name they gave — and nothing else: the size
// lives in the vessel, where it can be changed, and not in genesis, where it
// could not.
func makeRokhWith(vault, mount, library, pass, name string, room vessel.Params) (*session, error) {
	if name == "" {
		return nil, errors.New("no name was given; nothing was made")
	}
	if why := nameProblem(name); why != "" {
		return nil, fmt.Errorf("%q cannot be a name: %s", name, why)
	}
	if err := os.MkdirAll(vault, 0o700); err != nil {
		return nil, err
	}
	s := newSession(vault, mount, library, pass)
	err := withRoom(filepath.Join(s.ledgersDir(), name), room, func() error {
		_, err := s.initLedger(name, newLedgerStem+name)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// newLedgerStem is the canonical sentence that makes a ledger, up to its
// slot. The gate records it with the name filled in, and a test holds it to
// being the sentence the parser knows by that meaning.
const newLedgerStem = "open a new ledger named "

// yes is the one answer that means yes.
func yes(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}
