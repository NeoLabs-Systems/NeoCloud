package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/pwpolicy"
)

func (h *Handler) userCount(ctx context.Context) (int, error) {
	var count int
	err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func requestLanguage(r *http.Request) string {
	first := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Accept-Language"), ",")[0]))
	if strings.HasPrefix(first, "de") {
		return "de"
	}
	return "en"
}

func (h *Handler) validateUsername(ctx context.Context, username string) error {
	return h.validateUsernameWith(ctx, h.db, username)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (h *Handler) validateUsernameWith(ctx context.Context, q queryRower, username string) error {
	if len(username) < 3 {
		return authError{status: 400, message: "Username must be at least 3 characters.", code: "USERNAME_TOO_SHORT"}
	}
	if len(username) > 64 || !usernamePattern.MatchString(username) {
		return authError{status: 400, message: "Username may use letters, digits, dots, dashes and underscores.", code: "USERNAME_INVALID"}
	}
	var exists int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM users WHERE lower(username) = lower(?)`, username).Scan(&exists)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		return authError{status: 409, message: "Username is already taken", code: "USERNAME_TAKEN"}
	}
	if _, exists := h.reservedAdminNames[strings.ToLower(username)]; exists {
		return authError{status: 409, message: "Username is already taken", code: "USERNAME_TAKEN"}
	}
	return nil
}

func validateEmailAvailable(ctx context.Context, q queryRower, email string) error {
	var exists int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM users WHERE lower(email) = lower(?)`, email).Scan(&exists)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		return authError{status: 409, message: "Email is already in use", code: "EMAIL_TAKEN"}
	}
	return nil
}

func (h *Handler) validatePassword(username, email, password string) error {
	strength := pwpolicy.Evaluate(password, pwpolicy.Context{Username: username, Email: email, AppName: "neocloud"})
	if !strength.HasMinimumLength {
		return authError{status: 400, message: "Password must be at least 8 characters.", code: "PASSWORD_TOO_SHORT"}
	}
	if !strength.IsAcceptable {
		return authError{status: 400, message: pwpolicy.Error(strength), code: "PASSWORD_WEAK"}
	}
	return nil
}

// ValidateNewPassword applies the same password rules used by account sign-up,
// self-service changes and recovery to callers outside the auth route package.
// The return values are suitable for an API's stable error response.
func ValidateNewPassword(username, email, password string) (status int, message, code string, valid bool) {
	err := (&Handler{}).validatePassword(username, email, password)
	if err == nil {
		return 0, "", "", true
	}
	var rejected authError
	if errors.As(err, &rejected) {
		return rejected.status, rejected.message, rejected.code, false
	}
	return http.StatusInternalServerError, "Could not validate password", "INTERNAL_ERROR", false
}
