package publicshares

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const maxSafeInteger = int64(1<<53 - 1)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func uploadLimitReached() error {
	return apperr.New(http.StatusRequestEntityTooLarge, "This link can’t take more uploads.", "SHARE_UPLOAD_LIMIT")
}

// upload takes a visitor's file into the shared folder; it never replaces
// anything. On an upload-only link it goes to the shared folder itself and the
// answer names only what the visitor sent, not what the file is called now.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	s, root, err := h.open(r, false)
	if err != nil {
		return err
	}
	if s.Mode == "view" || root.Type != "folder" {
		return apperr.New(http.StatusForbidden, "This link does not accept uploads.", "UPLOAD_NOT_ALLOWED")
	}
	folder := root
	folderID := r.URL.Query().Get("folderId")
	if s.Mode == "upload_only" {
		if folderID != "" && folderID != root.ID {
			return uploadOnly()
		}
	} else if folder, err = h.nodeInShare(ctx, s, root, folderID, "folder"); err != nil {
		return err
	}
	length := r.ContentLength // -1 when the client sends no size up front.
	// A capped link reserves the upload's size before reading a byte, in one
	// conditional UPDATE, so parallel uploads can't together go past the cap.
	var reserved int64
	if s.MaxUploadBytes.Valid {
		if length < 0 || length > maxSafeInteger {
			return apperr.New(http.StatusLengthRequired, "This link needs the upload size up front (Content-Length).", "LENGTH_REQUIRED")
		}
		result, err := h.db.ExecContext(ctx, `UPDATE shares SET uploaded_bytes=uploaded_bytes+? WHERE id=? AND uploaded_bytes<max_upload_bytes AND uploaded_bytes+?<=max_upload_bytes`, length, s.ID, length)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return uploadLimitReached()
		}
		reserved = length
	}
	succeeded := false
	defer func() {
		// A failed upload gives its reservation back.
		if !succeeded && reserved > 0 {
			_, _ = h.db.ExecContext(context.WithoutCancel(ctx), `UPDATE shares SET uploaded_bytes=MAX(0,uploaded_bytes-?) WHERE id=?`, reserved, s.ID)
		}
	}()
	name, err := files.ValidateName(r.URL.Query().Get("name"))
	if err != nil {
		return err
	}
	expectedHash := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Content-SHA256")))
	if expectedHash != "" && !sha256Pattern.MatchString(expectedHash) {
		return apperr.New(http.StatusBadRequest, "sha256 must be 64 hex characters.", "INVALID_HASH")
	}
	limit, limitError, err := h.files.UploadAllowance(ctx, s.UserID)
	if err != nil {
		return err
	}
	if s.MaxUploadBytes.Valid && reserved < limit {
		limit, limitError = reserved, uploadLimitReached()
	}
	if length > limit {
		return limitError
	}
	if err = h.files.CheckDiskSpace(length); err != nil {
		return err
	}
	incoming, err := h.files.Receive(ctx, s.UserID, r.Body, limit, limitError)
	if err != nil {
		return err
	}
	defer incoming.Discard()
	if length >= 0 && incoming.Size() != length {
		return apperr.New(http.StatusBadRequest, fmt.Sprintf("Expected %d bytes but received %d.", length, incoming.Size()), "SIZE_MISMATCH")
	}
	if expectedHash != "" && expectedHash != incoming.Hash() {
		return apperr.New(http.StatusUnprocessableEntity, "The file arrived damaged (checksum mismatch).", "CHECKSUM_MISMATCH")
	}
	stored, err := h.files.CommitReceived(ctx, s.UserID, folder.ID, name, incoming, expectedHash != "", func(tx *sql.Tx, kept files.Node) error {
		// The folder may have moved or gone while the body arrived.
		var inside bool
		if err := tx.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id) AS (
			SELECT id,parent_id FROM nodes WHERE id=? AND user_id=?
			UNION ALL SELECT p.id,p.parent_id FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
		) SELECT EXISTS(SELECT 1 FROM up WHERE id=?)`, folder.ID, s.UserID, s.UserID, root.ID).Scan(&inside); err != nil {
			return err
		}
		if !inside || kept.VaultID != nil {
			return notFound()
		}
		// The link's count settles with the file.
		_, err := tx.ExecContext(ctx, `UPDATE shares SET uploaded_bytes=uploaded_bytes+? WHERE id=?`, kept.Size-reserved, s.ID)
		return err
	})
	if err != nil {
		return err
	}
	succeeded = true
	if s.Mode == "upload_only" {
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"name": name, "size": incoming.Size()})
		return nil
	}
	httpx.WriteJSON(w, http.StatusCreated, view{stored.ID, stored.Type, stored.Name, stored.Size, stored.Mime, stored.ModifiedAt})
	return nil
}
