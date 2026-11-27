// Package signing is NeoCloud's e-signature feature: documents their owner
// prepares and sends, the pages signers open with a link, accounts that sign,
// signing mail and the background work that seals finished files.
package signing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/agentapi"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
)

// Principal is the signed-in account behind a request.
type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

type Options struct {
	DB                *sql.DB
	Keys              *cryptox.Keyring
	BlobDir           string
	DefaultQuotaBytes int64
	ResolvePrincipal  func(*http.Request) (Principal, bool)
	// StoreFile saves a finished file through the file service (quota,
	// versions, activity, sync journal) and returns its node id.
	StoreFile func(ctx context.Context, userID int64, parentID, name string, content []byte) (string, error)
	// RemoveBlobs deletes blob files nothing references any more.
	RemoveBlobs func(userID int64, ids []string)
	// Agent serves the connected-app routes; nil leaves them out.
	Agent *agentapi.Handler
	// MailConfig is the live service mail setup.
	MailConfig func() servicemail.Config
	// MailEnabled is mail being configured and signing mail not switched off.
	MailEnabled  func() bool
	SendMail     func(ctx context.Context, to string, message servicemail.Message) error
	PublicOrigin func(*http.Request) string
	// Notify pushes a live event to an account's open sessions.
	Notify func(userID int64, name string, data map[string]any)
	// NoSeal turns the digital seal of finished files off.
	NoSeal bool
}

type Handler struct {
	db           *sql.DB
	keys         *cryptox.Keyring
	blobDir      string
	defaultQuota atomic.Int64
	resolve      func(*http.Request) (Principal, bool)
	storeFile    func(context.Context, int64, string, string, []byte) (string, error)
	// removeBlobs deletes unreferenced blob files and their previews.
	removeBlobs  func(userID int64, ids []string)
	agent        *agentapi.Handler
	mailConfig   func() servicemail.Config
	mailEnabled  func() bool
	sendMail     func(context.Context, string, servicemail.Message) error
	publicOrigin func(*http.Request) string
	notifyUser   func(int64, string, map[string]any)
	noSeal       bool
	now          func() time.Time

	finalizeWake chan struct{}
	mailWake     chan struct{}
	// buildMu allows one PDF build at a time: building and sealing a large
	// file takes several times its size in memory.
	buildMu    sync.Mutex
	autosaveMu sync.Mutex
	// cleanMu shares one build between simultaneous first requests.
	cleanMu         sync.Mutex
	attachmentLimit int64
}

func New(o Options) (*Handler, error) {
	if o.DB == nil || o.Keys == nil || o.ResolvePrincipal == nil || o.BlobDir == "" || o.StoreFile == nil || o.RemoveBlobs == nil {
		return nil, errors.New("signing: database, keyring, blob directory, file store and identity resolver are required")
	}
	h := &Handler{
		db: o.DB, keys: o.Keys, blobDir: o.BlobDir, resolve: o.ResolvePrincipal, storeFile: o.StoreFile, removeBlobs: o.RemoveBlobs, agent: o.Agent,
		mailConfig: o.MailConfig, mailEnabled: o.MailEnabled, sendMail: o.SendMail, publicOrigin: o.PublicOrigin,
		notifyUser: o.Notify, noSeal: o.NoSeal, now: time.Now,
		finalizeWake: make(chan struct{}, 1), mailWake: make(chan struct{}, 1), attachmentLimit: mailAttachmentMax,
	}
	h.defaultQuota.Store(o.DefaultQuotaBytes)
	if h.mailConfig == nil {
		h.mailConfig = func() servicemail.Config { return servicemail.Config{} }
	}
	if h.mailEnabled == nil || h.sendMail == nil {
		h.mailEnabled = func() bool { return false }
	}
	if h.notifyUser == nil {
		h.notifyUser = func(int64, string, map[string]any) {}
	}
	return h, nil
}

// SetDefaultQuota updates the quota of accounts without one of their own.
func (h *Handler) SetDefaultQuota(bytes int64) { h.defaultQuota.Store(bytes) }

// apiError is a refusal as clients see it.
type apiError struct {
	status     int
	message    string
	code       string
	fieldIDs   []string
	retryAfter int
}

func (e *apiError) Error() string { return e.message }

func httpError(status int, message, code string) *apiError {
	return &apiError{status: status, message: message, code: code}
}

func badRequest(message string) *apiError {
	return httpError(http.StatusBadRequest, message, "INVALID_REQUEST")
}

func writeErr(w http.ResponseWriter, err error) {
	var refusal *apiError
	if !errors.As(err, &refusal) {
		// Errors of the file service carry their own status and code.
		var coded interface {
			error
			HTTPStatus() int
			ErrorCode() string
		}
		if errors.As(err, &coded) {
			refusal = httpError(coded.HTTPStatus(), coded.Error(), coded.ErrorCode())
		}
	}
	if refusal == nil {
		slog.Error("signing request failed", "error", err)
		refusal = httpError(http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
	}
	body := map[string]any{"error": refusal.message, "code": refusal.code}
	if refusal.fieldIDs != nil {
		body["fieldIds"] = refusal.fieldIDs
	}
	if refusal.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(refusal.retryAfter))
		body["retryAfter"] = refusal.retryAfter
	}
	if refusal.status == http.StatusRequestEntityTooLarge {
		// A refused upload may still be sending its body.
		w.Header().Set("Connection", "close")
	}
	httpx.WriteJSON(w, refusal.status, body)
}

func respond(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.WriteJSON(w, status, value)
}

// body is a decoded JSON object request; keys that are absent stay absent.
type body map[string]any

const maxJSONBody = 1 << 20

func readBody(w http.ResponseWriter, r *http.Request) (body, error) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") {
		return body{}, nil
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		if errors.Is(err, io.EOF) {
			return body{}, nil
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, httpError(http.StatusRequestEntityTooLarge, "The request body is too large.", "BODY_TOO_LARGE")
		}
		return nil, httpError(http.StatusBadRequest, "The request body is not valid JSON.", "INVALID_JSON")
	}
	object, _ := value.(map[string]any)
	if object == nil {
		return body{}, nil
	}
	return plainNumbers(object).(map[string]any), nil
}

// plainNumbers turns json.Number into float64 throughout.
func plainNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		f, _ := v.Float64()
		return f
	case map[string]any:
		for key, item := range v {
			v[key] = plainNumbers(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = plainNumbers(item)
		}
		return v
	default:
		return v
	}
}

// ownerFunc is a route of the signed-in account.
type ownerFunc func(w http.ResponseWriter, r *http.Request, userID int64)

func limiter(max int, key func(*http.Request) string) *ratelimit.Limiter {
	return ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: max, Key: key,
		Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
}

type userKey struct{}

// session admits a browser session or the phone app.
func (h *Handler) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.resolve(r)
		if !ok || p.UserID < 1 || p.Device && p.Platform == "webdav" {
			httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
			return
		}
		if p.Device && p.Platform != "android" && p.Platform != "ios" {
			httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "Use a browser or the NeoCloud phone app for this.", "code": "PHONE_REQUIRED"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, p.UserID)))
	})
}

func userOf(r *http.Request) int64 {
	id, _ := r.Context().Value(userKey{}).(int64)
	return id
}

// Routes registers the owner, signer and connected-app routes.
func (h *Handler) Routes(mux *http.ServeMux) {
	general := limiter(1500, nil)
	// Creating and sending work on whole files: a tighter bound.
	heavy := limiter(200, nil)
	// Mail goes to other people: a tight bound on reminders.
	remind := limiter(60, nil)
	claim := limiter(60, func(r *http.Request) string { return "claim:" + strconv.FormatInt(userOf(r), 10) })

	route := func(pattern string, handler ownerFunc, extra ...*ratelimit.Limiter) {
		var next http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler(w, r, userOf(r)) })
		for _, l := range extra {
			next = l.Middleware(next)
		}
		method, path, _ := strings.Cut(pattern, " ")
		mux.Handle(method+" /api/signing"+path, h.session(general.Middleware(next)))
	}
	// call serves a route whose result is JSON.
	call := func(pattern string, status int, action func(r *http.Request, userID int64, input body) (any, error), extra ...*ratelimit.Limiter) {
		route(pattern, func(w http.ResponseWriter, r *http.Request, userID int64) {
			input, err := readBody(w, r)
			if err != nil {
				writeErr(w, err)
				return
			}
			value, err := action(r, userID, input)
			respond(w, status, value, err)
		}, extra...)
	}

	call("GET /capabilities", 200, func(*http.Request, int64, body) (any, error) { return h.capabilities(), nil })
	call("POST /documents", 201, h.createDocument, heavy)
	call("GET /documents", 200, h.listDocuments)
	call("GET /documents/{id}", 200, h.getDocument)
	call("PUT /documents/{id}", 200, h.updateSettings)
	call("DELETE /documents/{id}", 200, h.deleteDocument)
	call("PUT /documents/{id}/fields", 200, h.saveFields)
	call("PUT /documents/{id}/parties", 200, h.saveParties)
	call("POST /documents/{id}/import-fields", 200, h.importFields, heavy)
	call("POST /documents/{id}/send", 200, h.sendRoute, heavy)
	call("POST /documents/{id}/cancel", 200, h.cancelDocument)
	call("POST /documents/{id}/duplicate", 201, h.duplicateDocument, heavy)
	call("POST /documents/{id}/finalize", 200, h.retryFinalization)
	call("POST /documents/{id}/parties/{partyId}/link", 200, h.regenerateLinkRoute)
	call("POST /documents/{id}/parties/{partyId}/revoke", 200, h.revokeLink)
	call("POST /documents/{id}/parties/{partyId}/remind", 202, h.remindParty, remind)
	call("POST /documents/{id}/remind-all", 202, h.remindAll, remind)
	call("POST /documents/{id}/parties/{partyId}/open-as-owner", 200, h.openAsOwner)
	route("GET /documents/{id}/pdf", h.ownerPDF)
	call("GET /documents/{id}/preview-values", 200, h.previewValues)
	route("GET /documents/{id}/values/{fieldId}/image", h.ownerValueImage)
	call("GET /documents/{id}/events", 200, h.eventsRoute)
	route("GET /documents/{id}/audit.json", h.auditJSON)
	route("GET /documents/{id}/final", h.ownerFinal)
	route("GET /nodes/{nodeId}/clean", h.cleanForNode)
	call("GET /profile", 200, h.getProfile)
	call("PUT /profile", 200, h.putProfile)
	route("POST /profile/images", h.addProfileImage, heavy)
	route("GET /profile/images/{id}", h.profileImage)
	call("PUT /profile/images/{id}", 200, h.updateProfileImage)
	call("DELETE /profile/images/{id}", 200, h.deleteProfileImage)
	call("POST /claim", 200, h.claimParty, claim)
	call("GET /inbox", 200, h.listInbox)
	call("POST /inbox/{partyId}/open", 200, h.openInbox, claim)
	route("GET /inbox/{partyId}/final", h.inboxFinal)

	h.publicRoutes(mux)
	if h.agent != nil {
		h.agentRoutes(mux)
	}
}
