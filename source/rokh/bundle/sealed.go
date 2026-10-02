package bundle

import (
	"fmt"

	"rokh/covenant"
	"rokh/frame"
	"rokh/ledger"
	"rokh/seal"
)

// SealFor closes every selected event for the reading key the covenant names,
// so what leaves is already shut.
//
// This is where the ruling stops being a promise. Disclosure is decided when
// the bundle is sealed, not when it is sent: by the time anything is handed
// over, the decision has already been made and written into the bytes. The
// courier holds no key, so what must not travel never reaches it — and what
// does travel it cannot read.
//
// A covenant that names no reading key seals nothing, and says so rather than
// quietly handing over the plain bytes.
//
//	— T13.1, T7.4, N4.9
func SealFor(l *ledger.Ledger, sel Selection, reading []byte) ([]seal.Sealed, error) {
	if len(reading) == 0 {
		return nil, fmt.Errorf("bundle: this covenant names no reading key; " +
			"nothing can be sealed to a reader who has not given one")
	}
	r, err := seal.ReaderFrom(reading)
	if err != nil {
		return nil, err
	}
	out := make([]seal.Sealed, 0, len(sel.IDs))
	for _, id := range sel.IDs {
		s, ok := l.Get(id)
		if !ok {
			return nil, fmt.Errorf("bundle: %s is selected and not held", id.Short())
		}
		// The event's own name goes in as context: a sealed event cannot be
		// lifted out and offered as though it were a different one.
		box, err := seal.To(r, s.Raw, id[:])
		if err != nil {
			return nil, err
		}
		out = append(out, box)
	}
	return out, nil
}

// ReadingKeyFor reports the reading key a peer's covenants name, if any. It is
// the covenant that says who may read, and this is only the reading of it.
//
//	— T7.1, T7.3
func ReadingKeyFor(covs []covenant.Covenant, address string) ([]byte, bool) {
	for _, c := range covs {
		if len(c.SealTo) == seal.KeySize && c.Covers(address) {
			return append([]byte(nil), c.SealTo...), true
		}
	}
	return nil, false
}

// ReadingKeyForEvent is ReadingKeyFor asked about one event, so that a covenant
// naming a room can be sealed to the reader it names. An address alone cannot
// find one: a room covers no address.
//
//	— T7.1, T13.1
func ReadingKeyForEvent(covs []covenant.Covenant, id frame.ID, address string) ([]byte, bool) {
	for _, c := range covs {
		if len(c.SealTo) == seal.KeySize && c.Reach().Covers(id, address) {
			return append([]byte(nil), c.SealTo...), true
		}
	}
	return nil, false
}

// ReadingKeyForSelection is the reading key for a whole bundle, found from the
// events actually selected rather than from the scope that was typed.
//
// A scope cannot answer for a room. A room covenant covers no address, so
// asking by address finds no key and the bundle would leave unsealed — the
// events would cross in the clear under a covenant that had named a reader to
// shut them to. Asking the selection instead means the key is looked for where
// the disclosure was actually decided.
//
// It is still one key for the whole bundle and still never half and half: the
// first key any selected event's covenant names seals all of them.
//
//	— T13.1, T7.4
func ReadingKeyForSelection(l *ledger.Ledger, covs []covenant.Covenant, sel Selection) ([]byte, bool) {
	for _, id := range sel.IDs {
		e, found := l.Get(id)
		if !found {
			continue
		}
		if key, named := ReadingKeyForEvent(covs, id, e.Event.Address); named {
			return key, true
		}
	}
	return nil, false
}
