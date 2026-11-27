package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

const emailChangeTokenType = "email_change_confirmation"

type changeEmailRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"currentPassword"`
	Code            string `json:"code"`
}

func (h *Handler) changeEmail(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	var err error
	var body changeEmailRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !h.confirmIdentity(w, r, u.ID, body.CurrentPassword, body.Code) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if !emailPattern.MatchString(email) || len(email) > 254 {
		writeError(w, http.StatusBadRequest, "A valid email is required", "INVALID_EMAIL")
		return
	}
	if u.Email.Valid && strings.EqualFold(u.Email.String, email) {
		writeAccountPayload(w, r, h, u.ID, http.StatusOK, map[string]any{})
		return
	}
	var exists int
	err = h.db.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE lower(email) = lower(?) AND id != ? LIMIT 1`, email, u.ID).Scan(&exists)
	if err == nil {
		writeError(w, http.StatusConflict, "Email is already in use", "EMAIL_TAKEN")
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "Could not change email", "INTERNAL_ERROR")
		return
	}
	if h.emailSettings().EmailChangeConfirmationRequired {
		if !h.emailSettings().Configured || h.sendEmailConfirmation == nil {
			writeError(w, http.StatusConflict, "Email is not set up on this server", "EMAIL_NOT_CONFIGURED")
			return
		}
		count, err := h.recentTokenCount(r.Context(), u.ID, emailChangeTokenType)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not change email", "INTERNAL_ERROR")
			return
		}
		if count >= mailAccountLimit {
			writeError(w, http.StatusTooManyRequests, "Too many emails were requested for this account. Try again in a few minutes.", "RATE_LIMITED")
			return
		}
		token, err := h.issueEmailToken(r.Context(), u.ID, emailChangeTokenType, email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not change email", "INTERNAL_ERROR")
			return
		}
		if err := h.sendEmailConfirmation(r.Context(), u.Username, email, token, requestLanguage(r), true); err != nil {
			_ = h.spendEmailTokens(r.Context(), u.ID, emailChangeTokenType)
			writeError(w, http.StatusServiceUnavailable, "The email could not be sent. Try again later.", "EMAIL_SEND_FAILED")
			return
		}
		if h.sendEmailChangeRequested != nil && u.Email.Valid {
			oldEmail, username, language := u.Email.String, u.Username, requestLanguage(r)
			go func() {
				if err := h.sendEmailChangeRequested(context.Background(), username, oldEmail, email, language); err != nil {
					slog.Warn("email change notice failed", "error", err)
				}
			}()
		}
		writeAccountPayload(w, r, h, u.ID, http.StatusOK, map[string]any{"emailChangePending": true, "pendingEmail": email})
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not change email", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET email = ?, email_verified_at = NULL WHERE id = ?`, email, u.ID); err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE user_email_tokens SET consumed_at = COALESCE(consumed_at, datetime('now'))
			WHERE user_id = ? AND type IN ('signup_confirmation', 'email_change_confirmation', 'password_reset') AND consumed_at IS NULL`, u.ID)
	}
	if err != nil {
		_ = tx.Rollback()
		if isUniqueEmailError(err) {
			writeError(w, http.StatusConflict, "Email is already in use", "EMAIL_TAKEN")
		} else {
			writeError(w, http.StatusInternalServerError, "Could not change email", "INTERNAL_ERROR")
		}
		return
	}
	if err := tx.Commit(); err != nil {
		if isUniqueEmailError(err) {
			writeError(w, http.StatusConflict, "Email is already in use", "EMAIL_TAKEN")
		} else {
			writeError(w, http.StatusInternalServerError, "Could not change email", "INTERNAL_ERROR")
		}
		return
	}
	if u.Email.Valid {
		h.notifyEmailChange(u.Username, u.Email.String, email, requestLanguage(r))
	}
	writeAccountPayload(w, r, h, u.ID, http.StatusOK, map[string]any{})
}

func (h *Handler) resendEmailConfirmation(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	if !h.emailSettings().Configured || h.sendEmailConfirmation == nil {
		writeError(w, http.StatusConflict, "Email is not set up on this server", "EMAIL_NOT_CONFIGURED")
		return
	}
	pending, err := h.pendingEmail(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not resend email confirmation", "INTERNAL_ERROR")
		return
	}
	if pending == nil && (!u.Email.Valid || u.EmailVerified.Valid) {
		writeError(w, http.StatusConflict, "There is nothing to confirm", "NOTHING_TO_CONFIRM")
		return
	}
	count, err := h.recentTokenCount(r.Context(), u.ID, emailChangeTokenType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not resend email confirmation", "INTERNAL_ERROR")
		return
	}
	if count >= mailAccountLimit {
		writeError(w, http.StatusTooManyRequests, "Too many emails were requested for this account. Try again in a few minutes.", "RATE_LIMITED")
		return
	}
	address := ""
	changing := pending != nil
	if changing {
		address = *pending
	} else {
		address = u.Email.String
	}
	token, err := h.issueEmailToken(r.Context(), u.ID, emailChangeTokenType, address)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not resend email confirmation", "INTERNAL_ERROR")
		return
	}
	if err := h.sendEmailConfirmation(r.Context(), u.Username, address, token, requestLanguage(r), changing); err != nil {
		_ = h.spendEmailTokens(r.Context(), u.ID, emailChangeTokenType)
		writeError(w, http.StatusServiceUnavailable, "The email could not be sent. Try again later.", "EMAIL_SEND_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "pendingEmail": pending})
}

func (h *Handler) pendingEmail(ctx context.Context, userID int64) (*string, error) {
	var email string
	err := h.db.QueryRowContext(ctx, `SELECT t.email FROM user_email_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.user_id = ? AND t.type = ? AND t.consumed_at IS NULL AND datetime(t.expires_at) > datetime('now')
		AND lower(t.email) != lower(COALESCE(u.email, '')) ORDER BY t.id DESC LIMIT 1`, userID, emailChangeTokenType).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &email, nil
}

func (h *Handler) spendEmailTokens(ctx context.Context, userID int64, kind string) error {
	_, err := h.db.ExecContext(ctx, `UPDATE user_email_tokens SET consumed_at = datetime('now') WHERE user_id = ? AND type = ? AND consumed_at IS NULL`, userID, kind)
	return err
}

func writeAccountPayload(w http.ResponseWriter, r *http.Request, h *Handler, userID int64, status int, extra map[string]any) {
	u, err := h.loadUser(r.Context(), userID)
	if err != nil || u == nil {
		writeError(w, http.StatusInternalServerError, "Could not read account", "INTERNAL_ERROR")
		return
	}
	payload, err := h.payload(r.Context(), u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read account", "INTERNAL_ERROR")
		return
	}
	result := map[string]any{"user": payload}
	for key, value := range extra {
		result[key] = value
	}
	writeJSON(w, status, result)
}

func isUniqueEmailError(err error) bool {
	return err != nil && (strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: users.email") || strings.Contains(strings.ToLower(err.Error()), "users_email"))
}

func (h *Handler) notifyEmailChange(username, oldEmail, newEmail, language string) {
	if h.sendEmailChangedNotice == nil && h.sendEmailChangedNewNotice == nil {
		return
	}
	go func() {
		if h.sendEmailChangedNotice != nil {
			if err := h.sendEmailChangedNotice(context.Background(), username, oldEmail, newEmail, language); err != nil {
				slog.Warn("email change notice failed", "error", err)
			}
		}
		if h.sendEmailChangedNewNotice != nil {
			if err := h.sendEmailChangedNewNotice(context.Background(), username, newEmail, language); err != nil {
				slog.Warn("email change confirmation notice failed", "error", err)
			}
		}
	}()
}
