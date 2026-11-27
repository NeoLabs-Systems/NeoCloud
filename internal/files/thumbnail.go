package files

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
)

// Renditions of files, made on first request and cached encrypted: WebP
// thumbnails of pictures (here) and the PDF of an Office document
// (office_preview.go). A rendition belongs to a blob, not a node, so a new
// file version gets new previews and the old ones go with the old blob.
const (
	maxThumbnailSourceBytes = int64(200 << 20)
	squareThumbnailVariant  = "thumb-square"
	// Grid tiles are about 100–140 points wide; 320 pixels is sharp on a phone.
	squareThumbnailPixels = 320
	thumbnailMIME         = "image/webp"
)

var (
	errNoThumbnail     = apiError{http.StatusUnsupportedMediaType, "No thumbnail for this file type.", "PREVIEW_UNAVAILABLE"}
	errThumbnailSize   = apiError{http.StatusUnsupportedMediaType, "File is too large for a thumbnail.", "PREVIEW_UNAVAILABLE"}
	errUnreadableImage = apiError{http.StatusUnsupportedMediaType, "This image could not be read.", "PREVIEW_UNAVAILABLE"}
)

type thumbnailSize struct {
	variant string
	edge    int
	quality int
}

func thumbnailVariant(raw string) (thumbnailSize, error) {
	switch raw {
	case "", "256":
		return thumbnailSize{"thumb-256", 256, 78}, nil
	case "1024":
		return thumbnailSize{"thumb-1024", 1024, 78}, nil
	case "square":
		return thumbnailSize{squareThumbnailVariant, squareThumbnailPixels, 70}, nil
	default:
		return thumbnailSize{}, apiError{http.StatusBadRequest, "size must be 256, 1024 or square.", "INVALID_SIZE"}
	}
}

func (h *Handler) thumbnail(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	size, err := thumbnailVariant(r.URL.Query().Get("size"))
	if err != nil {
		fail(w, err)
		return
	}
	source, err := h.previewSource(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	h.sendRendition(w, r, source, size.variant, thumbnailMIME, func(ctx context.Context) ([]byte, error) {
		return h.renderThumbnail(ctx, source, size)
	})
}

// PrepareGridThumbnail makes the grid thumbnail of a picture ahead of time, so
// that the first scroll through the photo timeline does not wait for renders.
// A file with no thumbnail is skipped.
func (h *Handler) PrepareGridThumbnail(ctx context.Context, ownerID int64, nodeID string) error {
	source, err := h.previewSource(ctx, ownerID, nodeID)
	if err == nil && imaging.Supports(source.mime) && source.size <= maxThumbnailSourceBytes {
		size, _ := thumbnailVariant("square")
		_, err = h.rendition(ctx, source, size.variant, thumbnailMIME, func(ctx context.Context) ([]byte, error) {
			return h.renderThumbnail(ctx, source, size)
		})
	}
	var refused apiError
	if errors.As(err, &refused) && refused.Status < http.StatusInternalServerError {
		return nil
	}
	return err
}

func (h *Handler) renderThumbnail(ctx context.Context, source previewSource, size thumbnailSize) ([]byte, error) {
	if !imaging.Supports(source.mime) {
		return nil, errNoThumbnail
	}
	if source.size > maxThumbnailSourceBytes {
		return nil, errThumbnailSize
	}
	plain, err := h.readPreviewSource(ctx, source)
	if err != nil {
		slog.Warn("could not read a file for its thumbnail", "node_id", source.nodeID, "error", err)
		return nil, errUnreadableImage
	}
	picture, err := imaging.Decode(ctx, plain)
	if err != nil {
		slog.Warn("thumbnail failed", "mime", source.mime, "node_id", source.nodeID, "error", err)
		return nil, errUnreadableImage
	}
	if size.variant == squareThumbnailVariant {
		return imaging.EncodeWebP(picture.Cover(size.edge), size.quality)
	}
	return imaging.EncodeWebP(picture.Fit(size.edge), size.quality)
}

// previewSource is the live file a rendition is made from.
type previewSource struct {
	ownerID                          int64
	nodeID, blobID, name, mime, etag string
	size                             int64
}

// previewSource resolves a file the viewer owns or that is shared with them.
func (h *Handler) previewSource(ctx context.Context, viewer int64, nodeID string) (previewSource, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return previewSource{}, err
	}
	defer tx.Rollback()
	found, shared, err := h.resolveReadNode(tx, viewer, nodeID)
	if err != nil {
		return previewSource{}, err
	}
	if found.Type != "file" {
		return previewSource{}, apiError{http.StatusBadRequest, "That is not a file.", "NOT_A_FILE"}
	}
	// Vault files are ciphertext: there is nothing to render.
	if found.VaultID != nil {
		return previewSource{}, apiError{http.StatusNotFound, "No preview for vault files.", "NOT_FOUND"}
	}
	source := previewSource{ownerID: viewer, nodeID: found.ID, name: found.Name, etag: found.Etag, size: found.Size}
	if shared != nil {
		source.ownerID = shared.ownerID
	}
	if found.Mime != nil {
		source.mime = *found.Mime
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(blob_id,'') FROM nodes WHERE id=? AND user_id=?`, found.ID, source.ownerID).Scan(&source.blobID)
	if errors.Is(err, sql.ErrNoRows) {
		return previewSource{}, apiError{http.StatusNotFound, "File not found", "NOT_FOUND"}
	}
	return source, err
}

// sendRendition answers with a cached or freshly made rendition. Renditions
// never change for a given content, so clients may cache them.
func (h *Handler) sendRendition(w http.ResponseWriter, r *http.Request, source previewSource, variant, mime string, render func(context.Context) ([]byte, error)) {
	tag := source.etag + "-" + variant
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		w.Header().Set("ETag", `"`+tag+`"`)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, err := h.rendition(r.Context(), source, variant, mime, render)
	if err != nil {
		fail(w, err)
		return
	}
	// A URL naming the content (?v=<etag>) never shows anything else, so the
	// client may keep it for good.
	cacheControl := "private, max-age=86400"
	if r.URL.Query().Get("v") == source.etag {
		cacheControl = "private, max-age=31536000, immutable"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("ETag", `"`+tag+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", transferSandboxCSP)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
