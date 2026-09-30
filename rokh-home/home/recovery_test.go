package home

import (
	"bytes"
	"testing"

	"rokh-home/sheet"
)

// A crash after the ledger commit but before the catalog replacement must not
// hide the committed version, and replaying the import must not create another.
func TestCommittedImportSurvivesAnOlderCatalogAndJob(t *testing.T) {
	f := newFixture(t)
	source := f.file("نوشته.txt", researchText)
	_, job, err := f.h.ImportPreview(OwnerActor, source, "writing/note")
	if err != nil {
		t.Fatal(err)
	}
	oldCatalog, _, err := f.h.st.Pointer(ptrCatalog)
	if err != nil {
		t.Fatal(err)
	}
	oldJob, _, err := f.h.st.Pointer(ptrJournal + job)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.h.ImportRun(OwnerActor, job)
	if err != nil || first.Record != "recorded" {
		t.Fatalf("record: %+v %v", first, err)
	}
	if err := f.h.st.SetPointer(ptrCatalog, oldCatalog); err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.SetPointer(ptrJournal+job, oldJob); err != nil {
		t.Fatal(err)
	}
	f.h.Close()
	h, err := Open(f.root, pass, "recovery-test")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	iv, err := h.Stat(OwnerActor, "writing/note")
	if err != nil || len(iv.Versions) != 1 || iv.Versions[0].Event != first.Event {
		t.Fatalf("committed version disappeared after reopen: %+v %v", iv, err)
	}
	body, _, err := h.Bytes(OwnerActor, "writing/note", 1, "", 0, 1<<20)
	if err != nil || !bytes.Equal(body, []byte(researchText)) {
		t.Fatalf("bytes: %q %v", body, err)
	}
	again, err := h.ImportRun(OwnerActor, job)
	if err != nil || again.Event != first.Event || again.Version != 1 {
		t.Fatalf("retry duplicated the committed import: first=%+v again=%+v err=%v", first, again, err)
	}
}

// The signed authorship decision must contain enough information to recover
// exact segment attribution after its materialized catalog has been lost.
func TestAuthorshipDecisionSurvivesAnOlderCatalog(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("نوشته.txt", researchText), "writing/note")
	old, _, err := f.h.st.Pointer(ptrCatalog)
	if err != nil {
		t.Fatal(err)
	}
	in := DeclareInput{Version: 1, About: "whole", Author: "synthetic owner",
		Statement: "I wrote this synthetic note", Authorship: sheet.OwnerDeclared}
	wantSheet, wantEvent, err := f.h.DeclareAuthor(OwnerActor, "writing/note", in)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.SetPointer(ptrCatalog, old); err != nil {
		t.Fatal(err)
	}
	f.h.Close()
	h, err := Open(f.root, pass, "recovery-test")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ss, err := h.Sheet(OwnerActor, "writing/note", 1)
	if err != nil || len(ss) != 2 {
		t.Fatalf("authorship lost: %+v %v", ss, err)
	}
	last := ss[len(ss)-1]
	if last.Authorship != sheet.OwnerDeclared || len(last.Declarations) != 1 || last.Declarations[0].Event != wantEvent {
		t.Fatalf("wrong recovered declaration: %+v (sheet %s)", last, wantSheet)
	}
}

// Two brokers must not derive competing item versions from separate in-memory
// catalogs while both claim to be the opened writer of the same home.
func TestAHomeHasOneOpenBroker(t *testing.T) {
	f := newFixture(t)
	other, err := Open(f.root, pass, "other-writer")
	if err == nil {
		other.Close()
		t.Fatal("a second broker opened the same home")
	}
}

func TestInvalidAuthorshipNeverCommitsAnUnusableDecision(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("note.txt", researchText), "writing/note")
	before := f.accepted()
	_, evt, err := f.h.DeclareAuthor(OwnerActor, "writing/note", DeclareInput{
		Version: 1, About: "whole", Author: "synthetic owner", Authorship: "not-an-authorship-state"})
	if err == nil || evt != "" || f.accepted() != before {
		t.Fatalf("invalid attribution reached the ledger: event=%q err=%v before=%d after=%d", evt, err, before, f.accepted())
	}
}

func TestMixedAttributionIsRecoveredWithoutChangingText(t *testing.T) {
	f := newFixture(t)
	body := "این بند را صاحبِ آزمایشی نوشته است.\nاین بند نقلِ ماشینِ آزمایشی است."
	f.ownerImport(f.file("mixed.txt", body), "writing/mixed")
	old, _, err := f.h.st.Pointer(ptrCatalog)
	if err != nil {
		t.Fatal(err)
	}
	split := int64(bytes.IndexByte([]byte(body), '\n') + 1)
	in := DeclareInput{Version: 1, About: "segments", Author: "synthetic owner", Statement: "Only the first segment is mine", Authorship: sheet.Mixed,
		Segments: []sheet.Segment{
			{Start: 0, End: split, Status: sheet.OwnerDeclared, Author: "synthetic owner", Speaker: "user"},
			{Start: split, End: int64(len(body)), Status: sheet.Quoted, Author: "synthetic machine", Speaker: "machine"},
		}}
	_, evt, err := f.h.DeclareAuthor(OwnerActor, "writing/mixed", in)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.SetPointer(ptrCatalog, old); err != nil {
		t.Fatal(err)
	}
	f.h.Close()
	h, err := Open(f.root, pass, "recovery-test")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ss, err := h.Sheet(OwnerActor, "writing/mixed", 1)
	if err != nil || len(ss) != 2 {
		t.Fatalf("sheets: %+v %v", ss, err)
	}
	last := ss[len(ss)-1]
	if len(last.Segments) != 2 || last.Segments[0].Evidence != evt || last.Segments[0].Author != "synthetic owner" || last.Segments[1] != in.Segments[1] {
		t.Fatalf("segment attribution changed: %+v", last.Segments)
	}
	got, _, err := h.Bytes(OwnerActor, "writing/mixed", 1, "", 0, 1<<20)
	if err != nil || !bytes.Equal(got, []byte(body)) {
		t.Fatalf("text changed: %q %v", got, err)
	}
}
