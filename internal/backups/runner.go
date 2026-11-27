// Package backups runs crash-safe local snapshots in the Node-compatible
// encrypted backup layout. Remote adapters are added independently.
package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	_ "modernc.org/sqlite"
)

var (
	ErrAlreadyRunning = errors.New("a backup to this target is already running")
	ErrNotStarted     = errors.New("backup worker is not running")
)

var backupUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var backupUserIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

const maxBackupManifestBytes = 64 << 20

type Runner struct {
	db        *sql.DB
	keys      *cryptox.Keyring
	blobRoot  string
	tmpRoot   string
	backupDir string
	version   string

	mu                sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	active            map[int64]bool
	unsupportedLogged map[int64]bool
	wg                sync.WaitGroup
}

type Blob struct {
	ID     string `json:"id"`
	UserID int64  `json:"user_id"`
}

type Manifest struct {
	Format       int      `json:"format"`
	App          string   `json:"app"`
	Version      string   `json:"version"`
	Schema       int      `json:"schemaVersion"`
	CreatedAt    string   `json:"createdAt"`
	DatabaseSize int64    `json:"databaseSize"`
	Blobs        []string `json:"blobs"`
	MissingBlobs int      `json:"missingBlobs"`
}

func New(db *dbx.DB, keys *cryptox.Keyring, blobRoot, tmpRoot, backupDir, version string) (*Runner, error) {
	if db == nil || db.W == nil || keys == nil || blobRoot == "" || tmpRoot == "" || backupDir == "" {
		return nil, errors.New("backups: database, keyring and storage paths are required")
	}
	if version == "" {
		version = "development"
	}
	return &Runner{db: db.W, keys: keys, blobRoot: blobRoot, tmpRoot: tmpRoot, backupDir: backupDir, version: version, active: map[int64]bool{}, unsupportedLogged: map[int64]bool{}}, nil
}

func (r *Runner) Start(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	r.ctx, r.cancel = context.WithCancel(parent)
	ctx := r.ctx
	_, _ = r.db.ExecContext(r.ctx, `UPDATE backup_runs SET status='failed',finished_at=datetime('now'),error='interrupted'
		WHERE status='running' AND datetime(COALESCE(heartbeat_at,started_at))<=datetime('now','-5 minutes')`)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.schedule(ctx)
	}()
}

func (r *Runner) Close() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
	r.mu.Lock()
	r.cancel = nil
	r.ctx = nil
	r.mu.Unlock()
}

func (r *Runner) StartRun(targetID int64, destination string, retention int) (int64, error) {
	target, err := NewLocalTarget(destination)
	if err != nil || targetID < 1 || retention < 1 || retention > 10000 {
		return 0, errors.New("invalid local backup target")
	}
	return r.startTargetRun(targetID, target, retention)
}

// RunTarget starts a snapshot against the configured local or WebDAV backup
// target. Remote target credentials must be supplied from the encrypted
// configuration store by the caller.
func (r *Runner) RunTarget(targetID int64, kind string, config map[string]any, retention int) (int64, error) {
	if targetID < 1 || retention < 1 || retention > 10000 {
		return 0, errors.New("invalid backup target")
	}
	target, err := NewTarget(kind, config)
	if err != nil {
		return 0, err
	}
	return r.startTargetRun(targetID, target, retention)
}

func (r *Runner) startTargetRun(targetID int64, target Target, retention int) (int64, error) {
	r.mu.Lock()
	if r.ctx == nil || r.ctx.Err() != nil {
		r.mu.Unlock()
		return 0, ErrNotStarted
	}
	if r.active[targetID] {
		r.mu.Unlock()
		return 0, ErrAlreadyRunning
	}
	ctx := r.ctx
	r.active[targetID] = true
	r.wg.Add(1)
	r.mu.Unlock()

	name := time.Now().UTC().Format("2006-01-02T15-04-05.000Z")
	tx, err := r.db.BeginTx(ctx, nil)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE backup_runs SET status='failed',finished_at=datetime('now'),error='interrupted'
			WHERE target_id=? AND status='running' AND datetime(COALESCE(heartbeat_at,started_at))<=datetime('now','-5 minutes')`, targetID)
	}
	if err == nil {
		var active int
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_runs WHERE target_id=? AND status='running'`, targetID).Scan(&active)
		if err == nil && active != 0 {
			err = ErrAlreadyRunning
		}
	}
	var runID int64
	if err == nil {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `INSERT INTO backup_runs(target_id,status,snapshot,heartbeat_at) VALUES(?,'running',?,datetime('now'))`, targetID, name)
		if err == nil {
			runID, err = result.LastInsertId()
		}
	}
	if err == nil {
		err = tx.Commit()
	} else if tx != nil {
		_ = tx.Rollback()
	}
	if err != nil {
		r.finishActive(targetID)
		r.wg.Done()
		return 0, err
	}
	go func() {
		defer r.wg.Done()
		defer r.finishActive(targetID)
		err := r.run(ctx, targetID, runID, name, target, retention)
		status, message := "ok", ""
		if err != nil {
			status, message = "failed", safeBackupError(err)
		}
		_, _ = r.db.ExecContext(context.Background(), `UPDATE backup_runs SET status=?,finished_at=datetime('now'),heartbeat_at=NULL,error=? WHERE id=? AND status='running'`, status, nullable(message), runID)
	}()
	return runID, nil
}

func (r *Runner) finishActive(targetID int64) {
	r.mu.Lock()
	delete(r.active, targetID)
	r.mu.Unlock()
}

func (r *Runner) run(ctx context.Context, targetID, runID int64, name string, target Target, retention int) (runErr error) {
	if err := target.Open(ctx); err != nil {
		return err
	}
	defer target.Close()
	completed := false
	defer func() {
		if !completed {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = target.DeletePrefix(cleanupCtx, "snapshots/"+name)
		}
	}()
	beatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-beatDone:
				return
			case <-ticker.C:
				_, _ = r.db.ExecContext(context.Background(), `UPDATE backup_runs SET heartbeat_at=datetime('now') WHERE id=? AND status='running'`, runID)
			}
		}
	}()
	defer close(beatDone)
	tmp, err := os.MkdirTemp(r.tmpRoot, "backup-"+strconv.FormatInt(targetID, 10)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	plainDB := filepath.Join(tmp, "neocloud.db")
	if err = r.copyDatabase(ctx, plainDB); err != nil {
		return err
	}
	content, err := readSnapshot(plainDB)
	if err != nil {
		return err
	}
	manifest := Manifest{Format: 2, App: "NeoCloud", Version: r.version, Schema: content.schema, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), DatabaseSize: content.size, Blobs: make([]string, 0, len(content.blobs))}
	var bytesTotal int64
	var blobsTotal int
	for _, blob := range content.blobs {
		remote := "blobs/" + strconv.FormatInt(blob.UserID, 10) + "/" + blob.ID
		exists, existsErr := target.Exists(ctx, remote)
		if existsErr != nil {
			return existsErr
		}
		if !exists {
			blobsTotal++
			bytesTotal += fileSize(localBlobPath(r.blobRoot, blob))
		}
	}
	if _, err = r.db.ExecContext(ctx, `UPDATE backup_runs SET blobs_total=?,bytes_total=? WHERE id=? AND status='running'`, blobsTotal, bytesTotal, runID); err != nil {
		return err
	}
	for _, blob := range content.blobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := strconv.FormatInt(blob.UserID, 10) + "/" + blob.ID
		manifest.Blobs = append(manifest.Blobs, entry)
		local := localBlobPath(r.blobRoot, blob)
		remote := "blobs/" + strconv.FormatInt(blob.UserID, 10) + "/" + blob.ID
		exists, existsErr := target.Exists(ctx, remote)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			continue
		}
		if err := target.Put(ctx, remote, local); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				manifest.MissingBlobs++
				continue
			}
			return err
		}
		if _, err := r.db.ExecContext(ctx, `UPDATE backup_runs SET blobs_uploaded=blobs_uploaded+1,bytes_uploaded=bytes_uploaded+? WHERE id=? AND status='running'`, fileSize(local), runID); err != nil {
			return err
		}
	}
	backupKey, err := r.keys.BackupKey()
	if err != nil {
		return err
	}
	snapshotDir := filepath.Join(tmp, "snapshot")
	if err = os.Mkdir(snapshotDir, 0700); err != nil {
		return err
	}
	encPath := filepath.Join(snapshotDir, "neocloud.db.enc")
	if err = encryptFile(ctx, encPath, plainDB, backupKey, "snapshot:"+name); err != nil {
		return err
	}
	if err = atomicJSON(filepath.Join(snapshotDir, "manifest.json"), manifest); err != nil {
		return err
	}
	if err = target.Put(ctx, "snapshots/"+name+"/neocloud.db.enc", encPath); err != nil {
		return err
	}
	if err = target.Put(ctx, "snapshots/"+name+"/manifest.json", filepath.Join(snapshotDir, "manifest.json")); err != nil {
		return err
	}
	completed = true
	if err = pruneTarget(ctx, target, retention); err != nil {
		return err
	}
	return nil
}

func (r *Runner) copyDatabase(ctx context.Context, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	quoted := strings.ReplaceAll(destination, "'", "''")
	if _, err := r.db.ExecContext(ctx, `VACUUM INTO '`+quoted+`'`); err != nil {
		return fmt.Errorf("create consistent database snapshot: %w", err)
	}
	if err := os.Chmod(destination, 0600); err != nil {
		return err
	}
	return nil
}

type snapshotContent struct {
	schema int
	size   int64
	blobs  []Blob
}

func readSnapshot(path string) (snapshotContent, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return snapshotContent{}, err
	}
	defer db.Close()
	var check, hasUsers string
	var schemaVersion int
	if err = db.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return snapshotContent{}, errors.New("backup database snapshot failed integrity check")
	}
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil || schemaVersion < 1 {
		return snapshotContent{}, errors.New("backup database snapshot has no supported schema version")
	}
	if err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&hasUsers); err != nil || hasUsers != "users" {
		return snapshotContent{}, errors.New("backup database snapshot is not a NeoCloud database")
	}
	rows, err := db.Query(`SELECT id,user_id FROM blobs ORDER BY id`)
	if err != nil {
		return snapshotContent{}, err
	}
	blobs := []Blob{}
	for rows.Next() {
		var blob Blob
		if err = rows.Scan(&blob.ID, &blob.UserID); err != nil {
			rows.Close()
			return snapshotContent{}, err
		}
		if blob.UserID < 1 || !backupUUIDPattern.MatchString(blob.ID) {
			rows.Close()
			return snapshotContent{}, errors.New("backup database contains an invalid blob reference")
		}
		blobs = append(blobs, blob)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshotContent{}, err
	}
	rows.Close()
	info, err := os.Stat(path)
	if err != nil {
		return snapshotContent{}, err
	}
	return snapshotContent{schema: schemaVersion, size: info.Size(), blobs: blobs}, nil
}

func localBlobPath(root string, blob Blob) string {
	return filepath.Join(root, strconv.FormatInt(blob.UserID, 10), blob.ID[:2], blob.ID)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func copyDurable(ctx context.Context, source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(destination)
		}
	}()
	buf := make([]byte, 256*1024)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, err = out.Write(buf[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func encryptFile(ctx context.Context, destination, source string, key []byte, blobID string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := destination + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, encErr := cryptox.EncryptBlob(out, contextReader{ctx: ctx, reader: in}, key, blobID)
	if encErr == nil {
		encErr = out.Sync()
	}
	closeErr := out.Close()
	if encErr == nil {
		encErr = closeErr
	}
	if encErr != nil {
		_ = os.Remove(tmp)
		return encErr
	}
	if err = os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".part"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func safeSnapshotName(name string) bool {
	if len(name) < 2 || len(name) > 100 || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return name[0] >= 'A' && name[0] <= 'Z' || name[0] >= 'a' && name[0] <= 'z' || name[0] >= '0' && name[0] <= '9'
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func safeBackupError(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "interrupted"
	}
	return "backup failed; check server logs and target availability"
}
