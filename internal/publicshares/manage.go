package publicshares

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/jsvalue"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

const (
	minPasswordLength = 8
	maxPasswordLength = 200
	maxNoteLength     = 500
	maxDownloadLimit  = 1_000_000
)

type managedShare struct {
	ID             int64   `json:"id"`
	Token          string  `json:"token"`
	URL            string  `json:"url"`
	NodeID         string  `json:"nodeId"`
	NodeName       string  `json:"nodeName"`
	NodeType       string  `json:"nodeType"`
	Mode           string  `json:"mode"`
	AllowUpload    bool    `json:"allowUpload"`
	ExpiresAt      *string `json:"expiresAt"`
	Expired        bool    `json:"expired"`
	HasPassword    bool    `json:"hasPassword"`
	Note           *string `json:"note"`
	MaxDownloads   *int64  `json:"maxDownloads"`
	DownloadCount  int64   `json:"downloadCount"`
	Exhausted      bool    `json:"exhausted"`
	MaxUploadBytes *int64  `json:"maxUploadBytes"`
	UploadedBytes  int64   `json:"uploadedBytes"`
	CreatedAt      string  `json:"createdAt"`
}

const notExpired = `(s.expires_at IS NULL OR datetime(s.expires_at)>datetime('now'))`
const notExhausted = `(s.max_downloads IS NULL OR s.download_count<s.max_downloads)`

const managedSelect = `SELECT s.id,s.token,s.node_id,n.name,n.type,s.mode,s.expires_at,s.password_hash IS NOT NULL,s.note,s.max_downloads,s.download_count,s.max_upload_bytes,s.uploaded_bytes,s.created_at,
	NOT ` + notExpired + `, NOT ` + notExhausted + `
	FROM shares s JOIN nodes n ON n.id=s.node_id`

func scanManaged(row scanner, origin string) (managedShare, error) {
	var s managedShare
	err := row.Scan(&s.ID, &s.Token, &s.NodeID, &s.NodeName, &s.NodeType, &s.Mode, &s.ExpiresAt, &s.HasPassword, &s.Note, &s.MaxDownloads, &s.DownloadCount, &s.MaxUploadBytes, &s.UploadedBytes, &s.CreatedAt, &s.Expired, &s.Exhausted)
	s.URL = origin + "/s/" + s.Token
	s.AllowUpload = s.Mode != "view"
	return s, err
}

type ownerAction func(http.ResponseWriter, *http.Request, int64) error

func (h *Handler) ownerRoutes(mux *http.ServeMux) {
	handle := func(pattern string, run ownerAction) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			id, ok := requestauth.CurrentIdentity(r)
			if !ok {
				httpx.ErrorCode(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
				return
			}
			if err := run(w, r, id.UserID); err != nil {
				apijson.Fail(w, err, failureMessage)
			}
		})
	}
	handle("POST /api/shares", h.createShare)
	handle("GET /api/shares", h.listShares)
	handle("PATCH /api/shares/{id}", h.updateShare)
	handle("DELETE /api/shares/{id}", h.revokeShare)
}

// ownShare is one of the account's links that is not revoked.
func (h *Handler) ownShare(ctx context.Context, userID, id int64, origin string) (managedShare, error) {
	s, err := scanManaged(h.db.QueryRowContext(ctx, managedSelect+` WHERE s.id=? AND s.user_id=? AND s.revoked_at IS NULL`, id, userID), origin)
	if errors.Is(err, sql.ErrNoRows) {
		return s, shareNotFound()
	}
	return s, err
}

func shareNotFound() error {
	return apperr.New(http.StatusNotFound, "Share not found", "NOT_FOUND")
}

// shareableNode is a live node of the account that may be linked: not the root
// and nothing in a vault.
func (h *Handler) shareableNode(ctx context.Context, userID int64, id string) (string, string, error) {
	root := apperr.New(http.StatusBadRequest, "The root folder can’t be shared.", "ROOT_IMMUTABLE")
	if id == "root" {
		return "", "", root
	}
	var nodeType string
	var parent, vault sql.NullString
	var trashed bool
	err := h.db.QueryRowContext(ctx, `WITH RECURSIVE up(parent_id,trashed_at) AS (
		SELECT parent_id,trashed_at FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT p.parent_id,p.trashed_at FROM nodes p JOIN up ON p.id=up.parent_id WHERE p.user_id=?
	) SELECT type,parent_id,vault_id,EXISTS(SELECT 1 FROM up WHERE trashed_at IS NOT NULL) FROM nodes WHERE id=? AND user_id=?`,
		id, userID, userID, id, userID).Scan(&nodeType, &parent, &vault, &trashed)
	switch {
	case errors.Is(err, sql.ErrNoRows) || err == nil && trashed:
		return "", "", apperr.New(http.StatusNotFound, "File or folder not found", "NOT_FOUND")
	case err != nil:
		return "", "", err
	case !parent.Valid:
		return "", "", root
	case vault.Valid:
		return "", "", apperr.New(http.StatusConflict, "Vault folders can’t be shared.", "VAULT_NOT_SHAREABLE")
	}
	return id, nodeType, nil
}

func unset(value any) bool { return value == nil || value == "" }

// parseMode reads mode, which wins; the older allowUpload:true still means
// view_upload. view: browse and download. view_upload: also upload.
// upload_only: upload without seeing what is there.
func parseMode(body map[string]any, nodeType string) (string, error) {
	mode := "view"
	if body["allowUpload"] == true {
		mode = "view_upload"
	}
	if value := body["mode"]; !unset(value) {
		text, _ := value.(string)
		switch text {
		case "view", "view_upload", "upload_only":
			mode = text
		default:
			return "", apperr.New(http.StatusBadRequest, "mode must be one of view, view_upload, upload_only.", "INVALID_MODE")
		}
	}
	if mode != "view" && nodeType != "folder" {
		return "", apperr.New(http.StatusBadRequest, "Uploads need a shared folder.", "NOT_A_FOLDER")
	}
	return mode, nil
}

// parseExpiry stores a future date as SQLite UTC text; empty means never.
func (h *Handler) parseExpiry(value any) (any, error) {
	if unset(value) {
		return nil, nil
	}
	invalid := func(message string) (any, error) {
		return nil, apperr.New(http.StatusBadRequest, message, "INVALID_EXPIRY")
	}
	text := jsvalue.String(value)
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		parsed, err = time.Parse("2006-01-02", text)
	}
	// Without a zone a date-time is the server's local time.
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02T15:04"} {
		if err != nil {
			parsed, err = time.ParseInLocation(layout, text, time.Local)
		}
	}
	if err != nil {
		return invalid("expiresAt must be a date.")
	}
	if !parsed.After(h.now()) {
		return invalid("expiresAt must be in the future.")
	}
	return parsed.UTC().Format("2006-01-02 15:04:05"), nil
}

func parseCount(value any, limit int64, message, code string) (any, error) {
	if unset(value) {
		return nil, nil
	}
	count, ok := jsvalue.Integer(value)
	if !ok || count < 1 || count > limit {
		return nil, apperr.New(http.StatusBadRequest, message, code)
	}
	return count, nil
}

func parseMaxDownloads(value any) (any, error) {
	return parseCount(value, maxDownloadLimit, "maxDownloads must be between 1 and 1000000.", "INVALID_MAX_DOWNLOADS")
}

func parseMaxUploadBytes(value any) (any, error) {
	return parseCount(value, maxSafeInteger, "maxUploadBytes must be a positive byte count.", "INVALID_SIZE")
}

func parseNote(value any) (any, error) {
	if unset(value) {
		return nil, nil
	}
	note, ok := value.(string)
	if !ok {
		return nil, apperr.New(http.StatusBadRequest, "note must be text.", "INVALID_NOTE")
	}
	note = strings.TrimSpace(note)
	if len(utf16.Encode([]rune(note))) > maxNoteLength {
		return nil, apperr.New(http.StatusBadRequest, "note can be at most 500 characters.", "NOTE_TOO_LONG")
	}
	if note == "" {
		return nil, nil
	}
	return note, nil
}

// passwordHash is the bcrypt hash of a new link password; empty means none.
func passwordHash(value any) (any, error) {
	if unset(value) {
		return nil, nil
	}
	password := jsvalue.String(value)
	length := len(utf16.Encode([]rune(password)))
	if length < minPasswordLength {
		return nil, apperr.New(http.StatusBadRequest, "The password needs at least 8 characters.", "PASSWORD_TOO_SHORT")
	}
	if length > maxPasswordLength {
		return nil, apperr.New(http.StatusBadRequest, "Password is too long.", "PASSWORD_TOO_LONG")
	}
	return passhash.Hash(password, passhash.DefaultCost)
}

func (h *Handler) createShare(w http.ResponseWriter, r *http.Request, userID int64) error {
	body, err := apijson.Object(w, r)
	if err != nil {
		return err
	}
	nodeID, nodeType, err := h.shareableNode(r.Context(), userID, jsvalue.String(body["nodeId"]))
	if err != nil {
		return err
	}
	mode, err := parseMode(body, nodeType)
	if err != nil {
		return err
	}
	expires, err := h.parseExpiry(body["expiresAt"])
	if err != nil {
		return err
	}
	maxDownloads, err := parseMaxDownloads(body["maxDownloads"])
	if err != nil {
		return err
	}
	maxUploadBytes, err := parseMaxUploadBytes(body["maxUploadBytes"])
	if err != nil {
		return err
	}
	note, err := parseNote(body["note"])
	if err != nil {
		return err
	}
	password, err := passwordHash(body["password"])
	if err != nil {
		return err
	}
	token, err := passhash.RandomToken(24)
	if err != nil {
		return err
	}
	result, err := h.db.ExecContext(r.Context(), `INSERT INTO shares(user_id,node_id,token,password_hash,mode,allow_upload,expires_at,max_downloads,max_upload_bytes,note) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		userID, nodeID, token, password, mode, mode != "view", expires, maxDownloads, maxUploadBytes, note)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	created, err := h.ownShare(r.Context(), userID, id, h.origin(r))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// listShares lists the account's links, newest first. With nodeId: only that
// node's links still usable, so a share dialog can show the existing link.
func (h *Handler) listShares(w http.ResponseWriter, r *http.Request, userID int64) error {
	query := managedSelect + ` WHERE s.user_id=? AND s.revoked_at IS NULL`
	args := []any{userID}
	if nodeID := r.URL.Query().Get("nodeId"); nodeID != "" {
		query += ` AND s.node_id=? AND ` + notExpired + ` AND ` + notExhausted
		args = append(args, nodeID)
	}
	rows, err := h.db.QueryContext(r.Context(), query+` ORDER BY s.id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	origin := h.origin(r)
	shares := []managedShare{}
	for rows.Next() {
		s, err := scanManaged(rows, origin)
		if err != nil {
			return err
		}
		shares = append(shares, s)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"shares": shares})
	return nil
}

func shareID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.New(http.StatusBadRequest, "Invalid share id", "INVALID_ID")
	}
	return id, nil
}

// updateShare changes a link in place; the token, and so the URL, stays. Only
// the fields present change; null clears. A new or removed password signs every
// visitor out and forgets the failed tries.
func (h *Handler) updateShare(w http.ResponseWriter, r *http.Request, userID int64) error {
	id, err := shareID(r)
	if err != nil {
		return err
	}
	body, err := apijson.Object(w, r)
	if err != nil {
		return err
	}
	current, err := h.ownShare(r.Context(), userID, id, h.origin(r))
	if err != nil {
		return err
	}
	var columns []string
	var args []any
	set := func(column string, value any) {
		columns = append(columns, column+"=?")
		args = append(args, value)
	}
	field := func(name, column string, parse func(any) (any, error)) error {
		value, present := body[name]
		if !present {
			return nil
		}
		parsed, err := parse(value)
		if err == nil {
			set(column, parsed)
		}
		return err
	}
	if err = field("expiresAt", "expires_at", h.parseExpiry); err != nil {
		return err
	}
	_, hasMode := body["mode"]
	_, hasAllow := body["allowUpload"]
	if hasMode || hasAllow {
		mode, err := parseMode(body, current.NodeType)
		if err != nil {
			return err
		}
		set("mode", mode)
		set("allow_upload", mode != "view")
	}
	for _, next := range []struct {
		name, column string
		parse        func(any) (any, error)
	}{{"maxDownloads", "max_downloads", parseMaxDownloads}, {"maxUploadBytes", "max_upload_bytes", parseMaxUploadBytes}, {"note", "note", parseNote}, {"password", "password_hash", passwordHash}} {
		if err = field(next.name, next.column, next.parse); err != nil {
			return err
		}
	}
	if len(columns) > 0 {
		tx, err := h.db.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// Column names come from the fixed calls above, never from input.
		if _, err = tx.ExecContext(r.Context(), `UPDATE shares SET `+strings.Join(columns, ",")+` WHERE id=? AND user_id=? AND revoked_at IS NULL`, append(args, id, userID)...); err != nil {
			return err
		}
		if _, changed := body["password"]; changed {
			if _, err = tx.ExecContext(r.Context(), `DELETE FROM share_unlock_failures WHERE share_id=?`, id); err != nil {
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	updated, err := h.ownShare(r.Context(), userID, id, h.origin(r))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

func (h *Handler) revokeShare(w http.ResponseWriter, r *http.Request, userID int64) error {
	id, err := shareID(r)
	if err != nil {
		return err
	}
	result, err := h.db.ExecContext(r.Context(), `UPDATE shares SET revoked_at=datetime('now') WHERE id=? AND user_id=? AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return shareNotFound()
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	return nil
}
