package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

const maxRenditionBytes = int64(128 << 20)

// renderJob is one rendition being made; concurrent requests for it wait for
// the same result.
type renderJob struct {
	done chan struct{}
	data []byte
	err  error
}

// rendition returns the cached rendition of the source, making it on first use.
// At most a couple of renders run at once, so that a folder of photos opening
// in the grid cannot starve uploads and downloads of CPU.
func (h *Handler) rendition(ctx context.Context, source previewSource, variant, mime string, render func(context.Context) ([]byte, error)) ([]byte, error) {
	if data := h.cachedRendition(ctx, source, variant, mime); data != nil {
		return data, nil
	}
	key := source.blobID + ":" + variant
	h.rendersMu.Lock()
	job, running := h.renders[key]
	if !running {
		job = &renderJob{done: make(chan struct{})}
		h.renders[key] = job
	}
	h.rendersMu.Unlock()
	if !running {
		// Others may be waiting, so the render outlives its first requester.
		go func(ctx context.Context) {
			h.thumbnailSlots <- struct{}{}
			defer func() {
				// This goroutine is outside the server's recovery, and the file
				// it works on is untrusted.
				if recovered := recover(); recovered != nil {
					job.err = fmt.Errorf("rendering failed: %v", recovered)
				}
				<-h.thumbnailSlots
				h.rendersMu.Lock()
				delete(h.renders, key)
				h.rendersMu.Unlock()
				close(job.done)
			}()
			job.data, job.err = render(ctx)
			if job.err == nil {
				job.err = h.storeRendition(ctx, source, variant, mime, job.data)
			}
		}(context.WithoutCancel(ctx))
	}
	select {
	case <-job.done:
		return job.data, job.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func previewPath(root string, userID int64, blobID, variant string) string {
	return filepath.Join(root, strconv.FormatInt(userID, 10), blobID, variant)
}

func previewCipherID(blobID, variant string) string {
	return "preview-" + blobID + "-" + variant
}

// cachedRendition is nil when there is none, or when it can no longer be read.
func (h *Handler) cachedRendition(ctx context.Context, source previewSource, variant, mime string) []byte {
	var size int64
	err := h.db.QueryRowContext(ctx, `SELECT size FROM previews WHERE blob_id=? AND variant=? AND user_id=? AND mime=?`,
		source.blobID, variant, source.ownerID, mime).Scan(&size)
	if err != nil || size < 1 || size > maxRenditionBytes {
		return nil
	}
	data, err := h.decryptFile(ctx, source.ownerID, previewPath(h.previewDir, source.ownerID, source.blobID, variant), previewCipherID(source.blobID, variant), size)
	if err != nil {
		return nil
	}
	return data
}

// readPreviewSource decrypts the whole file into memory.
func (h *Handler) readPreviewSource(ctx context.Context, source previewSource) ([]byte, error) {
	if source.size < 1 || len(source.blobID) < 2 {
		return nil, errors.New("the file is empty")
	}
	return h.decryptFile(ctx, source.ownerID, blobPath(h.blobDir, source.ownerID, source.blobID), source.blobID, source.size)
}

func (h *Handler) decryptFile(ctx context.Context, ownerID int64, path, cipherID string, size int64) ([]byte, error) {
	if h.keys == nil {
		return nil, errors.New("the keyring is not ready")
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, ownerID)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	plain := bytes.NewBuffer(make([]byte, 0, size))
	if _, err = cryptox.DecryptBlobRange(file, size, 0, size-1, dek, cipherID, plain); err != nil {
		return nil, err
	}
	if int64(plain.Len()) != size {
		return nil, errors.New("decrypted size mismatch")
	}
	return plain.Bytes(), nil
}

// storeRendition encrypts data and records it as a rendition of the blob.
func (h *Handler) storeRendition(ctx context.Context, source previewSource, variant, mime string, data []byte) error {
	if h.previewDir == "" || h.keys == nil {
		return errors.New("preview storage is not configured")
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, source.ownerID)
	if err != nil {
		return err
	}
	target := previewPath(h.previewDir, source.ownerID, source.blobID, variant)
	directory := filepath.Dir(target)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(directory, ".rendition-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err = cryptox.EncryptBlob(tmp, bytes.NewReader(data), dek, previewCipherID(source.blobID, variant)); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	// The blob may have been deleted (file purged or replaced) while this
	// rendered; its preview folder is then removed again instead of recorded.
	stored, err := h.db.ExecContext(ctx, `INSERT INTO previews(blob_id,variant,user_id,size,mime)
		SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM blobs WHERE id=? AND user_id=?)
		ON CONFLICT(blob_id,variant) DO UPDATE SET size=excluded.size,mime=excluded.mime,user_id=excluded.user_id`,
		source.blobID, variant, source.ownerID, len(data), mime, source.blobID, source.ownerID)
	if err != nil {
		_ = os.Remove(target)
		return err
	}
	if changed, err := stored.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		if err = os.RemoveAll(directory); err != nil {
			return err
		}
		return apiError{http.StatusNotFound, "File not found", "NOT_FOUND"}
	}
	return nil
}
