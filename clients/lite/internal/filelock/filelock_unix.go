//go:build !windows

package filelock

import (
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes an exclusive, non-blocking lock on file.
func TryLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
