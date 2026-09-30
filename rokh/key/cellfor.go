package key

import (
	"fmt"
	"io"

	"rokh/event"
)

// CellFor makes a key generation's cell in a vessel from what its keyring add
// carries (contract 4.2, 4.6): the slot blob, the vessel key sealed to the
// generation's reader, and four random bytes. It is how a session that holds
// VK installs the cell of a key it learns from a keyring event. Nothing of
// the key's secret is used: the blob opens only under the key's own
// passphrase, as it does in every vessel of the rokh, since the salt and the
// cost are inherited.
func CellFor(blob, readerPub, vk []byte, rnd io.Reader) ([]byte, error) {
	if len(blob) != BlobSize {
		return nil, fmt.Errorf("key: a blob is %d bytes, not %d", len(blob), BlobSize)
	}
	if len(readerPub) != event.ReaderSize {
		return nil, fmt.Errorf("key: a reader is %d bytes, not %d", len(readerPub), event.ReaderSize)
	}
	vkseal, err := SealTo(readerPub, vk, InfoVK, rnd)
	if err != nil {
		return nil, err
	}
	tail, err := read(rnd, 4)
	if err != nil {
		return nil, err
	}
	out := append(append(append([]byte(nil), blob...), vkseal...), tail...)
	if len(out) != CellSize {
		return nil, fmt.Errorf("key: a cell came out %d bytes", len(out))
	}
	return out, nil
}

// Takes reports whether a vessel of the given scopes takes the cell of one
// live key generation, by a ruling of the design (contract 4.6,
// R2; axiom 19, "on seeds it is the same"), and by nothing wider: never the
// owner's generation, which has its own cell; only a generation whose add
// carries a slot, since a cell is made of that slot and nothing of a key's
// secret is asked for; and for a slice (scopes not empty) only a generation
// whose reads, or the scope of a live grant to its signer (granted), touch
// one of the slice's scopes. A vessel of the whole rokh takes every such
// generation. Whether the generation is live, and at which point, is the
// caller's to say from its ledger: this package reads none.
func Takes(k event.Keyring, granted, scopes []string) bool {
	if k.Op != event.KeyringAdd || k.IsOwner() || len(k.Slot) != BlobSize || len(k.Reader) != event.ReaderSize {
		return false
	}
	if len(scopes) == 0 {
		return true
	}
	return touches(k.Reads, scopes) || (k.Signer != nil && touches(granted, scopes))
}

// touches says whether one of the prefixes meets one of the scopes: one lies
// inside the other. The empty prefix is the whole rokh and meets every scope.
func touches(prefixes, scopes []string) bool {
	for _, p := range prefixes {
		for _, s := range scopes {
			if event.ScopeCovers(p, s) || event.ScopeCovers(s, p) {
				return true
			}
		}
	}
	return false
}

// Install puts cells into a vessel's slot cells where nobody's cell is: it is
// how a session that holds VK installs the cells of the keys it learns
// (contract 4.6, R2). A cell whose blob a slot already begins with is there
// already and is left as it is, so installing twice changes nothing. A slot
// is somebody's when it is the owner's (its sealed vessel key opens to
// owner) or when it begins with one of held, the slot blobs of the live
// generations; every other slot is the vessel's random filling or the cell of
// a generation taken back, and is free. It returns the new slot cells and how
// many cells it placed; when the free slots do not hold them all it places
// none, returns the slots as they were, and says so.
func Install(slots [][]byte, owner Reader, held map[string]bool, cells [][]byte) ([][]byte, int, error) {
	present := map[string]bool{}
	for _, s := range slots {
		if len(s) == CellSize {
			present[string(s[:BlobSize])] = true
		}
	}
	var fresh [][]byte
	for _, c := range cells {
		if len(c) != CellSize {
			return slots, 0, fmt.Errorf("key: a cell is %d bytes, not %d", len(c), CellSize)
		}
		if b := string(c[:BlobSize]); !present[b] {
			present[b] = true // a cell given twice is placed once
			fresh = append(fresh, c)
		}
	}
	if len(fresh) == 0 {
		return slots, 0, nil
	}
	var free []int
	for i, s := range slots {
		if len(s) != CellSize || held[string(s[:BlobSize])] {
			continue
		}
		if _, err := OpenFrom(owner, s[BlobSize:BlobSize+event.SealedKeySize], InfoVK); err == nil {
			continue // the owner's
		}
		free = append(free, i)
	}
	if len(free) < len(fresh) {
		return slots, 0, fmt.Errorf("key: %d key cells are to be installed and %d cells of this vessel are free; none was installed", len(fresh), len(free))
	}
	out := make([][]byte, len(slots))
	copy(out, slots)
	for j, c := range fresh {
		out[free[j]] = append([]byte(nil), c...)
	}
	return out, len(fresh), nil
}
