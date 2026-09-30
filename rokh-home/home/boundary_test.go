package home

import (
	"os"
	"path/filepath"
	"testing"

	"rokh-home/authority"
)

// The tests here are the ones the boundary audit added: each one names a way
// a program reached past its authority, and holds both the way that is shut
// and the work that must go on working.

// fresh is the same program in a session opened at the authority it now holds.
func fresh(t *testing.T, h *Home, a Actor) Actor {
	t.Helper()
	c, ok := h.Consumer(a.Consumer)
	if !ok {
		t.Fatal("no such program")
	}
	return Actor{Consumer: c.ID, Session: c.Version}
}

// ungrant withdraws every grant a program holds for an action.
func ungrant(t *testing.T, h *Home, a Actor, action authority.Action) {
	t.Helper()
	c, _ := h.Consumer(a.Consumer)
	for _, g := range c.Grants {
		if g.Action == action {
			if err := h.Ungrant(OwnerActor, a.Consumer, g.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// A draft opened on a recorded version starts from that version's bytes and is
// read back by whoever opened it, so draft alone must not open one.
func TestADraftOfAVersionNeedsBytes(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("یادداشت.md", researchText), "research/یادداشت.md")

	drafter := f.consumer("drafter", Places{}, map[authority.Action]string{authority.Draft: "research"})
	if _, _, err := f.h.Bytes(drafter, "research/یادداشت.md", 0, "", 0, 0); err == nil {
		t.Fatal("draft implied bytes")
	}
	if _, err := f.h.DraftOpen(drafter, "research/یادداشت.md"); err == nil {
		t.Fatal("draft alone opened a draft of a recorded version, which hands over its bytes")
	} else {
		denied(t, err, authority.NoGrant)
	}

	// Positive: where there is nothing to start from, draft alone is enough,
	// and the draft it opens is its own to write and read.
	d, err := f.h.DraftOpen(drafter, "research/تازه.md")
	if err != nil {
		t.Fatalf("an empty draft: %v", err)
	}
	if d.Base != 0 || d.Size != 0 {
		t.Fatalf("an empty draft came with content: %+v", d)
	}
	if _, err := f.h.DraftWrite(drafter, d.ID, []byte("نوشتهٔ ساختگیِ تازه\n"), d.Revision); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _, err := f.h.DraftRead(drafter, d.ID)
	if err != nil || string(b) != "نوشتهٔ ساختگیِ تازه\n" {
		t.Fatalf("read back its own draft: %v %q", err, b)
	}

	// Positive: an editor that may read the bytes edits the version.
	editor := f.consumer("editor", Places{}, map[authority.Action]string{
		authority.Draft: "research", authority.Bytes: "research"})
	ed, err := f.h.DraftOpen(editor, "research/یادداشت.md")
	if err != nil {
		t.Fatalf("editor: %v", err)
	}
	if ed.Base != 1 {
		t.Fatalf("the draft is not of the version: %+v", ed)
	}
	if b, _, err := f.h.DraftRead(editor, ed.ID); err != nil || string(b) != researchText {
		t.Fatalf("the editor's draft does not hold the version: %v", err)
	}

	// Negative: take bytes away and the same program opens no more drafts of
	// that version; the one it already holds is its own knowledge, not a way on.
	ungrant(t, f.h, editor, authority.Bytes)
	after := fresh(t, f.h, editor)
	if _, err := f.h.DraftOpen(after, "research/یادداشت.md"); err == nil {
		t.Fatal("a draft of the version after bytes was withdrawn")
	} else {
		denied(t, err, authority.NoGrant)
	}
}

// A confirmed disclosure is collected under the same two rights the preview
// was made under. The owner's confirmation is a second condition, not a grant.
func TestAConfirmedDisclosureIsCollectedUnderBothRights(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("یادداشت.md", researchText), "research/یادداشت.md")
	sharer := f.consumer("sharer", Places{}, map[authority.Action]string{
		authority.Share: "research", authority.Bytes: "research"})
	other := f.consumer("other", Places{}, map[authority.Action]string{
		authority.Share: "research", authority.Bytes: "research"})

	d, err := f.h.DisclosePreview(sharer, "research/یادداشت.md", 0, 0, 12, "peer-آزمون", "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.h.ConfirmedDisclosure(sharer, d.Hash); err == nil {
		t.Fatal("an unconfirmed disclosure was collected")
	}
	if _, err := f.h.ConfirmDisclosure(sharer, d.Hash); err == nil {
		t.Fatal("a program confirmed its own disclosure")
	}
	if _, err := f.h.ConfirmDisclosure(OwnerActor, d.Hash); err != nil {
		t.Fatal(err)
	}

	// Positive: the program that prepared it, still holding both rights,
	// collects the excerpt and the body.
	got, body, err := f.h.ConfirmedDisclosure(sharer, d.Hash)
	if err != nil || string(body) != researchText || string(got.Excerpt) != researchText[:12] {
		t.Fatalf("collect: %v %q", err, got.Excerpt)
	}

	// Negative: nobody else collects it, whatever they hold.
	if _, _, err := f.h.ConfirmedDisclosure(other, d.Hash); err == nil {
		t.Fatal("another program collected a disclosure it did not prepare")
	}

	// Negative: bytes withdrawn, session renewed — the body does not leave.
	ungrant(t, f.h, sharer, authority.Bytes)
	after := fresh(t, f.h, sharer)
	if _, _, err := f.h.Bytes(after, "research/یادداشت.md", 0, "", 0, 0); err == nil {
		t.Fatal("bytes survived being withdrawn")
	}
	if _, body, err := f.h.ConfirmedDisclosure(after, d.Hash); err == nil || len(body) != 0 {
		t.Fatalf("the body left the home under share alone: %v %d bytes", err, len(body))
	} else {
		denied(t, err, authority.NoGrant)
	}

	// Negative: and a program whose standing is gone collects nothing either.
	if err := f.h.RevokeConsumer(OwnerActor, sharer.Consumer); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.h.ConfirmedDisclosure(fresh(t, f.h, sharer), d.Hash); err == nil {
		t.Fatal("a revoked program collected a confirmed disclosure")
	}
}

// A single file is addressed by its item's own path. Reaching it under
// path/name would let a grant written one level below hand out the whole item.
func TestASingleFileIsAddressedOnlyByItsOwnPath(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("گزارش.md", researchText), "docs/گزارش")

	deep := f.consumer("deep", Places{}, map[authority.Action]string{authority.Bytes: "docs/گزارش/گزارش.md"})
	if _, _, err := f.h.Bytes(deep, "docs/گزارش", 0, "", 0, 0); err == nil {
		t.Fatal("a grant below the item read the item")
	} else {
		denied(t, err, authority.NoGrant)
	}
	if b, _, err := f.h.Bytes(deep, "docs/گزارش", 0, "گزارش.md", 0, 0); err == nil {
		t.Fatalf("the sub form of a single file handed out %d bytes", len(b))
	}

	// Positive: the item's own holder still reads it whole, and in parts.
	reader := f.consumer("reader", Places{}, map[authority.Action]string{authority.Bytes: "docs/گزارش"})
	b, total, err := f.h.Bytes(reader, "docs/گزارش", 0, "", 0, 0)
	if err != nil || string(b) != researchText || total != int64(len(researchText)) {
		t.Fatalf("whole: %v", err)
	}
	if part, _, err := f.h.Bytes(reader, "docs/گزارش", 0, "", 3, 5); err != nil || string(part) != researchText[3:8] {
		t.Fatalf("part: %v %q", err, part)
	}

	// Positive: a tree's files are still addressed by their own sub-paths, and
	// a scope inside the tree is still a scope.
	dir := t.TempDir()
	writeAt(t, dir, "باز.md", openText)
	writeAt(t, dir, "بسته.md", shutText)
	f.ownerImport(dir, "project")
	inner := f.consumer("inner", Places{}, map[authority.Action]string{authority.Bytes: "project/باز.md"})
	if b, _, err := f.h.Bytes(inner, "project", 0, "باز.md", 0, 0); err != nil || string(b) != openText {
		t.Fatalf("a file inside a tree: %v %q", err, b)
	}
	if _, _, err := f.h.Bytes(inner, "project", 0, "بسته.md", 0, 0); err == nil {
		t.Fatal("a scope inside a tree covered its sibling")
	} else {
		denied(t, err, authority.NoGrant)
	}
}

const (
	openText = "پروندهٔ ساختگیِ باز.\n"
	shutText = "پروندهٔ ساختگیِ بسته.\n"
)

// writeAt writes a synthetic file into a folder made for this test.
func writeAt(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// An import's journal is answered for only while its program still holds the
// authority it was previewed under.
func TestAnImportsStatusIsDecidedAgain(t *testing.T) {
	f := newFixture(t)
	intake := t.TempDir()
	w := f.consumer("importer", Places{Intake: intake}, map[authority.Action]string{authority.Record: "in"})
	src := writeAt(t, intake, "ورودی.md", researchText)

	_, id, err := f.h.ImportPreview(w, src, "in/ورودی.md")
	if err != nil {
		t.Fatal(err)
	}
	// Positive: while it stands, it reads where its import got to.
	st, err := f.h.ImportStatus(w, id)
	if err != nil || st["state"] != "previewed" || st["path"] != "in/ورودی.md" {
		t.Fatalf("status: %v %v", err, st)
	}

	// Negative: another program never sees it at all.
	stranger := f.consumer("stranger", Places{Intake: intake}, map[authority.Action]string{authority.Record: "in"})
	if _, err := f.h.ImportStatus(stranger, id); err == nil {
		t.Fatal("another program read an import's status")
	}

	// Negative: the grant withdrawn, a renewed session gets nothing.
	ungrant(t, f.h, w, authority.Record)
	if _, err := f.h.ImportStatus(fresh(t, f.h, w), id); err == nil {
		t.Fatal("an import's status after its grant was withdrawn")
	} else {
		denied(t, err, authority.NoGrant)
	}

	// Negative: and revocation closes it too.
	if _, err := f.h.Grant(OwnerActor, w.Consumer, authority.Record, "in"); err != nil {
		t.Fatal(err)
	}
	back := fresh(t, f.h, w)
	if _, err := f.h.ImportStatus(back, id); err != nil {
		t.Fatalf("the grant given back: %v", err)
	}
	if err := f.h.RevokeConsumer(OwnerActor, w.Consumer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.ImportStatus(fresh(t, f.h, w), id); err == nil {
		t.Fatal("a revoked program read an import's status")
	} else {
		denied(t, err, authority.ConsumerRevoked)
	}
}
