// Package state is the client's memory of the last synced state, one row
// per path per pair (docs/clients.md "Client state"). It is the "base" of
// the three-way comparison: without it the engine could not tell a local
// delete from a remote create.
package state

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver, so the binary needs no C SQLite
)

// Entry is what both sides looked like after the last successful sync of a path.
type Entry struct {
	Path       string // relative, "/"-separated
	NodeID     string
	Type       string // file or folder
	Etag       string // remote content hash
	Size       int64  // remote size
	LocalMtime int64  // UnixNano
	LocalSize  int64
	LocalHash  string
	State      string // synced
}

// Synced is the only row state the lite client writes; the column exists
// for the desktop app's online-only placeholders.
const Synced = "synced"

// DB is the state database.
type DB struct {
	db  *sql.DB
	dir string
}

const schema = `
CREATE TABLE IF NOT EXISTS pairs (
  pair_id TEXT PRIMARY KEY,
  cursor INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS entries (
  pair_id TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  node_id TEXT NOT NULL,
  type TEXT NOT NULL,
  etag TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  local_mtime INTEGER NOT NULL DEFAULT 0,
  local_size INTEGER NOT NULL DEFAULT 0,
  local_hash TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'synced',
  PRIMARY KEY (pair_id, relative_path)
);
CREATE TABLE IF NOT EXISTS meta (
  pair_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (pair_id, key)
);
CREATE TABLE IF NOT EXISTS uploads (
  pair_id TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  upload_id TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  size INTEGER NOT NULL,
  PRIMARY KEY (pair_id, relative_path)
);
`

// Open opens (and creates) state.db in dir.
func Open(dir string) (*DB, error) {
	path := filepath.Join(dir, "state.db")
	// WAL lets the settings page read status while the engine writes;
	// busy_timeout covers the rare overlap instead of failing.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("state schema: %w", err)
	}
	return &DB{db: db, dir: dir}, nil
}

// Close closes the database.
func (s *DB) Close() error { return s.db.Close() }

// Dir is the folder holding state.db (and the per-pair lock files).
func (s *DB) Dir() string { return s.dir }

// Meta reads one per-pair value ("" when unset).
func (s *DB) Meta(pairID, key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE pair_id = ? AND key = ?`, pairID, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// SetMeta stores one per-pair value; "" removes it.
func (s *DB) SetMeta(pairID, key, value string) error {
	if value == "" {
		_, err := s.db.Exec(`DELETE FROM meta WHERE pair_id = ? AND key = ?`, pairID, key)
		return err
	}
	_, err := s.db.Exec(`
		INSERT INTO meta (pair_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(pair_id, key) DO UPDATE SET value = excluded.value`, pairID, key, value)
	return err
}

// ClearEntries drops a pair's rows and pending uploads but keeps its
// meta values: used when the rows stop describing the pair (its remote
// folder was replaced), so the next pass merges from scratch.
func (s *DB) ClearEntries(pairID string) error {
	for _, table := range []string{"entries", "uploads"} {
		if _, err := s.db.Exec(`DELETE FROM `+table+` WHERE pair_id = ?`, pairID); err != nil {
			return err
		}
	}
	return nil
}

// Entries returns every row of a pair keyed by relative path.
func (s *DB) Entries(pairID string) (map[string]Entry, error) {
	rows, err := s.db.Query(`
		SELECT relative_path, node_id, type, etag, size, local_mtime, local_size, local_hash, state
		FROM entries WHERE pair_id = ?`, pairID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Path, &e.NodeID, &e.Type, &e.Etag, &e.Size, &e.LocalMtime, &e.LocalSize, &e.LocalHash, &e.State); err != nil {
			return nil, err
		}
		out[e.Path] = e
	}
	return out, rows.Err()
}

// Put inserts or replaces a row.
func (s *DB) Put(pairID string, e Entry) error {
	if e.State == "" {
		e.State = Synced
	}
	_, err := s.db.Exec(`
		INSERT INTO entries (pair_id, relative_path, node_id, type, etag, size, local_mtime, local_size, local_hash, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pair_id, relative_path) DO UPDATE SET
		  node_id = excluded.node_id, type = excluded.type, etag = excluded.etag, size = excluded.size,
		  local_mtime = excluded.local_mtime, local_size = excluded.local_size,
		  local_hash = excluded.local_hash, state = excluded.state`,
		pairID, e.Path, e.NodeID, e.Type, e.Etag, e.Size, e.LocalMtime, e.LocalSize, e.LocalHash, e.State)
	return err
}

// Delete removes a row.
func (s *DB) Delete(pairID, path string) error {
	_, err := s.db.Exec(`DELETE FROM entries WHERE pair_id = ? AND relative_path = ?`, pairID, path)
	return err
}

// Move re-keys a row and, for a folder, every row below it, in one
// transaction so a crash never leaves half a folder at the old path.
// Matching happens in Go: SQLite's substr counts characters, not bytes.
func (s *DB) Move(pairID, from, to string) error {
	entries, err := s.Entries(pairID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for path := range entries {
		// Whatever was recorded at the destination is replaced.
		if path == to || strings.HasPrefix(path, to+"/") {
			if _, err := tx.Exec(`DELETE FROM entries WHERE pair_id = ? AND relative_path = ?`, pairID, path); err != nil {
				return err
			}
		}
	}
	for path := range entries {
		if path != from && !strings.HasPrefix(path, from+"/") {
			continue
		}
		moved := to + strings.TrimPrefix(path, from)
		if _, err := tx.Exec(`UPDATE entries SET relative_path = ? WHERE pair_id = ? AND relative_path = ?`,
			moved, pairID, path); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Cursor is the journal position the pair has caught up to.
func (s *DB) Cursor(pairID string) (int64, error) {
	var cursor int64
	err := s.db.QueryRow(`SELECT cursor FROM pairs WHERE pair_id = ?`, pairID).Scan(&cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return cursor, err
}

// SetCursor stores the pair's journal position.
func (s *DB) SetCursor(pairID string, cursor int64) error {
	_, err := s.db.Exec(`
		INSERT INTO pairs (pair_id, cursor) VALUES (?, ?)
		ON CONFLICT(pair_id) DO UPDATE SET cursor = excluded.cursor`, pairID, cursor)
	return err
}

// Upload is an unfinished resumable upload that can be continued.
type Upload struct {
	ID     string
	SHA256 string
	Size   int64
}

// PendingUpload returns the unfinished upload recorded for a path, if any.
func (s *DB) PendingUpload(pairID, path string) (Upload, bool, error) {
	var u Upload
	err := s.db.QueryRow(`SELECT upload_id, sha256, size FROM uploads WHERE pair_id = ? AND relative_path = ?`,
		pairID, path).Scan(&u.ID, &u.SHA256, &u.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return Upload{}, false, nil
	}
	return u, err == nil, err
}

// SetPendingUpload remembers a resumable upload in progress.
func (s *DB) SetPendingUpload(pairID, path string, u Upload) error {
	_, err := s.db.Exec(`
		INSERT INTO uploads (pair_id, relative_path, upload_id, sha256, size) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(pair_id, relative_path) DO UPDATE SET
		  upload_id = excluded.upload_id, sha256 = excluded.sha256, size = excluded.size`,
		pairID, path, u.ID, u.SHA256, u.Size)
	return err
}

// ClearPendingUpload forgets a finished or abandoned upload.
func (s *DB) ClearPendingUpload(pairID, path string) error {
	_, err := s.db.Exec(`DELETE FROM uploads WHERE pair_id = ? AND relative_path = ?`, pairID, path)
	return err
}

// ForgetPair drops everything known about a pair (removed, or the account changed).
func (s *DB) ForgetPair(pairID string) error {
	for _, table := range []string{"entries", "uploads", "pairs", "meta"} {
		if _, err := s.db.Exec(`DELETE FROM `+table+` WHERE pair_id = ?`, pairID); err != nil {
			return err
		}
	}
	return nil
}

// Reset forgets every pair. Used on logout: rows describe one account's
// files, and comparing them against another account would read as mass
// deletions.
func (s *DB) Reset() error {
	_, err := s.db.Exec(`DELETE FROM entries; DELETE FROM uploads; DELETE FROM pairs; DELETE FROM meta;`)
	return err
}
