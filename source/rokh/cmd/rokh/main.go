// Command rokh is the terminal interface to a Rokh ledger.
//
// It is a thin shell. The core is the reference; everything here is a call
// into frame, event, carrier, ledger and announce. Every command works
// directly on the carrier, so the daemon is optional and nothing depends on
// it being up.
//
// Nothing in this program creates an event on its own. Every event is the
// result of one explicit command typed by a person. There is no watcher, no
// timer and no implicit commit.
//
// The passphrase comes from --passphrase-file, then ROKH_PASSPHRASE, and
// otherwise from the terminal with the echo off. That is the same rite the
// sentence surface uses, because the plumbing commands are typed by a person
// too — "rokh init" most of all.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"rokh/announce"
	"rokh/carrier"
	"rokh/covenant"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
	"rokh/ledger"
	"rokh/lineage"
	"rokh/medium"
	"rokh/oracle"
	pp "rokh/passphrase"
	"rokh/shell"
	"rokh/size"
	"rokh/transport"
	"rokh/turn"
	"rokh/vessel"
)

const defaultBranch = "main"

// v1Commands are the commands of v1 that live in their own files: each file
// adds its commands to this map in an init function.
var v1Commands = map[string]func([]string) error{}

func main() {
	shell.TerminalSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
	// Bare "rokh" opens the sentence surface: the gate, then the screen.
	//
	// This is the whole of what a person installed. The subcommands below are
	// the plumbing — they exist for programs and for the awkward jobs, and
	// somebody who types the name of the thing they installed should get the
	// thing, not a page telling them which second program to run.
	if len(os.Args) < 2 {
		os.Exit(shell.RunWithHome("rokh", nil, inheritedHomeStream))
	}
	cmds := map[string]func([]string) error{
		"home":      cmdHome,
		"init":      cmdInit,
		"write":     cmdWrite,
		"grant":     cmdGrant,
		"revoke":    cmdRevoke,
		"merge":     cmdMerge,
		"log":       cmdLog,
		"verify":    cmdVerify,
		"branch":    cmdBranch,
		"keys":      cmdKeys,
		"share":     cmdShare,
		"unshare":   cmdUnshare,
		"covenants": cmdCovenants,
		"bond":      cmdBond,
		"bind":      cmdBind,
		"bound":     cmdBound,
		"announce":  cmdAnnounce,
		"show":      cmdShow,
		"daemon":    cmdDaemon,
		"attempt":   cmdAttempt,
		"version":   cmdVersion,
	}
	// The two spellings a person actually types.
	for _, spelling := range []string{"--version", "-version", "-v"} {
		cmds[spelling] = cmdVersion
	}
	for _, spelling := range []string{"--help", "-h", "help"} {
		cmds[spelling] = func([]string) error { usage(); return nil }
	}
	// Commands of v1 register themselves here from their own files, so that
	// several writers never share one file (contract; packet C.3).
	for name, fn := range v1Commands {
		if _, taken := cmds[name]; !taken {
			cmds[name] = fn
		}
	}
	fn, found := cmds[os.Args[1]]
	if !found {
		// Not a subcommand. If it names a folder, or is a flag the surface
		// knows, the person is talking to the surface — "rokh ~/vault" and
		// "rokh -c ..." both mean the sentence surface, which is what the
		// name of this program is for.
		//
		// A bare word that is not a subcommand is *not* treated as a folder.
		// Otherwise a mistyped subcommand would quietly open a session
		// somewhere instead of saying it was mistyped, and a person would be
		// told nothing at the one moment they needed telling. A path is
		// another matter, and openable says which is which.
		if openable(os.Args[1]) {
			os.Exit(shell.RunWithHome("rokh", os.Args[1:], inheritedHomeStream))
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err := fn(os.Args[2:]); err != nil {
		// A subcommand's -h already printed its flags. Asking what something
		// does is not an error and does not exit like one.
		if errors.Is(err, errAskedForHelp) || errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		var st exitStatus
		if errors.As(err, &st) {
			os.Exit(st.code)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `rokh - an individual's event ledger: a graph of addressable data

  rokh home ...              open the encrypted home and its scoped program gate
  rokh VAULT                 the sentence surface, which is what this is
                             for. Everything below is plumbing.

  rokh init     DIR --message TEXT [--cold KEYFILE] [--size 64M] [--slab 1M] [--growth fixed|auto:STEP:MAX]
  rokh write    DIR --address ADDR [--verb V] [--message TEXT] [--key NAME] [--branch B]
                    [--attempt NAME] [--expect-heads H1,H2] [--json]
  rokh attempt  DIR --attempt NAME [--key NAME | --author HEX] [--json]
  rokh key add    DIR --name NAME --reads A,B [--write --scope ADDR] [--key-passphrase-file F] [--attempt NAME]
  rokh key list   DIR
  rokh key show   DIR --key ID|NAME
  rokh key revoke DIR --key ID|NAME [--attempt NAME]
  rokh key rotate DIR --key ID|NAME [--key-passphrase-file F] [--attempt NAME]
  rokh seed      SRC DST [--scope A,B] [--size 64M] [--slab 1M] [--attempt NAME]
  rokh reconcile DIR OTHER [--merge]
  rokh grow      DIR --to SIZE
  rokh shrink    DIR --to SIZE [--finish]
  rokh view      DIR
  rokh grant    DIR --to NAME [--scope ADDR] [--verbs a,b] [--delegate] [--root-key FILE]
  rokh revoke   DIR --target ID [--root-key FILE]
  rokh merge    DIR --branch A --from B [--key NAME] [--root-key FILE]
  rokh log      DIR [--long] [--json]
  rokh verify   DIR
  rokh branch   DIR [--create NAME --at ID]
  rokh keys     DIR [--public] [--reading] [--export NAME --out FILE] [--drop NAME]
  rokh share     DIR --to HEXKEY [--scope ADDR | --room ID] [--seal-to HEXKEY] [--root-key FILE]
  rokh unshare   DIR --target ID [--root-key FILE]
  rokh covenants DIR [--at ID]
  rokh bond leaf     DIR --with ANCHORS --doing TEXT --out FILE
  rokh bond accept   DIR --leaf FILE --place TEXT [--branch B] [--root-key FILE]
  rokh bond standing DIR --leaf FILE
  rokh bind      --socket PATH --namespace NS --version V --can VERB[,VERB]
                 --unknown refuse|ignore --repeat idempotent|duplicates
                 --retry may-retry|never-retry --compensate compensable|irreversible
                 --ending close-unknown|ask-a-person
  rokh bound     --socket PATH
  rokh announce DIR
  rokh show     DIR
  rokh daemon   DIR (--socket PATH | --listen unix:PATH|tcp:127.0.0.1:PORT | --stdio)
                    [--read-only] [--no-sign] [--no-root]     speaks rokh.booth/1
  rokh version

bind and bound speak to a running daemon, not to a carrier. All eight
declarations are required and not one of them has a default.

Passphrase: --passphrase-file FILE ("-" for stdin), then ROKH_PASSPHRASE,
otherwise asked for on the terminal. ROKH_PASSPHRASE_FILE is read too, and
means what --passphrase-file means.

An event is only ever created by one of these commands. Nothing is recorded
until you record it.
`)
}

// ---------- helpers ----------

// errAskedForHelp is how a subcommand's -h leaves. Asking what something does
// is not a failure, so main prints nothing more and exits zero.
var errAskedForHelp = errors.New("asked for help")

// asked holds the flag sets whose caller typed -h before naming a directory.
//
// The answer cannot be given where the question is noticed. A subcommand
// declares its own flags after this function returns, so at the moment "-h" is
// seen the set holds only --passphrase-file, and printing then would list one
// flag out of eight. It is printed at the next point every subcommand passes
// through with its flags declared, which is passphrase below.
var asked = map[*flag.FlagSet]bool{}

func newFlags(name string, args []string) (*flag.FlagSet, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.String("passphrase-file", "", "file holding the passphrase, or - for stdin")
	// A person asking a subcommand what its flags are is answered, rather than
	// told to give a carrier directory first. Every subcommand's -h used to
	// return "give the carrier directory", which is not an answer to the
	// question and left the flags reachable from nowhere.
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			asked[fs] = true
			return fs, "", nil
		}
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return nil, "", errors.New("give the carrier directory")
	}
	return fs, args[0], nil
}

func passphrase(fs *flag.FlagSet) (string, error) {
	// Every subcommand reaches here once, immediately after declaring and
	// parsing its own flags, and that makes it the first place a -h typed
	// before the directory can be answered in full. See newFlags.
	if asked[fs] {
		delete(asked, fs)
		fmt.Fprintf(os.Stderr, "rokh %s DIR [flags]\n\n", fs.Name())
		fs.SetOutput(os.Stderr)
		fs.PrintDefaults()
		return "", errAskedForHelp
	}
	// One rule for every command (contract section 6): --passphrase-file,
	// then ROKH_PASSPHRASE_FILE, then ROKH_PASSPHRASE, then the terminal.
	return shell.PassphraseFrom(fs.Lookup("passphrase-file").Value.String(), os.Stdin)
}

// keyFile is the on-disk form of an exported private key. It is
// self-describing because it may sit on cold media for years.
type keyFile struct {
	Version int    `json:"rokh_key"`
	Type    string `json:"type"`
	Key     string `json:"key"`
	Public  string `json:"public"`
}

func writeKeyFile(path string, priv ed25519.PrivateKey) error {
	pub := priv.Public().(ed25519.PublicKey)
	b, err := json.MarshalIndent(keyFile{
		Version: 1, Type: "ed25519-private",
		Key:    hex.EncodeToString(priv),
		Public: hex.EncodeToString(pub),
	}, "", " ")
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a key file", path)
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func readKeyFile(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var kf keyFile
	if err := json.Unmarshal(b, &kf); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if kf.Version != 1 || kf.Type != "ed25519-private" {
		return nil, fmt.Errorf("%s: unknown key file", path)
	}
	raw, err := hex.DecodeString(kf.Key)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s: bad key material", path)
	}
	return ed25519.PrivateKey(raw), nil
}

type session struct {
	car *carrier.Carrier
	led *ledger.Ledger
	// sec is the secret the passphrase opened: the owner's (the root's
	// signing seed, zero in cold custody, and the owner's reader), or a key's
	// own (its signing seed, zero when it cannot write, and its reader).
	sec key.Secret
	// lock is the writer's turn, for a session opened to write.
	lock *turn.Lock
	// layer is the key layer the session was opened with; a key's holds the
	// system reader its own add seals to it.
	layer *keyLayer
}

// keyRootName is the name the owner's signing key is asked for by.
const keyRootName = "root"

// openSession opens the v1 carrier in dir with the owner's passphrase: the
// passphrase opens the owner's cell, whose session seals and opens every
// record; the ledger is loaded from the carrier's branch tips.
func openSession(dir, pass string) (*session, error) {
	c, rep, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock(pass)), rand.Reader, nil)
	if err != nil {
		return nil, err
	}
	printFallback(rep)
	// The owner's session seals to the keyring's readers and the system
	// reader; a key's own passphrase opens the key's own session, never the
	// owner's.
	sess, sec, layer, err := keyLayerOf(c, pass)
	if err != nil {
		return nil, err
	}
	if layer.ledgerRead() == nil {
		// No event yet: there is no ledger to read or to record on.
		return nil, fmt.Errorf("%s %w", dir, errNoEventYet)
	}
	// The owner's session, like a key's, judges every record at its own
	// point: what a command records within one opening is read on from the
	// carrier when its point is asked (keyview.Layer.OwnersAt), and nothing
	// stands in for a point that is not known (T2).
	c.SetSealer(sess)
	raw, err := c.Get(c.Anchor())
	if err != nil {
		return nil, fmt.Errorf("genesis unreadable: %w", err)
	}
	heads, err := c.Heads()
	if err != nil {
		return nil, err
	}
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		return nil, err
	}
	// Names the references reach and nobody can prove are said on opening
	// (R4): damage to one's own vessel must not look like a few pending events.
	for _, u := range l.Unproven() {
		fmt.Fprintln(os.Stderr, "unproven:", u)
	}
	return &session{car: c, led: l, sec: sec, layer: layer}, nil
}

func (s *session) Close() {
	if s.lock != nil {
		s.lock.Release()
		s.lock = nil
	}
}

// turnPatience is how long a writing command waits for another writer.
const turnPatience = 15 * time.Second

// openWriting takes the vessel's writer's turn and only then opens it, so
// the heads a command writes on are read where no other writer can move
// them before its commit point.
func openWriting(dir, pass string) (*session, error) {
	lock, err := turn.Acquire(dir, turnPatience)
	if err != nil {
		return nil, fmt.Errorf("nothing was recorded: %w", err)
	}
	s, err := openSession(dir, pass)
	if err != nil {
		lock.Release()
		return nil, err
	}
	s.lock = lock
	return s, nil
}

// record makes one vessel commit: the events, the branch reference to the
// last one, and new slot cells when given. The ledger judges every event
// first; one already held is not recorded again.
func (s *session) record(events []event.Signed, branch string, slots [][]byte) error {
	if s.lock == nil {
		return errors.New("this session was not opened to write")
	}
	var fresh []event.Signed
	for _, e := range events {
		if s.led.State(e.ID) == ledger.Accepted {
			continue // already recorded: the same act again records nothing new
		}
		st, err := s.led.Add(e.Raw)
		if err != nil {
			return err
		}
		if st != ledger.Accepted {
			why, _ := s.led.Why(e.ID)
			return fmt.Errorf("event %s: %s; not stored", st, why)
		}
		fresh = append(fresh, e)
	}
	if len(fresh) == 0 && slots == nil {
		return nil
	}
	rec, err := s.car.Begin(s.lock)
	if err != nil {
		return err
	}
	for _, e := range fresh {
		// Each event is sealed at its own point, its parents, judged in this
		// session's ledger, which already holds the events of this commit it
		// rests on (contract E3, E4).
		sl, err := s.sealerAt(s.led, e.Event.Parents)
		if err != nil {
			rec.Abandon()
			return fmt.Errorf("event %s is not sealed at its own point, and nothing was recorded: %w", e.ID.Short(), err)
		}
		rec.SealWith(sl)
		if err := rec.Event(e.ID, e.Head, e.Body, e.Event.Address); err != nil {
			rec.Abandon()
			return err
		}
	}
	if branch != "" && len(events) > 0 {
		if err := rec.SetRef(branch, events[len(events)-1].ID); err != nil {
			rec.Abandon()
			return err
		}
	}
	if slots != nil {
		rec.Tx().SetSlots(slots)
	}
	out, err := rec.Commit()
	if out != vessel.Recorded {
		return fmt.Errorf("the vessel's commit was %s: %v", out, err)
	}
	return nil
}

// commit records one event and points the branch at it: one vessel commit.
func (s *session) commit(e event.Signed, branch string) error {
	return s.record([]event.Signed{e}, branch, nil)
}

// setRef points a branch at an event in its own commit.
func (s *session) setRef(branch string, id frame.ID) error {
	if s.lock == nil {
		return errors.New("this session was not opened to write")
	}
	rec, err := s.car.Begin(s.lock)
	if err != nil {
		return err
	}
	if err := rec.SetRef(branch, id); err != nil {
		rec.Abandon()
		return err
	}
	out, err := rec.Commit()
	if out != vessel.Recorded {
		return fmt.Errorf("the vessel's commit was %s: %v", out, err)
	}
	return nil
}

func (s *session) branchHead(name string) ([]frame.ID, error) {
	id, found, err := s.car.Ref(name)
	if err != nil {
		return nil, err
	}
	if !found {
		return []frame.ID{s.led.Genesis()}, nil
	}
	return []frame.ID{id}, nil
}

// signer resolves which key signs, and with what authority. The command line
// signs as the owner: the root from the owner's cell, or from --root-key in
// cold custody. A key of the keyring signs in its own session.
func (s *session) signer(name, rootKeyPath string) (ed25519.PrivateKey, *frame.ID, error) {
	if rootKeyPath != "" {
		priv, err := readKeyFile(rootKeyPath)
		if err != nil {
			return nil, nil, err
		}
		pub := priv.Public().(ed25519.PublicKey)
		if hex.EncodeToString(pub) != hex.EncodeToString(s.led.Root()) {
			return nil, nil, errors.New("key file is not this ledger's root key")
		}
		return priv, nil, nil
	}
	if name == "" || name == keyRootName {
		root := ownerRoot(s.sec)
		if root == nil {
			return nil, nil, errors.New("the root key is not in this vessel's owner cell (cold custody); pass --root-key FILE")
		}
		return root, nil, nil
	}
	return nil, nil, fmt.Errorf("key %q signs in its own session (its own passphrase, through a booth); the command line signs as the owner", name)
}

// doorName is what the command line attests on what it records: the
// registrar's mark, kept apart from the key that signed and the grant it
// signed under.
const doorName = "rokh"

func stamp() []event.Attestation {
	att, errs := oracle.Observe(oracle.Default())
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "warning:", e)
	}
	return append(att, event.Attestation{Oracle: "door", Claim: []byte(doorName)})
}

// ---------- commands ----------

func cmdInit(args []string) error {
	fs, dir, err := newFlags("init", args)
	if err != nil {
		return err
	}
	message := fs.String("message", "", "text of the first event")
	cold := fs.String("cold", "", "write the root key to this file and keep it out of the vessel")
	sz := fs.String("size", "64M", "the vessel's size")
	slab := fs.String("slab", "1M", "slab size, a power of two from 256K to 64M")
	growth := fs.String("growth", "fixed", "fixed, or auto:STEP:MAX in slabs")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if *message == "" {
		return errors.New("--message is required: the first page is not left blank")
	}
	if *cold != "" {
		if _, err := os.Stat(*cold); err == nil {
			return fmt.Errorf("%s already exists; refusing to overwrite a key file", *cold)
		}
	}
	params, err := vesselParams(*sz, *slab, *growth)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_, rootPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	// Genesis is signed before the vessel exists, because the vessel's
	// anchor *is* its id.
	gen, err := event.SignFresh(event.Event{
		Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte(*message), Attest: stamp(),
	}, rootPriv)
	if err != nil {
		return err
	}
	salt := make([]byte, 32)
	vk := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	if _, err := io.ReadFull(rand.Reader, vk); err != nil {
		return err
	}
	params.Salt = salt
	// The owner's cell holds the root's signing seed, or zeros in cold
	// custody, and the owner's reader (contract 4.6).
	var seed [32]byte
	if *cold == "" {
		copy(seed[:], rootPriv.Seed())
	}
	cell, sec, err := key.NewOwner(pass, salt, params.Iter, seed, vk, rand.Reader)
	if err != nil {
		return err
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		return err
	}
	ptk, err := vessel.SharedKey(vk)
	if err != nil {
		return err
	}
	sess.PTK = ptk
	c, err := carrier.Create(medium.Dir{Root: dir}, params, vk, [][]byte{cell}, gen.ID, frame.Zero, nil, sess)
	if err != nil {
		return err
	}
	lock, err := turn.Acquire(dir, turnPatience)
	if err != nil {
		return err
	}
	defer lock.Release()
	// The owner is a generation of the keyring too (contract 4.2: key id
	// zero, no slot), recorded with the genesis in one commit, and its add
	// carries the system reader's private half sealed to the owner's reader
	// (0x000B): every key holds the system reader, and system events are
	// sealed to it (3.3, E4). The one bootstrap of a ledger, the sentence
	// surface's too (keyview.Bootstrap).
	if out, err := keyview.Bootstrap(c, lock, defaultBranch, gen, sec, rootPriv); out != vessel.Recorded {
		return fmt.Errorf("the genesis was not recorded: %s %v", out, err)
	}
	if *cold != "" {
		if err := writeKeyFile(*cold, rootPriv); err != nil {
			return err
		}
	}
	fmt.Printf("carrier    %s\n", dir)
	fmt.Printf("anchor     %s\n", gen.ID)
	fmt.Printf("root key   %s\n", hex.EncodeToString(rootPriv.Public().(ed25519.PublicKey)))
	fmt.Printf("branch     %s\n", defaultBranch)
	fmt.Printf("vessel     %d slabs of %d bytes\n", params.Slabs, 1<<params.SlabLog2)
	if *cold != "" {
		fmt.Printf("cold root  %s\n", *cold)
		fmt.Println("The root key is NOT in this vessel. Move that file to separate media.")
		fmt.Println("You need it to grant or revoke: --root-key FILE.")
	}
	return nil
}

// vesselParams reads --size, --slab and --growth (contract section 1).
func vesselParams(sz, slab, growth string) (vessel.Params, error) {
	p := vessel.Params{Iter: 600000, Rand: rand.Reader}
	sb, err := size.Parse(slab)
	if err != nil {
		return p, err
	}
	k := 0
	for (int64(1) << k) < sb {
		k++
	}
	if int64(1)<<k != sb || k < 18 || k > 26 {
		return p, fmt.Errorf("--slab %s: a power of two from 256K to 64M", slab)
	}
	total, err := size.Parse(sz)
	if err != nil {
		return p, err
	}
	n := int((total + sb - 1) / sb)
	if n < 16 {
		n = 16
	}
	p.SlabLog2, p.Slabs = k, n
	switch {
	case growth == "fixed" || growth == "":
	case strings.HasPrefix(growth, "auto"):
		var step, max int
		if _, err := fmt.Sscanf(growth, "auto:%d:%d", &step, &max); err != nil {
			return p, fmt.Errorf("--growth %s: fixed, or auto:STEP:MAX", growth)
		}
		p.Growth = vessel.Growth{Auto: true, Step: step, Max: max}
	default:
		return p, fmt.Errorf("--growth %s: fixed, or auto:STEP:MAX", growth)
	}
	return p, nil
}

func cmdWrite(args []string) error {
	fs, dir, err := newFlags("write", args)
	if err != nil {
		return err
	}
	address := fs.String("address", "", "what this event is about")
	verb := fs.String("verb", "note", "verb")
	message := fs.String("message", "", "text payload")
	payloadFile := fs.String("payload-file", "", "read the payload bytes from a file, or - for stdin")
	key := fs.String("key", "", "signing key name (default: the key the passphrase opens: the root for the owner's, the key itself for a key's)")
	branch := fs.String("branch", defaultBranch, "branch")
	attempt := fs.String("attempt", "", "your own name for this recording; the same name and request again records nothing new")
	expect := fs.String("expect-heads", "", "comma-separated heads this write was prepared against; if the ledger moved, nothing is written")
	asJSON := fs.Bool("json", false, "print the typed answer as one JSON object")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if *address == "" {
		return errors.New("--address is required")
	}
	// The payload is opaque bytes to Rokh. --payload-file is how any other
	// program hands over something binary - a content descriptor, for
	// instance - without this CLI knowing what it is.
	payload := []byte(*message)
	if *payloadFile != "" {
		if *message != "" {
			return errors.New("give --message or --payload-file, not both")
		}
		var b []byte
		if *payloadFile == "-" {
			b, err = io.ReadAll(io.LimitReader(os.Stdin, event.MaxPayload+1))
		} else {
			b, err = os.ReadFile(*payloadFile)
		}
		if err != nil {
			return err
		}
		if len(b) > event.MaxPayload {
			return fmt.Errorf("payload is %d bytes; the inline limit is %d. Large content belongs outside the ledger, referenced by hash",
				len(b), event.MaxPayload)
		}
		payload = b
	}
	// The writer's turn is taken before the carrier is read, so the heads a
	// precondition names are judged on what the carrier holds under the turn,
	// not on a reading made before another writer finished (W2-004 R2).
	s, err := openWriting(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	// The command line records through the daemon's own commit point, in this
	// process: the same writing turn, the same precondition, the same attempt
	// and the same typed answer a program on the socket gets. There is one
	// way an event reaches the carrier, not two that must be kept alike. The
	// door records under the turn this command holds.
	//   — T8.5, T4.4
	// The owner's signing key is the one the passphrase opened; nil in cold
	// custody, where the door says so.
	opts := daemon.Options{AllowSign: true, Release: Release, Door: doorName,
		Root: ownerRoot(s.sec), Owner: s.lock, SealerAt: s.sealerAt}
	req := map[string]any{"op": "write", "address": *address, "verb": *verb,
		"payload": base64.StdEncoding.EncodeToString(payload), "key": *key, "branch": *branch}
	if *attempt != "" {
		req["attempt"] = *attempt
	}
	if *expect != "" {
		req["expect_heads"] = commaList(*expect)
	}
	human := func(r map[string]any) string {
		id, _ := r["id"].(string)
		if parsed, err := frame.ParseID(id); err == nil {
			id = parsed.Short()
		}
		note := ""
		if r["already"] == true {
			note = "  (already recorded under this attempt; nothing new)"
		}
		return fmt.Sprintf("%s  %s  %s%s", id, *address, *verb, note)
	}
	keyed := s.sec.Key != ([32]byte{})
	var ks oneKey
	if keyed {
		// A key's own passphrase opened that key's cell, not the owner's:
		// the record is the key's, signed by its signer under its own grant
		// and sealed at its own point, and never the root's (v1_keywrite.go).
		// The door is given that one key and no root.
		verbOf, branchOf := *verb, *branch
		if verbOf == "" {
			verbOf = "note" // the door's own default
		}
		if branchOf == "" {
			branchOf = defaultBranch
		}
		var bad map[string]any
		if ks, bad = s.keySigner(*key, *address, verbOf, branchOf); bad != nil {
			return answerOf(bad, *asJSON, human)
		}
		opts.Root, opts.NoRoot, opts.Keys = nil, true, ks
		req["key"], req["verb"], req["branch"] = ks.alias, verbOf, branchOf
	}
	srv := daemon.New(s.car, s.led, opts)
	if keyed {
		if bad := s.keyAttempt(srv, ks, *attempt); bad != nil {
			return answerOf(bad, *asJSON, human)
		}
	}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return answerOf(srv.Handle(line), *asJSON, human)
}

func cmdGrant(args []string) error {
	fs, dir, err := newFlags("grant", args)
	if err != nil {
		return err
	}
	to := fs.String("to", "", "name for the new key")
	scope := fs.String("scope", "", "address prefix; empty means the whole ledger")
	verbs := fs.String("verbs", "", "comma-separated verbs; empty means any non-reserved verb")
	delegate := fs.Bool("delegate", false, "allow this key to grant further")
	open := fs.Bool("open", false, "an open address: any key may write inside the scope and verbs (contract 4.5)")
	readOpen := fs.Bool("read-open", false, "with --open: the open address is readable by every key")
	rootKey := fs.String("root-key", "", "root key file, for a cold carrier")
	branch := fs.String("branch", defaultBranch, "branch")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if *open {
		return grantOpen(dir, pass, *scope, *verbs, *readOpen, *rootKey, *branch)
	}
	if *to == "" {
		return errors.New("--to is required")
	}
	s, err := openWriting(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	// Writing is a rokh.grant to a key's signer (K2): --to names a live key
	// of the keyring, by name or id.
	gens, err := findKey(s, *to)
	if err != nil {
		return err
	}
	pub := gens[len(gens)-1].k.Signer
	if pub == nil {
		return fmt.Errorf("key %q cannot write: it has no signer", *to)
	}
	var list []string
	if *verbs != "" {
		list = strings.Split(*verbs, ",")
	}
	payload, err := event.Grant{Subject: pub, Scope: *scope, Verbs: list, CanDelegate: *delegate}.Encode()
	if err != nil {
		return err
	}
	signPriv, authority, err := s.signer(keyRootName, *rootKey)
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
		Address: event.AddressRoot, Verb: event.VerbGrant, Payload: payload,
		Attest: stamp(),
	}, signPriv)
	if err != nil {
		return err
	}
	if err := s.commit(e, *branch); err != nil {
		return err
	}
	fmt.Printf("granted to %q  grant %s\n", *to, e.ID.Short())
	return nil
}

// grantOpen records an open grant: no subject, no delegation, only the root.
func grantOpen(dir, pass, scope, verbs string, readOpen bool, rootKey, branch string) error {
	var list []string
	if verbs != "" {
		list = strings.Split(verbs, ",")
	}
	payload, err := event.Grant{Open: true, Read: readOpen, Scope: scope, Verbs: list}.Encode()
	if err != nil {
		return err
	}
	s, err := openWriting(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()
	priv, authority, err := s.signer(keyRootName, rootKey)
	if err != nil {
		return err
	}
	if authority != nil {
		return errors.New("only the root writes an open grant")
	}
	parents, err := s.branchHead(branch)
	if err != nil {
		return err
	}
	anchor := s.car.Anchor()
	e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: parents,
		Address: event.AddressRoot, Verb: event.VerbGrant, Payload: payload, Attest: stamp()}, priv)
	if err != nil {
		return err
	}
	if err := s.commit(e, branch); err != nil {
		return err
	}
	where := scope
	if where == "" {
		where = "(the whole rokh)"
	}
	fmt.Printf("open address %s  grant %s\n", where, e.ID.Short())
	return nil
}

func cmdRevoke(args []string) error {
	fs, dir, err := newFlags("revoke", args)
	if err != nil {
		return err
	}
	target := fs.String("target", "", "id of the grant event")
	rootKey := fs.String("root-key", "", "root key file, for a cold carrier")
	branch := fs.String("branch", defaultBranch, "branch")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	id, err := frame.ParseID(*target)
	if err != nil {
		return err
	}
	s, err := openWriting(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	payload, err := event.Revoke{Target: id}.Encode()
	if err != nil {
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
		Address: event.AddressRoot, Verb: event.VerbRevoke, Payload: payload,
		Attest: stamp(),
	}, priv)
	if err != nil {
		return err
	}
	if err := s.commit(e, *branch); err != nil {
		return err
	}
	fmt.Printf("revoked %s (from here on, not in the past)\n", id.Short())
	return nil
}

func cmdMerge(args []string) error {
	fs, dir, err := newFlags("merge", args)
	if err != nil {
		return err
	}
	into := fs.String("branch", defaultBranch, "destination branch")
	from := fs.String("from", "", "source branch")
	key := fs.String("key", "", "signing key name")
	rootKey := fs.String("root-key", "", "root key file, for a cold carrier")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	if *from == "" {
		return errors.New("--from is required")
	}
	s, err := openWriting(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	a, err := s.branchHead(*into)
	if err != nil {
		return err
	}
	b, err := s.branchHead(*from)
	if err != nil {
		return err
	}
	if a[0] == b[0] {
		return errors.New("both branches point at the same head; nothing to merge")
	}
	priv, authority, err := s.signer(*key, *rootKey)
	if err != nil {
		return err
	}
	anchor := s.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Authority: authority, Parents: []frame.ID{a[0], b[0]},
		Address: event.AddressRoot, Verb: event.VerbMerge, Attest: stamp(),
	}, priv)
	if err != nil {
		return err
	}
	if err := s.commit(e, *into); err != nil {
		return err
	}
	fmt.Printf("merged %s\n", e.ID.Short())
	return nil
}

func cmdLog(args []string) error {
	fs, dir, err := newFlags("log", args)
	if err != nil {
		return err
	}
	long := fs.Bool("long", false, "full ids")
	asJSON := fs.Bool("json", false, "one JSON object per line")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	// Three questions beside each event, kept apart: the key that signed
	// (by the name this carrier holds it under), the grant it signed under,
	// and the door that recorded it.
	//   — T12.5
	names := map[string]string{string(s.led.Root()): keyRootName}
	for _, k := range liveKeys(s) {
		if k.k.Signer != nil {
			names[string(k.k.Signer)] = k.k.Name
		}
	}
	enc := json.NewEncoder(os.Stdout)
	for _, id := range s.led.Order() {
		e, _ := s.led.Get(id)
		key := names[string(e.Event.Author)]
		door := ""
		for _, a := range e.Event.Attest {
			if a.Oracle == "door" {
				door = string(a.Claim)
			}
		}
		if *asJSON {
			row := map[string]any{
				"id": id.String(), "address": e.Event.Address, "verb": e.Event.Verb,
				"author": hex.EncodeToString(e.Event.Author),
			}
			if key != "" {
				row["key"] = key
			}
			if e.Event.Authority != nil {
				row["authority"] = e.Event.Authority.String()
			}
			if door != "" {
				row["door"] = door
			}
			// base64, not a Go string: a payload may be binary (a content
			// descriptor is), and a string would mangle it.
			if len(e.Event.Payload) > 0 {
				row["payload"] = base64.StdEncoding.EncodeToString(e.Event.Payload)
			}
			if err := enc.Encode(row); err != nil {
				return err
			}
			continue
		}
		show := id.Short()
		if *long {
			show = id.String()
		}
		by := key
		if by == "" {
			by = hex.EncodeToString(e.Event.Author[:4]) + "…"
		}
		under := "owner"
		if e.Event.Authority != nil {
			under = "grant " + e.Event.Authority.Short()
		}
		if door != "" {
			under += " via " + door
		}
		// A system event is one of the same line, with its mark (contract 4.1).
		mark := "      "
		if e.System {
			mark = "system"
		}
		fmt.Printf("%s  %s %-24s %-12s %-10s %-28s %s\n", show, mark, e.Event.Address, e.Event.Verb,
			by, under, preview(e.Event.Payload))
	}
	return nil
}

// preview renders a payload for a terminal.
//
// The rule here is not the rule for addresses, deliberately: an address is an
// identifier, so it refuses every ambiguity. A payload is prose, so it only
// refuses *deception* - control bytes and bidirectional overrides. Anything
// binary is shown as a length and a hex head instead of being dumped raw.
func preview(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	printable := true
	for _, r := range string(b) {
		if r == 0xFFFD || r < 0x20 || r == 0x7F ||
			(r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			printable = false
			break
		}
	}
	if !printable {
		h := hex.EncodeToString(b)
		if len(h) > 24 {
			h = h[:24] + "..."
		}
		return fmt.Sprintf("<%d bytes> %s", len(b), h)
	}
	s := strings.ReplaceAll(string(b), "\n", " ")
	if len([]rune(s)) > 40 {
		return string([]rune(s)[:40]) + "..."
	}
	return s
}

func cmdVerify(args []string) error {
	fs, dir, err := newFlags("verify", args)
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	acc, rej, pend := s.led.Tally()
	fmt.Printf("anchor    %s\n", s.led.Genesis())
	fmt.Printf("root      %s\n", hex.EncodeToString(s.led.Root()))
	fmt.Printf("events    %d accepted, %d rejected, %d pending\n", acc, rej, pend)
	fmt.Printf("heads     ")
	for _, h := range s.led.Heads() {
		fmt.Printf("%s ", h.Short())
	}
	fmt.Println()
	if g := s.led.ActiveGrants(); len(g) > 0 {
		fmt.Printf("grants    ")
		for _, id := range g {
			fmt.Printf("%s ", id.Short())
		}
		fmt.Println()
	}
	if rej > 0 {
		return errors.New("ledger contains rejected events")
	}
	if pend > 0 {
		fmt.Println("note: some events are pending; their ancestors have not arrived")
	}
	fmt.Println("ok        every event on this carrier verifies")
	return nil
}

func cmdBranch(args []string) error {
	fs, dir, err := newFlags("branch", args)
	if err != nil {
		return err
	}
	create := fs.String("create", "", "name of a new branch")
	at := fs.String("at", "", "event id the branch points at")
	long := fs.Bool("long", false, "full ids")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	open := openSession
	if *create != "" {
		open = openWriting
	}
	s, err := open(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	if *create != "" {
		id, err := frame.ParseID(*at)
		if err != nil {
			return fmt.Errorf("--at: %w", err)
		}
		if s.led.State(id) != ledger.Accepted {
			return errors.New("a branch may only point at an accepted event")
		}
		if err := s.setRef(*create, id); err != nil {
			return err
		}
		fmt.Printf("branch %q at %s\n", *create, id.Short())
		return nil
	}
	refs, err := s.car.Refs()
	if err != nil {
		return err
	}
	for name, id := range refs {
		show := id.Short()
		if *long {
			show = id.String()
		}
		fmt.Printf("%-16s %s\n", name, show)
	}
	return nil
}

func cmdKeys(args []string) error {
	fs, dir, err := newFlags("keys", args)
	if err != nil {
		return err
	}
	export := fs.String("export", "", "keyring entry to export")
	out := fs.String("out", "", "file to write the exported key to")
	drop := fs.String("drop", "", "keyring entry to remove from the carrier")
	reading := fs.Bool("reading", false, "make this carrier's reading key if it has none, and print its public half")
	public := fs.Bool("public", false, "print the public keys — what you give somebody else")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()
	switch {
	case *public, *reading:
		return publicKeys(s)
	case *export != "":
		if *out == "" {
			return errors.New("--out is required with --export")
		}
		if *export != keyRootName {
			return fmt.Errorf("only the root is held by the owner's cell; key %q lives in its own cell", *export)
		}
		root := ownerRoot(s.sec)
		if root == nil {
			return errors.New("the root is not in this vessel (cold custody)")
		}
		if err := writeKeyFile(*out, root); err != nil {
			return err
		}
		fmt.Printf("exported %q to %s\n", *export, *out)
		fmt.Fprintln(os.Stderr, "\nThat file holds the PRIVATE root key. It signs as you.")
		fmt.Fprintf(os.Stderr, "To give somebody your public keys:  rokh keys %s --public\n", dir)
		return nil
	case *drop != "":
		return errors.New("a key of the keyring is taken back with rokh key revoke; nothing was changed")
	}
	return keyList(s, "")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// share, unshare and covenants are disclosure, which is not authorship.
// They write ordinary, non-reserved events at the address "peer"; the core
// neither knows nor privileges them. See docs/07.
//
// Only the ledger root may author one: the evaluator ignores any covenant
// signed by anyone else, so a write-delegate cannot disclose the ledger to
// itself. Delegated disclosure is a later ruling.
func cmdShare(args []string) error {
	fs, dir, err := newFlags("share", args)
	if err != nil {
		return err
	}
	to := fs.String("to", "", "recipient Ed25519 public key, hex")
	scope := fs.String("scope", "", "address prefix; empty means the whole ledger")
	room := fs.String("room", "", "id of the one event to disclose, instead of a scope")
	sealTo := fs.String("seal-to", "", "recipient X25519 key for sealed content, hex")
	rootKey := fs.String("root-key", "", "root key file, for a cold carrier")
	branch := fs.String("branch", defaultBranch, "branch")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	subject, err := hex.DecodeString(*to)
	if err != nil || len(subject) != ed25519.PublicKeySize {
		return errors.New("--to must be a 64-character hex Ed25519 public key")
	}
	var seal []byte
	if *sealTo != "" {
		seal, err = hex.DecodeString(*sealTo)
		if err != nil || len(seal) != covenant.SealKeySize {
			return errors.New("--seal-to must be a 64-character hex X25519 key")
		}
	}
	// A scope names a place, and a place is somewhere events keep arriving:
	// handing one over hands over what will be written there tomorrow. A room
	// names one event by the only name an event has and covers exactly that
	// one. They answer the same question two ways, so naming both is refused
	// rather than reconciled by a rule nobody wrote.
	//   — T6.1, N4.1
	var chamber frame.ID
	if *room != "" {
		if chamber, err = frame.ParseID(*room); err != nil {
			return fmt.Errorf("--room: %w", err)
		}
	}
	share := covenant.Share{Subject: subject, Scope: *scope, SealTo: seal, Room: chamber}
	payload, err := share.Encode()
	if err != nil {
		return err
	}
	id, err := writeCovenant(dir, pass, *rootKey, *branch, covenant.VerbShare, payload)
	if err != nil {
		return err
	}
	reach := fmt.Sprintf("scope %q", *scope)
	if share.Reach().IsRoom() {
		reach = "room " + chamber.Short() + " — that one event, and nothing else"
	}
	fmt.Printf("shared with %s...  %s  covenant %s\n", (*to)[:16], reach, id.Short())
	fmt.Println("this permits a transfer; it does not start one")
	return nil
}

func cmdUnshare(args []string) error {
	fs, dir, err := newFlags("unshare", args)
	if err != nil {
		return err
	}
	target := fs.String("target", "", "id of the share event to withdraw")
	rootKey := fs.String("root-key", "", "root key file, for a cold carrier")
	branch := fs.String("branch", defaultBranch, "branch")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	tid, err := resolveShare(dir, pass, *target)
	if err != nil {
		return err
	}
	payload, err := covenant.Unshare{Target: tid}.Encode()
	if err != nil {
		return err
	}
	id, err := writeCovenant(dir, pass, *rootKey, *branch, covenant.VerbUnshare, payload)
	if err != nil {
		return err
	}
	fmt.Printf("withdrew %s  (from here on, not in the past)  event %s\n", tid, id.Short())
	return nil
}

// resolveShare reads a share's name: the full id, or a short name of at least
// eight hex digits that is unique among the live shares. The event records
// the full id either way (contract section 6, goal 7.42).
func resolveShare(dir, pass, target string) (frame.ID, error) {
	if len(target) == frame.IDSize*2 {
		return frame.ParseID(target)
	}
	target = strings.ToLower(target)
	if len(target) < 8 {
		return frame.Zero, errors.New("--target is a share's id or a short name of at least 8 hex digits")
	}
	if _, err := hex.DecodeString(target[:len(target)&^1]); err != nil {
		return frame.Zero, fmt.Errorf("--target %q is not hex", target)
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return frame.Zero, err
	}
	defer s.Close()
	var found []frame.ID
	for _, c := range covenant.ActiveAt(s.led) {
		if strings.HasPrefix(c.ID.String(), target) {
			found = append(found, c.ID)
		}
	}
	switch len(found) {
	case 0:
		return frame.Zero, fmt.Errorf("no live share begins %s", target)
	case 1:
		return found[0], nil
	}
	names := make([]string, 0, len(found))
	for _, id := range found {
		names = append(names, id.String())
	}
	return frame.Zero, fmt.Errorf("%s names %d live shares; give more digits: %s", target, len(found), strings.Join(names, ", "))
}

func writeCovenant(dir, pass, rootKey, branch, verb string, payload []byte) (frame.ID, error) {
	s, err := openWriting(dir, pass)
	if err != nil {
		return frame.Zero, err
	}
	defer s.Close()

	priv, authority, err := s.signer(keyRootName, rootKey)
	if err != nil {
		return frame.Zero, err
	}
	parents, err := s.branchHead(branch)
	if err != nil {
		return frame.Zero, err
	}
	anchor := s.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Authority: authority, Parents: parents,
		Address: covenant.Address, Verb: verb, Payload: payload, Attest: stamp(),
	}, priv)
	if err != nil {
		return frame.Zero, err
	}
	if err := s.commit(e, branch); err != nil {
		return frame.Zero, err
	}
	return e.ID, nil
}

func cmdCovenants(args []string) error {
	fs, dir, err := newFlags("covenants", args)
	if err != nil {
		return err
	}
	at := fs.String("at", "", "event id to evaluate at; default is the heads")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	var points []frame.ID
	if *at != "" {
		id, err := frame.ParseID(*at)
		if err != nil {
			return err
		}
		points = append(points, id)
	}
	live := covenant.ActiveAt(s.led, points...)
	if len(live) == 0 {
		fmt.Println("no live covenants; nothing may be disclosed")
		return nil
	}
	for _, c := range live {
		// A room covenant has no scope, and an empty scope printed as its
		// reach would read as the whole ledger — the widest thing there is,
		// where the narrowest was meant. What a covenant reaches is asked of
		// the covenant rather than guessed from one of its fields.
		//   — T6.1, T7.3
		reach := "scope " + c.Scope
		switch {
		case c.Reach().IsRoom():
			reach = "room " + c.Room.Short() + " (that one event)"
		case c.Scope == "":
			reach = "scope (whole ledger)"
		}
		sealed := ""
		if len(c.SealTo) > 0 {
			sealed = "  sealed"
		}
		fmt.Printf("%s  -> %s  %s%s\n",
			c.ID.Short(), hex.EncodeToString(c.Subject)[:16], reach, sealed)
	}
	return nil
}

func cmdAnnounce(args []string) error {
	fs, dir, err := newFlags("announce", args)
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()

	heads := s.led.Heads()
	if len(heads) == 0 {
		return errors.New("no heads to announce")
	}
	acc, _, _ := s.led.Tally()
	for _, h := range heads {
		e, _ := s.led.Get(h)
		a := announce.New(s.led.Genesis(), h, acc, e.Event.Address)
		b, err := a.Encode()
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", hex.EncodeToString(b))
		fmt.Fprintf(os.Stderr, "  %d bytes  %s\n", len(b), a.String())
	}
	return nil
}

func cmdShow(args []string) error {
	fs, dir, err := newFlags("show", args)
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	// Without a key a v1 vessel says only that it is one, its KDF cost and
	// salt, and its size (contract 2.3); nothing else is readable here.
	fmt.Println("Rokh's footprint on this carrier:")
	for _, f := range carrier.Footprint() {
		fmt.Printf("  %s\n", filepath.Join(dir, f))
	}
	fmt.Println("Removing exactly those removes Rokh completely.")
	return nil
}

func cmdDaemon(args []string) error {
	fs, dir, err := newFlags("daemon", args)
	if err != nil {
		return err
	}
	socket := fs.String("socket", "", "a Unix socket path to listen on")
	listen := fs.String("listen", "", "where to listen: unix:PATH or tcp:127.0.0.1:PORT")
	stdio := fs.Bool("stdio", false, "serve one session on standard input and output")
	readOnly := fs.Bool("read-only", false, "refuse all writes")
	noSign := fs.Bool("no-sign", false, "refuse the write op; append still works")
	noRoot := fs.Bool("no-root", false, "never sign with the root key: the door a program is given")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	network, address := transport.Unix, *socket
	if *listen != "" {
		if network, address, err = transport.Parse(*listen); err != nil {
			return err
		}
	}
	if address == "" && !*stdio {
		return errors.New("--socket PATH, --listen unix:PATH|tcp:127.0.0.1:PORT or --stdio")
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()
	// The daemon is the booth of a carrier: rokh.booth/1 on any ordered byte
	// stream, every session bound to one key of the keyring (contract 5).
	// The owner's signing key goes to the door the owner opened, and never
	// to a door a program is given.
	var root ed25519.PrivateKey
	if !*noRoot {
		root = ownerRoot(s.sec)
	}
	srv := daemon.New(s.car, s.led, daemon.Options{
		ReadOnly:  *readOnly,
		AllowSign: !*noSign,
		NoRoot:    *noRoot,
		Root:      root,
		Dir:       dir,
		Release:   Release,
		Door:      doorName,
		SealerAt:  s.sealerAt,
		// The key layer the passphrase opened: the owner's, or a key's,
		// whose view this door then serves every session at most (B4).
		Layer: s.layer.Layer,
	})
	if *stdio {
		return srv.ServeBooth(transport.Streams(os.Stdin, os.Stdout))
	}
	ln, err := transport.Listen(network, address)
	if err != nil {
		return err
	}
	defer ln.Close()
	acc, _, _ := s.led.Tally()
	fmt.Fprintf(os.Stderr, "rokh daemon: rokh.booth/1 on %s:%s\n", network, ln.Addr())
	fmt.Fprintf(os.Stderr, "  anchor %s\n", s.led.Genesis())
	fmt.Fprintf(os.Stderr, "  %d accepted events, read-only=%v signing=%v\n", acc, *readOnly, !*noSign)
	fmt.Fprintln(os.Stderr, "  no timers, no watchers: only a request can write an event")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		fmt.Fprintln(os.Stderr, "\nshutting down")
		ln.Close()
	}()
	return srv.ListenBooth(ln)
}

// allHeads is every branch tip the carrier names. They are the commit points:
// what they reach is the ledger, and what they do not reach is not yet
// recorded.
//
//	— T8.5
func allHeads(c *carrier.Carrier) ([]frame.ID, error) {
	refs, err := c.Refs()
	if err != nil {
		return nil, err
	}
	out := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return out, nil
}

// openable reports whether an argument is the surface's business rather than a
// mistyped subcommand: a folder, or one of the surface's own flags.
//
// A folder that does not exist yet counts. The surface makes the vault it is
// pointed at, and the first-run screen says so, so requiring the folder to
// exist first made "rokh ~/rokh" — the one line that screen prints — answer
// "unknown command". What keeps a typo out is not the folder being there but
// the argument looking like a path at all: a mistyped subcommand is one bare
// word, and one bare word is still refused.
func openable(arg string) bool {
	switch arg {
	// Every flag the surface's own help lists, in both spellings. A flag that
	// help offers and this refuses tells somebody who did exactly as they
	// were told that they typed an unknown command.
	case "-c", "-library", "-mount", "-plain", "-home-fd",
		"--c", "--library", "--mount", "--plain", "--home-fd":
		return true
	}
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		return true
	}
	return strings.ContainsRune(arg, os.PathSeparator) ||
		arg == "~" || strings.HasPrefix(arg, "~"+string(os.PathSeparator))
}

// publicKeys prints what a person may hand to somebody else: the owner's
// signing and reading public halves. They are not secrets; handing them over
// is their whole purpose.
//
//	— T7.1, T13.1
func publicKeys(s *session) error {
	fmt.Printf("signing   %s\n", hex.EncodeToString(s.led.Root()))
	r, err := key.ReaderFrom(s.sec.Reader[:])
	if err != nil {
		return fmt.Errorf("the owner's reader is not one: %w", err)
	}
	fmt.Printf("reading   %s\n", hex.EncodeToString(r.Public()))
	fmt.Fprintln(os.Stderr, "\nBoth are public. Give them to whoever is sharing with you.")
	return nil
}

// exitUnknown is the exit status of a recording whose ending could not be read
// back. It is neither success nor failure, and a script must not write again
// on it: it asks "rokh attempt" first.
const exitUnknown = 4

// exitStatus carries an exit status other than 1 up to main.
type exitStatus struct {
	code int
	err  error
}

func (e exitStatus) Error() string { return e.err.Error() }
func (e exitStatus) Unwrap() error { return e.err }

// answerOf prints a typed answer and turns it into the command's exit: 0 when
// recorded, 1 when nothing was recorded, 4 when nobody could read back whether
// it was. The words and the status never disagree.
//
//	— T8.5, T10.4
func answerOf(r map[string]any, asJSON bool, human func(map[string]any) string) error {
	if asJSON {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	}
	ok, _ := r["ok"].(bool)
	if ok && r["record"] == daemon.Recorded {
		if !asJSON {
			fmt.Println(human(r))
		}
		return nil
	}
	msg, _ := r["error"].(string)
	code, _ := r["code"].(string)
	if r["record"] == daemon.Unknown {
		return exitStatus{code: exitUnknown, err: fmt.Errorf("%s (%s); whether it was recorded is unknown: ask rokh attempt, and do not write it again", msg, code)}
	}
	if msg == "" {
		msg = "nothing was recorded"
		if r["ignored"] == true {
			msg = "the harness bound at this address ignores that verb; nothing was recorded"
		}
	}
	if code == "" {
		return errors.New(msg)
	}
	return fmt.Errorf("%s (%s)", msg, code)
}

// commaList splits a comma-separated flag into its non-empty parts.
func commaList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// cmdAttempt asks, reading only, whether a recording was made under a name. A
// script whose write ended without an answer asks this instead of writing
// again.
//
//	— T8.5, T10.4
func cmdAttempt(args []string) error {
	fs, dir, err := newFlags("attempt", args)
	if err != nil {
		return err
	}
	name := fs.String("attempt", "", "the name the recording was given")
	key := fs.String("key", "", "whose attempt: a key name (default: the key the passphrase opens: the root for the owner's, the key itself for a key's)")
	author := fs.String("author", "", "whose attempt: a public key in 64 hex digits, instead of --key")
	asJSON := fs.Bool("json", false, "print the typed answer as one JSON object")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()
	srv := daemon.New(s.car, s.led, daemon.Options{Dir: dir, Release: Release, Root: ownerRoot(s.sec)})
	req := map[string]any{"op": "attempt", "attempt": *name, "key": *key, "author": *author}
	var r map[string]any
	var own liveKey
	keyed := s.sec.Key != ([32]byte{}) && *author == ""
	if keyed {
		// A key's own passphrase asks after the attempts of the key it
		// opened, as its writes record under it (v1_keywrite.go).
		var bad map[string]any
		own, bad = ownSigner(s, *key)
		r = bad
		req["key"], req["author"] = "", hex.EncodeToString(own.k.Signer)
	}
	if r == nil {
		line, err := json.Marshal(req)
		if err != nil {
			return err
		}
		r = srv.Handle(line)
		if keyed && r["record"] == daemon.NotRecorded {
			// Not found among what the key reads is not "not recorded" when
			// the key signed events it cannot read: the answer is unknown.
			if n := s.unreadOwn(own.k.Signer); n > 0 {
				r = map[string]any{"ok": false, "attempt": *name, "record": daemon.Unknown, "code": "attempt_unsupported",
					"error": fmt.Sprintf("key %q signed %d events it cannot read, so %v under %q; ask with the owner's passphrase", own.k.Name, n, errAttemptUnjudged, *name)}
			}
		}
	}
	if *asJSON {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	}
	if ok, _ := r["ok"].(bool); !ok {
		if r["record"] == daemon.Unknown {
			return exitStatus{code: exitUnknown, err: fmt.Errorf("%v (%v); whether it was recorded is unknown here", r["error"], r["code"])}
		}
		return fmt.Errorf("%v (%v)", r["error"], r["code"])
	}
	if !*asJSON {
		if r["record"] == daemon.Recorded {
			fmt.Printf("recorded  %v\n", r["id"])
		} else {
			fmt.Println("not recorded")
		}
	}
	return nil
}

// ---------- keys (contract 4.2 to 4.6) ----------

// readerPrefix names a key's reading half among the carrier's secrets.
const readerPrefix = "reader:"

// keyStream is the randomness of a key command. With an attempt it is drawn
// from the root's seed and the attempt's name, so the same attempt again
// makes the same request and the door answers the first result; without one
// it is the system's.
func keyStream(s *session, attempt string) (io.Reader, error) {
	if attempt == "" {
		return rand.Reader, nil
	}
	if s.sec.SignerSeed == ([32]byte{}) {
		return nil, errors.New("--attempt on a key command needs the root in the owner's cell")
	}
	raw := s.sec.SignerSeed[:]
	anchor := s.car.Anchor()
	return &detStream{seed: sha256.Sum256(append(append(append([]byte("rokh/key/attempt/1"), raw...), anchor[:]...), attempt...))}, nil
}

type detStream struct {
	seed [32]byte
	n    uint64
	buf  []byte
}

func (d *detStream) Read(p []byte) (int, error) {
	for i := range p {
		if len(d.buf) == 0 {
			var b [40]byte
			copy(b[:32], d.seed[:])
			binary.BigEndian.PutUint64(b[32:], d.n)
			d.n++
			h := sha256.Sum256(b[:])
			d.buf = h[:]
		}
		p[i] = d.buf[0]
		d.buf = d.buf[1:]
	}
	return len(p), nil
}

// record writes one reserved event through the door's own commit point, with
// its attempt: the same attempt again records nothing new.
func record(s *session, dir, verb string, payload []byte, attempt string) (string, error) {
	// The session already holds the carrier's writing turn (openWriting); the
	// door in this process records under it rather than taking it again.
	_ = dir
	srv := daemon.New(s.car, s.led, daemon.Options{AllowSign: true, Release: Release, Door: doorName,
		Root: ownerRoot(s.sec), Owner: s.lock, SealerAt: s.sealerAt})
	req := map[string]any{"op": "write", "address": event.AddressRoot, "verb": verb,
		"payload": base64.StdEncoding.EncodeToString(payload), "key": keyRootName}
	if attempt != "" {
		req["attempt"] = attempt
	}
	line, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	r := srv.Handle(line)
	if r["record"] != daemon.Recorded {
		return "", fmt.Errorf("%v (%v); nothing was recorded", r["error"], r["code"])
	}
	id, _ := r["id"].(string)
	return id, nil
}

func cmdKey(args []string) error {
	if len(args) == 0 {
		return errors.New("rokh key add|list|show|revoke|rotate|passwd DIR ...")
	}
	sub, rest := args[0], args[1:]
	fs, dir, err := newFlags("key "+sub, rest)
	if err != nil {
		return err
	}
	name := fs.String("name", "", "the key's name, one address component")
	reads := fs.String("reads", "", "comma-separated address prefixes the key reads; \"*\" reads everything")
	write := fs.Bool("write", false, "the key can write: it gets a signer and a grant")
	scope := fs.String("scope", "", "with --write: the address prefix it writes under")
	keyHex := fs.String("key", "", "the key's id (64 hex) or name")
	keyPassFile := fs.String("key-passphrase-file", "", "a file holding the key's own passphrase (its slot cell)")
	attempt := fs.String("attempt", "", "your own name for this act; the same attempt again records nothing new")
	if err := fs.Parse(rest[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "show":
		s, err := openSession(dir, pass)
		if err != nil {
			return err
		}
		defer s.Close()
		return keyList(s, *keyHex)
	case "add":
		s, err := openWriting(dir, pass)
		if err != nil {
			return err
		}
		defer s.Close()
		return keyAdd(s, dir, *keyPassFile, *name, *reads, *write, *scope, *attempt, 1, [32]byte{}, nil)
	case "revoke":
		s, err := openWriting(dir, pass)
		if err != nil {
			return err
		}
		defer s.Close()
		return keyRevoke(s, dir, *keyHex, *attempt)
	case "rotate":
		s, err := openWriting(dir, pass)
		if err != nil {
			return err
		}
		defer s.Close()
		return keyRotate(s, dir, *keyPassFile, *keyHex, *attempt)
	case "passwd":
		return errors.New("key passwd is not built in this version")
	}
	return fmt.Errorf("unknown key command %q", sub)
}

type liveKey struct {
	event frame.ID
	k     event.Keyring
}

func liveKeys(s *session) []liveKey {
	adds, _ := s.led.Keyring()
	var out []liveKey
	for _, id := range adds {
		e, _ := s.led.Get(id)
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err == nil {
			out = append(out, liveKey{id, k})
		}
	}
	return out
}

func matchKey(k event.Keyring, want string) bool {
	return want == "" || k.Name == want || hex.EncodeToString(k.Key[:]) == want
}

func keyList(s *session, want string) error {
	keys := liveKeys(s)
	count := map[[32]byte]int{}
	for _, k := range keys {
		count[k.k.Key]++
	}
	fmt.Println("owner                             reads everything, writes as the root")
	shown := 0
	for _, k := range keys {
		if !matchKey(k.k, want) {
			continue
		}
		shown++
		rd := "reads nothing"
		if k.k.Reads != nil {
			rd = "reads " + strings.Join(k.k.Reads, ",")
			if len(k.k.Reads) == 1 && k.k.Reads[0] == "" {
				rd = "reads everything"
			}
		}
		wr := "cannot write"
		if k.k.Signer != nil {
			wr = "can write"
		}
		mark := ""
		if count[k.k.Key] > 1 {
			mark = "  concurrent"
		}
		fmt.Printf("%s  %-16s gen %d  %s, %s  (event %s)%s\n", hex.EncodeToString(k.k.Key[:8]), k.k.Name, k.k.Gen, rd, wr, k.event.Short(), mark)
	}
	if want != "" && shown == 0 {
		return fmt.Errorf("no live key %q", want)
	}
	return nil
}

func splitReads(reads string) []string {
	if reads == "" {
		return nil
	}
	if reads == "*" {
		return []string{""}
	}
	var out []string
	for _, p := range strings.Split(reads, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	// A prefix list has one spelling: ascending byte order, once each.
	sort.Strings(out)
	uniq := out[:0]
	for i, p := range out {
		if i == 0 || out[i-1] != p {
			uniq = append(uniq, p)
		}
	}
	return uniq
}

// ownerOnly refuses a keyring change under a key's own passphrase: keyring
// adds and revokes come from the root (K1), and a key's session holds no
// owner cell. The command line does not record a key's revocation of its own
// generation either.
func ownerOnly(s *session) error {
	if s.sec.Key != ([32]byte{}) {
		return errors.New("a keyring change is made with the owner's passphrase (contract K1); this passphrase opens a key, not the owner's cell; nothing was recorded")
	}
	return nil
}

func keyAdd(s *session, dir, keyPassFile, name, reads string, write bool, scope, attempt string, gen uint32, id [32]byte, heirOf []byte) error {
	if err := ownerOnly(s); err != nil {
		return err
	}
	if name == "" {
		return errors.New("--name is required")
	}
	if keyPassFile == "" {
		return errors.New("a key opens with its own passphrase: --key-passphrase-file FILE (the owner's passphrase opens everything too)")
	}
	// The second envelope of the add names the owners of the add's own point
	// (E3), and they are asked for before anything is recorded: the add goes
	// on main's head, which this session's turn holds still. A point whose
	// owner fold was emptied by revocation, or that this ledger does not
	// know, is refused; only where no owner generation was ever added does
	// the owner's cell stand in (R4).
	owner, err := key.ReaderFrom(s.sec.Reader[:])
	if err != nil {
		return err
	}
	point, err := s.branchHead(defaultBranch)
	if err != nil {
		return err
	}
	readers, ever, known := s.led.OwnerFoldAt(point...)
	if _, err := ownersOf(readers, ever, known, owner.Public()); err != nil {
		return fmt.Errorf("the owner at the point of the key's add is not given: %w; nothing was recorded", err)
	}
	f, err := os.Open(keyPassFile)
	if err != nil {
		return err
	}
	keyPass, err := pp.Read(f)
	f.Close()
	if err != nil {
		return err
	}
	rnd, err := keyStream(s, attempt)
	if err != nil {
		return err
	}
	if id == ([32]byte{}) {
		if _, err := io.ReadFull(rnd, id[:]); err != nil {
			return err
		}
	}
	rd, err := key.NewReader(rnd)
	if err != nil {
		return err
	}
	k := event.Keyring{Op: event.KeyringAdd, Key: id, Gen: gen, Name: name, Reader: rd.Public(), Reads: splitReads(reads)}
	secret := key.Secret{Key: id, Gen: gen}
	copy(secret.Reader[:], rd.Bytes())
	if write {
		seed := make([]byte, ed25519.SeedSize)
		if _, err := io.ReadFull(rnd, seed); err != nil {
			return err
		}
		copy(secret.SignerSeed[:], seed)
		k.Signer = ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	}
	// The key's own cell: its passphrase opens its secret and the vessel's
	// key (contract 4.6); the keyring add carries the wrapped half as slot,
	// and the cell is made around that same half, so the cell is known by it.
	// It goes where nobody's cell is; when all are held, nothing is recorded.
	place, err := freeCell(s)
	if err != nil {
		return err
	}
	info := s.car.Vessel().Info()
	kk, err := key.PassKey(keyPass, info.Salt, info.Iter)
	if err != nil {
		return err
	}
	if k.Slot, err = key.Blob(kk, secret, rnd); err != nil {
		return err
	}
	cell, err := key.CellFrom(k.Slot, secret, s.car.Vessel().VK(), rnd)
	if err != nil {
		return err
	}
	// The system reader's private half, sealed to the new key's reader
	// (0x000B): the key reads system events through it (3.3). A carrier made
	// before the owner held a system reader has none to give.
	if sys, ok := systemReader(s); ok {
		if k.System, err = key.SealTo(rd.Public(), sys.Bytes(), key.InfoSystem, rnd); err != nil {
			return err
		}
	}
	if heirOf != nil {
		h, err := key.SealTo(rd.Public(), heirOf, key.InfoHeir, rnd)
		if err != nil {
			return err
		}
		k.Heir = h
	}
	payload, err := k.Encode()
	if err != nil {
		return err
	}
	addID, err := record(s, dir, event.VerbKeyring, payload, attempt)
	if err != nil {
		return err
	}
	if write {
		g, err := event.Grant{Subject: k.Signer, Scope: scope}.Encode()
		if err != nil {
			return err
		}
		grantAttempt := ""
		if attempt != "" {
			grantAttempt = attempt + "/grant"
		}
		if err := s.reload(); err != nil {
			return err
		}
		if _, err := record(s, dir, event.VerbGrant, g, grantAttempt); err != nil {
			return err
		}
	}
	slots := s.car.Vessel().Slots()
	if place >= len(slots) {
		return fmt.Errorf("the key's cell has no place: the vessel holds %d cells", len(slots))
	}
	slots[place] = cell
	// With the cell, a second envelope of the add, for the key it adds (E2):
	// the key reads its own generation, and through it the system reader. It
	// names the owner generations at the add's point too (E3).
	if err := s.reload(); err != nil {
		return err
	}
	aid, err := frame.ParseID(addID)
	if err != nil {
		return err
	}
	ae, ok := s.led.Get(aid)
	if !ok {
		return errors.New("the key's add is not in the ledger after it was recorded")
	}
	// The same rule at the add's own point, now that it is recorded: its
	// point is the one asked for above unless another writer moved main.
	owners, err := ownersAtEvent(s.led, aid, owner.Public())
	if err != nil {
		return fmt.Errorf("the owner at the key's add is not given: %w; the add %s is recorded, and nothing more: the key has no cell and no envelope of its own", err, aid.Short())
	}
	env2, err := key.SealReaders(key.TypeEvent, aid, nil, append(owners, rd.Public()), ae.Body, rnd)
	if err != nil {
		return err
	}
	rec, err := s.car.Begin(s.lock)
	if err != nil {
		return err
	}
	if err := rec.EventEnvelope(aid, ae.Head, env2); err != nil {
		rec.Abandon()
		return err
	}
	rec.Tx().SetSlots(slots)
	if out, err := rec.Commit(); out != vessel.Recorded {
		return fmt.Errorf("the key's cell was not installed: the vessel's commit was %s: %v", out, err)
	}
	// Said only once the key's own passphrase opens the vessel as recorded.
	got, _, err := key.Try(keyPass, s.car.Vessel().Slots(), info.Salt, info.Iter)
	if err != nil || got.Key != id || got.Gen != gen {
		return fmt.Errorf("the key's passphrase does not open the vessel after its cell was recorded (%v); the key is not usable", err)
	}
	fmt.Printf("key %s  %s  gen %d  event %s\n", hex.EncodeToString(id[:]), name, gen, addID[:8])
	return nil
}

// systemReader opens the system reader from the owner's live keyring
// generation, where it is sealed to the owner's reader (0x000B).
func systemReader(s *session) (key.Reader, bool) {
	owner, err := key.ReaderFrom(s.sec.Reader[:])
	if err != nil {
		return key.Reader{}, false
	}
	adds, _ := s.led.Keyring()
	for _, id := range adds {
		e, ok := s.led.Get(id)
		if !ok {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || !k.IsOwner() || len(k.System) == 0 {
			continue
		}
		if priv, err := key.OpenFrom(owner, k.System, key.InfoSystem); err == nil {
			if r, err := key.ReaderFrom(priv); err == nil {
				return r, true
			}
		}
	}
	return key.Reader{}, false
}

// freeCell finds a cell of the vessel that nobody holds: not the owner's
// (its sealed vessel key opens to the owner's reader) and not a live key's
// (it begins with the slot that key's keyring add carries). The others are
// the vessel's random filling. When every cell is held it refuses, before
// anything is recorded (W2-004 R1).
func freeCell(s *session) (int, error) {
	owner, err := key.ReaderFrom(s.sec.Reader[:])
	if err != nil {
		return -1, err
	}
	held := map[string]bool{}
	adds, _ := s.led.Keyring()
	for _, id := range adds {
		e, ok := s.led.Get(id)
		if !ok {
			continue
		}
		if k, err := event.DecodeKeyring(e.Event.Payload); err == nil && len(k.Slot) == key.BlobSize {
			held[string(k.Slot)] = true
		}
	}
	for i, c := range s.car.Vessel().Slots() {
		if len(c) != key.CellSize || held[string(c[:key.BlobSize])] {
			continue
		}
		if _, err := key.OpenFrom(owner, c[key.BlobSize:key.BlobSize+event.SealedKeySize], key.InfoVK); err == nil {
			continue
		}
		return i, nil
	}
	return -1, errors.New("every cell of this vessel is held; revoke a key before adding one")
}

// ownerRoot is the root's signing key, from the owner's own secret only: a
// key's secret, opened by the key's own passphrase, never signs as the root,
// whatever its cell holds. nil in cold custody.
func ownerRoot(sec key.Secret) ed25519.PrivateKey {
	if sec.Key != ([32]byte{}) {
		return nil
	}
	return sec.Signer()
}

// reload reads the ledger again from the carrier, after a recording made
// through a door of its own.
func (s *session) reload() error {
	heads, err := allHeads(s.car)
	if err != nil {
		return err
	}
	_, err = s.led.Extend(event.Fetch(s.car.Get), heads)
	return err
}

func findKey(s *session, want string) ([]liveKey, error) {
	if want == "" {
		return nil, errors.New("--key names the key, by id or name")
	}
	var out []liveKey
	for _, k := range liveKeys(s) {
		if matchKey(k.k, want) {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no live key %q", want)
	}
	return out, nil
}

func keyRevoke(s *session, dir, want, attempt string) error {
	if err := ownerOnly(s); err != nil {
		return err
	}
	gens, err := findKey(s, want)
	if err != nil {
		return err
	}
	for i, g := range gens {
		p, err := event.Keyring{Op: event.KeyringRevoke, Key: g.k.Key, Gen: g.k.Gen, Target: g.event}.Encode()
		if err != nil {
			return err
		}
		a := ""
		if attempt != "" {
			a = fmt.Sprintf("%s/%d", attempt, i)
		}
		if i > 0 {
			if err := s.reload(); err != nil {
				return err
			}
		}
		id, err := record(s, dir, event.VerbKeyring, p, a)
		if err != nil {
			return err
		}
		fmt.Printf("revoked %s gen %d  event %s  (from here on, not in the past)\n", g.k.Name, g.k.Gen, id[:8])
		// K2: writing is a rokh.grant to the key's signer, and taking it
		// back is rokh.revoke of every live grant made to it.
		if g.k.Signer == nil {
			continue
		}
		if err := s.reload(); err != nil {
			return err
		}
		for _, gid := range s.led.ActiveGrants() {
			ge, _ := s.led.Get(gid)
			gr, err := event.DecodeGrant(ge.Event.Payload)
			if err != nil || gr.Open || !bytes.Equal(gr.Subject, g.k.Signer) {
				continue
			}
			p, err := event.Revoke{Target: gid}.Encode()
			if err != nil {
				return err
			}
			a := ""
			if attempt != "" {
				a = fmt.Sprintf("%s/%d/grant/%s", attempt, i, gid.Short())
			}
			rid, err := record(s, dir, event.VerbRevoke, p, a)
			if err != nil {
				return err
			}
			if err := s.reload(); err != nil {
				return err
			}
			fmt.Printf("took back its writing grant %s  event %s\n", gid.Short(), rid[:8])
		}
	}
	return nil
}

// keyRotate is K3 and K5: gen+1 with new key pairs, the previous reader
// sealed to the new one as heir, then the old generation revoked.
func keyRotate(s *session, dir, keyPassFile, want, attempt string) error {
	if err := ownerOnly(s); err != nil {
		return err
	}
	gens, err := findKey(s, want)
	if err != nil {
		return err
	}
	last := gens[len(gens)-1].k
	// The owner does not hold a key's private halves in v1 (each key has
	// its own cell), so the heir seal is made only by a session of the key.
	var heir []byte
	reads := strings.Join(last.Reads, ",")
	if len(last.Reads) == 1 && last.Reads[0] == "" {
		reads = "*"
	}
	addAttempt, revokeAttempt := "", ""
	if attempt != "" {
		addAttempt, revokeAttempt = attempt+"/add", attempt+"/revoke"
	}
	if err := keyAdd(s, dir, keyPassFile, last.Name, reads, last.Signer != nil, "", addAttempt, last.Gen+1, last.Key, heir); err != nil {
		return err
	}
	if err := s.reload(); err != nil {
		return err
	}
	var old []liveKey
	for _, g := range gens {
		old = append(old, g)
	}
	for i, g := range old {
		p, err := event.Keyring{Op: event.KeyringRevoke, Key: g.k.Key, Gen: g.k.Gen, Target: g.event}.Encode()
		if err != nil {
			return err
		}
		a := ""
		if revokeAttempt != "" {
			a = fmt.Sprintf("%s/%d", revokeAttempt, i)
		}
		if _, err := record(s, dir, event.VerbKeyring, p, a); err != nil {
			return err
		}
		if err := s.reload(); err != nil {
			return err
		}
	}
	fmt.Printf("rotated %s to gen %d\n", last.Name, last.Gen+1)
	return nil
}

// cmdView shows the three layers and nothing else of the layout (contract
// section 6): Star, the rokh itself; Planet, the first address component;
// Moon, the second.
func cmdView(args []string) error {
	fs, dir, err := newFlags("view", args)
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pass, err := passphrase(fs)
	if err != nil {
		return err
	}
	s, err := openSession(dir, pass)
	if err != nil {
		return err
	}
	defer s.Close()
	planets := map[string]map[string]int{}
	system := 0
	for _, id := range s.led.Order() {
		e, _ := s.led.Get(id)
		if e.System {
			system++
			continue
		}
		parts := strings.SplitN(e.Event.Address, "/", 3)
		if planets[parts[0]] == nil {
			planets[parts[0]] = map[string]int{}
		}
		moon := ""
		if len(parts) > 1 {
			moon = parts[1]
		}
		planets[parts[0]][moon]++
	}
	adds, seeds := s.led.Keyring()
	fmt.Printf("Star    %s  %d system events, %d keys, %d seeds\n", s.led.Genesis(), system, len(adds), len(seeds))
	names := make([]string, 0, len(planets))
	for p := range planets {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		fmt.Printf("Planet  %s\n", p)
		moons := make([]string, 0, len(planets[p]))
		for m := range planets[p] {
			moons = append(moons, m)
		}
		sort.Strings(moons)
		for _, m := range moons {
			label := m
			if label == "" {
				label = "(the planet itself)"
			}
			fmt.Printf("  Moon  %s  %d events\n", label, planets[p][m])
		}
	}
	return nil
}

func init() {
	v1Commands["key"] = cmdKey
	v1Commands["view"] = cmdView
	// The key layer's hooks for the vessel commands of v1 (v1_vessel.go).
	v1Unlock = func(pass string) vessel.Unlock { return vessel.Unlock(key.Unlock(pass)) }
	v1Sealer = func(c *carrier.Carrier, pass string) (carrier.Sealer, error) {
		sess, _, _, err := keyLayerOf(c, pass)
		if err != nil {
			return nil, err
		}
		return sess, nil
	}
	v1Judge = func(c *carrier.Carrier, pass string) (func(frame.ID) string, func(frame.ID, []byte) bool, error) {
		_, _, k, err := keyLayerOf(c, pass)
		if err != nil {
			return nil, nil, err
		}
		return k.judge, k.covers, nil
	}
	v1Merge = func(c *carrier.Carrier, pass string, heads []frame.ID) (lineage.Event, error) {
		_, sec, err := ownerSession(c, pass)
		if err != nil {
			return lineage.Event{}, err
		}
		root := ownerRoot(sec)
		if root == nil {
			return lineage.Event{}, errors.New("the owner's signing key is in cold custody; a merge needs it")
		}
		anchor := c.Anchor()
		e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: heads,
			Address: event.AddressRoot, Verb: event.VerbMerge, Attest: stamp()}, root)
		if err != nil {
			return lineage.Event{}, err
		}
		return lineage.Event{ID: e.ID, Head: e.Head, Body: e.Body, Address: e.Event.Address}, nil
	}
}

// ownerSession opens the owner's session and secret for an opened vessel.
func ownerSession(c *carrier.Carrier, pass string) (*key.Session, key.Secret, error) {
	v := c.Vessel()
	info := v.Info()
	return key.VesselSession(pass, v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
}
