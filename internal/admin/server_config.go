package admin

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

func (h *Handler) ServerConfigRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/config/email", h.getEmailConfig)
	mux.Handle("PUT /api/admin/config/email", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateEmailConfig)))
	mux.HandleFunc("GET /api/admin/config/storage", h.getStorageSettings)
	mux.Handle("PUT /api/admin/config/storage", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateStorageSettings)))
	mux.HandleFunc("GET /api/admin/access", h.getAccessSettings)
	mux.Handle("PUT /api/admin/access/signup", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateSignupAccess)))
	mux.HandleFunc("GET /api/admin/config", h.getServerSettings)
	mux.Handle("PUT /api/admin/config", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateServerSettings)))
}

type storageSettings = StoragePolicy

func (h *Handler) getStorageSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	h.configMu.RLock()
	current := storageSettings{h.defaultQuotaBytes, h.maxUploadBytes, h.trashRetentionDays, h.versionRetention}
	h.configMu.RUnlock()
	httpx.WriteJSON(w, http.StatusOK, current)
}

func (h *Handler) updateStorageSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	updates := map[string]string{}
	var quota *int64
	var maxUpload *int64
	var trashDays, versions *int
	if raw, exists := body["defaultQuotaBytes"]; exists {
		if text, ok := raw.(string); ok && strings.EqualFold(strings.TrimSpace(text), "unlimited") || raw == json.Number("-1") {
			n := int64(-1)
			quota = &n
			updates["NEOCLOUD_DEFAULT_QUOTA"] = "unlimited"
		} else if n, err := parseBytes(raw); err == nil {
			quota = &n
			updates["NEOCLOUD_DEFAULT_QUOTA"] = strconv.FormatInt(n, 10)
		} else {
			writeError(w, 400, "Default quota must be a size.", "INVALID_VALUE")
			return
		}
	}
	if raw, exists := body["maxUploadBytes"]; exists {
		n, err := parseBytes(raw)
		if err != nil {
			writeError(w, 400, "Max upload size must be a size (0 for none).", "INVALID_VALUE")
			return
		}
		maxUpload = &n
		updates["NEOCLOUD_MAX_UPLOAD_SIZE"] = strconv.FormatInt(n, 10)
	}
	if raw, exists := body["trashRetentionDays"]; exists {
		n, ok := wholeNonNegative(raw)
		if !ok {
			writeError(w, 400, "trashRetentionDays must be a whole number ≥ 0.", "INVALID_VALUE")
			return
		}
		trashDays = &n
		updates["NEOCLOUD_TRASH_RETENTION_DAYS"] = strconv.Itoa(n)
	}
	if raw, exists := body["versionRetention"]; exists {
		n, ok := wholeNonNegative(raw)
		if !ok {
			writeError(w, 400, "versionRetention must be a whole number ≥ 0.", "INVALID_VALUE")
			return
		}
		versions = &n
		updates["NEOCLOUD_VERSION_RETENTION"] = strconv.Itoa(n)
	}
	h.configMu.Lock()
	current := storageSettings{h.defaultQuotaBytes, h.maxUploadBytes, h.trashRetentionDays, h.versionRetention}
	if quota != nil {
		current.DefaultQuotaBytes = *quota
	}
	if maxUpload != nil {
		current.MaxUploadBytes = *maxUpload
	}
	if trashDays != nil {
		current.TrashRetentionDays = *trashDays
	}
	if versions != nil {
		current.VersionRetention = *versions
	}
	if err := h.persistSettings(updates); err != nil {
		h.configMu.Unlock()
		writeError(w, 500, "Could not update storage settings.", "INTERNAL_ERROR")
		return
	}
	h.defaultQuotaBytes = current.DefaultQuotaBytes
	h.maxUploadBytes = current.MaxUploadBytes
	h.trashRetentionDays = current.TrashRetentionDays
	h.versionRetention = current.VersionRetention
	h.configMu.Unlock()
	if h.onStorageChanged != nil {
		h.onStorageChanged(StoragePolicy(current))
	}
	httpx.WriteJSON(w, 200, current)
}

func (h *Handler) getAccessSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	h.configMu.RLock()
	allowSignup := h.allowSignup
	h.configMu.RUnlock()
	httpx.WriteJSON(w, 200, map[string]bool{"allowSignup": allowSignup})
}
func (h *Handler) updateSignupAccess(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	value, exists := body["allowSignup"].(bool)
	if !exists {
		writeError(w, 400, "allowSignup must be true or false.", "INVALID_VALUE")
		return
	}
	raw := "false"
	if value {
		raw = "true"
	}
	h.configMu.Lock()
	if err := h.persistSettings(map[string]string{"NEOCLOUD_ALLOW_SIGNUP": raw}); err != nil {
		h.configMu.Unlock()
		writeError(w, 500, "Could not update access settings.", "INTERNAL_ERROR")
		return
	}
	h.allowSignup = value
	h.configMu.Unlock()
	if h.onSignupChanged != nil {
		h.onSignupChanged(value)
	}
	httpx.WriteJSON(w, 200, map[string]bool{"allowSignup": value})
}

func (h *Handler) getServerSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	h.configMu.RLock()
	fullTextSearchEnabled := h.fullTextSearchEnabled
	h.configMu.RUnlock()
	httpx.WriteJSON(w, 200, map[string]bool{"fullTextSearch": fullTextSearchEnabled})
}
func (h *Handler) updateServerSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	h.configMu.RLock()
	value := h.fullTextSearchEnabled
	h.configMu.RUnlock()
	if raw, exists := body["fullTextSearch"]; exists {
		parsed, valid := raw.(bool)
		if !valid {
			writeError(w, 400, "fullTextSearch must be true or false.", "INVALID_VALUE")
			return
		}
		value = parsed
	}
	if _, exists := body["fullTextSearch"]; exists {
		raw := "false"
		if value {
			raw = "true"
		}
		h.configMu.Lock()
		if err := h.persistSettings(map[string]string{"NEOCLOUD_FULL_TEXT_SEARCH": raw}); err != nil {
			h.configMu.Unlock()
			writeError(w, 500, "Could not update server settings.", "INTERNAL_ERROR")
			return
		}
		h.fullTextSearchEnabled = value
		h.configMu.Unlock()
		if h.onFullTextSearchChanged != nil {
			h.onFullTextSearchChanged(value)
		}
	}
	httpx.WriteJSON(w, 200, map[string]bool{"fullTextSearch": value})
}

func (h *Handler) persistSettings(updates map[string]string) error {
	if len(updates) == 0 {
		return nil
	}
	if err := config.UpdateEnv(h.envFile, updates); err != nil {
		return err
	}
	for key, value := range updates {
		_ = os.Setenv(key, value)
	}
	return nil
}
func decodeObject(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var body map[string]any
	if err := decoder.Decode(&body); err != nil || body == nil {
		writeError(w, 400, "Invalid request body.", "INVALID_VALUE")
		return nil, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, 400, "Invalid request body.", "INVALID_VALUE")
		return nil, false
	}
	return body, true
}
func wholeNonNegative(v any) (int, bool) {
	number, ok := asFloat(v)
	if !ok || number < 0 || number > float64(int(^uint(0)>>1)) || math.Trunc(number) != number {
		return 0, false
	}
	return int(number), true
}
func parseBytes(v any) (int64, error) {
	if n, ok := asFloat(v); ok {
		if n < 0 || n > 9007199254740991 || math.Trunc(n) != n {
			return 0, errors.New("invalid byte count")
		}
		return int64(n), nil
	}
	text, ok := v.(string)
	if !ok {
		return 0, errors.New("invalid byte size")
	}
	raw := strings.ToUpper(strings.TrimSpace(text))
	if raw == "" {
		return 0, errors.New("invalid byte size")
	}
	units := []struct {
		suffix     string
		multiplier float64
	}{{"TIB", 1 << 40}, {"TB", 1 << 40}, {"T", 1 << 40}, {"GIB", 1 << 30}, {"GB", 1 << 30}, {"G", 1 << 30}, {"MIB", 1 << 20}, {"MB", 1 << 20}, {"M", 1 << 20}, {"KIB", 1 << 10}, {"KB", 1 << 10}, {"K", 1 << 10}, {"B", 1}}
	multiplier := float64(1)
	number := raw
	for _, unit := range units {
		if strings.HasSuffix(raw, unit.suffix) {
			number = strings.TrimSpace(strings.TrimSuffix(raw, unit.suffix))
			multiplier = unit.multiplier
			break
		}
	}
	n, err := strconv.ParseFloat(number, 64)
	if err != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) || n*multiplier > 9007199254740991 {
		return 0, errors.New("invalid byte size")
	}
	return int64(math.Floor(n * multiplier)), nil
}
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, e := n.Float64()
		return f, e == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
