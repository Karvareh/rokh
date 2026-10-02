//go:build !windows

package main

import "syscall"

// closeOnExec keeps an inherited descriptor from reaching this command's own
// children. Host code.
func closeOnExec(fd int) error {
	syscall.CloseOnExec(fd)
	return nil
}
