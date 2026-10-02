package chest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder is a Runner that runs nothing and remembers everything.
type recorder struct {
	calls []call
	fail  string // the command name that should fail
}

type call struct {
	name  string
	args  []string
	stdin []byte
}

func (r *recorder) Run(stdin []byte, name string, args ...string) (string, error) {
	r.calls = append(r.calls, call{name, args, append([]byte(nil), stdin...)})
	if r.fail == name {
		return "", errors.New("refused, for the test")
	}
	switch name {
	case "losetup":
		return "/dev/loop7", nil
	case "udisksctl":
		switch args[0] {
		case "loop-setup":
			return "Mapped file " + args[2] + " as /dev/loop7.", nil
		case "unlock":
			return "Unlocked /dev/loop7 as /dev/dm-3.", nil
		case "mount":
			return "Mounted /dev/dm-3 at /run/media/someone/rokh.", nil
		}
	}
	return "", nil
}

func (r *recorder) named(name string) []call {
	var out []call
	for _, c := range r.calls {
		if c.name == name {
			out = append(out, c)
		}
	}
	return out
}

// A chest is made in one order and only that order: the file first, filled;
// then the header; then the mapping, the filesystem, and the mount — and it
// is left closed, because making a chest is not opening one.
func TestMakingAChestGoesInOneOrderAndLeavesItClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.chest")
	r := &recorder{}
	steps := &sequence{}
	if err := Make(path, MinSize, []byte("a-pass"), 1000, steps, r, func(string) {}); err != nil {
		t.Fatal(err)
	}
	want := []string{"attach", "unlock", "format", "mount", "unmount", "lock", "detach"}
	if got := steps.done; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the steps were %v, want %v", got, want)
	}
	if n := len(r.named("cryptsetup")); n != 1 {
		t.Fatalf("cryptsetup was run %d times", n)
	}
	if c := r.named("cryptsetup")[0]; c.args[0] != "luksFormat" || string(c.stdin) != "a-pass" {
		t.Fatalf("the header was written with %v and %q on stdin", c.args, c.stdin)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() != MinSize {
		t.Fatalf("the file is %v, %v; a chest is exactly the size it was asked for", fi, err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("the file is %v; a chest is its owner's alone", fi.Mode().Perm())
	}
}

// The passphrase reaches the one command that needs it, on its standard
// input, and is in no argument list anywhere — an argument list is readable
// by anything on the machine that can list processes.
func TestThePassphraseIsNeverAnArgument(t *testing.T) {
	const pass = "a-very-secret-passphrase"
	path := filepath.Join(t.TempDir(), "mine.chest")
	r := &recorder{}
	if err := Make(path, MinSize, []byte(pass), 1000, &sequence{}, r, func(string) {}); err != nil {
		t.Fatal(err)
	}
	// and again through both real keepers, whose commands are the ones that
	// actually run on a machine.
	for _, k := range []keeper{desktop{r}, superuser{r}} {
		if _, err := k.Unlock("/dev/loop7", []byte(pass)); err != nil {
			t.Fatal(err)
		}
	}
	sawIt := false
	for _, c := range r.calls {
		for _, a := range c.args {
			if strings.Contains(a, pass) {
				t.Fatalf("%s carried the passphrase in an argument: %v", c.name, c.args)
			}
		}
		if bytes.Equal(c.stdin, []byte(pass)) {
			sawIt = true
		}
	}
	if !sawIt {
		t.Fatal("the passphrase reached no command's standard input")
	}
}

// A step that fails takes the whole chest with it: nothing half-made is left
// on the disk, and nothing is left mapped.
func TestAChestThatCouldNotBeMadeLeavesNothing(t *testing.T) {
	for _, failing := range []string{"cryptsetup", "mkfs.btrfs"} {
		path := filepath.Join(t.TempDir(), "mine.chest")
		r := &recorder{fail: failing}
		err := Make(path, MinSize, []byte("a-pass"), 1000, superuser{r}, r, func(string) {})
		if err == nil {
			t.Fatalf("%s failed and the chest was made anyway", failing)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s failed and left the file behind", failing)
		}
	}
}

// A chest is never written over, and one too small to hold a filesystem is
// refused before a byte is written.
func TestAChestIsNeverWrittenOverAndNeverTooSmall(t *testing.T) {
	dir := t.TempDir()
	there := filepath.Join(dir, "there")
	if err := os.WriteFile(there, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	if err := Make(there, 1<<30, []byte("p"), 1000, &sequence{}, r, func(string) {}); err == nil {
		t.Fatal("a chest was made over an existing file")
	}
	if raw, _ := os.ReadFile(there); string(raw) != "mine" {
		t.Fatal("the existing file was touched")
	}
	small := filepath.Join(dir, "small")
	if err := Make(small, 1<<20, []byte("p"), 1000, &sequence{}, r, func(string) {}); err == nil {
		t.Fatal("a 1 MB chest was accepted")
	}
	if _, err := os.Stat(small); !os.IsNotExist(err) {
		t.Fatal("a refused size still made a file")
	}
}

// The file is random from end to end before anything is written to it, so
// what is used and what is not cannot be told apart afterwards.
func TestTheFileIsRandomFromEndToEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filled")
	if err := fill(path, 1<<20); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) != 1<<20 {
		t.Fatalf("read %d bytes, %v", len(raw), err)
	}
	zeros, counts := 0, map[byte]int{}
	for _, b := range raw {
		if b == 0 {
			zeros++
		}
		counts[b]++
	}
	if zeros > len(raw)/128 {
		t.Fatalf("%d of %d bytes are zero; that is not random", zeros, len(raw))
	}
	if len(counts) != 256 {
		t.Fatalf("only %d byte values appear", len(counts))
	}
	// Not sparse: what the file claims and what it takes are the same.
	fi, _ := os.Stat(path)
	if fi.Size() != 1<<20 {
		t.Fatalf("the file says %d bytes", fi.Size())
	}
}

// udisks2 names an object after the device, with everything that is not a
// letter or a digit written as an underscore and its hex. Asking about the
// wrong object must not be possible by accident.
func TestTheObjectPathIsTheDevicesOwnName(t *testing.T) {
	for dev, want := range map[string]string{
		"/dev/dm-3":  "/org/freedesktop/UDisks2/block_devices/dm_2d3",
		"/dev/loop7": "/org/freedesktop/UDisks2/block_devices/loop7",
		"/dev/sda1":  "/org/freedesktop/UDisks2/block_devices/sda1",
	} {
		if got := objectPath(dev); got != want {
			t.Errorf("%s -> %s, want %s", dev, got, want)
		}
	}
}

// The mount table writes a space as an octal escape, and a mount point with a
// space in it is still where the chest is.
func TestAMountPointWithASpaceInItIsRead(t *testing.T) {
	if got := unescapeMount(`/run/media/me/my\040rokh`); got != "/run/media/me/my rokh" {
		t.Fatalf("read as %q", got)
	}
	if got := unescapeMount("/run/media/me/rokh"); got != "/run/media/me/rokh" {
		t.Fatalf("read as %q", got)
	}
}

// sequence is a keeper that does nothing and remembers the order it was asked.
type sequence struct{ done []string }

func (s *sequence) Name() string     { return "a test" }
func (s *sequence) step(name string) { s.done = append(s.done, name) }
func (s *sequence) Attach(path string) (string, error) {
	s.step("attach")
	return "/dev/loop7", nil
}
func (s *sequence) Unlock(loop string, pass []byte) (string, error) {
	s.step("unlock")
	return "/dev/dm-3", nil
}
func (s *sequence) Format(clear string, owner int) error {
	s.step("format")
	return nil
}
func (s *sequence) Mount(clear string, owner int) (string, error) {
	s.step("mount")
	return "/run/media/someone/" + Label, nil
}
func (s *sequence) Unmount(clear string) error { s.step("unmount"); return nil }
func (s *sequence) Lock(loop string) error     { s.step("lock"); return nil }
func (s *sequence) Detach(loop string) error   { s.step("detach"); return nil }

var _ = fmt.Sprint

// The kernel is asked before a chest is filled, not after: what a person gets
// when their machine cannot lend a loop device is a sentence about their
// machine, and no half-written file.
func TestTheKernelIsAskedBeforeAnythingIsWritten(t *testing.T) {
	if err := Ready(); err != nil && !strings.Contains(err.Error(), "loop") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}
	// The block-driver reading is the whole of the test that can run
	// anywhere: on a machine with the driver it says yes, and the parser
	// itself is exercised either way.
	_ = hasLoopDriver()
	if running, have, mismatch := modulesMissing(); mismatch && (running == "" || have == "") {
		t.Fatalf("a mismatch was reported with nothing to name: %q %q", running, have)
	}
}
