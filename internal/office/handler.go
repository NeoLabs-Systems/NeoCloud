// Package office implements document editing: the editor's discovery, the
// session that opens a file in it, the WOPI host endpoints it calls back and
// the admin's settings.
package office

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

type Principal struct{ UserID int64 }
type ResolvePrincipal func(*http.Request) (Principal, bool)

// ResolveAdmin reports the account of a request that may change server
// settings: an admin in a browser session or the phone app.
type ResolveAdmin func(*http.Request) (userID int64, admin bool)

type File = files.OfficeFile

// FileStore is the access-checked file side; every call checks again.
type FileStore interface {
	OpenOfficeFile(ctx context.Context, viewerID int64, nodeID string, wantWrite bool) (File, error)
	ServeOfficeFile(w http.ResponseWriter, r *http.Request, viewerID int64, nodeID string)
	SaveOfficeFile(ctx context.Context, actorID int64, nodeID, expectedETag string, body io.Reader, size int64, lock string) (File, error)
}

type Options struct {
	DB               *sql.DB
	Files            FileStore
	ResolvePrincipal ResolvePrincipal
	ResolveAdmin     ResolveAdmin
	EnvFile          string
	SessionSecret    string
	EditorURL        string
	HostURL          string
	Enabled          bool
	Now              func() time.Time
}

type Handler struct {
	db           *sql.DB
	files        FileStore
	resolve      ResolvePrincipal
	resolveAdmin ResolveAdmin
	envFile      string
	tokenKey     []byte
	client       *http.Client
	now          func() time.Time

	mu       sync.RWMutex
	settings editorSettings
	cache    discovery

	adminUpdateMu sync.Mutex
}

func New(o Options) (*Handler, error) {
	if o.DB == nil || o.Files == nil || o.ResolvePrincipal == nil || len(o.SessionSecret) < 32 {
		return nil, errors.New("office: database, file store, principal resolver and session secret are required")
	}
	if o.ResolveAdmin != nil && o.EnvFile == "" {
		return nil, errors.New("office: admin office routes require the env-file path")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	editorURL := strings.TrimRight(strings.TrimSpace(o.EditorURL), "/")
	return &Handler{
		db: o.DB, files: o.Files, resolve: o.ResolvePrincipal, resolveAdmin: o.ResolveAdmin,
		envFile: o.EnvFile, tokenKey: tokenKey(o.SessionSecret), now: o.Now,
		// A redirect is reported to the admin instead of followed.
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		settings: editorSettings{
			Enabled: o.Enabled && editorURL != "", URL: editorURL,
			HostURL: strings.TrimRight(strings.TrimSpace(o.HostURL), "/"),
		},
	}, nil
}

func limiter(window time.Duration, max int, message string, key func(*http.Request) string) *ratelimit.Limiter {
	return ratelimit.New(ratelimit.Config{Window: window, Max: max, Key: key,
		Message: map[string]string{"error": message, "code": "RATE_LIMITED"}})
}

func (h *Handler) Routes(mux *http.ServeMux) {
	sessions := limiter(time.Minute, 60, "Too many requests, try again shortly", func(r *http.Request) string {
		p, _ := h.resolve(r)
		return "user:" + strconv.FormatInt(p.UserID, 10)
	})
	mux.Handle("GET /api/office/config", h.signedIn(http.HandlerFunc(h.officeConfig)))
	mux.Handle("POST /api/files/{id}/edit-session", h.signedIn(sessions.Middleware(http.HandlerFunc(h.createEditSession))))
	h.wopiRoutes(mux)
	if h.resolveAdmin != nil {
		h.adminRoutes(mux)
	}
}

func (h *Handler) signedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := h.resolve(r); !ok || p.UserID < 1 {
			writeError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) officeConfig(w http.ResponseWriter, r *http.Request) {
	extensions := extensionsOf(h.currentActions(r.Context()))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": len(extensions) > 0, "extensions": extensions})
}

func extensionOf(name string) string {
	index := strings.LastIndexByte(name, '.')
	if index < 0 {
		return ""
	}
	return strings.ToLower(name[index+1:])
}

func (h *Handler) createEditSession(w http.ResponseWriter, r *http.Request) {
	p, _ := h.resolve(r)
	file, err := h.files.OpenOfficeFile(r.Context(), p.UserID, r.PathValue("id"), true)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	actions := h.currentActions(r.Context())
	if actions == nil {
		writeError(w, http.StatusServiceUnavailable, "Document editing is not available right now.", "EDITOR_UNAVAILABLE")
		return
	}
	ext := extensionOf(file.Name)
	available, offered := actions[ext]
	if !offered || !slices.Contains(editableExtensions, ext) {
		writeError(w, http.StatusUnsupportedMediaType, "This kind of file can’t be edited here.", "NOT_EDITABLE")
		return
	}
	urlsrc, fallback := available.Edit, available.View
	if !file.CanWrite {
		urlsrc, fallback = fallback, urlsrc
	}
	if urlsrc == "" {
		urlsrc = fallback
	}
	// The admin's address for this server wins: the editor often sits on
	// another network than the person.
	host := h.currentSettings().HostURL
	if host == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https") {
			scheme = "https"
		}
		host = scheme + "://" + r.Host
	}
	token, expires := h.issueToken(p.UserID, file.ID, file.CanWrite)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"editorUrl":   editorPageURL(urlsrc, host+"/wopi/files/"+file.ID),
		"accessToken": token,
		// WOPI semantics: the moment the token ends, in milliseconds since 1970.
		"accessTokenTtl": expires,
		"canWrite":       file.CanWrite,
	})
}

func writeDomainError(w http.ResponseWriter, err error) {
	var failure interface {
		HTTPStatus() int
		ErrorCode() string
	}
	if errors.As(err, &failure) {
		writeError(w, failure.HTTPStatus(), err.Error(), failure.ErrorCode())
		return
	}
	writeError(w, http.StatusInternalServerError, "The file operation could not be completed.", "INTERNAL_ERROR")
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}
