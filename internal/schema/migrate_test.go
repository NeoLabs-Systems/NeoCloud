package schema

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestMigrateSnapshotsExistingDatabaseBeforeChangingIt(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL); CREATE TABLE changes (seq INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, node_id TEXT NOT NULL, op TEXT NOT NULL, at TEXT NOT NULL DEFAULT (datetime('now'))); INSERT INTO users (id, username) VALUES (7, 'keep me'); PRAGMA user_version = 31`); err != nil {
		t.Fatal(err)
	}
	snapshotDir := filepath.Join(t.TempDir(), "backups")
	if err := Migrate(ctx, db, snapshotDir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(snapshotDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot entries = %v, error = %v", entries, err)
	}
	if !migrationSnapshotName.MatchString(entries[0].Name()) {
		t.Fatalf("unexpected snapshot name %q", entries[0].Name())
	}
	snapshotPath := filepath.Join(snapshotDir, entries[0].Name())
	info, err := os.Stat(snapshotPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, error = %v", info, err)
	}
	snapshot, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var version int
	var username string
	if err := snapshot.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.QueryRow(`SELECT username FROM users WHERE id = 7`).Scan(&username); err != nil {
		t.Fatal(err)
	}
	if version != 31 || username != "keep me" {
		t.Fatalf("snapshot schema=%d existing user=%q", version, username)
	}
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != Version {
		t.Fatalf("live database schema=%d, error=%v", version, err)
	}
}

func TestFailedPreMigrationSnapshotPreventsSchemaChange(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(root, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY); CREATE TABLE changes (seq INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, node_id TEXT NOT NULL, op TEXT NOT NULL, at TEXT NOT NULL DEFAULT (datetime('now'))); PRAGMA user_version = 31`); err != nil {
		t.Fatal(err)
	}
	badSnapshotDir := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(badSnapshotDir, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, badSnapshotDir); err == nil {
		t.Fatal("migrated without a snapshot")
	}
	var version int
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != 31 {
		t.Fatalf("schema changed after snapshot failure: version=%d error=%v", version, err)
	}
}

func TestMigrateSnapshotsAndRefusesUnversionedDatabaseWithApplicationTables(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(root, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL); INSERT INTO users(id,username) VALUES(9,'preserve');`); err != nil {
		t.Fatal(err)
	}
	snapshotDir := filepath.Join(root, "backups")
	if err := Migrate(ctx, db, snapshotDir); err == nil {
		t.Fatal("migrated an application database with user_version 0")
	}
	entries, err := os.ReadDir(snapshotDir)
	if err != nil || len(entries) != 1 || !migrationSnapshotName.MatchString(entries[0].Name()) {
		t.Fatalf("unversioned database snapshot entries=%v error=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode=%v error=%v", info, err)
	}
	var version int
	var username string
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != 0 {
		t.Fatalf("live schema version=%d error=%v", version, err)
	}
	if err := db.W.QueryRowContext(ctx, `SELECT username FROM users WHERE id=9`).Scan(&username); err != nil || username != "preserve" {
		t.Fatalf("live user=%q error=%v", username, err)
	}
}

func TestMigrationSnapshotRetentionKeepsNewestThree(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "pre-migration-v31-2026-01-01T00-00-0"+strconv.Itoa(i)+"Z.db")
		if err := os.WriteFile(name, []byte("db"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, base.Add(time.Duration(i)*time.Second), base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "other.db"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneMigrationSnapshots(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("kept %d files, want three snapshots and the unrelated file", len(entries))
	}
	for _, name := range []string{"pre-migration-v31-2026-01-01T00-00-00Z.db", "pre-migration-v31-2026-01-01T00-00-01Z.db"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("old snapshot %s was not removed", name)
		}
	}
}
