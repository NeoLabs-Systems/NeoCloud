package admin

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type providerEnvSpec struct {
	key        string
	label      string
	typeName   string
	providerID string
	baseURL    bool
}

var serverProviderEnvSpecs = []providerEnvSpec{
	{key: "NEOCLOUD_OPENAI_API_KEY", label: "OpenAI", typeName: "key", providerID: "openai"},
	{key: "NEOCLOUD_OPENAI_BASE_URL", label: "OpenAI base URL override", typeName: "url", providerID: "openai", baseURL: true},
	{key: "NEOCLOUD_OPENAI_COMPATIBLE_API_KEY", label: "Custom OpenAI-compatible token", typeName: "key", providerID: "openai-compatible"},
	{key: "NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL", label: "Custom OpenAI-compatible base URL", typeName: "url", providerID: "openai-compatible", baseURL: true},
	{key: "NEOCLOUD_ANTHROPIC_API_KEY", label: "Anthropic (Claude)", typeName: "key", providerID: "anthropic"},
	{key: "NEOCLOUD_ANTHROPIC_BASE_URL", label: "Anthropic base URL override", typeName: "url", providerID: "anthropic", baseURL: true},
	{key: "NEOCLOUD_GOOGLE_AI_KEY", label: "Google (Gemini)", typeName: "key", providerID: "google"},
	{key: "NEOCLOUD_OPENROUTER_API_KEY", label: "OpenRouter", typeName: "key", providerID: "openrouter"},
	{key: "NEOCLOUD_OLLAMA_URL", label: "Ollama (Local)", typeName: "url", providerID: "ollama", baseURL: true},
}

func initialProviderValues(keys, baseURLs map[string]string) map[string]string {
	values := make(map[string]string, len(serverProviderEnvSpecs))
	for _, spec := range serverProviderEnvSpecs {
		if spec.baseURL {
			values[spec.key] = strings.TrimSpace(baseURLs[spec.providerID])
		} else {
			values[spec.key] = strings.TrimSpace(keys[spec.providerID])
		}
	}
	return values
}

func (h *Handler) getAdminProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	h.providerSettingsMu.RLock()
	providers := make([]map[string]any, 0, len(serverProviderEnvSpecs))
	for _, spec := range serverProviderEnvSpecs {
		value := h.providerValues[spec.key]
		hint := value
		if spec.typeName == "key" {
			hint = maskProviderSecret(value)
		}
		providers = append(providers, map[string]any{"key": spec.key, "label": spec.label, "type": spec.typeName, "configured": value != "", "hint": hint})
	}
	h.providerSettingsMu.RUnlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (h *Handler) updateAdminProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	key, _ := body["key"].(string)
	var spec *providerEnvSpec
	for i := range serverProviderEnvSpecs {
		if serverProviderEnvSpecs[i].key == key {
			spec = &serverProviderEnvSpecs[i]
			break
		}
	}
	if spec == nil {
		writeError(w, http.StatusBadRequest, "Unknown provider key", "UNKNOWN_PROVIDER")
		return
	}
	value, valid := cleanProviderValue(body["value"])
	if !valid {
		writeError(w, http.StatusBadRequest, "Provider value must be a string or null", "INVALID_VALUE")
		return
	}
	if spec.typeName == "url" && value != "" {
		if message := validateProviderURL(value); message != "" {
			writeError(w, http.StatusBadRequest, message, "INVALID_URL")
			return
		}
	}
	h.providerSettingsMu.Lock()
	if err := h.persistSettings(map[string]string{key: value}); err != nil {
		h.providerSettingsMu.Unlock()
		writeError(w, http.StatusInternalServerError, "Could not save provider settings", "INTERNAL_ERROR")
		return
	}
	h.providerValues[key] = value
	if h.onProviderChanged != nil {
		h.onProviderChanged(key, value)
	}
	h.providerSettingsMu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func cleanProviderValue(raw any) (string, bool) {
	var value string
	switch v := raw.(type) {
	case nil:
		value = ""
	case string:
		value = v
	case json.Number:
		number, ok := new(big.Rat).SetString(v.String())
		if !ok {
			return "", false
		}
		if number.Sign() != 0 {
			value = v.String()
		}
	case bool:
		if v {
			value = fmt.Sprint(v)
		}
	default:
		return "", false
	}
	value = strings.TrimSpace(value)
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", ""), true
}

func validateProviderURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "Provider URL must be a valid HTTP or HTTPS URL"
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "Provider URL must use HTTP or HTTPS"
	}
	if u.User != nil && (u.User.Username() != "" || passwordSet(u.User)) {
		return "Provider URL must not contain embedded credentials"
	}
	return ""
}

func passwordSet(user *url.Userinfo) bool {
	_, set := user.Password()
	return set
}

func maskProviderSecret(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 8 {
		return strings.Repeat("•", len(runes))
	}
	return string(runes[:4]) + "••••" + string(runes[len(runes)-4:])
}
