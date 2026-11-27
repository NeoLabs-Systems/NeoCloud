package publicshares

import (
	"context"
	"net/http"
	"net/url"

	"github.com/NeoLabs-Systems/neocloud/internal/files"
)

func (h *Handler) download(w http.ResponseWriter, r *http.Request) error {
	// Only downloads count, not inline views. Resuming a counted download does
	// not count again and may finish even once the limit is reached.
	s, root, err := h.open(r, true)
	if err != nil {
		return err
	}
	if s.Mode == "upload_only" {
		return uploadOnly()
	}
	n, err := h.nodeInShare(r.Context(), s, root, r.PathValue("nodeId"), "file")
	if err != nil {
		return err
	}
	start, _, ranged, unsatisfiable := files.ParseByteRange(r.Header.Get("Range"), n.Size)
	download := r.URL.Query().Get("download") == "1" || r.URL.Query().Get("download") == "true"
	// A range past byte 0 that carries the cookie of a counted download resumes
	// it; anything that includes byte 0 is a new download.
	resuming := download && ranged && !unsatisfiable && start > 0 &&
		h.validProof("continue", continuePayload(s, n.ID), cookieValue(r, continueCookie))
	if !resuming {
		if s.usedUp() {
			return exhausted()
		}
		if download && r.Method != http.MethodHead {
			if err = h.consumeDownload(r.Context(), s.ID); err != nil {
				return err
			}
			h.setProof(w, continueCookie, cookiePath(s.Token)+"/files/"+url.PathEscape(n.ID), "continue", continuePayload(s, n.ID), continueAge)
		}
	}
	mimeType := ""
	if n.MIME != nil {
		mimeType = *n.MIME
	}
	return h.files.SendContent(w, r, s.UserID, files.Content{BlobID: n.BlobID.String, Name: n.Name, MIME: mimeType, ETag: n.ETag, ModifiedAt: n.ModifiedAt, Size: n.Size}, download)
}

// consumeDownload counts one download before it is sent. It is atomic, so
// parallel requests can't go past the link's limit.
func (h *Handler) consumeDownload(ctx context.Context, shareID int64) error {
	result, err := h.db.ExecContext(ctx, `UPDATE shares SET download_count=download_count+1 WHERE id=? AND (max_downloads IS NULL OR download_count<max_downloads)`, shareID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err == nil && changed == 0 {
		return exhausted()
	}
	return err
}

func (h *Handler) downloadZip(w http.ResponseWriter, r *http.Request) error {
	s, root, err := h.open(r, false)
	if err != nil {
		return err
	}
	if s.Mode == "upload_only" {
		return uploadOnly()
	}
	selected, err := h.nodeInShare(r.Context(), s, root, r.URL.Query().Get("folderId"), "")
	if err != nil {
		return err
	}
	return h.files.SendSharedZIP(w, r, s.UserID, selected.ID, func() error { return h.consumeDownload(r.Context(), s.ID) })
}
