// Package ai is NeoCloud's AI: its status, the review queue of suggestions and
// the jobs behind it, sorting rules, summaries, duplicates and the folders AI
// must never read.
package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

// Principal is the signed-in caller: a browser session or a device token.
type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

type ResolvePrincipal func(*http.Request) (Principal, bool)

// RoleStatus is the status value returned for one AI role.
type RoleStatus struct {
	Ready                bool    `json:"ready"`
	Model                *string `json:"model"`
	Source               *string `json:"source"`
	RequestedUnavailable bool    `json:"requestedUnavailable"`
}

type ResolveRoles func(context.Context, int64, map[string]any) (map[string]RoleStatus, error)
type NotifyEvent func(int64, string, map[string]any)

// Models talks to the account's configured models and meters the usage.
type Models interface {
	ChatJSONForUserRole(ctx context.Context, userID int64, feature, role string, messages []llm.Message) (map[string]any, string, error)
	ChatForUserRole(ctx context.Context, userID int64, feature, role string, messages []llm.Message) (string, string, error)
	EmbedForUser(ctx context.Context, userID int64, feature string, texts []string) ([][]float32, string, error)
}

// Tree is the file operations AI applies suggestions with.
type Tree interface {
	UpdateTx(tx *sql.Tx, userID int64, nodeID string, name, parentID *string, numbered bool) (newName, newParentID string, err error)
	EnsureFolderTx(tx *sql.Tx, userID int64, parentID, name string) (folderID string, created bool, err error)
	TrashTx(tx *sql.Tx, userID int64, nodeID string) (parentID string, err error)
	FreeName(ctx context.Context, parentID, name, ignoreID string) (string, error)
	NodeViews(ctx context.Context, userID int64, ids []string) (map[string]any, error)
}

// Index is the full-text index's view of a file.
type Index interface {
	ReadIndexedText(ctx context.Context, userID int64, node searchindex.Node) (status, text string, err error)
	IsEnabled() bool
}

type Options struct {
	DB                   *sql.DB
	ResolvePrincipal     ResolvePrincipal
	ResolveRoles         ResolveRoles
	Enabled              bool
	DefaultMonthlyTokens int64
	NotifyEvent          NotifyEvent

	Files    Tree
	Keys     *cryptox.Keyring
	Models   Models
	ReadBlob searchindex.BlobReader
	Index    Index
	// ModelError turns a failed model call into the refusal to answer with.
	ModelError func(error) error
	// PhotoCatchUp brings the Photos index up to date before Duplicates reads it.
	PhotoCatchUp func(context.Context, int64) error
}

type Handler struct {
	db      *sql.DB
	options Options
	limiter *ratelimit.Limiter

	policyMu             sync.RWMutex
	enabled              bool
	defaultMonthlyTokens int64

	// One job pass at a time.
	passMu sync.Mutex
	wake   chan struct{}
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("ai: database and principal resolver are required")
	}
	return &Handler{
		db: options.DB, options: options, enabled: options.Enabled, defaultMonthlyTokens: options.DefaultMonthlyTokens,
		limiter: ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: 300, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}}),
		wake:    make(chan struct{}, 1),
	}, nil
}

// SetPolicy applies the server-wide AI switch and default monthly budget.
func (h *Handler) SetPolicy(enabled bool, defaultMonthlyTokens int64) {
	h.policyMu.Lock()
	h.enabled, h.defaultMonthlyTokens = enabled, defaultMonthlyTokens
	h.policyMu.Unlock()
}

func (h *Handler) policy() (bool, int64) {
	h.policyMu.RLock()
	defer h.policyMu.RUnlock()
	return h.enabled, h.defaultMonthlyTokens
}

// Error is a refusal with the status and code the apps branch on.
type Error struct {
	Status  int
	Message string
	Code    string
}

func (e *Error) Error() string     { return e.Message }
func (e *Error) HTTPStatus() int   { return e.Status }
func (e *Error) ErrorCode() string { return e.Code }

// DirectFeatureError is the refusal CheckDirectFeature answers with.
type DirectFeatureError = Error

func fail(status int, message, code string) error { return &Error{status, message, code} }

type statusError interface {
	error
	HTTPStatus() int
	ErrorCode() string
}

// codeOf is the application error code of an error, or "FAILED".
func codeOf(err error) string {
	var refusal statusError
	if errors.As(err, &refusal) {
		return refusal.ErrorCode()
	}
	return "FAILED"
}

func statusOf(err error) int {
	var refusal statusError
	if errors.As(err, &refusal) {
		return refusal.HTTPStatus()
	}
	return http.StatusInternalServerError
}

func writeError(w http.ResponseWriter, err error) {
	var refusal statusError
	if errors.As(err, &refusal) {
		httpx.WriteJSON(w, refusal.HTTPStatus(), map[string]string{"error": refusal.Error(), "code": refusal.ErrorCode()})
		return
	}
	slog.Error("AI request failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Something went wrong. Please try again."})
}

// body is a JSON request body; absent keys read as nil.
type body map[string]any

const maxBodyBytes = 1 << 20

func readBody(w http.ResponseWriter, r *http.Request) (body, error) {
	if r.Body == nil || r.Method == http.MethodGet {
		return body{}, nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, fail(http.StatusRequestEntityTooLarge, "The request body is too large.", "BODY_TOO_LARGE")
		}
		return nil, fail(http.StatusBadRequest, "The request body could not be read.", "INVALID_BODY")
	}
	if len(raw) == 0 {
		return body{}, nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, fail(http.StatusBadRequest, "The request body is not valid JSON.", "INVALID_JSON")
	}
	object, _ := value.(map[string]any)
	if object == nil {
		return body{}, nil
	}
	return object, nil
}

type action func(r *http.Request, userID int64, input body) (any, error)

// route serves an action to a browser session or the phone app; desktop tokens
// don't reach AI. limits apply once the caller is known.
func (h *Handler) route(status int, run action, limits ...func(http.Handler) http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := h.options.ResolvePrincipal(r)
		if !ok || principal.UserID < 1 {
			httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
			return
		}
		if principal.Device && principal.Platform != "android" && principal.Platform != "ios" {
			httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "Use a browser or the NeoCloud phone app for this.", "code": "PHONE_REQUIRED"})
			return
		}
		var serve http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			input, err := readBody(w, r)
			if err != nil {
				writeError(w, err)
				return
			}
			result, err := run(r, principal.UserID, input)
			if err != nil {
				writeError(w, err)
				return
			}
			httpx.WriteJSON(w, status, result)
		})
		for _, limit := range limits {
			serve = limit(serve)
		}
		serve.ServeHTTP(w, r)
	})
}

func (h *Handler) Routes(mux *http.ServeMux) {
	get := func(pattern string, run action) { mux.Handle("GET "+pattern, h.route(http.StatusOK, run)) }
	post := func(pattern string, run action) { mux.Handle("POST "+pattern, h.route(http.StatusOK, run)) }
	// Starting model work costs tokens; deciding on suggestions doesn't.
	work := func(pattern string, status int, run action) {
		mux.Handle("POST "+pattern, h.route(status, run, h.limiter.Middleware))
	}

	get("/api/ai/status", h.status)
	mux.Handle("PUT /api/ai/folders/{id}/excluded", h.route(http.StatusOK, h.setFolderExcluded))

	get("/api/ai/suggestions", h.listSuggestions)
	get("/api/ai/suggestions/counts", h.countSuggestions)
	post("/api/ai/suggestions/accept-high", h.acceptAllHigh)
	post("/api/ai/suggestions/bulk", h.bulkDecide)
	post("/api/ai/suggestions/{id}/accept", h.accept)
	post("/api/ai/suggestions/{id}/reject", h.reject)
	post("/api/ai/suggestions/{id}/undo", h.undo)

	get("/api/ai/cleanup/folders", h.listEmptyFolders)
	post("/api/ai/cleanup/folders", h.removeEmptyFolders)

	get("/api/ai/jobs", h.jobStatus)
	work("/api/ai/jobs", http.StatusAccepted, h.requestJobs)

	get("/api/ai/rules", h.listRules)
	mux.Handle("POST /api/ai/rules", h.route(http.StatusCreated, h.createRule))
	get("/api/ai/rules/proposals", h.listProposals)
	mux.Handle("POST /api/ai/rules/proposals/{id}", h.route(http.StatusCreated, h.acceptProposal))
	mux.Handle("DELETE /api/ai/rules/proposals/{id}", h.route(http.StatusOK, h.dismissProposal))
	mux.Handle("PATCH /api/ai/rules/{id}", h.route(http.StatusOK, h.updateRule))
	mux.Handle("DELETE /api/ai/rules/{id}", h.route(http.StatusOK, h.deleteRule))

	// Duplicates need no AI: they work with AI off too.
	get("/api/ai/duplicates", h.listDuplicates)
	post("/api/ai/duplicates/dismiss", h.dismissDuplicates)

	work("/api/ai/files/{id}/summary", http.StatusOK, h.summarize)
}
