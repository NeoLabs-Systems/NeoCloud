// Package auth contains the NeoCloud browser identity routes. Session storage,
// password hashing, TOTP, and account lockout use the shared NeoLabs contracts;
// app-specific account fields and response shapes remain here.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/lockout"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/qrlogin"
	"github.com/NeoLabs-Systems/neolabs-go/ratelimit"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
)

const (
	loginWindow       = 15 * time.Minute
	twoFactorLifetime = 10 * time.Minute
	maxTwoFactorCodes = 5
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	emailPattern    = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

// Options are process configuration and cryptographic dependencies required by
// the auth routes. DefaultQuotaBytes follows NeoCloud's storage policy.
type Options struct {
	DB                             *sql.DB
	Keys                           *cryptox.Keyring
	BlobDir                        string
	PreviewDir                     string
	UploadTmpDir                   string
	SessionSecret                  string
	DefaultQuotaBytes              int64
	SecureCookies                  bool
	PublicURL                      string
	PublicOrigin                   func(*http.Request) string
	AllowedOrigins                 []string
	TrustProxy                     string
	SignupOpen                     bool
	RequireSignupEmailConfirmation bool
	SignupTokenTTL                 time.Duration
	BrandName                      string
	PendingSignupMessage           func(string) string
	ReservedAdminNames             []string
	SendSignupConfirmation         func(context.Context, string, string, string, string) error
	SendPasswordReset              func(context.Context, string, string, string, string) error
	SendPasswordChangedNotice      func(context.Context, string, string, string) error
	SendEmailConfirmation          func(context.Context, string, string, string, string, bool) error
	SendEmailChangedNotice         func(context.Context, string, string, string, string) error
	SendEmailChangedNewNotice      func(context.Context, string, string, string) error
	SendEmailChangeRequestedNotice func(context.Context, string, string, string, string) error
	EmailStatus                    EmailStatus
	SendNewSignInNotice            func(ctx context.Context, username, email, ip, userAgent, language string) error
	Notify                         func(userID int64, event string, data map[string]any)
	// ReadPreferences are the account's settings as the API shows them, for the
	// account export.
	ReadPreferences func(ctx context.Context, userID int64) (map[string]any, error)
	Now             func() time.Time
}

type EmailStatus struct {
	Enabled                         bool `json:"enabled"`
	Configured                      bool `json:"configured"`
	SignupConfirmationRequired      bool `json:"signupConfirmationRequired"`
	EmailChangeConfirmationRequired bool `json:"emailChangeConfirmationRequired"`
}

type Handler struct {
	db                        *sql.DB
	keys                      *cryptox.Keyring
	blobDir                   string
	previewDir                string
	uploadTmpDir              string
	secret                    string
	dummyHash                 string
	quota                     int64
	quotaMu                   sync.RWMutex
	secure                    bool
	publicURL                 string
	publicOrigin              func(*http.Request) string
	webauthn                  *webauthnx.Service
	signupMu                  sync.RWMutex
	emailMu                   sync.RWMutex
	signup                    bool
	requireSignupConfirmation bool
	signupTokenTTL            time.Duration
	pendingSignupMessage      func(string) string
	brand                     string
	reservedAdminNames        map[string]struct{}
	sendSignupConfirmation    func(context.Context, string, string, string, string) error
	sendPasswordReset         func(context.Context, string, string, string, string) error
	sendPasswordChangedNotice func(context.Context, string, string, string) error
	sendEmailConfirmation     func(context.Context, string, string, string, string, bool) error
	sendEmailChangedNotice    func(context.Context, string, string, string, string) error
	sendEmailChangedNewNotice func(context.Context, string, string, string) error
	sendEmailChangeRequested  func(context.Context, string, string, string, string) error
	email                     EmailStatus
	now                       func() time.Time
	lockout                   lockout.Store
	handoffDeviceRevoker      func(ctx context.Context, userID, deviceID, keepDeviceID int64) error
	sendNewSignInNotice       func(context.Context, string, string, string, string, string) error
	notifier                  func(int64, string, map[string]any)
	signInNotices             sync.Map
	qr                        *qrlogin.Store
	readPreferences           func(context.Context, int64) (map[string]any, error)
	stopAccount               func(context.Context, int64) error
}

// SetAccountStopper registers what must stop before an account is deleted.
func (h *Handler) SetAccountStopper(stop func(ctx context.Context, userID int64) error) {
	h.stopAccount = stop
}

// SetHandoffDeviceRevoker connects credential revocation to active device
// handoffs. A device ID of zero means every revoked device token for the user.
func (h *Handler) SetHandoffDeviceRevoker(revoke func(ctx context.Context, userID, deviceID, keepDeviceID int64) error) {
	h.handoffDeviceRevoker = revoke
}

func (h *Handler) cancelDeviceHandoffs(ctx context.Context, userID, deviceID, keepDeviceID int64) {
	if h.handoffDeviceRevoker == nil {
		return
	}
	if err := h.handoffDeviceRevoker(ctx, userID, deviceID, keepDeviceID); err != nil {
		slog.Warn("could not cancel handoffs for revoked device credentials", "user_id", userID, "device_id", deviceID, "error", err)
	}
}

// SetSignupOpen applies the server's live public-registration policy.
// Admin configuration changes use this instead of waiting for a restart.
func (h *Handler) SetSignupOpen(open bool) {
	h.signupMu.Lock()
	h.signup = open
	h.signupMu.Unlock()
}

// SetEmailSettings applies administrator-managed confirmation policy and mail
// presentation settings to the running authentication service.
func (h *Handler) SetEmailSettings(status EmailStatus, signupTTL time.Duration, brand string) {
	h.emailMu.Lock()
	h.email = status
	h.requireSignupConfirmation = status.SignupConfirmationRequired
	if signupTTL > 0 {
		h.signupTokenTTL = signupTTL
	}
	if strings.TrimSpace(brand) != "" {
		h.brand = strings.TrimSpace(brand)
	}
	h.emailMu.Unlock()
}

func (h *Handler) emailSettings() EmailStatus {
	h.emailMu.RLock()
	defer h.emailMu.RUnlock()
	return h.email
}

func (h *Handler) requiresSignupConfirmation() bool {
	h.emailMu.RLock()
	defer h.emailMu.RUnlock()
	return h.requireSignupConfirmation
}

func (h *Handler) signupTTL() time.Duration {
	h.emailMu.RLock()
	defer h.emailMu.RUnlock()
	return h.signupTokenTTL
}

func (h *Handler) emailBrand() string {
	h.emailMu.RLock()
	defer h.emailMu.RUnlock()
	return h.brand
}

// SetDefaultQuota applies the live server default to accounts without an
// account-specific quota.
func (h *Handler) SetDefaultQuota(bytes int64) {
	h.quotaMu.Lock()
	h.quota = bytes
	h.quotaMu.Unlock()
}

func (h *Handler) defaultQuota() int64 {
	h.quotaMu.RLock()
	defer h.quotaMu.RUnlock()
	return h.quota
}

func (h *Handler) signupIsOpen() bool {
	h.signupMu.RLock()
	defer h.signupMu.RUnlock()
	return h.signup
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.Keys == nil || options.SessionSecret == "" {
		return nil, errors.New("auth: database, keyring and session secret are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.SignupTokenTTL <= 0 {
		options.SignupTokenTTL = 24 * time.Hour
	}
	if strings.TrimSpace(options.BrandName) == "" {
		options.BrandName = "NeoCloud"
	}
	if options.RequireSignupEmailConfirmation && options.SendSignupConfirmation == nil {
		return nil, errors.New("auth: signup email confirmation is required but no mail sender is configured")
	}
	reserved := make(map[string]struct{}, len(options.ReservedAdminNames))
	for _, name := range options.ReservedAdminNames {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			reserved[name] = struct{}{}
		}
	}
	dummyHash, err := passhash.Hash("neocloud-timing-equalizer", passhash.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("auth: create timing-equalizer hash: %w", err)
	}
	return &Handler{
		db: options.DB, keys: options.Keys, secret: options.SessionSecret,
		blobDir: options.BlobDir, previewDir: options.PreviewDir, uploadTmpDir: options.UploadTmpDir,
		dummyHash: dummyHash,
		quota:     options.DefaultQuotaBytes, secure: options.SecureCookies,
		publicURL: options.PublicURL, publicOrigin: options.PublicOrigin, signup: options.SignupOpen,
		webauthn:                  newWebAuthnService(options.DB, options.AllowedOrigins, options.TrustProxy != "false"),
		brand:                     options.BrandName,
		requireSignupConfirmation: options.RequireSignupEmailConfirmation,
		signupTokenTTL:            options.SignupTokenTTL, pendingSignupMessage: options.PendingSignupMessage,
		reservedAdminNames: reserved, sendSignupConfirmation: options.SendSignupConfirmation,
		sendPasswordReset: options.SendPasswordReset, sendPasswordChangedNotice: options.SendPasswordChangedNotice,
		sendEmailConfirmation: options.SendEmailConfirmation, sendEmailChangedNotice: options.SendEmailChangedNotice,
		sendEmailChangedNewNotice: options.SendEmailChangedNewNotice,
		sendEmailChangeRequested:  options.SendEmailChangeRequestedNotice,
		email:                     options.EmailStatus, now: options.Now,
		lockout:             lockout.Store{DB: options.DB, Now: options.Now},
		sendNewSignInNotice: options.SendNewSignInNotice, notifier: options.Notify, readPreferences: options.ReadPreferences,
		qr: &qrlogin.Store{DB: &dbx.DB{W: options.DB, R: options.DB}, Now: options.Now, WithoutLocation: true},
	}, nil
}

// Routes registers the current identity slice under /api/auth. The handlers
// require Runtime.SessionMiddleware to be applied by the HTTP composition root.
func (h *Handler) Routes(mux *http.ServeMux) {
	limiter := ratelimit.New(ratelimit.Config{
		Window: loginWindow, Max: 20,
		Message: map[string]string{"error": "Too many attempts, try again later", "code": "RATE_LIMITED"},
	})
	mux.HandleFunc("GET /api/auth/status", h.status)
	mux.Handle("POST /api/auth/register", limiter.Middleware(h.requireSecureTransport(http.HandlerFunc(h.register))))
	mux.Handle("POST /api/auth/login", limiter.Middleware(h.requireSecureTransport(http.HandlerFunc(h.login))))
	mux.Handle("POST /api/auth/login/2fa", limiter.Middleware(h.requireSecureTransport(http.HandlerFunc(h.loginTwoFactor))))
	webauthnLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 40, Message: map[string]string{"error": "Too many security key attempts, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/auth/webauthn/login/options", webauthnLimiter.Middleware(h.requireSecureTransport(http.HandlerFunc(h.browserWebAuthnLoginOptions))))
	mux.Handle("POST /api/auth/webauthn/login/verify", webauthnLimiter.Middleware(h.requireSecureTransport(http.HandlerFunc(h.browserWebAuthnLoginVerify))))
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/me", h.me)
	accountLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 30, Message: map[string]string{"error": "Too many account security attempts, try again later", "code": "RATE_LIMITED"}})
	qrCreateLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 60, Message: map[string]string{"error": "Too many QR sign-in requests, try again shortly", "code": "RATE_LIMITED"}})
	qrPollLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 400, Message: map[string]string{"error": "Too many QR sign-in status checks, try again shortly", "code": "RATE_LIMITED"}})
	qrClaimLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 40, Message: map[string]string{"error": "Too many QR sign-in attempts, try again shortly", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/auth/qr-login/challenge", qrCreateLimiter.Middleware(http.HandlerFunc(h.qrCreate)))
	mux.Handle("POST /api/auth/qr-login/challenge/{id}/status", qrPollLimiter.Middleware(http.HandlerFunc(h.qrStatus)))
	mux.Handle("POST /api/auth/qr-login/challenge/{id}/cancel", qrPollLimiter.Middleware(http.HandlerFunc(h.qrCancel)))
	mux.Handle("POST /api/auth/qr-login/challenge/{id}/claim", qrClaimLimiter.Middleware(h.requireSecureTransport(http.HandlerFunc(h.qrClaim))))
	mux.Handle("POST /api/account/qr-login/resolve", accountLimiter.Middleware(http.HandlerFunc(h.qrResolve)))
	mux.Handle("POST /api/account/qr-login/decline", accountLimiter.Middleware(http.HandlerFunc(h.qrDecline)))
	mux.Handle("POST /api/account/qr-login/approve", accountLimiter.Middleware(http.HandlerFunc(h.qrApprove)))
	mux.HandleFunc("GET /api/account", h.account)
	mux.HandleFunc("GET /api/account/usage", h.accountUsage)
	mux.HandleFunc("GET /api/account/sessions", h.listAccountSessions)
	deviceLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 30, Message: map[string]string{"error": "Too many account security attempts, try again later", "code": "RATE_LIMITED"}})
	mux.HandleFunc("GET /api/account/devices", h.listDeviceTokens)
	mux.Handle("POST /api/account/devices", deviceLimiter.Middleware(http.HandlerFunc(h.createDeviceToken)))
	pairingLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 30, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/account/devices/pairing", pairingLimiter.Middleware(http.HandlerFunc(h.createDevicePairing)))
	mux.HandleFunc("GET /api/account/devices/pairing/{code}", h.devicePairingStatus)
	mux.Handle("PATCH /api/account/devices/{id}", deviceLimiter.Middleware(http.HandlerFunc(h.renameDeviceToken)))
	mux.Handle("DELETE /api/account/devices/{id}", deviceLimiter.Middleware(http.HandlerFunc(h.revokeDeviceToken)))
	mux.Handle("DELETE /api/account/sessions/{id}", accountLimiter.Middleware(http.HandlerFunc(h.revokeAccountSession)))
	mux.Handle("PUT /api/account/display-name", accountLimiter.Middleware(http.HandlerFunc(h.updateDisplayName)))
	mux.Handle("PUT /api/account/password", accountLimiter.Middleware(http.HandlerFunc(h.changePassword)))
	mux.Handle("POST /api/account/2fa/setup", accountLimiter.Middleware(http.HandlerFunc(h.setupTwoFactor)))
	mux.Handle("POST /api/account/2fa/enable", accountLimiter.Middleware(http.HandlerFunc(h.enableTwoFactor)))
	mux.Handle("POST /api/account/2fa/disable", accountLimiter.Middleware(http.HandlerFunc(h.disableTwoFactor)))
	mux.Handle("POST /api/account/2fa/recovery-codes", accountLimiter.Middleware(http.HandlerFunc(h.regenerateTwoFactorRecoveryCodes)))
	mux.Handle("PUT /api/account/email", accountLimiter.Middleware(http.HandlerFunc(h.changeEmail)))
	mux.Handle("POST /api/account/email/resend", accountLimiter.Middleware(http.HandlerFunc(h.resendEmailConfirmation)))
	exportLimiter := ratelimit.New(ratelimit.Config{Window: time.Minute, Max: 5, Message: map[string]string{"error": "Too many attempts, try again in a minute", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/account/export", exportLimiter.Middleware(http.HandlerFunc(h.exportAccount)))
	mux.Handle("POST /api/account/delete", exportLimiter.Middleware(http.HandlerFunc(h.deleteAccountSelf)))
	mux.HandleFunc("GET /api/devices/me", h.deviceMe)
	renameLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 60, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("PATCH /api/devices/me", renameLimiter.Middleware(http.HandlerFunc(h.renameCurrentDevice)))
	pageLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 60, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	h.deviceAuthorizationRoutes(mux)
	pairLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 30, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/devices/pair", pairLimiter.Middleware(http.HandlerFunc(h.redeemDevicePairing)))
	securityKeyLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 40, Message: map[string]string{"error": "Too many security key attempts, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/devices/webauthn/options", securityKeyLimiter.Middleware(http.HandlerFunc(h.securityKeySignInOptions)))
	mux.Handle("POST /api/devices/webauthn/verify", securityKeyLimiter.Middleware(http.HandlerFunc(h.securityKeySignInVerify)))
	mux.Handle("POST /api/devices/webauthn/2fa", securityKeyLimiter.Middleware(http.HandlerFunc(h.securityKeySignInTwoFactor)))
	mux.HandleFunc("GET /api/account/security-keys", h.listSecurityKeys)
	mux.Handle("POST /api/account/security-keys/register/options", accountLimiter.Middleware(http.HandlerFunc(h.securityKeyRegistrationOptions)))
	mux.Handle("POST /api/account/security-keys/register/verify", accountLimiter.Middleware(http.HandlerFunc(h.securityKeyRegistrationVerify)))
	mux.Handle("PUT /api/account/security-keys/{id}", accountLimiter.Middleware(http.HandlerFunc(h.renameSecurityKey)))
	mux.Handle("DELETE /api/account/security-keys/{id}", accountLimiter.Middleware(http.HandlerFunc(h.deleteSecurityKey)))
	confirmLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 8, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("GET /api/auth/email/confirm", rateLimitedPage(pageLimiter, http.HandlerFunc(h.confirmationPage), func(w http.ResponseWriter, r *http.Request) {
		h.writeConfirmationPage(w, http.StatusTooManyRequests, "toomany", requestLanguage(r), "")
	}))
	mux.Handle("POST /api/auth/email/confirm", rateLimitedPage(confirmLimiter, http.HandlerFunc(h.confirmEmail), func(w http.ResponseWriter, r *http.Request) {
		h.writeConfirmationPage(w, http.StatusTooManyRequests, "toomany", requestLanguage(r), "")
	}))
	resendLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 10, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	forgotLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 10, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("POST /api/auth/email/resend", resendLimiter.Middleware(http.HandlerFunc(h.resendSignupConfirmation)))
	mux.Handle("POST /api/auth/password/forgot", forgotLimiter.Middleware(http.HandlerFunc(h.forgotPassword)))
	resetPageLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 60, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	resetLimiter := ratelimit.New(ratelimit.Config{Window: loginWindow, Max: 8, Message: map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"}})
	mux.Handle("GET /api/auth/password/reset", rateLimitedPage(resetPageLimiter, http.HandlerFunc(h.passwordResetPage), func(w http.ResponseWriter, r *http.Request) {
		h.writeResetPage(w, http.StatusTooManyRequests, requestLanguage(r), "toomany", "", "")
	}))
	mux.Handle("POST /api/auth/password/reset", rateLimitedPage(resetLimiter, http.HandlerFunc(h.resetPassword), func(w http.ResponseWriter, r *http.Request) {
		h.writeResetPage(w, http.StatusTooManyRequests, requestLanguage(r), "toomany", "", "")
	}))
}

// rateLimitedPage keeps the shared limiter's counters and headers while
// rendering its refusal through the same HTML page as the route.
func rateLimitedPage(limiter *ratelimit.Limiter, next http.Handler, limited http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := httptest.NewRecorder()
		limiter.Middleware(next).ServeHTTP(buffer, r)
		for name, values := range buffer.Header() {
			w.Header()[name] = append([]string(nil), values...)
		}
		if buffer.Code == http.StatusTooManyRequests {
			limited(w, r)
			return
		}
		w.WriteHeader(buffer.Code)
		_, _ = w.Write(buffer.Body.Bytes())
	})
}

type user struct {
	ID            int64          `json:"id"`
	Username      string         `json:"username"`
	DisplayName   sql.NullString `json:"-"`
	Email         sql.NullString `json:"-"`
	Password      string         `json:"-"`
	IsAdmin       int            `json:"-"`
	QuotaBytes    sql.NullInt64  `json:"-"`
	UsedBytes     int64          `json:"-"`
	CreatedAt     string         `json:"created_at"`
	LastLogin     sql.NullString `json:"-"`
	DisabledAt    sql.NullString `json:"-"`
	EmailVerified sql.NullString `json:"-"`
}

func (h *Handler) loadUser(ctx context.Context, id int64) (*user, error) {
	u := &user{}
	err := h.db.QueryRowContext(ctx, `SELECT id, username, display_name, email, password,
		is_admin, quota_bytes, used_bytes, created_at, last_login, disabled_at, email_verified_at
		FROM users WHERE id = ?`, id).Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Password, &u.IsAdmin,
		&u.QuotaBytes, &u.UsedBytes, &u.CreatedAt, &u.LastLogin, &u.DisabledAt, &u.EmailVerified,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (h *Handler) findUser(ctx context.Context, identifier string) (*user, error) {
	u := &user{}
	err := h.db.QueryRowContext(ctx, `SELECT id, username, display_name, email, password,
		is_admin, quota_bytes, used_bytes, created_at, last_login, disabled_at, email_verified_at
		FROM users WHERE lower(username) = lower(?) OR lower(email) = lower(?)
		ORDER BY lower(username) = lower(?) DESC, id LIMIT 1`, identifier, identifier, identifier).Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Password, &u.IsAdmin,
		&u.QuotaBytes, &u.UsedBytes, &u.CreatedAt, &u.LastLogin, &u.DisabledAt, &u.EmailVerified,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

type storageUsage struct {
	UsedBytes    int64 `json:"usedBytes"`
	QuotaBytes   int64 `json:"quotaBytes"`
	Unlimited    bool  `json:"unlimited"`
	CustomQuota  bool  `json:"customQuota"`
	TrashBytes   int64 `json:"trashBytes"`
	VersionBytes int64 `json:"versionBytes"`
}

type userPayload struct {
	ID              int64        `json:"id"`
	Username        string       `json:"username"`
	DisplayName     *string      `json:"displayName"`
	Email           *string      `json:"email"`
	EmailVerifiedAt *string      `json:"emailVerifiedAt"`
	PendingEmail    *string      `json:"pendingEmail"`
	CreatedAt       string       `json:"createdAt"`
	LastLogin       *string      `json:"lastLogin"`
	IsAdmin         bool         `json:"isAdmin"`
	Storage         storageUsage `json:"storage"`
}

func (h *Handler) payload(ctx context.Context, u *user) (userPayload, error) {
	quota := h.defaultQuota()
	if u.QuotaBytes.Valid {
		quota = u.QuotaBytes.Int64
	}
	usage := storageUsage{UsedBytes: u.UsedBytes, QuotaBytes: quota, Unlimited: quota == -1, CustomQuota: u.QuotaBytes.Valid}
	err := h.db.QueryRowContext(ctx, `WITH RECURSIVE trashed(id) AS (
		SELECT id FROM nodes WHERE user_id = ? AND trashed_at IS NOT NULL
		UNION SELECT n.id FROM nodes n JOIN trashed t ON n.parent_id = t.id
	) SELECT COALESCE(SUM(n.size), 0) FROM nodes n JOIN trashed t ON n.id = t.id WHERE n.type = 'file'`, u.ID).Scan(&usage.TrashBytes)
	if err != nil {
		return userPayload{}, err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(v.size), 0) FROM file_versions v
		JOIN nodes n ON n.id = v.node_id WHERE n.user_id = ?`, u.ID).Scan(&usage.VersionBytes); err != nil {
		return userPayload{}, err
	}
	var display, email, verified, last sql.NullString
	display, email, verified, last = u.DisplayName, u.Email, u.EmailVerified, u.LastLogin
	var pending sql.NullString
	_ = h.db.QueryRowContext(ctx, `SELECT t.email FROM user_email_tokens t JOIN users current ON current.id = t.user_id
		WHERE t.user_id = ? AND t.type = 'email_change_confirmation' AND t.consumed_at IS NULL
		AND datetime(t.expires_at) > datetime('now') AND lower(t.email) != lower(COALESCE(current.email, ''))
		ORDER BY t.id DESC LIMIT 1`, u.ID).Scan(&pending)
	return userPayload{
		ID: u.ID, Username: u.Username, DisplayName: nullableString(display), Email: nullableString(email),
		EmailVerifiedAt: nullableString(verified), PendingEmail: nullableString(pending), CreatedAt: u.CreatedAt,
		LastLogin: nullableString(last), IsAdmin: u.IsAdmin == 1, Storage: usage,
	}, nil
}

func nullableString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

func (h *Handler) authenticatedUser(r *http.Request) (*user, bool, error) {
	session := sessionx.FromContext(r.Context())
	if session == nil {
		return nil, false, errors.New("session middleware is not configured")
	}
	id, ok := session.Int64("userId")
	if !ok {
		return nil, false, nil
	}
	u, err := h.loadUser(r.Context(), id)
	if err != nil {
		return nil, false, err
	}
	if u == nil || u.DisabledAt.Valid {
		session.Destroy()
		return nil, false, nil
	}
	return u, true, nil
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		writeError(w, 500, "Could not read account status", "INTERNAL_ERROR")
		return
	}
	u, authenticated, err := h.authenticatedUser(r)
	if err != nil {
		writeError(w, 500, "Could not read account status", "INTERNAL_ERROR")
		return
	}
	var payload *userPayload
	if authenticated {
		value, err := h.payload(r.Context(), u)
		if err != nil {
			writeError(w, 500, "Could not read account status", "INTERNAL_ERROR")
			return
		}
		payload = &value
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hasUser": count > 0, "registrationOpen": count == 0 || h.signupIsOpen(),
		"authenticated": authenticated, "user": payload, "email": h.emailSettings(),
	})
}
