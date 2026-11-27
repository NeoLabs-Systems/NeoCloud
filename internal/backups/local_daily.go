package backups

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	localDailyMinAge = 23 * time.Hour
)

var localDailyNamePattern = regexp.MustCompile(`^daily-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}Z\.db\.enc$`)

type localDailySnapshot struct {
	name    string
	path    string
	created time.Time
}

func listLocalDailySnapshots(root string) ([]localDailySnapshot, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []localDailySnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]localDailySnapshot, 0)
	for _, entry := range entries {
		if !localDailyNamePattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		items = append(items, localDailySnapshot{name: entry.Name(), path: path, created: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name > items[j].name })
	return items, nil
}

func localDailySnapshotDue(root string, now time.Time) (bool, error) {
	items, err := listLocalDailySnapshots(root)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return true, nil
	}
	return now.Sub(items[0].created) >= localDailyMinAge, nil
}

// createLocalDailySnapshot is the fallback while no target is scheduled: it
// undoes database damage, not the loss of the disk.
func (r *Runner) createLocalDailySnapshot(ctx context.Context, now time.Time) error {
	_, err := r.CreateLocalSnapshot(ctx, SnapshotDaily, now)
	return err
}
