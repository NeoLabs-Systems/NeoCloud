package admin

import (
	"encoding/json"
	"net/http"
)

// Gate admits administrators to a route that lives outside this package, under
// the same rules and, for changes, the same rate limit as the admin routes.
func (h *Handler) Gate(changes bool, next func(http.ResponseWriter, *http.Request, int64)) http.Handler {
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if adminID, ok := h.authorize(w, r); ok {
			next(w, r, adminID)
		}
	})
	if changes {
		handler = h.settingsLimiter.Middleware(handler)
	}
	return handler
}

// Audit records an administrator's action in the access audit log.
func (h *Handler) Audit(r *http.Request, adminID int64, action string, detail map[string]any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,detail_json) VALUES(?,?,?)`, action, adminID, string(encoded))
	return err
}
