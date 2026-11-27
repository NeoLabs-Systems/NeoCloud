package auth

import (
	"database/sql"
	"net/http"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func (h *Handler) deviceMe(w http.ResponseWriter, r *http.Request) {
	identity, ok := IdentityFromContext(r.Context())
	if !ok || !identity.IsDevice {
		writeError(w, 401, "Unauthorized", "UNAUTHORIZED")
		return
	}
	var deviceID int64
	var name, platform, created string
	var lastSeen sql.NullString
	var username string
	var admin int
	var used int64
	var customQuota sql.NullInt64
	err := h.db.QueryRowContext(r.Context(), `SELECT d.id,d.name,d.platform,d.created_at,d.last_seen_at,u.username,u.is_admin,u.used_bytes,u.quota_bytes FROM device_tokens d JOIN users u ON u.id=d.user_id WHERE d.id=? AND d.user_id=? AND d.revoked_at IS NULL AND u.disabled_at IS NULL`, identity.DeviceID, identity.UserID).Scan(&deviceID, &name, &platform, &created, &lastSeen, &username, &admin, &used, &customQuota)
	if err != nil {
		writeError(w, 401, "Unauthorized", "UNAUTHORIZED")
		return
	}
	var seen any
	if lastSeen.Valid {
		seen = lastSeen.String
	}
	quota := h.defaultQuota()
	if customQuota.Valid {
		quota = customQuota.Int64
	}
	writeJSON(w, 200, map[string]any{"device": map[string]any{"id": deviceID, "name": name, "platform": platform, "createdAt": created, "lastSeenAt": seen}, "user": map[string]any{"id": identity.UserID, "username": username, "isAdmin": admin == 1}, "quota": map[string]any{"used": used, "limit": quota}})
}

func (h *Handler) renameCurrentDevice(w http.ResponseWriter, r *http.Request) {
	identity, ok := IdentityFromContext(r.Context())
	if !ok || !identity.IsDevice {
		writeError(w, 401, "Unauthorized", "UNAUTHORIZED")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	name, ok := cleanDeviceName(w, body.Name)
	if !ok {
		return
	}
	result, err := h.db.ExecContext(r.Context(), `UPDATE device_tokens SET name=? WHERE id=? AND user_id=? AND revoked_at IS NULL`, name, identity.DeviceID, identity.UserID)
	if err != nil {
		writeError(w, 500, "Could not update device", "INTERNAL_ERROR")
		return
	}
	if count, _ := result.RowsAffected(); count != 1 {
		writeError(w, 404, "Device not found", "NOT_FOUND")
		return
	}
	var platform, created string
	var lastSeen sql.NullString
	if err = h.db.QueryRowContext(r.Context(), `SELECT platform,created_at,last_seen_at FROM device_tokens WHERE id=? AND user_id=?`, identity.DeviceID, identity.UserID).Scan(&platform, &created, &lastSeen); err != nil {
		writeError(w, 500, "Could not read device", "INTERNAL_ERROR")
		return
	}
	var seen any
	if lastSeen.Valid {
		seen = lastSeen.String
	}
	writeJSON(w, 200, map[string]any{"device": map[string]any{"id": identity.DeviceID, "name": name, "platform": platform, "createdAt": created, "lastSeenAt": seen, "wipe": nil}})
}

// cleanDeviceName is a new name for an existing device: 1 to 80 characters,
// nothing invisible.
func cleanDeviceName(w http.ResponseWriter, raw string) (string, bool) {
	name := strings.TrimSpace(norm.NFC.String(raw))
	switch {
	case name == "":
		writeError(w, 400, "Give the device a name.", "NAME_REQUIRED")
	case strings.IndexFunc(name, func(r rune) bool { return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) }) >= 0:
		writeError(w, 400, "Names can’t contain control or invisible characters.", "NAME_INVALID")
	case len([]rune(name)) > 80:
		writeError(w, 400, "Names can be up to 80 characters.", "NAME_TOO_LONG")
	default:
		return name, true
	}
	return "", false
}
