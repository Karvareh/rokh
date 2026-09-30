package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// The home is a separate layer over the independently usable ledger. It is
// installed beside this executable as part of the same versioned release.
// Resolve that exact sibling, not an unrelated binary found in PATH; preserve
// stdin and inherited descriptors so passphrases never need an argument.
func cmdHome(args []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}
	bin := filepath.Join(filepath.Dir(self), "rokh-home")
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("rokh: the home executable is missing from this release; install the complete Rokh package")
	}
	return syscall.Exec(bin, append([]string{bin}, args...), os.Environ())
}
