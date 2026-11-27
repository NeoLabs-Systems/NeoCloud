package files

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neocloud/internal/documentconvert"
)

const (
	officePreviewVariant = "pdf"
	officePreviewMIME    = "application/pdf"
)

var (
	errNoDocumentPreview = apiError{http.StatusUnsupportedMediaType, "No document preview for this file type.", "PREVIEW_UNAVAILABLE"}
	errNoLibreOffice     = apiError{http.StatusUnsupportedMediaType, "Office previews need LibreOffice on the server.", "PREVIEW_UNAVAILABLE"}
	errDocumentSize      = apiError{http.StatusUnsupportedMediaType, "File is too large to preview.", "PREVIEW_UNAVAILABLE"}
	errUnconvertible     = apiError{http.StatusUnsupportedMediaType, "This document could not be converted for preview.", "PREVIEW_UNAVAILABLE"}
)

// officePreview answers with a PDF rendition of an Office document.
func (h *Handler) officePreview(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	source, err := h.previewSource(r.Context(), uid, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if !documentconvert.Supports(source.name) {
		fail(w, errNoDocumentPreview)
		return
	}
	h.sendRendition(w, r, source, officePreviewVariant, officePreviewMIME, func(ctx context.Context) ([]byte, error) {
		return h.renderOfficePDF(ctx, source)
	})
}

func (h *Handler) renderOfficePDF(ctx context.Context, source previewSource) ([]byte, error) {
	if !documentconvert.Available() {
		return nil, errNoLibreOffice
	}
	if source.size > documentconvert.MaxSourceBytes {
		return nil, errDocumentSize
	}
	plain, err := h.readPreviewSource(ctx, source)
	if err == nil {
		var pdf []byte
		if pdf, err = documentconvert.ConvertToPDF(ctx, source.name, bytes.NewReader(plain)); err == nil {
			return pdf, nil
		}
	}
	slog.Warn("office conversion failed", "node_id", source.nodeID, "error", err)
	return nil, errUnconvertible
}
