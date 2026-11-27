package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"golang.org/x/text/unicode/norm"
	"modernc.org/sqlite"
)

const (
	deviceAuthorizationLifetimeSeconds = 600
	deviceAuthorizationPollInterval    = 3
)

const deviceUserCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func deviceCodeHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func makeDeviceUserCode() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	for i := range value {
		value[i] = deviceUserCodeAlphabet[int(value[i])%len(deviceUserCodeAlphabet)]
	}
	return string(value[:4]) + "-" + string(value[4:]), nil
}

func normalizeDeviceUserCode(value string) string {
	var cleaned strings.Builder
	for _, r := range strings.ToUpper(value) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			cleaned.WriteRune(r)
		}
	}
	code := cleaned.String()
	if len(code) != 8 {
		return ""
	}
	return code[:4] + "-" + code[4:]
}

func cleanAuthorizationDevice(name, platform string) (string, string, string) {
	name = norm.NFC.String(strings.TrimSpace(name))
	if name == "" {
		return "", "", "NAME_REQUIRED"
	}
	if !utf8.ValidString(name) {
		return "", "", "NAME_INVALID"
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	if !devicePlatforms[platform] {
		return "", "", "PLATFORM_INVALID"
	}
	return name, platform, ""
}

// deviceInfo is a new device's name and platform as stored, or a 400.
func deviceInfo(w http.ResponseWriter, name, platform string) (string, string, bool) {
	name, platform, code := cleanAuthorizationDevice(name, platform)
	if code == "" {
		return name, platform, true
	}
	message := "Unknown platform."
	switch code {
	case "NAME_REQUIRED":
		message = "Give the device a name."
	case "NAME_INVALID":
		message = "The device name is invalid."
	}
	writeError(w, http.StatusBadRequest, message, code)
	return "", "", false
}

func (h *Handler) deviceAuthorizationRoutes(mux *http.ServeMux) {
	startLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 30, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	tokenLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 600, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	decisionLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 30, Message: map[string]string{"error": "Too many account security attempts, try again later", "code": "RATE_LIMITED"}})
	wipeLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 10, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/devices/authorize", startLimiter.Middleware(http.HandlerFunc(h.startDeviceAuthorization)))
	mux.Handle("POST /api/devices/token", tokenLimiter.Middleware(http.HandlerFunc(h.exchangeDeviceAuthorization)))
	mux.Handle("GET /api/account/devices/authorize/{userCode}", decisionLimiter.Middleware(http.HandlerFunc(h.describeDeviceAuthorization)))
	decide := decisionLimiter.Middleware(http.HandlerFunc(h.decideDeviceAuthorization))
	wipe := wipeLimiter.Middleware(http.HandlerFunc(h.requestDeviceWipe))
	mux.Handle("POST /api/account/devices/{id}/{action}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "authorize" {
			r.SetPathValue("userCode", r.PathValue("action"))
			decide.ServeHTTP(w, r)
			return
		}
		if r.PathValue("action") == "wipe" {
			wipe.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "Not found", "NOT_FOUND")
	}))
}

func (h *Handler) startDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	name, platform, ok := deviceInfo(w, body.Name, body.Platform)
	if !ok {
		return
	}
	deviceBytes := make([]byte, 32)
	if _, err := rand.Read(deviceBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device sign-in.", "INTERNAL_ERROR")
		return
	}
	deviceCode := base64.RawURLEncoding.EncodeToString(deviceBytes)
	var userCode string
	inserted := false
	if _, err := h.db.ExecContext(r.Context(), `DELETE FROM device_authorizations WHERE datetime(expires_at)<datetime('now','-1 day')`); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start device sign-in.", "INTERNAL_ERROR")
		return
	}
	for attempt := 0; attempt < 6; attempt++ {
		var err error
		userCode, err = makeDeviceUserCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not start device sign-in.", "INTERNAL_ERROR")
			return
		}
		_, err = h.db.ExecContext(r.Context(), `INSERT INTO device_authorizations(device_code_hash,user_code,name,platform,expires_at) VALUES(?,?,?,?,datetime('now','+600 seconds'))`, deviceCodeHash(deviceCode), userCode, name, platform)
		if err == nil {
			inserted = true
			break
		}
		var sqliteErr *sqlite.Error
		// SQLite's extended result codes retain the base constraint code in
		// their low byte. Only this INSERT's unique indexes are expected to
		// conflict; those collisions can safely draw a fresh user code.
		if !errors.As(err, &sqliteErr) || sqliteErr.Code()&0xff != 19 {
			writeError(w, http.StatusInternalServerError, "Could not start device sign-in.", "INTERNAL_ERROR")
			return
		}
	}
	if !inserted {
		writeError(w, http.StatusInternalServerError, "Could not start device sign-in.", "INTERNAL_ERROR")
		return
	}
	verificationURL := h.origin(r) + "/app/device?code=" + userCode
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"deviceCode": deviceCode, "userCode": userCode, "verificationUrl": verificationURL,
		"expiresIn": deviceAuthorizationLifetimeSeconds, "interval": deviceAuthorizationPollInterval,
	})
}

func (h *Handler) exchangeDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceCode string `json:"deviceCode"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.DeviceCode) != 43 {
		writeError(w, http.StatusGone, "The sign-in code has expired. Start again.", "EXPIRED_TOKEN")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not exchange device code.", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var id, userID int64
	var name, platform, status string
	var expires string
	err = tx.QueryRowContext(r.Context(), `SELECT id,COALESCE(user_id,0),name,platform,status,expires_at FROM device_authorizations WHERE device_code_hash=?`, deviceCodeHash(body.DeviceCode)).Scan(&id, &userID, &name, &platform, &status, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusGone, "The sign-in code has expired. Start again.", "EXPIRED_TOKEN")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not exchange device code.", "INTERNAL_ERROR")
		return
	}
	var expired int
	if err = tx.QueryRowContext(r.Context(), `SELECT datetime(?)<=datetime('now')`, expires).Scan(&expired); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not check device code.", "INTERNAL_ERROR")
		return
	}
	if expired == 1 || status == "consumed" {
		writeError(w, http.StatusGone, "The sign-in code has expired. Start again.", "EXPIRED_TOKEN")
		return
	}
	if status == "pending" {
		writeError(w, http.StatusPreconditionRequired, "Waiting for approval.", "AUTHORIZATION_PENDING")
		return
	}
	if status == "denied" {
		writeError(w, http.StatusForbidden, "The sign-in was denied.", "ACCESS_DENIED")
		return
	}
	if status != "approved" || userID < 1 {
		writeError(w, http.StatusGone, "The sign-in code has expired. Start again.", "EXPIRED_TOKEN")
		return
	}
	u, ok := h.signInAccount(w, r, tx, userID)
	if !ok {
		return
	}
	consumed, err := tx.ExecContext(r.Context(), `UPDATE device_authorizations SET status='consumed' WHERE id=? AND status='approved'`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not exchange device code.", "INTERNAL_ERROR")
		return
	}
	if count, _ := consumed.RowsAffected(); count != 1 {
		writeError(w, http.StatusGone, "The sign-in code has expired. Start again.", "EXPIRED_TOKEN")
		return
	}
	deviceID, token, err := issueDeviceToken(r.Context(), tx, userID, name, platform)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create device token.", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not exchange device code.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token":  token,
		"device": map[string]any{"id": deviceID, "name": name, "platform": platform},
		"user":   map[string]any{"id": userID, "username": u.Username},
	})
}

func (h *Handler) describeDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.accountIdentity(w, r); !ok {
		return
	}
	code := normalizeDeviceUserCode(r.PathValue("userCode"))
	var name, platform, createdAt, expiresAt string
	err := h.db.QueryRowContext(r.Context(), `SELECT name,platform,created_at,expires_at FROM device_authorizations WHERE user_code=? AND status='pending' AND datetime(expires_at)>datetime('now')`, code).Scan(&name, &platform, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || code == "" {
		writeError(w, http.StatusNotFound, "This code is unknown or has expired. Start the sign-in on the device again.", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read device sign-in.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"name": name, "platform": platform, "createdAt": createdAt, "expiresAt": expiresAt})
}

func (h *Handler) decideDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	var body struct {
		Approve *bool `json:"approve"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Approve == nil {
		writeError(w, http.StatusBadRequest, "approve must be true or false.", "INVALID_DECISION")
		return
	}
	code := normalizeDeviceUserCode(r.PathValue("userCode"))
	if code == "" {
		writeError(w, http.StatusNotFound, "This code is unknown or has expired. Start the sign-in on the device again.", "NOT_FOUND")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not decide device sign-in.", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	status := "denied"
	if *body.Approve {
		status = "approved"
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE device_authorizations SET status=?,user_id=? WHERE user_code=? AND status='pending' AND datetime(expires_at)>datetime('now')`, status, identity.UserID, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not decide device sign-in.", "INTERNAL_ERROR")
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not decide device sign-in.", "INTERNAL_ERROR")
		return
	}
	if changed != 1 {
		writeError(w, http.StatusNotFound, "This code is unknown or has expired. Start the sign-in on the device again.", "NOT_FOUND")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not decide device sign-in.", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
