// The rites: import, reunion, and the three carryings.
//
// A rite is a sentence whose work touches the world outside the ledger - the
// library, a berth, a bundle file - and whose honesty therefore needs a fixed
// order of steps. The order is stated at each rite and enforced, not hoped.
package shell

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rokh/bundle"
	"rokh/carrier"
	"rokh/content"
	"rokh/event"
	"rokh/frame"
	"rokh/ledger"
	"rokh/lineage"
	"rokh/oracle"
	"rokh/turn"
	"rokh/vessel"
)

// doImport is "bring": the import rite.
//
//	hash -> copy into the library -> re-read and compare -> only then one event.
//
// A file becomes one blob and one descriptor event. A folder becomes one blob
// per file plus one canonical tree manifest, referenced by ONE descriptor
// event. Symlinks and special files are refused - the whole import fails and
// the source is untouched, which is what the reply says.
func (s *session) doImport(c command) (string, error) {
	l, err := s.requireOwn()
	if err != nil {
		return "", err
	}
	fi, err := os.Lstat(c.Path)
	if err != nil {
		return fmt.Sprintf(tplImportFail, "there is no file or folder by that name here"), nil
	}
	var desc content.Descriptor
	var blobs []blob
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return fmt.Sprintf(tplImportFail, "it is a symbolic link, and it would make the tree's identity a lie"), nil
	case fi.Mode().IsRegular():
		desc, blobs, err = importFile(c.Path)
	case fi.IsDir():
		desc, blobs, err = importFolder(c.Path)
	default:
		return fmt.Sprintf(tplImportFail, "it is neither a file nor a folder"), nil
	}
	if err != nil {
		return fmt.Sprintf(tplImportFail, err.Error()), nil
	}

	payload, err := desc.Encode()
	if err != nil {
		return "", err
	}
	priv, authority, err := l.signer()
	if err != nil {
		return "", err
	}
	att, _ := oracle.Observe(oracle.Default())
	anchor := l.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Authority: authority, Parents: l.branchHead(),
		Address: c.Address, Verb: content.VerbPut, Payload: payload, Attest: att,
	}, priv)
	if err != nil {
		return "", err
	}
	// The bytes live inside the vessel (contract E5), in the same commit as
	// the event that names them: before the commit nothing, after it both.
	if err := l.commitWith(e, func(r *carrier.Recording) error {
		for _, b := range blobs {
			if _, _, err := content.BringBytes(r, c.Address, b.body, b.mediaType); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf(tplImported, e.ID.String()), nil
}

// blob is one piece of content a bring puts into the vessel.
type blob struct {
	body      []byte
	mediaType string
}

// describe names a blob by its own bytes.
func describe(body []byte, mediaType string) (content.Descriptor, blob, error) {
	d, err := content.New(body, mediaType)
	return d, blob{body: body, mediaType: mediaType}, err
}

func mediaTypeOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".json":
		return "application/json"
	}
	return "application/octet-stream"
}

func importFile(path string) (content.Descriptor, []blob, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return content.Descriptor{}, nil, err
	}
	d, b, err := describe(body, mediaTypeOf(path))
	return d, []blob{b}, err
}

// importFolder builds the canonical tree: sorted relative paths, size and
// hash per file. No symlinks, no special files, and no OS permissions or
// times in the identity.
func importFolder(dir string) (content.Descriptor, []blob, error) {
	var entries []content.TreeEntry
	var blobs []blob
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, p)
			if d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("a symbolic link at %s; a tree must mean one thing", rel)
			}
			return fmt.Errorf("a special file at %s; a tree takes only files", rel)
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		desc, b, err := describe(body, "application/octet-stream")
		if err != nil {
			return err
		}
		blobs = append(blobs, b)
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		entries = append(entries, content.TreeEntry{
			Path: filepath.ToSlash(rel), Size: desc.Size, Hash: desc.Hash,
		})
		return nil
	})
	if err != nil {
		return content.Descriptor{}, nil, err
	}
	if len(entries) == 0 {
		return content.Descriptor{}, nil, errors.New("the folder is empty; there is nothing to bring")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	manifest, err := content.EncodeTree(entries)
	if err != nil {
		return content.Descriptor{}, nil, err
	}
	d, b, err := describe(manifest, content.MediaTypeTree)
	return d, append(blobs, b), err
}

// ---------- reunion of berths ----------

// doReunite is "bring the returned ledger": scan the seats for berths of the current
// ledger and unite with each. A seat that has not declared itself is not
// operated on; a mirror is never united; a different anchor is refused and
// union is never offered for it.
func (s *session) doReunite(command) (string, error) {
	l, err := s.requireOwn()
	if err != nil {
		return "", err
	}
	seats := s.readSeats()
	if len(seats) == 0 {
		return "", errors.New("there is no seats folder; name one when you open the vault: rokh VAULT -mount FOLDER")
	}
	anchor := l.led.Genesis().String()
	for _, st := range seats {
		if st.declErr != "" {
			fmt.Fprintf(s.notes, "(seat %s: %s)\n", st.name, st.declErr)
			continue
		}
		switch st.kind {
		case "":
			fmt.Fprintf(s.notes, "(seat %s has not declared itself; I will not touch it)\n", st.name)
			continue
		case "mirror":
			fmt.Fprintf(s.notes, "(seat %s is a mirror; read-only, never made one with this)\n", st.name)
			continue
		case "berth":
		default:
			fmt.Fprintf(s.notes, "(seat %s declares a kind I do not know)\n", st.name)
			continue
		}
		if st.anchor != anchor {
			return fmt.Sprintf("this is another ledger; its anchor is different — they do not meet. (seat %s)", st.name), nil
		}
		// Both carriers are written: each one's turn is taken, and ours is read
		// afresh under it, before anything is offered either way.
		//   — T8.5, T4.4
		ours, err := turn.Acquire(l.dir, turnPatience)
		if err != nil {
			return "", fmt.Errorf("another writer holds this ledger; nothing was united: %w", err)
		}
		defer ours.Release()
		theirs, err := turn.Acquire(st.dir, turnPatience)
		if err != nil {
			return "", fmt.Errorf("another writer holds the berth; nothing was united: %w", err)
		}
		defer theirs.Release()
		l.reread()
		return s.unite(l, st, ours, theirs)
	}
	return "", errors.New("no berth of this ledger is among the seats")
}

// unite performs the two-way union with one berth: each side's raw signed
// bytes are offered to the other side's Add; each ledger alone judges what it
// receives; then each side installs the cells of the live keys it holds
// (R2). Nothing merges here - if two heads remain, the sentence asks, and
// only the exact reply closes them.
func (s *session) unite(l *openLedger, st seat, ours, theirs *turn.Lock) (string, error) {
	bc, _, err := s.openCarrier(st.dir, s.pass)
	if err != nil {
		return "", fmt.Errorf("the berth would not open: %w", err)
	}
	// The union of records both ways, each side under its own turn, each half
	// named (contract 4.8). Every event that crosses is judged by the ledger
	// that reads it when the carrier is read again.
	sa, err := judgedSide(l.car, ours, s.pass)
	if err != nil {
		return "", err
	}
	sb, err := judgedSide(bc, theirs, s.pass)
	if err != nil {
		return "", err
	}
	res, err := lineage.Reconcile(sa, sb)
	if err != nil {
		return "", reconcileRefusal(res.Local.Outcome, res.Remote)
	}
	// Then each side installs the cells of the live keys it holds and lacks
	// (R2), as the command line's reconcile does. A side that did not take
	// them fails the rite; the union stays recorded on both sides.
	for _, side := range []struct {
		name string
		c    *carrier.Carrier
		own  *turn.Lock
	}{{"this ledger", l.car, ours}, {"the berth", bc, theirs}} {
		n, byKey, out, err := learnCells(side.c, side.own, s.pass)
		switch {
		case err != nil:
			record := "not recorded"
			if out == vessel.Unknown {
				record = "unknown"
			}
			return "", refusal{code: "cells_not_installed", record: record,
				err: fmt.Errorf("the union is recorded on both sides, and %s did not take the cells of the keys it holds: %w", side.name, err)}
		case s.notes == nil:
		case byKey:
			fmt.Fprintf(s.notes, "(%s: no key cell installed; only the owner's passphrase tells a free cell from the owner's own)\n", side.name)
		case n > 0:
			fmt.Fprintf(s.notes, "(%s installed %s)\n", side.name, countOf(n, "key cell"))
		}
	}
	l.reread()
	bl, err := replay(bc)
	if err != nil {
		return "", err
	}
	fromThem, fromUs := res.Local.Added, res.Remote.Added
	verdicts := map[string]int{}
	for _, led := range []*ledger.Ledger{l.led, bl} {
		_, r, p := led.Tally()
		verdicts["rejected"] += r
		verdicts["pending"] += p
	}

	ruling := "all accepted"
	if verdicts["rejected"] > 0 || verdicts["pending"] > 0 {
		ruling = fmt.Sprintf("%s rejected, %s waiting",
			digits(verdicts["rejected"]), digits(verdicts["pending"]))
	}

	heads := l.led.Heads()
	_, r, p := l.led.Tally()
	if len(heads) > 1 && r == 0 && p == 0 {
		s.pending = &pendingMerge{
			ledgerName: l.name, heads: heads, berthDir: st.dir, berthPass: s.pass,
		}
		return fmt.Sprintf(tplReunited, countOf(fromThem, "event"), digits(fromUs), ruling), nil
	}
	return fmt.Sprintf("%s new from them, %s from us; %s. %s.",
		countOf(fromThem, "event"), digits(fromUs), ruling,
		countOf(len(heads), "head")), nil
}

// doReconcile is the exact reply to "reconcile": one rokh.merge, signed with a
// key the ledger accepts, committed to both sides. Neither side is erased and
// neither wins; the heads simply meet.
func (s *session) doReconcile() (string, error) {
	if s.pending == nil {
		return "", errors.New("there is nothing left to reconcile")
	}
	pm := s.pending
	s.pending = nil
	l, ok := s.open[pm.ledgerName]
	if !ok {
		return "", errors.New("that ledger is no longer open")
	}
	priv, authority, err := l.signer()
	if err != nil {
		return "", err
	}
	att, _ := oracle.Observe(oracle.Default())
	anchor := l.car.Anchor()
	e, err := event.SignFresh(event.Event{
		Carrier: &anchor, Authority: authority, Parents: pm.heads,
		Address: event.AddressRoot, Verb: event.VerbMerge, Attest: att,
	}, priv)
	if err != nil {
		return "", err
	}
	if err := l.commit(e); err != nil {
		return "", err
	}
	// The berth meets the same point, and its half is named beside ours.
	//
	// The berth's half is one recording on the berth, under the berth's own
	// turn: the heads that reached it and the merge, in one vessel commit.
	// Every step's error is kept. A berth that did not take the merge is a
	// failure of the whole, never "they met": the first half is never
	// reported as the whole (contract 4.8, R5).
	//   — T8.5
	berth := s.meetOnBerth(pm, e)
	if berth.Outcome != vessel.Recorded {
		return "", reconcileRefusal(vessel.Recorded, berth)
	}
	return fmt.Sprintf(tplMerged, e.ID.String()), nil
}

// meetOnBerth records the merge on the berth: every head of the merge whose
// event the berth holds is named there, then the merge itself, in one commit.
// It answers the berth's half with its outcome and its error, never nothing.
func (s *session) meetOnBerth(pm *pendingMerge, e event.Signed) lineage.Half {
	lock, err := turn.Acquire(pm.berthDir, turnPatience)
	if err != nil {
		return lineage.Half{Outcome: vessel.NotRecorded, Err: fmt.Errorf("another writer holds the berth: %w", err)}
	}
	defer lock.Release()
	bc, _, err := s.openCarrier(pm.berthDir, pm.berthPass)
	if err != nil {
		return lineage.Half{Outcome: vessel.NotRecorded, Err: fmt.Errorf("the berth would not open: %w", err)}
	}
	r, err := bc.Begin(lock)
	if err != nil {
		return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
	}
	for i, h := range pm.heads {
		if _, err := bc.Get(h); err != nil {
			r.Abandon()
			return lineage.Half{Outcome: vessel.NotRecorded, Err: fmt.Errorf("the berth does not hold head %s: %w", h.Short(), err)}
		}
		if err := r.SetRef(fmt.Sprintf("%s-%d", defaultBranch, i), e.ID); err != nil {
			r.Abandon()
			return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
		}
	}
	if err := r.Event(e.ID, e.Head, e.Body, event.AddressRoot); err != nil {
		r.Abandon()
		return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
	}
	if err := r.SetRef(defaultBranch, e.ID); err != nil {
		r.Abandon()
		return lineage.Half{Outcome: vessel.NotRecorded, Err: err}
	}
	out, err := r.Commit()
	return lineage.Half{Outcome: out, Added: 1, Err: err}
}

// reconcileRefusal names both halves of a reconcile that did not complete;
// an unknown half keeps its own word.
func reconcileRefusal(local vessel.Outcome, remote lineage.Half) error {
	record := "not recorded"
	if remote.Outcome == vessel.Unknown {
		record = "unknown"
	}
	return refusal{code: "reconcile_incomplete", record: record,
		err: fmt.Errorf("this ledger: %s; the berth: %s (%v)", local, remote.Outcome, remote.Err)}
}

// ---------- the three carryings ----------

// doCarryLedger is "carry the ledger": the keyless carrier copy. A fresh carrier at the
// path with the same anchor, objects and refs, and NO keyring material at
// all; verified against the anchor after writing. The seat declaration is
// written beside it, its ruling being the very sentence that made it.
// Key-carrying copies are a separate ruling and are not built.
func (s *session) doCarryLedger(c command) (string, error) {
	if _, err := s.requireOwn(); err != nil {
		return "", err
	}
	// In v1 no folder becomes a rokh except by init or by seed (contract
	// 4.7 S4): a copy of the ledger is a seed, with its own vessel key and the
	// give and take recorded on both sides. The seed's keyring events are the
	// key layer's to sign.
	return "", fmt.Errorf("a whole copy of a ledger is a seed; make it on the command line: rokh seed %s %s", s.current.dir, c.Path)
}

// doCarryAddress is "carry {address}": export the payloads at that address - and any
// referenced content, hash-verified - as plain files. Zero events; the ledger
// unchanged, and the reply says so.
func (s *session) doCarryAddress(c command) (string, error) {
	l, err := s.require()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(c.Path, 0o700); err != nil {
		return "", err
	}
	before := l.led.Len()
	n := 0
	for _, id := range l.led.Order() {
		e, _ := l.led.Get(id)
		if !event.ScopeCovers(c.Address, e.Event.Address) {
			continue
		}
		switch e.Event.Verb {
		case content.VerbPut:
			d, err := content.Decode(e.Event.Payload)
			if err != nil {
				continue
			}
			var buf bytes.Buffer
			if err := content.Fetch(l.car, d, &buf); err != nil {
				fmt.Fprintf(s.notes, "(%s is not readable in this carrier; left behind)\n", d.Hash.Short())
				continue
			}
			body := buf.Bytes()
			if err := d.Verify(body); err != nil {
				fmt.Fprintf(s.notes, "(%s does not match its witness; left behind)\n", d.Hash.Short())
				continue
			}
			if d.Type == content.MediaTypeTree {
				t, err := content.DecodeTree(body)
				if err != nil {
					continue
				}
				wrote, err := s.exportTree(l, c.Path, t)
				if err != nil {
					return "", err
				}
				n += wrote
				continue
			}
			out := filepath.Join(c.Path, d.Hash.String()[:16]+d.Extension())
			if err := os.WriteFile(out, body, 0o600); err != nil {
				return "", err
			}
			n++
		default:
			if len(e.Event.Payload) == 0 {
				continue
			}
			out := filepath.Join(c.Path, id.String()[:16]+".txt")
			if err := os.WriteFile(out, e.Event.Payload, 0o600); err != nil {
				return "", err
			}
			n++
		}
	}
	if l.led.Len() != before {
		return "", errors.New("export changed the ledger; that must never happen")
	}
	return fmt.Sprintf(tplExported, countOf(n, "writing")), nil
}

func (s *session) exportTree(l *openLedger, dst string, t content.Tree) (int, error) {
	n := 0
	for _, en := range t.Entries {
		blob := content.Descriptor{Hash: en.Hash, Size: en.Size, Type: "application/octet-stream"}
		var buf bytes.Buffer
		if err := content.Fetch(l.car, blob, &buf); err != nil {
			fmt.Fprintf(s.notes, "(%s from the tree is not readable in this carrier; left behind)\n", en.Path)
			continue
		}
		body := buf.Bytes()
		if err := blob.Verify(body); err != nil {
			fmt.Fprintf(s.notes, "(%s does not match its witness; left behind)\n", en.Path)
			continue
		}
		out := filepath.Join(dst, filepath.FromSlash(en.Path))
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return n, err
		}
		if err := os.WriteFile(out, body, 0o600); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// doCarryBundle is "carry the bundle": the courier bundle for one peer, under their
// live covenants and nothing else. What was excluded is named. A bundle
// permits carrying; it sends nothing.
func (s *session) doCarryBundle(c command) (string, error) {
	l, err := s.require()
	if err != nil {
		return "", err
	}
	peer, err := l.keyFor(c.Who)
	if err != nil {
		return "", err
	}
	sel, err := bundle.Disclosable(l.led, peer, frame.Zero, "", false)
	if err != nil {
		if errors.Is(err, bundle.ErrNoCovenant) {
			return "", errors.New(`nothing is opened to them to read; first "entrust reading <place> to <who>"`)
		}
		return "", err
	}
	if len(sel.IDs) == 0 {
		return "", errors.New("there is nothing under that opening to carry")
	}
	name := fmt.Sprintf("rokh-bundle-%s.jsonl", hex.EncodeToString(peer)[:12])
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("the bundle %s already exists; I will not write over it", name)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	have := bundle.Have{Kind: bundle.KindHave, Ledger: l.led.Genesis().String()}
	for _, id := range sel.IDs {
		have.IDs = append(have.IDs, id.String())
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(have); err != nil {
		return "", err
	}
	for _, id := range sel.IDs {
		e, _ := l.led.Get(id)
		if err := enc.Encode(struct {
			ID  string `json:"id"`
			Raw string `json:"raw"`
		}{id.String(), hex.EncodeToString(e.Raw)}); err != nil {
			return "", err
		}
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	excluded := "nothing"
	if len(sel.Missing) > 0 {
		excluded = fmt.Sprintf("ancestors that were never entrusted (%s)", countOf(len(sel.Missing), "event"))
	}
	fmt.Fprintf(s.notes, "(bundled at %s)\n", name)
	return fmt.Sprintf(tplBundled, countOf(len(sel.IDs), "event"), excluded), nil
}
