package home

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"rokh-home/archive"
	"rokh-home/authority"
	"rokh-home/index"
	"rokh-home/sheet"
	"rokh-home/store"
	"rokh/content"
	"rokh/daemon"
	"rokh/frame"
)

type storeIO struct{ st *store.Home }

func (s storeIO) PutBlob(id string, r io.Reader) (int64, [32]byte, error) {
	info, err := s.st.Put(kindBlob, id, r, nil)
	return info.Size, info.SHA256, err
}

func (s storeIO) OpenBlob(id string) (io.ReadCloser, error) { return s.st.Get(kindBlob, id) }

// RecordResult is what recording a version did, in the ledger's own words.
type RecordResult struct {
	Record  string `json:"record"`
	Code    string `json:"code,omitempty"`
	Error   string `json:"error,omitempty"`
	Event   string `json:"event,omitempty"`
	Attempt string `json:"attempt,omitempty"`
	Item    string `json:"item,omitempty"`
	Path    string `json:"path,omitempty"`
	Version int    `json:"version,omitempty"`
	Sheet   string `json:"sheet,omitempty"`
}

type recordManifest struct {
	Format   string `json:"format"`
	Item     string `json:"item"`
	Version  int    `json:"version"`
	Path     string `json:"path"`
	Manifest string `json:"manifest"`
	Sheet    string `json:"sheet"`
}

func (h *Home) putJSON(kind string, v any) (string, []byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	id := hex.EncodeToString(sum[:])
	if _, err := h.st.Put(kind, id, bytes.NewReader(b), sum[:]); err != nil {
		return "", nil, err
	}
	return id, b, nil
}

func (h *Home) getJSON(kind, id string, v any) error {
	b, err := h.readObject(kind, id, 64<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Read one extra byte so a limit never substitutes for authenticated EOF.
// Reject oversized objects instead of accepting a valid truncated prefix.
func (h *Home) readObject(kind, id string, limit int64) ([]byte, error) {
	r, err := h.st.Get(kind, id)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("home: %s exceeds its %d byte metadata limit", kind, limit)
	}
	return b, nil
}

func (h *Home) manifest(v Version) (archive.Manifest, error) {
	var m archive.Manifest
	b, err := h.readObject(kindTree, v.SHA256, 256<<20)
	if err != nil {
		return m, err
	}
	return archive.DecodeManifest(b)
}

func (h *Home) sheetOf(v Version) (sheet.Sheet, string, error) {
	var s sheet.Sheet
	if len(v.Sheets) == 0 {
		return s, "", errors.New("home: the version has no sheet")
	}
	id := v.Sheets[len(v.Sheets)-1]
	return s, id, h.getJSON(kindSheet, id, &s)
}

func (h *Home) ledgerCheck(id string) (sheet.EventFacts, bool) {
	acc, byOwner, addr, verb, found := h.eventFacts(id)
	return sheet.EventFacts{Accepted: acc, ByOwner: byOwner, Address: addr, Verb: verb}, found
}

func (h *Home) registrar(a Actor) sheet.Party {
	if a.Owner {
		return sheet.Party{Kind: "owner", ID: OwnerID, Name: OwnerID, Key: hex.EncodeToString(h.rootPub)}
	}
	p := sheet.Party{Kind: "consumer", ID: a.Consumer}
	if c, ok := h.reg.Get(a.Consumer); ok {
		p.Name, p.Key = c.Name, c.KeyPublic
	}
	return p
}

func lastSegment(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// recordVersion records a complete manifest as the next version of the item at
// path. It is called with the home's lock held.
func (h *Home) recordVersion(a Actor, path string, m archive.Manifest, origin sheet.Origin) (RecordResult, error) {
	if old, ok := h.recordedOrigin(a, path, origin.Via); ok {
		return old, nil
	}
	res := RecordResult{Path: path}
	enc, sum, err := m.Encode()
	if err != nil {
		return res, err
	}
	manifestID := hex.EncodeToString(sum[:])
	if _, err := h.st.Put(kindTree, manifestID, bytes.NewReader(enc), sum[:]); err != nil {
		return res, err
	}
	item := h.itemAt(path)
	fresh := item == nil
	if fresh {
		item = &Item{ID: newID(), Path: path, Names: []string{m.Name}}
	} else if item.Versions[len(item.Versions)-1].Kind != kindOf(m) {
		return res, fmt.Errorf("home: %s holds a %s; a %s is not a version of it", path, item.Versions[len(item.Versions)-1].Kind, kindOf(m))
	} else {
		next := *item
		next.Names = append([]string(nil), item.Names...)
		next.Versions = append([]Version(nil), item.Versions...)
		item = &next
	}
	if !containsString(item.Names, m.Name) {
		item.Names = append(item.Names, m.Name)
	}
	n := len(item.Versions) + 1
	var size int64
	for _, e := range m.Entries {
		size += e.Size
	}
	sh := sheet.Sheet{Format: sheet.Format, Item: item.ID, Version: n, Revision: 1, Kind: kindOf(m),
		SHA256: manifestID, Size: size, Name: sheet.Name{Exact: m.Name, Seen: item.Names}, Context: item.Context,
		Origin: origin, Registrar: h.registrar(a), Authorship: sheet.Unknown, Previous: n - 1}
	if kindOf(m) == "file" && len(m.Entries) == 1 {
		sh.Size = m.Entries[0].Size
	}
	if err := sh.Check(h.ledgerCheck); err != nil {
		return res, err
	}
	sheetID, _, err := h.putJSON(kindSheet, sh)
	if err != nil {
		return res, err
	}
	recID, recBytes, err := h.putJSON(kindRecord, recordManifest{Format: RecordFormat, Item: item.ID, Version: n,
		Path: path, Manifest: manifestID, Sheet: sheetID})
	if err != nil {
		return res, err
	}
	hash, _ := frame.ParseID(recID)
	payload, err := content.Descriptor{Hash: hash, Size: uint64(len(recBytes)), Type: RecordType}.Encode()
	if err != nil {
		return res, err
	}
	door, key := h.ownerDoor, ""
	by := OwnerID
	if !a.Owner {
		door, by = h.progDoor, a.Consumer
		if key = h.itemsKey(a.Consumer); key == "" {
			return res, &Denied{Decision: authority.Decision{Consumer: a.Consumer, Action: authority.Record, Path: path, Reason: "no_ledger_key"}}
		}
	}
	res.Attempt = fmt.Sprintf("home:record:%.8s:%.12s:%d:%.16s", by, item.ID, n, recID)
	r := ask(door, map[string]any{"op": "write", "address": ItemsAddress + "/" + item.ID, "verb": content.VerbPut,
		"payload": b64(payload), "key": key, "attempt": res.Attempt})
	if r["record"] == daemon.Unknown {
		// The ending was not read back. Ask under the attempt's name; never
		// write again blind.
		r = ask(door, map[string]any{"op": "attempt", "attempt": res.Attempt, "key": key})
		if r["record"] != daemon.Recorded {
			res.Record, res.Code = daemon.Unknown, "storage_failed"
			return res, errors.New("home: whether the version was recorded is unknown; ask again under its attempt")
		}
	}
	if r["record"] != daemon.Recorded {
		res.Record, res.Code, res.Error = fmt.Sprint(r["record"]), fmt.Sprint(r["code"]), fmt.Sprint(r["error"])
		return res, fmt.Errorf("home: the version was not recorded: %s", res.Error)
	}
	res.Record, res.Event, res.Item, res.Version, res.Sheet = daemon.Recorded, fmt.Sprint(r["id"]), item.ID, n, sheetID
	item.Versions = append(item.Versions, Version{N: n, Kind: kindOf(m), SHA256: manifestID, Size: size,
		Sheets: []string{sheetID}, Record: recID, Event: res.Event, By: by, Via: origin.Via, Previous: n - 1})
	h.cat.Items[item.ID] = item
	h.cat.Paths[path] = item.ID
	// Recording already committed. Both live projections follow that fact
	// even if saving the replaceable catalog pointer fails afterwards.
	h.indexVersion(item, item.Versions[len(item.Versions)-1])
	if err := h.save(ptrCatalog, h.cat); err != nil {
		return res, err
	}
	return res, nil
}

func kindOf(m archive.Manifest) string {
	if m.Single {
		return "file"
	}
	return "tree"
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (h *Home) itemAt(path string) *Item {
	id, ok := h.cat.Paths[path]
	if !ok {
		return nil
	}
	return h.cat.Items[id]
}

func (h *Home) itemsKey(consumer string) string {
	name := keyName(consumer, "")
	if _, ok := h.keys.Keys[name]; ok {
		return name
	}
	return ""
}

func keyName(consumer, namespace string) string {
	if namespace == "" {
		return "c-" + consumer[:16]
	}
	return "c-" + consumer[:16] + "@" + namespace
}

// within says whether path lies inside root, both made absolute and with the
// root's own links resolved.
func within(root, path string) bool {
	if root == "" {
		return false
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return false
	}
	p := filepath.Join(parent, filepath.Base(abs))
	return p != r && strings.HasPrefix(p, r+string(filepath.Separator))
}

// ---------- import ----------

type importJob struct {
	Journal archive.Journal `json:"journal"`
	Path    string          `json:"path"`
	By      string          `json:"by"`
	Source  string          `json:"source"`
	Result  *RecordResult   `json:"result,omitempty"`
}

// ImportPreview looks at a source and prepares its import to path. A program
// may only bring files in from its own intake folder.
func (h *Home) ImportPreview(a Actor, source, path string) (*archive.Preview, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.require(a, authority.Record, path); err != nil {
		return nil, "", err
	}
	if !a.Owner && !within(h.places[a.Consumer].Intake, source) {
		return nil, "", &Denied{Decision: authority.Decision{Consumer: a.Consumer, Action: authority.Record, Path: path, Reason: "source_outside_intake"}}
	}
	p, err := archive.MakePreview(source, h.root)
	if err != nil {
		return nil, "", err
	}
	if len(p.Refused) > 0 {
		return p, "", archive.ErrRefused
	}
	if !p.Fits {
		return p, "", archive.ErrNoSpace
	}
	j, err := archive.NewJournal(p)
	if err != nil {
		return p, "", err
	}
	job := importJob{Journal: *j, Path: path, By: actorID(a), Source: p.Source}
	if err := h.save(ptrJournal+j.ID, job); err != nil {
		return p, "", err
	}
	return p, j.ID, nil
}

func actorID(a Actor) string {
	if a.Owner {
		return OwnerID
	}
	return a.Consumer
}

func (h *Home) loadJob(a Actor, id string) (*importJob, error) {
	b, ok, err := h.st.Pointer(ptrJournal + id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	var job importJob
	if err := json.Unmarshal(b, &job); err != nil {
		return nil, err
	}
	if job.By != actorID(a) {
		return nil, ErrNotFound
	}
	return &job, nil
}

// ImportRun copies what an import has not copied yet and records the version.
// Authority is decided again when it runs, not only when it was previewed.
func (h *Home) ImportRun(a Actor, id string) (RecordResult, error) {
	h.mu.Lock()
	job, err := h.loadJob(a, id)
	if err == nil {
		_, err = h.require(a, authority.Record, job.Path)
	}
	if err == nil {
		if res, ok := h.recordedOrigin(a, job.Path, "import:"+id); ok {
			h.mu.Unlock()
			return res, nil
		}
	}
	h.mu.Unlock()
	if err != nil {
		return RecordResult{}, err
	}
	if job.Result != nil && job.Result.Record == daemon.Recorded {
		return *job.Result, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.jobsMu.Lock()
	if h.closing {
		h.jobsMu.Unlock()
		cancel()
		return RecordResult{Record: daemon.NotRecorded, Code: "home_closed"}, errors.New("home: the broker is closing")
	}
	if _, running := h.jobs[id]; running {
		h.jobsMu.Unlock()
		cancel()
		return RecordResult{}, errors.New("home: that import is already running")
	}
	h.jobs[id] = cancel
	h.jobsWait.Add(1)
	h.jobsMu.Unlock()
	defer func() {
		defer h.jobsWait.Done()
		h.jobsMu.Lock()
		delete(h.jobs, id)
		h.jobsMu.Unlock()
		cancel()
	}()
	save := func(j *archive.Journal) error {
		job.Journal = *j
		return h.save(ptrJournal+id, job)
	}
	j := job.Journal
	if err := j.Run(ctx, storeIO{h.st}, save); err != nil {
		return RecordResult{Path: job.Path, Record: daemon.NotRecorded, Code: j.State, Error: err.Error()}, err
	}
	m, err := j.Manifest()
	if err != nil {
		return RecordResult{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return RecordResult{Record: daemon.NotRecorded, Code: "stopped"}, err
	}
	if _, err := h.require(a, authority.Record, job.Path); err != nil {
		return RecordResult{}, err
	}
	res, err := h.recordVersion(a, job.Path, m, sheet.Origin{Via: "import:" + id, Source: job.Source})
	job.Result = &res
	job.Journal = j
	if serr := h.save(ptrJournal+id, job); serr != nil && err == nil {
		err = serr
	}
	return res, err
}

// ImportStop stops a running import; its journal keeps what was copied.
func (h *Home) ImportStop(a Actor, id string) error {
	h.mu.Lock()
	_, err := h.loadJob(a, id)
	h.mu.Unlock()
	if err != nil {
		return err
	}
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	stop, running := h.jobs[id]
	if !running {
		return errors.New("home: that import is not running")
	}
	stop()
	return nil
}

// ImportStatus says where an import stands. Authority is decided again here
// too: an import's own journal is still the home's to answer for, and a program
// whose standing or whose grant was withdrawn is not answered after it.
func (h *Home) ImportStatus(a Actor, id string) (map[string]any, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	job, err := h.loadJob(a, id)
	if err != nil {
		return nil, err
	}
	if _, err := h.require(a, authority.Record, job.Path); err != nil {
		return nil, err
	}
	copied := int64(0)
	for _, d := range job.Journal.Done {
		copied += d.Size
	}
	out := map[string]any{"id": id, "state": job.Journal.State, "note": job.Journal.Note, "path": job.Path,
		"files": job.Journal.Preview.Files, "files_copied": len(job.Journal.Done), "bytes": job.Journal.Preview.Bytes,
		"bytes_copied": copied, "runs": job.Journal.Runs}
	if job.Result != nil {
		out["result"] = job.Result
	}
	return out, nil
}

// ---------- drafts ----------

// DraftView is a draft as its writer sees it.
type DraftView struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Base     int    `json:"base"`
	Revision int    `json:"revision"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	State    string `json:"state"`
	Recorded bool   `json:"recorded"`
}

func (d *Draft) view() DraftView {
	return DraftView{ID: d.ID, Path: d.Path, Base: d.Base, Revision: d.Revision, SHA256: d.SHA256, Size: d.Size,
		State: d.State, Recorded: d.State == "recorded"}
}

// DraftOpen opens a draft at path: of the latest version if there is one, or
// empty. Opening, reading and writing a draft record nothing.
//
// A draft of a version starts from that version's own bytes, and a draft is
// read back by whoever opened it. Starting one is therefore a handing-over of
// content, and needs bytes as well as draft: an editor edits what it may read.
// Where there is nothing to start from, draft alone opens an empty draft.
func (h *Home) DraftOpen(a Actor, path string) (DraftView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.require(a, authority.Draft, path); err != nil {
		return DraftView{}, err
	}
	var body []byte
	base := 0
	if item := h.itemAt(path); item != nil {
		v := item.Versions[len(item.Versions)-1]
		if v.Kind != "file" {
			return DraftView{}, errors.New("home: a tree is changed by importing it again, not by a draft")
		}
		if _, err := h.require(a, authority.Bytes, path); err != nil {
			return DraftView{}, err
		}
		b, err := h.versionBytes(v, "")
		if err != nil {
			return DraftView{}, err
		}
		body, base = b, v.N
	}
	d := &Draft{ID: newID(), Path: path, Base: base, By: actorID(a), State: "open"}
	if err := h.putDraft(d, body); err != nil {
		return DraftView{}, err
	}
	return d.view(), h.persistDraft(d)
}

func (h *Home) putDraft(d *Draft, body []byte) error {
	d.Revision++
	sum := sha256.Sum256(body)
	d.Blob = fmt.Sprintf("draft/%s/%d/%x", d.ID, d.Revision, sum[:])
	_, err := h.st.Put(kindBlob, d.Blob, bytes.NewReader(body), sum[:])
	if err != nil {
		return err
	}
	d.SHA256, d.Size = hex.EncodeToString(sum[:]), int64(len(body))
	return nil
}

func (h *Home) draftFor(a Actor, id string, action authority.Action) (*Draft, error) {
	d, ok := h.cat.Drafts[id]
	if !ok || (d.By != actorID(a) && !a.Owner) {
		return nil, &Denied{Decision: authority.Decision{Consumer: actorID(a), Action: action, Path: "draft:" + id, Reason: authority.NoGrant}}
	}
	if _, err := h.require(a, action, d.Path); err != nil {
		return nil, err
	}
	return d, nil
}

// DraftWrite replaces a draft's bytes, if its revision is still the one the
// writer last saw.
func (h *Home) DraftWrite(a Actor, id string, body []byte, expectRevision int) (DraftView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, err := h.draftFor(a, id, authority.Draft)
	if err != nil {
		return DraftView{}, err
	}
	if d.State != "open" {
		return d.view(), errors.New("home: that draft is recorded; open a new one")
	}
	if expectRevision != d.Revision {
		return d.view(), fmt.Errorf("home: the draft is at revision %d, not %d; nothing was written", d.Revision, expectRevision)
	}
	next := *d
	if err := h.putDraft(&next, body); err != nil {
		return DraftView{}, err
	}
	if err := h.persistDraft(&next); err != nil {
		return d.view(), err
	}
	return next.view(), nil
}

// DraftRead reads a draft.
func (h *Home) DraftRead(a Actor, id string) ([]byte, DraftView, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	d, err := h.draftFor(a, id, authority.Draft)
	if err != nil {
		return nil, DraftView{}, err
	}
	r, err := h.st.Get(kindBlob, d.Blob)
	if err != nil {
		return nil, DraftView{}, err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	return b, d.view(), err
}

// DraftRecord records a draft as the next version of its item: one signed
// event, by the program's own key.
func (h *Home) DraftRecord(a Actor, id string) (RecordResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, err := h.draftFor(a, id, authority.Record)
	if err != nil {
		return RecordResult{}, err
	}
	if d.State == "recorded" {
		if old, ok := h.recordedOrigin(a, d.Path, "draft:"+d.ID); ok {
			return old, nil
		}
		return RecordResult{}, errors.New("home: recorded draft has no committed version")
	}
	if item := h.itemAt(d.Path); item != nil && item.Versions[len(item.Versions)-1].N != d.Base {
		return RecordResult{Path: d.Path, Record: daemon.NotRecorded, Code: "precondition_failed"},
			fmt.Errorf("home: %s has a newer version than this draft's base; nothing was recorded", d.Path)
	}
	m := archive.Manifest{Format: archive.TreeFormat, Name: lastSegment(d.Path), Single: true, Mode: 0o644,
		Entries: []archive.Entry{{Path: lastSegment(d.Path), Kind: "file", Mode: 0o644, Size: d.Size, SHA256: d.SHA256, Blob: d.Blob}}}
	res, err := h.recordVersion(a, d.Path, m, sheet.Origin{Via: "draft:" + d.ID})
	if res.Record == daemon.Recorded {
		d.State = "recorded"
		if saveErr := h.save(ptrCatalog, h.cat); err == nil {
			err = saveErr
		}
	}
	return res, err
}

// ---------- reading ----------

// VersionView is a version as a reader sees it.
type VersionView struct {
	N          int    `json:"n"`
	Kind       string `json:"kind"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Event      string `json:"event"`
	By         string `json:"by"`
	Authorship string `json:"authorship"`
	Sheet      string `json:"sheet"`
}

// ItemView is an item as a reader sees it.
type ItemView struct {
	ID       string        `json:"id"`
	Path     string        `json:"path"`
	Names    []string      `json:"names"`
	Versions []VersionView `json:"versions"`
}

func (h *Home) view(item *Item) ItemView {
	iv := ItemView{ID: item.ID, Path: item.Path, Names: append([]string(nil), item.Names...)}
	for _, v := range item.Versions {
		vv := VersionView{N: v.N, Kind: v.Kind, SHA256: v.SHA256, Size: v.Size, Event: v.Event, By: v.By}
		if s, id, err := h.sheetOf(v); err == nil {
			vv.Authorship, vv.Sheet = s.Authorship, id
		}
		iv.Versions = append(iv.Versions, vv)
	}
	return iv
}

// List lists the items under scope the asker may read. What it may not read is
// not listed, not counted and not hinted at.
func (h *Home) List(a Actor, scope string) ([]ItemView, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if err := h.Health(); err != nil {
		return nil, err
	}
	if _, err := authority.CleanPath(scope); err != nil {
		return nil, err
	}
	var paths []string
	for p := range h.cat.Paths {
		if authority.Covers(scope, p) && h.decide(a, authority.Read, p).Allowed {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	out := []ItemView{}
	for _, p := range paths {
		out = append(out, h.view(h.itemAt(p)))
	}
	return out, nil
}

// Stat describes one item.
func (h *Home) Stat(a Actor, path string) (ItemView, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, err := h.require(a, authority.Read, path); err != nil {
		return ItemView{}, err
	}
	item := h.itemAt(path)
	if item == nil {
		return ItemView{}, ErrNotFound
	}
	return h.view(item), nil
}

func (h *Home) versionOf(path string, n int) (*Item, Version, error) {
	item := h.itemAt(path)
	if item == nil {
		return nil, Version{}, ErrNotFound
	}
	if n == 0 {
		n = len(item.Versions)
	}
	if n < 1 || n > len(item.Versions) {
		return nil, Version{}, ErrNotFound
	}
	return item, item.Versions[n-1], nil
}

// Sheet reads a version's provenance: every revision, latest last.
func (h *Home) Sheet(a Actor, path string, n int) ([]sheet.Sheet, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, err := h.require(a, authority.Read, path); err != nil {
		return nil, err
	}
	_, v, err := h.versionOf(path, n)
	if err != nil {
		return nil, err
	}
	var out []sheet.Sheet
	for _, id := range v.Sheets {
		var s sheet.Sheet
		if err := h.getJSON(kindSheet, id, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (h *Home) versionFile(v Version, sub string) (*archive.Entry, error) {
	m, err := h.manifest(v)
	if err != nil {
		return nil, err
	}
	var e *archive.Entry
	if m.Single {
		// A single file is addressed by its own path and by nothing else. Were
		// its one entry also reachable under path/name, a grant written one
		// level below the item would hand out the whole of it.
		if sub == "" && len(m.Entries) > 0 {
			e = &m.Entries[0]
		}
	} else {
		for i := range m.Entries {
			if m.Entries[i].Path == sub {
				e = &m.Entries[i]
				break
			}
		}
	}
	if e == nil || e.Kind != "file" {
		return nil, ErrNotFound
	}
	return e, nil
}

func (h *Home) versionBytes(v Version, sub string) ([]byte, error) {
	e, err := h.versionFile(v, sub)
	if err != nil {
		return nil, err
	}
	r, err := h.st.Get(kindBlob, e.Blob)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if s := sha256.Sum256(b); hex.EncodeToString(s[:]) != e.SHA256 {
		return nil, errors.New("home: the stored bytes do not match their hash")
	}
	return b, nil
}

// Bytes reads a version's content: the file, or one file inside a tree. The
// decision is made on the file's own path, so a scope inside a tree is a scope.
func (h *Home) Bytes(a Actor, path string, n int, sub string, offset, length int64) ([]byte, int64, error) {
	b, total, _, err := h.BytesWithHash(a, path, n, sub, offset, length)
	return b, total, err
}

// BytesWithHash authenticates the complete object while retaining only the
// requested window. The returned hash names the file's content; a version's
// SHA256 names its archive manifest and is deliberately a different identity.
// Every read still reaches authenticated EOF, including when the requested
// range ends near the beginning. Memory use does not grow with file size.
func (h *Home) BytesWithHash(a Actor, path string, n int, sub string, offset, length int64) ([]byte, int64, string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	target := path
	if sub != "" {
		target = path + "/" + sub
	}
	if _, err := h.require(a, authority.Bytes, target); err != nil {
		return nil, 0, "", err
	}
	_, v, err := h.versionOf(path, n)
	if err != nil {
		return nil, 0, "", err
	}
	e, err := h.versionFile(v, sub)
	if err != nil {
		return nil, 0, "", err
	}
	total := e.Size
	if length <= 0 || length > maxInlineBytes {
		length = maxInlineBytes
	}
	if offset < 0 || offset > total {
		return nil, total, "", errors.New("home: offset past the end")
	}
	if length > total-offset {
		length = total - offset
	}
	r, err := h.st.Get(kindBlob, e.Blob)
	if err != nil {
		return nil, total, "", err
	}
	defer r.Close()
	digest := sha256.New()
	stream := io.TeeReader(r, digest)
	if _, err = io.CopyN(io.Discard, stream, offset); err != nil {
		return nil, total, "", err
	}
	b := make([]byte, int(length))
	if _, err = io.ReadFull(stream, b); err != nil {
		return nil, total, "", err
	}
	rest, err := io.Copy(io.Discard, stream)
	if err != nil {
		return nil, total, "", err
	}
	if offset+length+rest != total || hex.EncodeToString(digest.Sum(nil)) != e.SHA256 {
		return nil, total, "", errors.New("home: the stored bytes do not match their size or hash")
	}
	return b, total, e.SHA256, nil
}

// ---------- search ----------

func (h *Home) reindex() {
	h.idx = index.New()
	for _, item := range h.cat.Items {
		for _, v := range item.Versions {
			h.indexVersion(item, v)
		}
	}
}

func (h *Home) indexVersion(item *Item, v Version) {
	m, err := h.manifest(v)
	if err != nil {
		return
	}
	for _, e := range m.Entries {
		if e.Kind != "file" || e.Size > 4<<20 {
			continue
		}
		sub := e.Path
		docPath := item.Path + "/" + e.Path
		if m.Single {
			sub, docPath = "", item.Path
		}
		b, err := h.versionBytes(v, sub)
		if err != nil || !utf8.Valid(b) {
			continue
		}
		h.idx.Add(index.Doc{ID: fmt.Sprintf("%s@%d:%s", item.ID, v.N, sub), Item: item.ID, Path: docPath, Version: v.N, Sub: sub}, b)
	}
}

// SearchHit is one visible match, with the exact bytes around it when the
// asker may read them.
type SearchHit struct {
	Item       string       `json:"item"`
	Path       string       `json:"path"`
	Version    int          `json:"version"`
	Sub        string       `json:"sub,omitempty"`
	Spans      []index.Span `json:"spans"`
	Snippet    string       `json:"snippet,omitempty"`
	At         *index.Span  `json:"at,omitempty"`
	Authorship string       `json:"authorship,omitempty"`
}

func (h *Home) itemByID(id string) *Item { return h.cat.Items[id] }

// Search finds what the asker may search. A snippet is given only where the
// asker may also read the bytes, and authorship only where it may read the
// provenance.
func (h *Home) Search(a Actor, query string, limit int) ([]SearchHit, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if err := h.Health(); err != nil {
		return nil, err
	}
	visible := func(d index.Doc) bool { return h.decide(a, authority.Search, d.Path).Allowed }
	out := []SearchHit{}
	for _, hit := range h.idx.Search(query, visible, limit) {
		d := hit.Doc
		sh := SearchHit{Item: d.Item, Path: d.Path, Version: d.Version, Sub: d.Sub, Spans: hit.Spans}
		item := h.itemByID(d.Item)
		if item == nil || d.Version > len(item.Versions) {
			continue
		}
		v := item.Versions[d.Version-1]
		if h.decide(a, authority.Bytes, d.Path).Allowed {
			if b, err := h.versionBytes(v, d.Sub); err == nil && len(hit.Spans) > 0 {
				s, at := index.Snippet(b, hit.Spans[0], 60)
				sh.Snippet, sh.At = s, &at
			}
		}
		if h.decide(a, authority.Read, item.Path).Allowed {
			if s, _, err := h.sheetOf(v); err == nil {
				sh.Authorship = s.Authorship
			}
		}
		out = append(out, sh)
	}
	return out, nil
}

// Passage is context for a reader that answers from a home: exact bytes, with
// exactly where they are.
type Passage struct {
	Item       string     `json:"item"`
	Path       string     `json:"path"`
	Version    int        `json:"version"`
	Sub        string     `json:"sub,omitempty"`
	Event      string     `json:"event"`
	At         index.Span `json:"at"`
	Text       string     `json:"text"`
	Authorship string     `json:"authorship,omitempty"`
}

// Context gathers passages for a question, only from what the asker may both
// search and read, up to max bytes in all.
func (h *Home) Context(a Actor, query string, max int) ([]Passage, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if err := h.Health(); err != nil {
		return nil, err
	}
	visible := func(d index.Doc) bool {
		return h.decide(a, authority.Search, d.Path).Allowed && h.decide(a, authority.Bytes, d.Path).Allowed
	}
	out := []Passage{}
	used := 0
	for _, hit := range h.idx.Search(query, visible, 0) {
		d := hit.Doc
		item := h.itemByID(d.Item)
		if item == nil || len(hit.Spans) == 0 {
			continue
		}
		v := item.Versions[d.Version-1]
		b, err := h.versionBytes(v, d.Sub)
		if err != nil {
			continue
		}
		text, at := index.Passage(b, hit.Spans[0], 1024)
		if max > 0 && used+len(text) > max {
			break
		}
		used += len(text)
		p := Passage{Item: d.Item, Path: d.Path, Version: d.Version, Sub: d.Sub, Event: v.Event, At: at, Text: text}
		if h.decide(a, authority.Read, item.Path).Allowed {
			if s, _, err := h.sheetOf(v); err == nil {
				p.Authorship = s.Authorship
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// ---------- handing out ----------

// Export hands out a version under dest. A program may only hand out into its
// own handover folder.
func (h *Home) Export(a Actor, path string, n int, dest string, opts archive.ExportOptions) (archive.ExportReport, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.require(a, authority.Export, path); err != nil {
		return archive.ExportReport{State: "refused"}, err
	}
	if !a.Owner && !within(h.places[a.Consumer].Handover, dest) {
		return archive.ExportReport{State: "refused"}, &Denied{Decision: authority.Decision{Consumer: a.Consumer,
			Action: authority.Export, Path: path, Reason: "destination_outside_handover"}}
	}
	_, v, err := h.versionOf(path, n)
	if err != nil {
		return archive.ExportReport{State: "refused"}, err
	}
	m, err := h.manifest(v)
	if err != nil {
		return archive.ExportReport{State: "refused"}, err
	}
	return archive.Export(m, dest, storeIO{h.st}, opts)
}

// ExportHistory hands out every version of an item, each in its own folder
// named for its number and hash, so versions of one path never write over each
// other.
func (h *Home) ExportHistory(a Actor, path, dest string) ([]archive.ExportReport, error) {
	h.mu.Lock()
	item := h.itemAt(path)
	h.mu.Unlock()
	if item == nil {
		h.mu.Lock()
		_, err := h.require(a, authority.Export, path)
		h.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	var reports []archive.ExportReport
	for _, v := range item.Versions {
		sub := filepath.Join(dest, fmt.Sprintf("%d-%.8s", v.N, v.SHA256))
		rep, err := h.Export(a, path, v.N, sub, archive.ExportOptions{})
		reports = append(reports, rep)
		if err != nil {
			return reports, err
		}
	}
	return reports, nil
}
