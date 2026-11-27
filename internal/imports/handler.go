// Package imports brings files over from Nextcloud and other WebDAV servers in
// resumable background jobs. Only a browser session or the phone app may use
// it: a request carries another server's password.
package imports

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/outbound"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

// Principal is the signed-in caller.
type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

// ResolvePrincipal identifies the caller of a request.
type ResolvePrincipal func(*http.Request) (Principal, bool)

// Files is the account's own file tree, where imported items land.
type Files interface {
	OwnNode(context.Context, int64, string) (files.Node, error)
	OwnFolder(context.Context, int64, string) (files.Node, error)
	ChildByName(context.Context, int64, string, string) (files.Node, bool, error)
	EnsureFolderPath(context.Context, int64, string, string) (files.Node, error)
	ImportCopy(context.Context, int64, string, string, io.Reader, int64, string) (files.Node, error)
	SetModifiedAt(context.Context, int64, string, string) error
	MarkFavorite(context.Context, int64, string) error
	RemainingBytes(context.Context, int64) (int64, error)
	CheckUploadFits(context.Context, int64, int64) error
	CheckDiskSpace(int64) error
}

// Options configure New.
type Options struct {
	DB               *sql.DB
	Keys             *cryptox.Keyring
	Files            Files
	ResolvePrincipal ResolvePrincipal
	// AllowPrivate lets every account import from private networks; admins
	// always may.
	AllowPrivate bool
	IsAdmin      func(context.Context, int64) (bool, error)
	Resolve      outbound.Resolver
	Notify       func(userID int64, event string, data map[string]any)
}

// Handler serves /api/imports and runs the jobs.
type Handler struct {
	db           *sql.DB
	keys         *cryptox.Keyring
	files        Files
	resolve      ResolvePrincipal
	allowPrivate bool
	isAdmin      func(context.Context, int64) (bool, error)
	resolveHost  outbound.Resolver
	notify       func(int64, string, map[string]any)
	tests        *ratelimit.Limiter
	starts       *ratelimit.Limiter
	retryDelay   time.Duration

	mu         sync.Mutex
	active     map[string]*run
	stop       context.CancelFunc
	runContext context.Context

	progressMu sync.Mutex
	progressAt map[string]time.Time
}

// New builds the handler. Jobs run between StartWorker and Close.
func New(o Options) (*Handler, error) {
	if o.DB == nil || o.Keys == nil || o.Files == nil || o.ResolvePrincipal == nil {
		return nil, errors.New("imports: database, keyring, files and identity resolver are required")
	}
	limiter := func(max int) *ratelimit.Limiter {
		return ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: max, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	}
	return &Handler{
		db: o.DB, keys: o.Keys, files: o.Files, resolve: o.ResolvePrincipal,
		allowPrivate: o.AllowPrivate, isAdmin: o.IsAdmin, resolveHost: o.Resolve, notify: o.Notify,
		// Each test signs in to another server (keep it from becoming a password
		// oracle); starting or resuming a job does too, just later.
		tests: limiter(20), starts: limiter(20),
		retryDelay: defaultRetryDelay,
		active:     map[string]*run{}, progressAt: map[string]time.Time{},
	}, nil
}

type action func(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error)

// Routes registers the handlers.
func (h *Handler) Routes(mux *http.ServeMux) {
	route := func(pattern string, limiter *ratelimit.Limiter, perform action) {
		mux.Handle(pattern, h.authenticated(limiter, perform))
	}
	route("GET /api/imports", nil, h.list)
	route("POST /api/imports", h.starts, h.create)
	route("POST /api/imports/test", h.tests, h.test)
	route("GET /api/imports/{id}", nil, h.get)
	route("POST /api/imports/{id}/pause", nil, h.pause)
	route("POST /api/imports/{id}/resume", h.starts, h.resume)
	route("POST /api/imports/{id}/cancel", nil, h.cancel)
	route("DELETE /api/imports/{id}", nil, h.delete)
}

// authenticated checks the caller before the limiter counts the request.
func (h *Handler) authenticated(limiter *ratelimit.Limiter, perform action) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := h.resolve(r)
		if !ok || principal.UserID < 1 {
			httpx.ErrorCode(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		if principal.Device && principal.Platform != "android" && principal.Platform != "ios" {
			httpx.ErrorCode(w, http.StatusForbidden, "Use a browser or the NeoCloud phone app for this.", "PHONE_REQUIRED")
			return
		}
		var next http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			status, value, err := perform(w, r, principal.UserID)
			if err != nil {
				apijson.Fail(w, err, "Could not access imports.")
				return
			}
			httpx.WriteJSON(w, status, value)
		})
		if limiter != nil {
			next = limiter.Middleware(next)
		}
		next.ServeHTTP(w, r)
	})
}

// Announce publishes imports:changed for a job. Progress is throttled to one
// event a second; force is for status changes.
func (h *Handler) Announce(userID int64, jobID string, force bool) {
	if h.notify == nil || userID < 1 || jobID == "" {
		return
	}
	h.progressMu.Lock()
	now := time.Now()
	if !force && now.Sub(h.progressAt[jobID]) < time.Second {
		h.progressMu.Unlock()
		return
	}
	if force {
		delete(h.progressAt, jobID)
	} else {
		h.progressAt[jobID] = now
	}
	h.progressMu.Unlock()
	h.notify(userID, "imports:changed", map[string]any{"id": jobID})
}
