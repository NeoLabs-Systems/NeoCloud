package auth

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

var devicePlatforms = map[string]bool{"": true, "windows": true, "macos": true, "linux": true, "android": true, "ios": true, "lite": true, "webdav": true}

func (h *Handler) listDeviceTokens(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,name,platform,created_at,last_seen_at,wipe_requested_at,wipe_erase_files,wipe_delivered_at FROM device_tokens WHERE user_id=? AND (revoked_at IS NULL OR (wipe_requested_at IS NOT NULL AND (wipe_delivered_at IS NULL OR wipe_delivered_at>datetime('now','-30 days')))) ORDER BY id DESC`, identity.UserID)
	if err != nil {
		writeError(w, 500, "Could not read devices", "INTERNAL_ERROR")
		return
	}
	devices := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, platform, created string
		var lastSeen, wipeRequested, wipeDelivered sql.NullString
		var erase int
		if err = rows.Scan(&id, &name, &platform, &created, &lastSeen, &wipeRequested, &erase, &wipeDelivered); err != nil {
			rows.Close()
			writeError(w, 500, "Could not read devices", "INTERNAL_ERROR")
			return
		}
		var wipe any
		if wipeRequested.Valid {
			var delivered any
			if wipeDelivered.Valid {
				delivered = wipeDelivered.String
			}
			wipe = map[string]any{"requestedAt": wipeRequested.String, "eraseFiles": erase == 1, "deliveredAt": delivered}
		}
		var seen any
		if lastSeen.Valid {
			seen = lastSeen.String
		}
		devices = append(devices, map[string]any{"id": id, "name": name, "platform": platform, "createdAt": created, "lastSeenAt": seen, "wipe": wipe, "current": identity.IsDevice && identity.DeviceID == id})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, 500, "Could not read devices", "INTERNAL_ERROR")
		return
	}
	if err = rows.Close(); err != nil {
		writeError(w, 500, "Could not read devices", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"devices": devices})
}

func (h *Handler) createDeviceToken(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
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
	name, platform, ok := deviceInfo(w, body.Name, body.Platform)
	if !ok {
		return
	}
	if err := h.assertSignInAllowed(r.Context(), u); err != nil {
		writeSignInError(w, err)
		return
	}
	id, token, err := issueDeviceToken(r.Context(), h.db, u.ID, name, platform)
	if err != nil {
		writeError(w, 500, "Could not create device token", "INTERNAL_ERROR")
		return
	}
	var created string
	if err = h.db.QueryRowContext(r.Context(), `SELECT created_at FROM device_tokens WHERE id=?`, id).Scan(&created); err != nil {
		writeError(w, 500, "Could not create device token", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, 201, map[string]any{"device": map[string]any{"id": id, "name": name, "platform": platform, "createdAt": created, "lastSeenAt": nil, "wipe": nil, "current": false}, "token": token})
}

func (h *Handler) renameDeviceToken(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 404, "Device not found", "NOT_FOUND")
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
	result, err := h.db.ExecContext(r.Context(), `UPDATE device_tokens SET name=? WHERE id=? AND user_id=? AND revoked_at IS NULL`, name, id, identity.UserID)
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
	if err = h.db.QueryRowContext(r.Context(), `SELECT platform,created_at,last_seen_at FROM device_tokens WHERE id=? AND user_id=?`, id, identity.UserID).Scan(&platform, &created, &lastSeen); err != nil {
		writeError(w, 500, "Could not read device", "INTERNAL_ERROR")
		return
	}
	var seen any
	if lastSeen.Valid {
		seen = lastSeen.String
	}
	httpx.WriteJSON(w, 200, map[string]any{"device": map[string]any{"id": id, "name": name, "platform": platform, "createdAt": created, "lastSeenAt": seen, "wipe": nil, "current": identity.IsDevice && identity.DeviceID == id}})
}

func (h *Handler) revokeDeviceToken(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.accountIdentity(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 404, "Device not found", "NOT_FOUND")
		return
	}
	if identity.IsDevice && identity.DeviceID == id {
		writeError(w, 400, "Use Disconnect to sign this phone out", "CURRENT_DEVICE")
		return
	}
	result, err := h.db.ExecContext(r.Context(), `UPDATE device_tokens SET revoked_at=datetime('now') WHERE id=? AND user_id=? AND revoked_at IS NULL`, id, identity.UserID)
	if err != nil {
		writeError(w, 500, "Could not revoke device", "INTERNAL_ERROR")
		return
	}
	revoked, _ := result.RowsAffected()
	if revoked == 0 {
		result, err = h.db.ExecContext(r.Context(), `UPDATE device_tokens SET wipe_requested_at=NULL,wipe_erase_files=0 WHERE id=? AND user_id=? AND wipe_requested_at IS NOT NULL AND wipe_delivered_at IS NULL`, id, identity.UserID)
		if err != nil {
			writeError(w, 500, "Could not revoke device", "INTERNAL_ERROR")
			return
		}
		if count, _ := result.RowsAffected(); count == 0 {
			writeError(w, 404, "Device not found", "NOT_FOUND")
			return
		}
	}
	if revoked > 0 {
		h.cancelDeviceHandoffs(r.Context(), identity.UserID, id, 0)
	}
	httpx.WriteJSON(w, 200, map[string]bool{"success": true})
}
