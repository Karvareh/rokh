package turn

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func vesselDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rokh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(HeadFile)), make([]byte, 65536), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestHelperHoldsTheTurn is not a test on its own: it is the other process.
func TestHelperHoldsTheTurn(t *testing.T) {
	dir := os.Getenv("ROKH_TURN_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process only")
	}
	l, err := Acquire(dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("holding\n")
	io.Copy(io.Discard, os.Stdin)
	l.Release()
}

// One writer across processes; the turn comes back on release and on death,
// never by a timer while the holder lives.
func TestTheTurnIsOneWriterAcrossProcesses(t *testing.T) {
	for _, ending := range []string{"release", "kill", "pause"} {
		t.Run(ending, func(t *testing.T) {
			dir := vesselDir(t)
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldsTheTurn$")
			cmd.Env = append(os.Environ(), "ROKH_TURN_HELPER_DIR="+dir)
			stdin, _ := cmd.StdinPipe()
			stdout, _ := cmd.StdoutPipe()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "holding\n" {
				t.Fatalf("helper did not take the turn: %q %v", line, err)
			}
			if _, err := Acquire(dir, 150*time.Millisecond); !errors.Is(err, ErrBusy) {
				t.Fatalf("a second writer took a held turn: %v", err)
			}
			if _, err := AcquireShared(dir, 150*time.Millisecond); !errors.Is(err, ErrBusy) {
				t.Fatalf("a reader took the turn while a writer held it: %v", err)
			}
			switch ending {
			case "release":
				stdin.Close()
			case "kill":
				cmd.Process.Kill()
			case "pause":
				// A paused holder still holds: waiting longer changes nothing.
				time.Sleep(300 * time.Millisecond)
				if _, err := Acquire(dir, 200*time.Millisecond); !errors.Is(err, ErrBusy) {
					t.Fatalf("the turn was taken from a living holder: %v", err)
				}
				stdin.Close()
			}
			cmd.Wait()
			l, err := Acquire(dir, 5*time.Second)
			if err != nil {
				t.Fatalf("the turn did not come back after %s: %v", ending, err)
			}
			if err := l.Holds(); err != nil {
				t.Fatal(err)
			}
			l.Release()
		})
	}
}

func TestReadersShareAndKeepAWriterOut(t *testing.T) {
	dir := vesselDir(t)
	r1, err := AcquireShared(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := AcquireShared(dir, time.Second)
	if err != nil {
		t.Fatalf("two readers could not share: %v", err)
	}
	if _, err := Acquire(dir, 100*time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("a writer got in while readers held the turn: %v", err)
	}
	r1.Release()
	r2.Release()
	w, err := Acquire(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	w.Release()
}

// Holds answers from the live handle: released, or the head file replaced,
// is not holding.
func TestHoldsAnswersFromTheLiveHandle(t *testing.T) {
	dir := vesselDir(t)
	l, err := Acquire(dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Holds(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, filepath.FromSlash(HeadFile))
	other := p + ".other"
	os.WriteFile(other, make([]byte, 65536), 0o600)
	os.Rename(other, p)
	if err := l.Holds(); err == nil {
		t.Fatal("a lock on a replaced head file still says it holds")
	}
	l.Release()
	if err := l.Holds(); err == nil {
		t.Fatal("a released lock still says it holds")
	}
	if err := l.Release(); err != nil {
		t.Fatal("release is not idempotent")
	}
}

func TestNoTurnWithoutAVessel(t *testing.T) {
	if _, err := Acquire(t.TempDir(), time.Millisecond); err == nil {
		t.Fatal("took a turn on a folder with no vessel")
	}
}
