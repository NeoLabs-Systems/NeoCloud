package photos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
)

const (
	maxDuplicateImageBytes = int64(200 << 20)
	duplicateHashBatch     = 4
)

var duplicateHashMIMEs = []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "image/tiff", "image/heic", "image/heif", "image/bmp"}
var duplicateHashMIMEJSON, _ = json.Marshal(duplicateHashMIMEs)

type hashCandidate struct {
	userID               int64
	nodeID, blobID, etag string
	size                 int64
}

// HashPass reads and hashes a bounded number of live photos whose current
// content has no cached perceptual hash. Read/decode failures are stored as a
// NULL hash for this ETag so a bad file cannot pin the worker in a retry loop.
func (h *Handler) HashPass(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = duplicateHashBatch
	}
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE hidden(user_id,id) AS (
		SELECT user_id,id FROM nodes WHERE trashed_at IS NOT NULL OR vault_id IS NOT NULL
		UNION SELECT n.user_id,n.id FROM nodes n JOIN hidden x ON n.parent_id=x.id AND n.user_id=x.user_id
	) SELECT p.user_id,n.id,n.blob_id,n.etag,n.size FROM photo_items p
	JOIN nodes n ON n.id=p.node_id AND n.user_id=p.user_id AND n.blob_id=p.blob_id
		WHERE p.kind='photo' AND n.size<=? AND n.mime IN (SELECT value FROM json_each(?))
	AND NOT EXISTS(SELECT 1 FROM hidden WHERE hidden.user_id=n.user_id AND hidden.id=n.id)
		AND NOT EXISTS(SELECT 1 FROM image_hashes h WHERE h.node_id=n.id AND h.etag=n.etag)
		ORDER BY n.id LIMIT ?`, maxDuplicateImageBytes, string(duplicateHashMIMEJSON), limit)
	if err != nil {
		return 0, err
	}
	candidates := make([]hashCandidate, 0, limit)
	for rows.Next() {
		var item hashCandidate
		if err = rows.Scan(&item.userID, &item.nodeID, &item.blobID, &item.etag, &item.size); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	for _, item := range candidates {
		hash, hashErr := h.perceptualHash(ctx, item)
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		var value any
		if hashErr == nil {
			value = hash
		} else {
			slog.Warn("could not calculate photo duplicate hash", "node_id", item.nodeID, "error", hashErr)
		}
		_, err = h.db.ExecContext(ctx, `INSERT INTO image_hashes(node_id,user_id,etag,dhash) VALUES(?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET user_id=excluded.user_id,etag=excluded.etag,dhash=excluded.dhash`, item.nodeID, item.userID, item.etag, value)
		if err != nil {
			return 0, err
		}
	}
	return len(candidates), nil
}

func (h *Handler) perceptualHash(ctx context.Context, item hashCandidate) (string, error) {
	if item.size < 1 || item.size > maxDuplicateImageBytes {
		return "", errors.New("photo size is outside the hashing limit")
	}
	data, err := h.readBlobRange(ctx, item.userID, item.blobID, item.size, 0, item.size-1)
	if err != nil {
		return "", err
	}
	picture, err := imaging.Decode(ctx, data)
	if err != nil {
		return "", err
	}
	return picture.DHash(), nil
}
