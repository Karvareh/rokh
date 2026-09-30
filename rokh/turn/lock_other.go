//go:build !(darwin || linux || android || freebsd || openbsd || netbsd || dragonfly || windows)

package turn

import "os"

// A host without a kernel lock offers no owner (contract 2.9).
func tryLock(f *os.File, exclusive bool) (bool, error) { return false, ErrUnavailable }

func unlock(f *os.File) error { return nil }
