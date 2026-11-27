package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

func (h *Handler) confirmationPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if len(token) > 200 {
		token = token[:200]
	}
	if token == "" {
		h.writeConfirmationPage(w, http.StatusBadRequest, "missing", requestLanguage(r), "")
		return
	}
	row, err := h.findConfirmationToken(r.Context(), h.db, token)
	if err != nil {
		writeError(w, 500, "Could not validate confirmation link", "INTERNAL_ERROR")
		return
	}
	if row == nil {
		h.writeConfirmationPage(w, http.StatusBadRequest, "failed", requestLanguage(r), "")
		return
	}
	// GET never consumes the credential: mail-security scanners commonly open links.
	h.writeConfirmationPage(w, http.StatusOK, "ready", requestLanguage(r), token)
}

func (h *Handler) confirmEmail(w http.ResponseWriter, r *http.Request) {
	body, err := emailPageBody(w, r)
	if err != nil {
		h.writeConfirmationPage(w, http.StatusBadRequest, "failed", requestLanguage(r), "")
		return
	}
	token := strings.TrimSpace(body.Token)
	if len(token) > 200 {
		token = token[:200]
	}
	if token == "" {
		h.writeConfirmationPage(w, http.StatusBadRequest, "missing", requestLanguage(r), "")
		return
	}
	before, err := h.findConfirmationToken(r.Context(), h.db, token)
	if err != nil {
		h.writeConfirmationPage(w, http.StatusInternalServerError, "failed", requestLanguage(r), "")
		return
	}
	state, err := h.consumeConfirmationToken(r.Context(), token)
	if err != nil {
		var authErr authError
		if errors.As(err, &authErr) {
			status := authErr.status
			if authErr.code == "LINK_INVALID" {
				state, status = "failed", http.StatusBadRequest
			}
			if authErr.code == "EMAIL_TAKEN" {
				state, status = "taken", http.StatusConflict
			}
			h.writeConfirmationPage(w, status, state, requestLanguage(r), "")
			return
		}
		h.writeConfirmationPage(w, http.StatusInternalServerError, "failed", requestLanguage(r), "")
		return
	}
	if state == "changed" && before != nil && before.currentEmail.Valid {
		u, loadErr := h.loadUser(r.Context(), before.userID)
		if loadErr == nil && u != nil {
			h.notifyEmailChange(u.Username, before.currentEmail.String, before.email, requestLanguage(r))
		}
	}
	h.writeConfirmationPage(w, http.StatusOK, state, requestLanguage(r), "")
}

type confirmationToken struct {
	id           int64
	userID       int64
	typ          string
	email        string
	currentEmail sql.NullString
}

type queryExecRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (h *Handler) findConfirmationToken(ctx context.Context, q queryExecRower, token string) (*confirmationToken, error) {
	row := &confirmationToken{}
	err := q.QueryRowContext(ctx, `SELECT t.id, t.user_id, t.type, t.email, u.email FROM user_email_tokens t
		JOIN users u ON u.id = t.user_id WHERE t.token_hash = ? AND t.type IN ('signup_confirmation', 'email_change_confirmation')
		AND t.consumed_at IS NULL AND datetime(t.expires_at) > datetime('now')`, passhash.DigestToken(token)).Scan(
		&row.id, &row.userID, &row.typ, &row.email, &row.currentEmail,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (h *Handler) consumeConfirmationToken(ctx context.Context, token string) (string, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	row, err := h.findConfirmationToken(ctx, tx, token)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", authError{status: 400, message: "Confirmation link is invalid or expired", code: "LINK_INVALID"}
	}
	sameAddress := row.currentEmail.Valid && strings.EqualFold(row.currentEmail.String, row.email)
	if row.typ == "signup_confirmation" && !sameAddress {
		return "", authError{status: 400, message: "Confirmation link is invalid or expired", code: "LINK_INVALID"}
	}
	if row.typ == "email_change_confirmation" && !sameAddress {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE lower(email) = lower(?) AND id != ?`, row.email, row.userID).Scan(&exists)
		if err == nil {
			return "", authError{status: 409, message: "Email is already in use", code: "EMAIL_TAKEN"}
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	claimed, err := tx.ExecContext(ctx, `UPDATE user_email_tokens SET consumed_at = datetime('now') WHERE id = ? AND consumed_at IS NULL`, row.id)
	if err != nil {
		return "", err
	}
	changed, err := claimed.RowsAffected()
	if err != nil {
		return "", err
	}
	if changed != 1 {
		return "", authError{status: 400, message: "Confirmation link is invalid or expired", code: "LINK_INVALID"}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET email = ?, email_verified_at = datetime('now') WHERE id = ?`, row.email, row.userID); err != nil {
		if isUniqueEmailError(err) {
			return "", authError{status: http.StatusConflict, message: "Email is already in use", code: "EMAIL_TAKEN"}
		}
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_email_tokens SET consumed_at = COALESCE(consumed_at, datetime('now'))
		WHERE user_id = ? AND type IN ('signup_confirmation', 'email_change_confirmation') AND consumed_at IS NULL`, row.userID); err != nil {
		return "", err
	}
	if row.typ == "email_change_confirmation" && !sameAddress {
		if _, err := tx.ExecContext(ctx, `UPDATE user_email_tokens SET consumed_at = COALESCE(consumed_at, datetime('now'))
			WHERE user_id = ? AND type = 'password_reset' AND consumed_at IS NULL`, row.userID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if row.typ == "email_change_confirmation" && !sameAddress {
		return "changed", nil
	}
	return "done", nil
}
