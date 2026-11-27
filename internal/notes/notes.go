// Package notes serves Markdown notes: files under /Notes shown as cards, with
// AI help on demand. A browser session or the phone app; desktop tokens reach
// the same files through the file routes instead.
package notes

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/agentapi"
	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}
type ResolvePrincipal func(*http.Request) (Principal, bool)

// AI is the model access note help runs on. Failures are *apperr.Error.
type AI interface {
	// Check refuses unless AI may be used on this file by this account now.
	Check(ctx context.Context, userID int64, nodeID string) error
	Chat(ctx context.Context, userID int64, role string, messages []llm.Message) (string, string, error)
	ChatJSON(ctx context.Context, userID int64, role string, messages []llm.Message) (map[string]any, string, error)
}

type Options struct {
	Files            *agentapi.Handler
	ResolvePrincipal ResolvePrincipal
	AI               AI
}

type Handler struct {
	files     *agentapi.Handler
	resolve   ResolvePrincipal
	ai        AI
	cache     *cardCache
	aiLimiter *ratelimit.Limiter
}

func New(o Options) (*Handler, error) {
	if o.Files == nil || o.ResolvePrincipal == nil || o.AI == nil {
		return nil, errors.New("notes: file service, principal resolver and AI access are required")
	}
	return &Handler{
		files: o.Files, resolve: o.ResolvePrincipal, ai: o.AI, cache: newCardCache(),
		// Model calls cost tokens.
		aiLimiter: ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: 120, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}}),
	}, nil
}

func (h *Handler) Routes(m *http.ServeMux) {
	m.Handle("GET /api/notes", h.endpoint(http.StatusOK, func(w http.ResponseWriter, r *http.Request, userID int64) (any, error) {
		query := r.URL.Query()
		f := filter{query: lower(query.Get("q")), tag: lower(query.Get("tag")), archived: query.Get("archived") == "true"}
		if query.Has("notebook") {
			notebook := query.Get("notebook")
			f.notebook = &notebook
		}
		return h.listNotes(r.Context(), userID, f)
	}))
	m.Handle("POST /api/notes", h.endpoint(http.StatusCreated, withBody(func(r *http.Request, userID int64, body map[string]any) (any, error) {
		return h.createNote(r.Context(), userID, body)
	})))
	m.Handle("POST /api/notes/root", h.endpoint(http.StatusOK, func(w http.ResponseWriter, r *http.Request, userID int64) (any, error) {
		// Where the app creates an encrypted notebook (a vault).
		id, _, err := h.files.EnsureNotesFolder(r.Context(), userID, "/"+notesFolder)
		return map[string]string{"id": id}, err
	}))
	m.Handle("POST /api/notes/notebooks", h.endpoint(http.StatusOK, withBody(func(r *http.Request, userID int64, body map[string]any) (any, error) {
		return h.createNotebook(r.Context(), userID, body)
	})))
	m.Handle("GET /api/notes/{id}", h.endpoint(http.StatusOK, func(w http.ResponseWriter, r *http.Request, userID int64) (any, error) {
		return h.getNote(r.Context(), userID, r.PathValue("id"))
	}))
	m.Handle("PATCH /api/notes/{id}", h.endpoint(http.StatusOK, withBody(func(r *http.Request, userID int64, body map[string]any) (any, error) {
		return h.updateNote(r.Context(), userID, r.PathValue("id"), body)
	})))
	m.Handle("DELETE /api/notes/{id}", h.endpoint(http.StatusOK, func(w http.ResponseWriter, r *http.Request, userID int64) (any, error) {
		e, err := h.find(r.Context(), userID, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		return map[string]bool{"trashed": true}, h.files.TrashNote(r.Context(), userID, e.ID)
	}))
	assist := h.aiLimiter.Middleware(h.endpoint(http.StatusOK, withBody(func(r *http.Request, userID int64, body map[string]any) (any, error) {
		return h.assistNote(r.Context(), userID, r.PathValue("id"), body)
	})))
	m.Handle("POST /api/notes/{id}/ai", h.guard(assist))
}

type endpoint func(http.ResponseWriter, *http.Request, int64) (any, error)

func withBody(run func(*http.Request, int64, map[string]any) (any, error)) endpoint {
	return func(w http.ResponseWriter, r *http.Request, userID int64) (any, error) {
		body, err := apijson.Object(w, r)
		if err != nil {
			return nil, err
		}
		return run(r, userID, body)
	}
}

// guard admits a browser session or the phone app.
func (h *Handler) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.resolve(r)
		if !ok || p.UserID < 1 {
			httpx.ErrorCode(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		if p.Device && p.Platform != "android" && p.Platform != "ios" {
			httpx.ErrorCode(w, http.StatusForbidden, "Use a browser or the NeoCloud phone app for this.", "PHONE_REQUIRED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) endpoint(status int, run endpoint) http.Handler {
	return h.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := h.resolve(r)
		result, err := run(w, r, p.UserID)
		if err != nil {
			writeError(w, err)
			return
		}
		httpx.WriteJSON(w, status, result)
	}))
}

// writeError passes on the failures of the file services.
func writeError(w http.ResponseWriter, err error) {
	var failure interface {
		StatusCode() int
		ErrorCode() string
	}
	if errors.As(err, &failure) {
		err = apperr.New(failure.StatusCode(), err.Error(), failure.ErrorCode())
	}
	apijson.Fail(w, err, "The note operation could not be completed.")
}
