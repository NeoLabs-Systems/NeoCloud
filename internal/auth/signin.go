package auth

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

// The credential checks every sign-in shares: the browser's, and the OAuth
// connection's own page. Refusals carry a status and a stable code
// (Status, Code), or are a *lockout.LockedError.

func (e authError) Status() int  { return e.status }
func (e authError) Code() string { return e.code }

// VerifyPassword checks a typed account name or email and password under the
// account's lockout, and refuses an account that may not sign in. secondFactor
// reports that a code is still owed.
func (h *Handler) VerifyPassword(ctx context.Context, identifier, password string) (userID int64, secondFactor bool, err error) {
	u, secondFactor, err := h.verifyPassword(ctx, identifier, password)
	if err != nil {
		return 0, false, err
	}
	return u.ID, secondFactor, nil
}

func (h *Handler) verifyPassword(ctx context.Context, identifier, password string) (*user, bool, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || password == "" {
		return nil, false, authError{status: 400, message: "Username and password required", code: "MISSING_FIELDS"}
	}
	u, err := h.findUser(ctx, identifier)
	if err != nil {
		return nil, false, err
	}
	key := accountKey(u, identifier)
	if err := h.lockout.Check(ctx, key); err != nil {
		return nil, false, err
	}
	if err := h.recordFailure(ctx, key, "password", ""); err != nil {
		return nil, false, err
	}
	hash := h.dummyHash
	if u != nil {
		hash = u.Password
	}
	if valid := passhash.Verify(hash, password); u == nil || !valid {
		return nil, false, authError{status: 401, message: "Invalid credentials", code: "INVALID_CREDENTIALS"}
	}
	if err := h.lockout.Clear(ctx, key, "password"); err != nil {
		return nil, false, err
	}
	if err := h.assertSignInAllowed(ctx, u); err != nil {
		return nil, false, err
	}
	var enabled int
	if err := h.db.QueryRowContext(ctx, `SELECT enabled FROM user_two_factor WHERE user_id = ?`, u.ID).Scan(&enabled); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	return u, enabled == 1, nil
}

// VerifySignInCode checks the second factor of the pending sign-in named by
// challenge: at most five codes each, on top of the account lockout. exhausted
// tells the caller to drop the pending sign-in.
func (h *Handler) VerifySignInCode(ctx context.Context, userID int64, challenge, code string) (exhausted bool, err error) {
	var attempts int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_failures WHERE challenge_id = ?`, challenge).Scan(&attempts); err != nil {
		return false, err
	}
	if attempts >= maxTwoFactorCodes {
		return true, authError{status: 401, message: "Too many wrong codes. Sign in with your password again.", code: "TWO_FACTOR_ATTEMPTS_EXCEEDED"}
	}
	key := "user:" + strconv.FormatInt(userID, 10)
	if err := h.lockout.Check(ctx, key); err != nil {
		return false, err
	}
	if err := h.recordFailure(ctx, key, "second_factor", challenge); err != nil {
		return false, err
	}
	valid, err := h.verifySecondFactor(ctx, userID, code)
	if err != nil {
		return false, err
	}
	if !valid {
		return attempts+1 >= maxTwoFactorCodes, authError{status: 401, message: "Invalid 2FA code", code: "INVALID_CODE"}
	}
	return false, h.lockout.Clear(ctx, key, "")
}
