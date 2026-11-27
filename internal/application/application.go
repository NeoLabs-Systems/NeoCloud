// Package application composes the NeoCloud runtime and converted HTTP route
// groups. It is intentionally the only package that wires app policy into
// shared middleware and domain services.
package application

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/accountmail"
	"github.com/NeoLabs-Systems/neocloud/internal/activity"
	"github.com/NeoLabs-Systems/neocloud/internal/admin"
	"github.com/NeoLabs-Systems/neocloud/internal/agentapi"
	"github.com/NeoLabs-Systems/neocloud/internal/ai"
	"github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/backups"
	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
	"github.com/NeoLabs-Systems/neocloud/internal/comments"
	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/documentconvert"
	"github.com/NeoLabs-Systems/neocloud/internal/externalstorage"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/handoff"
	"github.com/NeoLabs-Systems/neocloud/internal/httppolicy"
	"github.com/NeoLabs-Systems/neocloud/internal/imports"
	"github.com/NeoLabs-Systems/neocloud/internal/loghistory"
	"github.com/NeoLabs-Systems/neocloud/internal/maintenance"
	"github.com/NeoLabs-Systems/neocloud/internal/members"
	"github.com/NeoLabs-Systems/neocloud/internal/neorouter"
	"github.com/NeoLabs-Systems/neocloud/internal/notes"
	"github.com/NeoLabs-Systems/neocloud/internal/oauth"
	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
	"github.com/NeoLabs-Systems/neocloud/internal/office"
	"github.com/NeoLabs-Systems/neocloud/internal/photos"
	"github.com/NeoLabs-Systems/neocloud/internal/publicshares"
	"github.com/NeoLabs-Systems/neocloud/internal/realtime"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
	"github.com/NeoLabs-Systems/neocloud/internal/settings"
	"github.com/NeoLabs-Systems/neocloud/internal/signing"
	syncapi "github.com/NeoLabs-Systems/neocloud/internal/sync"
	"github.com/NeoLabs-Systems/neocloud/internal/updates"
	"github.com/NeoLabs-Systems/neocloud/internal/vaults"
	"github.com/NeoLabs-Systems/neocloud/internal/webdav"
	"github.com/NeoLabs-Systems/neocloud/internal/webui"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/publicerr"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

type Application struct {
	Runtime              *runtimeapp.Runtime
	api                  http.Handler
	searchIndexer        *searchindex.Indexer
	aiHandler            *ai.Handler
	photos               *photos.Handler
	imports              *imports.Handler
	handoff              *handoff.Handler
	realtime             *realtime.Server
	signing              *signing.Handler
	backupRunner         *backups.Runner
	maintenanceRunner    *maintenance.Runner
	storageClassMigrator *backups.StorageClassMigrator
	workerMu             sync.Mutex
	workerCancel         context.CancelFunc
	workerDone           chan struct{}
}

func Open(ctx context.Context, getenv func(string) string, userHome string) (*Application, error) {
	startedAt := time.Now()
	runtime, err := runtimeapp.Open(ctx, getenv, userHome)
	if err != nil {
		return nil, err
	}
	documentconvert.Configure(documentconvert.Settings{
		Disabled: !runtime.Config.OfficePreviewEnabled, Binary: runtime.Config.LibreOfficePath,
		SandboxOff: !runtime.Config.PreviewSandbox,
	})
	ocr.Configure(ocr.Settings{Disabled: !runtime.Config.OCREnabled, Languages: runtime.Config.OCRLanguages})
	// Looking for the tools now reports them once at startup.
	if status := ocr.CurrentStatus(); status.Enabled {
		slog.Info("Text recognition is on", "languages", strings.Join(status.Languages, "+"), "scanned_pdfs", status.PDF, "missing_languages", strings.Join(status.Missing, "+"))
	} else if runtime.Config.OCREnabled {
		slog.Info("Text recognition is off: tesseract with one of the configured languages is not installed", "missing_languages", strings.Join(status.Missing, "+"))
	}
	origins := httpx.ParseOrigins(strings.Join(runtime.Config.Origins, ","))
	liveEvents, err := newRealtime(runtime, origins)
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	email := accountmail.New(runtime.Config.Email)
	email.SetSigningMails(runtime.Config.SigningMails)
	emailStatus := email.Status()
	identity, err := auth.New(auth.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, SessionSecret: runtime.Config.SessionSecret,
		BlobDir: runtime.Config.Paths.BlobDir, PreviewDir: runtime.Config.Paths.PreviewDir, UploadTmpDir: runtime.Config.Paths.UploadTmpDir,
		DefaultQuotaBytes: runtime.Config.DefaultQuotaBytes,
		SecureCookies:     runtime.Config.SecureCookies, PublicURL: runtime.Config.PublicURL,
		PublicOrigin:                   publicOriginResolver(runtime.Config.PublicURL),
		AllowedOrigins:                 runtime.Config.Origins,
		TrustProxy:                     runtime.Config.TrustProxy,
		SignupOpen:                     runtime.Config.AllowSignup,
		RequireSignupEmailConfirmation: email.SignupConfirmationRequired(),
		SignupTokenTTL:                 time.Duration(runtime.Config.Email.TokenTTLHours * float64(time.Hour)),
		BrandName:                      runtime.Config.Email.BrandName,
		PendingSignupMessage:           email.PendingSignupMessage,
		ReservedAdminNames:             runtime.Config.ReservedAdminNames,
		SendSignupConfirmation:         email.SendSignupConfirmation,
		SendPasswordReset:              email.SendPasswordReset,
		SendPasswordChangedNotice:      email.SendPasswordChangedNotice,
		SendNewSignInNotice:            email.SendNewSignInNotice,
		Notify:                         liveEvents.Publish,
		ReadPreferences: func(ctx context.Context, userID int64) (map[string]any, error) {
			return settings.ReadPreferences(ctx, runtime.DB.W, userID)
		},
		SendEmailConfirmation:          email.SendEmailConfirmation,
		SendEmailChangedNotice:         email.SendEmailChangeNotice,
		SendEmailChangedNewNotice:      email.SendEmailChangedNewNotice,
		SendEmailChangeRequestedNotice: email.SendEmailChangeRequestedNotice,
		EmailStatus: auth.EmailStatus{
			Enabled: emailStatus.Enabled, Configured: emailStatus.Configured,
			SignupConfirmationRequired:      emailStatus.SignupConfirmationRequired,
			EmailChangeConfirmationRequired: emailStatus.EmailChangeConfirmationRequired,
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	searchEnabled := &atomic.Bool{}
	searchEnabled.Store(runtime.Config.FullTextSearchEnabled)
	var fileHandler *files.Handler
	var signingHandler *signing.Handler
	var agentFiles *agentapi.Handler
	var importsHandler *imports.Handler
	var vaultsHandler *vaults.Handler
	var oauthProtocol *oauth.Protocol
	var settingsHandler *settings.Handler
	var aiHandler *ai.Handler
	var searchIndexer *searchindex.Indexer
	aiPolicy := newAIPolicy(runtime.Config.AIEnabled, runtime.Config.AIMonthlyTokens)
	backupRunner, err := backups.New(runtime.DB, runtime.Keys, runtime.Config.Paths.BlobDir, runtime.Config.Paths.UploadTmpDir, runtime.Config.Paths.BackupDir, serverBuildVersion())
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	storageClassMigrator, err := backups.NewStorageClassMigrator(runtime.Config.Paths.StateDir)
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	maintenanceRunner := maintenance.New(runtime.DB.W, runtime.Config.Paths.DatabaseFile)
	maintenanceRunner.SetBlobScrubber(runtime.Keys, runtime.Config.Paths.BlobDir)
	mux := http.NewServeMux()
	webHandler, err := webui.New(webui.Options{
		WebDir: runtime.Config.Paths.WebDir, LandingDir: runtime.Config.Paths.LandingDir, Analytics: runtime.Config.Analytics,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	webHandler.Routes(mux)
	identity.Routes(mux)
	adminHandler, err := admin.New(admin.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, DefaultQuotaBytes: runtime.Config.DefaultQuotaBytes,
		EnvFile: runtime.Config.Paths.EnvFile, AllowSignup: runtime.Config.AllowSignup,
		MaxUploadBytes: runtime.Config.MaxUploadBytes, TrashRetentionDays: runtime.Config.TrashRetentionDays,
		VersionRetention: runtime.Config.VersionRetention, FullTextSearchEnabled: runtime.Config.FullTextSearchEnabled,
		DisabledModelIDs: runtime.Config.DisabledModels, KnownModelIDs: runtime.Config.KnownModels,
		KnownModelsConfigured:         runtime.Config.KnownModelsConfigured,
		AutoEnableNewModels:           runtime.Config.AutoEnableNewModels,
		AutoEnableNewModelsConfigured: runtime.Config.AutoEnableNewModelsConfigured,
		AIEnabled:                     runtime.Config.AIEnabled, AIMonthlyTokens: runtime.Config.AIMonthlyTokens,
		ProviderKeys: runtime.Config.AIProviderKeys, ProviderBaseURLs: runtime.Config.AIProviderBaseURLs,
		MasterKeyHex: runtime.Config.MasterKeyHex,
		Notify:       liveEvents.Publish,
		Reauthenticate: func(ctx context.Context, userID int64, currentPassword, code string) error {
			return identity.ConfirmIdentity(ctx, userID, currentPassword, code)
		},
		OnProviderChanged: func(envKey, value string) {
			if settingsHandler != nil {
				settingsHandler.SetServerProviderValue(envKey, value)
			}
		},
		OnAIConfigChanged: func(enabled bool, monthlyTokens int64) {
			aiPolicy.set(enabled, monthlyTokens)
			if aiHandler != nil {
				aiHandler.SetPolicy(enabled, monthlyTokens)
			}
		},
		AdminModelCatalog: func(ctx context.Context) ([]settings.AdminModel, error) {
			if settingsHandler == nil {
				return []settings.AdminModel{}, nil
			}
			return settingsHandler.AdminModelCatalog(ctx)
		},
		OnDisabledModelsChanged: func(ids []string) {
			if settingsHandler != nil {
				settingsHandler.SetDisabledModelIDs(ids)
			}
		},
		OnBackupTargetRun: func(targetID int64, targetType string, config map[string]any, retention int) (int64, error) {
			runID, runErr := backupRunner.RunTarget(targetID, targetType, config, retention)
			if errors.Is(runErr, backups.ErrAlreadyRunning) {
				return 0, admin.ErrBackupRunning
			}
			if errors.Is(runErr, backups.ErrNotStarted) {
				return 0, admin.ErrBackupWorkerUnavailable
			}
			return runID, runErr
		},
		OnBackupTargetTest: probeBackupTarget,
		OnBackupTargetSnapshots: func(ctx context.Context, targetType string, config map[string]any) ([]string, error) {
			return listBackupTargetSnapshots(ctx, targetType, config)
		},
		OnBackupTargetVerify: func(ctx context.Context, targetType string, config map[string]any) any {
			return verifyBackupTargetSnapshot(backupRunner, ctx, targetType, config)
		},
		OnStorageClassMigrationStart: func(targetID int64, config map[string]any) error {
			migrationConfig := backups.S3Config{
				Endpoint: stringValue(config["endpoint"]), Region: stringValue(config["region"]), Bucket: stringValue(config["bucket"]),
				AccessKeyID: stringValue(config["accessKeyId"]), SecretAccessKey: stringValue(config["secretAccessKey"]),
				Prefix: stringValue(config["prefix"]), ForcePathStyle: config["forcePathStyle"] == true,
				StorageClass: stringValue(config["storageClass"]),
			}
			if err := storageClassMigrator.Start(targetID, migrationConfig); errors.Is(err, backups.ErrStorageMigrationRunning) {
				return admin.ErrStorageMigrationRunning
			} else {
				return err
			}
		},
		OnStorageClassMigrationStatus: func(targetID int64) any {
			status, statusErr := storageClassMigrator.Status(targetID)
			if statusErr != nil {
				slog.Error("Could not read backup storage migration status", "targetId", targetID, "error", statusErr)
				return nil
			}
			return status
		},
		OnBackupVerified: func(targetID int64, result any) error {
			return backups.RecordVerification(runtime.Config.Paths.StateDir, targetID, result)
		},
		OnBackupVerification: func(targetID int64) (any, error) {
			return backups.LastVerification(runtime.Config.Paths.StateDir, targetID)
		},
		OnHealthMetadata: func() map[string]any {
			check := maintenanceRunner.Status()
			damagedBlobs, blobScrubCycleAt, scrubErr := maintenanceRunner.ScrubStatus()
			if scrubErr != nil {
				slog.Error("Could not read blob scrub status", "error", scrubErr)
			}
			var checkedAt any
			if check.CheckedAt != "" {
				checkedAt = check.CheckedAt
			}
			return map[string]any{
				"name": "NeoCloud", "product": "neocloud",
				"version": serverBuildVersion(), "runtime": "Go " + goruntime.Version(),
				"minFreeDiskBytes": runtime.Config.MinFreeDiskBytes,
				"storageDisk":      admin.DiskSpace(runtime.Config.Paths.StorageDir),
				"dataDisk":         admin.DiskSpace(runtime.Config.Paths.DataDir),
				"uptimeSeconds":    int64(time.Since(startedAt).Seconds()),
				"database":         check.Status, "databaseCheckedAt": checkedAt,
				"damagedBlobs": damagedBlobs, "blobScrubCycleAt": blobScrubCycleAt,
				"previews": map[string]bool{
					"imageThumbnails": true, "officePreview": documentconvert.Available(),
					"fullTextSearch": searchEnabled.Load(),
				},
				"ocr": ocr.CurrentStatus(),
			}
		},
		OnSendTestEmail: func(ctx context.Context, recipient, language string) (string, error) {
			return email.SendTestEmail(ctx, recipient, language)
		},
		OnEmailChanged: func() {
			// Read the settings as startup does, so a change applies without a restart.
			reloaded, err := config.Load(getenv, userHome)
			if err != nil {
				slog.Error("Could not apply the email settings", "error", err)
				return
			}
			email.SetConfig(reloaded.Email)
			email.SetSigningMails(reloaded.SigningMails)
			identity.SetEmailSettings(auth.EmailStatus{
				Enabled: reloaded.Email.Enabled, Configured: reloaded.Email.Enabled,
				SignupConfirmationRequired:      reloaded.Email.RequireSignupConfirmation,
				EmailChangeConfirmationRequired: reloaded.Email.RequireChangeConfirmation,
			}, time.Duration(reloaded.Email.TokenTTLHours*float64(time.Hour)), reloaded.Email.BrandName)
		},
		OnSignupChanged: identity.SetSignupOpen,
		OnStorageChanged: func(policy admin.StoragePolicy) {
			identity.SetDefaultQuota(policy.DefaultQuotaBytes)
			if fileHandler != nil {
				fileHandler.SetStoragePolicy(files.StoragePolicy{DefaultQuotaBytes: policy.DefaultQuotaBytes, MaxUploadBytes: policy.MaxUploadBytes, TrashRetentionDays: policy.TrashRetentionDays, VersionRetention: policy.VersionRetention})
			}
			if agentFiles != nil {
				agentFiles.SetStoragePolicy(policy.DefaultQuotaBytes, policy.MaxUploadBytes, policy.VersionRetention)
			}
			if signingHandler != nil {
				signingHandler.SetDefaultQuota(policy.DefaultQuotaBytes)
			}
			if vaultsHandler != nil {
				vaultsHandler.SetDefaultQuota(policy.DefaultQuotaBytes)
			}
		},
		OnFullTextSearchChanged: searchEnabled.Store,
		OnSearchIndexStatus: func(ctx context.Context) (map[string]any, error) {
			return searchIndexStatus(searchIndexer.Status(ctx))
		},
		OnSearchIndexRebuild: func(ctx context.Context) (map[string]any, error) {
			return searchIndexStatus(searchIndexer.Rebuild(ctx))
		},
		SessionSecret: runtime.Config.SessionSecret,
		BlobDir:       runtime.Config.Paths.BlobDir, PreviewDir: runtime.Config.Paths.PreviewDir,
		UploadTmpDir: runtime.Config.Paths.UploadTmpDir,
		ResolvePrincipal: func(r *http.Request) (admin.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return admin.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	adminHandler.Routes(mux)
	adminHandler.LogsRoutes(mux, loghistory.Default())
	adminHandler.AuditRoutes(mux)
	adminHandler.UserActionRoutes(mux)
	connectedApps, err := oauth.New(oauth.Options{
		DB: runtime.DB.W,
		ResolvePrincipal: func(r *http.Request) (oauth.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return oauth.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	connectedApps.Routes(mux)
	connectedApps.CompanionRoutes(mux, publicOriginResolver(runtime.Config.PublicURL))
	oauthProtocol, err = oauth.NewProtocol(oauth.ProtocolOptions{
		DB: runtime.DB.W, SignIn: identity,
		ResolvePrincipal: func(r *http.Request) (oauth.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return oauth.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	oauthProtocol.Routes(mux)
	neoRouterConnector, err := neorouter.NewRuntimeConnector(neorouter.RuntimeOptions{
		DB: runtime.DB.W, Keys: runtime.Keys, EnvFile: runtime.Config.Paths.EnvFile, Env: runtime.Config.NeoRouterEnv,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	settingsHandler, err = settings.New(settings.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, ProviderBaseURLs: runtime.Config.AIProviderBaseURLs,
		ProviderKeys:     runtime.Config.AIProviderKeys,
		DisabledModelIDs: runtime.Config.DisabledModels,
		NeoRouter:        neoRouterConnector.Connection,
		AIPolicy:         aiPolicy.get,
		ReferenceURL:     runtime.Config.AIReferenceURL,
		ResolvePrincipal: func(r *http.Request) (settings.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return settings.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	settingsHandler.Routes(mux)
	externalStorageHandler, err := externalstorage.New(externalstorage.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, AllowPrivate: runtime.Config.ExternalAllowPrivate,
		MaxUploadBytes: runtime.Config.ExternalMaxUploadBytes,
		ResolvePrincipal: func(r *http.Request) (externalstorage.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return externalstorage.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
		IsAdmin: func(ctx context.Context, userID int64) (bool, error) {
			var isAdmin int
			var disabled sql.NullString
			if err := runtime.DB.W.QueryRowContext(ctx, `SELECT is_admin,disabled_at FROM users WHERE id=?`, userID).Scan(&isAdmin, &disabled); err != nil {
				return false, err
			}
			return isAdmin == 1 && !disabled.Valid, nil
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	externalStorageHandler.Routes(mux)
	neoRouterHandler, err := neorouter.New(neorouter.Options{
		ResolvePrincipal: func(r *http.Request) (neorouter.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return neorouter.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
		IsAdmin: func(ctx context.Context, userID int64) (bool, error) {
			var isAdmin int
			var disabled sql.NullString
			if err := runtime.DB.W.QueryRowContext(ctx, `SELECT is_admin,disabled_at FROM users WHERE id=?`, userID).Scan(&isAdmin, &disabled); err != nil {
				return false, err
			}
			return isAdmin == 1 && !disabled.Valid, nil
		},
		Connector: neoRouterConnector,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	neoRouterHandler.Routes(mux)
	vaultsHandlerInstance, err := vaults.New(vaults.Options{
		DB: runtime.DB.W, DefaultQuotaBytes: runtime.Config.DefaultQuotaBytes, MaxNodes: runtime.Config.MaxNodes,
		ResolvePrincipal: func(r *http.Request) (vaults.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return vaults.Principal{UserID: principal.UserID}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	vaultsHandler = vaultsHandlerInstance
	vaultsHandler.Routes(mux)
	photosHandler, err := photos.New(photos.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, BlobDir: runtime.Config.Paths.BlobDir,
		ResolvePrincipal: func(r *http.Request) (photos.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return photos.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	photosHandler.Routes(mux)
	activityHandler, err := activity.New(activity.Options{
		DB: runtime.DB.W,
		ResolvePrincipal: func(r *http.Request) (activity.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return activity.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	activityHandler.Routes(mux)
	syncHandler, err := syncapi.New(syncapi.Options{
		DB: runtime.DB.W,
		ResolvePrincipal: func(r *http.Request) (syncapi.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return syncapi.Principal{UserID: principal.UserID}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	syncHandler.Routes(mux)
	membersHandler, err := members.New(members.Options{
		DB: runtime.DB.W,
		NodeViews: func(ctx context.Context, ownerID int64, ids []string) (map[string]any, error) {
			return fileHandler.NodeViews(ctx, ownerID, ids)
		},
		ResolvePrincipal: func(r *http.Request) (members.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return members.Principal{UserID: principal.UserID}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	membersHandler.Routes(mux)
	commentsHandler, err := comments.New(comments.Options{
		DB: runtime.DB.W, Keys: runtime.Keys,
		ResolvePrincipal: func(r *http.Request) (comments.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return comments.Principal{UserID: principal.UserID}, ok
		},
		Notify: liveEvents.Publish,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	commentsHandler.Routes(mux)
	fileHandler = files.New(files.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, BlobDir: runtime.Config.Paths.BlobDir,
		UploadTmpDir: runtime.Config.Paths.UploadTmpDir, PreviewDir: runtime.Config.Paths.PreviewDir,
		TrashRetentionDays: runtime.Config.TrashRetentionDays, VersionRetention: runtime.Config.VersionRetention,
		MaxUploadBytes: runtime.Config.MaxUploadBytes, DefaultQuotaBytes: runtime.Config.DefaultQuotaBytes,
		MaxNodes:     runtime.Config.MaxNodes,
		MaxCopyNodes: runtime.Config.MaxCopyNodes, MinFreeDiskBytes: runtime.Config.MinFreeDiskBytes,
	})
	publicShareHandler, err := publicshares.New(publicshares.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, Files: fileHandler,
		SessionSecret: runtime.Config.SessionSecret, PublicURL: runtime.Config.PublicURL,
		PublicOrigin:  publicOriginResolver(runtime.Config.PublicURL),
		SecureCookies: runtime.Config.SecureCookies,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	publicShareHandler.Routes(mux)
	maintenanceRunner.SetFileCleanup(fileHandler.PurgeExpiredTrash, fileHandler.PurgeExpiredUploads)
	photosHandler.SetThumbnailer(fileHandler.PrepareGridThumbnail)
	externalStorageHandler.SetFiles(fileHandler)
	searchIndexer, err = searchindex.New(runtime.DB.W, runtime.Keys, searchBlobReader(runtime), searchindex.ExtractText, searchEnabled.Load)
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	fileHandler.SetSearchIndexer(searchIndexer)
	aiHandler, err = ai.New(ai.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, Enabled: runtime.Config.AIEnabled,
		DefaultMonthlyTokens: runtime.Config.AIMonthlyTokens,
		Files:                aiTree{fileHandler}, Models: settingsHandler, ModelError: aiModelError,
		ResolveRoles: settingsHandler.ResolveAIRoles, NotifyEvent: liveEvents.Publish,
		ReadBlob: searchBlobReader(runtime), Index: searchIndexer, PhotoCatchUp: photosHandler.CatchUp,
		ResolvePrincipal: func(r *http.Request) (ai.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return ai.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	aiHandler.Routes(mux)
	fileHandler.Routes(mux)
	officeHandler, err := office.New(office.Options{
		DB: runtime.DB.W, EnvFile: runtime.Config.Paths.EnvFile, Files: fileHandler,
		SessionSecret: runtime.Config.SessionSecret, EditorURL: runtime.Config.OfficeEditorURL,
		HostURL: runtime.Config.OfficeHostURL, Enabled: runtime.Config.OfficeEditorEnabled,
		ResolvePrincipal: func(r *http.Request) (office.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return office.Principal{UserID: principal.UserID}, ok
		},
		ResolveAdmin: func(r *http.Request) (int64, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			// A browser session or the phone app, never a desktop token.
			if !ok || principal.IsDevice && principal.Platform != "android" && principal.Platform != "ios" {
				return 0, false
			}
			var isAdmin int
			err := runtime.DB.W.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id=? AND disabled_at IS NULL`, principal.UserID).Scan(&isAdmin)
			return principal.UserID, err == nil && isAdmin == 1
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	officeHandler.Routes(mux)
	handoffHandler, err := handoff.New(handoff.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, BlobDir: runtime.Config.Paths.BlobDir, Files: fileHandler,
		ResolvePrincipal: func(r *http.Request) (handoff.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return handoff.Principal{UserID: principal.UserID, DeviceID: principal.DeviceID, IsDevice: principal.IsDevice, Platform: principal.Platform}, ok
		},
		RequestKey: func(r *http.Request, principal handoff.Principal) string {
			if principal.IsDevice && principal.DeviceID > 0 {
				return "d:" + strconv.FormatInt(principal.DeviceID, 10)
			}
			session := sessionx.FromContext(r.Context())
			if session == nil {
				return ""
			}
			return handoff.SessionKey(sessionx.HashID(runtime.Config.SessionSecret, session.ID()))
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	liveEvents.OnConnection(func(client realtime.Client) func() {
		return handoffHandler.AttachBrowser(client.UserID, client.Hash, client.UserAgent, client.Emit, client.Close)
	})
	identity.SetHandoffDeviceRevoker(handoffHandler.CancelRevokedDeviceOffers)
	adminHandler.SetDeviceCredentialsRevoked(func(ctx context.Context, userID, keepDeviceID int64) error {
		return handoffHandler.CancelRevokedDeviceOffers(ctx, userID, 0, keepDeviceID)
	})
	handoffHandler.Routes(mux)
	davHandler := webdav.NewHandler(webdav.Options{
		AllowedOrigin: func(origin string) bool { return origins.Allowed(origin, false) },
		Authenticate:  webdav.AppPasswords(runtime.DB.W),
		Dispatch:      fileHandler.DAVDispatch,
		// The proxy trust middleware has already put the client in RemoteAddr.
		ClientIP: func(r *http.Request) string { return httpx.ClientIP(r, false) },
	})
	agentFiles, err = agentapi.New(agentapi.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, BlobDir: runtime.Config.Paths.BlobDir,
		Files:             fileHandler,
		Authenticate:      oauthProtocol.AuthenticateAccessToken,
		DefaultQuotaBytes: runtime.Config.DefaultQuotaBytes,
		MaxUploadBytes:    runtime.Config.MaxUploadBytes, MaxNodes: runtime.Config.MaxNodes,
		VersionRetention: runtime.Config.VersionRetention,
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	importsHandler, err = imports.New(imports.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, Files: fileHandler,
		AllowPrivate: runtime.Config.ImportAllowPrivate,
		Notify:       liveEvents.Publish,
		IsAdmin: func(ctx context.Context, userID int64) (bool, error) {
			var isAdmin int
			var disabled sql.NullString
			if err := runtime.DB.W.QueryRowContext(ctx, `SELECT is_admin,disabled_at FROM users WHERE id=?`, userID).Scan(&isAdmin, &disabled); err != nil {
				return false, err
			}
			return isAdmin == 1 && !disabled.Valid, nil
		},
		ResolvePrincipal: func(r *http.Request) (imports.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return imports.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	importsHandler.Routes(mux)
	identity.SetAccountStopper(importsHandler.CancelAccount)
	adminHandler.SetAccountStopper(importsHandler.CancelAccount)
	notesHandler, err := notes.New(notes.Options{
		Files: agentFiles,
		AI:    noteAI{policy: aiHandler, chat: settingsHandler},
		ResolvePrincipal: func(r *http.Request) (notes.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return notes.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	notesHandler.Routes(mux)
	agentFiles.Routes(mux)
	signingHandler, err = signing.New(signing.Options{
		DB: runtime.DB.W, Keys: runtime.Keys, BlobDir: runtime.Config.Paths.BlobDir,
		DefaultQuotaBytes: runtime.Config.DefaultQuotaBytes,
		Agent:             agentFiles,
		MailConfig:        email.Config,
		MailEnabled:       email.SigningMailEnabled,
		SendMail:          email.SendMessage,
		PublicOrigin:      publicOriginResolver(runtime.Config.PublicURL),
		Notify:            liveEvents.Publish,
		NoSeal:            !runtime.Config.SigningSeal,
		RemoveBlobs:       fileHandler.RemoveBlobFiles,
		StoreFile: func(ctx context.Context, userID int64, parentID, name string, content []byte) (string, error) {
			stored, storeErr := fileHandler.StoreGeneratedFile(ctx, userID, parentID, name, content)
			return stored.ID, storeErr
		},
		ResolvePrincipal: func(r *http.Request) (signing.Principal, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			return signing.Principal{UserID: principal.UserID, Device: principal.IsDevice, Platform: principal.Platform}, ok
		},
	})
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	signingHandler.Routes(mux)
	systemRoutes(mux, runtime)
	updates.New(updates.Options{
		Version: buildinfo.Version, EnvFile: runtime.Config.Paths.EnvFile,
		StatusFile: runtime.Config.Paths.UpdateStatusFile, LogDir: runtime.Config.Paths.LogDir,
	}).Routes(mux, adminHandler.Gate, adminHandler.Audit)
	api := httpx.Chain(mux,
		httpx.Recover(slog.Default()),
		httppolicy.LogFailures(signedInUser),
		httpx.CompressJSON(),
		httppolicy.SecurityHeaders(httppolicy.HeaderOptions{
			SecureCookies: runtime.Config.SecureCookies, HSTSMaxAge: runtime.Config.HSTSMaxAgeSeconds,
			AllowExternal: runtime.Config.CSPAllowExternal, EditorOrigins: officeHandler.EditorOrigins,
			ConnectOrigins: analyticsOrigins(runtime.Config.Analytics),
		}),
		httpx.APICaseGuard(),
		httpx.OriginGuard(origins),
		httpx.NoStoreAPI(),
		httpx.CORS(origins),
		publicerr.Middleware(adminRequest(runtime.DB.W)),
		httppolicy.BodyLimit,
	)
	identified := func(next http.Handler) http.Handler {
		return runtime.SessionMiddleware(auth.IdentityMiddleware(runtime.DB.W, next, liveEvents.Publish))
	}
	routes := serveRealtime(identified(httpx.Recover(slog.Default())(liveEvents)), identified(api))
	return &Application{Runtime: runtime, realtime: liveEvents, api: serveWebDAV(davHandler, routes), searchIndexer: searchIndexer, aiHandler: aiHandler, photos: photosHandler, imports: importsHandler, handoff: handoffHandler, signing: signingHandler, backupRunner: backupRunner, maintenanceRunner: maintenanceRunner, storageClassMigrator: storageClassMigrator}, nil
}

// serveWebDAV sends WebDAV requests past the API middleware: they have their
// own sign-in, stream their bodies and use methods CORS would answer itself.
func serveWebDAV(dav, api http.Handler) http.Handler {
	recovered := httpx.Recover(slog.Default())(dav)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if webdav.Serves(r) {
			recovered.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}

// adminRequest reports whether the response may carry technical error detail.
func adminRequest(db *sql.DB) func(*http.Request) bool {
	return func(r *http.Request) bool {
		principal, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			return false
		}
		var admin bool
		err := db.QueryRowContext(r.Context(), `SELECT is_admin=1 FROM users WHERE id=? AND disabled_at IS NULL`, principal.UserID).Scan(&admin)
		return err == nil && admin
	}
}

func (a *Application) Handler() http.Handler { return a.api }

func (a *Application) Close() error {
	if a == nil {
		return nil
	}
	a.stopWorkers()
	if a.realtime != nil {
		a.realtime.Close()
	}
	if a.backupRunner != nil {
		a.backupRunner.Close()
	}
	if a.storageClassMigrator != nil {
		a.storageClassMigrator.Close()
	}
	return a.Runtime.Close()
}

func serverBuildVersion() string {
	return buildinfo.Version
}

func publicOriginResolver(configuredURL string) func(*http.Request) string {
	return func(r *http.Request) string {
		if configured := strings.TrimSpace(configuredURL); configured != "" {
			parsed, err := url.Parse(configured)
			if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
				return ""
			}
			return parsed.Scheme + "://" + parsed.Host
		}
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https") {
			scheme = "https"
		}
		parsed, err := url.Parse(scheme + "://" + r.Host)
		if err != nil || parsed.Host == "" || parsed.User != nil {
			return ""
		}
		if port := parsed.Port(); port != "" {
			value, portErr := strconv.Atoi(port)
			if portErr != nil || value < 1 || value > 65535 {
				return ""
			}
		}
		return parsed.Scheme + "://" + parsed.Host
	}
}
