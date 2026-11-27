package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

func (h *Handler) requestDeviceWipe(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		EraseFiles bool `json:"eraseFiles"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &body) {
		return
	}
	var name, platform, created string
	var lastSeen, revoked, requested, delivered sql.NullString
	var oldErase int
	err = h.db.QueryRowContext(r.Context(), `SELECT name,platform,created_at,last_seen_at,revoked_at,wipe_requested_at,wipe_erase_files,wipe_delivered_at FROM device_tokens WHERE id=? AND user_id=?`, id, identity.UserID).Scan(&name, &platform, &created, &lastSeen, &revoked, &requested, &oldErase, &delivered)
	if err == sql.ErrNoRows || platform == "webdav" || (revoked.Valid && !requested.Valid) {
		writeError(w, 404, "Device not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, 500, "Could not read device", "INTERNAL_ERROR")
		return
	}
	if delivered.Valid {
		httpx.WriteJSON(w, 200, map[string]any{"device": deviceWipePayload(id, name, platform, created, lastSeen, requested, oldErase, delivered)})
		return
	}
	erase := body.EraseFiles || oldErase == 1
	if _, err = h.db.ExecContext(r.Context(), `UPDATE device_tokens SET revoked_at=COALESCE(revoked_at,datetime('now')),wipe_requested_at=COALESCE(wipe_requested_at,datetime('now')),wipe_erase_files=? WHERE id=? AND user_id=?`, boolInt(erase), id, identity.UserID); err != nil {
		writeError(w, 500, "Could not request device wipe", "INTERNAL_ERROR")
		return
	}
	if !requested.Valid {
		detail, _ := json.Marshal(map[string]any{"deviceId": id, "deviceName": name, "eraseFiles": erase})
		if _, err = h.db.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('device.wipe_requested',?,?,?)`, identity.UserID, identity.UserID, string(detail)); err != nil {
			writeError(w, 500, "Could not request device wipe", "INTERNAL_ERROR")
			return
		}
	}
	h.cancelDeviceHandoffs(r.Context(), identity.UserID, id, 0)
	err = h.db.QueryRowContext(r.Context(), `SELECT wipe_requested_at,wipe_erase_files,wipe_delivered_at FROM device_tokens WHERE id=? AND user_id=?`, id, identity.UserID).Scan(&requested, &oldErase, &delivered)
	if err != nil {
		writeError(w, 500, "Could not read device", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"device": deviceWipePayload(id, name, platform, created, lastSeen, requested, oldErase, delivered)})
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func deviceWipePayload(id int64, name, platform, created string, lastSeen, requested sql.NullString, erase int, delivered sql.NullString) map[string]any {
	var seen any
	if lastSeen.Valid {
		seen = lastSeen.String
	}
	var wipe any
	if requested.Valid {
		var deliveredAt any
		if delivered.Valid {
			deliveredAt = delivered.String
		}
		wipe = map[string]any{"requestedAt": requested.String, "eraseFiles": erase == 1, "deliveredAt": deliveredAt}
	}
	return map[string]any{"id": id, "name": name, "platform": platform, "createdAt": created, "lastSeenAt": seen, "wipe": wipe}
}
