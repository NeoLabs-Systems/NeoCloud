package manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
)

const (
	// pinEnvKey records the release tag an install was pinned to.
	pinEnvKey = "NEOCLOUD_PINNED_VERSION"
	// versionEnvKey asks the installer for one release instead of the newest.
	versionEnvKey = "NEOCLOUD_VERSION"
)

// installRequest is what `install`, `setup` and `repair` were asked to set.
type installRequest struct {
	channel, port, publicURL, tag string
	setPublicURL                  bool
}

func (m *Manager) parseInstall(command string, args []string) (installRequest, error) {
	var request installRequest
	var err error
	if request.channel, err = channelArg(command, args); err != nil {
		return request, err
	}
	if port, ok := cli.FlagValue(args, "--port"); ok {
		if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
			return request, inputError("--port must be a number between 1 and 65535.")
		}
		request.port = port
	}
	request.publicURL, request.setPublicURL = cli.FlagValue(args, "--public-url")
	return request, nil
}

// saveSettings writes what the command line asked for into .env.
func (m *Manager) saveSettings(request installRequest) error {
	if err := m.ensureEnv(); err != nil {
		return err
	}
	envFile := m.paths().EnvFile
	if request.port != "" {
		if err := envfile.Upsert(envFile, "PORT", request.port); err != nil {
			return err
		}
	}
	if request.setPublicURL {
		if err := envfile.Upsert(envFile, "PUBLIC_URL", strings.TrimSpace(request.publicURL)); err != nil {
			return err
		}
	}
	channel := request.channel
	if channel == "" {
		channel = m.channel()
	}
	if err := m.setChannel(channel); err != nil {
		return err
	}
	if request.tag != "" {
		return envfile.Upsert(envFile, pinEnvKey, request.tag)
	}
	return nil
}

// stageRuntime downloads the signed runtime an install without one needs, and
// returns its version without activating it. An app directory someone chose
// with NEOCLOUD_APP_DIR is used as it is.
func (m *Manager) stageRuntime(ctx context.Context, cfg config.Config, channel, tag string) (string, error) {
	store := RuntimeStore(cfg.Paths.Home)
	current, err := store.Current()
	if err == nil && (tag == "" || "v"+current.Version == "v"+strings.TrimPrefix(tag, "v")) {
		return "", nil
	}
	if tag == "" && m.Getenv("NEOCLOUD_APP_DIR") != "" && exists(filepath.Join(cfg.Paths.WebDir, "index.html")) {
		return "", nil
	}
	source, candidate, err := m.findRuntime(ctx, channel, tag)
	if err != nil {
		return "", err
	}
	err = m.UI.Step("Downloading the verified NeoCloud release", "Downloaded the verified NeoCloud release", func() error {
		archive, err := source.Download(ctx, store, candidate)
		if err != nil {
			return err
		}
		// A runtime with missing files cannot be replaced while it is active.
		if _, err := store.Current(); err != nil {
			if err := os.Remove(filepath.Join(store.Root, "current.json")); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return update.Unpack(store, candidate.Version, archive, false)
	})
	if err != nil {
		return "", err
	}
	return candidate.Version, nil
}

// expectedVersion is the version the installed server reports.
func (m *Manager) expectedVersion(home string) string {
	if _, version, err := activeRuntime(home); err == nil {
		return version
	}
	return m.Version
}

// migrateDatabase prepares the database, unless an installed runtime of another
// version than this binary serves it: that server does it itself, and this
// binary's schema may not be one it can read.
func (m *Manager) migrateDatabase(ctx context.Context) error {
	if _, version, err := activeRuntime(m.paths().Home); err == nil && version != m.built {
		return nil
	}
	runtime, err := m.openUnchecked(ctx)
	if err != nil {
		return err
	}
	return runtime.Close()
}

// switchOver makes the service run this install: it downloads the release
// first, then stops the old server, saves what is about to change and starts the
// new one. When anything fails after that, everything is put back and the old
// server started again.
func (m *Manager) switchOver(ctx context.Context, request installRequest) (systemService bool, err error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return false, err
	}
	legacy := m.legacyInstall(cfg)
	channel := request.channel
	if channel == "" {
		channel = m.channel()
	}
	if err := cfg.Paths.Ensure(); err != nil {
		return false, err
	}
	// Nothing about the running install changes until the release is here.
	staged, err := m.stageRuntime(ctx, cfg, channel, request.tag)
	if err != nil {
		return false, err
	}
	snap, err := m.snapshotInstall(cfg, legacy)
	if err != nil {
		return false, err
	}
	if legacy.NeedsMigration {
		m.UI.Info("Detected an existing NeoCloud installation; keeping your files, keys and settings.")
	}
	step := func(doing, done string, run func() error) cli.Step {
		return cli.Step{Label: cli.Label{Doing: doing, Done: done}, Run: run}
	}
	err = m.UI.RunSteps(true,
		step("Saving your settings and data", "Saved your settings and data", func() error {
			if _, err := m.stopAndWait(ctx, cfg); err != nil {
				return err
			}
			if cli.PortOpen(cfg.Port) {
				return fmt.Errorf("Something is still using port %d. Stop it and run the command again.", cfg.Port)
			}
			if !exists(cfg.Paths.DatabaseFile) || cfg.MasterKeyHex == "" && !config.HasEncryptedData(cfg.Paths.DatabaseFile, cfg.Paths.BlobDir) {
				return nil
			}
			saved, err := m.snapshotDatabase(ctx, cfg)
			if err != nil {
				return fmt.Errorf("back up the database: %w", err)
			}
			snap.database = &saved
			return nil
		}),
		step("Preparing NeoCloud's files", "Prepared NeoCloud's files", func() error {
			if err := m.saveSettings(request); err != nil {
				return err
			}
			if staged != "" {
				return RuntimeStore(cfg.Paths.Home).Activate(staged)
			}
			return nil
		}),
		step("Updating the database", "Updated the database", func() error {
			return m.migrateDatabase(ctx)
		}),
		step("Adding the neocloud command", "Added the neocloud command", func() error {
			m.installCommand(legacy.NPMLink)
			return nil
		}),
		step("Starting NeoCloud", "Started NeoCloud", func() error {
			if cfg, err = m.loadConfig(); err != nil {
				return err
			}
			if !exists(filepath.Join(cfg.Paths.WebDir, "index.html")) {
				return fmt.Errorf("The web app is missing from %s.", cfg.Paths.AppDir)
			}
			systemService, err = m.installService(cfg)
			return err
		}),
		step("Checking it's answering", "It's answering", func() error {
			return m.waitForServer(ctx, cfg.Port, m.expectedVersion(cfg.Paths.Home), m.startupTimeout)
		}),
	)
	if err != nil {
		m.UI.Warn("Putting everything back the way it was")
		if restoreErr := m.restoreInstall(snap); restoreErr != nil {
			hint := ""
			if snap.database != nil {
				hint = " Your database from before is in " + snap.database.Path + "."
			}
			return false, fmt.Errorf("%w\nPutting it back also failed: %w.%s", err, restoreErr, hint)
		}
		return false, err
	}
	m.Version = m.expectedVersion(cfg.Paths.Home)
	return systemService, nil
}

func (m *Manager) cmdInstall(ctx context.Context, args []string) error {
	request, err := m.parseInstall("install", args)
	if err != nil {
		return err
	}
	request.tag = strings.TrimSpace(m.Getenv(versionEnvKey))
	if m.inContainer() {
		m.UI.OK("Container detected: starting the server in the foreground")
		return m.serve(ctx)
	}
	m.UI.Title("Welcome to NeoCloud", "Let's get it running on this computer. It can take a few minutes.")
	m.UI.Blank()
	if _, err := m.switchOver(ctx, request); err != nil {
		return err
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	m.UI.Blank()
	if m.hasAccount(ctx) {
		m.UI.Item("ok", m.UI.Bold("Done. Open "+address(cfg)+" and sign in."), "")
	} else {
		m.UI.Item("ok", m.UI.Bold("Done. Open "+address(cfg)+" and create the first account (it becomes the admin)."), "")
	}
	m.UI.Warn("Back up NEOCLOUD_MASTER_KEY (`neocloud keys export`). Without it, stored files cannot be decrypted.")
	m.UI.Blank()
	return nil
}

func (m *Manager) cmdSetup(ctx context.Context, args []string) error {
	request, err := m.parseInstall("setup", args)
	if err != nil {
		return err
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if m.legacyInstall(cfg).NeedsMigration {
		return errUpgradeFirst
	}
	m.UI.Title("NeoCloud setup", "")
	if err := m.saveSettings(request); err != nil {
		return err
	}
	if !m.inContainer() {
		staged, err := m.stageRuntime(ctx, cfg, m.channel(), "")
		if err != nil {
			return err
		}
		if staged != "" {
			if err := RuntimeStore(cfg.Paths.Home).Activate(staged); err != nil {
				return err
			}
		}
	}
	if err := m.migrateDatabase(ctx); err != nil {
		return err
	}
	m.UI.OK("Configuration: " + cfg.Paths.EnvFile)
	m.UI.OK("Data:          " + cfg.Paths.DataDir)
	m.UI.OK("File storage:  " + cfg.Paths.StorageDir)
	m.UI.Warn("Back up NEOCLOUD_MASTER_KEY (`neocloud keys export`). Without it, stored files cannot be decrypted.")
	m.UI.Info("Next: " + m.UI.Command("neocloud start"))
	return nil
}

// cmdRepair downloads what is missing, reinstalls the service and restarts.
func (m *Manager) cmdRepair(ctx context.Context, _ []string) error {
	m.UI.Title("Repairing NeoCloud", "Your data stays where it is. This can take a few minutes.")
	m.UI.Blank()
	if m.inContainer() {
		if err := m.ensureEnv(); err != nil {
			return err
		}
		if err := m.migrateDatabase(ctx); err != nil {
			return err
		}
		m.UI.OK("Repair complete. Restart the container to pick up the changes.")
		return nil
	}
	if _, err := m.switchOver(ctx, installRequest{}); err != nil {
		return err
	}
	m.UI.Blank()
	m.UI.OK("Repair complete.")
	return nil
}
