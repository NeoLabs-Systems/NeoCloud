package backups

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

// SnapshotVerification describes a local snapshot integrity check. It does
// not modify the backup target or the live database.
type SnapshotVerification struct {
	VerifiedAt   string `json:"verifiedAt"`
	OK           bool   `json:"ok"`
	Mode         string `json:"mode"`
	Snapshot     string `json:"snapshot"`
	Database     string `json:"database"`
	BlobsChecked int    `json:"blobsChecked"`
	MissingBlobs int    `json:"missingBlobs"`
	Error        string `json:"error"`
}

// ListSnapshots returns completed snapshot names in newest-first order.
// Partial directories are omitted by requiring a readable manifest and the
// encrypted database object.
func ListSnapshots(root string) ([]string, error) {
	base := filepath.Join(root, "snapshots")
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !safeSnapshotName(entry.Name()) {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		manifestInfo, manifestErr := os.Lstat(filepath.Join(dir, "manifest.json"))
		databaseInfo, databaseErr := os.Lstat(filepath.Join(dir, "neocloud.db.enc"))
		if manifestErr == nil && databaseErr == nil && manifestInfo.Mode().IsRegular() && databaseInfo.Mode().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// VerifySnapshot decrypts and checks the newest local snapshot and verifies
// that every blob named by its manifest is present. Errors are returned as a
// result, matching the Node endpoint's report-oriented contract.
func (r *Runner) VerifySnapshot(root string) SnapshotVerification {
	result := SnapshotVerification{VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Mode: "full"}
	names, err := ListSnapshots(root)
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
	manifestPath := filepath.Join(root, "snapshots", name, "manifest.json")
	manifestBytes, err := readBoundedManifest(manifestPath)
	if err != nil {
		result.Error = "The snapshot manifest could not be read."
		return result
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	if err = decoder.Decode(&manifest); err != nil || manifest.App != "NeoCloud" || manifest.Format < 1 || manifest.DatabaseSize < 1 || manifest.Blobs == nil {
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
		blobPath := filepath.Join(root, "blobs", parts[0], parts[1])
		info, statErr := os.Lstat(blobPath)
		if statErr != nil || !info.Mode().IsRegular() {
			result.MissingBlobs++
		}
	}
	if manifest.MissingBlobs > result.MissingBlobs {
		result.MissingBlobs = manifest.MissingBlobs
	}
	content, err := r.verifyDatabase(root, name, manifest.DatabaseSize)
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

func (r *Runner) verifyDatabase(root, name string, size int64) (snapshotContent, error) {
	if size < 1 || size > (1<<50) {
		return snapshotContent{}, errors.New("invalid database snapshot size")
	}
	backupKey, err := r.keys.BackupKey()
	if err != nil {
		return snapshotContent{}, err
	}
	file, err := os.Open(filepath.Join(root, "snapshots", name, "neocloud.db.enc"))
	if err != nil {
		return snapshotContent{}, err
	}
	defer file.Close()
	if err := os.MkdirAll(r.tmpRoot, 0700); err != nil {
		return snapshotContent{}, err
	}
	plain, err := os.CreateTemp(r.tmpRoot, "backup-verify-*.db")
	if err != nil {
		return snapshotContent{}, err
	}
	plainPath := plain.Name()
	defer os.Remove(plainPath)
	if err = plain.Chmod(0600); err == nil {
		_, err = cryptox.DecryptBlobRange(file, size, 0, size-1, backupKey, "snapshot:"+name, plain)
	}
	if err == nil {
		err = plain.Sync()
	}
	closeErr := plain.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return snapshotContent{}, err
	}
	return readSnapshot(plainPath)
}

func readBoundedManifest(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxBackupManifestBytes {
		return nil, errors.New("invalid backup manifest file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBackupManifestBytes+1))
	if err != nil || int64(len(data)) > maxBackupManifestBytes {
		return nil, errors.New("backup manifest too large")
	}
	return data, nil
}
