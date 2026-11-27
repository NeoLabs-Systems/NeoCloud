package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

type RequestIdentity struct {
	UserID   int64
	DeviceID int64
	Platform string
	IsDevice bool
}

type identityKey struct{}

// WithIdentity attaches a previously authenticated principal to an internal
// request dispatched through the same account-scoped service handlers.
func WithIdentity(ctx context.Context, identity RequestIdentity) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (RequestIdentity, bool) {
	v, ok := ctx.Value(identityKey{}).(RequestIdentity)
	return v, ok && v.UserID > 0
}

// IdentityMiddleware resolves the current browser account or the SHA-256 of
// an nc_ device bearer token. Device records are never cached, so revocation
// and account disabling take effect on the next request.
// notify, when given, hears that a wiped device has picked up its order.
func IdentityMiddleware(db *sql.DB, next http.Handler, notify ...func(userID int64, event string, data map[string]any)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if session := sessionx.FromContext(r.Context()); session != nil {
			if _, exists := session.Get("userId"); exists {
				userID, ok := session.Int64("userId")
				if !ok {
					userID, _ = strconv.ParseInt(strings.TrimSpace(session.String("userId")), 10, 64)
				}
				if userID < 1 {
					session.Destroy()
				} else {
					var active int
					err := db.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL`, userID).Scan(&active)
					if err == nil {
						identity := RequestIdentity{UserID: userID}
						whileSignedIn(db, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)), func(request *http.Request) {
							next.ServeHTTP(w, request)
						}, liveSessionQuery, userID, session.ID())
						return
					}
					if !errors.Is(err, sql.ErrNoRows) {
						httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not verify account", "code": "INTERNAL_ERROR"})
						return
					}
					// Clear stale browser identity before downstream handlers run. Some
					// handlers also accept an unwrapped session for internal dispatch,
					// so leaving this value behind would bypass the failed lookup.
					session.Destroy()
				}
			}
		}
		fields := strings.Fields(r.Header.Get("Authorization"))
		if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") && strings.HasPrefix(fields[1], "nc_") {
			hash := sha256.Sum256([]byte(fields[1]))
			var identity RequestIdentity
			err := db.QueryRowContext(r.Context(), `SELECT d.user_id,d.id,d.platform FROM device_tokens d JOIN users u ON u.id=d.user_id WHERE d.token_hash=? AND d.revoked_at IS NULL AND u.disabled_at IS NULL`, hex.EncodeToString(hash[:])).Scan(&identity.UserID, &identity.DeviceID, &identity.Platform)
			if err == nil && identity.Platform != "webdav" {
				identity.IsDevice = true
				_, _ = db.ExecContext(r.Context(), `UPDATE device_tokens SET last_seen_at=datetime('now') WHERE id=? AND (last_seen_at IS NULL OR last_seen_at<datetime('now','-1 minute'))`, identity.DeviceID)
				whileSignedIn(db, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)), func(request *http.Request) {
					next.ServeHTTP(w, request)
				}, liveDeviceQuery, identity.DeviceID)
				return
			}
			var deviceID, ownerID int64
			var eraseFiles int
			var deviceName string
			wipeErr := db.QueryRowContext(r.Context(), `SELECT id,user_id,name,wipe_erase_files FROM device_tokens WHERE token_hash=? AND wipe_requested_at IS NOT NULL AND wipe_delivered_at IS NULL`, hex.EncodeToString(hash[:])).Scan(&deviceID, &ownerID, &deviceName, &eraseFiles)
			if wipeErr == nil {
				result, claimErr := db.ExecContext(r.Context(), `UPDATE device_tokens SET wipe_delivered_at=datetime('now') WHERE id=? AND wipe_delivered_at IS NULL`, deviceID)
				if claimErr == nil {
					if claimed, _ := result.RowsAffected(); claimed == 1 {
						detail, _ := json.Marshal(map[string]any{"deviceId": deviceID, "deviceName": deviceName})
						if _, err := db.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('device.wipe_delivered',?,?,?)`, ownerID, ownerID, string(detail)); err != nil {
							slog.Error("could not record delivered device wipe", "device_id", deviceID, "error", err)
						}
						for _, publish := range notify {
							publish(ownerID, "devices:wiped", map[string]any{"id": deviceID, "name": deviceName})
						}
						httpx.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "This device was signed out remotely. Its NeoCloud data has to be erased.", "code": "DEVICE_WIPE_REQUESTED", "eraseFiles": eraseFiles == 1})
						return
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func CurrentIdentity(r *http.Request) (RequestIdentity, bool) {
	if identity, ok := IdentityFromContext(r.Context()); ok {
		return identity, true
	}
	if session := sessionx.FromContext(r.Context()); session != nil {
		if id, err := strconv.ParseInt(session.String("userId"), 10, 64); err == nil && id > 0 {
			return RequestIdentity{UserID: id}, true
		}
	}
	return RequestIdentity{}, false
}
