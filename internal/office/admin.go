package office

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type editorSettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	HostURL string `json:"hostUrl"`
}

func (h *Handler) currentSettings() editorSettings {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.settings
}

func (h *Handler) adminRoutes(mux *http.ServeMux) {
	changes := limiter(15*time.Minute, 60, "Too many changes, try again shortly", nil)
	admin := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := h.resolveAdmin(r); !ok {
				writeError(w, http.StatusForbidden, "Admin access required", "ADMIN_REQUIRED")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	mux.Handle("GET /api/admin/config/office", admin(http.HandlerFunc(h.getAdminConfig)))
	mux.Handle("PUT /api/admin/config/office", admin(changes.Middleware(http.HandlerFunc(h.putAdminConfig))))
	mux.Handle("POST /api/admin/config/office/check", admin(changes.Middleware(http.HandlerFunc(h.checkAdminConfig))))
}

func (h *Handler) getAdminConfig(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.adminView())
}

// cleanAddress is the origin and path of an http(s) address without trailing
// slash.
func cleanAddress(value, label string) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", nil
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" {
		return "", errors.New(label + " must be a web address like https://office.example.com.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New(label + " must be a plain http(s) address without login or parameters.")
	}
	return strings.TrimRight(parsed.Scheme+"://"+strings.ToLower(parsed.Host)+parsed.EscapedPath(), "/"), nil
}

// mergeSettings applies the body to current; nothing is written.
func mergeSettings(current editorSettings, body map[string]json.RawMessage) (editorSettings, error) {
	next := current
	for _, field := range []struct {
		key, label string
		target     *string
	}{{"url", "The editor address", &next.URL}, {"hostUrl", "The address of this server", &next.HostURL}} {
		raw, exists := body[field.key]
		if !exists {
			continue
		}
		var value *string
		if json.Unmarshal(raw, &value) != nil {
			return next, errors.New(field.label + " must be a web address like https://office.example.com.")
		}
		text := ""
		if value != nil {
			text = *value
		}
		cleaned, err := cleanAddress(text, field.label)
		if err != nil {
			return next, err
		}
		*field.target = cleaned
	}
	if raw, exists := body["enabled"]; exists {
		var value *bool
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return next, errors.New("enabled must be true or false.")
		}
		next.Enabled = *value
	}
	return next, nil
}

func (h *Handler) putAdminConfig(w http.ResponseWriter, r *http.Request) {
	actorID, _ := h.resolveAdmin(r)
	body := map[string]json.RawMessage{}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil {
		writeError(w, http.StatusBadRequest, "Request body must be a JSON object.", "INVALID_JSON")
		return
	}
	h.adminUpdateMu.Lock()
	defer h.adminUpdateMu.Unlock()
	next, err := mergeSettings(h.currentSettings(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_VALUE")
		return
	}
	if next.Enabled && next.URL == "" {
		writeError(w, http.StatusBadRequest, "Enter the editor’s address first.", "EDITOR_URL_REQUIRED")
		return
	}
	// Turning the editor on asks it first, so a wrong address is refused here
	// with a reason instead of failing later for everyone.
	if next.Enabled {
		if _, err = h.checkEditor(r.Context(), next.URL); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error(), "EDITOR_UNREACHABLE")
			return
		}
	}
	if err = h.saveSettings(r.Context(), actorID, next); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save the document editor settings.", "CONFIG_WRITE_FAILED")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.adminView())
}

func (h *Handler) saveSettings(ctx context.Context, actorID int64, next editorSettings) error {
	err := config.UpdateEnv(h.envFile, map[string]string{
		"NEOCLOUD_OFFICE_EDITOR_URL":     next.URL,
		"NEOCLOUD_OFFICE_HOST_URL":       next.HostURL,
		"NEOCLOUD_OFFICE_EDITOR_ENABLED": strconv.FormatBool(next.Enabled),
	})
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.settings = next
	h.mu.Unlock()
	_, err = h.db.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,detail_json) VALUES('office.config',?,?)`,
		actorID, `{"enabled":`+strconv.FormatBool(next.Enabled)+`}`)
	return err
}

func (h *Handler) checkAdminConfig(w http.ResponseWriter, r *http.Request) {
	editorURL := h.currentSettings().URL
	if editorURL == "" {
		writeError(w, http.StatusConflict, "Enter the editor’s address first.", "EDITOR_URL_REQUIRED")
		return
	}
	if _, err := h.checkEditor(r.Context(), editorURL); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "EDITOR_UNREACHABLE")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.adminView())
}

// adminView is the settings with whether the editor works and why not.
func (h *Handler) adminView() map[string]any {
	h.mu.RLock()
	settings, known := h.settings, h.cache
	h.mu.RUnlock()
	status := map[string]any{"state": "off"}
	switch {
	case settings.URL == "":
		status["state"] = "unconfigured"
	case known.url != settings.URL:
		if settings.Enabled {
			status["state"] = "unchecked"
		}
	case known.failure != "":
		status = map[string]any{"state": "error", "message": known.failure, "checkedAt": isoTime(known.checked)}
	default:
		if settings.Enabled {
			status["state"] = "ok"
		}
		status["checkedAt"] = isoTime(known.checked)
		status["extensions"] = extensionsOf(known.actions)
	}
	return map[string]any{"enabled": settings.Enabled, "url": settings.URL, "hostUrl": settings.HostURL, "status": status}
}
