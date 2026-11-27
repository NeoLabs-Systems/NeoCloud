package config

import (
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
)

func TestLoadUsesNeoCloudPathsAndNodeEnvPrecedence(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "installation")
	envFile := filepath.Join(root, "service.env")
	appDir := filepath.Join(root, "release")
	if err := os.WriteFile(envFile, []byte("PORT=4455\nPUBLIC_URL=https://cloud.example\nSECURE_COOKIES=no\nALLOWED_ORIGINS=https://one.example, https://two.example\nNEOCLOUD_ALLOW_SIGNUP=no\nNEOCLOUD_DEFAULT_QUOTA=2.5GB\nNEOCLOUD_ADMIN_USERS=owner, service\nNEOCLOUD_APP_DIR="+appDir+"\nNEOCLOUD_OPENAI_BASE_URL=http://127.0.0.1:11434/v1\nNEOCLOUD_AI_ENABLED=true\nNEOCLOUD_AI_MONTHLY_TOKENS=12000\nNEOCLOUD_DISABLED_MODELS=openai::legacy, openrouter::blocked\nNEOCLOUD_EXTERNAL_ALLOW_PRIVATE=true\nNEOCLOUD_EXTERNAL_MAX_UPLOAD_BYTES=12345\nNEOCLOUD_IMPORT_ALLOW_PRIVATE=true\nNEOCLOUD_OFFICE_EDITOR_URL=https://office.example\nNEOCLOUD_OFFICE_HOST_URL=https://cloud.example\nNEOCLOUD_OFFICE_EDITOR_ENABLED=true\nNEOCLOUD_EMAIL_SMTP_HOST=mail.example\nNEOCLOUD_EMAIL_FROM=cloud@example.test\nNEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION=yes\nNEOCLOUD_EMAIL_PUBLIC_URL=https://mail.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"NEOCLOUD_HOME":        home,
		"NEOCLOUD_ENV_FILE":    envFile,
		"NEOCLOUD_STORAGE_DIR": filepath.Join(root, "large-volume"),
		"PORT":                 "3344",
		"PUBLIC_URL":           "http://ignored.example",
	}
	cfg, err := Load(func(key string) string { return values[key] }, root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 4455 || cfg.PublicURL != "https://cloud.example" || cfg.SecureCookies {
		t.Fatalf(".env precedence or cookie settings wrong: %+v", cfg)
	}
	if cfg.TrustProxy != "false" {
		t.Fatalf("TRUST_PROXY default = %q, want false", cfg.TrustProxy)
	}
	if len(cfg.Origins) != 2 || cfg.Origins[1] != "https://two.example" {
		t.Fatalf("origins = %#v", cfg.Origins)
	}
	if cfg.AllowSignup || cfg.DefaultQuotaBytes != 2684354560 || len(cfg.ReservedAdminNames) != 2 || cfg.ReservedAdminNames[1] != "service" {
		t.Fatalf("account/storage configuration = %+v", cfg)
	}
	if !cfg.Email.Enabled || !cfg.Email.RequireSignupConfirmation || cfg.Email.PublicURL != "https://mail.example" {
		t.Fatalf("shared service-mail config = %+v", cfg.Email)
	}
	if cfg.Paths.Home != home || cfg.Paths.DatabaseFile != filepath.Join(home, "data", "neocloud.db") || cfg.Paths.BlobDir != filepath.Join(root, "large-volume", "blobs") || cfg.Paths.WebDir != filepath.Join(appDir, "flutter_app", "build", "web") || cfg.Paths.LandingDir != filepath.Join(appDir, "landing") {
		t.Fatalf("resolved paths = %+v", cfg.Paths)
	}
	if cfg.AIProviderBaseURLs["openai"] != "http://127.0.0.1:11434/v1" {
		t.Fatalf("operator AI provider URL = %#v", cfg.AIProviderBaseURLs)
	}
	if !cfg.AIEnabled || cfg.AIMonthlyTokens != 12000 || len(cfg.DisabledModels) != 2 || !cfg.ExternalAllowPrivate || !cfg.ImportAllowPrivate {
		t.Fatalf("AI/external-storage policy config = %+v", cfg)
	}
	if cfg.ExternalMaxUploadBytes != 12345 || cfg.MaxUploadBytes != 0 {
		t.Fatalf("external/account upload limits = %d/%d", cfg.ExternalMaxUploadBytes, cfg.MaxUploadBytes)
	}
	if cfg.MinFreeDiskBytes != 1<<30 {
		t.Fatalf("minimum free disk = %d, want 1 GiB", cfg.MinFreeDiskBytes)
	}
	if !cfg.OfficeEditorEnabled || cfg.OfficeEditorURL != "https://office.example" || cfg.OfficeHostURL != "https://cloud.example" {
		t.Fatalf("office editor configuration = %+v", cfg)
	}
	if defaultCfg, err := Load(func(string) string { return "" }, root); err != nil || defaultCfg.Port != DefaultPort || !defaultCfg.AllowSignup || defaultCfg.DefaultQuotaBytes != defaultQuotaBytes || defaultCfg.ExternalMaxUploadBytes != 20<<30 {
		t.Fatalf("default port = %d, error = %v", defaultCfg.Port, err)
	}
}

func TestMinimumFreeDiskConfigurationMatchesNodeByteSizes(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int64
	}{
		{value: "0", want: 0},
		{value: "512 MB", want: 512 << 20},
		{value: "invalid", want: defaultMinFreeDiskBytes},
	} {
		got := minFreeDiskBytes(test.value)
		if got != test.want {
			t.Errorf("minFreeDiskBytes(%q)=%d, want %d", test.value, got, test.want)
		}
	}
}

func TestEnsureSecretsGeneratesOnceAndPreservesExistingEnv(t *testing.T) {
	root := t.TempDir()
	paths := testPaths(root)
	if err := os.WriteFile(paths.EnvFile, []byte("# operator settings\nPUBLIC_URL=https://cloud.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := EnsureSecrets(paths, func(string) string { return "" }, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MasterKeyHex) != 64 || !validMasterKey(cfg.MasterKeyHex) || len(cfg.SessionSecret) != 64 {
		t.Fatalf("invalid generated secrets: key length=%d session length=%d", len(cfg.MasterKeyHex), len(cfg.SessionSecret))
	}
	contents, err := os.ReadFile(paths.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "# operator settings\n") || !strings.Contains(string(contents), "PUBLIC_URL=https://cloud.example\n") {
		t.Fatalf("existing env content was not preserved: %s", contents)
	}
	if !strings.Contains(string(contents), "NEOCLOUD_MASTER_KEY="+cfg.MasterKeyHex) || !strings.Contains(string(contents), "SESSION_SECRET="+cfg.SessionSecret) {
		t.Fatal("generated secrets were not persisted")
	}
	info, err := os.Stat(paths.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("env file mode = %o, want 600", info.Mode().Perm())
	}
	first := cfg
	second, err := EnsureSecrets(paths, func(string) string { return "" }, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.MasterKeyHex != first.MasterKeyHex || second.SessionSecret != first.SessionSecret {
		t.Fatal("restart replaced an existing secret")
	}
	for _, dir := range []string{paths.Home, paths.DataDir, paths.BlobDir} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("private directory %s mode=%v err=%v", dir, info, err)
		}
	}
}

func TestExternalUploadLimitMatchesNumericByteConfiguration(t *testing.T) {
	tests := []struct {
		value string
		want  int64
	}{
		{"", 20 << 30},
		{"4096", 4096},
		{"4096.9", 4096},
		{"0", 20 << 30},
		{"-1", 20 << 30},
		{"20GB", 20 << 30},
		{"Infinity", 20 << 30},
	}
	for _, tt := range tests {
		if got := externalUploadLimit(tt.value); got != tt.want {
			t.Errorf("externalUploadLimit(%q)=%d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestEnsureSecretsRefusesKeyReplacementWhenDataExists(t *testing.T) {
	root := t.TempDir()
	paths := testPaths(root)
	if err := os.WriteFile(paths.EnvFile, []byte("SESSION_SECRET=original-session\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSecrets(paths, func(string) string { return "" }, true); err == nil || !strings.Contains(err.Error(), "already holds encrypted data") {
		t.Fatalf("missing master key error = %v", err)
	}
	contents, err := os.ReadFile(paths.EnvFile)
	if err != nil || string(contents) != "SESSION_SECRET=original-session\n" {
		t.Fatalf("refused startup changed env file: %q err=%v", contents, err)
	}
	if _, err := EnsureSecrets(paths, func(key string) string {
		if key == "NEOCLOUD_MASTER_KEY" {
			return strings.Repeat("z", 64)
		}
		return ""
	}, false); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("invalid master key error = %v", err)
	}
}

func TestHasEncryptedDataProtectsExistingInstall(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "neocloud.db")
	blobDir := filepath.Join(root, "blobs")
	if HasEncryptedData(dbPath, blobDir) {
		t.Fatal("missing install was reported as having data")
	}
	if err := os.WriteFile(dbPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !HasEncryptedData(dbPath, blobDir) {
		t.Fatal("unreadable database must protect the existing key")
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY); INSERT INTO users VALUES (1)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if !HasEncryptedData(dbPath, blobDir) {
		t.Fatal("account row was not detected")
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(blobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobDir, "existing.blob"), []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if !HasEncryptedData(dbPath, blobDir) {
		t.Fatal("blob was not detected")
	}
}

func TestMasterKeyValidation(t *testing.T) {
	if !validMasterKey(hex.EncodeToString(make([]byte, 32))) || validMasterKey("abcd") {
		t.Fatal("master-key validation mismatch")
	}
	if !IsLoopbackOrigin("http://[::1]:3344") || IsLoopbackOrigin("https://example.com") {
		t.Fatal("loopback origin detection mismatch")
	}
	if !runtimecfg.Bool("yes", false) {
		t.Fatal("shared runtime boolean parsing mismatch")
	}
}

func testPaths(root string) Paths {
	data := filepath.Join(root, "data")
	storage := filepath.Join(data, "storage")
	return Paths{
		Home:         root,
		DataDir:      data,
		LogDir:       filepath.Join(data, "logs"),
		EnvFile:      filepath.Join(root, ".env"),
		DatabaseFile: filepath.Join(data, "neocloud.db"),
		PIDFile:      filepath.Join(data, "neocloud.pid"),
		BackupDir:    filepath.Join(data, "backups"),
		StateDir:     filepath.Join(data, "state"),
		StorageDir:   storage,
		BlobDir:      filepath.Join(storage, "blobs"),
		UploadTmpDir: filepath.Join(storage, "tmp"),
		PreviewDir:   filepath.Join(storage, "previews"),
	}
}
