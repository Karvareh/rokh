package native

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rokh-home/store"
)

// hooked is a real sealed store with a place to make one call fail. The failures
// below are injected, not argued about: what a checkpoint does when the seal or
// the profile save goes wrong is the thing under test.
type hooked struct {
	mu sync.Mutex
	Sealer
	putErr      error
	pointerErr  error
	pointerOn   string
	pointerSkip int
	getBroken   bool
	// slow holds every seal open for a while, so two callers racing really do
	// overlap rather than passing one after the other.
	slow time.Duration
}

func (h *hooked) Put(kind, id string, r io.Reader, want []byte) (store.Info, error) {
	h.mu.Lock()
	err, slow := h.putErr, h.slow
	h.mu.Unlock()
	if slow > 0 {
		time.Sleep(slow)
	}
	if err != nil {
		// Fail exactly as an early refusal does: without draining the reader.
		return store.Info{}, err
	}
	return h.Sealer.Put(kind, id, r, want)
}

func (h *hooked) SetPointer(name string, value []byte) error {
	h.mu.Lock()
	err := h.pointerErr
	if err != nil && (h.pointerOn == "" || strings.Contains(name, h.pointerOn)) {
		if h.pointerSkip > 0 {
			h.pointerSkip--
			err = nil
		}
	} else {
		err = nil
	}
	h.mu.Unlock()
	if err != nil {
		return err
	}
	return h.Sealer.SetPointer(name, value)
}

func (h *hooked) Get(kind, id string) (io.ReadCloser, error) {
	rc, err := h.Sealer.Get(kind, id)
	if err != nil || !h.getBroken {
		return rc, err
	}
	return &shortReader{rc: rc, left: 64}, nil
}

type shortReader struct {
	rc   io.ReadCloser
	left int
}

func (s *shortReader) Read(p []byte) (int, error) {
	if s.left <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if len(p) > s.left {
		p = p[:s.left]
	}
	n, err := s.rc.Read(p)
	s.left -= n
	return n, err
}

func (s *shortReader) Close() error { return s.rc.Close() }

func hookedManager(t *testing.T) (*Manager, *hooked) {
	t.Helper()
	h := &hooked{Sealer: sealed(t)}
	return New(h, Binaries{}), h
}

func quiescedProfile(t *testing.T, m *Manager) *Profile {
	t.Helper()
	p := newProfile(t, m)
	p.RuntimeDir, p.Quiesced, p.HeadRead = fakeRuntime(t, p), true, true
	p.LastHead = ChainHead{Seq: 3, Action: "uhCkkSYNTHETIC"}
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A seal that refuses early stops reading the pipe. The writer filling it must
// not be left waiting for a reader that will never come back.
func TestASealThatRefusesEarlyDoesNotWedgeTheWriter(t *testing.T) {
	m, h := hookedManager(t)
	p := quiescedProfile(t, m)
	h.putErr = errors.New("synthetic: the store refused")
	done := make(chan error, 1)
	go func() {
		_, err := m.Checkpoint(p.ID, "attempt-seal-fails")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not sealed") {
			t.Fatalf("want a sealing failure, got %v", err)
		}
	case <-timeout(t):
		t.Fatal("the checkpoint never returned: the tar writer is waiting on a reader that stopped")
	}
	// Nothing was activated.
	after, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Generation != p.Generation || after.Snapshot != nil {
		t.Fatalf("a failed seal must leave the identity where it was: %+v", after)
	}
}

// The object is sealed and then the profile cannot be saved. The identity stays
// where it was, the answer says so, and — the point of naming objects after
// attempts — a fresh attempt afterwards still works even though the runtime
// folder has changed in the meantime.
func TestAFailedProfileSaveDoesNotWedgeLaterAttempts(t *testing.T) {
	m, h := hookedManager(t)
	p := quiescedProfile(t, m)
	h.pointerErr, h.pointerOn = errors.New("synthetic: the profile could not be saved"), "native/profile/"
	out, err := m.Checkpoint(p.ID, "attempt-one")
	if err == nil || !strings.Contains(err.Error(), "did not move to it") {
		t.Fatalf("want a sealed-but-not-activated failure, got %v (%v)", err, out)
	}
	if out["sealed"] != true || out["activated"] != false {
		t.Fatalf("the answer should say sealed and not activated: %v", out)
	}
	h.pointerErr = nil
	// The runtime folder changes, as it would if the conductor had run again.
	if err := os.WriteFile(filepath.Join(p.RuntimeDir, "data", "extra"), []byte("more state"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The same attempt must not produce a different version.
	if _, err := m.Checkpoint(p.ID, "attempt-one"); err == nil {
		t.Fatal("repeating an attempt over a changed folder must not seal a different version under that name")
	}
	// A new attempt is a new name, and works.
	out, err = m.Checkpoint(p.ID, "attempt-two")
	if err != nil {
		t.Fatalf("a fresh attempt should not be wedged by the earlier failure: %v", err)
	}
	if out["already"] != false {
		t.Fatalf("unexpected: %v", out)
	}
	after, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Generation != 2 || after.Snapshot == nil || after.Snapshot.Attempt != "attempt-two" {
		t.Fatalf("the identity should have moved on the successful attempt: %+v", after.Snapshot)
	}
}

// A checkpoint that cannot be read back whole does not restore.
func TestRestoreRefusesAnObjectThatDoesNotReadBack(t *testing.T) {
	m, h := hookedManager(t)
	p := quiescedProfile(t, m)
	if _, err := m.Checkpoint(p.ID, "attempt-one"); err != nil {
		t.Fatal(err)
	}
	h.getBroken = true
	fresh := filepath.Join(t.TempDir(), "root-b")
	if _, err := m.Restore(p.ID, fresh); err == nil {
		t.Fatal("a short read of the sealed object must not count as a restore")
	}
	after, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RuntimeDir == fresh {
		t.Fatal("a failed restore must not move the profile onto the folder it failed into")
	}
}

// Restore leaves no dead conductor record behind, and takes the identity and
// head from the checkpoint rather than from whatever the profile last held.
func TestRestoreClearsTheOldLiveRecord(t *testing.T) {
	m := manager(t)
	p := quiescedProfile(t, m)
	p.AgentKey, p.DNAHash = "uhCAkAGENT", "uhC0kDNA"
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "attempt-one"); err != nil {
		t.Fatal(err)
	}
	stale, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale.Live = &Live{PID: 999999, RuntimeDir: stale.RuntimeDir, Generation: stale.Generation}
	stale.AgentKey = "uhCAkSOMEONE-ELSE"
	if err := m.saveProfile(stale); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(t.TempDir(), "root-b")
	if _, err := m.Restore(p.ID, fresh); err != nil {
		t.Fatal(err)
	}
	after, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Live != nil {
		t.Fatalf("a restored profile should hold no conductor record: %+v", after.Live)
	}
	if after.AgentKey != "uhCAkAGENT" || after.LastHead.Action != "uhCkkSYNTHETIC" || !after.HeadRead {
		t.Fatalf("the restored identity and head should be the checkpoint's: %+v", after)
	}
}

// A checkpoint whose head was never read names a head nobody saw.
func TestCheckpointRefusesWhenTheHeadWasNeverRead(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	p.RuntimeDir, p.Quiesced, p.HeadRead = fakeRuntime(t, p), true, false
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint(p.ID, "attempt-one"); !errors.Is(err, ErrUnknownEnding) {
		t.Fatalf("want ErrUnknownEnding, got %v", err)
	}
}

// A start that begins while the home is closing must not leave a conductor
// behind the shutdown.
func TestStartRefusesWhileClosing(t *testing.T) {
	m := New(sealed(t), fakeEngine(t))
	p := newProfile(t, m)
	m.Quiesce("")
	if !m.Closing() {
		t.Fatal("the manager should be closing")
	}
	if _, err := m.Start(StartInput{Profile: p.ID, RuntimeDir: t.TempDir()}); !errors.Is(err, ErrClosing) {
		t.Fatalf("want ErrClosing, got %v", err)
	}
}

// The owner's confirmation binds one identity, and is checked again when it is
// spent rather than only when it was given.
func TestConfirmationBindsTheIdentityItNamed(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	p.AgentKey, p.DNAHash = "uhCAkAGENT", "uhC0kDNA"
	if err := m.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	c, err := m.Confirm("hash-1", p.ID, "consumer-a", "path", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CheckIdentity(c); err != nil {
		t.Fatal(err)
	}
	moved, err := m.loadProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	moved.AgentKey = "uhCAkANOTHER"
	if err := m.saveProfile(moved); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckIdentity(c); err == nil {
		t.Fatal("a confirmation must not be spendable under an identity that is no longer the one confirmed")
	}
	if _, err := m.Confirmation("no-such-hash"); !errors.Is(err, ErrNoConfirmation) {
		t.Fatalf("want ErrNoConfirmation, got %v", err)
	}
}

// A profile with no identity yet cannot be confirmed for: there would be
// nothing to bind.
func TestConfirmRefusesAProfileWithNoIdentity(t *testing.T) {
	m := manager(t)
	p := newProfile(t, m)
	if _, err := m.Confirm("hash-1", p.ID, "consumer-a", "path", 1); err == nil {
		t.Fatal("a profile that has never started has no identity to confirm for")
	}
}

// An attempt's name carries the program that made it, so no program can name —
// or ask about — another's.
func TestAttemptNamesCarryTheProgram(t *testing.T) {
	mine := AttemptFor("hash-1", "consumer-a", "")
	theirs := AttemptFor("hash-1", "consumer-b", "")
	if mine == theirs {
		t.Fatal("two programs must not share one attempt name for the same disclosure")
	}
	if AttemptFor("hash-1", "consumer-a", "") != mine {
		t.Fatal("the same program and disclosure must name the same attempt")
	}
	if AttemptFor("hash-1", "consumer-a", "second") == mine {
		t.Fatal("a program's own suffix should make a second attempt of its own")
	}
}

func timeout(t *testing.T) <-chan time.Time {
	t.Helper()
	return time.After(30 * time.Second)
}
