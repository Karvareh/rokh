//go:build windows

package turn

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// On windows the turn is a byte-range lock beyond the end of the head file
// (LockFileEx), through syscall only. The range starts far past any length
// the file will have, so it never covers bytes a reader reads.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errorLockViolation      = 33
	rangeOffsetHigh         = 0x7FFFFFFF // bytes 2^62 and up: beyond the end of any head file
)

func overlapped() *syscall.Overlapped {
	return &syscall.Overlapped{Offset: 0, OffsetHigh: rangeOffsetHigh}
}

func tryLock(f *os.File, exclusive bool) (bool, error) {
	flags := uint32(lockfileFailImmediately)
	if exclusive {
		flags |= lockfileExclusiveLock
	}
	r, _, err := procLockFileEx.Call(f.Fd(), uintptr(flags), 0, 1, 0, uintptr(unsafe.Pointer(overlapped())))
	if r != 0 {
		return true, nil
	}
	if errno, ok := err.(syscall.Errno); ok && errno == errorLockViolation {
		return false, nil
	}
	return false, fmt.Errorf("turn: LockFileEx: %v", err)
}

func unlock(f *os.File) error {
	r, _, err := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(overlapped())))
	if r == 0 {
		return fmt.Errorf("turn: UnlockFileEx: %v", err)
	}
	return nil
}
