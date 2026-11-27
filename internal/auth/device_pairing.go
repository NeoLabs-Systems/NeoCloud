package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

func (h *Handler) createDevicePairing(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	var body struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	name, platform := strings.TrimSpace(body.Name), strings.ToLower(strings.TrimSpace(body.Platform))
	if name != "" {
		var code string
		name, platform, code = cleanAuthorizationDevice(name, platform)
		if code != "" {
			message := "Unknown platform."
			if code == "NAME_INVALID" {
				message = "The device name is invalid."
			}
			writeError(w, http.StatusBadRequest, message, code)
			return
		}
	} else if !devicePlatforms[platform] {
		writeError(w, http.StatusBadRequest, "Unknown platform.", "PLATFORM_INVALID")
		return
	}
	if _, ok := h.signInAccount(w, r, h.db, identity.UserID); !ok {
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device pairing.", "INTERNAL_ERROR")
		return
	}
	code := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(code))
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device pairing.", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM device_pairings WHERE datetime(expires_at)<datetime('now','-1 day')`); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device pairing.", "INTERNAL_ERROR")
		return
	}
	var open int
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM device_pairings WHERE user_id=? AND status='pending' AND datetime(expires_at)>datetime('now')`, identity.UserID).Scan(&open); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device pairing.", "INTERNAL_ERROR")
		return
	}
	if open >= 5 {
		_ = tx.Rollback()
		writeError(w, http.StatusTooManyRequests, "Too many open pairing codes. Wait a few minutes and try again.", "TOO_MANY_PAIRING_CODES")
		return
	}
	var expiry string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO device_pairings(user_id,code_hash,name,platform,expires_at) VALUES(?,?,?,?,datetime('now','+300 seconds')) RETURNING expires_at`, identity.UserID, hex.EncodeToString(hash[:]), sql.NullString{String: name, Valid: name != ""}, platform).Scan(&expiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device pairing.", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device pairing.", "INTERNAL_ERROR")
		return
	}
	origin := h.origin(r)
	values := url.Values{}
	values.Set("server", origin)
	values.Set("code", code)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"code": code, "expiresAt": expiry, "server": origin, "qrPayload": "neocloud://pair?" + values.Encode()})
}

func (h *Handler) devicePairingStatus(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	code := r.PathValue("code")
	hash := sha256.Sum256([]byte(code))
	var status string
	var deviceID sql.NullInt64
	if err := h.db.QueryRowContext(r.Context(), `SELECT status,device_id FROM device_pairings WHERE user_id=? AND code_hash=?`, identity.UserID, hex.EncodeToString(hash[:])).Scan(&status, &deviceID); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Pairing code not found", "NOT_FOUND")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read pairing status.", "INTERNAL_ERROR")
		return
	}
	if status == "used" {
		var device any
		if deviceID.Valid {
			var id int64
			var name, platform string
			if err := h.db.QueryRowContext(r.Context(), `SELECT id,name,platform FROM device_tokens WHERE id=?`, deviceID.Int64).Scan(&id, &name, &platform); err == nil {
				device = map[string]any{"id": id, "name": name, "platform": platform}
			}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "used", "device": device})
		return
	}
	var expiry string
	if err := h.db.QueryRowContext(r.Context(), `SELECT expires_at FROM device_pairings WHERE user_id=? AND code_hash=?`, identity.UserID, hex.EncodeToString(hash[:])).Scan(&expiry); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read pairing status.", "INTERNAL_ERROR")
		return
	}
	state := "pending"
	if expired, _ := time.ParseInLocation("2006-01-02 15:04:05", expiry, time.UTC); !expired.IsZero() && !expired.After(h.now().UTC()) {
		state = "expired"
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": state})
}

func (h *Handler) redeemDevicePairing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Code) != 43 {
		writeError(w, http.StatusGone, "The pairing code has expired. Show a new one in the web app.", "EXPIRED_TOKEN")
		return
	}
	hash := sha256.Sum256([]byte(body.Code))
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not redeem pairing code.", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var pairingID, userID int64
	var suggestedName, suggestedPlatform sql.NullString
	err = tx.QueryRowContext(r.Context(), `SELECT id,user_id,name,platform FROM device_pairings WHERE code_hash=? AND status='pending' AND datetime(expires_at)>datetime('now')`, hex.EncodeToString(hash[:])).Scan(&pairingID, &userID, &suggestedName, &suggestedPlatform)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusGone, "The pairing code has expired. Show a new one in the web app.", "EXPIRED_TOKEN")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not redeem pairing code.", "INTERNAL_ERROR")
		return
	}
	name, platform := strings.TrimSpace(body.Name), strings.ToLower(strings.TrimSpace(body.Platform))
	if name == "" && suggestedName.Valid {
		name = suggestedName.String
	}
	if platform == "" && suggestedPlatform.Valid {
		platform = suggestedPlatform.String
	}
	name, platform, ok := deviceInfo(w, name, platform)
	if !ok {
		return
	}
	u, ok := h.signInAccount(w, r, tx, userID)
	if !ok {
		return
	}
	deviceID, token, err := issueDeviceToken(r.Context(), tx, userID, name, platform)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create device token.", "INTERNAL_ERROR")
		return
	}
	updated, err := tx.ExecContext(r.Context(), `UPDATE device_pairings SET status='used',device_id=?,used_at=datetime('now') WHERE id=? AND status='pending' AND datetime(expires_at)>datetime('now')`, deviceID, pairingID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not redeem pairing code.", "INTERNAL_ERROR")
		return
	}
	if changed, _ := updated.RowsAffected(); changed != 1 {
		writeError(w, http.StatusGone, "The pairing code has expired. Show a new one in the web app.", "EXPIRED_TOKEN")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not redeem pairing code.", "INTERNAL_ERROR")
		return
	}
	device := map[string]any{"id": deviceID, "name": name, "platform": platform}
	h.notify(userID, "devices:paired", map[string]any{"device": device})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"token": token, "device": device, "user": map[string]any{"id": userID, "username": u.Username}})
}
