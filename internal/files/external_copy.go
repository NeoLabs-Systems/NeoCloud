package files

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

// This file is what other packages (cloud storage, imports) use to read and
// place files in an account's own tree, under the same checks, quota, journal
// and activity rules as the file routes. Shared folders are out of scope here.

// Node is a file or folder as the API answers it.
type Node = node

// ValidateName normalises a file or folder name, refusing what can't be stored.
func ValidateName(raw string) (string, error) { return validateName(raw) }

// MimeForName is the media type stored for a file of this name.
func MimeForName(name string) string {
	value, _ := mimeForName(name).(string)
	return value
}

// ContentDisposition builds the header with an ASCII and a UTF-8 file name.
func ContentDisposition(kind, name string) string { return contentDisposition(kind, name) }

func (h *Handler) inTx(ctx context.Context, action func(*sql.Tx) error) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = action(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// OwnNode returns the account's own live node ("root" is its top folder).
func (h *Handler) OwnNode(ctx context.Context, userID int64, id string) (Node, error) {
	var found node
	err := h.inTx(ctx, func(tx *sql.Tx) (err error) {
		found, err = h.live(tx, userID, id)
		return err
	})
	return found, err
}

// OwnFolder is OwnNode for a folder.
func (h *Handler) OwnFolder(ctx context.Context, userID int64, id string) (Node, error) {
	folder, err := h.OwnNode(ctx, userID, id)
	if err == nil && folder.Type != "folder" {
		err = apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
	}
	return folder, err
}

// ChildByName finds the live item of that name in a folder, ignoring case.
func (h *Handler) ChildByName(ctx context.Context, userID int64, parentID, name string) (Node, bool, error) {
	child, err := scanNode(h.db.QueryRowContext(ctx, nodeSelect+` WHERE n.user_id=? AND n.parent_id=? AND lower(n.name)=lower(?) AND n.trashed_at IS NULL`, userID, parentID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return node{}, false, nil
	}
	return child, err == nil, err
}

// EnsureFolderPath makes sure every folder along relative ("a/b/c") exists
// below parentID and returns the deepest one. It never creates anything in a
// vault: it stops at the first vault folder, which the caller sees by VaultID.
func (h *Handler) EnsureFolderPath(ctx context.Context, userID int64, parentID, relative string) (Node, error) {
	parts, err := pathParts(relative)
	if err != nil {
		return node{}, err
	}
	var current node
	err = h.inTx(ctx, func(tx *sql.Tx) (err error) {
		if current, err = h.live(tx, userID, parentID); err != nil {
			return err
		}
		if current.Type != "folder" {
			return apiError{400, "That is not a folder.", "NOT_A_FOLDER"}
		}
		for _, part := range parts {
			if current.VaultID != nil {
				return nil
			}
			if current, _, err = h.createFolderTx(tx, userID, userID, current, part, "replace"); err != nil {
				return err
			}
			if current.Type != "folder" {
				return nameExists(current.Name)
			}
		}
		return nil
	})
	return current, err
}

// SetModifiedAt keeps a date that came with the content. The journal entry
// lets sync clients that already saw the node pick it up.
func (h *Handler) SetModifiedAt(ctx context.Context, userID int64, nodeID, modifiedAt string) error {
	return h.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE nodes SET modified_at=? WHERE id=? AND user_id=?`, modifiedAt, nodeID, userID); err != nil {
			return err
		}
		return recordChange(tx, userID, nodeID, "update")
	})
}

// MarkFavorite stars one of the account's own live items.
func (h *Handler) MarkFavorite(ctx context.Context, userID int64, nodeID string) error {
	return h.inTx(ctx, func(tx *sql.Tx) error {
		found, err := h.live(tx, userID, nodeID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT OR IGNORE INTO favorites(user_id,node_id,created_at) VALUES(?,?,strftime('%Y-%m-%d %H:%M:%f','now'))`, userID, found.ID)
		return err
	})
}

// RemainingBytes is what the account may still store; math.MaxInt64-1 when
// unlimited.
func (h *Handler) RemainingBytes(ctx context.Context, userID int64) (int64, error) {
	var quota sql.NullInt64
	var used, reserved int64
	if err := h.db.QueryRowContext(ctx, `SELECT quota_bytes,used_bytes FROM users WHERE id=?`, userID).Scan(&quota, &used); errors.Is(err, sql.ErrNoRows) {
		return 0, apiError{404, "User not found", "NOT_FOUND"}
	} else if err != nil {
		return 0, err
	}
	limit := h.storagePolicy().DefaultQuotaBytes
	if quota.Valid {
		limit = quota.Int64
	}
	if limit < 0 {
		return math.MaxInt64 - 1, nil
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_size),0) FROM upload_sessions WHERE user_id=? AND datetime(expires_at)>datetime('now')`, userID).Scan(&reserved); err != nil {
		return 0, err
	}
	return max(limit-used-reserved, 0), nil
}

// CheckUploadFits refuses a file of that size before any of it is fetched.
func (h *Handler) CheckUploadFits(ctx context.Context, userID, size int64) error {
	allowed, code, err := h.uploadAllowance(ctx, userID)
	if err != nil {
		return err
	}
	if size > allowed {
		return apiError{413, uploadLimitMessage(code), code}
	}
	return nil
}

// ExternalCopySource returns a decrypted stream of one of the account's own
// files outside any vault. The caller closes it.
func (h *Handler) ExternalCopySource(ctx context.Context, userID int64, nodeID string) (Node, io.ReadCloser, error) {
	item, err := h.OwnNode(ctx, userID, nodeID)
	if err != nil {
		return node{}, nil, err
	}
	if item.Type != "file" {
		return node{}, nil, apiError{400, "Only files can be copied to cloud storage.", "NOT_A_FILE"}
	}
	if item.VaultID != nil {
		return node{}, nil, apiError{409, "Items can’t be moved or copied into or out of a vault.", "VAULT_BOUNDARY"}
	}
	var blobID string
	if err = h.db.QueryRowContext(ctx, `SELECT blob_id FROM nodes WHERE id=? AND user_id=?`, item.ID, userID).Scan(&blobID); err != nil {
		return node{}, nil, err
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return node{}, nil, err
	}
	file, err := os.Open(blobPath(h.blobDir, userID, blobID))
	if err != nil {
		return node{}, nil, err
	}
	reader, writer := io.Pipe()
	go func() {
		defer file.Close()
		var decryptErr error
		if item.Size > 0 {
			_, decryptErr = cryptox.DecryptBlobRange(file, item.Size, 0, item.Size-1, dek, blobID, writer)
		}
		_ = writer.CloseWithError(decryptErr)
	}()
	return item, reader, nil
}

// ImportCopy stores source as a file in one of the account's own folders; see
// StoreFile.
func (h *Handler) ImportCopy(ctx context.Context, userID int64, parentID, name string, source io.Reader, size int64, conflict string) (Node, error) {
	return h.StoreFile(ctx, userID, parentID, name, source, size, conflict, "")
}

// StoreFile stores source as a file in one of the account's own folders,
// through the same encrypted upload, quota, version, activity and journal path
// as local uploads. size is -1 when unknown. conflict is "fail", "rename" or
// "replace" (the file keeps its identity and history); baseETag refuses a
// replace of content other than that.
func (h *Handler) StoreFile(ctx context.Context, userID int64, parentID, name string, source io.Reader, size int64, conflict, baseETag string) (Node, error) {
	cleanName, err := validateName(name)
	if err != nil {
		return node{}, err
	}
	parent, err := h.OwnFolder(ctx, userID, parentID)
	if err != nil {
		return node{}, err
	}
	if parent.VaultID != nil {
		return node{}, apiError{409, "Items can’t be moved or copied into or out of a vault.", "VAULT_BOUNDARY"}
	}
	if err = h.CheckDiskSpace(size); err != nil {
		return node{}, err
	}
	allowed, code, err := h.uploadAllowance(ctx, userID)
	if err != nil {
		return node{}, err
	}
	tooLarge := apiError{413, uploadLimitMessage(code), code}
	if size > allowed {
		return node{}, tooLarge
	}
	readLimit := allowed + 1
	if size >= 0 {
		readLimit = size + 1
	}
	blob, err := h.encryptStream(ctx, userID, source, randomID(), readLimit, allowed, tooLarge)
	if err != nil {
		return node{}, err
	}
	if size >= 0 && blob.size != size {
		_ = os.Remove(blob.tempPath)
		return node{}, apiError{400, fmt.Sprintf("Expected %d bytes but received %d.", size, blob.size), "SIZE_MISMATCH"}
	}
	if blob.finalPath, err = h.placeUpload(userID, blob); err != nil {
		_ = os.Remove(blob.tempPath)
		return node{}, err
	}
	created, orphans, err := h.commitUpload(ctx, userID, userID, parent.ID, cleanName, conflict, baseETag, "", blob, nil, nil)
	if err != nil {
		_ = os.Remove(blob.finalPath)
		return node{}, err
	}
	h.RemoveBlobFiles(userID, orphans)
	return created, nil
}
