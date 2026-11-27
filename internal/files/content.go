package files

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

// Content is one stored file as a download sends it.
type Content struct {
	BlobID, Name, MIME, ETag, ModifiedAt string
	Size                                 int64
}

func fileUnreadable() error {
	return apiError{500, "This file can’t be read: its stored data is missing or damaged.", "FILE_UNREADABLE"}
}

// SendContent answers with a file of ownerID, with Range, conditional and HEAD
// handling. Active content is always an attachment and every response is
// sandboxed, so a stored file can never run as the app. It returns an error
// only while nothing has been sent.
func (h *Handler) SendContent(w http.ResponseWriter, r *http.Request, ownerID int64, content Content, download bool) error {
	size := content.Size
	start, end, ranged, unsatisfiable := parseByteRange(r.Header.Get("Range"), size)
	conditional := etagMatches(r.Header.Get("If-None-Match"), content.ETag)
	// The first segment is read before any header is set, so a missing or
	// damaged blob is still answered as an error.
	var blob *os.File
	var dek []byte
	if !conditional && !unsatisfiable && r.Method != http.MethodHead {
		path := blobPath(h.blobDir, ownerID, content.BlobID)
		var err error
		if dek, err = h.keys.GetUserDEK(r.Context(), h.db, ownerID); err != nil {
			return fileUnreadable()
		}
		if blob, err = os.Open(path); err != nil {
			return fileUnreadable()
		}
		defer blob.Close()
		if _, err = cryptox.DecryptBlobRange(blob, size, start, start, dek, content.BlobID, io.Discard); err != nil {
			return fileUnreadable()
		}
	}
	mimeType := content.MIME
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	disposition := "inline"
	if download || activeContent(mimeType) {
		disposition = "attachment"
	}
	h.HeaderForFile(w, content.Name, mimeType, disposition, content.ETag, content.ModifiedAt)
	switch {
	case conditional:
		w.WriteHeader(http.StatusNotModified)
		return nil
	case unsatisfiable:
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}
	length := int64(0)
	if size > 0 {
		length = end - start + 1
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if ranged {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if blob == nil {
		return nil
	}
	if _, err := cryptox.DecryptBlobRange(blob, size, start, end, dek, content.BlobID, w); err != nil {
		abortResponse("file download failed", err)
	}
	return nil
}
