package proof

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"rokh/carrier"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
)

// The host gives the core its randomness (contract C5): an event's freshness
// and its body's salt come from event.Entropy and nowhere else. The daemon
// this package imports sets it too; the fixture does not lean on that.
func init() {
	if event.Entropy == nil {
		event.Entropy = rand.Reader
	}
}

// testIter is the key-derivation round count for a carrier that exists for
// one test and is thrown away. Never a real number; see vessel.DefaultIter.
const testIter = 1000

// pass is the synthetic passphrase every fixture here uses.
const pass = "proof"

// world is one ledger on one carrier, with the root key in hand, built for
// attacking. Everything is signed explicitly: no helper here decides an
// authority, a parent or a scope on a test's behalf, because those are the
// very things under test.
type world struct {
	place
	t       *testing.T
	car     *carrier.Carrier
	root    ed25519.PrivateKey
	rootPub ed25519.PublicKey
	gen     event.Signed
	anchor  frame.ID
	keys    *heldKeys
}

// newWorld builds a world in a carrier folder on the drive, with the kernel's
// writer's turn: the carrier as it ships.
func newWorld(t *testing.T) *world {
	t.Helper()
	return newWorldAt(t, onFolder(t))
}

// newFastWorld is the same carrier with its vessel in memory, for the tests
// that record hundreds of events and are not about the drive or the folder's
// turn. See place for why that is honest here and nowhere else.
func newFastWorld(t *testing.T) *world {
	t.Helper()
	return newWorldAt(t, inMemory())
}

func newWorldAt(t *testing.T, p place) *world {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := event.SignFresh(event.Event{
		Author: pub, Address: event.AddressRoot, Verb: event.VerbGenesis,
	}, priv)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{place: p, t: t, root: priv, rootPub: pub, gen: gen, anchor: gen.ID,
		keys: &heldKeys{}}
	w.car = p.create(t, gen.ID, priv)
	w.record(gen, "main")
	return w
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// sign builds one event. Every field a verdict depends on is the caller's.
func (w *world) sign(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID,
	addr, verb string, payload []byte, attest ...event.Attestation) event.Signed {
	w.t.Helper()
	a := w.anchor
	s, err := event.SignFresh(event.Event{
		Carrier: &a, Authority: auth, Parents: parents,
		Address: addr, Verb: verb, Payload: payload, Attest: attest,
	}, priv)
	if err != nil {
		w.t.Fatalf("sign %s/%s: %v", addr, verb, err)
	}
	return s
}

// grantBy writes a grant under whatever authority the signer holds. A nil
// authority with the root key is the root's own grant; anything else is a
// sub-grant and is judged as one.
func (w *world) grantBy(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID,
	subject ed25519.PublicKey, scope string, verbs []string, canDelegate bool) event.Signed {
	w.t.Helper()
	p, err := event.Grant{Subject: subject, Scope: scope, Verbs: verbs, CanDelegate: canDelegate}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.sign(priv, auth, parents, event.AddressRoot, event.VerbGrant, p)
}

// grant is the root's own grant.
func (w *world) grant(parents []frame.ID, subject ed25519.PublicKey,
	scope string, verbs []string, canDelegate bool) event.Signed {
	w.t.Helper()
	return w.grantBy(w.root, nil, parents, subject, scope, verbs, canDelegate)
}

func (w *world) revokeBy(priv ed25519.PrivateKey, auth *frame.ID, parents []frame.ID, target frame.ID) event.Signed {
	w.t.Helper()
	p, err := event.Revoke{Target: target}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.sign(priv, auth, parents, event.AddressRoot, event.VerbRevoke, p)
}

// record puts an event on the carrier and moves a branch to it. In v1 the
// two are one recording and one vessel commit: before its head file is
// written nothing, after it both (contract 2.6).
func (w *world) record(s event.Signed, branch string) event.Signed {
	w.t.Helper()
	w.commit(func(r *carrier.Recording) error {
		if err := r.Event(s.ID, s.Head, s.Body, s.Event.Address); err != nil {
			return err
		}
		return r.SetRef(branch, s.ID)
	})
	return s
}

// commit makes one recording on this world's carrier under the writer's
// hold, and insists that it was recorded.
func (w *world) commit(fill func(*carrier.Recording) error) {
	w.t.Helper()
	commitOn(w.t, w.place, w.car, fill)
}

// hold gives a door the named key and the grant it signs under. The 0.9
// carrier kept such keys in a keyring file; a v1 carrier keeps no private
// key, and a door signs with the keys the program that embeds it holds
// (daemon.Options.Keys).
func (w *world) hold(name string, priv ed25519.PrivateKey, authority frame.ID) {
	w.t.Helper()
	w.keys.put(name, priv, authority)
}

// at gives a door on this world what the 0.9 carrier gave every door through
// its keyring, and the writer's hold of the place: the root's signing key
// (the owner opened this carrier with the owner's passphrase), the named keys
// held for this world, and the folder's turn or the process's own hold.
func (w *world) at(o daemon.Options) daemon.Options {
	o = w.place.door(o)
	o.Root = append(ed25519.PrivateKey(nil), w.root...)
	o.Keys = w.keys
	return o
}

// fresh builds a ledger holding nothing but the genesis.
func (w *world) fresh() *ledger.Ledger {
	w.t.Helper()
	l, err := ledger.New(w.gen.Raw)
	if err != nil {
		w.t.Fatal(err)
	}
	return l
}

// load reads the ledger back from the carrier by following the references —
// the recorded truth, whatever a live view says. An opened v1 carrier keeps
// its vessel's state in memory, so the truth is read by opening the place
// again, not through the carrier the doors write with.
func (w *world) load() *ledger.Ledger {
	w.t.Helper()
	return loadFrom(w.t, w.reopen())
}

func loadFrom(t *testing.T, c *carrier.Carrier) *ledger.Ledger {
	t.Helper()
	refs, err := c.Refs()
	if err != nil {
		t.Fatal(err)
	}
	heads := make([]frame.ID, 0, len(refs))
	for _, id := range refs {
		heads = append(heads, id)
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].Compare(heads[j]) < 0 })
	raw, err := c.Get(c.Anchor())
	if err != nil {
		t.Fatal(err)
	}
	l, err := ledger.Load(raw, c.Get, heads)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// reopen opens the same carrier a second time, as another process would: its
// own vessel state, its own session, nothing shared but the place.
func (w *world) reopen() *carrier.Carrier {
	w.t.Helper()
	c, _ := w.open(w.t)
	return c
}

// heldKeys is a door's source of named keys: the keys a program that embeds
// the door keeps, each with the grant it signs under.
type heldKeys struct {
	mu   sync.Mutex
	keys map[string]heldKey
}

type heldKey struct {
	priv      ed25519.PrivateKey
	authority frame.ID
}

func (h *heldKeys) put(name string, priv ed25519.PrivateKey, authority frame.ID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.keys == nil {
		h.keys = map[string]heldKey{}
	}
	h.keys[name] = heldKey{priv: append(ed25519.PrivateKey(nil), priv...), authority: authority}
}

// Key hands out a copy: a door wipes what it is given after use.
func (h *heldKeys) Key(name string) (ed25519.PrivateKey, *frame.ID, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k, ok := h.keys[name]
	if !ok {
		return nil, nil, false
	}
	a := k.authority
	return append(ed25519.PrivateKey(nil), k.priv...), &a, true
}

func (h *heldKeys) Names() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.keys))
	for n := range h.keys {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ---------- daemon helpers ----------

func ask(t *testing.T, s *daemon.Server, req map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return s.Handle(b)
}

func mustOK(t *testing.T, r map[string]any, why string) map[string]any {
	t.Helper()
	if ok, _ := r["ok"].(bool); !ok {
		t.Fatalf("%s: %v", why, r)
	}
	return r
}

// refused asserts the shape every refusal owes: no ok, a stable code, and a
// sentence for a person.
//
//	— T12.3
func refused(t *testing.T, r map[string]any, code string) map[string]any {
	t.Helper()
	if ok, _ := r["ok"].(bool); ok {
		t.Fatalf("expected refusal %q, got %v", code, r)
	}
	if r["code"] != code {
		t.Fatalf("code = %v, want %q (%v)", r["code"], code, r["error"])
	}
	if s, _ := r["error"].(string); s == "" {
		t.Fatalf("a refusal still owes a sentence: %v", r)
	}
	return r
}

// notRecorded is refused plus the one word a writing request owes.
//
//	— T8.5, T12.3
func notRecorded(t *testing.T, r map[string]any, code string) map[string]any {
	t.Helper()
	refused(t, r, code)
	if r["record"] != daemon.NotRecorded {
		t.Fatalf("record = %v, want %q: %v", r["record"], daemon.NotRecorded, r)
	}
	return r
}

func recorded(t *testing.T, r map[string]any, why string) map[string]any {
	t.Helper()
	mustOK(t, r, why)
	if r["record"] != daemon.Recorded {
		t.Fatalf("%s: record = %v, want %q: %v", why, r["record"], daemon.Recorded, r)
	}
	return r
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }

// witness is the five an intent owes. The sixth is the ledger's to fill.
//
//	— T10.2
func witness() map[string]any {
	return map[string]any{"origin": "the proof suite", "authority": "the root key",
		"audience": "this test", "state": "synthetic", "wayBack": "delete the temp directory"}
}

// shortDir gives a directory well inside the 104-byte sun_path limit, which
// the usual test temp directory is not.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := shortTemp(t, "rkp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// shortTemp is a folder for sockets with a short path, inside the work tree's
// short folder (ROKH_SHORT_TMP, or the work tree's .t); never a literal /tmp.
func shortTemp(t *testing.T, prefix string) (string, error) {
	t.Helper()
	base := os.Getenv("ROKH_SHORT_TMP")
	if base == "" {
		base = filepath.Join("..", "..", "..", "..", "..", "..", "..", ".t")
	}
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		t.Skip("no short socket folder in this work tree")
	}
	return os.MkdirTemp(base, prefix)
}
