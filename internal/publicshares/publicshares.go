// Package publicshares serves share links: as their owner manages them, and as
// a visitor without an account uses them.
package publicshares

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/jsvalue"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

const failureMessage = "Something went wrong. Please try again."

const (
	unlockCookie   = "nc_share_unlock"
	continueCookie = "nc_share_continue"
	maxListing     = 2000
	unlockAge      = time.Hour
	continueAge    = 12 * time.Hour

	// Wrong passwords per link: from the 5th on each next try waits twice as
	// long; the 10th locks the link for the rest of the window.
	failureWindow       = 30 * time.Minute
	failuresBeforeDelay = 5
	failuresBeforeLock  = 10
	baseDelay           = 2 * time.Second
)

type Options struct {
	DB            *sql.DB
	Keys          *cryptox.Keyring
	Files         *files.Handler
	SessionSecret string
	PublicURL     string
	PublicOrigin  func(*http.Request) string
	SecureCookies bool
	Now           func() time.Time
}

type Handler struct {
	db            *sql.DB
	keys          *cryptox.Keyring
	files         *files.Handler
	secret        []byte
	publicURL     string
	publicOrigin  func(*http.Request) string
	secureCookies bool
	now           func() time.Time
	browseLimit   *ratelimit.Limiter
	unlockLimit   *ratelimit.Limiter
	uploadLimit   *ratelimit.Limiter
}

func New(o Options) (*Handler, error) {
	if o.DB == nil || o.Keys == nil || strings.TrimSpace(o.SessionSecret) == "" {
		return nil, errors.New("publicshares: database, keyring and session secret are required")
	}
	if o.Files == nil {
		return nil, errors.New("publicshares: the file service is required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	h := &Handler{db: o.DB, keys: o.Keys, files: o.Files, secret: []byte(o.SessionSecret), publicURL: strings.TrimSpace(o.PublicURL), publicOrigin: o.PublicOrigin, secureCookies: o.SecureCookies, now: o.Now}
	limiter := func(max int) *ratelimit.Limiter {
		return ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: max, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	}
	h.browseLimit, h.unlockLimit, h.uploadLimit = limiter(600), limiter(20), limiter(100)
	return h, nil
}

func (h *Handler) Routes(mux *http.ServeMux) {
	h.ownerRoutes(mux)
	visitor := func(pattern string, limiter *ratelimit.Limiter, run func(http.ResponseWriter, *http.Request) error) {
		mux.Handle(pattern, limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := run(w, r); err != nil {
				apijson.Fail(w, err, failureMessage)
			}
		})))
	}
	visitor("GET /api/public/shares/{token}", h.browseLimit, h.describe)
	visitor("POST /api/public/shares/{token}/unlock", h.unlockLimit, h.unlock)
	visitor("GET /api/public/shares/{token}/files/{nodeId}/content", h.browseLimit, h.download)
	visitor("GET /api/public/shares/{token}/zip", h.browseLimit, h.downloadZip)
	visitor("PUT /api/public/shares/{token}/upload", h.uploadLimit, h.upload)
}

func notFound() error {
	return apperr.New(http.StatusNotFound, "This link does not exist.", "NOT_FOUND")
}
func uploadOnly() error {
	return apperr.New(http.StatusForbidden, "This link only accepts uploads.", "SHARE_UPLOAD_ONLY")
}
func exhausted() error {
	return apperr.New(http.StatusGone, "This link has reached its download limit.", "SHARE_EXHAUSTED")
}
func notOfType(kind string) error {
	if kind == "folder" {
		return apperr.New(http.StatusBadRequest, "That is not a folder.", "NOT_A_FOLDER")
	}
	return apperr.New(http.StatusBadRequest, "That is not a file.", "NOT_A_FILE")
}

type share struct {
	ID             int64
	UserID         int64
	NodeID         string
	Token          string
	PasswordHash   sql.NullString
	Mode           string
	ExpiresAt      sql.NullString
	MaxDownloads   sql.NullInt64
	DownloadCount  int64
	Note           sql.NullString
	MaxUploadBytes sql.NullInt64
	UploadedBytes  int64
	OwnerUsername  string
}

func (s share) usedUp() bool { return s.MaxDownloads.Valid && s.DownloadCount >= s.MaxDownloads.Int64 }

type node struct {
	ID         string
	Type       string
	Name       string
	Size       int64
	MIME       *string
	ModifiedAt string
	ETag       string
	BlobID     sql.NullString
}

const nodeColumns = `id,type,name,size,mime,modified_at,etag,blob_id`

type scanner interface{ Scan(...any) error }

func scanNode(row scanner) (node, error) {
	var n node
	err := row.Scan(&n.ID, &n.Type, &n.Name, &n.Size, &n.MIME, &n.ModifiedAt, &n.ETag, &n.BlobID)
	return n, err
}

// view is a node as a visitor sees it.
type view struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	MIME       *string `json:"mime"`
	ModifiedAt string  `json:"modifiedAt"`
}

func (n node) view() view {
	return view{n.ID, n.Type, n.Name, n.Size, n.MIME, n.ModifiedAt}
}

// findShare is the link with its state checked: unknown 404, revoked, expired
// or used up 410. A download being resumed defers the last check. A disabled
// owner's links are 404.
func (h *Handler) findShare(ctx context.Context, token string, allowExhausted bool) (share, error) {
	var s share
	var revoked, disabled sql.NullString
	var expired bool
	err := h.db.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.node_id,s.token,s.password_hash,s.mode,s.expires_at,s.revoked_at,s.max_downloads,s.download_count,s.note,s.max_upload_bytes,s.uploaded_bytes,u.username,u.disabled_at,
		(s.expires_at IS NOT NULL AND datetime(s.expires_at)<=datetime('now'))
		FROM shares s JOIN users u ON u.id=s.user_id WHERE s.token=?`, token).Scan(
		&s.ID, &s.UserID, &s.NodeID, &s.Token, &s.PasswordHash, &s.Mode, &s.ExpiresAt, &revoked, &s.MaxDownloads, &s.DownloadCount, &s.Note, &s.MaxUploadBytes, &s.UploadedBytes, &s.OwnerUsername, &disabled, &expired)
	switch {
	case errors.Is(err, sql.ErrNoRows) || err == nil && disabled.Valid:
		return share{}, notFound()
	case err != nil:
		return share{}, err
	case revoked.Valid:
		return share{}, apperr.New(http.StatusGone, "This link has been turned off.", "SHARE_REVOKED")
	case expired:
		return share{}, apperr.New(http.StatusGone, "This link has expired.", "SHARE_EXPIRED")
	case !allowExhausted && s.usedUp():
		return share{}, exhausted()
	}
	return s, nil
}

// liveNode is a node of the owner below their root, with nothing on its path in
// the trash and itself outside any vault.
func (h *Handler) liveNode(ctx context.Context, owner int64, id string) (node, error) {
	n, err := scanNode(h.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id=? AND user_id=? AND parent_id IS NOT NULL AND vault_id IS NULL`, id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		return n, notFound()
	}
	if err != nil {
		return n, err
	}
	var trashed bool
	err = h.db.QueryRowContext(ctx, `WITH RECURSIVE up(parent_id,trashed_at) AS (
		SELECT parent_id,trashed_at FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.parent_id,p.trashed_at FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
	) SELECT EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL)`, id, owner, owner).Scan(&trashed)
	if err != nil {
		return n, err
	}
	if trashed {
		return n, notFound()
	}
	return n, nil
}

// open is the link and its shared node for a request. A password-protected link
// without a valid unlock cookie answers 401.
func (h *Handler) open(r *http.Request, allowExhausted bool) (share, node, error) {
	s, err := h.findShare(r.Context(), r.PathValue("token"), allowExhausted)
	if err != nil {
		return s, node{}, err
	}
	root, err := h.liveNode(r.Context(), s.UserID, s.NodeID)
	if err != nil {
		return s, root, err
	}
	if s.PasswordHash.Valid && !h.validProof("unlock", unlockPayload(s), cookieValue(r, unlockCookie)) {
		return s, root, apperr.New(http.StatusUnauthorized, "This link is protected by a password.", "SHARE_PASSWORD_REQUIRED")
	}
	return s, root, nil
}

type crumb struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// pathInShare is the folders from the shared root down to id; empty when id is
// not inside the share.
func (h *Handler) pathInShare(ctx context.Context, s share, rootID, id string) ([]crumb, error) {
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE up(id,parent_id,name,depth) AS (
		SELECT id,parent_id,name,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.id,p.parent_id,p.name,up.depth+1 FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
	) SELECT id,name FROM up ORDER BY depth DESC`, id, s.UserID, s.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var path []crumb
	for rows.Next() {
		var part crumb
		if err = rows.Scan(&part.ID, &part.Name); err != nil {
			return nil, err
		}
		if len(path) > 0 || part.ID == rootID {
			path = append(path, part)
		}
	}
	return path, rows.Err()
}

// nodeInShare is a node inside the shared subtree, the root when id is empty.
// Anything else looks missing.
func (h *Handler) nodeInShare(ctx context.Context, s share, root node, id, wantType string) (node, error) {
	n := root
	if id != "" && id != root.ID {
		var err error
		if n, err = h.liveNode(ctx, s.UserID, id); err != nil {
			return n, err
		}
		path, err := h.pathInShare(ctx, s, root.ID, id)
		if err != nil {
			return n, err
		}
		if len(path) == 0 {
			return n, notFound()
		}
	}
	if wantType != "" && n.Type != wantType {
		return n, notOfType(wantType)
	}
	return n, nil
}

type summary struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Size            int64  `json:"size"`
	Mode            string `json:"mode"`
	AllowUpload     bool   `json:"allowUpload"`
	ExpiresAt       any    `json:"expiresAt"`
	HasPassword     bool   `json:"hasPassword"`
	SharedBy        string `json:"sharedBy"`
	Host            string `json:"host"`
	Note            any    `json:"note"`
	UploadBytesLeft *int64 `json:"uploadBytesLeft"`
}

type description struct {
	Share summary `json:"share"`
	Node  view    `json:"node"`
	Path  []crumb `json:"path"`
	Items []view  `json:"items"`
}

func (h *Handler) describe(w http.ResponseWriter, r *http.Request) error {
	s, root, err := h.open(r, false)
	if err != nil {
		return err
	}
	host := r.Host
	if origin, err := url.Parse(h.origin(r)); err == nil {
		host = origin.Host
	}
	result := description{
		Share: summary{Name: root.Name, Type: root.Type, Size: root.Size, Mode: s.Mode, AllowUpload: s.Mode != "view" && root.Type == "folder",
			ExpiresAt: nullableString(s.ExpiresAt), HasPassword: s.PasswordHash.Valid, SharedBy: s.OwnerUsername, Host: host, Note: nullableString(s.Note)},
		Node: root.view(), Path: []crumb{{root.ID, root.Name}}, Items: []view{},
	}
	if s.MaxUploadBytes.Valid {
		left := max(0, s.MaxUploadBytes.Int64-s.UploadedBytes)
		result.Share.UploadBytesLeft = &left
	}
	folderID := r.URL.Query().Get("folderId")
	switch {
	case s.Mode == "upload_only":
		if folderID != "" && folderID != root.ID {
			return uploadOnly()
		}
	case root.Type == "file":
		if _, err = h.nodeInShare(r.Context(), s, root, folderID, ""); err != nil {
			return err
		}
	default:
		folder, err := h.nodeInShare(r.Context(), s, root, folderID, "folder")
		if err != nil {
			return err
		}
		result.Node = folder.view()
		if result.Path, err = h.pathInShare(r.Context(), s, root.ID, folder.ID); err != nil {
			return err
		}
		rows, err := h.db.QueryContext(r.Context(), `SELECT `+nodeColumns+` FROM nodes WHERE user_id=? AND parent_id=? AND trashed_at IS NULL AND vault_id IS NULL ORDER BY (type='folder') DESC,lower(name),id LIMIT ?`, s.UserID, folder.ID, maxListing)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			n, err := scanNode(rows)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, n.view())
		}
		if err = rows.Err(); err != nil {
			return err
		}
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

func nullableString(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// Signed cookies a link hands to visitors are "<expiry ms>.<HMAC>", each
// purpose under a key of its own derived from the session secret.
//
//	unlock    the right password was given; tied to the link and its password
//	          hash, so a new password or another link voids it
//	continue  a counted download of one file; lets Range requests resume it
func (h *Handler) signature(purpose, payload string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte("neocloud-share-" + purpose))
	mac = hmac.New(sha256.New, mac.Sum(nil))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func unlockPayload(s share) string {
	return fmt.Sprintf("%d.%s.%s", s.ID, s.Token, s.PasswordHash.String)
}

func continuePayload(s share, nodeID string) string {
	return fmt.Sprintf("%d.%s.%s", s.ID, s.Token, nodeID)
}

func (h *Handler) validProof(purpose, payload, proof string) bool {
	expiryText, signature, ok := strings.Cut(proof, ".")
	expiry, err := strconv.ParseInt(expiryText, 10, 64)
	if !ok || err != nil || expiry <= h.now().UnixMilli() {
		return false
	}
	return passhash.Equal(signature, h.signature(purpose, fmt.Sprintf("%s.%d", payload, expiry)))
}

func (h *Handler) setProof(w http.ResponseWriter, name, path, purpose, payload string, age time.Duration) {
	expiry := h.now().Add(age).UnixMilli()
	value := strconv.FormatInt(expiry, 10) + "." + h.signature(purpose, fmt.Sprintf("%s.%d", payload, expiry))
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, MaxAge: int(age / time.Second), HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: h.secureCookies})
}

func cookiePath(token string) string { return "/api/public/shares/" + url.PathEscape(token) }

func (h *Handler) unlock(w http.ResponseWriter, r *http.Request) error {
	body, err := apijson.Object(w, r)
	if err != nil {
		return err
	}
	s, err := h.findShare(r.Context(), r.PathValue("token"), false)
	if err != nil {
		return err
	}
	if _, err = h.liveNode(r.Context(), s.UserID, s.NodeID); err != nil {
		return err
	}
	if s.PasswordHash.Valid {
		// Each try counts as a failure before bcrypt runs, so parallel guesses
		// count too; the right password clears the link's failures.
		retry, err := h.recordTry(r.Context(), s.ID)
		if err != nil {
			return err
		}
		if retry > 0 {
			seconds := int64((retry + time.Second - 1) / time.Second)
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
			httpx.WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many wrong passwords. Try again later.", "code": "SHARE_LOCKED", "retryAfter": seconds})
			return nil
		}
		if !passhash.Verify(s.PasswordHash.String, jsvalue.String(body["password"])) {
			return apperr.New(http.StatusUnauthorized, "Wrong password.", "SHARE_PASSWORD_INVALID")
		}
		if _, err = h.db.ExecContext(r.Context(), `DELETE FROM share_unlock_failures WHERE share_id=?`, s.ID); err != nil {
			return err
		}
	}
	h.setProof(w, unlockCookie, cookiePath(s.Token), "unlock", unlockPayload(s), unlockAge)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	return nil
}

// recordTry records a password try, or says how long the link still refuses one.
func (h *Handler) recordTry(ctx context.Context, shareID int64) (time.Duration, error) {
	now := h.now().UnixMilli()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM share_unlock_failures WHERE share_id=? AND failed_at_ms<=?`, shareID, now-failureWindow.Milliseconds()); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT failed_at_ms FROM share_unlock_failures WHERE share_id=? ORDER BY failed_at_ms`, shareID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var failures []int64
	for rows.Next() {
		var at int64
		if err = rows.Scan(&at); err != nil {
			return 0, err
		}
		failures = append(failures, at)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	count := len(failures)
	if count >= failuresBeforeLock {
		return max(time.Millisecond, time.Duration(failures[count-failuresBeforeLock]-now)*time.Millisecond+failureWindow), nil
	}
	if count >= failuresBeforeDelay {
		next := failures[count-1] + (baseDelay << (count - failuresBeforeDelay)).Milliseconds()
		if now < next {
			return time.Duration(next-now) * time.Millisecond, nil
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO share_unlock_failures(share_id,failed_at_ms) VALUES(?,?)`, shareID, now); err != nil {
		return 0, err
	}
	return 0, tx.Commit()
}

// origin is where people reach this server.
func (h *Handler) origin(r *http.Request) string {
	if h.publicOrigin != nil {
		if origin := strings.TrimRight(h.publicOrigin(r), "/"); origin != "" {
			return origin
		}
	}
	if parsed, err := url.Parse(h.publicURL); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
