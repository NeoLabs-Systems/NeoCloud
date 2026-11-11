package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/neolabs/neocloud/clients/lite/internal/filelock"
)

// lockPair makes sure only one process syncs a pair at a time: two
// instances working on the same folder would race each other's renames
// and deletions. The lock lives in the state folder and is released by
// the returned function (or by the OS when the process dies).
func lockPair(stateDir, pairID string) (func(), error) {
	dir := filepath.Join(stateDir, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, pairID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.TryLock(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("another NeoCloud instance is syncing this folder right now")
	}
	return func() { file.Close() }, nil
}
