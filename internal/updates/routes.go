package updates

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// Gate admits only administrators and passes their user id on. changes marks
// requests that alter the server, which are rate limited.
type Gate func(changes bool, next func(http.ResponseWriter, *http.Request, int64)) http.Handler

// Audit records an administrator's action.
type Audit func(r *http.Request, adminID int64, action string, detail map[string]any) error

// Routes registers the administrator update endpoints.
func (s *Service) Routes(mux *http.ServeMux, gate Gate, audit Audit) {
	mux.Handle("GET /api/admin/update", gate(false, func(w http.ResponseWriter, _ *http.Request, _ int64) {
		httpx.WriteJSON(w, http.StatusOK, s.Status())
	}))
	mux.Handle("POST /api/admin/update/check", gate(true, func(w http.ResponseWriter, r *http.Request, _ int64) {
		httpx.WriteJSON(w, http.StatusOK, s.Check(r.Context()))
	}))
	mux.Handle("POST /api/admin/update", gate(true, func(w http.ResponseWriter, r *http.Request, adminID int64) {
		status, err := s.Start()
		if err != nil {
			writeError(w, err)
			return
		}
		if err := audit(r, adminID, "server.update.started", map[string]any{"version": status.Version, "channel": status.Channel}); err != nil {
			slog.Error("Could not record the update in the audit log", "error", err)
		}
		httpx.WriteJSON(w, http.StatusAccepted, status)
	}))
	mux.Handle("PUT /api/admin/update/channel", gate(true, func(w http.ResponseWriter, r *http.Request, adminID int64) {
		var body struct {
			Channel string `json:"channel"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeError(w, errChannel)
			return
		}
		status, err := s.SetChannel(r.Context(), body.Channel)
		if err != nil {
			writeError(w, err)
			return
		}
		if err := audit(r, adminID, "server.update.channel", map[string]any{"channel": status.Channel}); err != nil {
			slog.Error("Could not record the channel change in the audit log", "error", err)
		}
		httpx.WriteJSON(w, http.StatusOK, status)
	}))
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errContainer):
		httpx.ErrorCode(w, http.StatusConflict, err.Error(), "UPDATE_CONTAINER")
	case errors.Is(err, errRunning):
		httpx.ErrorCode(w, http.StatusConflict, err.Error(), "UPDATE_RUNNING")
	case errors.Is(err, errChannel):
		httpx.ErrorCode(w, http.StatusBadRequest, err.Error(), "INVALID_CHANNEL")
	default:
		slog.Error("Update request failed", "error", err)
		httpx.ErrorCode(w, http.StatusInternalServerError, "The update could not be started.", "INTERNAL_ERROR")
	}
}
