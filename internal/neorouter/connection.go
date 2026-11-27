package neorouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

// A NeoRouter that NeoCloud works through. There are two kinds: the server's,
// set up by an admin and kept in the env file, and a person's own, from their
// sign-in and stored like a BYOK key. A person's own connection wins. Each
// connection lists the kinds of work (roles) that go through it; the rest keep
// their usual providers.

// ProviderID names NeoRouter among the AI providers.
const ProviderID = "neorouter"

const (
	envBaseURL = "NEOROUTER_BASE_URL"
	envAPIKey  = "NEOROUTER_API_KEY"
	envRoles   = "NEOROUTER_ROLES"

	configsSetting = "ai_provider_configs"
	secretsSetting = "ai_provider_api_keys"
	rolesSetting   = "ai_neorouter_roles"
)

// Roles are the kinds of work a NeoRouter can take over.
var Roles = []string{"chat", "embeddings", "transcription", "images"}

// Connection is one usable NeoRouter. The zero value means not connected.
type Connection struct {
	Scope   string
	APIBase string
	APIKey  string
	Roles   []string
	// Trusted marks the server's own NeoRouter, which may be on the LAN.
	Trusted bool
}

// Connected says whether there is an address and a key.
func (c Connection) Connected() bool { return c.APIBase != "" && c.APIKey != "" }

// Handles says whether the given kind of work goes through this connection.
func (c Connection) Handles(role string) bool {
	return c.Connected() && slices.Contains(c.Roles, role)
}

// Origin is the address without the API path.
func (c Connection) Origin() string { return strings.TrimSuffix(c.APIBase, "/v1") }

// APIBase turns "router.local:3333" or "https://router.example.com/app/" into
// the /v1 API base, or "" when it is not an http(s) address.
func APIBase(address string) string {
	raw := strings.TrimSpace(address)
	if raw == "" {
		return ""
	}
	if lower := strings.ToLower(raw); !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		raw = "https://" + raw
	}
	parsed, err := ssrf.CheckURL(raw)
	if err != nil {
		return ""
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: strings.ToLower(parsed.Host)}).String() + "/v1"
}

func normalizeRoles(values []string) []string {
	wanted := make(map[string]bool, len(values))
	for _, value := range values {
		wanted[strings.TrimSpace(value)] = true
	}
	out := []string{}
	for _, role := range Roles {
		if wanted[role] {
			out = append(out, role)
		}
	}
	return out
}

// ServerConnection is the connection set up for everyone.
func (c *RuntimeConnector) ServerConnection() Connection {
	c.envMu.RLock()
	defer c.envMu.RUnlock()
	apiBase, apiKey := APIBase(c.env[envBaseURL]), strings.TrimSpace(c.env[envAPIKey])
	if apiBase == "" || apiKey == "" {
		return Connection{}
	}
	// An unset variable means every role; "none" matches no role.
	roles := slices.Clone(Roles)
	if raw := strings.TrimSpace(c.env[envRoles]); raw != "" {
		roles = normalizeRoles(strings.Split(raw, ","))
	}
	return Connection{Scope: "server", APIBase: apiBase, APIKey: apiKey, Roles: roles, Trusted: true}
}

func (c *RuntimeConnector) userConnection(ctx context.Context, userID int64) (Connection, error) {
	if userID < 1 {
		return Connection{}, nil
	}
	values := map[string]string{}
	for _, key := range []string{configsSetting, secretsSetting, rolesSetting} {
		var value string
		err := c.db.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id=? AND key=?`, userID, key).Scan(&value)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Connection{}, err
		}
		values[key] = value
	}
	var configs map[string]struct {
		BaseURL string `json:"baseUrl"`
	}
	_ = json.Unmarshal([]byte(values[configsSetting]), &configs)
	apiBase := APIBase(configs[ProviderID].BaseURL)
	apiKey := strings.TrimSpace(c.secrets(values[secretsSetting])[ProviderID])
	if apiBase == "" || apiKey == "" {
		return Connection{}, nil
	}
	roles := slices.Clone(Roles)
	var stored []string
	if json.Unmarshal([]byte(values[rolesSetting]), &stored) == nil && stored != nil {
		roles = normalizeRoles(stored)
	}
	server := c.ServerConnection()
	return Connection{Scope: "user", APIBase: apiBase, APIKey: apiKey, Roles: roles, Trusted: server.APIBase == apiBase}, nil
}

// secrets reads the encrypted provider keys; unreadable data holds none.
func (c *RuntimeConnector) secrets(sealed string) map[string]string {
	secrets := map[string]string{}
	if sealed == "" {
		return secrets
	}
	plain, err := c.keys.DecryptValue(sealed)
	if err != nil || json.Unmarshal([]byte(plain), &secrets) != nil {
		return map[string]string{}
	}
	return secrets
}

// Connection is the NeoRouter an account works through: its own, else the
// server's. userID 0 asks for the server's.
func (c *RuntimeConnector) Connection(ctx context.Context, userID int64) (Connection, error) {
	own, err := c.userConnection(ctx, userID)
	if err != nil || own.Connected() {
		return own, err
	}
	return c.ServerConnection(), nil
}
