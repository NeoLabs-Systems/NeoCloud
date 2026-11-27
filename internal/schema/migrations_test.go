package schema

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestFreshDatabaseAppliesNodeSchema(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := dbx.Migrate(ctx, db.W, Steps()); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != Version {
		t.Fatalf("schema version = %d, expected Go/Node version %d", version, Version)
	}
	for _, table := range []string{"users", "nodes", "device_tokens", "oauth_clients", "external_mounts", "handoff_offers", "handoff_files", "wopi_locks", "maintenance_state", "blob_scrub_damaged", "webdav_locks"} {
		var found int
		if err := db.W.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found != 1 {
			t.Errorf("expected table %q", table)
		}
	}
	var hasAffectedParents int
	if err := db.W.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('changes') WHERE name='parent_ids_json'`).Scan(&hasAffectedParents); err != nil || hasAffectedParents != 1 {
		t.Fatalf("change parent column count = %d, error = %v", hasAffectedParents, err)
	}
	rows, err := db.W.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID, parent sql.NullInt64
		var fkID int
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("foreign key check reports %s row %v parent %v fk %d", table, rowID, parent, fkID)
	}
}

func TestVersion36DatabaseReceivesPersistentWebDAVLocks(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL);
		INSERT INTO users(id,username) VALUES(1,'dav-user'); PRAGMA user_version=36`); err != nil {
		t.Fatal(err)
	}
	if err := dbx.Migrate(ctx, db.W, Steps()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.ExecContext(ctx, `INSERT INTO webdav_locks
		(token,user_id,key,href,depth,scope,owner,timeout_ms,expires_at)
		VALUES('opaquelocktoken:test',1,'folder','/dav/folder/','infinity','exclusive','owner',60000,? )`, time.Now().Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.ExecContext(ctx, `DELETE FROM users WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var locks int
	if err := db.W.QueryRowContext(ctx, `SELECT count(*) FROM webdav_locks`).Scan(&locks); err != nil || locks != 0 {
		t.Fatalf("account deletion leaves %d WebDAV locks, error=%v", locks, err)
	}
	var version int
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != Version {
		t.Fatalf("schema version=%d, want %d, error=%v", version, Version, err)
	}
}

func TestExistingNodeDatabaseReceivesOnlyPendingMigrations(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL); CREATE TABLE changes (seq INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, node_id TEXT NOT NULL, op TEXT NOT NULL, at TEXT NOT NULL DEFAULT (datetime('now'))); INSERT INTO users (id, username) VALUES (7, 'existing-account'); PRAGMA user_version = 31`); err != nil {
		t.Fatal(err)
	}
	if err := dbx.Migrate(ctx, db.W, Steps()); err != nil {
		t.Fatal(err)
	}
	var username string
	if err := db.W.QueryRowContext(ctx, `SELECT username FROM users WHERE id = 7`).Scan(&username); err != nil || username != "existing-account" {
		t.Fatalf("existing user = %q, error = %v", username, err)
	}
	var version int
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != Version {
		t.Fatalf("schema version = %d, error = %v", version, err)
	}
	var found int
	if err := db.W.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'wopi_locks'`).Scan(&found); err != nil || found != 1 {
		t.Fatalf("v33 WOPI lock table count = %d, error = %v", found, err)
	}
	if err := db.W.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'security_key_login_challenges'`).Scan(&found); err != nil || found != 1 {
		t.Fatalf("v35 security key challenge table count = %d, error = %v", found, err)
	}
}

func TestVersion35DatabaseReceivesBlobScrubMigration(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(root, "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	steps := Steps()
	if len(steps) < 2 {
		t.Fatalf("only %d migration steps", len(steps))
	}
	if err := dbx.Migrate(ctx, db.W, steps[:len(steps)-1]); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != Version-1 {
		t.Fatalf("pre-migration schema version=%d error=%v", version, err)
	}
	if err := Migrate(ctx, db, filepath.Join(root, "backups")); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"maintenance_state", "blob_scrub_damaged"} {
		var found int
		if err := db.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil || found != 1 {
			t.Errorf("table %s count=%d error=%v", table, found, err)
		}
	}
	if err := db.W.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != Version {
		t.Fatalf("post-migration schema version=%d error=%v", version, err)
	}
}

func TestNewerDatabaseVersionIsRejected(t *testing.T) {
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neocloud.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.W.ExecContext(ctx, `PRAGMA user_version = `+strconv.Itoa(Version+1)); err != nil {
		t.Fatal(err)
	}
	if err := dbx.Migrate(ctx, db.W, Steps()); err == nil || !strings.Contains(err.Error(), "newer than this build supports") {
		t.Fatalf("newer schema error = %v", err)
	}
}
