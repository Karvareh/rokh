package chest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// System is the Runner that actually runs things. The passphrase goes to the
// command's standard input and is never an argument.
type System struct{}

func (System) Run(stdin []byte, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("%s: %s", name, firstLine(text))
	}
	return text, nil
}

func firstLine(v string) string {
	if i := strings.IndexByte(v, '\n'); i >= 0 {
		return v[:i]
	}
	return v
}

var devPattern = regexp.MustCompile(`/dev/[a-zA-Z0-9_/-]+`)

func lastDevice(out, prefix string) string {
	found := ""
	for _, m := range devPattern.FindAllString(out, -1) {
		if strings.HasPrefix(m, prefix) {
			found = m
		}
	}
	return found
}

// ---------- the desktop's own disk service ----------

// desktop asks udisks2, which is the service a desktop already trusts to do
// this on behalf of whoever is sitting at it. Nothing here can prompt: every
// call says so, and a refusal is returned as a refusal rather than becoming a
// password prompt in the middle of a script.
type desktop struct{ r Runner }

func (desktop) Name() string { return "the desktop's disk service" }

func (d desktop) udisks(stdin []byte, args ...string) (string, error) {
	out, err := d.r.Run(stdin, "udisksctl", append(args, "--no-user-interaction")...)
	if err != nil && strings.Contains(out, "NotAuthorized") {
		return out, ErrNotAuthorized
	}
	return out, err
}

func (d desktop) Attach(path string) (string, error) {
	out, err := d.udisks(nil, "loop-setup", "-f", path)
	if err != nil {
		return "", err
	}
	loop := lastDevice(out, "/dev/loop")
	if loop == "" {
		return "", fmt.Errorf("the disk service did not say which device it made: %s", out)
	}
	return loop, nil
}

func (d desktop) Unlock(loop string, pass []byte) (string, error) {
	// The key is read from this process's own standard input, so the
	// passphrase travels down a pipe and never becomes an argument.
	out, err := d.udisks(pass, "unlock", "-b", loop, "--key-file", "/dev/stdin")
	if err != nil {
		return "", err
	}
	clear := lastDevice(out, "/dev/")
	if clear == "" {
		return "", fmt.Errorf("the disk service did not say what it unlocked: %s", out)
	}
	return clear, nil
}

func (d desktop) Format(clear string, owner int) error {
	// take-ownership hands the new filesystem's root to whoever asked for it,
	// which is the person whose chest this is. Without it a freshly made
	// filesystem belongs to root and its owner cannot write in it.
	_, err := d.r.Run(nil, "gdbus", "call", "--system",
		"--dest", "org.freedesktop.UDisks2",
		"--object-path", objectPath(clear),
		"--method", "org.freedesktop.UDisks2.Block.Format", "btrfs",
		"{'label': <'"+Label+"'>, 'take-ownership': <true>, 'no-discard': <true>}")
	return err
}

func (d desktop) Mount(clear string, owner int) (string, error) {
	out, err := d.udisks(nil, "mount", "-b", clear)
	if err != nil {
		return "", err
	}
	// "Mounted /dev/dm-1 at /run/media/name/rokh"
	if i := strings.LastIndex(out, " at "); i >= 0 {
		return strings.TrimRight(strings.TrimSpace(out[i+4:]), "."), nil
	}
	return "", fmt.Errorf("the disk service did not say where it mounted it: %s", out)
}

func (d desktop) Unmount(clear string) error {
	_, err := d.udisks(nil, "unmount", "-b", clear)
	return err
}

func (d desktop) Lock(loop string) error {
	_, err := d.udisks(nil, "lock", "-b", loop)
	return err
}

func (d desktop) Detach(loop string) error {
	_, err := d.udisks(nil, "loop-delete", "-b", loop)
	return err
}

// objectPath is a device's name in udisks2's own naming: every byte that is
// not a letter or a digit is written as an underscore and its hex. So dm-1 is
// dm_2d1, and asking about the wrong object is not possible by accident.
func objectPath(dev string) string {
	name := filepath.Base(dev)
	var b strings.Builder
	b.WriteString("/org/freedesktop/UDisks2/block_devices/")
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "_%02x", c)
	}
	return b.String()
}

// ---------- root, where there is nobody left to ask ----------

// superuser does the same steps with the ordinary tools. It is the route for
// a remote session, a server or a script, and it is the same order of the
// same operations; only the asking is different.
type superuser struct{ r Runner }

func (superuser) Name() string { return "root" }

func (s superuser) Attach(path string) (string, error) {
	out, err := s.r.Run(nil, "losetup", "--find", "--show", path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s superuser) Unlock(loop string, pass []byte) (string, error) {
	name := mappingFor(loop)
	if _, err := s.r.Run(pass, "cryptsetup", "open", "--key-file", "-", loop, name); err != nil {
		return "", err
	}
	return "/dev/mapper/" + name, nil
}

func (s superuser) Format(clear string, owner int) error {
	if _, err := s.r.Run(nil, "mkfs.btrfs", "-L", Label, "-f", clear); err != nil {
		return err
	}
	// A fresh filesystem's root belongs to whoever made it, and that is root
	// here. It is handed to the person whose chest this is, once, now.
	tmp, err := os.MkdirTemp("", "rokh-chest-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := s.r.Run(nil, "mount", clear, tmp); err != nil {
		return err
	}
	chownErr := os.Chown(tmp, owner, -1)
	_, umountErr := s.r.Run(nil, "umount", tmp)
	if chownErr != nil {
		return chownErr
	}
	return umountErr
}

func (s superuser) Mount(clear string, owner int) (string, error) {
	who := strconv.Itoa(owner)
	if u, err := userName(owner); err == nil {
		who = u
	}
	mp := filepath.Join("/run/media", who, Label)
	if err := os.MkdirAll(mp, 0o755); err != nil {
		return "", err
	}
	if _, err := s.r.Run(nil, "mount", clear, mp); err != nil {
		return "", err
	}
	return mp, nil
}

func (s superuser) Unmount(clear string) error {
	_, err := s.r.Run(nil, "umount", clear)
	return err
}

func (s superuser) Lock(loop string) error {
	_, err := s.r.Run(nil, "cryptsetup", "close", mappingFor(loop))
	return err
}

func (s superuser) Detach(loop string) error {
	_, err := s.r.Run(nil, "losetup", "-d", loop)
	return err
}

// mappingFor names the cleartext mapping after the loop device standing under
// it, so two chests open at once never collide and the name says what it is.
func mappingFor(loop string) string { return "rokh-" + filepath.Base(loop) }

// userName reads a name out of the password file for the mount point, so a
// chest opened by root for somebody lands where that person's disks land.
func userName(uid int) (string, error) {
	raw, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return "", err
	}
	want := strconv.Itoa(uid)
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 2 && f[2] == want {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no name for uid %d", uid)
}
