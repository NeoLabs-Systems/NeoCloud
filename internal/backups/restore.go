package backups

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	_ "modernc.org/sqlite"
)

// RestoreOptions describes a stopped NeoCloud installation and one completed
// local backup target. Restore never opens the live server application.
type RestoreOptions struct {
	DatabaseFile string
	BlobDir      string
	PIDFile      string
	BackupRoot   string
	Target       Target
	Snapshot     string
	Tier         string
	Port         int
	Keys         *cryptox.Keyring
	HTTPClient   *http.Client
	Now          func() time.Time
}

type RestoreResult struct {
	Snapshot      string
	Previous      string
	BlobsRestored int
	SchemaVersion int
	DatabaseSize  int64
	Waiting       bool
	RestoreTier   string
	Requested     int
	Pending       int
	Archived      int
}

type restoreBlob struct {
	entry       string
	source      string
	root        string
	destination string
	staged      string
}

// RestoreSnapshot replaces the database from a completed local backup. It
// refuses a live server, validates the manifest/database/blob set, stages all
// data first, and keeps the previous database (including its WAL/SHM sidecars)
// before the atomic database rename.
func RestoreSnapshot(ctx context.Context, options RestoreOptions) (RestoreResult, error) {
	var result RestoreResult
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Target != nil {
		return restoreFromTarget(ctx, options)
	}
	if options.DatabaseFile == "" || options.BlobDir == "" || options.PIDFile == "" || options.BackupRoot == "" || options.Snapshot == "" || options.Keys == nil {
		return result, errors.New("restore requires database, blob, PID, backup paths, snapshot name, and master key")
	}
	if !safeSnapshotName(options.Snapshot) {
		return result, errors.New("invalid snapshot name")
	}
	if err := os.MkdirAll(filepath.Dir(options.DatabaseFile), 0700); err != nil {
		return result, err
	}
	lock, err := acquireRestoreLock(options.PIDFile)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if err = assertRestoreServerStopped(ctx, options); err != nil {
		return result, err
	}

	root, err := filepath.Abs(options.BackupRoot)
	if err != nil {
		return result, err
	}
	snapshotDir := filepath.Join(root, "snapshots", options.Snapshot)
	manifestPath := filepath.Join(snapshotDir, "manifest.json")
	manifestFile, err := openRegularUnderRoot(root, manifestPath)
	if err != nil {
		return result, fmt.Errorf("read snapshot manifest: %w", err)
	}
	manifestBytes, err := readBoundedFromFile(manifestFile, maxBackupManifestBytes)
	closeManifestErr := manifestFile.Close()
	if err == nil {
		err = closeManifestErr
	}
	if err != nil {
		return result, fmt.Errorf("read snapshot manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil {
		return result, errors.New("snapshot manifest is invalid")
	}
	if manifest.App != "NeoCloud" || manifest.Format != 2 || manifest.DatabaseSize < 1 || manifest.Blobs == nil || manifest.Schema < 1 {
		return result, errors.New("snapshot manifest is incomplete or unsupported")
	}
	if manifest.Schema > schema.Version {
		return result, fmt.Errorf("snapshot schema %d is newer than this NeoCloud build supports (maximum %d)", manifest.Schema, schema.Version)
	}

	stage, err := os.CreateTemp(filepath.Dir(options.DatabaseFile), ".neocloud-restore-*.db")
	if err != nil {
		return result, err
	}
	stagePath := stage.Name()
	if err = stage.Chmod(0600); err == nil {
		err = stage.Close()
	} else {
		_ = stage.Close()
	}
	if err != nil {
		_ = os.Remove(stagePath)
		return result, err
	}
	defer os.Remove(stagePath)

	backupKey, err := options.Keys.BackupKey()
	if err != nil {
		return result, err
	}
	encryptedPath := filepath.Join(snapshotDir, "neocloud.db.enc")
	encrypted, err := openRegularUnderRoot(root, encryptedPath)
	if err != nil {
		return result, fmt.Errorf("open encrypted database: %w", err)
	}
	expectedEncryptedSize, err := cryptox.EncryptedSize(manifest.DatabaseSize)
	if err != nil {
		encrypted.Close()
		return result, err
	}
	encryptedInfo, err := encrypted.Stat()
	if err != nil || encryptedInfo.Size() != expectedEncryptedSize {
		encrypted.Close()
		return result, errors.New("encrypted snapshot database has an unexpected size")
	}
	plain, err := os.OpenFile(stagePath, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		encrypted.Close()
		return result, err
	}
	_, decryptErr := cryptox.DecryptBlobRange(encrypted, manifest.DatabaseSize, 0, manifest.DatabaseSize-1, backupKey, "snapshot:"+options.Snapshot, plain)
	closeEncryptedErr := encrypted.Close()
	if decryptErr == nil {
		decryptErr = plain.Sync()
	}
	closePlainErr := plain.Close()
	if decryptErr == nil {
		decryptErr = closeEncryptedErr
	}
	if decryptErr == nil {
		decryptErr = closePlainErr
	}
	if decryptErr != nil {
		return result, errors.New("snapshot database could not be decrypted; verify that NEOCLOUD_MASTER_KEY matches the backup")
	}

	content, err := readSnapshot(stagePath)
	if err != nil {
		return result, fmt.Errorf("snapshot database failed validation: %w", err)
	}
	if content.schema != manifest.Schema || content.size != manifest.DatabaseSize {
		return result, errors.New("snapshot manifest does not match its database")
	}
	entries := make([]string, 0, len(content.blobs))
	for _, blob := range content.blobs {
		entries = append(entries, strconv.FormatInt(blob.UserID, 10)+"/"+blob.ID)
	}
	if !reflect.DeepEqual(entries, manifest.Blobs) {
		return result, errors.New("snapshot manifest blob list does not match its database")
	}

	blobSizes, err := readSnapshotBlobSizes(stagePath)
	if err != nil {
		return result, err
	}
	needed, err := planRestoreBlobs(root, options.BlobDir, content.blobs, blobSizes)
	if err != nil {
		return result, err
	}
	stamp := restoreTimestamp(options.Now)
	if err = stageRestoreBlobs(ctx, needed, stamp); err != nil {
		return result, err
	}
	defer cleanupStagedBlobs(needed)
	previous, err := preserveDatabase(options.DatabaseFile, stamp)
	if err != nil {
		if previous != "" {
			return result, fmt.Errorf("preserve current database before restore (partial backup at %s): %w", previous, err)
		}
		return result, fmt.Errorf("preserve current database before restore: %w", err)
	}
	if err = commitRestoreBlobs(needed, stamp); err != nil {
		if previous != "" {
			return result, fmt.Errorf("restore blob files; previous database is preserved at %s: %w", previous, err)
		}
		return result, err
	}
	if err = os.Rename(stagePath, options.DatabaseFile); err != nil {
		return result, fmt.Errorf("replace database; previous database remains at %s: %w", previous, err)
	}
	if err = syncDirectory(filepath.Dir(options.DatabaseFile)); err != nil {
		return result, fmt.Errorf("database was replaced but parent directory sync failed (previous database: %s): %w", previous, err)
	}
	result = RestoreResult{Snapshot: options.Snapshot, Previous: previous, BlobsRestored: len(needed), SchemaVersion: content.schema, DatabaseSize: content.size}
	return result, nil
}

func restoreTimestamp(now func() time.Time) string {
	if now == nil {
		now = time.Now
	}
	return now().UTC().Format("20060102T150405.000000000Z")
}

func acquireRestoreLock(pidFile string) (*os.File, error) {
	file, err := os.OpenFile(pidFile+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("NeoCloud server or another restore operation holds the runtime lock")
	}
	return file, nil
}

func assertRestoreServerStopped(ctx context.Context, options RestoreOptions) error {
	if raw, err := os.ReadFile(options.PIDFile); err == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if parseErr == nil && pid > 0 && processIsAlive(pid) {
			return errors.New("NeoCloud is still running according to its PID file; stop it before restoring")
		}
	}
	port := options.Port
	if port == 0 {
		port = 3344
	}
	dialer := net.Dialer{Timeout: 750 * time.Millisecond}
	connection, dialErr := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if dialErr == nil {
		connection.Close()
		return errors.New("a service is listening on NeoCloud's configured port; stop it before restoring")
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 1500 * time.Millisecond}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/health", port), nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err == nil {
		response.Body.Close()
		return errors.New("NeoCloud is responding to its health endpoint; stop it before restoring")
	}
	return nil
}

func processIsAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func readBoundedFromFile(file *os.File, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file exceeds size limit")
	}
	return data, nil
}

func openRegularUnderRoot(root, path string) (*os.File, error) {
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(rootResolved, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("backup object escapes the selected backup directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("backup object is not a regular file")
	}
	return os.Open(path)
}

func readSnapshotBlobSizes(databasePath string) (map[string]int64, error) {
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id,user_id,size FROM blobs ORDER BY id")
	if err != nil {
		return nil, err
	}
	sizes := make(map[string]int64)
	for rows.Next() {
		var id string
		var userID, size int64
		if err = rows.Scan(&id, &userID, &size); err != nil {
			rows.Close()
			return nil, err
		}
		sizes[strconv.FormatInt(userID, 10)+"/"+id] = size
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	return sizes, nil
}

func planRestoreBlobs(backupRoot, blobRoot string, blobs []Blob, sizes map[string]int64) ([]restoreBlob, error) {
	if err := os.MkdirAll(blobRoot, 0700); err != nil {
		return nil, err
	}
	resolvedBlobRoot, err := filepath.EvalSymlinks(blobRoot)
	if err != nil {
		return nil, err
	}
	blobRootAbsolute, err := filepath.Abs(blobRoot)
	if err != nil {
		return nil, err
	}
	needed := make([]restoreBlob, 0)
	for _, blob := range blobs {
		uid := strconv.FormatInt(blob.UserID, 10)
		entry := uid + "/" + blob.ID
		backupPath := filepath.Join(backupRoot, "blobs", uid, blob.ID)
		localPath := localBlobPath(blobRoot, blob)
		relative, relErr := filepath.Rel(blobRootAbsolute, localPath)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("blob destination escapes the configured storage directory")
		}
		localPath = filepath.Join(resolvedBlobRoot, relative)
		if err := ensureRestoreBlobDirectory(resolvedBlobRoot, filepath.Dir(localPath)); err != nil {
			return nil, err
		}
		size, ok := sizes[entry]
		if !ok {
			return nil, fmt.Errorf("blob %s is missing from the restored database size index", entry)
		}
		expected, err := cryptox.EncryptedSize(size)
		if err != nil {
			return nil, err
		}
		if regularFileHasSize(localPath, expected) {
			continue
		}
		file, err := openRegularUnderRoot(backupRoot, backupPath)
		if err != nil {
			return nil, fmt.Errorf("required blob %s is missing from both local storage and the snapshot", entry)
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil {
			return nil, statErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if info.Size() != expected {
			return nil, fmt.Errorf("required blob %s has an unexpected encrypted size", entry)
		}
		needed = append(needed, restoreBlob{entry: entry, source: backupPath, root: resolvedBlobRoot, destination: localPath})
	}
	return needed, nil
}

func regularFileHasSize(path string, size int64) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() == size
}

func stageRestoreBlobs(ctx context.Context, blobs []restoreBlob, stamp string) error {
	for i := range blobs {
		item := &blobs[i]
		if err := ensureRestoreBlobDirectory(item.root, filepath.Dir(item.destination)); err != nil {
			cleanupStagedBlobs(blobs)
			return err
		}
		item.staged = item.destination + ".restore-" + stamp + ".part"
		if err := copyDurable(ctx, item.source, item.staged); err != nil {
			cleanupStagedBlobs(blobs)
			return err
		}
	}
	return nil
}

func ensureRestoreBlobDirectory(root, directory string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("blob storage root is not a regular directory")
	}
	relative, err := filepath.Rel(absolute, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("blob destination escapes the configured storage directory")
	}
	current := absolute
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if err = os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("blob destination path contains a symlink or non-directory")
		}
	}
	return nil
}

func cleanupStagedBlobs(blobs []restoreBlob) {
	for _, item := range blobs {
		if item.staged != "" {
			_ = os.Remove(item.staged)
		}
	}
}

func commitRestoreBlobs(blobs []restoreBlob, stamp string) error {
	for i := range blobs {
		item := &blobs[i]
		if info, err := os.Lstat(item.destination); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("local blob destination %s is not a regular file", item.entry)
			}
			backup := item.destination + ".before-restore-" + stamp
			if err = copyFileDurable(item.destination, backup); err != nil {
				return err
			}
		}
		if err := os.Rename(item.staged, item.destination); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(item.destination)); err != nil {
			return err
		}
	}
	return nil
}

func preserveDatabase(databaseFile, stamp string) (string, error) {
	if _, err := os.Lstat(databaseFile); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	info, err := os.Lstat(databaseFile)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("database path is not a regular file")
	}
	previous := databaseFile + ".before-restore-" + stamp
	if err = copyFileDurable(databaseFile, previous); err != nil {
		return "", err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, statErr := os.Lstat(databaseFile + suffix); statErr == nil {
			if err = copyFileDurable(databaseFile+suffix, previous+suffix); err != nil {
				return previous, err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return previous, statErr
		}
	}
	// The server has been verified stopped. Checkpoint before sidecars are
	// removed so a failed rename cannot strand committed writes in the WAL.
	db, err := sql.Open("sqlite", databaseFile)
	if err != nil {
		return "", err
	}
	var busy, logFrames, checkpointed int
	checkpointErr := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	closeErr := db.Close()
	if checkpointErr != nil {
		return previous, checkpointErr
	}
	if closeErr != nil {
		return previous, closeErr
	}
	if busy != 0 {
		return previous, errors.New("current database WAL is busy; restore was not applied")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err = os.Remove(databaseFile + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return previous, err
		}
	}
	return previous, nil
}

func copyFileDurable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	return syncDirectory(filepath.Dir(destination))
}

// Keep the sql import used by database-preservation verification explicit.
var _ = sql.ErrNoRows
