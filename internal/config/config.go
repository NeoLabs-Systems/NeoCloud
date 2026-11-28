package config

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
	_ "modernc.org/sqlite"
)

const DefaultPort = 3344

const defaultQuotaBytes = int64(10 * 1024 * 1024 * 1024)
const defaultMinFreeDiskBytes = int64(1024 * 1024 * 1024)

var byteSizePattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(B|KB|MB|GB|TB|K|M|G|T)?I?B?$`)

var sessionSecretPlaceholders = map[string]struct{}{
	"change-me": {},
	"change-this-to-a-random-secret-in-production": {},
}

func falseSetting(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0", "no", "off":
		return true
	default:
		return false
	}
}

func splitComma(value string) []string {
	parts := strings.Split(value, ",")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			clean = append(clean, part)
		}
	}
	return clean
}

func envExists(getenv func(string) string, key string) bool {
	if _, ok := os.LookupEnv(key); ok {
		return true
	}
	return getenv != nil && getenv(key) != ""
}

func hasEnvFileValue(values map[string]string, key string) bool {
	_, ok := values[key]
	return ok
}

func nonNegativeInt64(value string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func readDefaultQuota(value string) int64 {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "unlimited" || raw == "-1" {
		return -1
	}
	if raw == "" {
		return defaultQuotaBytes
	}
	parsed := byteSizePattern.FindStringSubmatch(strings.ToUpper(raw))
	if parsed == nil {
		return defaultQuotaBytes
	}
	amount, err := strconv.ParseFloat(parsed[1], 64)
	if err != nil {
		return defaultQuotaBytes
	}
	unit := parsed[2]
	if len(unit) == 1 && unit != "B" {
		unit += "B"
	}
	multiplier := float64(1)
	switch unit {
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "", "B":
	default:
		return defaultQuotaBytes
	}
	bytes := math.Floor(amount * multiplier)
	if bytes < 0 || bytes > 9007199254740991 {
		return defaultQuotaBytes
	}
	return int64(bytes)
}

// Paths is the resolved filesystem layout for one NeoCloud installation.
type Paths struct {
	Home         string
	DataDir      string
	LogDir       string
	EnvFile      string
	DatabaseFile string
	PIDFile      string
	BackupDir    string
	StateDir     string
	StorageDir   string
	BlobDir      string
	UploadTmpDir string
	PreviewDir   string
	// UpdateStatusFile is what the update runner and the admin UI talk through;
	// UpdateStateFile records what the last update changed, for rollback.
	UpdateStatusFile string
	UpdateStateFile  string
	// AppDir holds what a release ships beside the binary: the web client and
	// the landing page.
	AppDir     string
	WebDir     string
	LandingDir string
}

// Config contains process settings read from the environment and install env file.
type Config struct {
	Paths                         Paths
	Port                          int
	PublicURL                     string
	SecureCookies                 bool
	TrustProxy                    string
	Origins                       []string
	MasterKeyHex                  string
	SessionSecret                 string
	Email                         servicemail.Config
	SigningMails                  bool
	SigningSeal                   bool
	AllowSignup                   bool
	DefaultQuotaBytes             int64
	MinFreeDiskBytes              int64
	TrashRetentionDays            int
	MaxUploadBytes                int64
	ExternalMaxUploadBytes        int64
	VersionRetention              int
	MaxNodes                      int
	MaxCopyNodes                  int
	ReservedAdminNames            []string
	AIProviderBaseURLs            map[string]string
	AIProviderKeys                map[string]string
	DisabledModels                []string
	KnownModels                   []string
	KnownModelsConfigured         bool
	AutoEnableNewModels           bool
	AutoEnableNewModelsConfigured bool
	AIEnabled                     bool
	AIMonthlyTokens               int64
	AIReferenceURL                string
	NeoRouterEnv                  map[string]string
	ExternalAllowPrivate          bool
	ImportAllowPrivate            bool
	FullTextSearchEnabled         bool
	OfficePreviewEnabled          bool
	LibreOfficePath               string
	PreviewSandbox                bool
	OCREnabled                    bool
	OCRLanguages                  string
	OfficeEditorURL               string
	OfficeHostURL                 string
	OfficeEditorEnabled           bool
	CSPAllowExternal              bool
	HSTSMaxAgeSeconds             int
	LogLevel                      string
	Analytics                     bool
	// TrustProxyUnset records that TrustProxy is the default, not a setting.
	TrustProxyUnset bool
}

var envFileWriteMu sync.Mutex

// ReadEnv is the install env file as startup reads it; a missing file is empty.
func ReadEnv(path string) (map[string]string, error) {
	return readEnvFile(path)
}

// UpdateEnv persists environment values in the private install env file. An
// empty value is stored as an empty assignment, matching startup semantics.
// A value can never add a line: a line break would let it set other keys.
func UpdateEnv(path string, updates map[string]string) error {
	if path == "" || len(updates) == 0 {
		return errors.New("config: env file path and at least one update are required")
	}
	for key, value := range updates {
		if !envfile.ValidKey(key) {
			return fmt.Errorf("config: invalid environment variable name %q", key)
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("config: the value of %s contains a line break", key)
		}
	}
	envFileWriteMu.Lock()
	defer envFileWriteMu.Unlock()
	return writeEnvUpdates(path, updates)
}

// ResolvePaths is the layout the process environment selects; AppDir and the
// directories below it depend on .env too and are filled in by Load.
func ResolvePaths(getenv func(string) string, userHome string) Paths {
	base := runtimecfg.Resolve("NEOCLOUD", ".neocloud", getenv, userHome)
	storage := strings.TrimSpace(getenv("NEOCLOUD_STORAGE_DIR"))
	if storage == "" {
		storage = filepath.Join(base.DataDir, "storage")
	} else if absoluteStorage, err := filepath.Abs(storage); err == nil {
		storage = filepath.Clean(absoluteStorage)
	}
	stateDir := filepath.Join(base.DataDir, "state")
	return Paths{
		Home:             base.Home,
		DataDir:          base.DataDir,
		LogDir:           filepath.Join(base.DataDir, "logs"),
		EnvFile:          base.EnvFile,
		DatabaseFile:     filepath.Join(base.DataDir, "neocloud.db"),
		PIDFile:          filepath.Join(base.DataDir, "neocloud.pid"),
		BackupDir:        filepath.Join(base.DataDir, "backups"),
		StateDir:         stateDir,
		StorageDir:       storage,
		BlobDir:          filepath.Join(storage, "blobs"),
		UploadTmpDir:     filepath.Join(storage, "tmp"),
		PreviewDir:       filepath.Join(storage, "previews"),
		UpdateStatusFile: filepath.Join(stateDir, "update-status.json"),
		UpdateStateFile:  filepath.Join(stateDir, "last-update.json"),
	}
}

// WebClientDir is the built web client below an app directory.
func WebClientDir(appDir string) string { return filepath.Join(appDir, "flutter_app", "build", "web") }

// LandingDir is the landing page below an app directory.
func LandingDir(appDir string) string { return filepath.Join(appDir, "landing") }

// Load resolves paths from the real process environment, then loads service
// settings from .env, which wins over the process environment. `userHome` is
// injectable so paths can be tested without touching a real installation.
func Load(getenv func(string) string, userHome string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if userHome == "" {
		var err error
		userHome, err = os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("config: resolve home directory: %w", err)
		}
	}
	paths := ResolvePaths(getenv, userHome)
	fileEnv, err := readEnvFile(paths.EnvFile)
	if err != nil {
		return Config{}, err
	}
	get := func(key string) string {
		if value, ok := fileEnv[key]; ok {
			return value
		}
		return getenv(key)
	}
	appDir := strings.TrimSpace(get("NEOCLOUD_APP_DIR"))
	if appDir == "" {
		appDir = discoverAppDir()
	}
	if paths.AppDir, err = filepath.Abs(appDir); err != nil {
		return Config{}, fmt.Errorf("resolve NEOCLOUD_APP_DIR: %w", err)
	}
	paths.WebDir, paths.LandingDir = WebClientDir(paths.AppDir), LandingDir(paths.AppDir)
	port := DefaultPort
	if raw := strings.TrimSpace(get("PORT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return Config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
		port = parsed
	}
	publicURL := strings.TrimSpace(get("PUBLIC_URL"))
	secureDefault := strings.HasPrefix(strings.ToLower(publicURL), "https://")
	secureCookies := secureDefault
	if raw := strings.TrimSpace(get("SECURE_COOKIES")); raw != "" {
		secureCookies = truthy(raw)
	}
	var origins []string
	for _, origin := range strings.Split(get("ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	defaultMailURL := publicURL
	if defaultMailURL == "" {
		defaultMailURL = fmt.Sprintf("http://localhost:%d", port)
	}
	mailConfig := servicemail.LoadConfig("NEOCLOUD", get, "NeoCloud", defaultMailURL)
	// NeoCloud's name for the shared loader's unusual sign-in switch.
	mailConfig.NotifyUnusualLogin = !falseSetting(get("NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN"))
	reservedAdmins := make([]string, 0)
	for _, username := range strings.Split(get("NEOCLOUD_ADMIN_USERS"), ",") {
		if username = strings.TrimSpace(username); username != "" {
			reservedAdmins = append(reservedAdmins, username)
		}
	}
	return Config{
		Paths:                  paths,
		Port:                   port,
		PublicURL:              publicURL,
		SecureCookies:          secureCookies,
		CSPAllowExternal:       !falseSetting(get("NEOCLOUD_CSP_ALLOW_EXTERNAL")),
		HSTSMaxAgeSeconds:      nonNegativeInt(get("NEOCLOUD_HSTS_MAX_AGE"), 86400),
		TrustProxy:             trustProxy(get("TRUST_PROXY"), secureCookies),
		Origins:                origins,
		MasterKeyHex:           strings.TrimSpace(get("NEOCLOUD_MASTER_KEY")),
		SessionSecret:          strings.TrimSpace(get("SESSION_SECRET")),
		Email:                  mailConfig,
		AllowSignup:            !falseSetting(get("NEOCLOUD_ALLOW_SIGNUP")),
		SigningMails:           !falseSetting(get("NEOCLOUD_EMAIL_SIGNING_MAILS")),
		SigningSeal:            !falseSetting(get("NEOCLOUD_SIGNING_SEAL")),
		DefaultQuotaBytes:      readDefaultQuota(get("NEOCLOUD_DEFAULT_QUOTA")),
		MinFreeDiskBytes:       minFreeDiskBytes(get("NEOCLOUD_MIN_FREE_DISK")),
		TrashRetentionDays:     nonNegativeInt(get("NEOCLOUD_TRASH_RETENTION_DAYS"), 30),
		MaxUploadBytes:         readOptionalByteLimit(get("NEOCLOUD_MAX_UPLOAD_SIZE")),
		ExternalMaxUploadBytes: externalUploadLimit(get("NEOCLOUD_EXTERNAL_MAX_UPLOAD_BYTES")),
		VersionRetention:       nonNegativeInt(get("NEOCLOUD_VERSION_RETENTION"), 10),
		MaxNodes:               positiveInt(get("NEOCLOUD_MAX_NODES"), 1_000_000),
		MaxCopyNodes:           positiveInt(get("NEOCLOUD_MAX_COPY_NODES"), 50_000),
		ReservedAdminNames:     reservedAdmins,
		AIProviderBaseURLs: map[string]string{
			"openai":            strings.TrimSpace(get("NEOCLOUD_OPENAI_BASE_URL")),
			"openai-compatible": strings.TrimSpace(get("NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL")),
			"anthropic":         strings.TrimSpace(get("NEOCLOUD_ANTHROPIC_BASE_URL")),
			"ollama":            strings.TrimSpace(get("NEOCLOUD_OLLAMA_URL")),
		},
		AIProviderKeys: map[string]string{
			"openai":            strings.TrimSpace(get("NEOCLOUD_OPENAI_API_KEY")),
			"openai-compatible": strings.TrimSpace(get("NEOCLOUD_OPENAI_COMPATIBLE_API_KEY")),
			"anthropic":         strings.TrimSpace(get("NEOCLOUD_ANTHROPIC_API_KEY")),
			"google":            strings.TrimSpace(get("NEOCLOUD_GOOGLE_AI_KEY")),
			"openrouter":        strings.TrimSpace(get("NEOCLOUD_OPENROUTER_API_KEY")),
		},
		DisabledModels:                splitComma(get("NEOCLOUD_DISABLED_MODELS")),
		KnownModels:                   splitComma(get("NEOCLOUD_KNOWN_MODELS")),
		KnownModelsConfigured:         hasEnvFileValue(fileEnv, "NEOCLOUD_KNOWN_MODELS") || envExists(getenv, "NEOCLOUD_KNOWN_MODELS"),
		AutoEnableNewModels:           get("NEOCLOUD_AUTO_ENABLE_NEW_MODELS") != "false",
		AutoEnableNewModelsConfigured: hasEnvFileValue(fileEnv, "NEOCLOUD_AUTO_ENABLE_NEW_MODELS") || envExists(getenv, "NEOCLOUD_AUTO_ENABLE_NEW_MODELS"),
		AIEnabled:                     strings.EqualFold(strings.TrimSpace(get("NEOCLOUD_AI_ENABLED")), "true"),
		AIMonthlyTokens:               nonNegativeInt64(get("NEOCLOUD_AI_MONTHLY_TOKENS")),
		AIReferenceURL:                strings.TrimSpace(get("NEOCLOUD_AI_REFERENCE_URL")),
		NeoRouterEnv: map[string]string{
			"NEOROUTER_BASE_URL": strings.TrimSpace(get("NEOROUTER_BASE_URL")),
			"NEOROUTER_API_KEY":  strings.TrimSpace(get("NEOROUTER_API_KEY")),
			"NEOROUTER_ROLES":    strings.TrimSpace(get("NEOROUTER_ROLES")),
		},
		ExternalAllowPrivate:  strings.EqualFold(strings.TrimSpace(get("NEOCLOUD_EXTERNAL_ALLOW_PRIVATE")), "true"),
		ImportAllowPrivate:    strings.EqualFold(strings.TrimSpace(get("NEOCLOUD_IMPORT_ALLOW_PRIVATE")), "true"),
		FullTextSearchEnabled: strings.EqualFold(strings.TrimSpace(get("NEOCLOUD_FULL_TEXT_SEARCH")), "true"),
		OfficePreviewEnabled:  strings.TrimSpace(get("NEOCLOUD_OFFICE_PREVIEW")) == "" || truthy(get("NEOCLOUD_OFFICE_PREVIEW")),
		LibreOfficePath:       strings.TrimSpace(get("NEOCLOUD_LIBREOFFICE_PATH")),
		PreviewSandbox:        !strings.EqualFold(strings.TrimSpace(get("NEOCLOUD_PREVIEW_SANDBOX")), "off"),
		OCREnabled:            strings.TrimSpace(get("NEOCLOUD_OCR")) == "" || truthy(get("NEOCLOUD_OCR")),
		OCRLanguages:          strings.TrimSpace(get("NEOCLOUD_OCR_LANGUAGES")),
		OfficeEditorURL:       strings.TrimRight(strings.TrimSpace(get("NEOCLOUD_OFFICE_EDITOR_URL")), "/"),
		OfficeHostURL:         strings.TrimRight(strings.TrimSpace(get("NEOCLOUD_OFFICE_HOST_URL")), "/"),
		OfficeEditorEnabled:   strings.EqualFold(strings.TrimSpace(get("NEOCLOUD_OFFICE_EDITOR_ENABLED")), "true"),
		LogLevel:              logLevel(get("LOG_LEVEL")),
		Analytics:             !falseSetting(get("ANALYTICS_ENABLED")),
		TrustProxyUnset:       strings.TrimSpace(get("TRUST_PROXY")) == "",
	}, nil
}

// discoverAppDir finds the directory holding landing/: the one a release
// unpacks the binary into (app/bin), or a source checkout above the binary or
// the working directory.
func discoverAppDir() string {
	var roots []string
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		roots = append(roots, filepath.Dir(executable))
	}
	cwd, err := os.Getwd()
	if err == nil {
		roots = append(roots, cwd)
	}
	for _, root := range roots {
		for dir := filepath.Clean(root); ; dir = filepath.Dir(dir) {
			if info, err := os.Stat(filepath.Join(LandingDir(dir), "index.html")); err == nil && info.Mode().IsRegular() {
				return dir
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return cwd
}

func nonNegativeInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func positiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func readOptionalByteLimit(raw string) int64 {
	value, ok := parseByteSize(raw)
	if !ok {
		return 0
	}
	return value
}

func minFreeDiskBytes(raw string) int64 {
	if value, ok := parseByteSize(raw); ok {
		return value
	}
	return defaultMinFreeDiskBytes
}

func parseByteSize(raw string) (int64, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, false
	}
	parsed := byteSizePattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(raw)))
	if parsed == nil {
		return 0, false
	}
	amount, err := strconv.ParseFloat(parsed[1], 64)
	if err != nil {
		return 0, false
	}
	unit := parsed[2]
	if len(unit) == 1 && unit != "B" {
		unit += "B"
	}
	multiplier := float64(1)
	switch unit {
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "", "B":
	default:
		return 0, false
	}
	value := math.Floor(amount * multiplier)
	if value < 0 || value > 9007199254740991 {
		return 0, false
	}
	return int64(value), true
}

// Node's external-storage limit is configured independently from the account
// upload limit and is expressed as a numeric byte count.
func externalUploadLimit(raw string) int64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 20 << 30
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if value >= float64(maxInt64) {
		return maxInt64
	}
	return int64(value)
}

func logLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "info"
	}
}

func truthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// trustProxy preserves the Node service's default: private hops are trusted
// only when secure cookies are enabled. Explicit values are returned intact
// for the HTTP layer to validate and interpret.
func trustProxy(raw string, secureCookies bool) string {
	if value := strings.TrimSpace(raw); value != "" {
		return value
	}
	if secureCookies {
		return "loopback,linklocal,uniquelocal"
	}
	return "false"
}

// Ensure creates the app's directories with private permissions.
func (p Paths) Ensure() error {
	for _, dir := range []string{
		p.Home, p.DataDir, p.LogDir, p.BackupDir, p.StateDir,
		p.StorageDir, p.BlobDir, p.UploadTmpDir, p.PreviewDir,
	} {
		if err := runtimecfg.EnsurePrivateDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// EnsureSecrets initializes persistent secrets only on a fresh install. The
// caller must determine encryptedData from the database and blob store first.
// Process environment values take precedence here, matching the Node bootstrap
// performed before dotenv's later file-over-environment application.
func EnsureSecrets(paths Paths, getenv func(string) string, encryptedData bool) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if err := paths.Ensure(); err != nil {
		return Config{}, err
	}
	parsed, err := readEnvFile(paths.EnvFile)
	if err != nil {
		return Config{}, err
	}
	updates := make(map[string]string, 2)
	secret := strings.TrimSpace(getenv("SESSION_SECRET"))
	if secret == "" {
		secret = strings.TrimSpace(parsed["SESSION_SECRET"])
	}
	if _, placeholder := sessionSecretPlaceholders[secret]; secret == "" || placeholder {
		secret, err = randomHex(32)
		if err != nil {
			return Config{}, err
		}
		updates["SESSION_SECRET"] = secret
	}
	master := strings.TrimSpace(getenv("NEOCLOUD_MASTER_KEY"))
	if master == "" {
		master = strings.TrimSpace(parsed["NEOCLOUD_MASTER_KEY"])
	}
	if master == "" {
		if encryptedData {
			return Config{}, fmt.Errorf("NEOCLOUD_MASTER_KEY is missing, but this install already holds encrypted data. Restore the original key; a new key would make stored data unreadable, so none was created")
		}
		master, err = randomHex(32)
		if err != nil {
			return Config{}, err
		}
		updates["NEOCLOUD_MASTER_KEY"] = master
	} else if !validMasterKey(master) {
		return Config{}, fmt.Errorf("NEOCLOUD_MASTER_KEY is malformed. Restore the original 64-character key; stored data cannot be decrypted without it")
	}
	if len(updates) > 0 {
		if err := writeEnvUpdates(paths.EnvFile, updates); err != nil {
			return Config{}, err
		}
	}
	return Config{MasterKeyHex: master, SessionSecret: secret, Paths: paths}, nil
}

// HasEncryptedData reports whether the blob store or any account rows exist.
// If the database exists but cannot be read, it errs on the safe side and
// reports data present so bootstrap never generates a replacement key.
func HasEncryptedData(databaseFile, blobDir string) bool {
	entries, err := os.ReadDir(blobDir)
	if err == nil && len(entries) > 0 {
		return true
	}
	if _, err := os.Stat(databaseFile); errors.Is(err, os.ErrNotExist) {
		return false
	} else if err != nil {
		return true
	}
	dbURI := (&url.URL{Scheme: "file", Path: databaseFile}).String() + "?mode=ro"
	db, err := sql.Open("sqlite", dbURI)
	if err != nil {
		return true
	}
	defer db.Close()
	var exists int
	err = db.QueryRow(`SELECT 1 FROM users LIMIT 1`).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		return !strings.Contains(strings.ToLower(err.Error()), "no such table")
	}
	return true
}

func validMasterKey(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func randomHex(length int) (string, error) {
	value := make([]byte, length)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("config: generate secret: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func readEnvFile(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return runtimecfg.ParseEnv(string(raw)), nil
}

func writeEnvUpdates(path string, updates map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	for key, value := range updates {
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, key+"=") {
				lines[i] = key + "=" + value
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, key+"="+value)
		}
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: create env directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".env-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temporary env file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("config: secure temporary env file: %w", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write env file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: sync env file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close env file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: replace env file: %w", err)
	}
	if err := runtimecfg.EnsurePrivateFile(path); err != nil {
		return fmt.Errorf("config: secure env file: %w", err)
	}
	return nil
}

// IsLoopbackOrigin recognizes the local origins allowed by default by both
// NeoCloud servers. Other origins require an explicit ALLOWED_ORIGINS entry.
func IsLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	switch strings.Trim(u.Hostname(), "[]") {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
