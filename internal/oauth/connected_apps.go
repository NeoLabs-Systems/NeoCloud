// Package oauth provides the account-facing connected-app management API.
// The authorization-code protocol and companion bootstrap remain separate.
package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

var validScopes = map[string]struct{}{
	"files:read": {}, "files:write": {}, "signing:read": {}, "signing:write": {},
}

// Principal carries only what this route group needs from authentication.
type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

type ResolvePrincipal func(*http.Request) (Principal, bool)

type Options struct {
	DB               *sql.DB
	ResolvePrincipal ResolvePrincipal
	Now              func() time.Time
}

type Handler struct {
	db      *sql.DB
	resolve ResolvePrincipal
	now     func() time.Time
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("oauth: database and principal resolver are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Handler{db: options.DB, resolve: options.ResolvePrincipal, now: options.Now}, nil
}

// Routes registers GET and DELETE /api/account/connected-apps.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account/connected-apps", h.list)
	mux.HandleFunc("DELETE /api/account/connected-apps/{id}", h.disconnect)
}

type connectedApp struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Host        string   `json:"host"`
	Scopes      []string `json:"scopes"`
	ConnectedAt string   `json:"connectedAt"`
	LastUsedAt  *string  `json:"lastUsedAt"`
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		httpx.WriteJSON(w, 401, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
		return Principal{}, false
	}
	if p.Device && p.Platform != "android" && p.Platform != "ios" {
		httpx.WriteJSON(w, 403, map[string]string{"error": "Use a browser or the NeoCloud phone app for this.", "code": "PHONE_REQUIRED"})
		return Principal{}, false
	}
	return p, true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	apps, err := h.listForUser(r.Context(), p.UserID)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not read connected apps", "code": "INTERNAL_ERROR"})
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"apps": apps})
}

func (h *Handler) listForUser(ctx context.Context, userID int64) ([]connectedApp, error) {
	now := h.now().UTC().Format(time.RFC3339Nano)
	rows, err := h.db.QueryContext(ctx, `SELECT c.id,c.name,c.redirect_uri,
      (SELECT MIN(r.created_at) FROM oauth_refresh_tokens r WHERE r.client_id=c.id AND r.user_id=?) AS connected_at,
      (SELECT MAX(t.last_used_at) FROM oauth_access_tokens t WHERE t.client_id=c.id AND t.user_id=?) AS last_used_at,
      (SELECT r.scopes_json FROM oauth_refresh_tokens r WHERE r.client_id=c.id AND r.user_id=? AND r.revoked_at IS NULL AND r.expires_at>? ORDER BY r.id DESC LIMIT 1) AS scopes_json
    FROM oauth_clients c WHERE c.revoked_at IS NULL AND EXISTS(SELECT 1 FROM oauth_refresh_tokens r WHERE r.client_id=c.id AND r.user_id=? AND r.revoked_at IS NULL AND r.expires_at>?) ORDER BY connected_at`, userID, userID, userID, now, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	apps := []connectedApp{}
	for rows.Next() {
		var app connectedApp
		var redirect, scopes string
		var lastUsed sql.NullString
		if err = rows.Scan(&app.ID, &app.Name, &redirect, &app.ConnectedAt, &lastUsed, &scopes); err != nil {
			return nil, err
		}
		host, parseErr := callbackHost(redirect)
		if parseErr != nil {
			return nil, parseErr
		}
		app.Host = host
		app.Scopes = parseScopes(scopes)
		if lastUsed.Valid {
			app.LastUsedAt = &lastUsed.String
		}
		apps = append(apps, app)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	return apps, nil
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	clientID := r.PathValue("id")
	if clientID == "" {
		httpx.WriteJSON(w, 404, map[string]string{"error": "Connected app not found", "code": "NOT_FOUND"})
		return
	}
	now := h.now().UTC().Format(time.RFC3339Nano)
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not disconnect app", "code": "INTERNAL_ERROR"})
		return
	}
	defer tx.Rollback()
	var live int
	err = tx.QueryRowContext(r.Context(), `SELECT 1 FROM oauth_clients c WHERE c.id=? AND c.revoked_at IS NULL AND EXISTS(SELECT 1 FROM oauth_refresh_tokens r WHERE r.client_id=c.id AND r.user_id=? AND r.revoked_at IS NULL AND r.expires_at>?)`, clientID, p.UserID, now).Scan(&live)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "Connected app not found", "code": "NOT_FOUND"})
		return
	}
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not disconnect app", "code": "INTERNAL_ERROR"})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE oauth_access_tokens SET revoked_at=? WHERE user_id=? AND client_id=? AND revoked_at IS NULL`, now, p.UserID, clientID); err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE oauth_refresh_tokens SET revoked_at=? WHERE user_id=? AND client_id=? AND revoked_at IS NULL`, now, p.UserID, clientID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE oauth_codes SET consumed_at=? WHERE user_id=? AND client_id=? AND consumed_at IS NULL`, now, p.UserID, clientID)
	}
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not disconnect app", "code": "INTERNAL_ERROR"})
		return
	}
	if err = tx.Commit(); err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not disconnect app", "code": "INTERNAL_ERROR"})
		return
	}
	httpx.WriteJSON(w, 200, map[string]bool{"success": true})
}

func callbackHost(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("oauth: invalid stored redirect URI")
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	return host, nil
}

func parseScopes(value string) []string {
	var parsed []string
	if json.Unmarshal([]byte(value), &parsed) != nil {
		return []string{}
	}
	out := make([]string, 0, len(parsed))
	seen := map[string]bool{}
	for _, scope := range parsed {
		if _, ok := validScopes[scope]; ok && !seen[scope] {
			seen[scope] = true
			out = append(out, scope)
		}
	}
	return out
}
