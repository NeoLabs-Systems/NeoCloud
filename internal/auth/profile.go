package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) account(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	payload, err := h.payload(r.Context(), u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read account", "INTERNAL_ERROR")
		return
	}
	twoFactor, err := h.twoFactorStatus(r, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read account", "INTERNAL_ERROR")
		return
	}
	keys, err := h.securityKeys(r, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read account", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": payload, "twoFactor": twoFactor, "securityKeys": keys})
}

func (h *Handler) accountUsage(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	payload, err := h.payload(r.Context(), u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read account usage", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, payload.Storage)
}

func (h *Handler) updateDisplayName(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if !decodeJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(displayNameString(body["displayName"]))
	name = truncateRunes(name, 80)
	var value any
	if name != "" {
		value = name
	}
	if _, err := h.db.ExecContext(r.Context(), `UPDATE users SET display_name = ? WHERE id = ?`, value, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account", "INTERNAL_ERROR")
		return
	}
	u, err := h.loadUser(r.Context(), u.ID)
	if err != nil || u == nil {
		writeError(w, http.StatusInternalServerError, "Could not update account", "INTERNAL_ERROR")
		return
	}
	payload, err := h.payload(r.Context(), u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update account", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": payload})
}

func displayNameString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		items := make([]string, len(v))
		for i, item := range v {
			if item != nil {
				items[i] = displayNameString(item)
			}
		}
		return strings.Join(items, ",")
	case map[string]any:
		return "[object Object]"
	default:
		return fmt.Sprint(v)
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

type twoFactorStatus struct {
	Enabled                bool    `json:"enabled"`
	Pending                bool    `json:"pending"`
	EnabledAt              *string `json:"enabledAt"`
	RecoveryCodesRemaining int     `json:"recoveryCodesRemaining"`
}

func (h *Handler) twoFactorStatus(r *http.Request, userID int64) (twoFactorStatus, error) {
	var enabled int
	var pending, enabledAt sql.NullString
	err := h.db.QueryRowContext(r.Context(), `SELECT enabled, pending_secret, enabled_at FROM user_two_factor WHERE user_id = ?`, userID).Scan(&enabled, &pending, &enabledAt)
	if err != nil && err != sql.ErrNoRows {
		return twoFactorStatus{}, err
	}
	var remaining int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&remaining); err != nil {
		return twoFactorStatus{}, err
	}
	return twoFactorStatus{Enabled: enabled == 1, Pending: pending.Valid && pending.String != "", EnabledAt: nullableString(enabledAt), RecoveryCodesRemaining: remaining}, nil
}

func (h *Handler) securityKeys(r *http.Request, userID int64) ([]map[string]any, error) {
	rows, err := h.db.QueryContext(r.Context(), `SELECT id, label, rp_id, transports_json, backed_up, created_at, last_used_at
		FROM user_webauthn_credentials WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var label, rpID, transportsJSON, createdAt sql.NullString
		var backedUp int
		var lastUsed sql.NullString
		if err := rows.Scan(&id, &label, &rpID, &transportsJSON, &backedUp, &createdAt, &lastUsed); err != nil {
			return nil, err
		}
		transports := make([]string, 0)
		if transportsJSON.Valid {
			_ = json.Unmarshal([]byte(transportsJSON.String), &transports)
		}
		keys = append(keys, map[string]any{
			"id": id, "label": label.String, "rpId": rpID.String, "transports": transports,
			"backedUp": backedUp == 1, "createdAt": nullableString(createdAt), "lastUsedAt": nullableString(lastUsed),
		})
	}
	return keys, rows.Err()
}
