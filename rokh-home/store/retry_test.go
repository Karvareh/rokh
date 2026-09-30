//go:build legacy09

// This test pins the 0.9 home store (home.json, objects/, pointers/) that
// rokh-home/2 replaced with records in the ledger vessel.

package store

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
)

func TestAnExistingObjectIsVerifiedBeforeRetrySucceeds(t *testing.T) {
	h, _ := newHome(t)
	body := []byte("synthetic exact bytes")
	want := sha256.Sum256(body)
	first, err := h.Put("blob", "stable-attempt", bytes.NewReader(body), want[:])
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.Put("blob", "stable-attempt", bytes.NewReader(body), want[:])
	if err != nil || again != first {
		t.Fatalf("retry returned false metadata: first=%+v again=%+v err=%v", first, again, err)
	}
	_, err = h.Put("blob", "stable-attempt", bytes.NewReader([]byte("different bytes")), nil)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("same name accepted different bytes: %v", err)
	}
	p := objectPath(h, "blob", "stable-attempt")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Put("blob", "stable-attempt", bytes.NewReader(body), want[:]); err == nil {
		t.Fatal("corrupt existing object was reported as a successful stored retry")
	}
}
