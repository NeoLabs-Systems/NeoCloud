package oauth

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

const (
	companionClientKind = "companion:neoagent"
	companionClientName = "NeoAgent"
	companionCallback   = "/api/integrations/oauth/callback"
)

var companionScopes = []string{"files:read", "files:write", "signing:read", "signing:write"}

type publicOriginResolver func(*http.Request) string

// CompanionRoutes registers the public NeoAgent bootstrap endpoint. The
// caller supplies the configured/request-aware public-origin resolver so a
// reverse proxy's host and scheme policy stays centralized in the app.
func (h *Handler) CompanionRoutes(mux *http.ServeMux, publicOrigin publicOriginResolver) {
	limiter := ratelimit.New(ratelimit.Config{
		Window: 15 * time.Minute,
		Max:    60,
		Message: map[string]string{
			"error": "Too many requests, try again later",
			"code":  "RATE_LIMITED",
		},
	})
	mux.Handle("POST /api/oauth/companion/neoagent/bootstrap", limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.bootstrapCompanion(w, r, publicOrigin)
	})))
}

func (h *Handler) bootstrapCompanion(w http.ResponseWriter, r *http.Request, publicOrigin publicOriginResolver) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	var body map[string]json.RawMessage
	if err := decoder.Decode(&body); err != nil {
		bootstrapError(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		bootstrapError(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return
	}
	if body == nil {
		body = map[string]json.RawMessage{}
	}
	if appName, present := body["appName"]; present && !rawIsNull(appName) && strings.TrimSpace(jsString(appName)) != companionClientName {
		bootstrapError(w, http.StatusBadRequest, "Only NeoAgent can connect this way.", "INVALID_REQUEST")
		return
	}
	redirectURI, err := canonicalCompanionRedirect(jsString(body["redirectUri"]))
	if err != nil {
		bootstrapError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}
	origin := ""
	if publicOrigin != nil {
		origin = strings.TrimRight(publicOrigin(r), "/")
	}
	if origin == "" {
		bootstrapError(w, http.StatusInternalServerError, "Could not determine the public server URL.", "INTERNAL_ERROR")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		bootstrapError(w, http.StatusInternalServerError, "Could not register NeoAgent.", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var clientID string
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM oauth_clients WHERE kind=? AND redirect_uri=? AND revoked_at IS NULL ORDER BY created_at LIMIT 1`, companionClientKind, redirectURI).Scan(&clientID)
	created := false
	if errors.Is(err, sql.ErrNoRows) {
		clientID, err = newCompanionClientID()
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO oauth_clients(id,kind,name,redirect_uri) VALUES(?,?,?,?)`, clientID, companionClientKind, companionClientName, redirectURI)
		}
		created = err == nil
	}
	if err != nil {
		bootstrapError(w, http.StatusInternalServerError, "Could not register NeoAgent.", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		bootstrapError(w, http.StatusInternalServerError, "Could not register NeoAgent.", "INTERNAL_ERROR")
		return
	}
	base := strings.TrimRight(origin, "/")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"companion": "neoagent", "created": created, "clientId": clientID,
		"redirectUri": redirectURI, "scopes": companionScopes,
		"authorizationEndpoint": base + "/oauth/authorize",
		"tokenEndpoint":         base + "/oauth/token",
		"userinfoEndpoint":      base + "/oauth/userinfo",
		"revocationEndpoint":    base + "/oauth/revoke",
	})
}

func bootstrapError(w http.ResponseWriter, status int, message, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}

func rawIsNull(raw json.RawMessage) bool { return strings.TrimSpace(string(raw)) == "null" }

func jsString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || value == nil || value == false {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return "true"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, len(v))
		for i := range v {
			b, _ := json.Marshal(v[i])
			parts[i] = jsString(b)
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

func canonicalCompanionRedirect(raw string) (string, error) {
	invalidURL := errors.New("redirectUri must be a URL.")
	invalidForm := errors.New("redirectUri must be an https NeoAgent callback ending in " + companionCallback + " (http only on a local network).")
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", invalidURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && privateOrLoopbackHost(parsed.Hostname())) {
		return "", invalidForm
	}
	if parsed.User != nil || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(raw, "#") {
		return "", invalidForm
	}
	escapedPath := normalizeCompanionPath(parsed.EscapedPath())
	if !strings.HasSuffix(escapedPath, companionCallback) {
		return "", invalidForm
	}

	// WHATWG URL canonicalization lowercases the host, removes default ports,
	// normalizes dot segments and escapes the path before storing it. Preserve
	// non-default ports and IPv6 brackets.
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port != "" {
		portNumber, portErr := strconv.ParseUint(port, 10, 16)
		if portErr != nil {
			return "", invalidForm
		}
		port = strconv.FormatUint(portNumber, 10)
	}
	if parsed.Scheme == "https" && port == "443" || parsed.Scheme == "http" && port == "80" {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	parsed.Host = host
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", invalidForm
	}
	parsed.Path = decodedPath
	parsed.RawPath = escapedPath
	return parsed.String(), nil
}

func normalizeCompanionPath(raw string) string {
	if raw == "" {
		return "/"
	}
	segments := strings.Split(raw, "/")
	result := []string{""}
	for i := 1; i < len(segments); i++ {
		segment := segments[i]
		lower := strings.ToLower(segment)
		switch lower {
		case ".", "%2e":
			if i == len(segments)-1 {
				result = append(result, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(result) > 1 {
				result = result[:len(result)-1]
			}
			if i == len(segments)-1 {
				result = append(result, "")
			}
		default:
			result = append(result, segment)
		}
	}
	clean := strings.Join(result, "/")
	if clean == "" {
		return "/"
	}
	return clean
}

func privateOrLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && (address.IsPrivate() || address.IsLoopback())
}

func newCompanionClientID() (string, error) {
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "ncoc_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}
