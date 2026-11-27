package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, ok, err := h.authenticatedUser(r)
	if err != nil {
		writeError(w, 500, "Could not read account", "INTERNAL_ERROR")
		return
	}
	if !ok {
		writeError(w, 401, "Not authenticated", "UNAUTHORIZED")
		return
	}
	payload, err := h.payload(r.Context(), u)
	if err != nil {
		writeError(w, 500, "Could not read account", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, 200, map[string]any{"user": payload})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	s := sessionx.FromContext(r.Context())
	if s == nil {
		writeError(w, 500, "Session middleware is not configured", "INTERNAL_ERROR")
		return
	}
	if id, ok := s.Int64("userId"); ok {
		hash := sessionx.HashID(h.secret, s.ID())
		if _, err := h.db.ExecContext(r.Context(), `UPDATE user_sessions SET revoked_at = datetime('now') WHERE user_id = ? AND session_hash = ? AND revoked_at IS NULL`, id, hash); err != nil {
			writeError(w, 500, "Could not revoke session", "INTERNAL_ERROR")
			return
		}
	}
	s.Destroy()
	http.SetCookie(w, &http.Cookie{Name: "neocloud.sid", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.requestSecure(r), MaxAge: -1, Expires: time.Unix(1, 0)})
	writeJSON(w, 200, map[string]any{"success": true})
}

func (h *Handler) requestSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func (h *Handler) requireSecureTransport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.secure || h.requestSecure(r) {
			next.ServeHTTP(w, r)
			return
		}
		httpsURL := ""
		if origin, err := ParseOrigin(h.publicURL); err == nil && strings.HasPrefix(origin, "https://") {
			httpsURL = origin
		} else if r.Host != "" {
			httpsURL = "https://" + r.Host
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "Sign-in needs a secure connection. Open " + httpsURL + " instead.",
			"code":  "INSECURE_TRANSPORT", "httpsUrl": httpsURL,
		})
	})
}

// signInNoticeCooldown keeps a browser that changes address from mailing the
// owner on every sign-in.
const signInNoticeCooldown = time.Hour

// noticeNewSignIn tells the owner about a sign-in from an address and browser
// none of the recorded sessions used. Call it before the new session is
// recorded; the first sign-in of an account says nothing.
func (h *Handler) noticeNewSignIn(r *http.Request, u *user) {
	if h.sendNewSignInNotice == nil || !u.Email.Valid || u.Email.String == "" {
		return
	}
	ip, agent := ratelimit.ClientIP(r), truncateRunes(r.UserAgent(), 500)
	var seen, matching int
	err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*), COUNT(CASE WHEN COALESCE(ip_address,'')=? AND COALESCE(user_agent,'')=? THEN 1 END) FROM user_sessions WHERE user_id=?`, ip, agent, u.ID).Scan(&seen, &matching)
	if err != nil || seen == 0 || matching > 0 {
		return
	}
	now := h.now()
	if last, ok := h.signInNotices.Load(u.ID); ok && now.Sub(last.(time.Time)) < signInNoticeCooldown {
		return
	}
	h.signInNotices.Store(u.ID, now)
	username, email, language := u.Username, u.Email.String, requestLanguage(r)
	go func() {
		if err := h.sendNewSignInNotice(context.Background(), username, email, ip, truncateRunes(agent, 120), language); err != nil {
			slog.Warn("new sign-in notice failed", "error", err)
		}
	}()
}
