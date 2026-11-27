// Package settings implements the account-scoped general preferences API.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
	"github.com/NeoLabs-Systems/neolabs-go/llm/providers"
	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
)

const folderViewsLimit = 500

type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}
type ResolvePrincipal func(*http.Request) (Principal, bool)
type Options struct {
	DB               *sql.DB
	Keys             *cryptox.Keyring
	ProviderBaseURLs map[string]string
	ProviderKeys     map[string]string
	// NeoRouter is the connection an account works through; userID 0 asks for
	// the server's.
	NeoRouter func(context.Context, int64) (neorouter.Connection, error)
	// AIPolicy is the admin's AI switch and default monthly token budget. Model
	// calls are checked against it and the account's opt-in.
	AIPolicy func() (bool, int64)
	// ReferenceURL overrides the public model catalog; "off" skips it.
	ReferenceURL     string
	DisabledModelIDs []string
	ResolvePrincipal ResolvePrincipal
	Now              func() time.Time
}

type Handler struct {
	modelMu          sync.RWMutex
	providerMu       sync.RWMutex
	db               *sql.DB
	keys             *cryptox.Keyring
	providerBaseURLs map[string]string
	providerKeys     map[string]string
	neoRouter        func(context.Context, int64) (neorouter.Connection, error)
	aiPolicy         func() (bool, int64)
	disabledModels   map[string]bool
	resolve          ResolvePrincipal
	now              func() time.Time
	log              *slog.Logger

	reference *llm.Reference
	caches    *providers.Caches
	cacheMu   sync.Mutex
	catalogs  map[string]cachedCatalog
	probes    map[string]ollamaProbe
	refreshes transport.Flight[[]listedModel]
}

// SetServerProviderValue updates a server-side provider credential or base URL
// after the admin route has persisted it to the environment file.
func (h *Handler) SetServerProviderValue(envKey, value string) bool {
	field, ok := serverProviderEnvFields[envKey]
	if !ok {
		return false
	}
	h.providerMu.Lock()
	defer h.providerMu.Unlock()
	if field.baseURL {
		h.providerBaseURLs[field.providerID] = value
	} else {
		h.providerKeys[field.providerID] = value
	}
	return true
}

type serverProviderEnvField struct {
	providerID string
	baseURL    bool
}

var serverProviderEnvFields = map[string]serverProviderEnvField{
	"NEOCLOUD_OPENAI_API_KEY":             {providerID: "openai"},
	"NEOCLOUD_OPENAI_BASE_URL":            {providerID: "openai", baseURL: true},
	"NEOCLOUD_OPENAI_COMPATIBLE_API_KEY":  {providerID: "openai-compatible"},
	"NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL": {providerID: "openai-compatible", baseURL: true},
	"NEOCLOUD_ANTHROPIC_API_KEY":          {providerID: "anthropic"},
	"NEOCLOUD_ANTHROPIC_BASE_URL":         {providerID: "anthropic", baseURL: true},
	"NEOCLOUD_GOOGLE_AI_KEY":              {providerID: "google"},
	"NEOCLOUD_OPENROUTER_API_KEY":         {providerID: "openrouter"},
	"NEOCLOUD_OLLAMA_URL":                 {providerID: "ollama", baseURL: true},
}

func (h *Handler) serverProviderConfig(providerID string) (string, string) {
	h.providerMu.RLock()
	defer h.providerMu.RUnlock()
	return h.providerKeys[providerID], h.providerBaseURLs[providerID]
}

// SetDisabledModelIDs applies a server-admin visibility change immediately to
// model discovery without requiring a runtime restart.
func (h *Handler) SetDisabledModelIDs(ids []string) {
	disabled := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			disabled[id] = true
		}
	}
	h.modelMu.Lock()
	h.disabledModels = disabled
	h.modelMu.Unlock()
}

func (h *Handler) modelDisabled(id string) bool {
	h.modelMu.RLock()
	defer h.modelMu.RUnlock()
	return h.disabledModels[id]
}

type settingSpec struct {
	defaultValue     any
	validate         func(any) bool
	mergeFolderViews bool
}

var (
	featureKey    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,39}$`)
	folderKey     = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,64}$`)
	extension     = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
	controlOrPath = regexp.MustCompile(`[\\/\x00-\x1f]`)
)

var categoryIDs = map[string]bool{
	"images": true, "videos": true, "documents": true, "spreadsheets": true,
	"presentations": true, "archives": true, "audio": true, "code": true, "other": true,
}

var specs = map[string]settingSpec{
	"language":               {nil, func(v any) bool { return v == nil || oneOfString(v, "de", "en") }, false},
	"theme":                  {"system", func(v any) bool { return oneOfString(v, "system", "light", "dark") }, false},
	"fileView":               {"list", func(v any) bool { return oneOfString(v, "list", "grid") }, false},
	"sortField":              {"name", func(v any) bool { return oneOfString(v, "name", "size", "modified", "type") }, false},
	"sortOrder":              {"asc", func(v any) bool { return oneOfString(v, "asc", "desc") }, false},
	"showHiddenFiles":        {false, isBool, false},
	"confirmDelete":          {true, isBool, false},
	"uploadConflict":         {"ask", func(v any) bool { return oneOfString(v, "ask", "replace", "rename") }, false},
	"fullTextSearch":         {true, isBool, false},
	"ai_opt_in":              {false, isBool, false},
	"ai_model_text":          {"auto", isModelChoice, false},
	"ai_model_smart":         {"auto", isModelChoice, false},
	"ai_model_vision":        {"auto", isModelChoice, false},
	"ai_model_embedding":     {"auto", isModelChoice, false},
	"ai_model_transcription": {"auto", isModelChoice, false},
	"ai_features":            {map[string]any{}, isFeatureFlags, false},
	"ai_naming_template":     {"{date} {issuer} – {subject}", isNamingTemplate, false},
	"ai_skip_types":          {map[string]any{"categories": []any{}, "extensions": []any{}}, isSkipTypes, false},
	"folder_views":           {map[string]any{}, isFolderViewsPatch, true},
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("settings: database and principal resolver are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	disabled := make(map[string]bool, len(options.DisabledModelIDs))
	for _, id := range options.DisabledModelIDs {
		if id = strings.TrimSpace(id); id != "" {
			disabled[id] = true
		}
	}
	reference := llm.NewReference()
	reference.URL = strings.TrimSpace(options.ReferenceURL)
	return &Handler{db: options.DB, keys: options.Keys, providerBaseURLs: cloneStringMap(options.ProviderBaseURLs), providerKeys: cloneStringMap(options.ProviderKeys),
		neoRouter: options.NeoRouter, aiPolicy: options.AIPolicy, disabledModels: disabled, resolve: options.ResolvePrincipal, now: options.Now, log: slog.Default(),
		reference: reference, caches: providers.NewCaches(), catalogs: map[string]cachedCatalog{}, probes: map[string]ollamaProbe{}}, nil
}

func cloneStringMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func (h *Handler) Routes(mux *http.ServeMux) {
	for _, path := range []string{"/api/settings", "/api/settings/"} {
		mux.HandleFunc("GET "+path, h.read)
		mux.HandleFunc("PUT "+path, h.update)
	}
	mux.HandleFunc("GET /api/settings/meta/models", h.modelMetadata)
	mux.HandleFunc("GET /api/settings/meta/ai-providers", h.providerMetadata)
	if h.keys != nil {
		h.byokRoutes(mux)
	}
}

func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "code": "UNAUTHORIZED"})
		return 0, false
	}
	if p.Device && p.Platform != "android" && p.Platform != "ios" {
		httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "Use a browser or the NeoCloud phone app for this.", "code": "PHONE_REQUIRED"})
		return 0, false
	}
	return p.UserID, true
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/settings" && r.URL.Path != "/api/settings/" {
		http.NotFound(w, r)
		return
	}
	userID, ok := h.principal(w, r)
	if !ok {
		return
	}
	values, err := h.readSettings(r, userID)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not read settings", "code": "INTERNAL_ERROR"})
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"settings": values})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/settings" && r.URL.Path != "/api/settings/" {
		http.NotFound(w, r)
		return
	}
	userID, ok := h.principal(w, r)
	if !ok {
		return
	}
	var patch map[string]any
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&patch); err != nil || patch == nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "Settings must be an object.", "code": "INVALID_SETTINGS"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		httpx.WriteJSON(w, 400, map[string]string{"error": "Settings must be an object.", "code": "INVALID_SETTINGS"})
		return
	}
	for key, value := range patch {
		spec, exists := specs[key]
		if !exists {
			httpx.WriteJSON(w, 400, map[string]string{"error": "Unknown setting: " + key, "code": "UNKNOWN_SETTING"})
			return
		}
		if !spec.validate(value) {
			httpx.WriteJSON(w, 400, map[string]string{"error": "Invalid value for " + key + ".", "code": "INVALID_SETTING_VALUE"})
			return
		}
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not update settings", "code": "INTERNAL_ERROR"})
		return
	}
	defer tx.Rollback()
	keys := make([]string, 0, len(patch))
	for key := range patch {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := patch[key]
		if specs[key].mergeFolderViews {
			var storedRaw string
			err = tx.QueryRowContext(r.Context(), `SELECT value FROM user_settings WHERE user_id=? AND key=?`, userID, key).Scan(&storedRaw)
			stored := map[string]any{}
			if err == nil {
				_ = json.Unmarshal([]byte(storedRaw), &stored)
			} else if err != sql.ErrNoRows {
				break
			}
			value = h.mergeFolderViews(stored, value.(map[string]any))
		}
		var encoded []byte
		encoded, err = json.Marshal(value)
		if err != nil {
			break
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO user_settings(user_id,key,value,updated_at) VALUES(?,?,?,datetime('now')) ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, userID, key, string(encoded))
		if err != nil {
			break
		}
	}
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not update settings", "code": "INTERNAL_ERROR"})
		return
	}
	if err = tx.Commit(); err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not update settings", "code": "INTERNAL_ERROR"})
		return
	}
	values, err := h.readSettings(r, userID)
	if err != nil {
		httpx.WriteJSON(w, 500, map[string]string{"error": "Could not read settings", "code": "INTERNAL_ERROR"})
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"settings": values})
}

func (h *Handler) readSettings(r *http.Request, userID int64) (map[string]any, error) {
	return ReadPreferences(r.Context(), h.db, userID)
}

// ReadPreferences are the account's preferences as the API shows them. Other
// rows of user_settings (provider keys, replay guards) are not preferences.
func ReadPreferences(ctx context.Context, db *sql.DB, userID int64) (map[string]any, error) {
	values := make(map[string]any, len(specs))
	for key, spec := range specs {
		values[key] = spec.defaultValue
	}
	rows, err := db.QueryContext(ctx, `SELECT key,value FROM user_settings WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err = rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		if _, known := specs[key]; !known {
			continue
		}
		var value any
		if json.Unmarshal([]byte(raw), &value) == nil {
			values[key] = value
		}
	}
	return values, rows.Err()
}

func (h *Handler) mergeFolderViews(stored, patch map[string]any) map[string]any {
	merged := make(map[string]any, len(stored)+len(patch))
	for k, v := range stored {
		merged[k] = v
	}
	now := h.now().UnixMilli()
	for key, value := range patch {
		if value == nil {
			delete(merged, key)
			continue
		}
		view := value.(map[string]any)
		merged[key] = map[string]any{"sort": view["sort"], "dir": view["dir"], "view": view["view"], "at": now}
	}
	if len(merged) <= folderViewsLimit {
		return merged
	}
	type entry struct {
		key   string
		at    int64
		order int
	}
	list := make([]entry, 0, len(merged))
	order := 0
	for key, value := range merged {
		at := int64(0)
		if obj, ok := value.(map[string]any); ok {
			switch v := obj["at"].(type) {
			case float64:
				at = int64(v)
			case int64:
				at = v
			case json.Number:
				at, _ = v.Int64()
			}
		}
		list = append(list, entry{key, at, order})
		order++
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].at == list[j].at {
			return list[i].order > list[j].order
		}
		return list[i].at > list[j].at
	})
	keep := map[string]any{}
	for _, item := range list[:folderViewsLimit] {
		keep[item.key] = merged[item.key]
	}
	return keep
}

func oneOfString(v any, choices ...string) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, choice := range choices {
		if s == choice {
			return true
		}
	}
	return false
}
func isBool(v any) bool { _, ok := v.(bool); return ok }
func isModelChoice(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	if s == "auto" {
		return true
	}
	if len(s) > 300 {
		return false
	}
	sep := strings.Index(s, "::")
	if sep <= 0 {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(s[:sep]))
	model := strings.TrimSpace(s[sep+2:])
	return provider != "" && model != "" && model != "auto"
}
func isFeatureFlags(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) > 32 {
		return false
	}
	for key, flag := range m {
		if !featureKey.MatchString(key) || !isBool(flag) {
			return false
		}
	}
	return true
}
func isNamingTemplate(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= 200 && !controlOrPath.MatchString(s)
}
func isSkipTypes(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for key := range m {
		if key != "categories" && key != "extensions" {
			return false
		}
	}
	for _, item := range []struct {
		key   string
		max   int
		valid func(any) bool
	}{{"categories", len(categoryIDs), func(x any) bool { s, ok := x.(string); return ok && categoryIDs[s] }}, {"extensions", 200, func(x any) bool { s, ok := x.(string); return ok && extension.MatchString(s) }}} {
		raw, exists := m[item.key]
		if !exists {
			continue
		}
		values, ok := raw.([]any)
		if !ok || len(values) > item.max {
			return false
		}
		for _, value := range values {
			if !item.valid(value) {
				return false
			}
		}
	}
	return true
}
func isFolderViewsPatch(v any) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) > folderViewsLimit {
		return false
	}
	for key, value := range m {
		if !folderKey.MatchString(key) {
			return false
		}
		if value == nil {
			continue
		}
		view, ok := value.(map[string]any)
		if !ok || len(view) != 3 || !oneOfString(view["sort"], "name", "size", "modified", "type", "relevance") || !oneOfString(view["dir"], "asc", "desc") || !oneOfString(view["view"], "list", "grid") {
			return false
		}
	}
	return true
}
