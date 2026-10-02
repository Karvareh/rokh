package home

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"rokh-home/authority"
	"rokh-home/store"
	"rokh/carrier"
	"rokh/content"
	"rokh/event"
	"rokh/turn"
)

const (
	ReplicaFormat = "rokh-home.replica/1"
	WriterReplica = "writer"
	MirrorReplica = "mirror"
	ptrReplica    = "replica"
)

// A mirror carries committed content and its provenance, not the writer's
// authority, drafts, unfinished imports or native application identities.
type ReplicaInfo struct {
	Format       string `json:"format"`
	Kind         string `json:"kind"`
	SourceAnchor string `json:"source_anchor,omitempty"`
}

func mirrorAction(action authority.Action) bool {
	return action == authority.Read || action == authority.Search || action == authority.Bytes || action == authority.Export
}

func (h *Home) Replica() ReplicaInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.replica
}

// Mirror makes a separately encrypted reading replica at a new destination.
// It copies an explicit closure of committed content references, never every
// object: an unrelated encrypted object may be an application's private key.
// Neither source data nor source grants change. A failure leaves an explicitly
// incomplete destination; it is never silently overwritten on another try.
func (h *Home) Mirror(a Actor, destination, passphrase string, testIterations ...int) (ReplicaInfo, error) {
	info := ReplicaInfo{Format: ReplicaFormat, Kind: MirrorReplica}
	if err := ownerOnly(a, "replica.mirror"); err != nil {
		return info, err
	}
	if passphrase == "" {
		return info, errors.New("home: the reading replica needs its own passphrase")
	}
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if len(h.jobs) != 0 {
		return info, errors.New("home: finish or stop active imports before making a mirror")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if within(h.root, destination) {
		return info, errors.New("home: a mirror must be outside its source home")
	}
	release, err := turn.Share(h.LedgerPath(), 15*time.Second)
	if err != nil {
		return info, err
	}
	defer release()
	if err := os.Mkdir(destination, 0o700); err != nil {
		return info, err
	}
	marker := filepath.Join(destination, "INCOMPLETE")
	if err := os.WriteFile(marker, []byte("Reading replica is incomplete. No source data was changed.\n"), 0o600); err != nil {
		return info, err
	}
	// A reading mirror is a vessel of its own under its own passphrase, with
	// the same anchor and no signing secret; its store is records in it.
	dir := filepath.Join(destination, LedgerDir)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return info, err
	}
	if err := createLedger(dir, passphrase, event.Signed{}, [32]byte{}, h.car.Anchor(), testIterations); err != nil {
		return info, err
	}
	car, _, err := openLedger(dir, passphrase)
	if err != nil {
		return info, err
	}
	dst, err := store.On(destination, car.Vessel(), passphrase)
	if err != nil {
		return info, err
	}
	defer dst.Close()
	info.SourceAnchor = h.car.Anchor().String()
	objects := map[[2]string]bool{}
	add := func(kind, id string) { objects[[2]string{kind, id}] = true }
	for _, item := range h.cat.Items {
		for _, v := range item.Versions {
			add(kindRecord, v.Record)
			add(kindTree, v.SHA256)
			for _, id := range v.Sheets {
				add(kindSheet, id)
			}
			m, err := h.manifest(v)
			if err != nil {
				return info, err
			}
			for _, e := range m.Entries {
				if e.Kind == "file" {
					add(kindBlob, e.Blob)
				}
			}
		}
	}
	l, err := loadLedger(h.car)
	if err != nil {
		return info, err
	}
	for _, id := range l.Order() {
		e, _ := l.Get(id)
		d, err := content.Decode(e.Event.Payload)
		if err == nil && d.Type == declarationRecordType {
			add("declaration", d.Hash.String())
		}
	}
	ordered := make([][2]string, 0, len(objects))
	for o := range objects {
		ordered = append(ordered, o)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i][0]+ordered[i][1] < ordered[j][0]+ordered[j][1] })
	for _, o := range ordered {
		r, err := h.st.Get(o[0], o[1])
		if err != nil {
			return info, err
		}
		_, copyErr := dst.Put(o[0], o[1], r, nil)
		closeErr := r.Close()
		if copyErr != nil {
			return info, fmt.Errorf("home: incomplete reading replica: %w", copyErr)
		}
		if closeErr != nil {
			return info, closeErr
		}
	}
	src, err := loadLedger(h.car)
	if err != nil {
		return info, err
	}
	refs, err := h.car.Refs()
	if err != nil {
		return info, err
	}
	if err := recordOnce(dir, car, func(rec *carrier.Recording) error {
		for _, id := range src.Order() {
			e, _ := src.Get(id)
			if err := rec.Event(id, e.Head, e.Body, e.Event.Address); err != nil {
				return err
			}
		}
		for name, id := range refs {
			if err := rec.SetRef(name, id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return info, err
	}
	cat := *h.cat
	cat.Drafts = map[string]*Draft{}
	for name, v := range map[string]any{
		ptrCatalog: &cat, ptrRegistry: authority.New(), ptrKeys: &keyring{Keys: map[string]heldKey{}},
		ptrPlaces: map[string]Places{}, ptrReplica: info,
	} {
		b, err := json.Marshal(v)
		if err != nil {
			return info, err
		}
		if err := dst.SetPointer(name, b); err != nil {
			return info, err
		}
	}
	if err := os.Remove(marker); err != nil {
		return info, err
	}
	f, err := os.Open(destination)
	if err != nil {
		return info, err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return info, err
	}
	if closeErr != nil {
		return info, closeErr
	}
	return info, nil
}
