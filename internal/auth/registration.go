package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

type registrationRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var body registrationRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	username := strings.TrimSpace(body.Username)
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if username == "" || email == "" || body.Password == "" {
		writeError(w, 400, "Username, email, and password required", "MISSING_FIELDS")
		return
	}
	count, err := h.userCount(r.Context())
	if err != nil {
		writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		return
	}
	if count > 0 && !h.signupIsOpen() {
		writeError(w, 403, "Registration is closed. Ask the person who runs this NeoCloud for an account.", "SIGNUP_DISABLED")
		return
	}
	if err := h.validateUsername(r.Context(), username); err != nil {
		writeAuthError(w, err)
		return
	}
	if len(email) > 254 || !emailPattern.MatchString(email) {
		writeError(w, 400, "A valid email is required", "INVALID_EMAIL")
		return
	}
	if err := h.validatePassword(username, email, body.Password); err != nil {
		writeAuthError(w, err)
		return
	}
	hash, err := passhash.Hash(body.Password, passhash.DefaultCost)
	if err != nil {
		writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		return
	}
	if count > 0 && !h.signupIsOpen() {
		writeError(w, 403, "Registration is closed. Ask the person who runs this NeoCloud for an account.", "SIGNUP_DISABLED")
		return
	}
	if err := h.validateUsernameWith(r.Context(), tx, username); err != nil {
		writeAuthError(w, err)
		return
	}
	if err := validateEmailAvailable(r.Context(), tx, email); err != nil {
		writeAuthError(w, err)
		return
	}
	result, err := tx.ExecContext(r.Context(), `INSERT INTO users (username, email, password) VALUES (?, ?, ?)`, username, email, hash)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "email") {
			writeError(w, 409, "Email is already in use", "EMAIL_TAKEN")
		} else if strings.Contains(strings.ToLower(err.Error()), "username") {
			writeError(w, 409, "Username is already taken", "USERNAME_TAKEN")
		} else {
			writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		}
		return
	}
	userID, err := result.LastInsertId()
	if err != nil {
		writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		return
	}
	needsConfirmation := count > 0 && h.requiresSignupConfirmation()
	token := ""
	if needsConfirmation {
		token, err = passhash.RandomToken(32)
		if err != nil {
			writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
			return
		}
		expires := h.now().Add(h.signupTTL()).UTC().Format("2006-01-02 15:04:05")
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO user_email_tokens
			(user_id, type, email, token_hash, expires_at) VALUES (?, 'signup_confirmation', ?, ?, ?)`,
			userID, email, passhash.DigestToken(token), expires); err != nil {
			writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		return
	}
	if needsConfirmation {
		language := requestLanguage(r)
		if err := h.sendSignupConfirmation(r.Context(), username, email, token, language); err != nil {
			_, _ = h.db.ExecContext(context.WithoutCancel(r.Context()), `DELETE FROM users WHERE id = ?`, userID)
			writeError(w, http.StatusServiceUnavailable, "The confirmation email could not be sent. Try again later.", "EMAIL_SEND_FAILED")
			return
		}
		message := "Check your email to confirm your NeoCloud account."
		if h.pendingSignupMessage != nil {
			message = h.pendingSignupMessage(language)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"success": false, "requiresEmailConfirmation": true, "email": email, "message": message,
		})
		return
	}
	u, err := h.loadUser(r.Context(), userID)
	if err != nil || u == nil {
		writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
		return
	}
	h.establishSession(w, r, u, accountKey(u, username))
}
