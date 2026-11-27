package backups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

// ListTargetSnapshots returns completed snapshots newest-first.
func ListTargetSnapshots(ctx context.Context, target Target) ([]string, error) {
	if err := target.Open(ctx); err != nil {
		return nil, err
	}
	defer target.Close()
	keys, err := target.List(ctx, "snapshots")
	if err != nil {
		return nil, err
	}
	manifests := map[string]bool{}
	databases := map[string]bool{}
	for _, key := range keys {
		parts := strings.Split(key, "/")
		if len(parts) != 3 || parts[0] != "snapshots" || !safeSnapshotName(parts[1]) {
			continue
		}
		switch parts[2] {
		case "manifest.json":
			manifests[parts[1]] = true
		case "neocloud.db.enc":
			databases[parts[1]] = true
		}
	}
	names := make([]string, 0)
	for name := range manifests {
		if databases[name] {
			names = append(names, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// VerifyTargetSnapshot verifies the newest completed snapshot and its required
// encrypted blobs, downloading only into a private temporary directory.
func (r *Runner) VerifyTargetSnapshot(ctx context.Context, target Target) SnapshotVerification {
	result := SnapshotVerification{VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Mode: "full"}
	if err := target.Open(ctx); err != nil {
		result.Error = "Could not open backup target."
		return result
	}
	defer target.Close()
	names, err := listTargetSnapshotsOpen(ctx, target)
	if err != nil {
		result.Error = "Could not list backup snapshots."
		return result
	}
	if len(names) == 0 {
		result.Error = "There is no finished snapshot on this target yet."
		return result
	}
	name := names[0]
	result.Snapshot = name
	tmp, err := os.MkdirTemp(r.tmpRoot, "backup-verify-target-")
	if err != nil {
		result.Error = "Could not prepare snapshot verification."
		return result
	}
	defer os.RemoveAll(tmp)
	manifestPath := filepath.Join(tmp, "manifest.json")
	if err = target.Get(ctx, "snapshots/"+name+"/manifest.json", manifestPath); err != nil {
		result.Error = "The snapshot manifest could not be read."
		return result
	}
	manifestBytes, err := readBoundedManifest(manifestPath)
	if err != nil {
		result.Error = "The snapshot manifest is invalid."
		return result
	}
	var manifest Manifest
	if err = json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.App != "NeoCloud" || manifest.Format < 1 || manifest.DatabaseSize < 1 || manifest.Blobs == nil {
		result.Error = "The snapshot manifest is invalid."
		return result
	}
	result.BlobsChecked = len(manifest.Blobs)
	for _, item := range manifest.Blobs {
		parts := strings.Split(item, "/")
		if len(parts) != 2 || !backupUserIDPattern.MatchString(parts[0]) || !backupUUIDPattern.MatchString(parts[1]) {
			result.Error = "The snapshot manifest contains an invalid blob reference."
			return result
		}
		exists, existsErr := target.Exists(ctx, "blobs/"+parts[0]+"/"+parts[1])
		if existsErr != nil || !exists {
			result.MissingBlobs++
		}
	}
	if manifest.MissingBlobs > result.MissingBlobs {
		result.MissingBlobs = manifest.MissingBlobs
	}
	if err = target.Get(ctx, "snapshots/"+name+"/neocloud.db.enc", filepath.Join(tmp, "neocloud.db.enc")); err != nil {
		result.Database = "failed"
		result.Error = "The snapshot database could not be read."
		return result
	}
	content, err := r.verifyTargetDatabase(filepath.Join(tmp, "neocloud.db.enc"), manifest.DatabaseSize, name, tmp)
	if err != nil {
		result.Database = "failed"
		result.Error = "The snapshot database could not be decrypted or failed its integrity check."
		return result
	}
	manifestBlobs := append([]string{}, manifest.Blobs...)
	databaseBlobs := make([]string, 0, len(content.blobs))
	for _, blob := range content.blobs {
		databaseBlobs = append(databaseBlobs, strconv.FormatInt(blob.UserID, 10)+"/"+blob.ID)
	}
	if !reflect.DeepEqual(manifestBlobs, databaseBlobs) {
		result.Database = "failed"
		result.Error = "The snapshot manifest does not match the database blob references."
		return result
	}
	result.Database = "ok"
	result.OK = result.MissingBlobs == 0
	if !result.OK {
		result.Error = strconv.Itoa(result.MissingBlobs) + " blob(s) listed in the snapshot are missing on the target."
	}
	return result
}

func (r *Runner) verifyTargetDatabase(encryptedPath string, size int64, name, tmp string) (snapshotContent, error) {
	if size < 1 || size > (1<<50) {
		return snapshotContent{}, errors.New("invalid backup database size")
	}
	key, err := r.keys.BackupKey()
	if err != nil {
		return snapshotContent{}, err
	}
	input, err := os.Open(encryptedPath)
	if err != nil {
		return snapshotContent{}, err
	}
	defer input.Close()
	plainPath := filepath.Join(tmp, "snapshot.db")
	plain, err := os.OpenFile(plainPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return snapshotContent{}, err
	}
	_, decryptErr := cryptox.DecryptBlobRange(input, size, 0, size-1, key, "snapshot:"+name, plain)
	if decryptErr == nil {
		decryptErr = plain.Sync()
	}
	closeErr := plain.Close()
	if decryptErr == nil {
		decryptErr = closeErr
	}
	if decryptErr != nil {
		return snapshotContent{}, decryptErr
	}
	return readSnapshot(plainPath)
}

func listTargetSnapshotsOpen(ctx context.Context, target Target) ([]string, error) {
	keys, err := target.List(ctx, "snapshots")
	if err != nil {
		return nil, err
	}
	manifests, databases := map[string]bool{}, map[string]bool{}
	for _, key := range keys {
		parts := strings.Split(key, "/")
		if len(parts) != 3 || parts[0] != "snapshots" || !safeSnapshotName(parts[1]) {
			continue
		}
		if parts[2] == "manifest.json" {
			manifests[parts[1]] = true
		}
		if parts[2] == "neocloud.db.enc" {
			databases[parts[1]] = true
		}
	}
	names := []string{}
	for name := range manifests {
		if databases[name] {
			names = append(names, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

func pruneTarget(ctx context.Context, target Target, retention int) error {
	names, err := listTargetSnapshotsOpen(ctx, target)
	if err != nil {
		return err
	}
	needed := map[string]bool{}
	for _, name := range names[:minInt(retention, len(names))] {
		manifestKey := "snapshots/" + name + "/manifest.json"
		tmp, err := os.CreateTemp("", "neocloud-backup-manifest-*.json")
		if err != nil {
			return err
		}
		manifestPath := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(manifestPath)
		if err = target.Get(ctx, manifestKey, manifestPath); err != nil {
			return err
		}
		data, readErr := readBoundedManifest(manifestPath)
		_ = os.Remove(manifestPath)
		if readErr != nil {
			return readErr
		}
		var manifest Manifest
		if err = json.Unmarshal(data, &manifest); err != nil || manifest.Blobs == nil {
			return errors.New("backup manifest is invalid during retention")
		}
		for _, item := range manifest.Blobs {
			parts := strings.Split(item, "/")
			if len(parts) != 2 || !backupUserIDPattern.MatchString(parts[0]) || !backupUUIDPattern.MatchString(parts[1]) {
				return errors.New("backup manifest contains invalid blob key")
			}
			needed[item] = true
		}
	}
	for _, stale := range names[minInt(retention, len(names)):] {
		if err = target.DeletePrefix(ctx, "snapshots/"+stale); err != nil {
			return err
		}
	}
	blobKeys, err := target.List(ctx, "blobs")
	if err != nil {
		return err
	}
	for _, key := range blobKeys {
		rel := strings.TrimPrefix(key, "blobs/")
		parts := strings.Split(rel, "/")
		if len(parts) != 2 || !backupUserIDPattern.MatchString(parts[0]) || !backupUUIDPattern.MatchString(parts[1]) {
			continue
		}
		if !needed[rel] {
			if err = target.Delete(ctx, key); err != nil {
				return fmt.Errorf("prune backup blob: %w", err)
			}
		}
	}
	return nil
}
