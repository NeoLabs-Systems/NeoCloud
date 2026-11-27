package manager

import (
	"context"
	"database/sql"
	"net/url"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/backups"
	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	_ "modernc.org/sqlite"
)

// openReadOnly opens the database without migrating or writing it, safe next to
// a running server of any version.
func openReadOnly(path string) (*sql.DB, error) {
	return sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String()+"?mode=ro&_pragma=busy_timeout(5000)")
}

func schemaVersion(ctx context.Context, path string) (int, error) {
	db, err := openReadOnly(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var version int
	err = db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	return version, err
}

// hasAccount reports whether anyone registered yet.
func (m *Manager) hasAccount(ctx context.Context) bool {
	db, err := openReadOnly(m.paths().DatabaseFile)
	if err != nil {
		return false
	}
	defer db.Close()
	var one int
	return db.QueryRowContext(ctx, `SELECT 1 FROM users LIMIT 1`).Scan(&one) == nil
}

// snapshotDatabase saves an encrypted copy of the database as it is, before
// anything migrates it.
func (m *Manager) snapshotDatabase(ctx context.Context, cfg config.Config) (backups.LocalSnapshot, error) {
	keys, err := cryptox.NewKeyring(cfg.MasterKeyHex)
	if err != nil {
		return backups.LocalSnapshot{}, err
	}
	db, err := dbx.Open(ctx, cfg.Paths.DatabaseFile, dbx.Options{})
	if err != nil {
		return backups.LocalSnapshot{}, err
	}
	defer db.Close()
	runner, err := backups.New(db, keys, cfg.Paths.BlobDir, cfg.Paths.UploadTmpDir, cfg.Paths.BackupDir, m.Version)
	if err != nil {
		return backups.LocalSnapshot{}, err
	}
	return runner.CreateLocalSnapshot(ctx, backups.SnapshotPreUpdate, time.Now())
}

// restoreDatabaseIfChanged puts a snapshot back when the schema moved on: an
// older server refuses a newer database. Otherwise the current data is kept.
func (m *Manager) restoreDatabaseIfChanged(ctx context.Context, cfg config.Config, snapshot backups.LocalSnapshot) error {
	current, err := schemaVersion(ctx, cfg.Paths.DatabaseFile)
	if err == nil && current == snapshot.SchemaVersion {
		return nil
	}
	keys, err := cryptox.NewKeyring(cfg.MasterKeyHex)
	if err != nil {
		return err
	}
	previous, err := backups.RestoreLocalSnapshot(ctx, backups.RestoreOptions{
		DatabaseFile: cfg.Paths.DatabaseFile, PIDFile: cfg.Paths.PIDFile, Port: cfg.Port, Keys: keys,
	}, snapshot)
	if err == nil && previous != "" {
		m.UI.Info("The database as the new version left it is at " + previous + ".")
	}
	return err
}
