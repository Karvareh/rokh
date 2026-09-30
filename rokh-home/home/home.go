// Package home is a Rokh home as its gate holds it open: the sealed store, the
// owner's ledger, the catalog of items, the registry of programs, the keys held
// for them and the index — and every read, write and handing-out decided
// against the asking program's authority before anything is read or done.
//
// Two doors onto the one ledger are kept in this process. The owner's door may
// sign with the root key, and it is reached only by the owner. The programs'
// door never signs with the root: it signs with the key the home holds for the
// asking program, under the ledger grant the owner made for that key, and the
// ledger judges that grant in the event's causal past. Neither door is a socket
// a program can reach.
package home

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"rokh-home/authority"
	"rokh-home/index"
	"rokh-home/store"
	"rokh/carrier"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
	"rokh/oracle"
)

// Layout and formats.
const (
	LedgerDir      = "ledger"
	CatalogFormat  = "rokh-home.catalog/1"
	RecordFormat   = "rokh-home.record/1"
	RecordType     = "application/vnd.rokh.home.record+json"
	ItemsAddress   = "home/items"
	TasksAddress   = "home/tasks"
	OwnerID        = "owner"
	OwnerDoor      = "rokh-home/owner"
	ProgramDoor    = "rokh-home/program"
	defaultBranch  = "main"
	ptrCatalog     = "catalog"
	ptrRegistry    = "registry"
	ptrKeys        = "keys"
	ptrPlaces      = "places"
	ptrJournal     = "journal/"
	ptrDisclosure  = "disclosure/"
	kindBlob       = "blob"
	kindTree       = "tree"
	kindSheet      = "sheet"
	kindRecord     = "record"
	maxInlineBytes = 1 << 20
)

// Actor is who is asking: the owner, or a program in a session opened at a
// version of its authority.
type Actor struct {
	Owner    bool
	Consumer string
	Session  uint64
}

// OwnerActor is the owner.
var OwnerActor = Actor{Owner: true}

// Denied is a refusal with the decision behind it. It names what was asked for
// and why it was refused, and never whether the thing asked for exists.
type Denied struct {
	Decision authority.Decision
}

func (d *Denied) Error() string {
	return fmt.Sprintf("home: %s is not allowed on %q (%s)", d.Decision.Action, d.Decision.Path, d.Decision.Reason)
}

// ErrNotFound is returned for a path inside the asker's scope that holds
// nothing.
var ErrNotFound = errors.New("home: nothing is at that path")

// Places are the folders a program may bring files in from and hand files out
// to. The owner sets them; a program cannot name any other.
type Places struct {
	Intake   string `json:"intake,omitempty"`
	Handover string `json:"handover,omitempty"`
}

type heldKey struct {
	Priv     []byte `json:"priv"`
	Grant    string `json:"grant"`
	Attempt  string `json:"attempt,omitempty"`
	Consumer string `json:"consumer"`
	Scope    string `json:"scope"`
}

// keyring is the keys the home holds for its programs. The home reads and
// changes Keys under its own lock; the doors read it through Key and Names,
// and the program's door does so while a program waits on its shelf, when the
// home's lock is not held. So the map is never changed in place: publish
// swaps in a new one under the keyring's own lock, which Key and Names take.
type keyring struct {
	mu   sync.RWMutex
	Keys map[string]heldKey `json:"keys"`
	// Readers are the reading halves of the programs' own keys, by program
	// (programkeys.go); System is the home's system reader. Both are kept
	// sealed here beside the signing keys, and never leave the home.
	Readers map[string]heldReader `json:"readers,omitempty"`
	System  []byte                `json:"system,omitempty"`
}

// publish makes next the held keys.
func (k *keyring) publish(next map[string]heldKey) {
	k.mu.Lock()
	k.Keys = next
	k.mu.Unlock()
}

// wipe zeroes every held private key.
func (k *keyring) wipe() {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, v := range k.Keys {
		for i := range v.Priv {
			v.Priv[i] = 0
		}
	}
	for _, v := range k.Readers {
		for i := range v.Reader {
			v.Reader[i] = 0
		}
	}
	for i := range k.System {
		k.System[i] = 0
	}
}

func (k *keyring) Key(name string) (ed25519.PrivateKey, *frame.ID, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	v, ok := k.Keys[name]
	if !ok {
		return nil, nil, false
	}
	id, err := frame.ParseID(v.Grant)
	if err != nil {
		return nil, nil, false
	}
	return append(ed25519.PrivateKey(nil), v.Priv...), &id, true
}

func (k *keyring) Names() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]string, 0, len(k.Keys))
	for n := range k.Keys {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Catalog is every item and every open draft.
type Catalog struct {
	Format string            `json:"format"`
	Items  map[string]*Item  `json:"items"`
	Paths  map[string]string `json:"paths"`
	Drafts map[string]*Draft `json:"drafts"`
}

// Item is one thing a home keeps, with every version of it.
type Item struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Names    []string  `json:"names"`
	Context  string    `json:"context,omitempty"`
	Versions []Version `json:"versions"`
}

// Version is one recorded version of an item.
type Version struct {
	N        int      `json:"n"`
	Kind     string   `json:"kind"`
	SHA256   string   `json:"sha256"`
	Size     int64    `json:"size"`
	Sheets   []string `json:"sheets"`
	Record   string   `json:"record"`
	Event    string   `json:"event"`
	By       string   `json:"by"`
	Via      string   `json:"via"`
	Previous int      `json:"previous,omitempty"`
}

// Draft is work not yet recorded.
type Draft struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Base     int    `json:"base"`
	Revision int    `json:"revision"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Blob     string `json:"blob"`
	By       string `json:"by"`
	State    string `json:"state"`
}

// Home is an opened home.
type Home struct {
	// mu is held alone by whatever changes the home — a recording, a grant,
	// an import — and shared by whatever only looks: several programs
	// reading at once are answered at once, and none of them waits for
	// another reader.
	mu         sync.RWMutex
	jobsMu     sync.Mutex
	jobsWait   sync.WaitGroup
	closing    bool
	root       string
	st         *store.Home
	car        *carrier.Carrier
	ownerDoor  *daemon.Server
	progDoor   *daemon.Server
	cat        *Catalog
	reg        *authority.Registry
	keys       *keyring
	places     map[string]Places
	regKey     []byte
	rootPub    ed25519.PublicKey
	idx        *index.Index
	jobs       map[string]func()
	release    string
	brokerLock *os.File
	replica    ReplicaInfo
	// layer is the owner's key layer over the home's ledger (keyview): it
	// judges every record at its own point and gives the session a record is
	// sealed with; sec is the owner's secret it was opened with.
	layer *keyview.Layer
	sec   key.Secret
	// views are the doors over each program's own view (programkeys.go).
	views programViews
	// signs says the owner's cell of this ledger holds the root's signing
	// seed; a reading mirror's holds none.
	signs bool
}

// Create makes a new home at root with its own ledger, whose first page is the
// owner's genesis sentence. The iteration count may only be lowered by tests.
func Create(root, pass, genesis string, iter ...int) error {
	if pass == "" {
		return errors.New("home: an empty passphrase is not allowed")
	}
	dir := filepath.Join(root, LedgerDir)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	_, rootPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	att, _ := oracle.Observe(oracle.Default())
	gen, err := event.SignFresh(event.Event{Address: event.AddressRoot, Verb: event.VerbGenesis,
		Payload: []byte(genesis), Attest: att}, rootPriv)
	if err != nil {
		return err
	}
	var seed [32]byte
	copy(seed[:], rootPriv.Seed())
	if err := createLedger(dir, pass, gen, seed, gen.ID, iter); err != nil {
		return err
	}
	// The home keeps no store beside its ledger's vessel (contract 2.10): its
	// catalog, registry, keys and places are records of the home's spaces.
	car, _, err := openLedger(dir, pass)
	if err != nil {
		return err
	}
	st, err := store.On(root, car.Vessel(), pass)
	if err != nil {
		return err
	}
	defer st.Close()
	for name, v := range map[string]any{
		ptrCatalog:  &Catalog{Format: CatalogFormat, Items: map[string]*Item{}, Paths: map[string]string{}, Drafts: map[string]*Draft{}},
		ptrRegistry: authority.New(),
		ptrKeys:     &keyring{Keys: map[string]heldKey{}},
		ptrPlaces:   map[string]Places{},
	} {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err := st.SetPointer(name, b); err != nil {
			return err
		}
	}
	return nil
}

// Open opens a home with its passphrase.
func Open(root, pass, release string) (*Home, error) {
	if _, err := os.Lstat(filepath.Join(root, "INCOMPLETE")); err == nil {
		return nil, errors.New("home: this replica is incomplete; the source is unchanged")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock, err := holdHome(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, LedgerDir)
	car, sec, err := openLedger(dir, pass)
	if err != nil {
		lock.Close()
		if errors.Is(err, key.ErrPassphrase) {
			return nil, fmt.Errorf("%w: %v", store.ErrPass, err)
		}
		return nil, fmt.Errorf("home: the ledger does not open: %w", err)
	}
	st, err := store.On(root, car.Vessel(), pass)
	if err != nil {
		lock.Close()
		return nil, err
	}
	fail := func(err error) (*Home, error) {
		st.Close()
		lock.Close()
		return nil, err
	}
	h := &Home{root: root, st: st, car: car, jobs: map[string]func(){}, release: release, brokerLock: lock}
	load := func(name string, v any) error {
		b, ok, err := st.Pointer(name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("home: the %s is missing", name)
		}
		return json.Unmarshal(b, v)
	}
	h.cat, h.reg, h.keys = &Catalog{}, &authority.Registry{}, &keyring{}
	for name, v := range map[string]any{ptrCatalog: h.cat, ptrRegistry: h.reg, ptrKeys: h.keys, ptrPlaces: &h.places} {
		if err := load(name, v); err != nil {
			return fail(err)
		}
	}
	if err := h.reg.Check(); err != nil {
		return fail(err)
	}
	h.replica = ReplicaInfo{Format: ReplicaFormat, Kind: WriterReplica}
	if b, exists, err := st.Pointer(ptrReplica); err != nil {
		return fail(err)
	} else if exists {
		if err := json.Unmarshal(b, &h.replica); err != nil || h.replica.Format != ReplicaFormat ||
			(h.replica.Kind != WriterReplica && h.replica.Kind != MirrorReplica) {
			return fail(errors.New("home: unsupported replica profile"))
		}
	}
	if h.replica.Kind == MirrorReplica && (len(h.keys.Keys) != 0 || rootOf(sec) != nil) {
		return fail(errors.New("home: a reading mirror must contain no signing secrets"))
	}
	if h.cat.Drafts == nil {
		h.cat.Drafts = map[string]*Draft{}
	}
	if h.places == nil {
		h.places = map[string]Places{}
	}
	if h.regKey, err = st.SubKey("registry"); err != nil {
		return fail(err)
	}
	// The owner's key layer (T2): every record is judged at its own point,
	// and the doors seal each record to the readers of its point, so what is
	// recorded in a program's namespace is sealed to the program's own key.
	sess, _, layer, err := keyview.OwnerSessionOf(car, sec)
	if err != nil {
		return fail(fmt.Errorf("home: the owner's key layer does not open: %w", err))
	}
	car.SetSealer(sess)
	h.layer, h.sec = layer, sec
	l1, err := loadLedger(car)
	if err != nil {
		return fail(err)
	}
	// The program's door reads the carrier on an opening of its own, so a
	// refresh by one door never moves the vessel under the other's reading
	// of it (T10, T2): each door's view and its answers are of one reading.
	v2, _, err := car.Vessel().Reopen()
	if err != nil {
		return fail(err)
	}
	car2 := carrier.Wrap(v2, sess)
	l2, err := loadLedger(car2)
	if err != nil {
		return fail(err)
	}
	h.rootPub = l1.Root()
	// Each door leaves its name on what it records — the registrar's mark,
	// beside the key that signed and the grant it signed under.
	h.ownerDoor = daemon.New(car, l1, daemon.Options{AllowSign: h.replica.Kind == WriterReplica, Dir: dir, Release: release, Door: OwnerDoor, Named: h.keys, Root: rootOf(sec), SealerAt: h.sealerAt})
	h.signs = rootOf(sec) != nil
	h.progDoor = daemon.New(car2, l2, daemon.Options{AllowSign: h.replica.Kind == WriterReplica, NoRoot: true, Dir: dir, Release: release, Keys: h.keys, Door: ProgramDoor, SealerAt: h.sealerAt})
	if err := h.recoverCatalog(); err != nil {
		return fail(err)
	}
	h.reindex()
	return h, nil
}

// Close gives back everything the home holds in memory.
func (h *Home) Close() {
	h.jobsMu.Lock()
	h.closing = true
	for _, stop := range h.jobs {
		stop()
	}
	h.jobsMu.Unlock()
	h.jobsWait.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.keys != nil {
		h.keys.wipe()
	}
	h.st.Close()
	if h.brokerLock != nil {
		h.brokerLock.Close()
		h.brokerLock = nil
	}
}

// CancelWork withdraws active import execution without erasing resumable work.
func (h *Home) CancelWork() {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	for _, stop := range h.jobs {
		stop()
	}
}

// Root is the home's folder.
func (h *Home) Root() string { return h.root }

// Health is non-nil after storage publication became uncertain. The gate must
// stop serving this opening and let the owner explicitly reopen disk state.
func (h *Home) Health() error { return h.st.Health() }

// LedgerDir is the folder of the owner's ledger.
func (h *Home) LedgerPath() string { return filepath.Join(h.root, LedgerDir) }

func (h *Home) save(name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return h.st.SetPointer(name, b)
}

func ask(door *daemon.Server, req map[string]any) map[string]any {
	b, err := json.Marshal(req)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "code": "bad_request"}
	}
	return door.Handle(b)
}

// OwnerLedger answers a request on the owner's door. It is for the owner's own
// channel only.
func (h *Home) OwnerLedger(req map[string]any) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.Health(); err != nil {
		return map[string]any{"ok": false, "code": "durability_unknown", "error": err.Error(), "reopen_required": true}
	}
	if h.replica.Kind == MirrorReplica {
		switch req["op"] {
		case "status", "capabilities", "log", "get", "announce", "bound", "answer", "attempt", "receipts":
		default:
			return map[string]any{"ok": false, "record": "not-recorded", "code": "denied", "reason": "read_only_replica"}
		}
	}
	return ask(h.ownerDoor, req)
}

func (h *Home) decide(a Actor, action authority.Action, path string) authority.Decision {
	if h.Health() != nil {
		return authority.Decision{Consumer: actorID(a), Action: action, Path: path, Reason: "durability_unknown"}
	}
	if h.replica.Kind == MirrorReplica && !mirrorAction(action) {
		return authority.Decision{Consumer: actorID(a), Action: action, Path: path, Reason: "read_only_replica"}
	}
	if a.Owner {
		return authority.Decision{Allowed: true, Consumer: OwnerID, Name: OwnerID, Action: action,
			Path: path, Reason: authority.Allowed}
	}
	return h.reg.Decide(a.Consumer, a.Session, action, path)
}

func (h *Home) require(a Actor, action authority.Action, path string) (authority.Decision, error) {
	if err := h.Health(); err != nil {
		return authority.Decision{}, err
	}
	d := h.decide(a, action, path)
	if !d.Allowed {
		return d, &Denied{Decision: d}
	}
	return d, nil
}

func ownerOnly(a Actor, what string) error {
	if a.Owner {
		return nil
	}
	return &Denied{Decision: authority.Decision{Consumer: a.Consumer, Action: authority.Action(what),
		Path: authority.WholeHome, Reason: "owner_only"}}
}

// Authenticate finds the program a credential belongs to and the version of
// its authority a session opened now is bound to.
func (h *Home) Authenticate(credential []byte) (authority.Consumer, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.Health() != nil {
		return authority.Consumer{}, false
	}
	c, ok := h.reg.Authenticate(h.regKey, credential)
	if !ok {
		return authority.Consumer{}, false
	}
	return *c, true
}

// Consumer returns a program's registration as it stands.
func (h *Home) Consumer(id string) (authority.Consumer, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.Health() != nil {
		return authority.Consumer{}, false
	}
	c, ok := h.reg.Get(id)
	if !ok {
		return authority.Consumer{}, false
	}
	return *c, true
}

// eventFacts is what the ledger says about an event, for a sheet's check.
func (h *Home) eventFacts(id string) (factsAccepted, factsByOwner bool, address, verb string, found bool) {
	r := ask(h.ownerDoor, map[string]any{"op": "get", "id": id})
	if ok, _ := r["ok"].(bool); !ok {
		return false, false, "", "", false
	}
	raw, err := hex.DecodeString(fmt.Sprint(r["raw"]))
	if err != nil {
		return false, false, "", "", false
	}
	e, err := event.Parse(raw)
	if err != nil {
		return false, false, "", "", false
	}
	return r["state"] == "accepted", bytes.Equal(e.Event.Author, h.rootPub), e.Event.Address, e.Event.Verb, true
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func newID() string { return authority.NewID() }

// OwnerProofKey is the key the owner's channel is authenticated with, derived
// from the passphrase when the home was opened.
func (h *Home) OwnerProofKey() []byte { return h.st.OwnerProofKey() }

// Sealed is this home's sealed store, for a layer of the broker that keeps its
// own secrets and state there. It is not reachable from any socket: the gate
// hands it to the runtime manager it starts, and to nothing else. The home's
// own work does not depend on anything that layer does.
func (h *Home) Sealed() *store.Home { return h.st }

// Summary is what the owner's status shows: counts and the ledger's own
// status, and nothing a program could not already see of its own.
func (h *Home) Summary() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := ask(h.ownerDoor, map[string]any{"op": "status"})
	versions, open := 0, 0
	for _, it := range h.cat.Items {
		versions += len(it.Versions)
	}
	for _, d := range h.cat.Drafts {
		if d.State == "open" {
			open++
		}
	}
	return map[string]any{"items": len(h.cat.Items), "versions": versions, "open_drafts": open,
		"consumers": len(h.reg.Consumers), "ledger": map[string]any{"anchor": st["anchor"],
			"accepted": st["accepted"], "heads": st["heads"]}}
}
