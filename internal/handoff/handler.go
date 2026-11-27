// Package handoff is "Send to device": files from one of an account's devices
// to another. Every route knows who is asking as a device: a signed-in app by
// its token, a browser by its session.
package handoff

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

const (
	maxFiles      = 50
	maxTotalBytes = int64(2) << 30
	maxOpenOffers = 10
	maxOpenBytes  = int64(4) << 30
	// A send must be fully uploaded within this long, then it waits for its receiver.
	uploadWindow = "+60 minutes"
	waitingTTL   = "+24 hours"
	// Finished sends stay listed for the sender this long.
	keepFinished = "-3 days"
	internalText = "The send could not be completed."
)

// Principal is the signed-in caller.
type Principal struct {
	UserID, DeviceID int64
	IsDevice         bool
	Platform         string
}

// Files is what a send needs from the account's file tree and storage rules.
type Files interface {
	OwnNode(ctx context.Context, userID int64, id string) (files.Node, error)
	CheckUploadFits(ctx context.Context, userID, size int64) error
	CheckDiskSpace(needed int64) error
	RemoveBlobFiles(userID int64, ids []string)
}

// Options configure a Handler.
type Options struct {
	DB               *sql.DB
	Keys             *cryptox.Keyring
	BlobDir          string
	Files            Files
	ResolvePrincipal func(*http.Request) (Principal, bool)
	// RequestKey addresses the caller: "d:<token id>" or "s:<session hash prefix>".
	RequestKey func(*http.Request, Principal) string
}

// Handler serves /api/handoff and knows which devices are online.
type Handler struct {
	db       *sql.DB
	keys     *cryptox.Keyring
	blobDir  string
	files    Files
	options  Options
	presence *presence
	// recheck is how often an event stream confirms its device is still signed in.
	recheck time.Duration

	creates, uploads, actions *ratelimit.Limiter
}

// New builds the handler.
func New(options Options) (*Handler, error) {
	if options.DB == nil || options.Keys == nil || options.BlobDir == "" || options.Files == nil || options.ResolvePrincipal == nil || options.RequestKey == nil {
		return nil, errors.New("handoff: database, keyring, blob directory, files and principal resolvers are required")
	}
	limiter := func(max int) *ratelimit.Limiter {
		return ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: max, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	}
	return &Handler{
		db: options.DB, keys: options.Keys, blobDir: options.BlobDir, files: options.Files, options: options,
		presence: newPresence(), recheck: 5 * time.Second,
		creates: limiter(60), uploads: limiter(600), actions: limiter(300),
	}, nil
}

type caller struct {
	Principal
	key string
}

type action func(w http.ResponseWriter, r *http.Request, c caller) error

// Routes mounts the handler.
func (h *Handler) Routes(mux *http.ServeMux) {
	route := func(pattern string, limiter *ratelimit.Limiter, perform action) {
		var handler http.Handler = h.authenticated(perform)
		if limiter != nil {
			handler = limiter.Middleware(handler)
		}
		mux.Handle(pattern, handler)
	}
	route("GET /api/handoff/devices", nil, h.listDevices)
	route("GET /api/handoff/events", nil, h.events)
	route("GET /api/handoff/offers", nil, h.list)
	route("POST /api/handoff/offers", h.creates, h.create)
	route("GET /api/handoff/offers/{id}", nil, h.get)
	route("PUT /api/handoff/offers/{id}/files/{fileId}", h.uploads, h.upload)
	route("POST /api/handoff/offers/{id}/accept", h.actions, h.change(h.accept))
	route("POST /api/handoff/offers/{id}/decline", h.actions, h.change(h.decline))
	route("POST /api/handoff/offers/{id}/complete", h.actions, h.change(h.complete))
	route("DELETE /api/handoff/offers/{id}", h.actions, h.change(h.cancel))
	route("GET /api/handoff/offers/{id}/files/{fileId}", nil, h.download)
	route("GET /api/handoff/offers/{id}/zip", nil, h.zip)
}

func (h *Handler) authenticated(perform action) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := h.options.ResolvePrincipal(r)
		if !ok || principal.UserID < 1 {
			httpx.ErrorCode(w, http.StatusUnauthorized, "Authentication required", "AUTH_REQUIRED")
			return
		}
		key := h.options.RequestKey(r, principal)
		if key == "" {
			httpx.ErrorCode(w, http.StatusUnauthorized, "Authentication required", "AUTH_REQUIRED")
			return
		}
		if err := perform(w, r, caller{principal, key}); err != nil {
			apijson.Fail(w, err, internalText)
		}
	})
}

func failure(status int, message, code string) error { return apperr.New(status, message, code) }

func (h *Handler) listDevices(w http.ResponseWriter, r *http.Request, c caller) error {
	devices, err := h.devices(r.Context(), c.UserID)
	if err != nil {
		return err
	}
	for i := range devices {
		current := devices[i].ID == c.key
		devices[i].Current = &current
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"devices": devices})
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, c caller) error {
	sent, err := h.sent(r.Context(), c.UserID, c.key)
	if err != nil {
		return err
	}
	inbox, err := h.inbox(r.Context(), c.UserID, c.key)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sent": sent, "inbox": inbox})
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, c caller) error {
	o, err := h.offerFor(r.Context(), c.UserID, c.key, r.PathValue("id"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, o)
	return nil
}

func (h *Handler) change(apply func(ctx context.Context, c caller, id string) (offer, error)) action {
	return func(w http.ResponseWriter, r *http.Request, c caller) error {
		o, err := apply(r.Context(), c, r.PathValue("id"))
		if err != nil {
			return err
		}
		httpx.WriteJSON(w, http.StatusOK, o)
		return nil
	}
}
