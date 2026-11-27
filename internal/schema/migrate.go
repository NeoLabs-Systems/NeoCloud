package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
)

const migrationSnapshotKeep = 3

var migrationSnapshotName = regexp.MustCompile(`^pre-migration-v\d+-.+\.db$`)

// Migrate backs up an existing older NeoCloud database before applying any
// pending migration. Fresh databases need no snapshot. A failed copy prevents
// the schema from being changed.
func Migrate(ctx context.Context, db *dbx.DB, snapshotDir string) error {
	var current int
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("schema: read current version: %w", err)
	}
	if current > Version {
		return fmt.Errorf("The database has schema version %d, but this NeoCloud only knows up to %d. Update NeoCloud or restore the database snapshot from before the update.", current, Version)
	}
	if current == 0 {
		var applicationObjects int
		if err := db.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
			WHERE name NOT LIKE 'sqlite_%' AND type IN ('table','view','trigger')`).Scan(&applicationObjects); err != nil {
			return fmt.Errorf("schema: inspect unversioned database: %w", err)
		}
		if applicationObjects != 0 {
			if err := snapshotBeforeMigration(ctx, db.W, current, snapshotDir); err != nil {
				return err
			}
			return errors.New("The database contains application tables but reports schema version 0. A private pre-migration snapshot was saved; no schema changes were made. Restore the correct schema version or recover the database before starting NeoCloud.")
		}
	}
	if current > 0 && current < Version {
		if err := snapshotBeforeMigration(ctx, db.W, current, snapshotDir); err != nil {
			return err
		}
	}
	if err := dbx.Migrate(ctx, db.W, Steps()); err != nil {
		return err
	}
	return nil
}

func snapshotBeforeMigration(ctx context.Context, db *sql.DB, current int, snapshotDir string) error {
	if err := runtimecfg.EnsurePrivateDir(snapshotDir); err != nil {
		return fmt.Errorf("Could not copy the database to %s before upgrading its schema (%v). Nothing was changed; free disk space and start again.", snapshotDir, err)
	}
	stamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	path := filepath.Join(snapshotDir, fmt.Sprintf("pre-migration-v%d-%s.db", current, stamp))
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("Could not copy the database to %s before upgrading its schema (%v). Nothing was changed; free disk space and start again.", snapshotDir, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("Could not secure the database snapshot in %s (%v). Nothing was changed; restore directory permissions and start again.", snapshotDir, err)
	}
	if err := pruneMigrationSnapshots(snapshotDir); err != nil {
		return fmt.Errorf("schema: remove old migration snapshots: %w", err)
	}
	return nil
}

func pruneMigrationSnapshots(snapshotDir string) error {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return err
	}
	type snapshot struct {
		name    string
		modTime time.Time
	}
	var snapshots []snapshot
	for _, entry := range entries {
		if entry.IsDir() || !migrationSnapshotName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		snapshots = append(snapshots, snapshot{name: entry.Name(), modTime: info.ModTime()})
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].modTime.Equal(snapshots[j].modTime) {
			return snapshots[i].name > snapshots[j].name
		}
		return snapshots[i].modTime.After(snapshots[j].modTime)
	})
	if len(snapshots) > migrationSnapshotKeep {
		for _, stale := range snapshots[migrationSnapshotKeep:] {
			if err := os.Remove(filepath.Join(snapshotDir, stale.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
