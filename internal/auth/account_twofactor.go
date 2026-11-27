package auth

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/totp"
)

const twoFactorLastStepKey = "two_factor_last_step"

type accountFactorRequest struct {
	CurrentPassword string `json:"currentPassword"`
	Code            string `json:"code"`
	CurrentCode     string `json:"currentCode"`
}

func (h *Handler) setupTwoFactor(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	var body accountFactorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !h.confirmIdentity(w, r, u.ID, body.CurrentPassword, body.Code) {
		return
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not set up two-factor authentication", "INTERNAL_ERROR")
		return
	}
	sealed, err := h.keys.EncryptValue(secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not set up two-factor authentication", "INTERNAL_ERROR")
		return
	}
	var accountName string
	if u.Email.Valid && u.Email.String != "" {
		accountName = u.Email.String
	} else if u.Username != "" {
		accountName = u.Username
	} else {
		accountName = "user-" + strconv.FormatInt(u.ID, 10)
	}
	_, err = h.db.ExecContext(r.Context(), `INSERT INTO user_two_factor (user_id, pending_secret, enabled, created_at, updated_at)
		VALUES (?, ?, COALESCE((SELECT enabled FROM user_two_factor WHERE user_id = ?), 0), datetime('now'), datetime('now'))
		ON CONFLICT(user_id) DO UPDATE SET pending_secret = excluded.pending_secret, updated_at = datetime('now')`, u.ID, sealed, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not set up two-factor authentication", "INTERNAL_ERROR")
		return
	}
	status := h.statusTwoFactor(r, w, u.ID)
	if status == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"otpauthUrl": totp.URI("NeoCloud", accountName, secret),
		"manualKey":  secret,
		"status":     status,
	})
}

func (h *Handler) enableTwoFactor(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	var err error
	var body accountFactorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	var enabled int
	var pending sql.NullString
	err = h.db.QueryRowContext(r.Context(), `SELECT enabled, pending_secret FROM user_two_factor WHERE user_id = ?`, u.ID).Scan(&enabled, &pending)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !pending.Valid) {
		writeError(w, http.StatusBadRequest, "No two-factor setup is pending", "TWO_FACTOR_SETUP_REQUIRED")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not enable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	secret, err := h.keys.DecryptValue(pending.String)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not enable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	step, valid, err := matchedTOTPStep(secret, body.Code, h.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not enable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "Invalid 2FA code", "INVALID_CODE")
		return
	}
	if err := h.confirmFactor(r.Context(), u.ID, body.CurrentCode); err != nil {
		writeConfirmError(w, err)
		return
	}
	codes, hashes, err := h.newRecoveryCodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not enable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not enable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM user_recovery_codes WHERE user_id = ?`, u.ID); err == nil {
		for _, hash := range hashes {
			if _, err = tx.ExecContext(r.Context(), `INSERT INTO user_recovery_codes (user_id, code_hash, created_at) VALUES (?, ?, datetime('now'))`, u.ID, hash); err != nil {
				break
			}
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE user_two_factor SET secret = pending_secret, pending_secret = NULL, enabled = 1,
			enabled_at = datetime('now'), updated_at = datetime('now') WHERE user_id = ?`, u.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, datetime('now'))
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`, u.ID, twoFactorLastStepKey, strconv.FormatInt(step, 10))
	}
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "Could not enable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not enable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	status := h.statusTwoFactor(r, w, u.ID)
	if status == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "recoveryCodes": codes, "status": status})
}

func (h *Handler) disableTwoFactor(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	var body accountFactorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !h.confirmIdentity(w, r, u.ID, body.CurrentPassword, body.Code) {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not disable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE user_two_factor SET enabled = 0, secret = NULL, pending_secret = NULL, enabled_at = NULL,
		updated_at = datetime('now') WHERE user_id = ?`, u.ID); err == nil {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM user_recovery_codes WHERE user_id = ?`, u.ID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM user_settings WHERE user_id = ? AND key = ?`, u.ID, twoFactorLastStepKey)
	}
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "Could not disable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not disable two-factor authentication", "INTERNAL_ERROR")
		return
	}
	status := h.statusTwoFactor(r, w, u.ID)
	if status == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": status})
}

func (h *Handler) regenerateTwoFactorRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	var body accountFactorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !h.confirmIdentity(w, r, u.ID, body.CurrentPassword, body.Code) {
		return
	}
	codes, hashes, err := h.newRecoveryCodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not regenerate recovery codes", "INTERNAL_ERROR")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not regenerate recovery codes", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM user_recovery_codes WHERE user_id = ?`, u.ID); err == nil {
		for _, hash := range hashes {
			if _, err = tx.ExecContext(r.Context(), `INSERT INTO user_recovery_codes (user_id, code_hash, created_at) VALUES (?, ?, datetime('now'))`, u.ID, hash); err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "Could not regenerate recovery codes", "INTERNAL_ERROR")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not regenerate recovery codes", "INTERNAL_ERROR")
		return
	}
	status := h.statusTwoFactor(r, w, u.ID)
	if status == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "recoveryCodes": codes, "status": status})
}

func (h *Handler) newRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		code, err := totp.NewRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		codes[i] = code
		hash, err := passhash.Hash(totp.NormalizeRecoveryCode(code), passhash.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		hashes[i] = hash
	}
	return codes, hashes, nil
}

func matchedTOTPStep(secret, code string, now time.Time) (int64, bool, error) {
	normalized := totp.NormalizeCode(code)
	if len(normalized) != 6 {
		return 0, false, nil
	}
	var matched int64 = -1
	for _, delta := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		at := now.Add(delta)
		expected, err := totp.Code(secret, at)
		if err != nil {
			return 0, false, err
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(normalized)) == 1 {
			matched = at.Unix() / 30
		}
	}
	return matched, matched >= 0, nil
}

func (h *Handler) statusTwoFactor(r *http.Request, w http.ResponseWriter, userID int64) *twoFactorStatus {
	status, err := h.twoFactorStatus(r, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read two-factor status", "INTERNAL_ERROR")
		return nil
	}
	return &status
}
