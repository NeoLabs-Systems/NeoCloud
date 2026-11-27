// Package manager implements the neocloud command: install, service control,
// status and diagnostics, settings, admins, backups, updates and the upgrade of
// a Node.js installation.
//
// Terminal work, service definitions, release channels, verified downloads and
// the update steps come from neolabs-go.
package manager

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neocloud/internal/updates"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

// The server finishes running uploads and downloads for up to drainTimeout
// when asked to stop; the service managers wait a little longer.
const (
	DrainTimeout = 30 * time.Second
	stopTimeout  = DrainTimeout + 10*time.Second
)

// Options are what the binary gives the manager: the one thing it cannot do
// itself is run the server.
type Options struct {
	Serve func(ctx context.Context) error
}

// Manager is one run of the CLI over one installation.
type Manager struct {
	Getenv   func(string) string
	UserHome string
	// Version is what this install runs; built is this binary's own.
	Version, built string
	Out            io.Writer
	UI             *cli.UI

	serve func(ctx context.Context) error
	// Tests replace these.
	releases       *release.Client
	publicKey      string
	newService     func(service.Spec) (*service.Manager, error)
	startupTimeout time.Duration
}

// New builds a manager over the process's own environment.
func New(opts Options) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	ui := cli.NewUI(false)
	ui.In, ui.Out, ui.Err = os.Stdin, os.Stdout, os.Stderr
	m := &Manager{
		Getenv: os.Getenv, UserHome: home, Version: buildinfo.Version, built: buildinfo.Version, Out: os.Stdout, UI: ui,
		serve: opts.Serve, startupTimeout: time.Minute,
		// Runtime archives are large; metadata requests carry their own deadline.
		releases: &release.Client{HTTP: ssrf.NewClient(ssrf.Options{Timeout: 30 * time.Minute}), UserAgent: appName + " CLI " + buildinfo.Version},
		newService: func(spec service.Spec) (*service.Manager, error) {
			platform, _ := update.Host()
			return service.New(spec, platform)
		},
	}
	if _, version, err := activeRuntime(m.paths().Home); err == nil {
		m.Version = version
	}
	return m, nil
}

// paths is the install's layout. On Linux the systemd unit loads .env, so the
// directories it names there are the server's.
func (m *Manager) paths() config.Paths {
	paths := config.ResolvePaths(m.Getenv, m.UserHome)
	if platform, _ := update.Host(); platform != "linux" {
		return paths
	}
	raw, err := os.ReadFile(paths.EnvFile)
	if err != nil {
		return paths
	}
	values := runtimecfg.ParseEnv(string(raw))
	return config.ResolvePaths(func(key string) string {
		if value := m.Getenv(key); value != "" {
			return value
		}
		return values[key]
	}, m.UserHome)
}

// loadConfig reads the install's settings the way the server does, pointed at
// the active runtime when one is installed.
func (m *Manager) loadConfig() (config.Config, error) {
	cfg, err := config.Load(m.environment(), m.UserHome)
	if err != nil {
		return cfg, err
	}
	if dir, _, err := activeRuntime(cfg.Paths.Home); err == nil {
		cfg.Paths.AppDir = runtimeAppDir(dir)
		cfg.Paths.WebDir, cfg.Paths.LandingDir = config.WebClientDir(cfg.Paths.AppDir), config.LandingDir(cfg.Paths.AppDir)
	}
	return cfg, nil
}

// openRuntime opens the install's database for one command, exactly as the
// server does on start.
func (m *Manager) openRuntime(ctx context.Context) (*runtimeapp.Runtime, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return nil, err
	}
	if m.legacyInstall(cfg).NeedsMigration {
		return nil, errUpgradeFirst
	}
	return m.openUnchecked(ctx)
}

// openUnchecked is openRuntime for the installer, which has already stopped a
// Node.js server and saved its database.
func (m *Manager) openUnchecked(ctx context.Context) (*runtimeapp.Runtime, error) {
	return runtimeapp.Open(ctx, m.environment(), m.UserHome)
}

// environment is the process environment with the install's directories
// resolved.
func (m *Manager) environment() func(string) string {
	paths := m.paths()
	return func(key string) string {
		switch key {
		case "NEOCLOUD_DATA_DIR":
			return paths.DataDir
		case "NEOCLOUD_STORAGE_DIR":
			return paths.StorageDir
		}
		return m.Getenv(key)
	}
}

func (m *Manager) channel() string { return updates.Channel(m.Getenv, m.paths().EnvFile) }

// setChannel records the release channel for this and later runs.
func (m *Manager) setChannel(channel string) error {
	if err := updates.SetChannel(m.paths().EnvFile, channel); err != nil {
		return err
	}
	return os.Setenv(updates.ChannelEnvKey, release.NormalizeChannel(channel))
}

// ensureEnv creates the install's directories, settings file and secrets. An
// existing value is never replaced.
func (m *Manager) ensureEnv() error {
	paths := m.paths()
	if err := paths.Ensure(); err != nil {
		return err
	}
	if _, err := config.EnsureSecrets(paths, m.Getenv, config.HasEncryptedData(paths.DatabaseFile, paths.BlobDir)); err != nil {
		return err
	}
	values, err := envfile.Load(paths.EnvFile)
	if err != nil {
		return err
	}
	if values["PORT"] == "" {
		port := m.Getenv("PORT")
		if port == "" {
			port = fmt.Sprint(defaultPort)
		}
		return envfile.Upsert(paths.EnvFile, "PORT", port)
	}
	return nil
}

func (m *Manager) port() int {
	if cfg, err := m.loadConfig(); err == nil {
		return cfg.Port
	}
	return defaultPort
}

// address is where people open the app.
func address(cfg config.Config) string {
	if cfg.PublicURL != "" {
		return cfg.PublicURL
	}
	return fmt.Sprintf("http://localhost:%d", cfg.Port)
}

func (m *Manager) inContainer() bool { return updates.InContainer(m.Getenv) }

// currentBinary is the binary the service runs: the active runtime's, or the
// one executing.
func (m *Manager) currentBinary() string {
	if dir, _, err := activeRuntime(m.paths().Home); err == nil {
		return runtimeBinary(dir)
	}
	if executable, err := os.Executable(); err == nil {
		return executable
	}
	return AppCommand
}

// serviceSpec describes the launchd agent, systemd user unit or detached
// process of this install.
func (m *Manager) serviceSpec(cfg config.Config, binary, appDir string) service.Spec {
	env := []service.EnvVar{
		{Key: "NEOCLOUD_HOME", Value: cfg.Paths.Home},
		{Key: "NEOCLOUD_APP_DIR", Value: appDir},
		{Key: "PATH", Value: servicePath(m.Getenv("PATH"), binary)},
	}
	for _, key := range []string{"NEOCLOUD_DATA_DIR", "NEOCLOUD_STORAGE_DIR", "NEOCLOUD_ENV_FILE"} {
		if value := m.Getenv(key); value != "" {
			if absolute, err := filepath.Abs(value); err == nil {
				env = append(env, service.EnvVar{Key: key, Value: absolute})
			}
		}
	}
	return service.Spec{
		Label: serviceLabel, Name: AppCommand, TaskName: appName,
		Description: "NeoCloud — self-hosted encrypted file storage",
		Program:     binary, Args: []string{"serve"}, WorkDir: appDir,
		Env: env, EnvFile: cfg.Paths.EnvFile,
		LogDir: cfg.Paths.LogDir, LogName: AppCommand, PIDFile: cfg.Paths.PIDFile,
		StopTimeout: stopTimeout,
	}
}

func (m *Manager) serviceManager(cfg config.Config) (*service.Manager, error) {
	return m.newService(m.serviceSpec(cfg, m.currentBinary(), cfg.Paths.AppDir))
}

// servicePath: services start with the bare PATH of launchd or systemd, so the
// usual install locations are appended.
func servicePath(current, binary string) string {
	seen := map[string]bool{}
	var dirs []string
	for _, dir := range append(filepath.SplitList(current), filepath.Dir(binary), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin") {
		if dir != "" && dir != "." && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// commands is the CLI surface.
func (m *Manager) commands() []cli.Command {
	return []cli.Command{
		{Name: "install", Run: m.cmdInstall},
		{Name: "setup", Run: m.cmdSetup},
		{Name: "start", Run: m.cmdStart},
		{Name: "stop", Run: m.cmdStop},
		{Name: "restart", Run: m.cmdRestart},
		{Name: "status", Run: m.cmdStatus},
		{Name: "doctor", Run: m.cmdDoctor},
		{Name: "logs", Run: m.cmdLogs},
		{Name: "env", Run: m.cmdEnv},
		{Name: "admin", Run: m.cmdAdmin},
		{Name: "backup", Run: m.cmdBackup},
		{Name: "keys", Run: m.cmdKeys},
		{Name: "update", Run: m.cmdUpdate},
		{Name: "rollback", Run: m.cmdRollback},
		{Name: "channel", Run: m.cmdChannel},
		{Name: "repair", Aliases: []string{"fix"}, Run: m.cmdRepair},
		{Name: "uninstall", Run: m.cmdUninstall},
		{Name: "serve", Aliases: []string{"run"}, Run: m.cmdServe},
		{Name: "healthcheck", Run: m.cmdHealthcheck},
		{Name: "version", Aliases: []string{"--version", "-V", "-v"}, Run: m.cmdVersion},
		{Name: "help", Aliases: []string{"--help", "-h"}, Run: m.cmdHelp},
	}
}

// Run runs one neocloud command, or the home screen when there is none.
func Run(ctx context.Context, args []string, opts Options) error {
	m, err := New(opts)
	if err != nil {
		return err
	}
	return m.Run(ctx, args)
}

// Run runs one command over this manager.
func (m *Manager) Run(ctx context.Context, args []string) error {
	args = withoutDetails(args)
	if len(args) == 0 {
		return m.cmdHome(ctx)
	}
	command := cli.Find(m.commands(), args[0])
	if command == nil {
		return inputError(fmt.Sprintf("Unknown command %q. Run `neocloud help`.", args[0]))
	}
	if helpRequested(command.Name, args[1:]) {
		return m.cmdHelp(ctx, nil)
	}
	if err := checkArgs(command.Name, args[1:]); err != nil {
		return err
	}
	return command.Run(ctx, args[1:])
}

func (m *Manager) cmdServe(ctx context.Context, _ []string) error { return m.serve(ctx) }
