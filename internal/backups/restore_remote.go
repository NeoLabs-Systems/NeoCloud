package backups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
)

type archiveObjectStat struct {
	StorageClass string
	Restore      string
	Exists       bool
}

type archiveAwareTarget interface {
	StatObject(context.Context, string) (archiveObjectStat, error)
	RequestObjectRestore(context.Context, string, string) error
}

type archiveRestoreCounts struct {
	Requested int
	Pending   int
	Archived  int
}

// restoreFromTarget first stages a checked mirror of just the snapshot and
// missing blobs into a private temporary directory. If S3 archive objects
// need time to hydrate, it returns without changing the local installation.
func restoreFromTarget(ctx context.Context, options RestoreOptions) (RestoreResult, error) {
	var result RestoreResult
	if options.DatabaseFile == "" || options.BlobDir == "" || options.PIDFile == "" || options.Snapshot == "" || options.Keys == nil {
		return result, errors.New("restore requires database, blob, PID paths, snapshot name, and master key")
	}
	if !safeSnapshotName(options.Snapshot) {
		return result, errors.New("invalid snapshot name")
	}
	if err := assertRestoreServerStopped(ctx, options); err != nil {
		return result, err
	}
	target := options.Target
	if err := target.Open(ctx); err != nil {
		return result, fmt.Errorf("open backup target: %w", err)
	}
	defer target.Close()

	workspace, err := os.MkdirTemp("", "neocloud-remote-restore-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(workspace)
	if err = os.Chmod(workspace, 0700); err != nil {
		return result, err
	}
	snapshotDir := filepath.Join(workspace, "snapshots", options.Snapshot)
	manifestPath := filepath.Join(snapshotDir, "manifest.json")
	databasePath := filepath.Join(snapshotDir, "neocloud.db.enc")
	if err = os.MkdirAll(snapshotDir, 0700); err != nil {
		return result, err
	}

	tier := options.Tier
	if tier == "" {
		tier = "Standard"
	}
	counts := archiveRestoreCounts{}
	manifestKey := "snapshots/" + options.Snapshot + "/manifest.json"
	databaseKey := "snapshots/" + options.Snapshot + "/neocloud.db.enc"
	manifestWake, err := wakeArchivedTargetObjects(ctx, target, []string{manifestKey}, tier)
	if err != nil {
		return result, err
	}
	addArchiveCounts(&counts, manifestWake)
	if manifestWake.Requested+manifestWake.Pending > 0 {
		// The blob list cannot be read until the manifest has hydrated. Wake
		// backup-owned blobs so the next attempt can proceed in one pass.
		objects, listErr := target.List(ctx, "blobs")
		if listErr != nil {
			return result, fmt.Errorf("list backup blobs for archive hydration: %w", listErr)
		}
		blobKeys := make([]string, 0, len(objects))
		for _, key := range objects {
			if strings.HasPrefix(key, "blobs/") {
				blobKeys = append(blobKeys, key)
			}
		}
		blobWake, wakeErr := wakeArchivedTargetObjects(ctx, target, blobKeys, tier)
		if wakeErr != nil {
			return result, wakeErr
		}
		addArchiveCounts(&counts, blobWake)
		return waitingRestoreResult(tier, counts), nil
	}
	if err = target.Get(ctx, manifestKey, manifestPath); err != nil {
		return result, fmt.Errorf("read snapshot manifest: %w", err)
	}
	manifest, err := readRemoteManifest(manifestPath)
	if err != nil {
		return result, err
	}
	if manifest.Schema > schema.Version {
		return result, fmt.Errorf("snapshot schema %d is newer than this NeoCloud build supports (maximum %d)", manifest.Schema, schema.Version)
	}
	manifestKeys := make([]string, 0, len(manifest.Blobs)+1)
	manifestKeys = append(manifestKeys, databaseKey)
	for _, entry := range manifest.Blobs {
		manifestKeys = append(manifestKeys, "blobs/"+entry)
	}
	manifestWake, err = wakeArchivedTargetObjects(ctx, target, manifestKeys, tier)
	if err != nil {
		return result, err
	}
	addArchiveCounts(&counts, manifestWake)
	if counts.Requested+counts.Pending > 0 {
		return waitingRestoreResult(tier, counts), nil
	}
	if err = target.Get(ctx, databaseKey, databasePath); err != nil {
		return result, fmt.Errorf("read encrypted snapshot database: %w", err)
	}
	content, blobSizes, err := validateRemoteDatabase(ctx, databasePath, manifest, options)
	if err != nil {
		return result, err
	}
	missing := make([]string, 0)
	for _, blob := range content.blobs {
		entry := strconv.FormatInt(blob.UserID, 10) + "/" + blob.ID
		size, ok := blobSizes[entry]
		if !ok {
			return result, fmt.Errorf("blob %s is missing from the restored database size index", entry)
		}
		encryptedSize, sizeErr := cryptox.EncryptedSize(size)
		if sizeErr != nil {
			return result, sizeErr
		}
		if !regularFileHasSize(localBlobPath(options.BlobDir, blob), encryptedSize) {
			missing = append(missing, "blobs/"+entry)
		}
	}
	blobWake, err := wakeArchivedTargetObjects(ctx, target, missing, tier)
	if err != nil {
		return result, err
	}
	addArchiveCounts(&counts, blobWake)
	if counts.Requested+counts.Pending > 0 {
		return waitingRestoreResult(tier, counts), nil
	}
	for _, key := range missing {
		destination := filepath.Join(workspace, filepath.FromSlash(key))
		if err = target.Get(ctx, key, destination); err != nil {
			return result, fmt.Errorf("download required backup blob %s: %w", strings.TrimPrefix(key, "blobs/"), err)
		}
	}
	localOptions := options
	localOptions.Target = nil
	localOptions.BackupRoot = workspace
	localOptions.Tier = ""
	return RestoreSnapshot(ctx, localOptions)
}

func readRemoteManifest(path string) (Manifest, error) {
	file, err := openRegularUnderRoot(filepath.Dir(filepath.Dir(path)), path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read snapshot manifest: %w", err)
	}
	data, err := readBoundedFromFile(file, maxBackupManifestBytes)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read snapshot manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil || manifest.App != "NeoCloud" || manifest.Format != 2 || manifest.DatabaseSize < 1 || manifest.Blobs == nil || manifest.Schema < 1 {
		return Manifest{}, errors.New("snapshot manifest is incomplete or unsupported")
	}
	return manifest, nil
}

func validateRemoteDatabase(ctx context.Context, encryptedPath string, manifest Manifest, options RestoreOptions) (snapshotContent, map[string]int64, error) {
	file, err := os.Open(encryptedPath)
	if err != nil {
		return snapshotContent{}, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return snapshotContent{}, nil, err
	}
	expected, err := cryptox.EncryptedSize(manifest.DatabaseSize)
	if err != nil || info.Size() != expected {
		return snapshotContent{}, nil, errors.New("encrypted snapshot database has an unexpected size")
	}
	key, err := options.Keys.BackupKey()
	if err != nil {
		return snapshotContent{}, nil, err
	}
	plainPath := encryptedPath + ".checked"
	defer os.Remove(plainPath)
	plain, err := os.OpenFile(plainPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return snapshotContent{}, nil, err
	}
	_, decryptErr := cryptox.DecryptBlobRange(file, manifest.DatabaseSize, 0, manifest.DatabaseSize-1, key, "snapshot:"+options.Snapshot, plain)
	if decryptErr == nil {
		decryptErr = plain.Sync()
	}
	closeErr := plain.Close()
	if decryptErr == nil {
		decryptErr = closeErr
	}
	if decryptErr != nil {
		return snapshotContent{}, nil, errors.New("snapshot database could not be decrypted; verify that NEOCLOUD_MASTER_KEY matches the backup")
	}
	content, err := readSnapshot(plainPath)
	if err != nil {
		return snapshotContent{}, nil, fmt.Errorf("snapshot database failed validation: %w", err)
	}
	if content.schema != manifest.Schema || content.size != manifest.DatabaseSize {
		return snapshotContent{}, nil, errors.New("snapshot manifest does not match its database")
	}
	entries := make([]string, 0, len(content.blobs))
	for _, blob := range content.blobs {
		entries = append(entries, strconv.FormatInt(blob.UserID, 10)+"/"+blob.ID)
	}
	if !sameStringSlice(entries, manifest.Blobs) {
		return snapshotContent{}, nil, errors.New("snapshot manifest blob list does not match its database")
	}
	sizes, err := readSnapshotBlobSizes(plainPath)
	if err != nil {
		return snapshotContent{}, nil, err
	}
	return content, sizes, nil
}

func wakeArchivedTargetObjects(ctx context.Context, target Target, keys []string, tier string) (archiveRestoreCounts, error) {
	counts := archiveRestoreCounts{}
	if len(keys) == 0 {
		return counts, nil
	}
	if tier != "Standard" && tier != "Bulk" {
		return counts, errors.New("restore tier must be Standard or Bulk")
	}
	archiveTarget, ok := target.(archiveAwareTarget)
	if !ok {
		return counts, nil
	}
	for _, key := range keys {
		stat, err := archiveTarget.StatObject(ctx, key)
		if err != nil {
			return counts, err
		}
		if !stat.Exists || stat.StorageClass != "GLACIER" && stat.StorageClass != "DEEP_ARCHIVE" {
			continue
		}
		counts.Archived++
		switch stat.Restore {
		case "available":
			continue
		case "ongoing":
			counts.Pending++
		default:
			if err = archiveTarget.RequestObjectRestore(ctx, key, tier); err != nil {
				return counts, err
			}
			counts.Requested++
		}
	}
	return counts, nil
}

func addArchiveCounts(target *archiveRestoreCounts, value archiveRestoreCounts) {
	target.Requested += value.Requested
	target.Pending += value.Pending
	target.Archived += value.Archived
}

func waitingRestoreResult(tier string, counts archiveRestoreCounts) RestoreResult {
	return RestoreResult{Waiting: true, RestoreTier: tier, Requested: counts.Requested, Pending: counts.Pending, Archived: counts.Archived}
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
