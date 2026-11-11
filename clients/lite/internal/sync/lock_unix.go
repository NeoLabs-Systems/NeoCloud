//go:build !windows

package sync

import (
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes an exclusive, non-blocking lock on file.
func tryLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
