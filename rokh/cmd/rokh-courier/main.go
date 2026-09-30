// Command rokh-courier moves already-signed bytes between two ledgers.
//
// It is not part of Rokh. It has two halves and they are deliberately
// different in what they are trusted with:
//
//   - bundle: the *sender's* side. It reads the sender's own ledger, consults
//     the live covenants, and writes out only what may cross. This half needs
//     the carrier and its passphrase because it is the owner's own machine.
//
//   - apply: the *courier* proper. Keyless, untrusted, and dumb. It reads a
//     bundle file and offers each event to a target daemon's append. It cannot
//     sign, cannot grant authority, and cannot decide validity. If it lies,
//     drops, reorders or replays, nothing invalid is accepted: the receiving
//     ledger re-parses and re-verifies every byte and judges authority over
//     causal history.
//
// The split is the point. Disclosure is enforced where the keys and the
// covenants are, at bundling time. A courier is only ever handed what it may
// carry.
package main

import (
	"bufio"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"rokh/bundle"
	"rokh/covenant"
	"rokh/frame"
	"rokh/ledger"
	pp "rokh/passphrase"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "bundle":
		err = cmdBundle(os.Args[2:])
	case "open":
		err = cmdOpen(os.Args[2:])
	case "apply":
		err = cmdApply(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `rokh-courier - move signed bytes between ledgers

  rokh-courier bundle --carrier DIR --peer HEXKEY --out FILE
                      [--after ID] [--scope ADDR] [--with-ancestry]
  rokh-courier open   --dir DIR --in FILE --out FILE
  rokh-courier apply  --socket PATH --in FILE

bundle reads your ledger and your covenants, and writes only what may cross.
apply is keyless: it offers bytes to a daemon and reports what it decided.

Passphrase for bundle and open: --passphrase-file FILE ("-" for standard
input), then ROKH_PASSPHRASE_FILE, then ROKH_PASSPHRASE, then the terminal.
`)
}

// line is one event on its way. Exactly one of Raw and Box is set.
//
// Raw is the event's own bytes in hex — readable by anyone holding the file,
// which is the honest default: a courier is handed only what the covenant said
// may cross, and the covenant is where disclosure was decided.
//
// Box is the same bytes sealed to one named reader. It appears when the
// covenant carries a reading key, and then the courier carries something it
// cannot read. Eph is the ephemeral public key that seal needs to open it.
//
//	— T13.1, T7.4
type line struct {
	ID  string `json:"id"`
	Raw string `json:"raw,omitempty"`
	Eph string `json:"eph,omitempty"`
	Box string `json:"box,omitempty"`
}

// passphraseFile is --passphrase-file of the command being run.
var passphraseFile string

// passphrase follows the one rule of every command (contract section 6).
func passphrase() (string, error) { return pp.From(passphraseFile, os.Stdin) }

func cmdBundle(args []string) error {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	dir := fs.String("carrier", "", "carrier directory")
	peerHex := fs.String("peer", "", "recipient Ed25519 public key, hex")
	after := fs.String("after", "", "a head the recipient already has")
	scope := fs.String("scope", "", "narrow further than the covenants do")
	withAncestry := fs.Bool("with-ancestry", false,
		"also include ancestors the covenants do not name (discloses more than the covenant)")
	out := fs.String("out", "", "bundle file to write")
	fs.StringVar(&passphraseFile, "passphrase-file", "", "a file holding the passphrase, or - for standard input")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *peerHex == "" || *out == "" {
		return errors.New("--carrier, --peer and --out are required")
	}
	peer, err := hex.DecodeString(*peerHex)
	if err != nil || len(peer) != ed25519.PublicKeySize {
		return errors.New("--peer must be a 64-character hex Ed25519 public key")
	}
	pass, err := passphrase()
	if err != nil {
		return err
	}

	c, _, err := openOwner(*dir, pass)
	if err != nil {
		return err
	}
	gen, err := c.Get(c.Anchor())
	if err != nil {
		return err
	}
	// The ledger is what the references reach, not what lies in the object
	// store: an object left behind by a recording that stopped is not an
	// event, and a courier must not carry it as one.
	//   — T8.5
	refs, err := c.Refs()
	if err != nil {
		return err
	}
	heads := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		heads = append(heads, id)
	}
	l, err := ledger.Load(gen, c.Get, heads)
	if err != nil {
		return err
	}

	var afterID frame.ID
	if *after != "" {
		if afterID, err = frame.ParseID(*after); err != nil {
			return err
		}
	}

	sel, err := bundle.Disclosable(l, peer, afterID, *scope, *withAncestry)
	if err != nil {
		if errors.Is(err, bundle.ErrNoCovenant) {
			return fmt.Errorf("%w: run `rokh share` first. A covenant permits a transfer; nothing crosses without one", err)
		}
		return err
	}
	if len(sel.IDs) == 0 {
		return errors.New("no event may cross to this peer under the live covenants")
	}

	f, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%s: %w (refusing to overwrite)", *out, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	have := bundle.Have{
		Kind: bundle.KindHave, Ledger: l.Genesis().String(),
		After: *after, Scope: *scope,
	}
	for _, id := range sel.IDs {
		have.IDs = append(have.IDs, id.String())
	}
	if err := json.NewEncoder(w).Encode(have); err != nil {
		return err
	}
	// If the covenant names a reading key, everything in this bundle is
	// sealed to it, and the courier carries what it cannot read. If it names
	// none, the events travel as they are — which is not a lapse: the
	// covenant is where disclosure was decided, and a courier is only ever
	// handed what it may carry.
	//
	// It is never half and half. A bundle where some events are sealed and
	// some are not tells the courier exactly which ones were worth sealing.
	//   — T13.1, T7.4
	// Only this recipient's covenants. They were not narrowed before, so a
	// bundle for one peer could be sealed to a reading key another peer's
	// covenant named — unreadable by the person it was addressed to, and shut
	// to the wrong reader. Whose covenant it is decides whose key it is.
	//   — T7.1, T13.1
	var covs []covenant.Covenant
	for _, c := range covenant.ActiveAt(l) {
		if c.For(peer) {
			covs = append(covs, c)
		}
	}
	// Asked of the selection rather than of the typed scope: a covenant that
	// names one room covers no address, so a scope would find no key and the
	// bundle would leave in the clear.
	reading, sealed := bundle.ReadingKeyForSelection(l, covs, sel)

	total := 0
	if sealed {
		boxes, err := bundle.SealFor(l, sel, reading)
		if err != nil {
			return err
		}
		for i, id := range sel.IDs {
			e, _ := l.Get(id)
			total += len(e.Raw)
			if err := json.NewEncoder(w).Encode(line{
				ID:  id.String(),
				Eph: hex.EncodeToString(boxes[i].Ephemeral),
				Box: hex.EncodeToString(boxes[i].Box),
			}); err != nil {
				return err
			}
		}
	} else {
		for _, id := range sel.IDs {
			e, _ := l.Get(id)
			total += len(e.Raw)
			if err := json.NewEncoder(w).Encode(line{
				ID: id.String(), Raw: hex.EncodeToString(e.Raw),
			}); err != nil {
				return err
			}
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	how := "in the clear"
	if sealed {
		how = "sealed to the reading key the covenant names"
	}
	fmt.Printf("bundled %d event(s), %d bytes, under %d live covenant(s), %s\n",
		len(sel.IDs), total, len(covs), how)
	if len(sel.Missing) > 0 {
		fmt.Fprintf(os.Stderr, "\nwarning: %d ancestor(s) are not covered by any covenant.\n", len(sel.Missing))
		fmt.Fprintln(os.Stderr, "The recipient will hold the dependent events as pending, which means")
		fmt.Fprintln(os.Stderr, "\"the ancestry has not arrived\", not \"the event is bad\".")
		fmt.Fprintln(os.Stderr, "This is the open tension in docs/07 section 11: a scope narrower than")
		fmt.Fprintln(os.Stderr, "the authority chain cannot be verified on its own. --with-ancestry")
		fmt.Fprintln(os.Stderr, "would send them, but it discloses more than the covenant names.")
	}
	return nil
}

func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	socket := fs.String("socket", "", "target daemon socket")
	in := fs.String("in", "", "bundle file to apply")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socket == "" || *in == "" {
		return errors.New("--socket and --in are required")
	}
	f, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer f.Close()

	conn, err := net.Dial("unix", *socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	ask := func(req map[string]any) (map[string]any, error) {
		b, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		if _, err := rw.Write(append(b, '\n')); err != nil {
			return nil, err
		}
		if err := rw.Flush(); err != nil {
			return nil, err
		}
		resp, err := rw.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var out map[string]any
		return out, json.Unmarshal(resp, &out)
	}

	st, err := ask(map[string]any{"op": "status"})
	if err != nil {
		return err
	}
	target, _ := st["anchor"].(string)

	ack := bundle.Ack{Kind: bundle.KindAck, Ledger: target,
		Accepted: []string{}, Rejected: []string{}, Pending: []string{}}

	// Every line is read before any is offered, so that an event whose parent
	// comes later in the file still lands.
	//
	// A courier may reorder — the design says so and says nothing invalid gets
	// accepted as a result — but reordering was *losing* events, not merely
	// shuffling them: an event offered before its parent comes back pending,
	// and pending is not stored, and nothing offered it again. So the offering
	// goes round until a pass changes nothing. Being dumb is the courier's
	// job; being forgetful is not.
	//   — T5.1, T8.5
	var lines []line
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	first := true
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		if first {
			first = false
			var h bundle.Have
			if err := json.Unmarshal([]byte(raw), &h); err == nil && h.Kind == bundle.KindHave {
				fmt.Printf("bundle offers %d event(s) from ledger %s\n", len(h.IDs), short(h.Ledger))
				// Every event is carrier-bound: it names the anchor of the
				// ledger it belongs to. A ledger therefore cannot accept
				// another ledger's events, and saying so now is kinder than
				// letting every line be rejected in silence.
				if target != "" && h.Ledger != "" && h.Ledger != target {
					fmt.Fprintf(os.Stderr, "\nwarning: this bundle is from ledger %s but the target is %s.\n",
						short(h.Ledger), short(target))
					fmt.Fprintln(os.Stderr, "Every event names its own ledger's anchor, so all of these will be")
					fmt.Fprintln(os.Stderr, "rejected. Holding another person's ledger needs a mirror - a separate")
					fmt.Fprintln(os.Stderr, "carrier verified against their anchor - which is not built and needs a")
					fmt.Fprintln(os.Stderr, "ruling. See docs/07 section 11.")
				}
				continue
			}
		}
		var ln line
		if err := json.Unmarshal([]byte(raw), &ln); err != nil {
			return fmt.Errorf("bad bundle line: %w", err)
		}
		lines = append(lines, ln)
	}
	if err := sc.Err(); err != nil {
		return err
	}

	for _, ln := range lines {
		if ln.Box != "" {
			// Sealed, and this half has no key — by design. Refusing is the
			// only honest answer: offering an empty "raw" would have the
			// daemon reject every line and the person would read that as
			// their ledger refusing the events, when in fact nothing was
			// ever offered to it.
			//   — T13.1
			return fmt.Errorf("this bundle is sealed to a reading key; open it "+
				"first on the machine that holds that key:\n  rokh-courier open "+
				"--dir YOURCARRIER --in %s --out opened.bundle", *in)
		}
		break
	}

	// Round after round until a pass accepts nothing new. Each round is one
	// pass over what is still waiting; an event whose parent landed in an
	// earlier round becomes acceptable in a later one.
	waiting := lines
	for len(waiting) > 0 {
		var again []line
		progress := false
		for _, ln := range waiting {
			// The courier does not judge. It offers, and reports what was
			// decided.
			resp, err := ask(map[string]any{"op": "append", "raw": ln.Raw})
			if err != nil {
				return err
			}
			if ok, _ := resp["ok"].(bool); ok {
				ack.Accepted = append(ack.Accepted, ln.ID)
				progress = true
				continue
			}
			// The daemon names the reason in a code; the sentence beside it
			// is for people. An older door without the code still says the
			// word in its sentence.
			code, _ := resp["code"].(string)
			msg, _ := resp["error"].(string)
			if code == "ancestry_pending" || (code == "" && strings.Contains(msg, "pending")) {
				again = append(again, ln)
			} else {
				ack.Rejected = append(ack.Rejected, ln.ID)
			}
		}
		if !progress {
			// Nothing moved, so nothing will. Whatever is left is waiting on
			// ancestry that is not in this bundle, which is a real answer and
			// not a failure: "the ancestry has not arrived" is different from
			// "the event is bad".
			for _, ln := range again {
				ack.Pending = append(ack.Pending, ln.ID)
			}
			break
		}
		waiting = again
	}

	out, err := json.Marshal(ack)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	fmt.Fprintf(os.Stderr, "%d accepted, %d rejected, %d pending\n",
		len(ack.Accepted), len(ack.Rejected), len(ack.Pending))
	if len(ack.Pending) > 0 {
		fmt.Fprintln(os.Stderr, "pending means the ancestry has not arrived, not that the event is bad")
	}
	return nil
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
