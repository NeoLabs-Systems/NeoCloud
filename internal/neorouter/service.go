package neorouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/llm/openaichat"
	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
)

// RuntimeOptions configure the connector. Env holds the NEOROUTER_* values the
// install env file had at start.
type RuntimeOptions struct {
	DB          *sql.DB
	Keys        *cryptox.Keyring
	EnvFile     string
	Env         map[string]string
	ResolveHost func(context.Context, string) ([]netip.Addr, error)
	Hostname    func() (string, error)
	Now         func() time.Time
}

// RuntimeConnector connects, describes and disconnects NeoRouters.
type RuntimeConnector struct {
	db          *sql.DB
	keys        *cryptox.Keyring
	envFile     string
	envMu       sync.RWMutex
	env         map[string]string
	resolveHost func(context.Context, string) ([]netip.Addr, error)
	hostname    func() (string, error)
	now         func() time.Time
	flowMu      sync.Mutex
	flows       map[string]*deviceFlow
}

func NewRuntimeConnector(o RuntimeOptions) (*RuntimeConnector, error) {
	if o.DB == nil || o.Keys == nil {
		return nil, errors.New("neorouter: runtime connector requires database and keyring")
	}
	if o.Hostname == nil {
		o.Hostname = os.Hostname
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	env := map[string]string{}
	for _, key := range []string{envBaseURL, envAPIKey, envRoles} {
		env[key] = o.Env[key]
	}
	return &RuntimeConnector{db: o.DB, keys: o.Keys, envFile: o.EnvFile, env: env, resolveHost: o.ResolveHost,
		hostname: o.Hostname, now: o.Now, flows: map[string]*deviceFlow{}}, nil
}

// Describe is the settings page's view of both connections. Only admins see
// where the server's NeoRouter lives and what it offers.
func (c *RuntimeConnector) Describe(ctx context.Context, userID int64, isAdmin bool) (any, error) {
	own, err := c.userConnection(ctx, userID)
	if err != nil {
		return nil, err
	}
	server := c.ServerConnection()
	var serverState any = map[string]any{"connected": server.Connected()}
	if isAdmin {
		serverState = describe(ctx, server)
	}
	return map[string]any{"roles": Roles, "isAdmin": isAdmin, "server": serverState, "user": describe(ctx, own)}, nil
}

func describe(ctx context.Context, connection Connection) map[string]any {
	if !connection.Connected() {
		return map[string]any{"connected": false, "address": nil, "roles": []string{}, "offer": nil}
	}
	offer := describeOffer(ctx, connection)
	return map[string]any{"connected": true, "address": connection.Origin(), "roles": connection.Roles, "offer": offer, "reachable": offer != nil}
}

// roleKinds name each role's model list the way NeoRouter's ?type= does.
var roleKinds = map[string]string{"chat": "chat", "embeddings": "embedding", "transcription": "transcription", "images": "image"}

// describeOffer counts, per role, the models the router has and how many more
// the key's free/plan/paid choice leaves out. It always asks afresh: what a key
// may use is changed in NeoRouter. nil means the router did not answer.
func describeOffer(ctx context.Context, connection Connection) any {
	provider := openaichat.NewNeoRouter(transport.Config{APIKey: connection.APIKey, BaseURL: connection.APIBase, TrustedBaseURL: connection.Trusted}, nil)
	var mu sync.Mutex
	var workers sync.WaitGroup
	roles, notAllowed := map[string]int{}, map[string]int{}
	for role, kind := range roleKinds {
		workers.Add(1)
		go func() {
			defer workers.Done()
			list, err := provider.ModelList(ctx, kind)
			if err != nil {
				return
			}
			count := 0
			for _, model := range list.Data {
				if id, _ := model["id"].(string); !strings.HasPrefix(id, "auto") {
					count++
				}
			}
			mu.Lock()
			defer mu.Unlock()
			roles[role] = count
			notAllowed[role] = list.NotAllowed["free"] + list.NotAllowed["plan"] + list.NotAllowed["paid"]
		}()
	}
	workers.Wait()
	if len(roles) == 0 {
		return nil
	}
	return map[string]any{"roles": roles, "notAllowed": notAllowed}
}

// SetRoles stores which work goes through a connection and returns the known
// roles of the request.
func (c *RuntimeConnector) SetRoles(ctx context.Context, scope string, userID int64, roles []string) ([]string, error) {
	list := normalizeRoles(roles)
	if scope == "server" {
		// An empty list is stored as "none": an unset variable means every role.
		value := strings.Join(list, ",")
		if value == "" {
			value = "none"
		}
		return list, c.persistServer(map[string]string{envRoles: value})
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	_, err = c.db.ExecContext(ctx, upsertSetting, userID, rolesSetting, string(raw))
	return list, err
}

// Disconnect forgets a connection's address and key.
func (c *RuntimeConnector) Disconnect(ctx context.Context, scope string, userID int64) error {
	if scope == "server" {
		return c.persistServer(map[string]string{envBaseURL: "", envAPIKey: "", envRoles: ""})
	}
	return c.persistUser(ctx, userID, "", "", nil)
}

const upsertSetting = `INSERT INTO user_settings(user_id,key,value,updated_at) VALUES(?,?,?,datetime('now'))
	ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`

func (c *RuntimeConnector) persistServer(updates map[string]string) error {
	if c.envFile == "" {
		return errors.New("neorouter: install env file is not configured")
	}
	c.envMu.Lock()
	defer c.envMu.Unlock()
	if err := config.UpdateEnv(c.envFile, updates); err != nil {
		return err
	}
	for key, value := range updates {
		c.env[key] = value
	}
	return nil
}

// persistUser writes the NeoRouter entries of the person's provider settings,
// leaving their other providers as they are. nil roles keep the stored ones.
func (c *RuntimeConnector) persistUser(ctx context.Context, userID int64, baseURL, token string, roles []string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	read := func(key string) (string, error) {
		var value string
		err := tx.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id=? AND key=?`, userID, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return value, err
	}
	rawConfigs, err := read(configsSetting)
	if err != nil {
		return err
	}
	configs := map[string]map[string]any{}
	_ = json.Unmarshal([]byte(rawConfigs), &configs)
	if configs == nil {
		configs = map[string]map[string]any{}
	}
	if configs[ProviderID] == nil {
		configs[ProviderID] = map[string]any{"label": ""}
	}
	configs[ProviderID]["baseUrl"] = baseURL
	configJSON, err := json.Marshal(configs)
	if err != nil {
		return err
	}
	sealed, err := read(secretsSetting)
	if err != nil {
		return err
	}
	secrets := c.secrets(sealed)
	if token == "" {
		delete(secrets, ProviderID)
	} else {
		secrets[ProviderID] = token
	}
	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	encrypted, err := c.keys.EncryptValue(string(secretsJSON))
	if err != nil {
		return err
	}
	updates := map[string]string{configsSetting: string(configJSON), secretsSetting: encrypted}
	if roles != nil {
		roleJSON, err := json.Marshal(roles)
		if err != nil {
			return err
		}
		updates[rolesSetting] = string(roleJSON)
	}
	for key, value := range updates {
		if _, err := tx.ExecContext(ctx, upsertSetting, userID, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}
