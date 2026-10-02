//go:build linux

package gate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Harden makes the gate's own process harder to read from outside: it is not
// dumpable, so another process of the same user cannot open its memory through
// /proc or attach to it, and it leaves no core. Root and the kernel are not
// kept out by this, and nothing here claims they are.
func Harden() error {
	if _, _, e := syscall.RawSyscall6(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0, 0, 0, 0); e != 0 {
		return e
	}
	return syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{})
}

// Enclosure names the mechanism on this platform.
const Enclosure = "bwrap"

// platformLaunch starts a program with bubblewrap: its own user, PID, IPC, UTS
// and cgroup namespaces, a new session, no capabilities, its own /tmp and /proc,
// the system's /usr and /etc read-only, and nothing of the user's home except
// the folders the owner named. The network namespace is its own too, unless the
// owner asked for the host's.
func platformLaunch(spec RunSpec) (*exec.Cmd, error) {
	sockets, err := socketPaths(spec.UnixSockets)
	if err != nil {
		return nil, err
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, errors.New("gate: bubblewrap is not installed")
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid",
		"--unshare-ipc", "--unshare-uts", "--unshare-cgroup-try", "--cap-drop", "ALL"}
	if spec.Net != "host" {
		args = append(args, "--unshare-net")
	}
	args = append(args, "--ro-bind", "/usr", "/usr", "--ro-bind", "/etc", "/etc",
		"--symlink", "usr/bin", "/bin", "--symlink", "usr/bin", "/sbin",
		"--symlink", "usr/lib", "/lib", "--symlink", "usr/lib", "/lib64",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run")
	for _, p := range spec.ReadOnly {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--ro-bind", p, p)
		}
	}
	for _, p := range spec.Writable {
		abs, _ := filepath.Abs(p)
		args = append(args, "--bind", abs, abs)
	}
	for _, p := range sockets {
		args = append(args, "--ro-bind", p, p)
	}
	args = append(args, "--chdir", spec.Dir, "--")
	args = append(args, spec.Argv...)
	cmd := exec.Command(bwrap, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: false}
	return cmd, nil
}
