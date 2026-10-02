//go:build darwin || linux || android || freebsd || openbsd || netbsd || dragonfly

package turn

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// tryLock takes flock without waiting. It reports false when another holder
// has it.
func tryLock(f *os.File, exclusive bool) (bool, error) {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	for {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EAGAIN):
			return false, nil
		default:
			return false, fmt.Errorf("turn: %w", err)
		}
	}
}

func unlock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
