package admin

import (
	"errors"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type identityError interface {
	error
	Status() int
	Code() string
}

func (h *Handler) exportMasterKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	if h.reauthenticate == nil {
		writeError(w, http.StatusServiceUnavailable, "Identity confirmation is unavailable", "UNAVAILABLE")
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	password, _ := body["password"].(string)
	code, _ := body["code"].(string)
	if err := h.reauthenticate(r.Context(), userID, password, code); err != nil {
		var identity identityError
		if errors.As(err, &identity) {
			writeError(w, identity.Status(), identity.Error(), identity.Code())
			return
		}
		writeError(w, http.StatusInternalServerError, "Could not confirm your identity", "INTERNAL_ERROR")
		return
	}
	if h.masterKeyHex == "" {
		writeError(w, http.StatusInternalServerError, "Master key is unavailable", "INTERNAL_ERROR")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not record master key export", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,detail_json) VALUES('keys.master_export',?,'{}')`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not record master key export", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not record master key export", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"masterKey": h.masterKeyHex})
}
