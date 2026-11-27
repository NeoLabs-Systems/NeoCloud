package auth

import (
	"crypto/hmac"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

const browserSessionLifetime = 30 * 24 * time.Hour

// recordSession lists the browser session under the account, so it can be
// shown and revoked without storing its id in readable form.
func (h *Handler) recordSession(r *http.Request, userID int64, session *sessionx.Session) error {
	_, err := h.db.ExecContext(r.Context(), `INSERT INTO user_sessions (user_id, session_hash, ip_address, user_agent, expires_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(session_hash) DO UPDATE SET ip_address = excluded.ip_address, user_agent = excluded.user_agent,
		last_seen_at = datetime('now'), expires_at = excluded.expires_at`, userID, sessionx.HashID(h.secret, session.ID()),
		nullableText(ratelimit.ClientIP(r)), truncateRunes(r.UserAgent(), 500),
		h.now().Add(browserSessionLifetime).UTC().Format("2006-01-02T15:04:05.000Z"))
	return err
}

type accountSession struct {
	ID         int64   `json:"id"`
	Current    bool    `json:"current"`
	IPAddress  *string `json:"ipAddress"`
	UserAgent  string  `json:"userAgent"`
	CreatedAt  string  `json:"createdAt"`
	LastSeenAt string  `json:"lastSeenAt"`
	ExpiresAt  *string `json:"expiresAt"`
}

func (h *Handler) listAccountSessions(w http.ResponseWriter, r *http.Request) {
	u, identity, ok := h.accountRequestUser(w, r)
	if !ok {
		return
	}
	// A phone app request carries no session anybody signed in to.
	currentHash := ""
	if !identity.IsDevice {
		session := sessionx.FromContext(r.Context())
		currentHash = sessionx.HashID(h.secret, session.ID())
		if err := h.recordSession(r, u.ID, session); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not read sessions", "INTERNAL_ERROR")
			return
		}
	}
	sessions, err := h.loadAccountSessions(r, u.ID, currentHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read sessions", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h *Handler) loadAccountSessions(r *http.Request, userID int64, currentHash string) ([]accountSession, error) {
	rows, err := h.db.QueryContext(r.Context(), `SELECT id, session_hash, ip_address, user_agent, created_at, last_seen_at, expires_at
		FROM user_sessions WHERE user_id = ? AND revoked_at IS NULL
		AND (expires_at IS NULL OR datetime(expires_at) > datetime('now'))
		ORDER BY datetime(last_seen_at) DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]accountSession, 0)
	for rows.Next() {
		var item accountSession
		var hash string
		var ip, agent, expiresAt sql.NullString
		if err := rows.Scan(&item.ID, &hash, &ip, &agent, &item.CreatedAt, &item.LastSeenAt, &expiresAt); err != nil {
			return nil, err
		}
		item.Current = hmac.Equal([]byte(hash), []byte(currentHash))
		item.IPAddress = nullableString(ip)
		item.UserAgent = agent.String
		item.ExpiresAt = nullableString(expiresAt)
		sessions = append(sessions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (h *Handler) revokeAccountSession(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "Invalid session id", "INVALID_ID")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	fail := func(status int, message, code string) {
		_ = tx.Rollback()
		writeError(w, status, message, code)
	}
	var hash string
	err = tx.QueryRowContext(r.Context(), `SELECT session_hash FROM user_sessions
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, u.ID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		fail(http.StatusNotFound, "Session not found", "NOT_FOUND")
		return
	}
	if err != nil {
		fail(http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	if session := sessionx.FromContext(r.Context()); session != nil && session.ID() != "" && hmac.Equal([]byte(hash), []byte(sessionx.HashID(h.secret, session.ID()))) {
		fail(http.StatusBadRequest, "Use sign out to end the current session", "CURRENT_SESSION")
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT sid FROM sessions`)
	if err != nil {
		fail(http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	var matching []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			_ = rows.Close()
			fail(http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
			return
		}
		candidate := sessionx.HashID(h.secret, sid)
		if hmac.Equal([]byte(candidate), []byte(hash)) {
			matching = append(matching, sid)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		fail(http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	if err := rows.Close(); err != nil {
		fail(http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	for _, sid := range matching {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE sid = ?`, sid); err != nil {
			fail(http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE user_sessions SET revoked_at = datetime('now') WHERE id = ?`, id); err != nil {
		fail(http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	session := sessionx.FromContext(r.Context())
	currentHash := ""
	if session != nil {
		currentHash = sessionx.HashID(h.secret, session.ID())
	}
	sessions, err := h.loadAccountSessions(r, u.ID, currentHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not revoke session", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "sessions": sessions})
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
