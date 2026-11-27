package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

var adminTestEmailPattern = regexp.MustCompile(`^[^[:space:]@]+@[^[:space:]@]+[.][^[:space:]@]+$`)

// EmailTestRoutes registers the explicit SMTP test action with a much lower
// limit than ordinary settings writes because each request can deliver mail.
func (h *Handler) EmailTestRoutes(mux *http.ServeMux) {
	limiter := ratelimit.New(ratelimit.Config{
		Window: 15 * time.Minute,
		Max:    5,
		Message: map[string]string{
			"error": "Too many test emails, try again later",
			"code":  "RATE_LIMITED",
		},
	})
	mux.Handle("POST /api/admin/config/email/test", limiter.Middleware(http.HandlerFunc(h.sendEmailTest)))
}

func (h *Handler) sendEmailTest(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	view, err := h.readEmailSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read email settings.", "INTERNAL_ERROR")
		return
	}
	if !view.Configured {
		writeError(w, http.StatusConflict, "Set the sender address and SMTP host first.", "EMAIL_NOT_CONFIGURED")
		return
	}
	if h.onSendTestEmail == nil {
		writeError(w, http.StatusServiceUnavailable, "Email delivery is unavailable.", "EMAIL_UNAVAILABLE")
		return
	}
	body := map[string]any{}
	if r.ContentLength != 0 {
		var valid bool
		body, valid = decodeObject(w, r)
		if !valid {
			return
		}
	}
	address, _ := body["to"].(string)
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		if err := h.db.QueryRowContext(r.Context(), `SELECT COALESCE(email,'') FROM users WHERE id=?`, actorID).Scan(&address); err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "Could not read the administrator account.", "INTERNAL_ERROR")
			return
		}
		address = strings.ToLower(strings.TrimSpace(address))
	}
	if address == "" {
		writeError(w, http.StatusBadRequest, "Your account has no email address: enter one to send the test to.", "EMAIL_REQUIRED")
		return
	}
	if len(address) > 254 || !adminTestEmailPattern.MatchString(address) {
		writeError(w, http.StatusBadRequest, "A valid email is required", "INVALID_EMAIL")
		return
	}
	if _, err := h.onSendTestEmail(r.Context(), address, r.Header.Get("Accept-Language")); err != nil {
		if auditErr := h.recordEmailTest(r.Context(), actorID, false); auditErr != nil {
			writeError(w, http.StatusInternalServerError, "Could not record the email test result.", "INTERNAL_ERROR")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "EMAIL_TEST_FAILED")
		return
	}
	if err := h.recordEmailTest(r.Context(), actorID, true); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not record the email test result.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Test email sent to " + address + "."})
}

func (h *Handler) recordEmailTest(ctx context.Context, actorID int64, ok bool) error {
	detail, err := json.Marshal(map[string]bool{"ok": ok})
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,detail_json) VALUES('email.test',?,?)`, actorID, string(detail))
	return err
}
