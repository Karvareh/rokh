// Package keyview is the key layer over an opened carrier: which session a
// passphrase opens (the owner's, or a key's own, never the owner's for a
// key), the view that session holds, and the one rule for the owner
// generations at a record's own point (contract 3.3, E3, E4, 4.4).
//
// Every record is judged at its own point: an event at its parents, content
// at the event that describes it. A point the ledger cannot give is refused,
// and nothing stands in for it; an owner fold emptied by revocation is
// refused too, and only where no owner generation was ever added does the
// owner's first generation stand in.
//
// A view's ledger is read with the view's own readers only: every event's
// body is opened at its own point, after everything it rests on, and kept
// only where its envelope names the owner generations live there. What does
// not open is a head only: lineage, no body.
//
// It is the one key layer of the command line, the sentence surface and the
// booths. It reads the carrier only through its public door and holds no
// file, clock or lock of its own.
package keyview

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"rokh/carrier"
	"rokh/content"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/ledger"
	"rokh/vessel"
)

// ErrOwnerEmptied refuses a point whose owner fold was emptied by
// revocation: an owner generation was added in its past and none is live
// there. It is known and empty, and nothing stands in for it (R4).
var ErrOwnerEmptied = fmt.Errorf("%w: every owner generation added before this point was revoked there, and nothing stands in for an owner fold emptied by revocation (R4)", key.ErrOwnerUnknown)

// ErrNoEventYet is a vessel that holds no event at all: a seed whose command
// was cut after it made the folder and before any event arrived (goal 7.41).
// It opens with the owner's passphrase, so the commands that act on the
// vessel (grow, shrink) work there; it has no ledger, so a key's passphrase
// opens no session there.
var ErrNoEventYet = errors.New("holds no event yet (a seed cut before its first event arrived: the same rokh seed SRC DIR goes on with it, and grow and shrink open it)")

// ErrNoSystemReader is a key whose own add in this carrier seals it no system
// reader: it cannot read the system layer, so it opens no session.
var ErrNoSystemReader = errors.New("this key's generation gives it no system reader in this carrier, so it opens no session here")

// ---------- the rule of a point ----------

// OwnersOf is the one rule for the owner's reader generations at a point,
// given the ledger's fold there (ledger.OwnerFoldAt), in three states and an
// unknown (R4):
//   - owner generations live at the point: they are the owners;
//   - none live and none ever added before it: the first bootstrap (the
//     genesis, the owner's own first add, or a carrier made without one).
//     Only here does first, the owner's first generation, stand in;
//   - none live where one was added: emptied by revocation, refused;
//   - a point this ledger does not know: refused.
func OwnersOf(readers [][]byte, ever, known bool, first []byte) ([][]byte, error) {
	switch {
	case !known:
		return nil, key.ErrOwnerUnknown
	case len(readers) > 0:
		return readers, nil
	case ever:
		return nil, ErrOwnerEmptied
	case len(first) == 0:
		return nil, fmt.Errorf("%w: no owner generation was added before this point, and no first generation is held here to stand in", key.ErrOwnerUnknown)
	}
	return [][]byte{first}, nil
}

// OwnersAtPoint is OwnersOf at the point named by parents: the point of an
// event signed on them. No parents is the genesis's point.
func OwnersAtPoint(l *ledger.Ledger, parents []frame.ID, first []byte) ([][]byte, error) {
	readers, ever, known := l.OwnerFoldAt(parents...)
	return OwnersOf(readers, ever, known, first)
}

// OwnersAtEvent is OwnersOf at an accepted event's own point: its parents.
func OwnersAtEvent(l *ledger.Ledger, id frame.ID, first []byte) ([][]byte, error) {
	e, ok := l.Get(id)
	if !ok || l.State(id) != ledger.Accepted {
		return nil, fmt.Errorf("%w: %s is not accepted here", key.ErrOwnerUnknown, id.Short())
	}
	return OwnersAtPoint(l, e.Event.Parents, first)
}

// ReadOpenAt says whether a live open grant makes an address readable by
// every key, as seen from given points of a ledger; no point is its heads
// (contract 4.5).
func ReadOpenAt(l *ledger.Ledger, addr string, at ...frame.ID) bool {
	for _, id := range l.OpenGrants(at...) {
		e, _ := l.Get(id)
		if g, err := event.DecodeGrant(e.Event.Payload); err == nil && g.Read && event.ScopeCovers(g.Scope, addr) {
			return true
		}
	}
	return false
}

// RingAt is the keyring's fold at given points (no point: the heads): every
// live generation there (contract 4.4).
func RingAt(l *ledger.Ledger, at ...frame.ID) (key.Ring, error) {
	adds, _ := l.Keyring(at...)
	return key.Fold(adds, func(id frame.ID) ([]byte, bool) {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly {
			return nil, false
		}
		return e.Event.Payload, true
	})
}

// BootstrapOwner is the owner's first generation as the ledger holds it: the
// owner's generation-1 add, accepted here (so signed by the root, K1), at
// whose own point no owner generation had been added before; `rokh init`
// records it with the genesis. It stands for the owner at the points before
// any owner generation. Two such adds that name different readers leave the
// first owner unknown.
func BootstrapOwner(l *ledger.Ledger) (key.Gen, error) {
	var first key.Gen
	for _, id := range l.Order() {
		e, _ := l.Get(id)
		if e.HeadOnly || !e.System || e.Event.Verb != event.VerbKeyring {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil || k.Op != event.KeyringAdd || !k.IsOwner() || k.Gen != 1 || len(k.Reader) == 0 {
			continue
		}
		if ever, known := l.OwnerEverAt(id); !known || ever {
			continue
		}
		if len(first.Reader) > 0 && !bytes.Equal(first.Reader, k.Reader) {
			return key.Gen{}, fmt.Errorf("%w: two first owner generations name different readers", key.ErrOwnerUnknown)
		}
		if len(first.Reader) == 0 {
			first = key.Gen{Event: id, Keyring: k}
		}
	}
	if len(first.Reader) == 0 {
		return key.Gen{}, fmt.Errorf("%w: this carrier's keyring names no first owner generation", key.ErrOwnerUnknown)
	}
	return first, nil
}

// AllHeads is every branch tip the carrier names, sorted: the commit points
// a ledger is read from.
func AllHeads(c *carrier.Carrier) ([]frame.ID, error) {
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

// errFound stops a scan at the first record it looks for.
var errFound = errors.New("found")

// HoldsEvents says whether a vessel holds any event record, whole or a head
// only. It opens nothing and stops at the first.
func HoldsEvents(c *carrier.Carrier) (bool, error) {
	err := c.Vessel().Scan(func(h vessel.Header, _ vessel.Ref) error {
		if h.Type == vessel.RecEvent {
			return errFound
		}
		return nil
	})
	if errors.Is(err, errFound) {
		return true, nil
	}
	return false, err
}

// ---------- reading a view ----------

// reading opens event bodies for a view: with the view's own readers only,
// each at its own point.
type reading struct {
	c       *carrier.Carrier
	readers []key.Reader
	first   []byte
}

func (r reading) head(id frame.ID) ([]byte, error) { return r.c.Head(id) }

// complete gives an event whole when one of the view's readers opens an
// envelope of it that names the owner generations live at its own point, the
// fold at its parents; a head only otherwise. An envelope that opens to bytes
// other than the ones the head names is a forgery (E1), and nothing of it is
// taken. l is nil for the genesis, whose point has nothing before it.
func (r reading) complete(l *ledger.Ledger, s event.Signed) ([]byte, error) {
	var owners [][]byte
	var err error
	if l == nil {
		owners, err = OwnersOf(nil, false, true, r.first)
	} else {
		owners, err = OwnersAtPoint(l, s.Event.Parents, r.first)
	}
	if err != nil {
		return nil, nil // a point without a known owner opens nothing (E3)
	}
	envs, err := r.c.Envelopes(s.ID)
	if err != nil {
		return nil, err
	}
	want, err := carrier.HeadBody(s.Head)
	if err != nil {
		return nil, err
	}
	for _, env := range envs {
		for _, rd := range r.readers {
			body, err := key.OpenReaders(key.TypeEvent, s.ID, nil, env, rd)
			if err != nil {
				continue
			}
			if sha256.Sum256(body) != want {
				return nil, fmt.Errorf("%w: event %s", carrier.ErrForged, s.ID.Short())
			}
			if key.NamesAll(env, owners) != nil {
				break // this envelope does not name the owner of its point: refused (E3)
			}
			return append(append([]byte(nil), s.Head...), body...), nil
		}
	}
	return nil, nil
}

// Load reads a carrier's ledger as the view of the given readers: every
// event whose envelope one of them opens and which names the owner of its
// own point is whole; the rest is a head only (contract 3.3, E3, E4). first
// stands for the owner at the points before any owner generation was added.
func Load(c *carrier.Carrier, readers []key.Reader, first []byte) (*ledger.Ledger, error) {
	r := reading{c: c, readers: readers, first: first}
	heads, err := AllHeads(c)
	if err != nil {
		return nil, err
	}
	gh, err := c.Head(c.Anchor())
	if err != nil {
		return nil, fmt.Errorf("genesis unreadable: %w", err)
	}
	gs, err := event.Parse(gh)
	if err != nil {
		return nil, fmt.Errorf("genesis unreadable: %w", err)
	}
	graw, err := r.complete(nil, gs)
	if err != nil {
		return nil, fmt.Errorf("genesis unreadable: %w", err)
	}
	if graw == nil {
		return nil, errors.New("genesis unreadable: no envelope of it opens for this view and names the owner's first generation")
	}
	return ledger.LoadWith(graw, r.head, r.complete, heads)
}

// Extend reads a view's ledger on to the carrier's references as they are
// now, by the rule of Load, and returns the events it newly accepted.
func Extend(l *ledger.Ledger, c *carrier.Carrier, readers []key.Reader, first []byte) ([]frame.ID, error) {
	heads, err := AllHeads(c)
	if err != nil {
		return nil, err
	}
	r := reading{c: c, readers: readers, first: first}
	return l.ExtendWith(r.head, r.complete, heads)
}

// pointOpener opens content at one point: with the view's own readers, and
// only an envelope that names the owners given (E3, E5). It seals nothing.
type pointOpener struct {
	readers []key.Reader
	owners  [][]byte
}

var errOpensOnly = errors.New("keyview: this opener reads content at its point and seals nothing")

func (p pointOpener) Seal(byte, frame.ID, string, []byte) ([]byte, error) { return nil, errOpensOnly }
func (p pointOpener) SealAt(byte, frame.ID, string, []byte, []byte) ([]byte, error) {
	return nil, errOpensOnly
}
func (p pointOpener) Open(kind byte, id frame.ID, env []byte) ([]byte, error) {
	return p.OpenAt(kind, id, nil, env)
}
func (p pointOpener) OpenAt(kind byte, id frame.ID, at []byte, env []byte) ([]byte, error) {
	if err := key.NamesAll(env, p.owners); err != nil {
		return nil, err
	}
	var last error = key.ErrNotForMe
	for _, r := range p.readers {
		b, err := key.OpenReaders(kind, id, at, env, r)
		if err == nil {
			return b, nil
		}
		if !errors.Is(err, key.ErrNotForMe) {
			last = err
		}
	}
	return nil, last
}

// ---------- the layer ----------

// Layer is one reading of a carrier's ledger for its key layer: the ledger
// of one view, which content each event describes, and where the ledger is
// read on from.
//
// A session lives as long as the process that opened it, and a door's lives
// on while other writers record: a record that reaches it later has its
// point in what they recorded. So a layer keeps the carrier it read its
// ledger through, and on a record whose point it does not hold it reads its
// ledger on from that carrier as it is now, once for each generation of the
// vessel, and answers from that. A point it still cannot give is refused.
type Layer struct {
	// mu guards led, described and readAt: a long-lived session opens
	// records while its door reads more of the carrier.
	mu  sync.Mutex
	led *ledger.Ledger
	// cell is the owner's first generation: for the owner, the generation
	// held in the vessel's owner cell; for a key, which holds no owner cell,
	// the owner's first keyring add as the ledger accepted it. It stands for
	// the owner only where no owner generation was added yet (OwnersOf).
	cell key.Gen
	// described maps a content id to the accepted event whose descriptor
	// names it: content is judged at that event's point.
	described map[frame.ID]frame.ID
	// sys is the system reader: for a key, the one its own add seals to it
	// (0x000B); for the owner, the one the owner's add seals to the owner.
	// nil where the carrier holds none.
	sys *key.Reader
	// src is the carrier, on the same vessel, the ledger is read on from, and
	// readers are the view's own readers; nil in a layer that answers from
	// the ledger it was given and nothing more.
	src     *carrier.Carrier
	readers []key.Reader
	// readAt is the vessel generation led was last read at: until the
	// vessel moves past it, reading on finds nothing new.
	readAt uint64
	// owner says whether this is the owner's layer; keyID and keyGen name the
	// key whose view a key's layer is.
	owner  bool
	keyID  [32]byte
	keyGen uint32
}

// LayerOf is the layer of a ledger already read, answering from it alone:
// which event describes each content id, a folder's files included. cell is
// the owner's first generation (Layer.cell); readers open the manifests of
// folders, at the point of the event that describes each.
func LayerOf(c *carrier.Carrier, l *ledger.Ledger, cell key.Gen, readers ...key.Reader) *Layer {
	k := &Layer{led: l, cell: cell, described: map[frame.ID]frame.ID{}, readers: readers}
	k.describe(c, l.Order())
	return k
}

// describe notes which content each of the given accepted events describes,
// a folder's files included; the manifests are read through c with the
// layer's readers at the point of the event that describes them. The caller
// holds k.mu, or is the only one who knows k.
func (k *Layer) describe(c *carrier.Carrier, ids []frame.ID) {
	l := k.led
	var trees []content.Descriptor
	treeOf := map[frame.ID]frame.ID{}
	for _, id := range ids {
		e, _ := l.Get(id)
		if e.HeadOnly || e.System || l.State(id) != ledger.Accepted {
			continue
		}
		if d, err := content.Decode(e.Event.Payload); err == nil {
			if _, seen := k.described[d.Hash]; !seen {
				k.described[d.Hash] = id
				if d.Type == content.MediaTypeTree {
					trees = append(trees, d)
					treeOf[d.Hash] = id
				}
			}
		}
	}
	// A file of a folder is named by the folder's manifest, not by an event:
	// it is judged at the point of the event that describes the manifest.
	for _, d := range trees {
		if c == nil || len(k.readers) == 0 {
			break
		}
		owners, err := OwnersAtEvent(l, treeOf[d.Hash], k.cell.Reader)
		if err != nil {
			continue // a manifest at no known point leaves its files unknown, so refused
		}
		var b bytes.Buffer
		if err := content.Fetch(carrier.Wrap(c.Vessel(), pointOpener{readers: k.readers, owners: owners}), d, &b); err != nil {
			continue // a manifest not here leaves its files unknown, so refused
		}
		t, err := content.DecodeTree(b.Bytes())
		if err != nil {
			continue
		}
		for _, entry := range t.Entries {
			if _, seen := k.described[entry.Hash]; !seen {
				k.described[entry.Hash] = treeOf[d.Hash]
			}
		}
	}
}

// Ledger is the layer's ledger: nil for a vessel that holds no event.
func (k *Layer) Ledger() *ledger.Ledger {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.led
}

// First is the owner's first generation, as this layer holds it.
func (k *Layer) First() key.Gen { return k.cell }

// System is the system reader this layer holds, or nil.
func (k *Layer) System() *key.Reader { return k.sys }

// Owner says whether this is the owner's layer.
func (k *Layer) Owner() bool { return k.owner }

// Key names the key whose view this layer is: its id and generation. The
// owner's layer answers the zero id.
func (k *Layer) Key() ([32]byte, uint32) { return k.keyID, k.keyGen }

// Readers are the view's own readers.
func (k *Layer) Readers() []key.Reader { return append([]key.Reader(nil), k.readers...) }

// OwnersAt answers the owner's reader generations live at a record's own
// point: an event's parents, or for content the parents of the event that
// describes it. A point this ledger cannot give is unknown and refused, and
// so is a point whose owner fold was emptied by revocation; only where no
// owner generation was ever added is the owner the first generation.
//
// A layer that cannot give the point reads its ledger on from the carrier as
// it is now, once, and answers from that: a record another writer made after
// this session opened has its point there. What it still cannot give stays
// refused; nothing stands in for a point (E3).
func (k *Layer) OwnersAt(id frame.ID) ([][]byte, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.led == nil {
		return nil, false // a vessel that holds no event: no point is known
	}
	owners, err := k.ownersAtLocked(id)
	if err != nil && k.src != nil && k.readOnLocked() {
		owners, err = k.ownersAtLocked(id)
	}
	return owners, err == nil
}

// ownersAtLocked is OwnersAt from what led holds now, under k.mu.
func (k *Layer) ownersAtLocked(id frame.ID) ([][]byte, error) {
	point := id
	if !k.led.Has(id) {
		d, ok := k.described[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s is not held here", key.ErrOwnerUnknown, id.Short())
		}
		point = d
	}
	return OwnersAtEvent(k.led, point, k.cell.Reader)
}

// readOnLocked reads led on to the carrier's references as they are now, by
// the rule of Load, cut short at every event already held; the events it
// adds describe their content. It reports whether it added any. It records
// nothing, and it reads at most once for each generation of the vessel.
// Under k.mu.
func (k *Layer) readOnLocked() bool {
	gen := k.src.Vessel().Info().Generation
	if gen == k.readAt {
		return false
	}
	added, err := Extend(k.led, k.src, k.readers, k.cell.Reader)
	if err != nil {
		return false
	}
	k.readAt = gen
	if len(added) == 0 {
		return false
	}
	k.describe(k.src, added)
	return true
}

// ReadOn reads the layer's ledger on to what the carrier holds now, as a
// door does before it answers from a view that the carrier may have moved
// past. It returns the events it newly accepted.
func (k *Layer) ReadOn() ([]frame.ID, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.led == nil || k.src == nil {
		return nil, nil
	}
	added, err := Extend(k.led, k.src, k.readers, k.cell.Reader)
	if err != nil {
		return nil, err
	}
	k.readAt = k.src.Vessel().Info().Generation
	k.describe(k.src, added)
	return added, nil
}

// Judge is the ledger's verdict on an event of this side, for union and
// seeding: "full", "lineage", "pending" or "refused".
func (k *Layer) Judge(id frame.ID) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.led == nil || !k.led.Has(id) {
		return "pending"
	}
	switch k.led.State(id) {
	case ledger.Accepted:
		if k.led.Judged(id) == ledger.Full {
			return "full"
		}
		return "lineage"
	case ledger.Rejected:
		return "refused"
	}
	return "pending"
}

// Covers says whether an envelope names every owner generation live at its
// record's point (E3); an unknown point is no.
func (k *Layer) Covers(id frame.ID, env []byte) bool {
	owners, ok := k.OwnersAt(id)
	return ok && key.NamesAll(env, owners) == nil
}

// ReadOpen says whether a live open grant makes an address readable by
// every key, at the layer's heads (contract 4.5).
func (k *Layer) ReadOpen(addr string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.led != nil && ReadOpenAt(k.led, addr)
}

// SessionAt is a key's own session for a record it is about to sign on
// parents: the ring is the keyring's fold at that point, so the record is
// sealed to the owner generations live there (E3) and to every live
// generation there whose reads cover its address, the system reader too
// where the address is rokh or read-open there (E4). Nothing stands in for
// the owner of a record a key signs: the point must hold a live owner
// generation, and the key must be live there (key.ErrKeyNotLive), or there
// is no session. l is the ledger the record is judged in: the writer's own,
// which may already hold the events of the same commit it rests on.
func (k *Layer) SessionAt(l *ledger.Ledger, sec key.Secret, ptk []byte, parents []frame.ID) (*key.Session, error) {
	if k.sys == nil || k.owner {
		return nil, errors.New("this key layer is the owner's; a key's session is opened by the key's own passphrase")
	}
	if len(parents) == 0 {
		return nil, fmt.Errorf("%w: a record is signed on at least one parent", key.ErrOwnerUnknown)
	}
	if _, err := OwnersAtPoint(l, parents, nil); err != nil {
		return nil, err
	}
	ring, err := RingAt(l, parents...)
	if err != nil {
		return nil, err
	}
	sess, err := key.KeySession(sec, ring, k.OwnersAt, k.sys, rand.Reader)
	if err != nil {
		return nil, err
	}
	sess.PTK = ptk
	sess.ReadOpen = func(addr string) bool { return ReadOpenAt(l, addr, parents...) }
	return sess, nil
}

// OwnerSessionAt is the owner's own session for a record signed on parents:
// the ring is the keyring's fold at that point, the owner generations live
// there (the owner's first generation only where none was ever added), every
// live generation there whose reads cover the record's address, the system
// reader where the address is rokh or read-open there (E3, E4). A point whose
// owner fold was emptied by revocation, or that is not known, seals nothing.
func (k *Layer) OwnerSessionAt(l *ledger.Ledger, sec key.Secret, ptk []byte, parents []frame.ID) (*key.Session, error) {
	if !k.owner {
		return nil, errors.New("this key layer is a key's; the owner's session is opened by the owner's passphrase")
	}
	if len(parents) == 0 {
		return nil, fmt.Errorf("%w: a record is signed on at least one parent", key.ErrOwnerUnknown)
	}
	if _, err := OwnersAtPoint(l, parents, k.cell.Reader); err != nil {
		return nil, err
	}
	ring, err := RingAt(l, parents...)
	if err != nil {
		return nil, err
	}
	if len(ring.Owner()) == 0 {
		// No owner generation was ever added before this point (the rule
		// above refused an emptied one): the owner's first generation.
		ring = key.Ring{Live: append([]key.Gen{k.cell}, ring.Live...)}
	}
	r, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		return nil, err
	}
	sess := &key.Session{Ring: ring, Mine: []key.Reader{r}, Rand: rand.Reader, OwnerAt: k.OwnersAt, PTK: ptk}
	if k.sys != nil {
		sess.System = k.sys.Public()
	}
	sess.ReadOpen = func(addr string) bool { return ReadOpenAt(l, addr, parents...) }
	return sess, nil
}

// SealerAt is the session a record signed on parents is sealed with: the
// owner's at that point in the owner's layer, the key's own in a key's.
func (k *Layer) SealerAt(l *ledger.Ledger, sec key.Secret, ptk []byte, parents []frame.ID) (*key.Session, error) {
	if k.owner {
		return k.OwnerSessionAt(l, sec, ptk, parents)
	}
	return k.SessionAt(l, sec, ptk, parents)
}

// Dress gives the owner's session its ring (the owner's cell generation and
// the keyring's live generations at the heads), its system reader, its
// read-open addresses and the owner generations at each record's point.
func (k *Layer) Dress(s *key.Session) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	ring, err := RingAt(k.led)
	if err != nil {
		return err
	}
	s.Ring = ring
	if len(ring.Owner()) == 0 && !k.led.OwnerEver() {
		// A carrier whose keyring never held an owner generation. An owner
		// fold emptied by revocation is not replaced: it seals nothing.
		s.Ring = key.Ring{Live: append([]key.Gen{k.cell}, ring.Live...)}
	}
	// The system reader: its private half is sealed to the owner's reader
	// in the owner's add (0x000B). System events are sealed to it too (E4).
	for _, g := range ring.Live {
		if !g.IsOwner() || len(g.System) == 0 || len(s.Mine) == 0 {
			continue
		}
		if priv, err := key.OpenFrom(s.Mine[0], g.System, key.InfoSystem); err == nil {
			if sys, err := key.ReaderFrom(priv); err == nil {
				s.System = sys.Public()
			}
		}
	}
	s.ReadOpen = k.ReadOpen
	s.OwnerAt = k.OwnersAt
	return nil
}

// SystemView reads a carrier with the system reader alone: the system layer
// and the read-open addresses whole, as every key of the rokh reads them,
// and everything else a head (contract 3.3, 4.5, B5). first is the owner's
// first generation. A booth answers a key that gave no reader of its own, and
// a guest, from it, and never from what the owner's reader opened.
func SystemView(c *carrier.Carrier, sys key.Reader, first key.Gen) (*Layer, error) {
	l, err := Load(c, []key.Reader{sys}, first.Reader)
	if err != nil {
		return nil, err
	}
	k := LayerOf(c, l, first, sys)
	k.sys = &sys
	k.src = c
	k.readAt = c.Vessel().Info().Generation
	return k, nil
}

// LiveIn says whether a key's generation is live at the heads of a ledger:
// added, and not taken back there (contract 4.4).
func LiveIn(l *ledger.Ledger, id [32]byte, gen uint32, reader []byte) bool {
	ring, err := RingAt(l)
	if err != nil {
		return false
	}
	for _, g := range ring.Of(id) {
		if g.Gen == gen && bytes.Equal(g.Reader, reader) {
			return true
		}
	}
	return false
}

// ---------- the first page ----------

// OwnerAdd is the owner's first keyring generation (contract 4.2, 4.6): key
// id zero, the owner's reader, the root as signer, reads everything, no slot,
// and a fresh system reader's private half sealed to the owner's reader
// (0x000B), signed by the root on the genesis. Every key the owner adds later
// is given the same system reader, sealed to its own reader (3.3, E4).
func OwnerAdd(anchor frame.ID, owner key.Reader, root ed25519.PrivateKey, rnd io.Reader) (event.Signed, key.Reader, error) {
	sys, err := key.NewReader(rnd)
	if err != nil {
		return event.Signed{}, key.Reader{}, err
	}
	sealed, err := key.SealTo(owner.Public(), sys.Bytes(), key.InfoSystem, rnd)
	if err != nil {
		return event.Signed{}, key.Reader{}, err
	}
	payload, err := event.Keyring{Op: event.KeyringAdd, Gen: 1, Name: "owner", Reader: owner.Public(),
		Signer: root.Public().(ed25519.PublicKey), Reads: []string{""}, System: sealed}.Encode()
	if err != nil {
		return event.Signed{}, key.Reader{}, err
	}
	e, err := event.SignFresh(event.Event{Carrier: &anchor, Parents: []frame.ID{anchor},
		Address: event.AddressRoot, Verb: event.VerbKeyring, Payload: payload}, root)
	return e, sys, err
}

// Bootstrap records the first page of a new carrier in one commit: the
// genesis and the owner's first keyring generation (OwnerAdd), both sealed to
// the owner's reader and the system reader, and the branch pointed at the
// add. It is the one bootstrap of a ledger, the same for `rokh init` and the
// sentence surface (T2): after it a key the owner adds is given the system
// reader and opens its own view with its own passphrase. sec is the owner's
// secret, as the owner's cell holds it; own is the host's hold on the vessel.
func Bootstrap(c *carrier.Carrier, own vessel.Owner, branch string, gen event.Signed, sec key.Secret, root ed25519.PrivateKey) (vessel.Outcome, error) {
	if root == nil {
		return vessel.NotRecorded, errors.New("the first page is signed by the root, and no root is at hand")
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		return vessel.NotRecorded, err
	}
	sess.PTK = c.Vessel().SharedKey()
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		return vessel.NotRecorded, err
	}
	add, sys, err := OwnerAdd(gen.ID, owner, root, rand.Reader)
	if err != nil {
		return vessel.NotRecorded, err
	}
	sess.System = sys.Public()
	rec, err := c.Begin(own)
	if err != nil {
		return vessel.NotRecorded, err
	}
	rec.SealWith(sess)
	for _, e := range []event.Signed{gen, add} {
		if err := rec.Event(e.ID, e.Head, e.Body, e.Event.Address); err != nil {
			rec.Abandon()
			return vessel.NotRecorded, err
		}
	}
	if err := rec.SetRef(branch, add.ID); err != nil {
		rec.Abandon()
		return vessel.NotRecorded, err
	}
	return rec.Commit()
}

// ---------- opening ----------

// Open opens the session that a passphrase's cell holds in an opened
// carrier: the owner's cell gives the owner's session, dressed from the
// ledger it reads; a key's cell gives that key's own session (KeySessionOf),
// never the owner's. The carrier's sealer is the session it returns. A
// vessel that holds no event yet has no ledger: the owner's session is the
// plain one of its cell, its layer judges nothing, and a key's passphrase
// opens no session there (ErrNoEventYet).
func Open(c *carrier.Carrier, pass string) (*key.Session, key.Secret, *Layer, error) {
	v := c.Vessel()
	info := v.Info()
	sec, _, err := key.Try(pass, v.Slots(), info.Salt, info.Iter)
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	held, err := HoldsEvents(c)
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	if !held {
		if sec.Key != ([32]byte{}) {
			return nil, key.Secret{}, nil, fmt.Errorf("this vessel %w", ErrNoEventYet)
		}
		sess, err := key.SessionFor(sec, rand.Reader)
		if err != nil {
			return nil, key.Secret{}, nil, err
		}
		sess.PTK = v.SharedKey()
		return sess, sec, &Layer{described: map[frame.ID]frame.ID{}, owner: true}, nil
	}
	if sec.Key != ([32]byte{}) {
		// A key's own passphrase: its own session, never the owner's.
		sess, k, err := KeySessionOf(c, sec)
		return sess, sec, k, err
	}
	return OwnerSessionOf(c, sec)
}

// OwnerSessionOf opens the owner's session from the owner's secret: the
// ledger read with the owner's reader, every record at its own point, and
// the session dressed from it. The carrier's sealer is that session.
func OwnerSessionOf(c *carrier.Carrier, sec key.Secret) (*key.Session, key.Secret, *Layer, error) {
	if sec.Key != ([32]byte{}) {
		return nil, key.Secret{}, nil, errors.New("this secret is a key's; a key's session is KeySessionOf")
	}
	v := c.Vessel()
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	cell := key.Gen{Keyring: event.Keyring{Op: event.KeyringAdd, Key: sec.Key, Gen: sec.Gen,
		Name: "owner", Reader: owner.Public(), Reads: []string{""}}}
	l, err := Load(c, []key.Reader{owner}, owner.Public())
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	k := LayerOf(c, l, cell, owner)
	k.owner = true
	k.keyGen = sec.Gen
	k.src = c
	k.readAt = v.Info().Generation
	// The system reader, from the owner's first add: every key holds it
	// (0x000B), and the owner's booth serves a guest with it.
	if first, err := BootstrapOwner(l); err == nil && len(first.System) > 0 {
		if priv, err := key.OpenFrom(owner, first.System, key.InfoSystem); err == nil {
			if sys, err := key.ReaderFrom(priv); err == nil {
				k.sys = &sys
			}
		}
	}
	sess, err := key.SessionFor(sec, rand.Reader)
	if err != nil {
		return nil, key.Secret{}, nil, err
	}
	sess.PTK = v.SharedKey()
	if err := k.Dress(sess); err != nil {
		return nil, key.Secret{}, nil, err
	}
	c.SetSealer(sess)
	return sess, sec, k, nil
}

// KeySessionOf opens the session of a key that is not the owner, from the
// carrier itself (contract 3.3):
//  1. the key's own add, by the envelope sealed to the key, and in it the
//     system reader's private half sealed to the key (0x000B);
//  2. the owner's first generation: the owner's generation-1 add signed by
//     the root (K1), which the ledger confirms below;
//  3. the ledger, read with the key's reader and the system reader, every
//     event at its own point: what the key cannot open is a head only;
//  4. E3 again, at the point of every record this key opens, by the rule of
//     OwnersOf: an envelope that does not name the owner of its point
//     refuses the whole opening, and so does a point whose owner fold was
//     emptied by revocation or is unknown;
//  5. the key's session: the keyring's fold, the owners at each point, the
//     system reader and the vessel's pointer key. A generation not live in
//     the fold (revoked) has none. The root is never taken.
func KeySessionOf(c *carrier.Carrier, sec key.Secret) (*key.Session, *Layer, error) {
	return KeySessionWith(c, sec, nil)
}

// KeySessionWith is KeySessionOf where a door that opened the owner's cell
// vouches for the owner's first generation (the cell's): on a carrier whose
// keyring names no owner generation it stands for the owner at every point,
// and on one whose keyring names one, the ledger must agree with it. It is
// the owner's public half only; nothing of the owner's secret is given.
func KeySessionWith(c *carrier.Carrier, sec key.Secret, vouched *key.Gen) (*key.Session, *Layer, error) {
	mine, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		return nil, nil, err
	}
	type record struct {
		head []byte
		envs [][]byte
	}
	recs := map[frame.ID]*record{}
	err = c.Vessel().Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type != vessel.RecEvent {
			return nil
		}
		r := recs[h.ID]
		if r == nil {
			r = &record{head: append([]byte(nil), h.Head...)}
			recs[h.ID] = r
		}
		if env, err := c.Vessel().Body(ref); err == nil {
			r.envs = append(r.envs, env)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	open := func(id frame.ID, readers ...key.Reader) (event.Signed, []byte, bool) {
		r := recs[id]
		if r == nil {
			return event.Signed{}, nil, false
		}
		for _, env := range r.envs {
			for _, rd := range readers {
				body, err := key.OpenReaders(key.TypeEvent, id, nil, env, rd)
				if err != nil {
					continue
				}
				e, err := event.Parse(append(append([]byte(nil), r.head...), body...))
				if err == nil && e.ID == id {
					return e, env, true
				}
			}
		}
		return event.Signed{}, nil, false
	}
	keyringOf := func(e event.Signed) (event.Keyring, bool) {
		if e.Event.Verb != event.VerbKeyring || e.HeadOnly {
			return event.Keyring{}, false
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		return k, err == nil && k.Op == event.KeyringAdd
	}
	var sys key.Reader
	found := false
	for id := range recs {
		e, _, ok := open(id, mine)
		if !ok {
			continue
		}
		if k, ok := keyringOf(e); ok && k.Key == sec.Key && k.Gen == sec.Gen && len(k.System) > 0 {
			if priv, err := key.OpenFrom(mine, k.System, key.InfoSystem); err == nil {
				if sys, err = key.ReaderFrom(priv); err == nil {
					found = true
				}
			}
		}
	}
	if !found {
		return nil, nil, ErrNoSystemReader
	}
	// The owner's first generation, read from the records to load the ledger
	// only: an owner add of generation 1 whose author is the root, the author
	// of the genesis. The ledger says below whether it is the owner's.
	var root []byte
	if g, ok := recs[c.Anchor()]; ok {
		if gh, err := event.Parse(g.head); err == nil {
			root = gh.Event.Author
		}
	}
	var first []byte
	named := false
	for id := range recs {
		e, _, ok := open(id, sys)
		if !ok {
			continue
		}
		k, ok := keyringOf(e)
		if !ok || !k.IsOwner() || len(k.Reader) == 0 {
			continue
		}
		named = true
		if k.Gen != 1 || !bytes.Equal(e.Event.Author, root) {
			continue
		}
		if first != nil && !bytes.Equal(first, k.Reader) {
			return nil, nil, fmt.Errorf("%w: two first owner generations name different readers", key.ErrOwnerUnknown)
		}
		first = append([]byte(nil), k.Reader...)
	}
	if vouched != nil && len(vouched.Reader) > 0 {
		if first != nil && !bytes.Equal(first, vouched.Reader) {
			return nil, nil, fmt.Errorf("%w: the owner's first generation this carrier's records name is not the one the door vouches for", key.ErrOwnerUnknown)
		}
		first, named = append([]byte(nil), vouched.Reader...), true
	}
	if !named {
		return nil, nil, fmt.Errorf("%w: this carrier's keyring names no owner generation", key.ErrOwnerUnknown)
	}
	if first == nil {
		return nil, nil, fmt.Errorf("%w: this carrier's keyring names no first owner generation", key.ErrOwnerUnknown)
	}
	readers := []key.Reader{mine, sys}
	l, err := Load(c, readers, first)
	if err != nil {
		return nil, nil, err
	}
	// The owner's first generation, as the ledger accepted it, stands where
	// no owner generation was added yet, as the owner's cell does for the
	// owner (OwnersOf). It is the one the ledger was read with, or nothing is.
	// Where the ledger holds no owner generation at all, only the one a door
	// vouched for stands.
	var confirmed key.Gen
	if vouched != nil && len(vouched.Reader) > 0 && !l.OwnerEver() {
		confirmed = *vouched
	} else {
		if confirmed, err = BootstrapOwner(l); err != nil {
			return nil, nil, err
		}
	}
	if !bytes.Equal(confirmed.Reader, first) {
		return nil, nil, fmt.Errorf("%w: the ledger's first owner generation is not the one its records name", key.ErrOwnerUnknown)
	}
	for id := range recs {
		e, env, ok := open(id, readers...)
		if !ok {
			continue
		}
		at, err := OwnersAtPoint(l, e.Event.Parents, first)
		if err != nil {
			return nil, nil, fmt.Errorf("the owner at the point of %s is not given; nothing is opened: %w", id.Short(), err)
		}
		if err := key.NamesAll(env, at); err != nil {
			return nil, nil, fmt.Errorf("the envelope of %s does not name the owner of its own point; nothing is opened: %w", id.Short(), err)
		}
	}
	k := LayerOf(c, l, confirmed, readers...)
	k.sys = &sys
	k.src = c
	k.readAt = c.Vessel().Info().Generation
	k.keyID, k.keyGen = sec.Key, sec.Gen
	ring, err := RingAt(l)
	if err != nil {
		return nil, nil, err
	}
	if len(ring.Owner()) == 0 && !l.OwnerEver() {
		// No owner generation was ever added: the first stands for the owner.
		ring = key.Ring{Live: append([]key.Gen{confirmed}, ring.Live...)}
	}
	sess, err := key.KeySession(sec, ring, k.OwnersAt, &sys, rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	sess.PTK = c.Vessel().SharedKey()
	sess.ReadOpen = k.ReadOpen
	c.SetSealer(sess)
	return sess, k, nil
}
