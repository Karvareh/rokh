package shell

// The cells one side of a union installs (contract 4.8, R2), as the command
// line's reconcile has them (cmd/rokh, learnCells), by a ruling of the
// design: after the union each side installs the cell of every live key
// generation its ledger holds and its vessel lacks, by the rule a new seed
// follows (key.Takes), at its present heads and inside its own scopes, where
// nobody's cell is (key.Install). It records no event, and a cell the vessel
// holds is left as it is. A key revoked on either side is not live after the
// union and takes no cell.

import (
	"crypto/rand"
	"fmt"

	"rokh/carrier"
	"rokh/event"
	"rokh/key"
	"rokh/ledger"
	"rokh/vessel"
)

// learnCells installs the cells on one side and says how many. byKey is set
// when the passphrase opens a key's cell: only the owner's passphrase tells
// the owner's cell from a free one, so none is installed then (when in doubt,
// no cell). out is the outcome of the commit that installed them, empty when
// none was made. When the free cells do not hold them all, none is installed.
func learnCells(c *carrier.Carrier, own vessel.Owner, pass string) (installed int, byKey bool, out vessel.Outcome, err error) {
	v := c.Vessel()
	info := v.Info()
	sec, _, err := key.Try(pass, v.Slots(), info.Salt, info.Iter)
	if err != nil {
		return 0, false, "", err
	}
	if sec.Key != ([32]byte{}) {
		return 0, true, "", nil
	}
	owner, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		return 0, false, "", err
	}
	// The union changed what the vessel holds: its ledger is read again,
	// through a session dressed from it, which the carrier keeps.
	sess, _, layer, err := keyLayerOf(c, pass)
	if err != nil {
		return 0, false, "", err
	}
	c.SetSealer(sess)
	l := layer.led
	heads := l.Heads()
	granted := map[string][]string{} // a signer's live grant scopes
	for _, id := range l.ActiveGrants(heads...) {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly {
			continue
		}
		if g, err := event.DecodeGrant(e.Event.Payload); err == nil && !g.Open {
			granted[string(g.Subject)] = append(granted[string(g.Subject)], g.Scope)
		}
	}
	held := map[string]bool{}
	var cells [][]byte
	adds, _ := l.Keyring(heads...)
	for _, id := range adds {
		e, ok := l.Get(id)
		if !ok || e.HeadOnly || l.State(id) != ledger.Accepted {
			continue
		}
		k, err := event.DecodeKeyring(e.Event.Payload)
		if err != nil {
			continue
		}
		if len(k.Slot) == key.BlobSize {
			held[string(k.Slot)] = true
		}
		if !key.Takes(k, granted[string(k.Signer)], info.Scopes) {
			continue
		}
		cell, err := key.CellFor(k.Slot, k.Reader, v.VK(), rand.Reader)
		if err != nil {
			return 0, false, "", err
		}
		cells = append(cells, cell)
	}
	slots, n, err := key.Install(v.Slots(), owner, held, cells)
	if err != nil || n == 0 {
		return 0, false, "", err
	}
	r, err := c.Begin(own)
	if err != nil {
		return 0, false, vessel.NotRecorded, err
	}
	r.Tx().SetSlots(slots)
	out, err = r.Commit()
	if out != vessel.Recorded {
		return 0, false, out, fmt.Errorf("the commit of %s was %s: %v", countOf(n, "key cell"), out, err)
	}
	return n, false, out, nil
}
