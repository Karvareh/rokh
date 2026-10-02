package home

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"

	"rokh-home/authority"
	"rokh/carrier"
	"rokh/daemon"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/keyview"
	"rokh/ledger"
)

// Each program of a home reads the home's ledger through a key of its own
// (contract B4, 3.3; T2): a generation in the home's keyring whose reads are
// the program's ledger namespaces and its shelf of tasks, and whose reader
// the home holds for it beside its signing keys, in the same sealed store.
// The home's doors seal every record to the readers of its own point, so
// what is recorded in a program's namespace or on its shelf is sealed to
// that program's reader; and a program is answered from its own view, read
// with that reader and the system reader only. Nothing the owner's reader
// opened reaches a program, whole or cut.

// heldReader is the reading half of a program's key, as the home holds it.
type heldReader struct {
	Gen     uint32   `json:"gen"`
	Reader  []byte   `json:"reader"`
	Reads   []string `json:"reads"`
	Event   string   `json:"event,omitempty"` // the keyring add, once recorded
	Attempt string   `json:"attempt"`
}

// ProgramKey is the id of a program's key in the home's keyring: the same
// id the gate binds the program's session to.
func ProgramKey(consumer string) [32]byte {
	return sha256.Sum256([]byte("rokh-home/program/" + consumer))
}

// programReads is what a program's key reads: its ledger namespaces and its
// shelf of tasks, sorted.
func programReads(reg *authority.Registry, consumer string) []string {
	seen := map[string]bool{TaskShelf(consumer): true}
	for _, ns := range reg.Scopes(consumer, authority.Ledger) {
		seen[ns] = true
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func sameReads(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// systemReaderLocked is the home's system reader: every program key's add
// seals it to that key's reader, and the doors seal the system layer to it
// (E4). It is made, and kept in the home's sealed store, the first time a
// program key needs it. Under h.mu held alone.
func (h *Home) systemReaderLocked() (key.Reader, error) {
	h.keys.mu.RLock()
	sys := append([]byte(nil), h.keys.System...)
	h.keys.mu.RUnlock()
	if len(sys) == 32 {
		return key.ReaderFrom(sys)
	}
	r, err := key.NewReader(rand.Reader)
	if err != nil {
		return key.Reader{}, err
	}
	// The system layer recorded before the home had a system reader (the
	// genesis, the grants) is given a second envelope for it, beside the
	// owner of each record's point (E2, E4): a backfill of envelopes, which
	// records no event (C6). A program's own view reads the system layer
	// through it.
	if err := h.backfillSystemLocked(r); err != nil {
		return key.Reader{}, err
	}
	if err := h.persistKeyring(func(next *keyring) { next.System = r.Bytes() }); err != nil {
		return key.Reader{}, err
	}
	return r, nil
}

// backfillSystemLocked seals every event at the address rokh that the owner's
// view holds whole to the system reader too, in one commit: one more envelope
// of each, naming the owner generations at its own point and the system
// reader. Under h.mu.
func (h *Home) backfillSystemLocked(sys key.Reader) error {
	if _, err := h.layer.ReadOn(); err != nil {
		return err
	}
	l := h.layer.Ledger()
	type sealed struct {
		id   frame.ID
		head []byte
		env  []byte
	}
	var out []sealed
	for _, id := range l.Order() {
		e, _ := l.Get(id)
		if e.HeadOnly || e.Event.Address != event.AddressRoot {
			continue
		}
		owners, ok := h.layer.OwnersAt(id)
		if !ok {
			continue // a point without a known owner is sealed to nobody more
		}
		env, err := key.SealReaders(key.TypeEvent, id, nil, append(append([][]byte(nil), owners...), sys.Public()), e.Body, rand.Reader)
		if err != nil {
			return err
		}
		out = append(out, sealed{id: id, head: e.Head, env: env})
	}
	if len(out) == 0 {
		return nil
	}
	return recordOnce(h.LedgerPath(), h.car, func(rec *carrier.Recording) error {
		for _, x := range out {
			if err := rec.EventEnvelope(x.id, x.head, x.env); err != nil {
				return err
			}
		}
		return nil
	})
}

// systemPublic is the public half of the home's system reader, or nil.
func (h *Home) systemPublic() []byte {
	h.keys.mu.RLock()
	defer h.keys.mu.RUnlock()
	if len(h.keys.System) != 32 {
		return nil
	}
	r, err := key.ReaderFrom(h.keys.System)
	if err != nil {
		return nil
	}
	return r.Public()
}

// ensureProgramReaderLocked makes the program's keyring generation read what
// its grants say: its ledger namespaces and its shelf. A program whose reads
// are already live in its generation changes nothing. Otherwise a new
// generation is added (its add carries the system reader sealed to the new
// reader, and an envelope of its own for it, E2) and the previous one is
// taken back (K3). The reader is stored before the add, and the add is
// recorded under an attempt, so a retry records nothing twice. Under h.mu.
func (h *Home) ensureProgramReaderLocked(reg *authority.Registry, c *authority.Consumer) error {
	want := programReads(reg, c.ID)
	h.keys.mu.RLock()
	cur, have := h.keys.Readers[c.ID]
	h.keys.mu.RUnlock()
	if have && cur.Event != "" && sameReads(cur.Reads, want) {
		return nil
	}
	sys, err := h.systemReaderLocked()
	if err != nil {
		return err
	}
	next := cur
	if !have || cur.Event != "" || !sameReads(cur.Reads, want) {
		// A new generation: a new reader, and a new attempt for its add.
		r, err := key.NewReader(rand.Reader)
		if err != nil {
			return err
		}
		gen := uint32(1)
		if have {
			gen = cur.Gen + 1
			if cur.Event == "" {
				gen = cur.Gen
			}
		}
		next = heldReader{Gen: gen, Reader: r.Bytes(), Reads: want, Attempt: "home:reader:" + newID()}
		if err := h.persistKeyring(func(k *keyring) { k.Readers[c.ID] = next }); err != nil {
			return err
		}
	}
	r, err := key.ReaderFrom(next.Reader)
	if err != nil {
		return errors.New("home: a program's stored reader is corrupt")
	}
	sysSeal, err := key.SealTo(r.Public(), sys.Bytes(), key.InfoSystem, rand.Reader)
	if err != nil {
		return err
	}
	id := ProgramKey(c.ID)
	payload, err := event.Keyring{Op: event.KeyringAdd, Key: id, Gen: next.Gen, Name: "program-" + c.ID[:16],
		Reader: r.Public(), Reads: want, System: sysSeal}.Encode()
	if err != nil {
		return err
	}
	ans := ask(h.ownerDoor, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbKeyring,
		"payload": b64(payload), "attempt": next.Attempt})
	if ans["record"] != daemon.Recorded {
		return fmt.Errorf("home: the key of %s was not recorded: %v", c.Name, ans["error"])
	}
	addID, err := frame.ParseID(fmt.Sprint(ans["id"]))
	if err != nil {
		return err
	}
	if err := h.envelopeForLocked(addID, r); err != nil {
		return err
	}
	if have && cur.Event != "" {
		target, err := frame.ParseID(cur.Event)
		if err != nil {
			return err
		}
		rv, err := event.Keyring{Op: event.KeyringRevoke, Key: id, Gen: cur.Gen, Target: target}.Encode()
		if err != nil {
			return err
		}
		ans := ask(h.ownerDoor, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbKeyring,
			"payload": b64(rv), "attempt": "home:reader-revoke:" + cur.Event[:40]})
		if ans["record"] != daemon.Recorded {
			return fmt.Errorf("home: the previous key of %s was not taken back: %v", c.Name, ans["error"])
		}
	}
	next.Event = addID.String()
	return h.persistKeyring(func(k *keyring) { k.Readers[c.ID] = next })
}

// retireProgramReaderLocked takes a program's key back when its standing is
// withdrawn: from then on nothing is sealed to it (K4). Under h.mu.
func (h *Home) retireProgramReaderLocked(consumer string) error {
	h.keys.mu.RLock()
	cur, have := h.keys.Readers[consumer]
	h.keys.mu.RUnlock()
	if !have || cur.Event == "" {
		return nil
	}
	target, err := frame.ParseID(cur.Event)
	if err != nil {
		return err
	}
	rv, err := event.Keyring{Op: event.KeyringRevoke, Key: ProgramKey(consumer), Gen: cur.Gen, Target: target}.Encode()
	if err != nil {
		return err
	}
	ans := ask(h.ownerDoor, map[string]any{"op": "write", "address": event.AddressRoot, "verb": event.VerbKeyring,
		"payload": b64(rv), "attempt": "home:reader-revoke:" + cur.Event[:40]})
	if ans["record"] != daemon.Recorded {
		return fmt.Errorf("home: the key of a withdrawn program was not taken back: %v", ans["error"])
	}
	return nil
}

// envelopeForLocked records a second envelope of an event for one more
// reader, beside the owner generations at its point (E2, E3): what a key's
// own add needs, so that the key finds its system reader with its own
// reader. Under h.mu.
func (h *Home) envelopeForLocked(id frame.ID, r key.Reader) error {
	got := ask(h.ownerDoor, map[string]any{"op": "get", "id": id.String()})
	raw, err := hex.DecodeString(fmt.Sprint(got["raw"]))
	if err != nil {
		return err
	}
	e, err := event.Parse(raw)
	if err != nil || e.HeadOnly {
		return fmt.Errorf("home: the key's add is not held whole: %v", err)
	}
	owners, ok := h.layer.OwnersAt(id)
	if !ok {
		return errors.New("home: the owner at the key's add is not given; the key has no envelope of its own")
	}
	env, err := key.SealReaders(key.TypeEvent, id, nil, append(append([][]byte(nil), owners...), r.Public()), e.Body, rand.Reader)
	if err != nil {
		return err
	}
	return recordOnce(h.LedgerPath(), h.car, func(rec *carrier.Recording) error {
		return rec.EventEnvelope(id, e.Head, env)
	})
}

// persistKeyring saves a changed keyring and publishes it only once its
// sealed pointer is stored.
func (h *Home) persistKeyring(edit func(*keyring)) error {
	h.keys.mu.RLock()
	next := &keyring{Keys: make(map[string]heldKey, len(h.keys.Keys)), Readers: make(map[string]heldReader, len(h.keys.Readers)+1),
		System: append([]byte(nil), h.keys.System...)}
	for n, k := range h.keys.Keys {
		next.Keys[n] = k
	}
	for n, r := range h.keys.Readers {
		next.Readers[n] = r
	}
	h.keys.mu.RUnlock()
	edit(next)
	if err := h.save(ptrKeys, next); err != nil {
		return err
	}
	h.keys.mu.Lock()
	h.keys.Keys, h.keys.Readers, h.keys.System = next.Keys, next.Readers, next.System
	h.keys.mu.Unlock()
	return nil
}

// sealerAt is the session a record the home's doors sign on parents is
// sealed with: the owner's at that point (keyview), with the home's system
// reader for the system layer (E3, E4).
func (h *Home) sealerAt(l *ledger.Ledger, parents []frame.ID) (carrier.Sealer, error) {
	sess, err := h.layer.OwnerSessionAt(l, h.sec, h.car.Vessel().SharedKey(), parents)
	if err != nil {
		return nil, err
	}
	if sys := h.systemPublic(); sys != nil {
		sess.System = sys
	}
	return sess, nil
}

// programViews are the doors over each program's own view, made at its
// first question and kept while its key's generation stands.
type programViews struct {
	mu    sync.Mutex
	views map[string]programView
}

type programView struct {
	gen    uint32
	reader []byte
	srv    *daemon.Server
}

// errNoProgramKey is a program that holds no key of its own in the home's
// keyring: its ledger is not opened for it with anyone else's reader.
var errNoProgramKey = errors.New("home: this program holds no key of its own in the home's keyring yet, so its view of the ledger does not open; the owner grants it a namespace")

// programView is the door over a program's own view of the home's ledger:
// its own opening of the vessel, the ledger read with the program's reader
// and the system reader only, every record at its own point.
func (h *Home) programView(consumer string) (*daemon.Server, error) {
	h.keys.mu.RLock()
	hr, ok := h.keys.Readers[consumer]
	h.keys.mu.RUnlock()
	if !ok || hr.Event == "" {
		return nil, errNoProgramKey
	}
	h.views.mu.Lock()
	defer h.views.mu.Unlock()
	if v, ok := h.views.views[consumer]; ok && v.gen == hr.Gen && bytes.Equal(v.reader, hr.Reader) {
		return v.srv, nil
	}
	v2, _, err := h.car.Vessel().Reopen()
	if err != nil {
		return nil, err
	}
	c2 := carrier.Wrap(v2, nil)
	sec := key.Secret{Key: ProgramKey(consumer), Gen: hr.Gen}
	copy(sec.Reader[:], hr.Reader)
	first := h.layer.First()
	_, layer, err := keyview.KeySessionWith(c2, sec, &first)
	if err != nil {
		return nil, fmt.Errorf("home: this program's own view does not open: %w", err)
	}
	srv := daemon.New(c2, layer.Ledger(), daemon.Options{ReadOnly: true, Dir: h.LedgerPath(), Release: h.release,
		Door: ProgramDoor, Keys: h.keys, ReadOn: layer.ReadOn})
	if h.views.views == nil {
		h.views.views = map[string]programView{}
	}
	h.views.views[consumer] = programView{gen: hr.Gen, reader: append([]byte(nil), hr.Reader...), srv: srv}
	return srv, nil
}
