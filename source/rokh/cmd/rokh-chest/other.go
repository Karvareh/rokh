//go:build !linux

package main

import (
	"fmt"
	"os"
)

// A chest is a LUKS2 header and a btrfs filesystem inside one file, and both
// are the Linux kernel's. On any other system this says so and does nothing:
// a command that pretended to work here would leave somebody believing their
// ledger was in a chest when it was in a folder.
func main() {
	fmt.Fprintln(os.Stderr, "rokh-chest works on Linux only: a chest is LUKS2 and btrfs, and both are the Linux kernel's.")
	fmt.Fprintln(os.Stderr, "On this system a Rokh is a folder — everything in it is sealed, but the folder is a folder.")
	os.Exit(2)
}
