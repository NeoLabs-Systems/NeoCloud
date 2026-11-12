//go:build windows

package filelock

import (
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes an exclusive, non-blocking lock on file.
func TryLock(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
}
