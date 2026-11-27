// Package externalstorage lets an account connect its own cloud storage (S3,
// WebDAV, SFTP) and browse it beside its files. Nothing is copied into
// NeoCloud unless the person copies it. Only a browser session or the phone
// app may use it: the connection settings hold other servers' passwords.
package externalstorage

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
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

const defaultMaxUploadBytes = 20 << 30

// Principal is the signed-in caller.
type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

// ResolvePrincipal identifies the caller of a request.
type ResolvePrincipal func(*http.Request) (Principal, bool)

// Files is the account's own file tree, for copies in both directions.
type Files interface {
	OwnFolder(context.Context, int64, string) (files.Node, error)
	ExternalCopySource(context.Context, int64, string) (files.Node, io.ReadCloser, error)
	ImportCopy(context.Context, int64, string, string, io.Reader, int64, string) (files.Node, error)
}

// Options configure New.
type Options struct {
	DB               *sql.DB
	Keys             *cryptox.Keyring
	ResolvePrincipal ResolvePrincipal
	// AllowPrivate lets every account reach private networks; admins always may.
	AllowPrivate   bool
	IsAdmin        func(context.Context, int64) (bool, error)
	Resolve        outbound.Resolver
	MaxUploadBytes int64
}

// Handler serves /api/external-storage.
type Handler struct {
	db             *sql.DB
	keys           *cryptox.Keyring
	resolve        ResolvePrincipal
	allowPrivate   bool
	isAdmin        func(context.Context, int64) (bool, error)
	resolveHost    outbound.Resolver
	maxUploadBytes int64
	files          Files
	connects       *ratelimit.Limiter
	operations     *ratelimit.Limiter
	testTimeout    time.Duration
	activeMu       sync.Mutex
	active         map[int64]int
}

// New builds the handler.
func New(o Options) (*Handler, error) {
	if o.DB == nil || o.Keys == nil || o.ResolvePrincipal == nil {
		return nil, errors.New("externalstorage: database, keyring and identity resolver are required")
	}
	if o.MaxUploadBytes <= 0 {
		o.MaxUploadBytes = defaultMaxUploadBytes
	}
	limiter := func(max int) *ratelimit.Limiter {
		return ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: max, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	}
	return &Handler{
		db: o.DB, keys: o.Keys, resolve: o.ResolvePrincipal, allowPrivate: o.AllowPrivate, isAdmin: o.IsAdmin,
		resolveHost: o.Resolve, maxUploadBytes: o.MaxUploadBytes,
		// Each test or connect signs in to another server; keep this from becoming a probe.
		connects: limiter(30), operations: limiter(3000),
		testTimeout: 45 * time.Second, active: map[int64]int{},
	}, nil
}

// SetFiles connects the account file tree before the server starts serving.
func (h *Handler) SetFiles(f Files) { h.files = f }

type action func(w http.ResponseWriter, r *http.Request, userID int64) (int, any, error)

// Routes registers the handlers.
func (h *Handler) Routes(mux *http.ServeMux) {
	route := func(pattern string, limiter *ratelimit.Limiter, run action) {
		mux.Handle(pattern, h.authenticated(limiter, run))
	}
	route("GET /api/external-storage", nil, h.list)
	route("POST /api/external-storage/test", h.connects, h.test)
	route("POST /api/external-storage", h.connects, h.connect)
	route("PATCH /api/external-storage/{id}", h.connects, h.update)
	route("DELETE /api/external-storage/{id}", nil, h.disconnect)
	route("GET /api/external-storage/{id}/list", h.operations, h.listFolder)
	route("GET /api/external-storage/{id}/file", h.operations, h.download)
	route("PUT /api/external-storage/{id}/file", h.operations, h.upload)
	route("POST /api/external-storage/{id}/folder", h.operations, h.createFolder)
	route("POST /api/external-storage/{id}/move", h.operations, h.move)
	route("DELETE /api/external-storage/{id}/entry", h.operations, h.remove)
	route("POST /api/external-storage/{id}/copy-in", h.operations, h.copyIn)
	route("POST /api/external-storage/{id}/copy-out", h.operations, h.copyOut)
}

// authenticated checks the caller before the limiter counts the request. An
// action that already wrote its answer returns status 0.
func (h *Handler) authenticated(limiter *ratelimit.Limiter, run action) http.Handler {
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
			status, value, err := run(w, r, principal.UserID)
			switch {
			case err != nil:
				apijson.Fail(w, err, "Could not access external storage.")
			case status != 0:
				httpx.WriteJSON(w, status, value)
			}
		})
		if limiter != nil {
			next = limiter.Middleware(next)
		}
		next.ServeHTTP(w, r)
	})
}

func tooManyTransfers() error {
	return apperr.New(http.StatusTooManyRequests, "Too many transfers at once. Wait for one to finish.", "RATE_LIMITED")
}
