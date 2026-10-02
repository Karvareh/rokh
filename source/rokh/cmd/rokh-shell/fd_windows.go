//go:build windows

package main

import "errors"

// closeOnExec: an inherited Unix descriptor is not offered on this platform.
// It is refused, never faked.
func closeOnExec(int) error {
	return errors.New("an inherited home descriptor is not available on windows")
}
