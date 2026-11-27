package backups

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

// Kinds of encrypted database-only snapshots kept in the backup directory.
const (
	SnapshotDaily     = "daily"
	SnapshotPreUpdate = "pre-update"
)

var snapshotRetention = map[string]int{SnapshotDaily: 14, SnapshotPreUpdate: 5}

const localSnapshotSuffix = ".db.enc"

// LocalSnapshot is one encrypted copy of the database, without file contents.
// Size is the size of the database it holds.
type LocalSnapshot struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	SchemaVersion int    `json:"schemaVersion"`
}

// CreateLocalSnapshot saves a consistent, checked and encrypted copy of the
// database and drops the oldest ones of its kind.
func (r *Runner) CreateLocalSnapshot(ctx context.Context, kind string, now time.Time) (LocalSnapshot, error) {
	var snapshot LocalSnapshot
	retention, known := snapshotRetention[kind]
	if !known {
		return snapshot, fmt.Errorf("unknown local snapshot kind %q", kind)
	}
	if err := os.MkdirAll(r.backupDir, 0o700); err != nil {
		return snapshot, err
	}
	if err := os.MkdirAll(r.tmpRoot, 0o700); err != nil {
		return snapshot, err
	}
	dir, err := os.MkdirTemp(r.tmpRoot, "snapshot-")
	if err != nil {
		return snapshot, err
	}
	defer os.RemoveAll(dir)
	plain := filepath.Join(dir, "neocloud.db")
	if err := r.copyDatabase(ctx, plain); err != nil {
		return snapshot, err
	}
	content, err := readSnapshot(plain)
	if err != nil {
		return snapshot, err
	}
	base := kind + "-" + now.UTC().Format("2006-01-02T15-04-05Z")
	destination := filepath.Join(r.backupDir, base+localSnapshotSuffix)
	key, err := r.keys.BackupKey()
	if err != nil {
		return snapshot, err
	}
	if err := encryptFile(ctx, destination, plain, key, "local:"+base); err != nil {
		return snapshot, err
	}
	if err := pruneLocalSnapshots(r.backupDir, kind, retention); err != nil {
		return snapshot, err
	}
	return LocalSnapshot{Path: destination, Size: content.size, SchemaVersion: content.schema}, nil
}

func pruneLocalSnapshots(root, kind string, retention int) error {
	matches, err := filepath.Glob(filepath.Join(root, kind+"-*"+localSnapshotSuffix))
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, stale := range matches[min(retention, len(matches)):] {
		if err := os.Remove(stale); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// RestoreLocalSnapshot puts a local snapshot back in place of the database of a
// stopped server, and returns where the replaced database was kept.
func RestoreLocalSnapshot(ctx context.Context, options RestoreOptions, snapshot LocalSnapshot) (string, error) {
	if options.DatabaseFile == "" || options.PIDFile == "" || options.Keys == nil {
		return "", errors.New("restore requires the database and PID paths and the master key")
	}
	lock, err := acquireRestoreLock(options.PIDFile)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := assertRestoreServerStopped(ctx, options); err != nil {
		return "", err
	}
	source, err := os.Open(snapshot.Path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	key, err := options.Keys.BackupKey()
	if err != nil {
		return "", err
	}
	staged, err := os.CreateTemp(filepath.Dir(options.DatabaseFile), ".neocloud-restore-*.db")
	if err != nil {
		return "", err
	}
	defer os.Remove(staged.Name())
	identity := "local:" + strings.TrimSuffix(filepath.Base(snapshot.Path), localSnapshotSuffix)
	_, err = cryptox.DecryptBlobRange(source, snapshot.Size, 0, snapshot.Size-1, key, identity, staged)
	if err == nil {
		err = staged.Sync()
	}
	if closeErr := staged.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("the database snapshot could not be decrypted: %w", err)
	}
	if _, err := readSnapshot(staged.Name()); err != nil {
		return "", fmt.Errorf("the database snapshot failed validation: %w", err)
	}
	previous, err := preserveDatabase(options.DatabaseFile, restoreTimestamp(options.Now))
	if err != nil {
		return "", err
	}
	if err := os.Rename(staged.Name(), options.DatabaseFile); err != nil {
		return previous, err
	}
	return previous, syncDirectory(filepath.Dir(options.DatabaseFile))
}

// StartManual makes the runner accept runs without starting its schedule, for a
// command-line run next to or instead of the server.
func (r *Runner) StartManual(parent context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel == nil {
		r.ctx, r.cancel = context.WithCancel(parent)
	}
}

// Wait blocks until every started run has finished.
func (r *Runner) Wait() { r.wg.Wait() }
