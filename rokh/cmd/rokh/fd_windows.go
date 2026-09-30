//go:build windows

package main

import "errors"

// closeOnExec: a home's gate hands a program an inherited Unix descriptor,
// which this platform does not offer. It is refused, never faked.
func closeOnExec(int) error {
	return errors.New("an inherited home descriptor is not available on windows")
}
