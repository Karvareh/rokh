package home

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"rokh-home/authority"
	"rokh-home/sheet"
)

func TestAReadingMirrorPreservesTextAndProvenanceWithoutSigningSecrets(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("گزاره.md", researchText), "writing/گزاره")
	_, _, err := f.h.DeclareAuthor(OwnerActor, "writing/گزاره", DeclareInput{Version: 1, About: "whole", Author: "synthetic author", Statement: "the test owner declares authorship", Authorship: sheet.OwnerDeclared})
	if err != nil {
		t.Fatal(err)
	}
	f.consumer("writer", Places{}, map[authority.Action]string{authority.Record: "writing"})
	// An unrelated app object and an unrecorded draft must not carry a
	// second native identity or uncommitted work into a reading replica.
	if _, err := f.h.st.Put("app-state", "synthetic-identity", bytes.NewBufferString("synthetic native private identity"), nil); err != nil {
		t.Fatal(err)
	}
	d, err := f.h.DraftOpen(OwnerActor, "writing/unrecorded")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.DraftWrite(OwnerActor, d.ID, []byte("not committed"), d.Revision); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "mirror")
	const mirrorPass = "synthetic distinct reading password"
	info, err := f.h.Mirror(OwnerActor, dest, mirrorPass, 1000)
	if err != nil || info.Kind != MirrorReplica {
		t.Fatalf("mirror: %+v %v", info, err)
	}
	m, err := Open(dest, mirrorPass, "mirror-test")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, _, err := m.Bytes(OwnerActor, "writing/گزاره", 1, "", 0, -1)
	if err != nil || string(b) != researchText {
		t.Fatalf("mirror bytes: %q %v", b, err)
	}
	item, err := m.Stat(OwnerActor, "writing/گزاره")
	if err != nil || len(item.Versions) != 1 {
		t.Fatalf("mirror provenance: %+v %v", item, err)
	}
	if m.signs || len(m.keys.Keys) != 0 || len(m.cat.Drafts) != 0 {
		t.Fatal("mirror carried writing authority or uncommitted work")
	}
	if exists, err := m.st.Has("app-state", "synthetic-identity"); err != nil || exists {
		t.Fatal("mirror carried another native writer")
	}
	if _, err := m.DraftOpen(OwnerActor, "writing/گزاره"); err == nil {
		t.Fatal("mirror owner could edit")
	}
	for _, op := range []string{"write", "append", "intent", "outcome", "bind"} {
		r := m.OwnerLedger(map[string]any{"op": op})
		if r["ok"] != false || r["reason"] != "read_only_replica" {
			t.Fatalf("mirror allowed %s: %+v", op, r)
		}
	}
	cv, _, err := m.AddConsumer(OwnerActor, "reader", "test", Places{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Grant(OwnerActor, cv.ID, authority.Bytes, "writing"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Grant(OwnerActor, cv.ID, authority.Record, "writing"); err == nil {
		t.Fatal("mirror could mint writing authority")
	}
	if _, err := f.h.Mirror(OwnerActor, dest, mirrorPass, 1000); err == nil {
		t.Fatal("an existing mirror was overwritten")
	}
	if f.h.Replica().Kind != WriterReplica {
		t.Fatal("source writer was changed")
	}
	if _, err := f.h.DraftOpen(OwnerActor, "writing/still-writable"); err != nil {
		t.Fatal(err)
	}
}

func TestAnIncompleteReplicaDoesNotOpen(t *testing.T) {
	f := newFixture(t)
	f.h.Close()
	if err := os.WriteFile(filepath.Join(f.root, "INCOMPLETE"), []byte("synthetic interrupted replica"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, err := Open(f.root, pass, "test"); err == nil {
		h.Close()
		t.Fatal("an interrupted replica opened as complete")
	}
}
