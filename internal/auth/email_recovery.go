package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

const (
	mailWindow       = 15 * time.Minute
	mailAccountLimit = 3
	anonymousMailMin = 350 * time.Millisecond
	resetTokenType   = "password_reset"
	signupTokenType  = "signup_confirmation"
)

func (h *Handler) resendSignupConfirmation(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if account, ok := accountRequest(w, r); ok {
		account = strings.TrimSpace(truncate(account, 254))
		language := requestLanguage(r)
		if h.emailSettings().Configured && h.emailSettings().SignupConfirmationRequired && account != "" && h.sendSignupConfirmation != nil {
			if u, err := h.findUser(r.Context(), account); err == nil && u != nil && u.Email.Valid && !u.DisabledAt.Valid && !u.EmailVerified.Valid {
				count, countErr := h.recentTokenCount(r.Context(), u.ID, signupTokenType)
				if h.awaitingSignupConfirmation(r.Context(), u.ID) && countErr == nil && count < mailAccountLimit {
					if token, err := h.issueEmailToken(r.Context(), u.ID, signupTokenType, u.Email.String); err == nil {
						go func() {
							if err := h.sendSignupConfirmation(context.Background(), u.Username, u.Email.String, token, language); err != nil {
								slog.Warn("signup confirmation resend failed", "error", err)
							}
						}()
					}
				}
			}
		}
		h.padAnonymousMail(started)
		message := "If that account is waiting for confirmation, " + h.emailBrand() + " sends a new confirmation link."
		if requestLanguage(r) == "de" {
			message = "Wartet dieses Konto auf die Bestätigung, schickt " + h.emailBrand() + " einen neuen Bestätigungslink."
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": message})
	}
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	account, ok := accountRequest(w, r)
	if !ok {
		return
	}
	account = strings.TrimSpace(truncate(account, 254))
	language := requestLanguage(r)
	if h.emailSettings().Configured && account != "" && h.sendPasswordReset != nil {
		if u, err := h.findUser(r.Context(), account); err == nil && u != nil && u.Email.Valid && !u.DisabledAt.Valid {
			// A token left over from when confirmation was required must not
			// suppress recovery after the operator disables that policy. Match
			// the Node flow, which only gates reset on an enforced confirmation
			// requirement.
			awaiting := h.emailSettings().SignupConfirmationRequired && h.awaitingSignupConfirmation(r.Context(), u.ID)
			count, countErr := h.recentTokenCount(r.Context(), u.ID, resetTokenType)
			if !awaiting && countErr == nil && count < mailAccountLimit {
				if token, err := h.issueEmailToken(r.Context(), u.ID, resetTokenType, u.Email.String); err == nil {
					go func() {
						if err := h.sendPasswordReset(context.Background(), u.Username, u.Email.String, token, language); err != nil {
							slog.Warn("password reset email failed", "error", err)
						}
					}()
				}
			}
		}
	}
	h.padAnonymousMail(started)
	message := "If that account has a confirmed email, " + h.emailBrand() + " will send a password reset link."
	if language == "de" {
		message = "Hat dieses Konto eine bestätigte E-Mail, schickt " + h.emailBrand() + " einen Link zum Zurücksetzen des Passworts."
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": message})
}

func accountRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body any
	if !decodeJSON(w, r, &body) {
		return "", false
	}
	fields, ok := body.(map[string]any)
	if !ok {
		return "", true
	}
	account, _ := fields["account"].(string)
	return account, true
}

func (h *Handler) padAnonymousMail(started time.Time) {
	if remaining := anonymousMailMin - time.Since(started); remaining > 0 {
		time.Sleep(remaining)
	}
}

func (h *Handler) awaitingSignupConfirmation(ctx context.Context, userID int64) bool {
	var exists int
	err := h.db.QueryRowContext(ctx, `SELECT 1 FROM user_email_tokens WHERE user_id = ? AND type = ? AND consumed_at IS NULL LIMIT 1`, userID, signupTokenType).Scan(&exists)
	return err == nil
}

func (h *Handler) recentTokenCount(ctx context.Context, userID int64, kind string) (int, error) {
	var count int
	err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_email_tokens WHERE user_id = ? AND type = ? AND datetime(created_at) > datetime(?)`,
		userID, kind, h.now().Add(-mailWindow).UTC().Format("2006-01-02 15:04:05")).Scan(&count)
	return count, err
}

func (h *Handler) issueEmailToken(ctx context.Context, userID int64, kind, email string) (string, error) {
	token, err := passhash.RandomToken(32)
	if err != nil {
		return "", err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE user_email_tokens SET consumed_at = datetime('now') WHERE user_id = ? AND type = ? AND consumed_at IS NULL`, userID, kind); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_email_tokens (user_id, type, email, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		userID, kind, email, passhash.DigestToken(token), h.now().Add(h.signupTTL()).UTC().Format("2006-01-02 15:04:05")); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (h *Handler) passwordResetPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(truncate(r.URL.Query().Get("token"), 200))
	if token == "" {
		h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "missing", "", "")
		return
	}
	row, err := h.findPasswordResetToken(r.Context(), h.db, token)
	if err != nil {
		h.writeResetPage(w, http.StatusInternalServerError, requestLanguage(r), "failed", "", "")
		return
	} else if row == nil {
		h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "failed", "", "")
		return
	}
	h.writeResetPage(w, http.StatusOK, requestLanguage(r), "form", token, "")
}

type emailPagePostBody struct {
	Token           string  `json:"token"`
	Password        string  `json:"password"`
	ConfirmPassword *string `json:"confirmPassword"`
}

// emailPageBody accepts the same JSON and URL-encoded bodies as the Node
// middleware while keeping browser form submissions intact.
func emailPageBody(w http.ResponseWriter, r *http.Request) (emailPagePostBody, error) {
	var body emailPagePostBody
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return body, err
	}
	if mediaType == "application/json" {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err = decoder.Decode(&body); err != nil {
			return body, err
		}
		return body, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err = r.ParseForm(); err != nil {
		return body, err
	}
	body.Token = r.Form.Get("token")
	body.Password = r.Form.Get("password")
	if values, exists := r.Form["confirmPassword"]; exists {
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		body.ConfirmPassword = &value
	}
	return body, nil
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	body, err := emailPageBody(w, r)
	if err != nil {
		h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "failed", "", "")
		return
	}
	token := strings.TrimSpace(truncate(body.Token, 200))
	password := body.Password
	confirmation := password
	if body.ConfirmPassword != nil {
		confirmation = *body.ConfirmPassword
	}
	if token == "" {
		h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "missing", "", "")
		return
	}
	if password != confirmation {
		h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "form", token, h.newMailPage(requestLanguage(r)).T.Mismatch)
		return
	}
	row, err := h.findPasswordResetToken(r.Context(), h.db, token)
	if err != nil {
		h.writeResetPage(w, http.StatusInternalServerError, requestLanguage(r), "failed", "", "")
		return
	}
	if row == nil {
		h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "failed", "", "")
		return
	}
	if password == "" {
		h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "form", token, "A new password is required.")
		return
	}
	if err := h.validatePassword(row.username, row.currentEmail, password); err != nil {
		var authErr authError
		if errors.As(err, &authErr) {
			h.writeResetPage(w, authErr.status, requestLanguage(r), "form", token, authErr.message)
			return
		}
		h.writeResetPage(w, http.StatusInternalServerError, requestLanguage(r), "failed", "", "")
		return
	}
	hash, err := passhash.Hash(password, passhash.DefaultCost)
	if err != nil {
		h.writeResetPage(w, http.StatusInternalServerError, requestLanguage(r), "failed", "", "")
		return
	}
	if err := h.applyPasswordReset(r.Context(), row, hash); err != nil {
		if errors.Is(err, errResetLinkInvalid) {
			h.writeResetPage(w, http.StatusBadRequest, requestLanguage(r), "failed", "", "")
		} else {
			h.writeResetPage(w, http.StatusInternalServerError, requestLanguage(r), "failed", "", "")
		}
		return
	}
	h.cancelDeviceHandoffs(r.Context(), row.userID, 0, 0)
	if h.sendPasswordChangedNotice != nil {
		language := requestLanguage(r)
		go func() {
			if err := h.sendPasswordChangedNotice(context.Background(), row.username, row.email, language); err != nil {
				slog.Warn("password change notice failed", "error", err)
			}
		}()
	}
	h.writeResetPage(w, http.StatusOK, requestLanguage(r), "done", "", "")
}

var errResetLinkInvalid = errors.New("password reset link is invalid or expired")

type resetToken struct {
	id           int64
	userID       int64
	username     string
	email        string // address the reset link was sent to
	currentEmail string
}

func (h *Handler) findPasswordResetToken(ctx context.Context, q queryExecRower, token string) (*resetToken, error) {
	row := &resetToken{}
	err := q.QueryRowContext(ctx, `SELECT t.id, t.user_id, u.username, t.email, u.email FROM user_email_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.type = ? AND t.consumed_at IS NULL AND datetime(t.expires_at) > datetime('now')`,
		passhash.DigestToken(token), resetTokenType).Scan(&row.id, &row.userID, &row.username, &row.email, &row.currentEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

func (h *Handler) applyPasswordReset(ctx context.Context, row *resetToken, hash string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	claimed, err := tx.ExecContext(ctx, `UPDATE user_email_tokens SET consumed_at = datetime('now') WHERE id = ? AND consumed_at IS NULL AND datetime(expires_at) > datetime('now')`, row.id)
	if err != nil {
		return err
	}
	if n, _ := claimed.RowsAffected(); n != 1 {
		return errResetLinkInvalid
	}
	updated, err := tx.ExecContext(ctx, `UPDATE users SET password = ?, email_verified_at = COALESCE(email_verified_at, datetime('now')) WHERE id = ? AND lower(email) = lower(?)`, hash, row.userID, row.email)
	if err != nil {
		return err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return errResetLinkInvalid
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_email_tokens SET consumed_at = COALESCE(consumed_at, datetime('now')) WHERE user_id = ? AND type IN ('password_reset', 'signup_confirmation') AND consumed_at IS NULL`, row.userID); err != nil {
		return err
	}
	if err := RevokeCredentials(ctx, tx, row.userID, "", "", 0); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_failures WHERE account_key IN (?, ?)`, "user:"+strconv.FormatInt(row.userID, 10), "confirm:user:"+strconv.FormatInt(row.userID, 10)); err != nil {
		return err
	}
	return tx.Commit()
}
