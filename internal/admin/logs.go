package admin

import (
	"net/http"

	"github.com/NeoLabs-Systems/neocloud/internal/loghistory"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// LogsRoutes registers the admin log-history endpoint independently from the
// core admin route set.
func (h *Handler) LogsRoutes(mux *http.ServeMux, history *loghistory.Store) {
	mux.HandleFunc("GET /api/admin/logs", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.authorize(w, r); !ok {
			return
		}
		var entries []loghistory.Entry
		if history != nil {
			entries = history.Entries()
		}
		if entries == nil {
			entries = []loghistory.Entry{}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"logs": entries})
	})
}
