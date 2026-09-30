package home

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rokh-home/archive"
	"rokh-home/authority"
	"rokh-home/sheet"
)

const pass = "synthetic home passphrase"

var (
	researchText = "# یادداشت ساختگی پژوهش\nاین متنِ ساختگی دربارهٔ کتاب‌خانهٔ آزمون است.\nسطر دوم با «ي» و «ك» عربی: كتابخانه.\n"
	privateText  = "دفترچهٔ ساختگیِ خصوصی: کتابخانهٔ پنهان را هیچ برنامه‌ای نبیند.\n"
)

type fixture struct {
	t    *testing.T
	root string
	h    *Home
	src  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, pass, "synthetic genesis of a test home", 1000); err != nil {
		t.Fatal(err)
	}
	h, err := Open(root, pass, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return &fixture{t: t, root: root, h: h, src: t.TempDir()}
}

func (f *fixture) file(name, body string) string {
	p := filepath.Join(f.src, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) ownerImport(source, path string) RecordResult {
	f.t.Helper()
	_, id, err := f.h.ImportPreview(OwnerActor, source, path)
	if err != nil {
		f.t.Fatal(err)
	}
	res, err := f.h.ImportRun(OwnerActor, id)
	if err != nil || res.Record != "recorded" {
		f.t.Fatalf("owner import: %v %+v", err, res)
	}
	return res
}

func (f *fixture) consumer(name string, places Places, grants map[authority.Action]string) Actor {
	f.t.Helper()
	cv, cred, err := f.h.AddConsumer(OwnerActor, name, "test", places)
	if err != nil {
		f.t.Fatal(err)
	}
	for action, scope := range grants {
		if _, err := f.h.Grant(OwnerActor, cv.ID, action, scope); err != nil {
			f.t.Fatal(err)
		}
	}
	c, ok := f.h.Authenticate(cred)
	if !ok || c.ID != cv.ID {
		f.t.Fatal("credential does not authenticate")
	}
	return Actor{Consumer: c.ID, Session: c.Version}
}

func denied(t *testing.T, err error, reason string) {
	t.Helper()
	var d *Denied
	if !errors.As(err, &d) {
		t.Fatalf("expected a refusal (%s), got %v", reason, err)
	}
	if reason != "" && d.Decision.Reason != reason {
		t.Fatalf("refused for %s, want %s", d.Decision.Reason, reason)
	}
}

func (f *fixture) accepted() int {
	st := f.h.OwnerLedger(map[string]any{"op": "status"})
	return int(st["accepted"].(int))
}

func TestTheAuthorityMatrix(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("یادداشت.md", researchText), "research/یادداشت.md")
	f.ownerImport(f.file("دفترچه.md", privateText), "private/دفترچه.md")
	handover := t.TempDir()
	intake := t.TempDir()

	reader := f.consumer("reader", Places{}, map[authority.Action]string{authority.Read: "research"})
	searcher := f.consumer("searcher", Places{}, map[authority.Action]string{authority.Search: "research"})
	fetcher := f.consumer("fetcher", Places{}, map[authority.Action]string{authority.Bytes: "research"})
	writer := f.consumer("writer", Places{Intake: intake}, map[authority.Action]string{authority.Draft: "research", authority.Record: "research"})
	exporter := f.consumer("exporter", Places{Handover: handover}, map[authority.Action]string{authority.Export: "research"})

	// read
	if items, err := f.h.List(reader, authority.WholeHome); err != nil || len(items) != 1 || items[0].Path != "research/یادداشت.md" {
		t.Fatalf("reader lists: %v %+v", err, items)
	}
	if _, err := f.h.Stat(reader, "private/دفترچه.md"); err == nil {
		t.Fatal("reader read a private item")
	} else {
		denied(t, err, authority.NoGrant)
	}
	if _, err := f.h.Stat(reader, "private/nothing-here.md"); err == nil {
		t.Fatal("reader learned something about a path outside its scope")
	} else {
		denied(t, err, authority.NoGrant)
	}
	if _, _, err := f.h.Bytes(reader, "research/یادداشت.md", 0, "", 0, 0); err == nil {
		t.Fatal("read implied bytes")
	}
	if items, _ := f.h.List(fetcher, authority.WholeHome); len(items) != 0 {
		t.Fatal("bytes implied read")
	}

	// search: only in scope, and a snippet only with bytes
	hits, err := f.h.Search(searcher, "کتابخانه", 10)
	if err != nil || len(hits) != 1 || hits[0].Path != "research/یادداشت.md" {
		t.Fatalf("searcher: %v %+v", err, hits)
	}
	if hits[0].Snippet != "" || hits[0].Authorship != "" {
		t.Fatal("search without bytes or read gave a snippet or authorship")
	}
	if hits, _ := f.h.Search(reader, "کتابخانه", 10); len(hits) != 0 {
		t.Fatal("read implied search")
	}
	if ctx, _ := f.h.Context(searcher, "کتابخانه", 4096); len(ctx) != 0 {
		t.Fatal("context was built without bytes")
	}

	// bytes
	b, total, err := f.h.Bytes(fetcher, "research/یادداشت.md", 0, "", 0, 0)
	if err != nil || string(b) != researchText || total != int64(len(researchText)) {
		t.Fatalf("fetcher: %v", err)
	}
	if _, _, err := f.h.Bytes(fetcher, "private/دفترچه.md", 0, "", 0, 0); err == nil {
		t.Fatal("fetcher read private bytes")
	}

	// draft and record: drafts record nothing; recording is one event by the
	// program's own key
	before := f.accepted()
	d, err := f.h.DraftOpen(writer, "research/پیش‌نویس.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.DraftWrite(writer, d.ID, []byte("پیش‌نویسِ ساختگی\n"), d.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.DraftWrite(writer, d.ID, []byte("stale"), d.Revision); err == nil {
		t.Fatal("a stale draft revision was written")
	}
	if f.accepted() != before {
		t.Fatal("opening or writing a draft recorded an event")
	}
	if _, err := f.h.DraftOpen(writer, "private/x.md"); err == nil {
		t.Fatal("a draft outside scope")
	}
	res, err := f.h.DraftRecord(writer, d.ID)
	if err != nil || res.Record != "recorded" {
		t.Fatalf("record: %v %+v", err, res)
	}
	if f.accepted() != before+1 {
		t.Fatalf("recording made %d events", f.accepted()-before)
	}
	get := f.h.OwnerLedger(map[string]any{"op": "get", "id": res.Event})
	raw, _ := hex.DecodeString(get["raw"].(string))
	cv, _ := f.h.Consumer(writer.Consumer)
	if !bytes.Contains(raw, mustHex(t, cv.KeyPublic)) || bytes.Contains(raw, f.h.rootPub) {
		t.Fatal("the recording was not signed by the program's own key")
	}
	if _, err := f.h.DraftRecord(fetcher, d.ID); err == nil {
		t.Fatal("another program recorded someone's draft")
	}

	// export only into the handover folder
	if _, err := f.h.Export(exporter, "research/یادداشت.md", 0, filepath.Join(t.TempDir(), "elsewhere"), archive.ExportOptions{}); err == nil {
		t.Fatal("export outside the handover folder")
	} else {
		denied(t, err, "destination_outside_handover")
	}
	rep, err := f.h.Export(exporter, "research/یادداشت.md", 0, filepath.Join(handover, "out"), archive.ExportOptions{})
	if err != nil || rep.State != "complete" {
		t.Fatalf("export: %v %+v", err, rep)
	}
	if got, _ := os.ReadFile(filepath.Join(handover, "out", "یادداشت.md")); string(got) != researchText {
		t.Fatal("exported bytes differ")
	}
	if _, err := f.h.Export(exporter, "private/دفترچه.md", 0, filepath.Join(handover, "p"), archive.ExportOptions{}); err == nil {
		t.Fatal("export outside scope")
	}
	if _, _, err := f.h.ImportPreview(writer, f.file("بیرون.md", "x"), "research/بیرون.md"); err == nil {
		t.Fatal("import from outside the intake folder")
	} else {
		denied(t, err, "source_outside_intake")
	}

	// same-named scope, stale session, revocation
	near := f.consumer("near", Places{}, map[authority.Action]string{authority.Read: "researc"})
	if _, err := f.h.Stat(near, "research/یادداشت.md"); err == nil {
		t.Fatal("a scope that is a prefix of a name covered it")
	}
	if _, err := f.h.Grant(OwnerActor, reader.Consumer, authority.Search, "research"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.Stat(reader, "research/یادداشت.md"); err == nil {
		t.Fatal("a stale session was honoured")
	} else {
		denied(t, err, authority.StaleSession)
	}
	if err := f.h.RevokeConsumer(OwnerActor, writer.Consumer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.DraftOpen(Actor{Consumer: writer.Consumer, Session: writer.Session + 1}, "research/z.md"); err == nil {
		t.Fatal("a revoked program opened a draft")
	}
	if st := f.h.OwnerLedger(map[string]any{"op": "get", "id": res.Event}); st["state"] != "accepted" {
		t.Fatal("revoking the program took back what it had recorded")
	}

	// the owner-only door
	if _, _, err := f.h.AddConsumer(reader, "sneaky", "x", Places{}); err == nil {
		t.Fatal("a program registered a program")
	}
	if _, err := f.h.Grant(reader, reader.Consumer, authority.Bytes, authority.WholeHome); err == nil {
		t.Fatal("a program granted itself")
	}
}

func mustHex(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAuthorAndRegistrarStayApart(t *testing.T) {
	f := newFixture(t)
	intake := t.TempDir()
	importer := f.consumer("importer", Places{Intake: intake}, map[authority.Action]string{authority.Record: "writing", authority.Read: "writing"})
	src := filepath.Join(intake, "گفتگوی ساختگی.md")
	mixed := "بند یک: نوشتهٔ صاحب.\n" + "بند دو: پاسخ ماشین.\n" + "بند سه: نامعلوم.\n"
	os.WriteFile(src, []byte(mixed), 0o644)
	_, id, err := f.h.ImportPreview(importer, src, "writing/گفتگو.md")
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.h.ImportRun(importer, id)
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := f.h.Sheet(importer, "writing/گفتگو.md", 0)
	if err != nil || len(sheets) != 1 {
		t.Fatal(err)
	}
	s := sheets[0]
	if s.Registrar.Kind != "consumer" || s.Registrar.ID != importer.Consumer || s.Authorship != sheet.Unknown {
		t.Fatalf("an import's sheet: %+v", s)
	}
	if _, _, err := f.h.DeclareAuthor(importer, "writing/گفتگو.md", DeclareInput{Version: 1, About: "whole", Author: "me", Statement: "mine", Authorship: sheet.OwnerDeclared}); err == nil {
		t.Fatal("a program declared authorship")
	}
	one := int64(len("بند یک: نوشتهٔ صاحب.\n"))
	two := one + int64(len("بند دو: پاسخ ماشین.\n"))
	sheetID, evt, err := f.h.DeclareAuthor(OwnerActor, "writing/گفتگو.md", DeclareInput{Version: 1, About: "segments",
		Author: "صاحب‌آزمایشیِ الف", Statement: "بند نخست از من است؛ بند دوم نقل ماشین است؛ بند سوم معلوم نیست.",
		Authorship: sheet.Mixed, Segments: []sheet.Segment{
			{Start: 0, End: one, Status: sheet.OwnerDeclared, Speaker: "user", Author: "صاحب‌آزمایشیِ الف"},
			{Start: one, End: two, Status: sheet.Quoted, Speaker: "machine"},
			{Start: two, End: int64(len(mixed)), Status: sheet.Unknown},
		}})
	if err != nil {
		t.Fatal(err)
	}
	sheets, _ = f.h.Sheet(importer, "writing/گفتگو.md", 0)
	if len(sheets) != 2 || sheets[1].Authorship != sheet.Mixed || sheets[1].Supersedes == "" || sheets[1].Registrar.ID != importer.Consumer {
		t.Fatalf("after the declaration: %+v", sheets)
	}
	if sheets[0].Authorship != sheet.Unknown {
		t.Fatal("the first revision was rewritten")
	}
	if st := f.h.OwnerLedger(map[string]any{"op": "get", "id": res.Event}); st["state"] != "accepted" || sheetID == "" || evt == "" {
		t.Fatal("the import event or the declaration is missing")
	}
}

func TestAnImportStopsResumesAndNoticesAChangedSource(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(t.TempDir(), "پروژه")
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	for i := 0; i < 20; i++ {
		os.WriteFile(filepath.Join(dir, "src", fmt.Sprintf("f%02d.txt", i)), bytes.Repeat([]byte{byte(i)}, 300000), 0o644)
	}
	_, id, err := f.h.ImportPreview(OwnerActor, dir, "code/پروژه")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.h.ImportRun(OwnerActor, id)
		done <- err
	}()
	for {
		st, _ := f.h.ImportStatus(OwnerActor, id)
		if st["state"] == "running" {
			f.h.ImportStop(OwnerActor, id)
			break
		}
	}
	if err := <-done; !errors.Is(err, archive.ErrStopped) && err != nil {
		t.Fatalf("stopping: %v", err)
	}
	st, _ := f.h.ImportStatus(OwnerActor, id)
	if st["state"] != "stopped" && st["state"] != "complete" {
		t.Fatalf("after stop: %v", st)
	}
	res, err := f.h.ImportRun(OwnerActor, id)
	if err != nil || res.Record != "recorded" {
		t.Fatalf("resume: %v %+v", err, res)
	}
	os.WriteFile(filepath.Join(dir, "src", "f00.txt"), []byte("changed"), 0o644)
	_, id2, err := f.h.ImportPreview(OwnerActor, dir, "code/پروژه")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "src", "f01.txt"), []byte("changed after the preview"), 0o644)
	before := f.accepted()
	if _, err := f.h.ImportRun(OwnerActor, id2); !errors.Is(err, archive.ErrChanged) {
		t.Fatalf("a changed source: %v", err)
	}
	if f.accepted() != before {
		t.Fatal("a changed source was recorded")
	}
}

func TestNothingOnDiskReadsAsTheHome(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("یادداشت.md", researchText), "research/یادداشت.md")
	cv, cred, err := f.h.AddConsumer(OwnerActor, "synthetic-program-name", "ai", Places{})
	if err != nil {
		t.Fatal(err)
	}
	f.h.Grant(OwnerActor, cv.ID, authority.Record, "research")
	d, _ := f.h.DraftOpen(OwnerActor, "research/پیش‌نویسِ-پنهان.md")
	f.h.DraftWrite(OwnerActor, d.ID, []byte("متن پیش‌نویسِ پنهانِ ساختگی"), d.Revision)
	needles := []string{"کتاب‌خانهٔ آزمون", "یادداشت ساختگی", "research/یادداشت.md", "پیش‌نویسِ-پنهان",
		"متن پیش‌نویسِ پنهانِ ساختگی", "synthetic-program-name", hex.EncodeToString(cred), pass}
	files := 0
	filepath.Walk(f.root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range needles {
			if strings.Contains(p, n) {
				t.Errorf("a path reads as the home: %s", p)
			}
		}
		if fi.IsDir() {
			return nil
		}
		files++
		b, _ := os.ReadFile(p)
		for _, n := range needles {
			if bytes.Contains(b, []byte(n)) {
				t.Errorf("%s holds %q in the clear", p, n)
			}
		}
		return nil
	})
	if files < 10 {
		t.Fatalf("only %d files searched", files)
	}
}

func TestAReopenedHomeIsTheSameHome(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("یادداشت.md", researchText), "research/یادداشت.md")
	cv, cred, _ := f.h.AddConsumer(OwnerActor, "keeper", "cli", Places{})
	f.h.Grant(OwnerActor, cv.ID, authority.Search, "research")
	f.h.Close()
	h, err := Open(f.root, pass, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	c, ok := h.Authenticate(cred)
	if !ok {
		t.Fatal("a credential did not survive reopening")
	}
	hits, err := h.Search(Actor{Consumer: c.ID, Session: c.Version}, "كتابخانه", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("search after reopening: %v %+v", err, hits)
	}
	if _, err := Open(f.root, "wrong passphrase", "test"); err == nil {
		t.Fatal("a wrong passphrase opened the home")
	}
}
