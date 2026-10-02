//go:build !windows

package main

import "syscall"

// closeOnExec keeps an inherited descriptor from reaching this command's own
// children. Host code: the portable core never sees a descriptor.
func closeOnExec(fd int) error {
	syscall.CloseOnExec(fd)
	return nil
}
