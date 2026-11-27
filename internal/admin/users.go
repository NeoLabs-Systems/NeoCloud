// Package admin implements a bounded, audited server-administration API.
package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/settings"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
)

type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

type ResolvePrincipal func(*http.Request) (Principal, bool)

var ErrBackupRunning = errors.New("a backup to this target is already running")
var ErrBackupWorkerUnavailable = errors.New("backup worker is not running")

// StoragePolicy is the subset of server configuration enforced by storage
// request handlers. The callback runs only after the env-file update succeeds.
type StoragePolicy struct {
	DefaultQuotaBytes  int64 `json:"defaultQuotaBytes"`
	MaxUploadBytes     int64 `json:"maxUploadBytes"`
	TrashRetentionDays int   `json:"trashRetentionDays"`
	VersionRetention   int   `json:"versionRetention"`
}

type Options struct {
	DB                            *sql.DB
	Keys                          *cryptox.Keyring
	ResolvePrincipal              ResolvePrincipal
	DefaultQuotaBytes             int64
	SessionSecret                 string
	BlobDir                       string
	PreviewDir                    string
	UploadTmpDir                  string
	EnvFile                       string
	AllowSignup                   bool
	MaxUploadBytes                int64
	TrashRetentionDays            int
	VersionRetention              int
	FullTextSearchEnabled         bool
	OnSignupChanged               func(bool)
	OnStorageChanged              func(StoragePolicy)
	OnFullTextSearchChanged       func(bool)
	OnEmailChanged                func()
	OnSendTestEmail               func(ctx context.Context, recipient, language string) (string, error)
	OnBackupTargetRun             func(targetID int64, targetType string, config map[string]any, retention int) (int64, error)
	OnBackupTargetTest            func(ctx context.Context, targetType string, config map[string]any) (map[string]any, error)
	OnBackupTargetSnapshots       func(ctx context.Context, targetType string, config map[string]any) ([]string, error)
	OnBackupTargetVerify          func(ctx context.Context, targetType string, config map[string]any) any
	OnStorageClassMigrationStart  func(targetID int64, config map[string]any) error
	OnStorageClassMigrationStatus func(targetID int64) any
	// OnBackupVerified keeps a verification; OnBackupVerification reads it back.
	OnBackupVerified              func(targetID int64, result any) error
	OnBackupVerification          func(targetID int64) (any, error)
	OnHealthMetadata              func() map[string]any
	OnSearchIndexStatus           func(ctx context.Context) (map[string]any, error)
	OnSearchIndexRebuild          func(ctx context.Context) (map[string]any, error)
	DisabledModelIDs              []string
	KnownModelIDs                 []string
	KnownModelsConfigured         bool
	AutoEnableNewModels           bool
	AutoEnableNewModelsConfigured bool
	AdminModelCatalog             func(ctx context.Context) ([]settings.AdminModel, error)
	OnDisabledModelsChanged       func([]string)
	AIEnabled                     bool
	AIMonthlyTokens               int64
	OnAIConfigChanged             func(enabled bool, monthlyTokens int64)
	ProviderKeys                  map[string]string
	ProviderBaseURLs              map[string]string
	OnProviderChanged             func(envKey, value string)
	MasterKeyHex                  string
	Reauthenticate                func(ctx context.Context, userID int64, currentPassword, code string) error
	OnDeviceCredentialsRevoked    func(ctx context.Context, userID, keepDeviceID int64) error
	Notify                        func(userID int64, event string, data map[string]any)
}

type Handler struct {
	configMu                      sync.RWMutex
	stopAccount                   func(context.Context, int64) error
	notify                        func(userID int64, event string, data map[string]any)
	db                            *sql.DB
	keys                          *cryptox.Keyring
	resolve                       ResolvePrincipal
	defaultQuotaBytes             int64
	sessionSecret                 string
	blobDir                       string
	previewDir                    string
	uploadTmpDir                  string
	envFile                       string
	allowSignup                   bool
	maxUploadBytes                int64
	trashRetentionDays            int
	versionRetention              int
	fullTextSearchEnabled         bool
	onSignupChanged               func(bool)
	onStorageChanged              func(StoragePolicy)
	onFullTextSearchChanged       func(bool)
	onEmailChanged                func()
	onSendTestEmail               func(ctx context.Context, recipient, language string) (string, error)
	onBackupTargetRun             func(targetID int64, targetType string, config map[string]any, retention int) (int64, error)
	onBackupTargetTest            func(ctx context.Context, targetType string, config map[string]any) (map[string]any, error)
	onBackupTargetSnapshots       func(ctx context.Context, targetType string, config map[string]any) ([]string, error)
	onBackupTargetVerify          func(ctx context.Context, targetType string, config map[string]any) any
	onStorageClassMigrationStart  func(targetID int64, config map[string]any) error
	onStorageClassMigrationStatus func(targetID int64) any
	onBackupVerified              func(int64, any) error
	onBackupVerification          func(int64) (any, error)
	onHealthMetadata              func() map[string]any
	onSearchIndexStatus           func(ctx context.Context) (map[string]any, error)
	onSearchIndexRebuild          func(ctx context.Context) (map[string]any, error)
	settingsLimiter               *ratelimit.Limiter
	modelVisibilityMu             sync.Mutex
	disabledModelIDs              []string
	knownModelIDs                 []string
	knownModelsConfigured         bool
	autoEnableNewModels           bool
	adminModelCatalog             func(context.Context) ([]settings.AdminModel, error)
	onDisabledModelsChanged       func([]string)
	aiConfigMu                    sync.RWMutex
	aiEnabled                     bool
	aiMonthlyTokens               int64
	onAIConfigChanged             func(bool, int64)
	providerSettingsMu            sync.RWMutex
	providerValues                map[string]string
	onProviderChanged             func(string, string)
	masterKeyHex                  string
	reauthenticate                func(context.Context, int64, string, string) error
	keyExportLimiter              *ratelimit.Limiter
	onDeviceCredentialsRevoked    func(context.Context, int64, int64) error
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("admin: database and principal resolver are required")
	}
	h := &Handler{db: options.DB, keys: options.Keys, resolve: options.ResolvePrincipal, defaultQuotaBytes: options.DefaultQuotaBytes, sessionSecret: options.SessionSecret,
		blobDir: options.BlobDir, previewDir: options.PreviewDir, uploadTmpDir: options.UploadTmpDir,
		envFile: options.EnvFile, allowSignup: options.AllowSignup, maxUploadBytes: options.MaxUploadBytes,
		trashRetentionDays: options.TrashRetentionDays, versionRetention: options.VersionRetention,
		fullTextSearchEnabled: options.FullTextSearchEnabled,
		disabledModelIDs:      uniqueModelIDs(options.DisabledModelIDs), knownModelIDs: uniqueModelIDs(options.KnownModelIDs),
		knownModelsConfigured: options.KnownModelsConfigured, autoEnableNewModels: !options.AutoEnableNewModelsConfigured || options.AutoEnableNewModels,
		adminModelCatalog: options.AdminModelCatalog, onDisabledModelsChanged: options.OnDisabledModelsChanged,
		aiEnabled: options.AIEnabled, aiMonthlyTokens: options.AIMonthlyTokens, onAIConfigChanged: options.OnAIConfigChanged,
		providerValues: initialProviderValues(options.ProviderKeys, options.ProviderBaseURLs), onProviderChanged: options.OnProviderChanged}
	h.masterKeyHex = options.MasterKeyHex
	h.reauthenticate = options.Reauthenticate
	h.onDeviceCredentialsRevoked = options.OnDeviceCredentialsRevoked
	h.keyExportLimiter = ratelimit.New(ratelimit.Config{Window: 15 * time.Minute, Max: 5, Message: map[string]string{"error": "Too many attempts, try again later", "code": "RATE_LIMITED"}})
	h.onSignupChanged = options.OnSignupChanged
	h.onStorageChanged = options.OnStorageChanged
	h.onFullTextSearchChanged = options.OnFullTextSearchChanged
	h.onEmailChanged = options.OnEmailChanged
	h.onSendTestEmail = options.OnSendTestEmail
	h.onBackupTargetRun = options.OnBackupTargetRun
	h.onBackupTargetTest = options.OnBackupTargetTest
	h.onBackupTargetSnapshots = options.OnBackupTargetSnapshots
	h.onBackupTargetVerify = options.OnBackupTargetVerify
	h.onStorageClassMigrationStart = options.OnStorageClassMigrationStart
	h.onStorageClassMigrationStatus = options.OnStorageClassMigrationStatus
	h.onBackupVerified, h.onBackupVerification = options.OnBackupVerified, options.OnBackupVerification
	h.onHealthMetadata = options.OnHealthMetadata
	h.onSearchIndexStatus = options.OnSearchIndexStatus
	h.onSearchIndexRebuild = options.OnSearchIndexRebuild
	h.notify = options.Notify
	h.settingsLimiter = ratelimit.New(ratelimit.Config{
		Window: 15 * time.Minute,
		Max:    60,
		Message: map[string]string{
			"error": "Too many changes, try again shortly",
			"code":  "RATE_LIMITED",
		},
	})
	return h, nil
}

// SetAccountStopper registers what must stop before an account is deleted.
func (h *Handler) SetAccountStopper(stop func(ctx context.Context, userID int64) error) {
	h.stopAccount = stop
}

func (h *Handler) SetDeviceCredentialsRevoked(revoke func(context.Context, int64, int64) error) {
	h.onDeviceCredentialsRevoked = revoke
}

func (h *Handler) cancelRevokedDeviceHandoffs(ctx context.Context, userID, keepDeviceID int64) {
	if h.onDeviceCredentialsRevoked == nil {
		return
	}
	if err := h.onDeviceCredentialsRevoked(ctx, userID, keepDeviceID); err != nil {
		slog.Warn("could not cancel handoffs after administrator device revocation", "user_id", userID, "keep_device_id", keepDeviceID, "error", err)
	}
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/version", h.adminVersion)
	mux.HandleFunc("GET /api/admin/health", h.adminHealth)
	mux.HandleFunc("GET /api/admin/users", h.listUsers)
	mux.Handle("PUT /api/admin/users/{id}/quota", h.settingsLimiter.Middleware(http.HandlerFunc(h.setUserQuota)))
	mux.Handle("POST /api/admin/users/{id}/usage/recalculate", h.settingsLimiter.Middleware(http.HandlerFunc(h.recalculateUserUsage)))
	mux.Handle("POST /api/admin/storage/recalculate", h.settingsLimiter.Middleware(http.HandlerFunc(h.recalculateAllUsage)))
	h.ServerConfigRoutes(mux)
	h.EmailTestRoutes(mux)
	h.BackupRoutes(mux)
	mux.HandleFunc("GET /api/admin/search-index", h.searchIndexStatus)
	mux.Handle("POST /api/admin/search-index/rebuild", h.settingsLimiter.Middleware(http.HandlerFunc(h.rebuildSearchIndex)))
	mux.HandleFunc("GET /api/admin/models", h.getAdminModels)
	mux.Handle("PUT /api/admin/models/config", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateAdminModels)))
	mux.HandleFunc("GET /api/admin/ai", h.getAdminAIConfig)
	mux.Handle("PUT /api/admin/ai", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateAdminAIConfig)))
	mux.HandleFunc("GET /api/admin/ai/usage", h.getAdminAIUsage)
	mux.HandleFunc("GET /api/admin/providers", h.getAdminProviders)
	mux.Handle("PUT /api/admin/providers", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateAdminProvider)))
	mux.Handle("POST /api/admin/keys/master", h.keyExportLimiter.Middleware(http.HandlerFunc(h.exportMasterKey)))
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 || (p.Device && p.Platform != "android" && p.Platform != "ios") {
		writeError(w, http.StatusForbidden, "Admin access required", "ADMIN_REQUIRED")
		return 0, false
	}
	var admin int
	err := h.db.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id=? AND disabled_at IS NULL`, p.UserID).Scan(&admin)
	if errors.Is(err, sql.ErrNoRows) || err == nil && admin != 1 {
		writeError(w, http.StatusForbidden, "Admin access required", "ADMIN_REQUIRED")
		return 0, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not check admin access", "INTERNAL_ERROR")
		return 0, false
	}
	return p.UserID, true
}

type userRow struct {
	ID                       int64
	Username                 string
	DisplayName              sql.NullString
	Email                    sql.NullString
	EmailVerifiedAt          sql.NullString
	IsAdmin                  int
	DisabledAt               sql.NullString
	UsedBytes                int64
	QuotaBytes               sql.NullInt64
	FileCount                int64
	AIMonthlyTokens          sql.NullInt64
	CreatedAt                string
	LastLogin                sql.NullString
	EmailConfirmationPending bool
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	var pattern any
	if search != "" {
		pattern = "%" + search + "%"
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT u.id,u.username,u.display_name,u.email,u.email_verified_at,u.is_admin,u.disabled_at,u.used_bytes,u.quota_bytes,
		(SELECT COUNT(*) FROM nodes n WHERE n.user_id=u.id AND n.type='file'),u.ai_monthly_tokens,u.created_at,u.last_login,
		EXISTS(SELECT 1 FROM user_email_tokens t WHERE t.user_id=u.id AND t.type='signup_confirmation' AND t.consumed_at IS NULL)
		FROM users u WHERE ? IS NULL OR u.username LIKE ? OR u.email LIKE ? ORDER BY u.created_at DESC LIMIT 500`, pattern, pattern, pattern)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not list users", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()
	users := make([]map[string]any, 0)
	for rows.Next() {
		var u userRow
		if err = rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.EmailVerifiedAt, &u.IsAdmin, &u.DisabledAt, &u.UsedBytes, &u.QuotaBytes, &u.FileCount, &u.AIMonthlyTokens, &u.CreatedAt, &u.LastLogin, &u.EmailConfirmationPending); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not list users", "INTERNAL_ERROR")
			return
		}
		users = append(users, h.serializeUser(u))
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not list users", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
}

func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func (h *Handler) serializeUser(u userRow) map[string]any {
	h.configMu.RLock()
	quota := h.defaultQuotaBytes
	h.configMu.RUnlock()
	mode := "default"
	if u.QuotaBytes.Valid {
		quota = u.QuotaBytes.Int64
		if quota == -1 {
			mode = "unlimited"
		} else {
			mode = "custom"
		}
	}
	var emailVerified, disabled bool
	var aiMonthlyTokens any
	if u.EmailVerifiedAt.Valid {
		emailVerified = true
	}
	if u.DisabledAt.Valid {
		disabled = true
	}
	if u.AIMonthlyTokens.Valid {
		aiMonthlyTokens = u.AIMonthlyTokens.Int64
	}
	return map[string]any{
		"id": u.ID, "username": u.Username, "displayName": nullableString(u.DisplayName), "email": nullableString(u.Email),
		"emailVerified": emailVerified, "emailConfirmationPending": u.EmailConfirmationPending,
		"isAdmin": u.IsAdmin == 1, "disabled": disabled, "disabledAt": nullableString(u.DisabledAt),
		"usedBytes": u.UsedBytes, "quotaBytes": quota, "quotaMode": mode, "fileCount": u.FileCount,
		"aiMonthlyTokens": aiMonthlyTokens, "createdAt": u.CreatedAt, "lastLogin": nullableString(u.LastLogin),
	}
}

type quotaRequest struct {
	Mode  string          `json:"mode"`
	Bytes json.RawMessage `json:"bytes"`
}

func (h *Handler) setUserQuota(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || userID < 1 {
		writeError(w, http.StatusBadRequest, "Invalid user id", "INVALID_ID")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var body quotaRequest
	if decoder.Decode(&body) != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return
	}
	var quota any
	switch body.Mode {
	case "default":
		quota = nil
	case "unlimited":
		quota = int64(-1)
	case "custom":
		bytes, valid := parseByteSize(body.Bytes)
		if !valid {
			writeError(w, http.StatusBadRequest, "Enter the quota as a size, e.g. 50GB.", "INVALID_QUOTA")
			return
		}
		quota = bytes
	default:
		writeError(w, http.StatusBadRequest, "mode must be default, unlimited or custom.", "INVALID_QUOTA_MODE")
		return
	}
	detail, err := json.Marshal(map[string]any{"mode": body.Mode, "bytes": quota})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update user quota", "INTERNAL_ERROR")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update user quota", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(r.Context(), `SELECT 1 FROM users WHERE id=?`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read user", "INTERNAL_ERROR")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET quota_bytes=? WHERE id=?`, quota, userID); err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('account.quota',?,?,?)`, actorID, userID, string(detail))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update user quota", "INTERNAL_ERROR")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update user quota", "INTERNAL_ERROR")
		return
	}
	var u userRow
	err = h.db.QueryRowContext(r.Context(), `SELECT u.id,u.username,u.display_name,u.email,u.email_verified_at,u.is_admin,u.disabled_at,u.used_bytes,u.quota_bytes,
		(SELECT COUNT(*) FROM nodes n WHERE n.user_id=u.id AND n.type='file'),u.ai_monthly_tokens,u.created_at,u.last_login,
		EXISTS(SELECT 1 FROM user_email_tokens t WHERE t.user_id=u.id AND t.type='signup_confirmation' AND t.consumed_at IS NULL)
		FROM users u WHERE u.id=?`, userID).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.EmailVerifiedAt, &u.IsAdmin, &u.DisabledAt, &u.UsedBytes, &u.QuotaBytes, &u.FileCount, &u.AIMonthlyTokens, &u.CreatedAt, &u.LastLogin, &u.EmailConfirmationPending)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "User not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read updated user", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.serializeUser(u))
}

var byteSizePattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(B|KB|MB|GB|TB|K|M|G|T)?I?B?$`)

func parseByteSize(raw json.RawMessage) (int64, bool) {
	text := strings.TrimSpace(string(raw))
	if len(text) > 0 && text[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return 0, false
		}
		text = strings.TrimSpace(value)
	} else {
		number, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || number < 0 || number > 9007199254740991 || math.Trunc(number) != number {
			return 0, false
		}
		return int64(number), true
	}
	match := byteSizePattern.FindStringSubmatch(strings.ToUpper(text))
	if match == nil {
		return 0, false
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil || math.IsInf(amount, 0) || math.IsNaN(amount) {
		return 0, false
	}
	unit := match[2]
	if len(unit) == 1 && unit != "B" {
		unit += "B"
	}
	multiplier := float64(1)
	switch unit {
	case "", "B":
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, false
	}
	value := math.Floor(amount * multiplier)
	if value < 0 || value > 9007199254740991 {
		return 0, false
	}
	return int64(value), true
}
