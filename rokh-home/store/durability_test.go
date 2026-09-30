//go:build legacy09

// This test pins the 0.9 home store (home.json, objects/, pointers/) that
// rokh-home/2 replaced with records in the ledger vessel.

package store

import (
	"bytes"
	"errors"
	"os"
	"syscall"
	"testing"
)

// Inject failure at the actual directory-sync boundary, after publication.
// The opening must acknowledge uncertainty and refuse further reads/writes;
// reopening then reconciles the bytes that the filesystem actually retained.
func TestDirectorySyncFailureRequiresReopening(t *testing.T) {
	for _, object := range []bool{false, true} {
		t.Run(map[bool]string{false: "pointer", true: "object"}[object], func(t *testing.T) {
			h, root := newHome(t)
			if err := h.SetPointer("prior", []byte("prior value")); err != nil {
				t.Fatal(err)
			}
			calls := 0
			h.directorySync = func(string) error { calls++; return syscall.EIO }
			var err error
			if object {
				_, err = h.Put("synthetic", "new", bytes.NewBufferString("new object"), nil)
			} else {
				err = h.SetPointer("new", []byte("new pointer"))
			}
			if calls != 1 || !errors.Is(err, ErrDurability) || !errors.Is(err, syscall.EIO) {
				t.Fatalf("publication reported %v; syncs=%d", err, calls)
			}
			if _, _, err := h.Pointer("prior"); !errors.Is(err, ErrDurability) {
				t.Fatalf("uncertain store still served old projection: %v", err)
			}
			if err := h.SetPointer("later", []byte("must not publish")); !errors.Is(err, ErrDurability) {
				t.Fatalf("uncertain store kept writing: %v", err)
			}
			h.Close()
			reopened, err := Open(root, "synthetic passphrase")
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if object {
				if _, err := reopened.Put("synthetic", "new", bytes.NewBufferString("new object"), nil); err != nil {
					t.Fatalf("reconcile existing object: %v", err)
				}
			} else if value, exists, err := reopened.Pointer("new"); err != nil || !exists || string(value) != "new pointer" {
				t.Fatalf("reconcile published pointer: %q %v %v", value, exists, err)
			}
			if _, exists, err := reopened.Pointer("later"); exists || err != nil {
				t.Fatal("a write after uncertainty reached disk")
			}
		})
	}
}

func TestFailedSecretReplacementPreservesAcknowledgedState(t *testing.T) {
	h, root := newHome(t)
	if err := h.SetSecrets("synthetic passphrase", map[string][]byte{"synthetic": []byte("prior")}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })
	err := h.SetSecrets("synthetic passphrase", map[string][]byte{"synthetic": []byte("unacknowledged")})
	if err == nil {
		t.Fatal("fault did not prevent descriptor replacement")
	}
	if value, exists := h.Secret("synthetic"); !exists || string(value) != "prior" {
		t.Fatal("failed replacement changed the live secret")
	}
}
