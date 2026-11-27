package files

import (
	"context"
	"database/sql"
	"io"
	"math"
	"os"
)

// Incoming is a file another package is receiving for an account: encrypted on
// disk, not yet in the tree.
type Incoming struct{ blob uploadedBlob }

func (i *Incoming) Size() int64  { return i.blob.size }
func (i *Incoming) Hash() string { return i.blob.hash }

// Discard removes the received content unless it was committed.
func (i *Incoming) Discard() { _ = os.Remove(i.blob.tempPath) }

// UploadAllowance is the most bytes one upload of the account may carry and the
// error to answer past it.
func (h *Handler) UploadAllowance(ctx context.Context, userID int64) (int64, error, error) {
	allowed, code, err := h.uploadAllowance(ctx, userID)
	if err != nil {
		return 0, nil, err
	}
	return allowed, apiError{413, uploadLimitMessage(code), code}, nil
}

// Receive encrypts source under the account's key, answering overflow past
// limit bytes.
func (h *Handler) Receive(ctx context.Context, userID int64, source io.Reader, limit int64, overflow error) (*Incoming, error) {
	if err := h.CheckDiskSpace(0); err != nil {
		return nil, err
	}
	blob, err := h.encryptStream(ctx, userID, source, randomID(), min(limit, math.MaxInt64-1)+1, limit, overflow)
	if err != nil {
		return nil, err
	}
	return &Incoming{blob}, nil
}

// CommitReceived adds received content to a folder of the account under a free
// name; it never replaces anything. With declared (the sender gave the SHA-256)
// a resent upload returns the file already there. within runs in the
// committing transaction with the stored node and can refuse it.
func (h *Handler) CommitReceived(ctx context.Context, userID int64, parentID, name string, incoming *Incoming, declared bool, within func(*sql.Tx, Node) error) (Node, error) {
	blob := incoming.blob
	blob.declared, blob.within = declared, within
	var err error
	if blob.finalPath, err = h.placeUpload(userID, blob); err != nil {
		return node{}, err
	}
	stored, orphans, err := h.commitUpload(ctx, userID, userID, parentID, name, "rename", "", "", blob, nil, nil)
	if err != nil {
		_ = os.Remove(blob.finalPath)
		return node{}, err
	}
	h.RemoveBlobFiles(userID, orphans)
	return stored, nil
}
