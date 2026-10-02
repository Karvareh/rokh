// Package chest is Rokh's other carrier profile: one file, of a size its
// owner fixes, holding an encrypted filesystem that the vault lives in.
//
// # Why there is a second profile at all
//
// The base profile is a folder. Every event in it is sealed on its own and
// even the file names are covered, so nothing in it can be read without the
// passphrase — but the folder is still a folder. A file manager shows that a
// Rokh is there, how many events it holds, how large each one is and when it
// was last written. The ruling says the shape is a profile and not the
// carrier, and that another profile is allowed while the properties hold, so
// this is the other one: outside Rokh a chest is a single opaque file of a
// fixed size, and it says nothing about what is inside it — not the count,
// not the sizes, not the times.
//
//	— T8, T8.7, T7.6
//
// # What it is made of
//
// A regular file, filled with random bytes before anything is written to it,
// so that used and unused space cannot be told apart afterwards. A LUKS2
// header in it. A btrfs filesystem inside that. Nothing here touches a
// partition table, formats a disk or writes to a raw device: the whole chest
// is one file, and removing it is removing that file, which is what makes
// the right of exit checkable rather than promised.
//
// # Who holds the privilege
//
// Mapping an encrypted device is the kernel's device mapper, and creating a
// mapping needs privilege. That privilege does not have to be a person typing
// sudo: a desktop already trusts udisks2 to do exactly this on behalf of the
// person sitting at it, and the rule that governs it says an active local
// session may do so with no password at all. So this asks udisks2, and only
// where there is no such session does it need root — over ssh, on a server,
// or in a script. Both routes run the same steps in the same order; the
// difference is who is asked.
//
// The passphrase never appears in an argument list. It is written to the
// standard input of the one command that needs it, because an argument list
// is readable by anything on the machine that can list processes.
package chest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Runner runs one system command. Anything the passphrase must reach gets it
// on stdin; nothing gets it as an argument.
type Runner interface {
	Run(stdin []byte, name string, args ...string) (string, error)
}

// Opened is a chest that is open: the file, the loop device standing for it,
// the cleartext device the passphrase produced, and where it is mounted.
type Opened struct {
	Path  string
	Loop  string
	Clear string
	Mount string
}

// Label is the filesystem label inside a chest. It is inside the encryption,
// so it is visible only once the chest is open.
const Label = "rokh"

// MinSize is the smallest chest there is any point in making: btrfs will not
// make a filesystem in less than about 110 MB, and a chest that could not
// hold its own filesystem would fail three steps later with somebody else's
// error message.
const MinSize = 128 << 20

// keeper is whoever is asked to do the privileged part. Two answer: the
// desktop's own disk service, and root.
type keeper interface {
	Name() string
	Attach(path string) (loop string, err error)
	Unlock(loop string, pass []byte) (clear string, err error)
	Format(clear string, owner int) error
	Mount(clear string, owner int) (string, error)
	Unmount(clear string) error
	Lock(loop string) error
	Detach(loop string) error
}

// Keeper picks who is asked. Root does it directly, because there is nothing
// left to ask; anybody else asks the desktop's disk service.
func Keeper(euid int, r Runner) keeper {
	if euid == 0 {
		return superuser{r}
	}
	return desktop{r}
}

// ErrNotAuthorized is udisks2 refusing because this is not an active local
// session. It is not a failure of the chest and the message says what to do.
var ErrNotAuthorized = errors.New(
	"this is not an active local session, so the disk service will not do it without a password.\n" +
		"Run it from the terminal on your own desktop, where it needs neither sudo nor a password —\n" +
		"or, from a remote session, as root.")

// Make writes a new chest: a file of exactly this many bytes, filled with
// random, with a LUKS2 header and a btrfs filesystem inside it.
//
// The filling is the point of the size being fixed. A container that grew as
// it was used would say how much was in it, and one whose unused part was
// zeros would say the same to anyone who looked at the file; random before
// anything else means the whole file reads the same from the first byte to
// the last, whether it holds one sentence or ten thousand.
func Make(path string, bytes int64, pass []byte, owner int, k keeper, r Runner, say func(string)) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s is already there; a chest is never written over", path)
	}
	if bytes < MinSize {
		return fmt.Errorf("a chest smaller than %s has no room for its own filesystem",
			"128 MB")
	}
	say(fmt.Sprintf("filling %s with random bytes…", path))
	if err := fill(path, bytes); err != nil {
		os.Remove(path)
		return err
	}
	say("closing it with your passphrase…")
	// luksFormat writes a header into a file this person owns. It needs no
	// privilege at all: no mapping is made and no device is touched.
	if _, err := r.Run(pass, "cryptsetup", "luksFormat", "--type", "luks2",
		"--batch-mode", "--key-file", "-", path); err != nil {
		os.Remove(path)
		return fmt.Errorf("the chest could not be closed: %w", err)
	}
	say("making the filesystem inside it…")
	o, err := open(path, pass, owner, k, true)
	if err != nil {
		os.Remove(path)
		return err
	}
	return closeUp(o, k)
}

// Open unlocks a chest and mounts it, and gives back where it is.
func Open(path string, pass []byte, owner int, k keeper) (Opened, error) {
	if _, err := os.Stat(path); err != nil {
		return Opened{}, fmt.Errorf("there is no chest at %s", path)
	}
	if o, found := Attached(path); found {
		return o, fmt.Errorf("%s is already open at %s", path, o.Mount)
	}
	return open(path, pass, owner, k, false)
}

func open(path string, pass []byte, owner int, k keeper, format bool) (Opened, error) {
	o := Opened{Path: path}
	var err error
	if o.Loop, err = k.Attach(path); err != nil {
		return o, err
	}
	if o.Clear, err = k.Unlock(o.Loop, pass); err != nil {
		_ = k.Detach(o.Loop)
		return o, err
	}
	if format {
		if err = k.Format(o.Clear, owner); err != nil {
			_ = k.Lock(o.Loop)
			_ = k.Detach(o.Loop)
			return o, err
		}
	}
	if o.Mount, err = k.Mount(o.Clear, owner); err != nil {
		_ = k.Lock(o.Loop)
		_ = k.Detach(o.Loop)
		return o, err
	}
	return o, nil
}

// Close unmounts a chest, locks it and lets go of the file. Every step is
// tried even when an earlier one failed: a chest half-closed is worse than a
// chest closed with a complaint.
func Close(path string) func(k keeper) error {
	return func(k keeper) error {
		o, found := Attached(path)
		if !found {
			return fmt.Errorf("%s is not open", path)
		}
		return closeUp(o, k)
	}
}

func closeUp(o Opened, k keeper) error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	if o.Mount != "" || o.Clear != "" {
		keep(k.Unmount(o.Clear))
	}
	keep(k.Lock(o.Loop))
	keep(k.Detach(o.Loop))
	return first
}

// Attached finds a chest that is already open, by asking the kernel which
// file each loop device stands for. It reads /sys and needs no privilege.
func Attached(path string) (Opened, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Opened{}, false
	}
	for _, o := range Openings() {
		if o.Path == abs {
			return o, true
		}
	}
	return Opened{}, false
}

// Openings is every chest the kernel is holding open, with what each one is
// mapped and mounted as.
func Openings() []Opened {
	entries, err := filepath.Glob("/sys/block/loop*/loop/backing_file")
	if err != nil {
		return nil
	}
	var out []Opened
	for _, e := range entries {
		raw, err := os.ReadFile(e)
		if err != nil {
			continue
		}
		backing := strings.TrimSpace(string(raw))
		backing = strings.TrimSuffix(backing, " (deleted)")
		name := filepath.Base(filepath.Dir(filepath.Dir(e))) // loopN
		o := Opened{Path: backing, Loop: "/dev/" + name}
		o.Clear, o.Mount = clearOf(name)
		out = append(out, o)
	}
	return out
}

// clearOf finds the cleartext device a loop device was unlocked as, and where
// it is mounted, by following what the kernel already publishes: the mapping
// is a holder of the loop device, and the mount table says the rest.
func clearOf(loop string) (clear, mount string) {
	holders, _ := filepath.Glob("/sys/block/" + loop + "/holders/*")
	for _, h := range holders {
		clear = "/dev/" + filepath.Base(h)
		break
	}
	if clear == "" {
		return "", ""
	}
	// The mount table names the mapper device by its real path, and by the
	// dm name it was given; either may be what is written there.
	raw, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return clear, ""
	}
	names := []string{clear}
	if dm, err := os.ReadFile("/sys/block/" + filepath.Base(clear) + "/dm/name"); err == nil {
		names = append(names, "/dev/mapper/"+strings.TrimSpace(string(dm)))
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		for _, n := range names {
			if f[0] == n {
				return clear, unescapeMount(f[1])
			}
		}
	}
	return clear, ""
}

// unescapeMount undoes the octal escapes the mount table writes for spaces
// and the like, so a mount point with a space in it is the path it really is.
func unescapeMount(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+3 < len(v) {
			n := 0
			ok := true
			for _, c := range v[i+1 : i+4] {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				n = n*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(v[i])
	}
	return b.String()
}

// fill writes the file, full size, from the kernel's random source. It is
// written in one pass and the file is left exactly the size that was asked
// for: no sparseness, no growing, nothing to read a usage off.
func fill(path string, bytes int64) error {
	src, err := os.Open("/dev/urandom")
	if err != nil {
		return err
	}
	defer src.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 1<<20)
	for written := int64(0); written < bytes; {
		n := int64(len(buf))
		if left := bytes - written; left < n {
			n = left
		}
		if _, err := src.Read(buf[:n]); err != nil {
			return err
		}
		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
		written += n
	}
	return f.Sync()
}
