// Package neorouter connects the server and individual accounts to a NeoRouter
// and serves the connection routes.
package neorouter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

type ResolvePrincipal func(*http.Request) (Principal, bool)

// Connector is what the routes do with connections; RuntimeConnector is it.
type Connector interface {
	Describe(context.Context, int64, bool) (any, error)
	Begin(context.Context, string, int64, string) (any, error)
	Status(string, int64) (any, bool)
	SetRoles(context.Context, string, int64, []string) ([]string, error)
	Disconnect(context.Context, string, int64) error
}

type Options struct {
	ResolvePrincipal ResolvePrincipal
	IsAdmin          func(context.Context, int64) (bool, error)
	Connector        Connector
}

type Handler struct {
	resolve   ResolvePrincipal
	isAdmin   func(context.Context, int64) (bool, error)
	connector Connector
}

func New(o Options) (*Handler, error) {
	if o.ResolvePrincipal == nil || o.IsAdmin == nil || o.Connector == nil {
		return nil, errors.New("neorouter: identity, admin check, and connector are required")
	}
	return &Handler{resolve: o.ResolvePrincipal, isAdmin: o.IsAdmin, connector: o.Connector}, nil
}

func (h *Handler) Routes(mux *http.ServeMux) {
	// Connecting makes the server call the address a person typed.
	limiter := ratelimit.New(ratelimit.Config{
		Window: 15 * time.Minute, Max: 30,
		Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"},
	})
	signedIn := func(next func(http.ResponseWriter, *http.Request, Principal)) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p, ok := h.principal(w, r); ok {
				next(w, r, p)
			}
		})
	}
	limited := func(next func(http.ResponseWriter, *http.Request, Principal)) http.Handler {
		return signedIn(func(w http.ResponseWriter, r *http.Request, p Principal) {
			limiter.Middleware(signedIn(next)).ServeHTTP(w, r)
		})
	}
	mux.Handle("GET /api/neorouter", signedIn(h.describe))
	mux.Handle("GET /api/neorouter/{$}", signedIn(h.describe))
	mux.Handle("POST /api/neorouter/connect", limited(h.connect))
	mux.Handle("GET /api/neorouter/connect/{id}", signedIn(h.status))
	mux.Handle("PUT /api/neorouter/{scope}/roles", limited(h.roles))
	mux.Handle("DELETE /api/neorouter/{scope}", limited(h.disconnect))
}

func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return Principal{}, false
	}
	if p.Device && p.Platform != "android" && p.Platform != "ios" {
		writeError(w, http.StatusForbidden, "Use a browser or the NeoCloud phone app for this.", "PHONE_REQUIRED")
		return Principal{}, false
	}
	return p, true
}

func (h *Handler) describe(w http.ResponseWriter, r *http.Request, p Principal) {
	admin, err := h.isAdmin(r.Context(), p.UserID)
	if err != nil {
		internal(w)
		return
	}
	view, err := h.connector.Describe(r.Context(), p.UserID, admin)
	if err != nil {
		internal(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request, p Principal) {
	var body struct {
		Scope   string `json:"scope"`
		Address string `json:"address"`
	}
	if !readBody(w, r, &body) || !h.checkScope(w, r, p.UserID, body.Scope) {
		return
	}
	started, err := h.connector.Begin(r.Context(), body.Scope, p.UserID, strings.TrimSpace(body.Address))
	var refused *ConnectError
	switch {
	case errors.As(err, &refused):
		writeError(w, http.StatusBadRequest, refused.Message, refused.Code)
	case err != nil:
		internal(w)
	default:
		httpx.WriteJSON(w, http.StatusOK, started)
	}
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request, p Principal) {
	view, ok := h.connector.Status(r.PathValue("id"), p.UserID)
	if !ok {
		writeError(w, http.StatusNotFound, "This sign-in expired. Start again.", "NOT_FOUND")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) roles(w http.ResponseWriter, r *http.Request, p Principal) {
	scope := r.PathValue("scope")
	if !h.checkScope(w, r, p.UserID, scope) {
		return
	}
	var body struct {
		Roles json.RawMessage `json:"roles"`
	}
	if !readBody(w, r, &body) {
		return
	}
	var wanted []string
	if json.Unmarshal(body.Roles, &wanted) != nil || wanted == nil {
		writeError(w, http.StatusBadRequest, "roles must be a list.", "INVALID_VALUE")
		return
	}
	roles, err := h.connector.SetRoles(r.Context(), scope, p.UserID, wanted)
	if err != nil {
		internal(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request, p Principal) {
	scope := r.PathValue("scope")
	if !h.checkScope(w, r, p.UserID, scope) {
		return
	}
	if err := h.connector.Disconnect(r.Context(), scope, p.UserID); err != nil {
		internal(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// checkScope allows "user" to everyone and "server" to admins.
func (h *Handler) checkScope(w http.ResponseWriter, r *http.Request, userID int64, scope string) bool {
	if scope != "server" && scope != "user" {
		writeError(w, http.StatusBadRequest, `scope must be "server" or "user".`, "INVALID_SCOPE")
		return false
	}
	if scope == "user" {
		return true
	}
	admin, err := h.isAdmin(r.Context(), userID)
	if err != nil {
		internal(w)
		return false
	}
	if !admin {
		writeError(w, http.StatusForbidden, "Only an admin can connect NeoRouter for the whole server.", "ADMIN_REQUIRED")
	}
	return admin
}

func readBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid request body.", "INVALID_VALUE")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}

func internal(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "Could not process NeoRouter request", "INTERNAL_ERROR")
}
