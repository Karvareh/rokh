package chest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A chest needs the kernel to lend it a loop device: a file cannot be mapped
// as an encrypted volume without first being a block device. That is asked
// for before anything is written, because filling a file with a few gigabytes
// of random and only then finding out is a long way to go for a refusal.
//
// The check is a reading of what the kernel publishes about itself, and the
// answer says what to do rather than what failed. One cause is common enough
// to name: a machine updated and not restarted since is running a kernel
// whose modules have already been replaced on disk by the next one's, so
// every driver that was not already loaded is simply gone until it restarts.

// Ready reports whether this kernel can lend a loop device, and says why not
// in terms of the machine rather than in terms of the failing call.
func Ready() error {
	if _, err := os.Stat("/dev/loop-control"); err != nil {
		return fmt.Errorf("this kernel has no loop device at all (/dev/loop-control is not there), " +
			"so a file cannot be opened as a volume here")
	}
	if hasLoopDriver() {
		return nil
	}
	msg := "the loop driver is not in the running kernel, so a file cannot be mapped as a volume"
	if running, have, mismatch := modulesMissing(); mismatch {
		msg += fmt.Sprintf(".\nThis machine is running %s and the modules on disk are for %s: "+
			"it was updated and has not been restarted since, and every driver that was not\n"+
			"already loaded is gone until it is. Restarting puts them back", running, have)
	}
	return fmt.Errorf("%s", msg)
}

// hasLoopDriver reads the block drivers the kernel has. The loop driver
// registers itself there when it is present, whether it was built in or
// loaded later.
func hasLoopDriver() bool {
	raw, err := os.ReadFile("/proc/devices")
	if err != nil {
		return true // nothing to read; let the tools speak for themselves
	}
	block := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "Block devices:") {
			block = true
			continue
		}
		if strings.HasSuffix(line, "devices:") {
			block = false
			continue
		}
		if block {
			f := strings.Fields(line)
			if len(f) == 2 && f[1] == "loop" {
				return true
			}
		}
	}
	return false
}

// modulesMissing compares the kernel that is running with the modules that
// are on the disk for it.
func modulesMissing() (running, have string, mismatch bool) {
	raw, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return "", "", false
	}
	running = strings.TrimSpace(string(raw))
	if _, err := os.Stat(filepath.Join("/lib/modules", running)); err == nil {
		return running, running, false
	}
	entries, err := os.ReadDir("/lib/modules")
	if err != nil || len(entries) == 0 {
		return running, "nothing", true
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return running, strings.Join(names, ", "), true
}
