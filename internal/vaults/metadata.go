// Package vaults serves the API for end-to-end encrypted folders. The server
// keeps the vault key only wrapped under the user's passphrase and never sees
// names or contents in the clear.
package vaults

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/jsvalue"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const failureMessage = "Something went wrong. Please try again."

const (
	saltBytes       = 16
	wrappedKeyBytes = 60 // 12-byte nonce + encrypted 32-byte key + 16-byte GCM tag.
	minIterations   = 600000
	maxIterations   = 10000000
)

type Principal struct{ UserID int64 }
type ResolvePrincipal func(*http.Request) (Principal, bool)
type Options struct {
	DB                *sql.DB
	ResolvePrincipal  ResolvePrincipal
	DefaultQuotaBytes int64
	MaxNodes          int
}
type Handler struct {
	policyMu          sync.RWMutex
	db                *sql.DB
	resolve           ResolvePrincipal
	defaultQuotaBytes int64
	maxNodes          int
}

// SetDefaultQuota applies a new server default to future vault conversions.
func (h *Handler) SetDefaultQuota(bytes int64) {
	h.policyMu.Lock()
	h.defaultQuotaBytes = bytes
	h.policyMu.Unlock()
}

func (h *Handler) defaultQuota() int64 {
	h.policyMu.RLock()
	defer h.policyMu.RUnlock()
	return h.defaultQuotaBytes
}

func New(o Options) (*Handler, error) {
	if o.DB == nil || o.ResolvePrincipal == nil {
		return nil, errors.New("vaults: database and principal resolver are required")
	}
	if o.MaxNodes < 1 {
		return nil, errors.New("vaults: max node limit must be positive")
	}
	return &Handler{db: o.DB, resolve: o.ResolvePrincipal, defaultQuotaBytes: o.DefaultQuotaBytes, maxNodes: o.MaxNodes}, nil
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/vaults", h.authenticated(h.create))
	mux.HandleFunc("GET /api/vaults/conversions/{folderId}", h.authenticated(h.conversionCheck))
	mux.HandleFunc("POST /api/vaults/{id}/replace", h.authenticated(h.replace))
	mux.HandleFunc("GET /api/vaults/{id}", h.authenticated(h.get))
	mux.HandleFunc("PUT /api/vaults/{id}/key", h.authenticated(h.updateKey))
}

func (h *Handler) authenticated(run func(http.ResponseWriter, *http.Request, int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.resolve(r)
		if !ok || p.UserID < 1 {
			httpx.ErrorCode(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
			return
		}
		if err := run(w, r, p.UserID); err != nil {
			apijson.Fail(w, err, failureMessage)
		}
	}
}

type record struct {
	ID         string `json:"id"`
	FolderID   string `json:"folderId"`
	Salt       string `json:"salt"`
	Iterations int64  `json:"iterations"`
	WrappedKey string `json:"wrappedKey"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// find loads one of the account's vaults; any other id is simply missing.
func find(ctx context.Context, q queryer, uid int64, id string) (record, error) {
	var v record
	err := q.QueryRowContext(ctx, `SELECT id,folder_id,salt,iterations,wrapped_key,created_at,updated_at FROM vaults WHERE id=? AND user_id=?`, id, uid).Scan(&v.ID, &v.FolderID, &v.Salt, &v.Iterations, &v.WrappedKey, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, apperr.New(http.StatusNotFound, "Vault not found", "NOT_FOUND")
	}
	return v, err
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, uid int64) error {
	v, err := find(r.Context(), h.db, uid, r.PathValue("id"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}

type keyParams struct {
	salt, wrappedKey string
	iterations       int64
}

var base64Pattern = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

func invalidParams(message string) error {
	return apperr.New(http.StatusBadRequest, message, "VAULT_PARAMS_INVALID")
}

func base64Field(body map[string]any, field string, size int) (string, error) {
	text := jsvalue.String(body[field])
	if !base64Pattern.MatchString(text) || len(text)%4 != 0 {
		return "", invalidParams(field + " must be base64.")
	}
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", invalidParams(field + " must be base64.")
	}
	if len(decoded) != size {
		return "", invalidParams(fmt.Sprintf("%s must be %d bytes.", field, size))
	}
	return text, nil
}

// parseKey checks the shape of the client's key material.
func parseKey(body map[string]any) (keyParams, error) {
	var key keyParams
	var err error
	if key.salt, err = base64Field(body, "salt", saltBytes); err != nil {
		return key, err
	}
	if key.wrappedKey, err = base64Field(body, "wrappedKey", wrappedKeyBytes); err != nil {
		return key, err
	}
	rounds, ok := jsvalue.Integer(body["iterations"])
	if !ok || rounds < minIterations || rounds > maxIterations {
		return key, invalidParams(fmt.Sprintf("iterations must be between %d and %d.", minIterations, maxIterations))
	}
	key.iterations = rounds
	return key, nil
}

// updateKey stores a new wrapping of the same vault key.
func (h *Handler) updateKey(w http.ResponseWriter, r *http.Request, uid int64) error {
	body, err := apijson.Object(w, r)
	if err != nil {
		return err
	}
	key, err := parseKey(body)
	if err != nil {
		return err
	}
	v, err := find(r.Context(), h.db, uid, r.PathValue("id"))
	if err != nil {
		return err
	}
	if _, err = h.db.ExecContext(r.Context(), `UPDATE vaults SET salt=?,iterations=?,wrapped_key=?,updated_at=datetime('now') WHERE id=? AND user_id=?`, key.salt, key.iterations, key.wrappedKey, v.ID, uid); err != nil {
		return err
	}
	if v, err = find(r.Context(), h.db, uid, v.ID); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}
