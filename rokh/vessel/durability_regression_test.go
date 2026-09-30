// Regression tests for the durability of a recording.

package vessel_test

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"rokh/frame"
	"rokh/vessel"
)

type hostOwner struct{}

func (hostOwner) Holds() error { return nil }

type hostFlushFault struct {
	*vessel.Memory
	failHeadFlush bool
	failHeadRead  bool
	headWritten   bool
}

func (m *hostFlushFault) Write(name string, b []byte) error {
	if err := m.Memory.Write(name, b); err != nil {
		return err
	}
	if strings.HasPrefix(name, "rokh/head") {
		m.headWritten = true
		if m.failHeadFlush {
			return errors.New("synthetic head fsync failed after bytes reached the page cache")
		}
	}
	return nil
}
func (m *hostFlushFault) Read(name string, max int) ([]byte, error) {
	if m.headWritten && m.failHeadRead && strings.HasPrefix(name, "rokh/head") {
		return nil, errors.New("synthetic readback unavailable")
	}
	return m.Memory.Read(name, max)
}
func hostVessel(t *testing.T, m vessel.Medium) *vessel.Vessel {
	t.Helper()
	v, err := vessel.Create(m, vessel.Params{SlabLog2: 18, Slabs: 16, Iter: 1, Rand: rand.NewChaCha8([32]byte{4})}, bytes.Repeat([]byte{7}, 32), nil, vessel.Root{Anchor: frame.Hash([]byte("synthetic host anchor"))})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func hostCommit(t *testing.T, v *vessel.Vessel) (vessel.Outcome, error) {
	t.Helper()
	tx, err := v.Begin(hostOwner{})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("synthetic opaque content envelope")
	if err := tx.Put(vessel.Header{Type: vessel.RecContent, ID: frame.Hash(body), Chunk: 0, Chunks: 1, Size: uint64(len(body))}, body); err != nil {
		t.Fatal(err)
	}
	return tx.Commit()
}

func TestFailedHeadFlushCannotClaimRecorded(t *testing.T) {
	m := &hostFlushFault{Memory: vessel.NewMemory()}
	v := hostVessel(t, m)
	m.headWritten = false
	m.failHeadFlush = true
	outcome, err := hostCommit(t, v)
	if outcome == vessel.Recorded {
		t.Fatalf("reported recorded despite explicit head flush failure; outcome=%s err=%v", outcome, err)
	}
	if outcome != vessel.Unknown {
		t.Fatalf("visible bytes with unconfirmed durability require unknown, got %s (%v)", outcome, err)
	}
}

func TestUnavailableReadbackReturnsUnknownAndReopenRecovers(t *testing.T) {
	m := &hostFlushFault{Memory: vessel.NewMemory()}
	v := hostVessel(t, m)
	m.headWritten = false
	m.failHeadRead = true
	outcome, err := hostCommit(t, v)
	if outcome != vessel.Unknown || err == nil {
		t.Fatalf("readback failure must be unknown: %s %v", outcome, err)
	}
	m.failHeadRead = false
	opened, rep, err := vessel.Open(m, func([][]byte, []byte, int) ([]byte, error) { return bytes.Repeat([]byte{7}, 32), nil }, rand.NewChaCha8([32]byte{5}))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := opened.Scan(func(h vessel.Header, r vessel.Ref) error { _, e := opened.Body(r); n++; return e }); err != nil {
		t.Fatal(err)
	}
	if rep.Generation != 2 || n != 1 {
		t.Fatalf("recovered generation=%d records=%d", rep.Generation, n)
	}
}
