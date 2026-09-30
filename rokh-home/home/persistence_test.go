//go:build legacy09

// This test pins the 0.9 home store layout (pointers/ as files) that
// rokh-home/2 replaced with records in the ledger vessel.

package home

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"rokh-home/authority"
	"rokh-home/store"
)

func denyPointerWrites(t *testing.T, h *Home) func() {
	t.Helper()
	p := filepath.Join(h.Root(), store.PointersDir)
	if err := os.Chmod(p, 0o500); err != nil {
		t.Fatal(err)
	}
	restore := func() {
		if err := os.Chmod(p, 0o700); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(restore)
	return restore
}

// A failed grant cannot silently become usable by reauthenticating the same
// credential. The authority visible in memory must not run ahead of its store.
func TestFailedGrantDoesNotAuthorizeAConsumer(t *testing.T) {
	f := newFixture(t)
	f.ownerImport(f.file("private.txt", privateText), "private/note")
	cv, credential, err := f.h.AddConsumer(OwnerActor, "reader", "test", Places{})
	if err != nil {
		t.Fatal(err)
	}
	restore := denyPointerWrites(t, f.h)
	_, err = f.h.Grant(OwnerActor, cv.ID, authority.Read, "private")
	restore()
	if err == nil {
		t.Fatal("fault did not prevent the authority pointer write")
	}
	c, ok := f.h.Authenticate(credential)
	if !ok {
		t.Fatal("existing credential was lost")
	}
	_, err = f.h.Stat(Actor{Consumer: c.ID, Session: c.Version}, "private/note")
	if err == nil {
		t.Fatal("a failed grant became usable after reauthentication")
	}
}

// A failed draft revision preserves the last acknowledged bytes and revision
// for both the still-running broker and the next opener.
func TestFailedDraftSavePreservesAcknowledgedBytes(t *testing.T) {
	f := newFixture(t)
	d, err := f.h.DraftOpen(OwnerActor, "writing/note")
	if err != nil {
		t.Fatal(err)
	}
	d, err = f.h.DraftWrite(OwnerActor, d.ID, []byte(researchText), d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restore := denyPointerWrites(t, f.h)
	_, err = f.h.DraftWrite(OwnerActor, d.ID, []byte("unacknowledged replacement"), d.Revision)
	restore()
	if err == nil {
		t.Fatal("fault did not prevent the draft pointer write")
	}
	got, view, err := f.h.DraftRead(OwnerActor, d.ID)
	if err != nil || !bytes.Equal(got, []byte(researchText)) || view.Revision != d.Revision {
		t.Fatalf("failed write changed the live draft: bytes=%q view=%+v err=%v", got, view, err)
	}
	f.h.Close()
	h, err := Open(f.root, pass, "persistence-test")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	got, view, err = h.DraftRead(OwnerActor, d.ID)
	if err != nil || !bytes.Equal(got, []byte(researchText)) || view.Revision != d.Revision {
		t.Fatal("reopened draft differs")
	}
}

// A ledger must never acknowledge authority for a private key that the home
// has not stored. Otherwise a crash strands the grant and conflicts with retry.
func TestFailedSigningGrantCanBeRetriedAfterRestart(t *testing.T) {
	f := newFixture(t)
	cv, _, err := f.h.AddConsumer(OwnerActor, "writer", "test", Places{})
	if err != nil {
		t.Fatal(err)
	}
	restore := denyPointerWrites(t, f.h)
	_, err = f.h.Grant(OwnerActor, cv.ID, authority.Record, "writing")
	restore()
	if err == nil {
		t.Fatal("fault did not prevent storing the signing grant")
	}
	f.h.Close()
	h, err := Open(f.root, pass, "persistence-test")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err := h.Grant(OwnerActor, cv.ID, authority.Record, "writing"); err != nil {
		t.Fatalf("retry after restart stranded the signing authority: %v", err)
	}
	c, _ := h.Consumer(cv.ID)
	a := Actor{Consumer: c.ID, Session: c.Version}
	if _, err := h.Grant(OwnerActor, cv.ID, authority.Draft, "writing"); err != nil {
		t.Fatal(err)
	}
	c, _ = h.Consumer(cv.ID)
	a.Session = c.Version
	d, err := h.DraftOpen(a, "writing/new")
	if err != nil {
		t.Fatal(err)
	}
	d, err = h.DraftWrite(a, d.ID, []byte("synthetic recovered writer"), d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.DraftRecord(a, d.ID)
	if err != nil || r.Record != "recorded" {
		t.Fatalf("recovered grant cannot sign: %+v %v", r, err)
	}
}
