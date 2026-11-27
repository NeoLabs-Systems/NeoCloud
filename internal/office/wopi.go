package office

// The WOPI host endpoints the document editor (Collabora Online) calls. They
// are not reached with the session cookie: every call carries a signed,
// short-lived access token, and the account's right to the file is checked
// again on each one.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const (
	tokenTTL      = 10 * time.Hour
	lockTTL       = 30 * time.Minute
	maxLockLength = 1024
)

// isoLayout is the JavaScript toISOString form editors have seen so far; they
// echo it back to detect changes.
const isoLayout = "2006-01-02T15:04:05.000Z"

func isoTime(at time.Time) string { return at.UTC().Format(isoLayout) }

func modifiedTime(stored string) string {
	parsed, err := time.Parse(time.DateTime, stored)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, stored)
	}
	if err != nil {
		return stored
	}
	return isoTime(parsed)
}

type tokenClaims struct {
	UserID  int64  `json:"u"`
	NodeID  string `json:"n"`
	Write   int    `json:"w"`
	Expires int64  `json:"e"`
}

func tokenKey(sessionSecret string) []byte {
	mac := hmac.New(sha256.New, []byte(sessionSecret))
	_, _ = mac.Write([]byte("neocloud-wopi-access-token-v1"))
	return mac.Sum(nil)
}

func (h *Handler) sign(payload string) []byte {
	mac := hmac.New(sha256.New, h.tokenKey)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// issueToken binds one account, one file and a read or write right.
func (h *Handler) issueToken(userID int64, nodeID string, write bool) (string, int64) {
	claims := tokenClaims{UserID: userID, NodeID: nodeID, Expires: h.now().Add(tokenTTL).UnixMilli()}
	if write {
		claims.Write = 1
	}
	data, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(data)
	return payload + "." + base64.RawURLEncoding.EncodeToString(h.sign(payload)), claims.Expires
}

func (h *Handler) verifyToken(token string) (tokenClaims, bool) {
	payload, signature, found := strings.Cut(token, ".")
	if !found {
		return tokenClaims{}, false
	}
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(h.sign(payload), given) {
		return tokenClaims{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return tokenClaims{}, false
	}
	var claims tokenClaims
	if json.Unmarshal(data, &claims) != nil || claims.UserID < 1 || claims.NodeID == "" || claims.Expires <= h.now().UnixMilli() {
		return tokenClaims{}, false
	}
	return claims, true
}

type claimsKey struct{}

type wopiHandler func(w http.ResponseWriter, r *http.Request, claims tokenClaims, file File)

func (h *Handler) wopiRoutes(mux *http.ServeMux) {
	// Per person and file, not per address: the editor server is one address.
	calls := limiter(time.Minute, 300, "Too many requests", func(r *http.Request) string {
		claims, _ := r.Context().Value(claimsKey{}).(tokenClaims)
		return "wopi:" + strconv.FormatInt(claims.UserID, 10) + ":" + claims.NodeID
	})
	route := func(pattern string, next wopiHandler) {
		limited := calls.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := r.Context().Value(claimsKey{}).(tokenClaims)
			file, err := h.files.OpenOfficeFile(r.Context(), claims.UserID, claims.NodeID, claims.Write == 1)
			if err != nil {
				writeDomainError(w, err)
				return
			}
			next(w, r, claims, file)
		}))
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			token := r.URL.Query().Get("access_token")
			if parts := strings.Fields(r.Header.Get("Authorization")); token == "" && len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				token = parts[1]
			}
			claims, valid := h.verifyToken(token)
			if !valid || claims.NodeID != r.PathValue("id") || !h.currentSettings().Enabled {
				writeError(w, http.StatusUnauthorized, "The editing session is invalid or has ended.", "INVALID_TOKEN")
				return
			}
			limited.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims)))
		})
	}
	route("GET /wopi/files/{id}", h.checkFileInfo)
	route("GET /wopi/files/{id}/contents", h.getFile)
	route("POST /wopi/files/{id}/contents", h.putFile)
	route("POST /wopi/files/{id}", h.lockOperation)
}

func (h *Handler) checkFileInfo(w http.ResponseWriter, _ *http.Request, claims tokenClaims, file File) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"BaseFileName": file.Name, "OwnerId": strconv.FormatInt(file.OwnerID, 10), "UserId": strconv.FormatInt(claims.UserID, 10),
		"UserFriendlyName": file.UserName, "Size": file.Size, "Version": strconv.FormatInt(file.Version, 10),
		"LastModifiedTime": modifiedTime(file.ModifiedAt), "UserCanWrite": file.CanWrite, "UserCanNotWriteRelative": true,
		"SupportsUpdate": true, "SupportsLocks": true, "SupportsGetLock": true, "SupportsRename": false, "UserCanRename": false,
	})
}

func (h *Handler) getFile(w http.ResponseWriter, r *http.Request, claims tokenClaims, file File) {
	w.Header().Set("X-WOPI-ItemVersion", strconv.FormatInt(file.Version, 10))
	h.files.ServeOfficeFile(w, r, claims.UserID, claims.NodeID)
}

func writeChanged(w http.ResponseWriter) {
	// Collabora's own code: it then offers to reload.
	httpx.WriteJSON(w, http.StatusConflict, map[string]int{"COOL_STATUS_CODE": 1010})
}

func (h *Handler) putFile(w http.ResponseWriter, r *http.Request, claims tokenClaims, file File) {
	if !file.CanWrite {
		writeError(w, http.StatusForbidden, "You may only view this file.", "READ_ONLY")
		return
	}
	lock := r.Header.Get("X-WOPI-Lock")
	held, err := h.currentLock(r.Context(), file.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if lock != "" && held != lock {
		writeLockError(w, lockConflict(held))
		return
	}
	// Without a lock only an empty file may be written (a new document).
	if lock == "" && (held != "" || file.Size > 0) {
		writeLockError(w, lockError{status: http.StatusConflict, message: "Lock the file before saving it.", code: "LOCK_REQUIRED", held: held})
		return
	}
	// Collabora sends the modified time it opened the file with; a different
	// one means somebody else saved since.
	stamp := r.Header.Get("X-COOL-WOPI-Timestamp")
	if stamp == "" {
		stamp = r.Header.Get("X-LOOL-WOPI-Timestamp")
	}
	if stamp != "" && stamp != modifiedTime(file.ModifiedAt) {
		writeChanged(w)
		return
	}
	saved, err := h.files.SaveOfficeFile(r.Context(), claims.UserID, claims.NodeID, file.ETag, r.Body, r.ContentLength, lock)
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) && coded.ErrorCode() == "ETAG_MISMATCH" {
		writeChanged(w)
		return
	}
	if errors.As(err, &coded) && coded.ErrorCode() == "WOPI_LOCK_MISMATCH" {
		if held, err = h.currentLock(r.Context(), file.ID); err == nil {
			err = lockConflict(held)
		}
	}
	if err != nil {
		writeLockError(w, err)
		return
	}
	w.Header().Set("X-WOPI-ItemVersion", strconv.FormatInt(saved.Version, 10))
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"LastModifiedTime": modifiedTime(saved.ModifiedAt)})
}

// Lock operations share one URL and tell themselves apart by X-WOPI-Override.
func (h *Handler) lockOperation(w http.ResponseWriter, r *http.Request, _ tokenClaims, file File) {
	operation := strings.ToUpper(r.Header.Get("X-WOPI-Override"))
	if operation != "GET_LOCK" && !file.CanWrite {
		writeError(w, http.StatusForbidden, "You may only view this file.", "READ_ONLY")
		return
	}
	w.Header().Set("X-WOPI-ItemVersion", strconv.FormatInt(file.Version, 10))
	lock := r.Header.Get("X-WOPI-Lock")
	var err error
	switch operation {
	case "LOCK":
		if old := r.Header.Values("X-WOPI-OldLock"); len(old) > 0 {
			err = h.relockFile(r.Context(), file.ID, lock, old[0])
		} else {
			err = h.lockFile(r.Context(), file.ID, lock)
		}
	case "REFRESH_LOCK":
		err = h.relockFile(r.Context(), file.ID, lock, lock)
	case "UNLOCK":
		err = h.unlockFile(r.Context(), file.ID, lock)
	case "GET_LOCK":
		var held string
		held, err = h.currentLock(r.Context(), file.ID)
		w.Header().Set("X-WOPI-Lock", held)
	default:
		writeError(w, http.StatusNotImplemented, "This WOPI operation is not supported.", "NOT_IMPLEMENTED")
		return
	}
	if err != nil {
		writeLockError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// A file has one opaque lock string, valid for 30 minutes unless refreshed.
func (h *Handler) currentLock(ctx context.Context, id string) (string, error) {
	var value string
	err := h.db.QueryRowContext(ctx, `SELECT lock_value FROM wopi_locks WHERE node_id=? AND expires_at>?`, id, h.now().UnixMilli()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// changeLock runs a conditional write; when it matches nothing the caller
// learns which lock the file really has.
func (h *Handler) changeLock(ctx context.Context, id, query string, args ...any) error {
	result, err := h.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed > 0 {
		return err
	}
	held, err := h.currentLock(ctx, id)
	if err != nil {
		return err
	}
	return lockConflict(held)
}

func (h *Handler) lockFile(ctx context.Context, id, wanted string) error {
	if err := requireLock(wanted); err != nil {
		return err
	}
	return h.changeLock(ctx, id, `INSERT INTO wopi_locks(node_id,lock_value,expires_at) VALUES(?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET lock_value=excluded.lock_value,expires_at=excluded.expires_at
		WHERE wopi_locks.expires_at<=? OR wopi_locks.lock_value=excluded.lock_value`,
		id, wanted, h.now().Add(lockTTL).UnixMilli(), h.now().UnixMilli())
}

// relockFile is UnlockAndRelock, and RefreshLock when old is wanted.
func (h *Handler) relockFile(ctx context.Context, id, wanted, old string) error {
	for _, value := range []string{wanted, old} {
		if err := requireLock(value); err != nil {
			return err
		}
	}
	return h.changeLock(ctx, id, `UPDATE wopi_locks SET lock_value=?,expires_at=? WHERE node_id=? AND lock_value=? AND expires_at>?`,
		wanted, h.now().Add(lockTTL).UnixMilli(), id, old, h.now().UnixMilli())
}

func (h *Handler) unlockFile(ctx context.Context, id, wanted string) error {
	if err := requireLock(wanted); err != nil {
		return err
	}
	return h.changeLock(ctx, id, `DELETE FROM wopi_locks WHERE node_id=? AND lock_value=? AND expires_at>?`, id, wanted, h.now().UnixMilli())
}

// lockError carries the lock the file really has in X-WOPI-Lock.
type lockError struct {
	status              int
	message, code, held string
}

func (e lockError) Error() string     { return e.message }
func (e lockError) HTTPStatus() int   { return e.status }
func (e lockError) ErrorCode() string { return e.code }

func requireLock(value string) error {
	if value == "" || len(value) > maxLockLength {
		return lockError{status: http.StatusBadRequest, message: "A lock id is required.", code: "LOCK_REQUIRED"}
	}
	return nil
}

func lockConflict(held string) error {
	return lockError{status: http.StatusConflict, message: "The file is locked by someone else.", code: "LOCK_MISMATCH", held: held}
}

func writeLockError(w http.ResponseWriter, err error) {
	var lock lockError
	if errors.As(err, &lock) && lock.status == http.StatusConflict {
		w.Header().Set("X-WOPI-Lock", lock.held)
	}
	writeDomainError(w, err)
}
