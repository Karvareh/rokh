package home

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"rokh-home/authority"
	"rokh-home/sheet"
	"rokh/content"
	"rokh/daemon"
	"rokh/frame"
	"rokh/ledger"
	"rokh/turn"
)

const declarationRecordType = "application/vnd.rokh.home.authorship+json"
const declarationFormat = "rokh-home.declaration/2"

// The complete decision is an immutable encrypted object before its descriptor
// is recorded. In particular, segment attribution must not live only in a
// replaceable catalog pointer, or a crash could erase the owner's distinction.
type declarationRecord struct {
	Format        string       `json:"format"`
	Item          string       `json:"item"`
	Version       int          `json:"version"`
	Manifest      string       `json:"manifest"`
	PreviousSheet string       `json:"previous_sheet"`
	Input         DeclareInput `json:"input"`
}

// A home's materialized state has one broker. This is separate from the short
// ledger writing turn: holding that turn for a session would deadlock writes.
// The directory inode is stable across atomic descriptor/pointer replacements.
func holdHome(root string) (*os.File, error) {
	f, err := os.Open(root)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("home: another broker holds this home: %w", err)
	}
	return f, nil
}

// recoverCatalog derives recorded versions from accepted ledger events.
// Drafts remain explicit unrecorded work; recovery creates no ledger event.
// The old catalog is only a projection, never evidence that a version committed.
// A conflicting or incomplete committed record is reported, not silently lost.
func (h *Home) recoverCatalog() error {
	release, err := turn.Share(h.LedgerPath(), 15*time.Second)
	if err != nil {
		return err
	}
	defer release()
	l, err := loadLedger(h.car)
	if err != nil {
		return err
	}
	check := func(id string) (sheet.EventFacts, bool) {
		fid, err := frame.ParseID(id)
		if err != nil {
			return sheet.EventFacts{}, false
		}
		e, ok := l.Get(fid)
		return sheet.EventFacts{Accepted: l.State(fid) == ledger.Accepted,
			ByOwner: bytes.Equal(e.Event.Author, l.Root()), Address: e.Event.Address, Verb: e.Event.Verb}, ok
	}
	old := h.cat
	if old == nil || old.Format != CatalogFormat {
		return errors.New("home: unsupported or missing catalog format")
	}
	cat := &Catalog{Format: CatalogFormat, Items: map[string]*Item{}, Paths: map[string]string{}, Drafts: old.Drafts}
	if cat.Drafts == nil {
		cat.Drafts = map[string]*Draft{}
	}
	for _, id := range l.Order() {
		e, _ := l.Get(id)
		if !strings.HasPrefix(e.Event.Address, ItemsAddress+"/") {
			continue
		}
		desc, derr := content.Decode(e.Event.Payload)
		switch e.Event.Verb {
		case content.VerbPut:
			if derr != nil || desc.Type != RecordType {
				continue
			}
			var rec recordManifest
			if err := h.getJSON(kindRecord, desc.Hash.String(), &rec); err != nil {
				return fmt.Errorf("home: committed record %s: %w", id, err)
			}
			if rec.Format != RecordFormat || rec.Item == "" || rec.Version < 1 || e.Event.Address != ItemsAddress+"/"+rec.Item {
				return fmt.Errorf("home: invalid committed record %s", id)
			}
			if _, err := authority.CleanPath(rec.Path); err != nil {
				return err
			}
			item := cat.Items[rec.Item]
			if item == nil {
				item = &Item{ID: rec.Item, Path: rec.Path}
			}
			if item.Path != rec.Path || rec.Version != len(item.Versions)+1 || (cat.Paths[rec.Path] != "" && cat.Paths[rec.Path] != rec.Item) {
				return fmt.Errorf("home: conflicting committed item/version at %q; both records retained", rec.Path)
			}
			var sh sheet.Sheet
			if err := h.getJSON(kindSheet, rec.Sheet, &sh); err != nil {
				return err
			}
			if err := sh.Check(check); err != nil {
				return err
			}
			if sh.Item != rec.Item || sh.Version != rec.Version || sh.SHA256 != rec.Manifest || sh.Registrar.Key != hex.EncodeToString(e.Event.Author) {
				return fmt.Errorf("home: provenance does not match committed version %s", id)
			}
			v := Version{N: rec.Version, Kind: sh.Kind, SHA256: rec.Manifest, Size: sh.Size, Sheets: []string{rec.Sheet},
				Record: desc.Hash.String(), Event: id.String(), By: sh.Registrar.ID, Via: sh.Origin.Via, Previous: rec.Version - 1}
			if _, err := h.manifest(v); err != nil {
				return fmt.Errorf("home: committed manifest missing or corrupt: %w", err)
			}
			item.Context = sh.Context
			for _, name := range append([]string{sh.Name.Exact}, sh.Name.Seen...) {
				if name != "" && !containsString(item.Names, name) {
					item.Names = append(item.Names, name)
				}
			}
			item.Versions = append(item.Versions, v)
			cat.Items[item.ID], cat.Paths[item.Path] = item, item.ID
			if strings.HasPrefix(v.Via, "draft:") {
				if d := cat.Drafts[strings.TrimPrefix(v.Via, "draft:")]; d != nil {
					d.State = "recorded"
				}
			}
		case sheet.DeclarationVerb:
			if !bytes.Equal(e.Event.Author, l.Root()) {
				return errors.New("home: authorship decision is not the owner's")
			}
			if derr != nil || desc.Type != declarationRecordType {
				// Older prototypes did not record all attribution fields. Their
				// existing checked projection is retained; absent evidence is not
				// guessed from the sender role or registrar signature.
				if err := h.recoverLegacyDeclaration(cat, old, id.String(), e.Event.Address, check); err != nil {
					return err
				}
				continue
			}
			var rec declarationRecord
			if err := h.getJSON("declaration", desc.Hash.String(), &rec); err != nil {
				return err
			}
			item := cat.Items[rec.Item]
			if rec.Format != declarationFormat || item == nil || rec.Version < 1 || rec.Version > len(item.Versions) || e.Event.Address != sheet.DeclarationAddress(rec.Item) {
				return fmt.Errorf("home: invalid authorship decision %s", id)
			}
			v := &item.Versions[rec.Version-1]
			if v.SHA256 != rec.Manifest || v.Sheets[len(v.Sheets)-1] != rec.PreviousSheet {
				return fmt.Errorf("home: authorship decision %s has a conflicting base", id)
			}
			var prev sheet.Sheet
			if err := h.getJSON(kindSheet, rec.PreviousSheet, &prev); err != nil {
				return err
			}
			next := h.declaredSheet(prev, rec.PreviousSheet, rec.Input, id.String())
			if err := next.Check(check); err != nil {
				return err
			}
			sid, _, err := h.putJSON(kindSheet, next)
			if err != nil {
				return err
			}
			v.Sheets = append(v.Sheets, sid)
		}
	}
	h.cat = cat
	return nil
}

func (h *Home) recoverLegacyDeclaration(cat, old *Catalog, eventID, address string, check sheet.Ledger) error {
	itemID := strings.TrimPrefix(address, ItemsAddress+"/")
	item, prior := cat.Items[itemID], old.Items[itemID]
	if item != nil && prior != nil {
		for _, ov := range prior.Versions {
			if ov.N < 1 || ov.N > len(item.Versions) || len(ov.Sheets) < 2 {
				continue
			}
			v := &item.Versions[ov.N-1]
			for _, sid := range ov.Sheets[1:] {
				var sh sheet.Sheet
				if h.getJSON(kindSheet, sid, &sh) != nil || sh.Check(check) != nil || sh.SHA256 != v.SHA256 {
					continue
				}
				if len(sh.Declarations) > 0 && sh.Declarations[len(sh.Declarations)-1].Event == eventID && sh.Supersedes == v.Sheets[len(v.Sheets)-1] {
					v.Sheets = append(v.Sheets, sid)
					return nil
				}
			}
		}
	}
	return fmt.Errorf("home: legacy authorship decision %s lacks its complete attribution projection; no author guessed", eventID)
}

func (h *Home) declaredSheet(prev sheet.Sheet, prevID string, in DeclareInput, eventID string) sheet.Sheet {
	next := prev
	next.Revision, next.Supersedes = prev.Revision+1, prevID
	next.Declarations = append(append([]sheet.Declaration(nil), prev.Declarations...), sheet.Declaration{
		By:    sheet.Party{Kind: "owner", ID: OwnerID, Name: OwnerID, Key: hex.EncodeToString(h.rootPub)},
		About: in.About, Author: in.Author, Statement: in.Statement, Event: eventID})
	next.Authorship = in.Authorship
	if in.Segments != nil {
		next.Segments = append([]sheet.Segment(nil), in.Segments...)
		for i := range next.Segments {
			if next.Segments[i].Status == sheet.OwnerDeclared && next.Segments[i].Evidence == "" {
				next.Segments[i].Evidence = eventID
			}
		}
	}
	return next
}

// A stable import/draft identifier identifies the user's attempt, even when
// the last catalog or job-pointer write did not survive a process exit.
func (h *Home) recordedOrigin(a Actor, path, via string) (RecordResult, bool) {
	item := h.itemAt(path)
	if item == nil || via == "" {
		return RecordResult{}, false
	}
	for _, v := range item.Versions {
		if v.Via != via || v.By != actorID(a) {
			continue
		}
		return RecordResult{Record: daemon.Recorded, Event: v.Event, Item: item.ID, Path: item.Path,
			Version: v.N, Sheet: v.Sheets[0], Attempt: fmt.Sprintf("home:record:%.8s:%.12s:%d:%.16s", v.By, item.ID, v.N, v.Record)}, true
	}
	return RecordResult{}, false
}

// Persist a checked projection. Its failure leaves the returned record state
// truthful; the next open can derive it again from the ledger and object store.
func (h *Home) saveCatalog() error { return h.save(ptrCatalog, h.cat) }
