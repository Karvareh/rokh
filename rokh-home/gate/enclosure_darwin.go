//go:build darwin

package gate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const ptDenyAttach = 31

// Harden makes the gate's own process harder to read from outside: a debugger
// may not attach to it, and it leaves no core. The binary is also meant to be
// signed with the hardened runtime, which keeps other processes of the same
// user from taking its task port. Root and the kernel are not kept out by this,
// and nothing here claims they are.
func Harden() error {
	if _, _, e := syscall.Syscall(syscall.SYS_PTRACE, ptDenyAttach, 0, 0); e != 0 {
		return e
	}
	return syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{})
}

// Enclosure names the mechanism on this platform.
const Enclosure = "sandbox-exec"

func quote(p string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(p, `\`, `\\`), `"`, `\"`) + `"`
}

// resolved is the path the kernel will judge the profile against: the deepest
// part of it that exists, with that part's links followed, and the rest of the
// name joined back on. Seatbelt matches a rule against the canonical path of
// what is opened, and on this system /tmp and /var are links into /private, so
// a folder the owner named but has not made yet — a handover made on first use,
// a program's folder made by the program — would otherwise be written into the
// profile under a name the kernel never sees, and the permission would quietly
// do nothing.
func resolved(p string) (string, bool) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	rest, cur := "", abs
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, true
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func subpaths(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		if p == "" {
			continue
		}
		abs, ok := resolved(p)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, " (subpath %s)", quote(abs))
	}
	return b.String()
}

// ancestorDirectories permits descriptor-relative, no-follow traversal to a
// granted path. Opening a directory O_RDONLY is a data read on Darwin, even
// when the caller only needs it as an openat base. Each parent is a literal:
// its directory entries are visible, but no sibling file or subtree is opened.
func ancestorDirectories(paths []string) string {
	seen := make(map[string]bool)
	for _, path := range paths {
		if path == "" {
			continue
		}
		path, ok := resolved(path)
		if !ok {
			continue
		}
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			seen[parent] = true
			if parent == filepath.Dir(parent) {
				break
			}
		}
	}
	parents := make([]string, 0, len(seen))
	for parent := range seen {
		parents = append(parents, parent)
	}
	sort.Strings(parents)
	var b strings.Builder
	for _, parent := range parents {
		fmt.Fprintf(&b, " (literal %s)", quote(parent))
	}
	return b.String()
}

// Profile is the Seatbelt profile a program runs under: nothing by default;
// the system's libraries and tools read-only; the folders the owner named; no
// network but loopback when the owner asked for the host's; no other process's
// information; and no socket or file of the gate's except the connection it was
// handed.
func Profile(spec RunSpec) string {
	exeDir := ""
	if len(spec.Argv) > 0 {
		if p, err := exec.LookPath(spec.Argv[0]); err == nil {
			exeDir = filepath.Dir(p)
		}
	}
	reads := append([]string{"/usr", "/bin", "/sbin", "/System", "/Library/Apple", "/Library/Preferences",
		"/private/etc", "/private/var/db/timezone", "/private/var/db/dyld", "/opt/homebrew", "/dev", exeDir},
		spec.ReadOnly...)
	reads = append(reads, spec.Writable...)
	reads = append(reads, spec.UnixSockets...)
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	b.WriteString("(allow process-fork)\n(allow signal (target same-sandbox))\n(allow sysctl-read)\n")
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow mach-lookup (global-name \"com.apple.system.opendirectoryd.libinfo\") (global-name \"com.apple.system.logger\") (global-name \"com.apple.system.notification_center\") (global-name \"com.apple.trustd.agent\"))\n")
	b.WriteString("(allow ipc-posix-shm-read-data (ipc-posix-name \"apple.shm.notification_center\"))\n")
	b.WriteString("(allow file-read*" + subpaths(reads) + ancestorDirectories(reads) + ")\n")
	b.WriteString("(allow file-write*" + subpaths(spec.Writable) + " (literal \"/dev/null\"))\n")
	b.WriteString("(allow file-ioctl (literal \"/dev/null\") (literal \"/dev/tty\"))\n")
	b.WriteString("(allow process-exec" + subpaths(append([]string{"/usr/bin", "/bin", "/usr/libexec", "/opt/homebrew", exeDir}, spec.ReadOnly...)) + ")\n")
	if spec.Net == "host" {
		b.WriteString("(allow network-outbound (remote ip \"localhost:*\"))\n")
		b.WriteString("(allow network-outbound (literal \"/private/var/run/mDNSResponder\"))\n")
	}
	for _, p := range spec.UnixSockets {
		b.WriteString("(allow network-outbound (literal " + quote(p) + "))\n")
	}
	return b.String()
}

func platformLaunch(spec RunSpec) (*exec.Cmd, error) {
	var err error
	spec.UnixSockets, err = socketPaths(spec.UnixSockets)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		return nil, errors.New("gate: sandbox-exec is not present")
	}
	args := append([]string{"-p", Profile(spec)}, spec.Argv...)
	cmd := exec.Command("/usr/bin/sandbox-exec", args...)
	cmd.Dir = spec.Dir
	return cmd, nil
}
