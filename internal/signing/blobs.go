package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/google/uuid"
)

// Bytes in and out of an account's encrypted blob storage, for what the
// signing feature keeps as blobs. The file is put in place first; the caller
// records it in a transaction and discards the file if that fails.

type storedBlob struct {
	id, sha256, path string
	size             int64
}

func (h *Handler) blobPath(userID int64, id string) string {
	return filepath.Join(h.blobDir, strconv.FormatInt(userID, 10), id[:2], id)
}

func (h *Handler) openBlob(ctx context.Context, userID int64, id string) (*os.File, []byte, error) {
	if !uuidPattern.MatchString(id) {
		return nil, nil, errors.New("signing: stored file id is invalid")
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(h.blobPath(userID, id))
	return file, dek, err
}

func (h *Handler) readBlob(ctx context.Context, userID int64, id string, size int64) ([]byte, error) {
	if size == 0 {
		return []byte{}, nil
	}
	file, dek, err := h.openBlob(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out bytes.Buffer
	out.Grow(int(size))
	if _, err = cryptox.DecryptBlobRange(file, size, 0, size-1, dek, id, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (h *Handler) storeBlob(ctx context.Context, userID int64, data []byte) (blob storedBlob, err error) {
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return blob, err
	}
	sum := sha256.Sum256(data)
	blob = storedBlob{id: uuid.NewString(), sha256: hex.EncodeToString(sum[:]), size: int64(len(data))}
	blob.path = h.blobPath(userID, blob.id)
	dir := filepath.Dir(blob.path)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return blob, err
	}
	temp, err := os.CreateTemp(dir, ".sign-*")
	if err != nil {
		return blob, err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(temp.Name())
		}
	}()
	_, err = cryptox.EncryptBlob(temp, io.Reader(bytes.NewReader(data)), dek, blob.id)
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return blob, err
	}
	err = os.Rename(temp.Name(), blob.path)
	return blob, err
}

func (blob storedBlob) discard() { _ = os.Remove(blob.path) }

// adoptBlob records a stored blob with one reference and counts it against the
// account.
func adoptBlob(ctx context.Context, q querier, userID int64, blob storedBlob) error {
	if _, err := q.ExecContext(ctx, `INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,?,?,?,1,1)`, blob.id, userID, blob.size, blob.sha256); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `UPDATE users SET used_bytes=used_bytes+? WHERE id=?`, blob.size, userID)
	return err
}

// releaseBlobs drops one reference each and returns the ids whose files are no
// longer referenced; remove them after the commit.
func releaseBlobs(ctx context.Context, q querier, ids ...string) ([]string, error) {
	var orphaned []string
	for _, id := range ids {
		var userID, size, refs int64
		err := q.QueryRowContext(ctx, `SELECT user_id,size,ref_count FROM blobs WHERE id=?`, id).Scan(&userID, &size, &refs)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if refs > 1 {
			if _, err = q.ExecContext(ctx, `UPDATE blobs SET ref_count=ref_count-1 WHERE id=?`, id); err != nil {
				return nil, err
			}
			continue
		}
		if _, err = q.ExecContext(ctx, `DELETE FROM blobs WHERE id=?`, id); err != nil {
			return nil, err
		}
		if _, err = q.ExecContext(ctx, `UPDATE users SET used_bytes=MAX(0,used_bytes-?) WHERE id=?`, size, userID); err != nil {
			return nil, err
		}
		orphaned = append(orphaned, id)
	}
	return orphaned, nil
}

// checkRoom refuses a write the account has no quota left for.
func (h *Handler) checkRoom(ctx context.Context, q querier, userID, size int64, message string) error {
	var quota sql.NullInt64
	var used, reserved int64
	if err := q.QueryRowContext(ctx, `SELECT quota_bytes,used_bytes FROM users WHERE id=?`, userID).Scan(&quota, &used); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_size),0) FROM upload_sessions WHERE user_id=? AND datetime(expires_at)>datetime('now')`, userID).Scan(&reserved); err != nil {
		return err
	}
	limit := h.defaultQuota.Load()
	if quota.Valid {
		limit = quota.Int64
	}
	if limit >= 0 && size > limit-used-reserved {
		return httpError(413, message, "QUOTA_EXCEEDED")
	}
	return nil
}
