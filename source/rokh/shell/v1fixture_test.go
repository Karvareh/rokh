package shell

import (
	"crypto/ed25519"
	"os"
	"testing"
	"time"

	"rokh/carrier"
	"rokh/event"
	"rokh/frame"
	"rokh/key"
	"rokh/turn"
	"rokh/vessel"
)

// Fixtures build carriers with the lowered work factor; the shell's own init
// path uses whatever iterations holds.
func init() {
	iterations = testIter
	// The key layer's judge stands in here as a test judge that judges every
	// record full and every envelope covering: tests only.
	if JudgeV1 == nil {
		JudgeV1 = func(*carrier.Carrier, string) (func(frame.ID) string, func(frame.ID, []byte) bool, error) {
			return func(frame.ID) string { return "full" }, func(frame.ID, []byte) bool { return true }, nil
		}
	}
}

// makeV1 makes a v1 carrier in dir owned by pass, whose root key is priv, and
// records its genesis on the main branch.
func makeV1(t *testing.T, dir, pass string, gen event.Signed, priv ed25519.PrivateKey) *carrier.Carrier {
	t.Helper()
	return makeV1With(t, dir, pass, gen, priv, nil)
}

// makeV1With makes a berth of an existing ledger: its owner slot keeps that
// ledger's owner reader.
func makeV1With(t *testing.T, dir, pass string, gen event.Signed, priv ed25519.PrivateKey, owner *key.Reader) *carrier.Carrier {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c, _, err := createCarrierWith(dir, pass, gen.ID, priv, owner)
	if err != nil {
		t.Fatal(err)
	}
	recordV1(t, c, dir, gen, defaultBranch)
	return c
}

// recordV1 records one signed event under the carrier's writer's turn.
func recordV1(t *testing.T, c *carrier.Carrier, dir string, e event.Signed, branch string) {
	t.Helper()
	lock, err := turn.Acquire(dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if out, err := record(c, lock, e, branch); err != nil || out != vessel.Recorded {
		t.Fatalf("recording %s: %s %v", e.ID.Short(), out, err)
	}
}

// ownerReaderOf is the owner reader the passphrase holds in a carrier.
func ownerReaderOf(t *testing.T, dir, pass string) *key.Reader {
	t.Helper()
	_, sec, err := (&session{}).openCarrier(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	r, err := key.ReaderFrom(sec.Reader[:])
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

// openV1 opens a v1 carrier with a passphrase.
func openV1(t *testing.T, dir, pass string) *carrier.Carrier {
	t.Helper()
	c, _, err := (&session{}).openCarrier(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
