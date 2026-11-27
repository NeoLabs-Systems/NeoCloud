package admin

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// recalculateUserUsage reconciles the cached account total with its recorded
// encrypted blobs. The aggregate and update share a transaction so concurrent
// file mutations cannot leave the recalculated value stale at commit time.
func (h *Handler) recalculateUserUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	userID, ok := h.actionUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate user storage", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var bytes int64
	if err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(size),0) FROM blobs WHERE user_id=?`, userID).Scan(&bytes); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate user storage", "INTERNAL_ERROR")
		return
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE users SET used_bytes=? WHERE id=?`, bytes, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate user storage", "INTERNAL_ERROR")
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate user storage", "INTERNAL_ERROR")
		return
	}
	if changed == 0 {
		var exists int
		if err = tx.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id=?`, userID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate user storage", "INTERNAL_ERROR")
		return
	}
	// Preserve the Node endpoint's response: the freshly serialized user.
	h.writeUser(w, r, userID)
}

func (h *Handler) recalculateAllUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate storage", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var users int
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not count users", "INTERNAL_ERROR")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET used_bytes=COALESCE((SELECT SUM(size) FROM blobs WHERE blobs.user_id=users.id),0)`); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate storage", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not recalculate storage", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "users": users})
}
