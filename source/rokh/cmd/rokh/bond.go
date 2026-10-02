package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"rokh/bond"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// The commands that reach package bond.
//
// Two people cannot make one shared event. An event has one author and carries
// its own ledger's anchor, so there is nothing for two of them to sign. What
// they can make is one founding leaf, a byte string whose name is the hash of
// exactly those bytes, which each of them then accepts in their own ledger.
// Two events, one name between them.
//
//	— T11.6, T11.5
//
// So the three commands split along that seam. Making a leaf records nothing
// and needs no key: both people compute the same bytes for themselves.
// Accepting one records an event, in the caller's own ledger and in no other.
// Showing a leaf's standing reads one ledger, which is the only one this
// program has, and says so rather than reporting silence as refusal.
//
//	— T11.7, T13.6

// bondAddress is where an acceptance is written.
//
// The verb is package bond's own — bond.VerbAccept — and the address is the
// same namespace, the way a covenant's peer.share sits at "peer". Both are
// ordinary and non-reserved: the core neither knows what a bond is nor
// privileges one, and every rule it applies to this event is the rule it
// applies to any other.
//
//	— T11.10, T11.3
const bondAddress = "bond"

// cmdBond dispatches the three. They are one family and one word, because the
// leaf, the acceptance and the standing are three views of a single thing.
func cmdBond(args []string) error {
	verbs := map[string]func([]string) error{
		"leaf":     cmdBondLeaf,
		"accept":   cmdBondAccept,
		"standing": cmdBondStanding,
	}
	if len(args) == 0 {
		return errors.New("say which: leaf, accept or standing")
	}
	fn, found := verbs[args[0]]
	if !found {
		return fmt.Errorf("I do not know %q; leaf, accept or standing", args[0])
	}
	return fn(args[1:])
}

// ---------- the leaf ----------

// cmdBondLeaf builds the founding leaf and records nothing.
//
// That is not an omission. The leaf is not an event: it is the byte string
// both people can arrive at without asking each other, and its name is the
// hash of those bytes like everything else in Rokh. Recording it here would
// put one person's ledger in the middle of a thing that has no middle.
//
//	— T11.6, T3.2
func cmdBondLeaf(args []string) error {
	fs, dir, err := newFlags("bond leaf", args)
	if err != nil {
		return err
	}
	with := fs.String("with", "", "the other founders' anchors, comma-separated hex ids")
	doing := fs.String("doing", "", "the work undertaken")
	out := fs.String("out", "", "file to write the leaf's bytes to")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*doing) == "" {
		return errors.New("--doing is required: the leaf says what is being taken on")
	}
	if *out == "" {
		return errors.New("--out is required: the leaf's bytes have to survive to reach anyone else")
	}
	// The carrier is opened for one thing: this ledger's anchor. Each person
	// brings their own, and a bond made of one anchor twice is a shared root
	// key wearing a bond's clothes.
	//   — T11.8
	sess, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer sess.Close()

	mine := sess.car.Anchor()
	founders, err := bondAnchors(mine, *with)
	if err != nil {
		return err
	}
	raw, err := bond.Leaf{Kind: bond.Founding, Founders: founders, Doing: *doing}.Encode()
	if err != nil {
		return err
	}
	// Read back what was just written, so what is printed and what is filed
	// are the same bytes read the same way the other person will read them.
	//   — N4.1
	leaf, err := bond.Parse(raw)
	if err != nil {
		return err
	}
	if err := bondWriteLeaf(*out, raw); err != nil {
		return err
	}
	fmt.Println(string(raw))

	fmt.Fprintf(os.Stderr, "leaf name   %s\n", bond.Name(raw))
	fmt.Fprintln(os.Stderr, "anchors")
	for _, f := range leaf.Founders {
		if f == mine {
			fmt.Fprintf(os.Stderr, "  %s  ← this ledger\n", f)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s\n", f)
	}
	fmt.Fprintf(os.Stderr, "the work     %s\n", leaf.Doing)
	fmt.Fprintf(os.Stderr, "bytes        %s\n", *out)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Nothing was recorded. A leaf is not an event; it is a run of bytes")
	fmt.Fprintln(os.Stderr, "that each person builds for themselves and arrives at the same one")
	fmt.Fprintln(os.Stderr, "name. Get the leaf to the other person, and each of you accepts it")
	fmt.Fprintln(os.Stderr, "in your own ledger:")
	fmt.Fprintf(os.Stderr, "  rokh bond accept DIR --leaf %s --place «…»\n", *out)
	return nil
}

// bondAnchors reads the founders: this ledger's anchor, and the ones named.
//
// The caller's own anchor is always in. A leaf a person makes and is not named
// in is one they cannot accept, and building it would only be a way to get a
// name for somebody else's bond.
//
//	— T11.6
func bondAnchors(mine frame.ID, with string) ([]frame.ID, error) {
	out := []frame.ID{mine}
	for _, s := range strings.Split(with, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := frame.ParseID(s)
		if err != nil {
			return nil, fmt.Errorf("«--with»: %w", err)
		}
		out = append(out, id)
	}
	return out, nil
}

// bondWriteLeaf files the leaf's bytes and will not overwrite a different leaf.
//
// The bytes are a function of the anchors and the undertaking, so running the
// same command twice writes the same file and that is harmless. A file holding
// *other* bytes is another leaf, and replacing it silently would destroy the
// only copy of a name another ledger may already have accepted.
//
//	— T11.6
func bondWriteLeaf(path string, raw []byte) error {
	filed := append(append([]byte(nil), raw...), '\n')
	if old, err := os.ReadFile(path); err == nil {
		if bytes.Equal(old, filed) || bytes.Equal(old, raw) {
			return nil
		}
		return fmt.Errorf("%s is a different leaf; I will not write over it", path)
	}
	return os.WriteFile(path, filed, 0o600)
}

// bondReadLeaf reads a leaf back from a file.
//
// bond.Parse refuses a second spelling of the same content, so a leaf that
// arrived re-indented or re-ordered is refused here rather than accepted under
// a name nobody else computes.
//
//	— N4.1
func bondReadLeaf(path string) (bond.Leaf, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return bond.Leaf{}, err
	}
	// Only the trailing newline this program files it with. Canonical bytes
	// never end in one, so trimming it takes nothing away.
	return bond.Parse(bytes.TrimRight(b, "\r\n"))
}

// ---------- accepting ----------

// cmdBondAccept records, in this ledger and no other, that this person accepts
// that leaf and their own place in it.
//
// Both halves are needed. A leaf says who does what, so accepting one without
// saying which part is yours is agreement with no undertaking behind it.
//
// Only the person accepts, so only the person's own key signs one — and the
// cold path is --root-key, exactly as it is for share and unshare, which are
// the ledger's other owner-only acts.
//
// It took --key NAME before, which handed a write-delegate the pen for an
// undertaking made in someone else's name. Taking a thing on is not a kind of
// writing, and a right to write must not turn into a right to act for another.
// It also left a cold carrier with no way through at all: with the root key on
// separate media there was no --root-key here, so delegating was the only path
// that worked, and the one act that must be the owner's was the one act only a
// delegate could perform.
//
//	— T11.6, T11.7, T11.8
func cmdBondAccept(args []string) error {
	fs, dir, err := newFlags("bond accept", args)
	if err != nil {
		return err
	}
	leafFile := fs.String("leaf", "", "file holding the leaf's bytes")
	place := fs.String("place", "", "what you take on in it, in your own words")
	rootKey := fs.String("root-key", "", "root key file, for a cold carrier")
	branch := fs.String("branch", defaultBranch, "branch")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if *leafFile == "" {
		return errors.New("--leaf is required: the file holding the leaf's bytes")
	}
	leaf, err := bondReadLeaf(*leafFile)
	if err != nil {
		return err
	}
	s, err := openWriting(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	payload, name, err := bond.Accept(leaf, s.car.Anchor(), *place)
	if err != nil {
		switch {
		case errors.Is(err, bond.ErrNotAFounder):
			return fmt.Errorf("this leaf does not name this ledger's anchor; accepting it is somebody else's act: %w", err)
		case errors.Is(err, bond.ErrNoPlace):
			return fmt.Errorf("--place is required: accepting a leaf without saying your own place commits to nothing: %w", err)
		}
		return err
	}
	priv, authority, err := s.signer(keyRootName, *rootKey)
	if err != nil {
		return err
	}
	parents, err := s.branchHead(*branch)
	if err != nil {
		return err
	}
	anchor := s.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Authority: authority, Parents: parents,
		Address: bondAddress, Verb: bond.VerbAccept, Payload: payload,
		Attest: stamp(),
	}, priv)
	if err != nil {
		return err
	}
	if err := s.commit(e, *branch); err != nil {
		return err
	}
	fmt.Printf("accepted  %s\n", e.ID.Short())
	fmt.Printf("leaf      %s\n", name)
	fmt.Printf("place     %s\n", *place)
	fmt.Fprintln(os.Stderr, "\nThis is in your ledger and was written in nobody else's; there is no")
	fmt.Fprintln(os.Stderr, "ledger of the bond either. Ledgers are never made one.")
	return nil
}

// ---------- standing ----------

// bondHereOnly is the several ledgers a bond spans, seen from inside exactly one
// of them.
//
// bond.Ledgers is the interface for reading the others, and this program holds
// one carrier. So it answers for its own anchor out of its own events, and for
// every other anchor it answers what the interface already documents an
// unreachable ledger as: not seen. That is the honest implementation, and it is
// also the whole of what a correct one could do from here.
//
// The danger is entirely on the printing side — "not seen" read out as "has not
// accepted" would be this command inventing testimony about a ledger it has
// never opened. cmdBondStanding keeps the two apart, which is why it does not
// print the closed flag at all.
//
//	— T13.6, T11.6
type bondHereOnly struct {
	anchor frame.ID
	led    *ledger.Ledger
}

// mine reports whether an event is this person's own acceptance: written by
// them, at the address an acceptance is written at, under the accepting verb.
//
// All three, and none of them is decoration.
//
// The author, because "bond" is an ordinary address and a write-delegate
// holding a broad scope could otherwise author an acceptance that this command
// then reported as the person's own act. A right to write would have become a
// right to act on another's behalf, and keeping those two apart is the whole
// of the demand. Package covenant refuses a delegate's covenant for this
// reason already; the same reason applies here and was not being applied.
//
// The address, because a verb is not a place. An acceptance-shaped payload
// written under this verb at any address at all was counted, which left the
// address this program declares acceptances at declared and never read. An
// event is recognised by address, verb and payload together; a reader that
// checks two of the three can be answered by an event that is somewhere else.
//
//	— T11.8, T11.6, T10.6
func (h bondHereOnly) mine(e event.Event) bool {
	return e.Address == bondAddress &&
		e.Verb == bond.VerbAccept &&
		bytes.Equal(e.Author, h.led.Root())
}

// Accepted reads this ledger's own acceptance of a leaf, and refuses to guess
// at anyone else's.
//
// The first acceptance in causal order is the acceptance. Changing a leaf is
// its own act, separate from joining, and there is no command for it here — so
// a later event is not quietly treated as an amendment of an earlier one.
//
//	— T11.7
func (h bondHereOnly) Accepted(anchor, leaf frame.ID) (bond.Acceptance, bool) {
	if anchor != h.anchor {
		return bond.Acceptance{}, false
	}
	for _, id := range h.led.Order() {
		s, ok := h.led.Get(id)
		if !ok || !h.mine(s.Event) {
			continue
		}
		a, err := bond.ReadAcceptance(s.Event.Payload)
		if err != nil || a.Leaf != leaf {
			continue
		}
		return a, true
	}
	return bond.Acceptance{}, false
}

// cmdBondStanding says how a leaf stands as far as this one ledger can see.
//
// Which is not far, and the output says so in as many words. A leaf is closed
// when everyone it names has referred to that same name in their own ledger,
// and the only way to establish that is to read each of those ledgers where it
// is kept. This command has one.
//
//	— T11.7, T13.6
func cmdBondStanding(args []string) error {
	fs, dir, err := newFlags("bond standing", args)
	if err != nil {
		return err
	}
	leafFile := fs.String("leaf", "", "file holding the leaf's bytes")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if *leafFile == "" {
		return errors.New("--leaf is required: the file holding the leaf's bytes")
	}
	leaf, err := bondReadLeaf(*leafFile)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	view := bondHereOnly{anchor: s.car.Anchor(), led: s.led}
	said, err := bond.Places(leaf, view)
	if err != nil {
		return err
	}
	// The closed flag is deliberately dropped. From inside one ledger it can
	// only ever be false, and a false that means "the others are out of reach"
	// printed where a true would mean "everyone accepted" is a lie in the shape
	// of a field.
	//   — T11.7, T13.6
	unseen, _, err := bond.Settled(leaf, view)
	if err != nil {
		return err
	}
	notSeen := map[frame.ID]bool{}
	for _, f := range unseen {
		notSeen[f] = true
	}
	raw, err := leaf.Encode()
	if err != nil {
		return err
	}
	founders := append([]frame.ID(nil), leaf.Founders...)
	sort.Slice(founders, func(i, j int) bool { return founders[i].Compare(founders[j]) < 0 })
	iAmNamed := false
	for _, f := range founders {
		if f == view.anchor {
			iAmNamed = true
		}
	}

	fmt.Printf("leaf  %s\n", bond.Name(raw))
	fmt.Printf("work  %s\n", leaf.Doing)
	fmt.Println()
	fmt.Println("What this ledger sees, and no more than this:")
	for _, f := range founders {
		switch {
		case f != view.anchor:
			fmt.Printf("  %s  another ledger · not readable from here\n", f)
		case notSeen[f]:
			fmt.Printf("  %s  this ledger · has not accepted yet\n", f)
		default:
			fmt.Printf("  %s  this ledger · accepted, with the place \"%s\"\n", f, said[f])
		}
	}
	fmt.Println()
	if !iAmNamed {
		fmt.Println("This ledger's anchor is not in this leaf; none of these places is readable here.")
	}
	fmt.Println("\"Not readable\" means not readable, not that they have not accepted.")
	fmt.Println("The work is closed when each person has named this same leaf in their")
	fmt.Println("own ledger, and that has to be read in their ledger. This command has")
	fmt.Println("nobody's ledger but yours. Mutual acceptance is this and only this:")
	fmt.Println("no effect outside the ledgers, and no ruling.")
	return nil
}
