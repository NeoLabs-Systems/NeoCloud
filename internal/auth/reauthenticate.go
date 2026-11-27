package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/lockout"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

// ReauthenticationError carries the safe HTTP contract for a failed
// sensitive-action identity check.
type ReauthenticationError struct {
	Message    string
	ErrorCode  string
	HTTPStatus int
}

func (e *ReauthenticationError) Error() string { return e.Message }
func (e *ReauthenticationError) Code() string  { return e.ErrorCode }
func (e *ReauthenticationError) Status() int   { return e.HTTPStatus }

// ConfirmIdentity rechecks a signed-in user's password and, when enabled,
// consumes one current TOTP or recovery code. Password and factor attempts use
// the account confirmation lockout shared by other sensitive account actions.
func (h *Handler) ConfirmIdentity(ctx context.Context, userID int64, currentPassword, code string) error {
	if err := h.confirmPassword(ctx, userID, currentPassword); err != nil {
		return err
	}
	return h.confirmFactor(ctx, userID, code)
}

func (h *Handler) confirmPassword(ctx context.Context, userID int64, password string) error {
	key := lockout.ConfirmKey(strconv.FormatInt(userID, 10))
	if err := h.lockout.Check(ctx, key); err != nil {
		return err
	}
	if err := h.lockout.Record(ctx, key, "password"); err != nil {
		return err
	}
	var passwordHash string
	err := h.db.QueryRowContext(ctx, `SELECT password FROM users WHERE id=?`, userID).Scan(&passwordHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) || !passhash.Verify(passwordHash, password) {
		return &ReauthenticationError{Message: "Current password is incorrect", ErrorCode: "WRONG_PASSWORD", HTTPStatus: http.StatusUnauthorized}
	}
	return h.lockout.Clear(ctx, key, "password")
}

// confirmFactor passes when 2FA is off.
func (h *Handler) confirmFactor(ctx context.Context, userID int64, code string) error {
	var enabled int
	err := h.db.QueryRowContext(ctx, `SELECT enabled FROM user_two_factor WHERE user_id=?`, userID).Scan(&enabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if enabled != 1 {
		return nil
	}
	if strings.TrimSpace(code) == "" {
		return &ReauthenticationError{Message: "Enter a code from your current authenticator or a recovery code.", ErrorCode: "TWO_FACTOR_REQUIRED", HTTPStatus: http.StatusUnauthorized}
	}
	key := lockout.ConfirmKey(strconv.FormatInt(userID, 10))
	if err = h.lockout.Check(ctx, key); err != nil {
		return err
	}
	if err = h.lockout.Record(ctx, key, "second_factor"); err != nil {
		return err
	}
	valid, err := h.verifySecondFactor(ctx, userID, code)
	if err != nil {
		return err
	}
	if !valid {
		return &ReauthenticationError{Message: "Invalid 2FA code", ErrorCode: "INVALID_CODE", HTTPStatus: http.StatusUnauthorized}
	}
	return h.lockout.Clear(ctx, key, "second_factor")
}

func writeConfirmError(w http.ResponseWriter, err error) {
	var rejected *ReauthenticationError
	if errors.As(err, &rejected) {
		writeError(w, rejected.HTTPStatus, rejected.Message, rejected.ErrorCode)
		return
	}
	var locked *lockout.LockedError
	if errors.As(err, &locked) {
		writeLocked(w, locked, locked.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "Could not verify identity", "INTERNAL_ERROR")
}

// confirmIdentity is ConfirmIdentity for a handler: it answers the refusal.
func (h *Handler) confirmIdentity(w http.ResponseWriter, r *http.Request, userID int64, password, code string) bool {
	if err := h.ConfirmIdentity(r.Context(), userID, password, code); err != nil {
		writeConfirmError(w, err)
		return false
	}
	return true
}
