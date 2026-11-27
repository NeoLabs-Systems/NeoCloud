package runtimeapp

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/proxytrust"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

// Runtime owns the process-wide database, encryption keys and resolved paths.
// It is the Go server's startup boundary; handlers receive these dependencies
// explicitly rather than opening their own database handles.
type Runtime struct {
	Config   config.Config
	DB       *dbx.DB
	Keys     *cryptox.Keyring
	sessions *sessionx.Manager
}

// Open initializes private directories and persistent secrets, opens the one
// NeoCloud SQLite database, migrates it safely and verifies the master key
// against any existing account data keys.
func Open(ctx context.Context, getenv func(string) string, userHome string) (*Runtime, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg, err := config.Load(getenv, userHome)
	if err != nil {
		return nil, err
	}
	secrets, err := config.EnsureSecrets(cfg.Paths, getenv, config.HasEncryptedData(cfg.Paths.DatabaseFile, cfg.Paths.BlobDir))
	if err != nil {
		return nil, err
	}
	cfg, err = config.Load(getenv, userHome)
	if err != nil {
		return nil, err
	}
	db, err := dbx.Open(ctx, cfg.Paths.DatabaseFile, dbx.Options{Synchronous: "FULL"})
	if err != nil {
		return nil, err
	}
	closeDB := func(cause error) (*Runtime, error) {
		_ = db.Close()
		return nil, cause
	}
	if err := schema.Migrate(ctx, db, cfg.Paths.BackupDir); err != nil {
		return closeDB(fmt.Errorf("runtimeapp: migrate database: %w", err))
	}
	masterKey := cfg.MasterKeyHex
	if masterKey == "" {
		masterKey = secrets.MasterKeyHex
	}
	keys, err := cryptox.NewKeyring(masterKey)
	if err != nil {
		return closeDB(err)
	}
	if err := keys.VerifyMasterKey(ctx, db.W); err != nil {
		return closeDB(err)
	}
	if err := applyEnvironmentAdminGrants(ctx, db.W, cfg.ReservedAdminNames); err != nil {
		return closeDB(fmt.Errorf("runtimeapp: apply environment admin grants: %w", err))
	}
	store, err := sessionx.NewStore(ctx, db.W)
	if err != nil {
		return closeDB(err)
	}
	sessions, err := sessionx.NewManager(store, sessionx.Options{
		Name:       "neocloud.sid",
		Secrets:    []string{cfg.SessionSecret},
		Secure:     cfg.SecureCookies,
		TrustProxy: true, // SessionMiddleware strips this header from untrusted peers.
		SameSite:   http.SameSiteLaxMode,
		Rolling:    true,
		Path:       "/",
	})
	if err != nil {
		return closeDB(fmt.Errorf("runtimeapp: configure sessions: %w", err))
	}
	return &Runtime{Config: cfg, DB: db, Keys: keys, sessions: sessions}, nil
}

// SessionMiddleware applies NeoCloud's configured proxy trust policy before
// the shared session middleware evaluates X-Forwarded-Proto.
func (r *Runtime) SessionMiddleware(next http.Handler) http.Handler {
	return proxytrust.Middleware(r.Config.TrustProxy, r.sessions.Middleware(next))
}

// Close releases both the writer and read-only SQLite pools.
func (r *Runtime) Close() error {
	if r == nil || r.DB == nil {
		return nil
	}
	return r.DB.Close()
}
