package admin

import (
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

func (h *Handler) searchIndexStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	if h.onSearchIndexStatus == nil {
		writeError(w, http.StatusServiceUnavailable, "Search indexing is not available.", "SERVICE_UNAVAILABLE")
		return
	}
	result, err := h.onSearchIndexStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read search-index status.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) rebuildSearchIndex(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	if h.onSearchIndexRebuild == nil {
		writeError(w, http.StatusServiceUnavailable, "Search indexing is not available.", "SERVICE_UNAVAILABLE")
		return
	}
	result, err := h.onSearchIndexRebuild(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not rebuild the search index.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, result)
}
