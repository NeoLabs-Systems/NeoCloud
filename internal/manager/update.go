package manager

// `neocloud update` moves an install to the newest signed release of its
// channel; `neocloud rollback` goes back to the one before.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/backups"
	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/updates"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
)

// metadataTimeout bounds each look at the release list and manifest.
const metadataTimeout = time.Minute

func (m *Manager) runtimeSource() (update.Source, error) {
	key := m.publicKey
	if key == "" {
		var err error
		if key, err = RuntimePublicKey(); err != nil {
			return update.Source{}, err
		}
	}
	return update.Source{
		Client: m.releases, Repository: updates.Repo, ManifestPrefix: updates.ManifestPrefix,
		PublicKey: key, SchemaVersion: 1, Targets: SupportedTargets(),
	}, nil
}

// findRuntime is the verified runtime of a tag, or the newest of a channel, for
// this computer.
func (m *Manager) findRuntime(ctx context.Context, channel, tag string) (update.Source, update.Candidate, error) {
	source, err := m.runtimeSource()
	if err != nil {
		return source, update.Candidate{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	if tag == "" {
		candidate, err := source.Find(ctx, channel)
		return source, candidate, err
	}
	found, err := source.Client.ReleaseByTag(ctx, updates.Repo, tag, updates.ManifestPrefix)
	if err != nil {
		return source, update.Candidate{}, err
	}
	candidate, err := source.Select(ctx, found)
	return source, candidate, err
}

// updateState records what the last update changed, for rollback.
type updateState struct {
	Previous     string                 `json:"previous"`
	Version      string                 `json:"version"`
	Snapshot     *backups.LocalSnapshot `json:"snapshot"`
	UpdatedAt    string                 `json:"updatedAt"`
	RolledBackAt string                 `json:"rolledBackAt,omitempty"`
}

func writeUpdateState(cfg config.Config, state updateState) error {
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return envfile.WriteAtomic(cfg.Paths.UpdateStateFile, body, 0o600)
}

func pinned(cfg config.Config) string {
	values, err := envfile.Load(cfg.Paths.EnvFile)
	if err != nil {
		return ""
	}
	return values[pinEnvKey]
}

func (m *Manager) cmdUpdate(ctx context.Context, args []string) error {
	channel, err := channelArg("update", args)
	if err != nil {
		return err
	}
	tag, tagged := cli.FlagValue(args, "--tag")
	if tagged && tag == "" {
		return inputError("Usage: neocloud " + argSpecs["update"].usage)
	}
	if m.inContainer() {
		m.printContainerUpdate()
		return nil
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	requested := channel
	if channel == "" {
		channel = m.channel()
	}
	// Looking changes nothing, not even the saved channel.
	if cli.HasFlag(args, "--check") || cli.HasFlag(args, "--notes") {
		return m.printAvailable(ctx, channel, requested)
	}
	status := updates.StatusFile{Path: cfg.Paths.UpdateStatusFile}
	record := func(state, phase string, percent int, message, version string, rolledBack bool) {
		if err := status.Record(state, phase, percent, message, version, rolledBack); err != nil {
			m.UI.Warn("Could not record the update's progress: " + err.Error())
		}
	}
	fail := func(err error) error {
		record(updates.StateFailed, updates.StateFailed, 100, err.Error(), "", update.RolledBack(err))
		return err
	}
	if m.legacyInstall(cfg).NeedsMigration {
		if _, err := m.switchOver(ctx, installRequest{channel: requested, tag: tag}); err != nil {
			return fail(err)
		}
		return nil
	}
	if requested == "" && tag == "" && pinned(cfg) != "" {
		return fail(errors.New("This install is pinned to a version tag. Update with `neocloud update --tag <version>`."))
	}
	if err := m.ensureEnv(); err != nil {
		return fail(err)
	}
	if requested != "" {
		if err := m.setChannel(requested); err != nil {
			return fail(err)
		}
	}
	before := m.Version
	if _, current, err := activeRuntime(cfg.Paths.Home); err == nil {
		before = current
	}
	record(updates.StateRunning, "checking", 10, "Checking the latest "+channel+" release on GitHub", "", false)
	source, candidate, err := m.findRuntime(ctx, channel, tag)
	if err != nil {
		return fail(err)
	}
	// A named tag or channel may go back; the plain command only goes forward.
	order := release.CompareVersions(candidate.Version, before)
	wanted := order > 0 || order != 0 && (tag != "" || requested != "")
	if wanted {
		if err := m.applyUpdate(ctx, source, candidate, before, channel, record); err != nil {
			return fail(err)
		}
		m.Version = candidate.Version
	}
	// A new pin, or leaving one for a channel.
	if tag != "" {
		err = envfile.Upsert(cfg.Paths.EnvFile, pinEnvKey, "v"+candidate.Version)
	} else {
		_, err = envfile.Remove(cfg.Paths.EnvFile, pinEnvKey)
	}
	if err != nil {
		return fail(err)
	}
	if !wanted {
		record(updates.StateCompleted, updates.StateCompleted, 100, "Already up to date.", before, false)
		update.UpToDate(m.UI, before, channel)
		return nil
	}
	record(updates.StateCompleted, updates.StateCompleted, 100, "Updated NeoCloud to "+candidate.Version+".", candidate.Version, false)
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold(fmt.Sprintf("Updated %s → %s.", before, candidate.Version)), address(cfg))
	m.UI.Note("Undo with " + m.UI.Command("neocloud rollback") + ".")
	m.UI.Blank()
	return nil
}

// applyUpdate installs and activates candidate, restarts the service on it and
// goes back when it does not answer.
func (m *Manager) applyUpdate(ctx context.Context, source update.Source, candidate update.Candidate, before, channel string,
	record func(state, phase string, percent int, message, version string, rolledBack bool)) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	store := RuntimeStore(cfg.Paths.Home)
	previous := ""
	if current, err := store.Current(); err == nil {
		previous = current.Version
	}
	snap, err := m.snapshotInstall(cfg, legacyState{})
	if err != nil {
		return err
	}
	sm := snap.service
	installed := sm.Installed()
	runs := installed || snap.wasRunning
	oldSpec := sm.Spec
	oldSpec.Env = append([]service.EnvVar(nil), sm.Spec.Env...)
	dir, err := store.RuntimeDir(candidate.Version)
	if err != nil {
		return err
	}
	start := func(install bool) error {
		switch {
		case !runs:
			return nil
		case !installed:
			return sm.StartFallback()
		case install:
			return sm.Install()
		}
		return sm.Launch()
	}
	stop := func(ctx context.Context) error {
		if !runs {
			return nil
		}
		_, err := m.stopAndWait(ctx, cfg)
		return err
	}
	var takeBackup func(context.Context) error
	if exists(cfg.Paths.DatabaseFile) {
		takeBackup = func(ctx context.Context) error {
			saved, err := m.snapshotDatabase(ctx, cfg)
			snap.database = &saved
			return err
		}
	}
	var archive string
	err = update.Apply(ctx, m.UI, update.Plan{
		App: appName, From: before, To: candidate.Version, Channel: channel,
		Backup: takeBackup,
		Progress: func(phase string, percent int, message string) {
			record(updates.StateRunning, phase, percent, message, "", false)
		},
		Download: func(ctx context.Context) (err error) {
			archive, err = source.Download(ctx, store, candidate)
			return err
		},
		Install: func(context.Context) error { return update.Unpack(store, candidate.Version, archive, false) },
		Restart: func(ctx context.Context) error {
			if err := writeUpdateState(cfg, updateState{
				Previous: previous, Version: candidate.Version, Snapshot: snap.database,
				UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
				return err
			}
			if err := stop(ctx); err != nil {
				return err
			}
			if err := store.Activate(candidate.Version); err != nil {
				return err
			}
			sm.Spec = m.serviceSpec(cfg, runtimeBinary(dir), runtimeAppDir(dir))
			return start(true)
		},
		Verify: func(ctx context.Context) error {
			if !runs {
				return nil
			}
			return m.waitForServer(ctx, cfg.Port, candidate.Version, m.startupTimeout)
		},
		RollbackTimeout: m.startupTimeout + time.Minute,
		Rollback: func(ctx context.Context) error {
			if err := stop(ctx); err != nil {
				return err
			}
			if snap.database != nil {
				if err := m.restoreDatabaseIfChanged(ctx, cfg, *snap.database); err != nil {
					return err
				}
			}
			if err := restoreFiles(m.markerPath(cfg), snap); err != nil {
				return err
			}
			if err := os.Remove(cfg.Paths.UpdateStateFile); err != nil && !os.IsNotExist(err) {
				return err
			}
			sm.Spec = oldSpec
			// Without a saved service file the old definition is written again.
			if err := start(snap.unit == nil); err != nil {
				return err
			}
			if !runs {
				return nil
			}
			return m.waitForServer(ctx, cfg.Port, "", m.startupTimeout)
		},
	})
	if err != nil {
		return err
	}
	m.refreshLauncher()
	if err := store.Prune(candidate.Version, previous); err != nil {
		m.UI.Warn("Old runtime cleanup failed: " + err.Error())
	}
	return nil
}

// restoreFiles puts the runtime marker and the service definition back.
func restoreFiles(markerPath string, snap installSnapshot) error {
	var result error
	if snap.marker != nil {
		result = envfile.WriteAtomic(markerPath, snap.marker, 0o600)
	} else if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		result = err
	}
	if snap.unit != nil {
		result = errors.Join(result, envfile.WriteAtomic(unitPath(snap.service), snap.unit, 0o644))
	}
	return result
}

func (m *Manager) cmdRollback(ctx context.Context, _ []string) error {
	if m.inContainer() {
		m.printContainerUpdate()
		return nil
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	var state updateState
	raw, err := os.ReadFile(cfg.Paths.UpdateStateFile)
	if err != nil || json.Unmarshal(raw, &state) != nil || state.Previous == "" {
		return errors.New("No update to roll back: `neocloud update` records one before it switches versions.")
	}
	if state.RolledBackAt != "" {
		return fmt.Errorf("The last update was already rolled back (%s).", state.RolledBackAt)
	}
	store := RuntimeStore(cfg.Paths.Home)
	dir, err := store.RuntimeDir(state.Previous)
	if err == nil {
		err = store.Validate(state.Previous)
	}
	if err != nil {
		return fmt.Errorf("NeoCloud %s is no longer installed: %w", state.Previous, err)
	}
	m.UI.Title("Rolling back to "+state.Previous, "")
	if _, err := m.stopAndWait(ctx, cfg); err != nil {
		return err
	}
	if state.Snapshot != nil {
		current, err := schemaVersion(ctx, cfg.Paths.DatabaseFile)
		if err != nil || current != state.Snapshot.SchemaVersion {
			m.UI.Info("The update changed the database schema; restoring the snapshot taken before it.")
		} else {
			m.UI.Info("Database schema unchanged; keeping the current data.")
		}
		if err := m.restoreDatabaseIfChanged(ctx, cfg, *state.Snapshot); err != nil {
			return err
		}
	}
	if err := store.Activate(state.Previous); err != nil {
		return err
	}
	state.RolledBackAt = time.Now().UTC().Format(time.RFC3339)
	if err := writeUpdateState(cfg, state); err != nil {
		return err
	}
	sm, err := m.newService(m.serviceSpec(cfg, runtimeBinary(dir), runtimeAppDir(dir)))
	if err != nil {
		return err
	}
	if err := sm.Install(); err != nil {
		return err
	}
	m.refreshLauncher()
	if err := m.waitForServer(ctx, cfg.Port, state.Previous, m.startupTimeout); err != nil {
		return fmt.Errorf("Rolled back, but the server does not answer. See `neocloud logs`: %w", err)
	}
	m.UI.OK("Rolled back and running.")
	return nil
}

// printAvailable is `update --check`: what is waiting.
func (m *Manager) printAvailable(ctx context.Context, channel, requested string) error {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	latest, err := updates.Latest(ctx, m.releases, channel)
	if err != nil {
		return err
	}
	order := release.CompareVersions(latest, m.Version)
	if order <= 0 && (requested == "" || order == 0) {
		update.UpToDate(m.UI, m.Version, channel)
		return nil
	}
	command := strings.TrimSpace("neocloud update " + requested)
	m.UI.Blank()
	m.UI.Item("warn", m.UI.Bold(latest+" is ready"), "you have "+m.Version)
	m.UI.Note(fmt.Sprintf("Get it with %s. What changed: https://github.com/%s/releases/tag/v%s", m.UI.Command(command), updates.Repo, latest))
	m.UI.Blank()
	return nil
}

func (m *Manager) printContainerUpdate() {
	m.UI.Title("NeoCloud runs from a container image", "Update the image, not the code inside it:")
	m.UI.Blank()
	m.UI.Print("  " + m.UI.Command("docker compose pull && docker compose up -d"))
	m.UI.Blank()
	m.UI.Note("To go back, set the previous NEOCLOUD_VERSION and run " + m.UI.Command("docker compose up -d") + ".")
	m.UI.Note("Data on the " + filepath.Clean(m.paths().Home) + " volume survives the update.")
	m.UI.Blank()
}

func (m *Manager) cmdChannel(_ context.Context, args []string) error {
	next, err := channelArg("channel", args)
	if err != nil {
		return err
	}
	if next == "" {
		m.UI.Blank()
		m.UI.Item("info", fmt.Sprintf("Updates come from the %s channel.", m.channel()), "")
		m.UI.Note(fmt.Sprintf("Switch with %s or %s.", m.UI.Command("neocloud channel stable"), m.UI.Command("neocloud channel beta")))
		m.UI.Blank()
		return nil
	}
	if err := m.paths().Ensure(); err != nil {
		return err
	}
	if err := m.setChannel(next); err != nil {
		return err
	}
	if _, err := envfile.Remove(m.paths().EnvFile, pinEnvKey); err != nil {
		return err
	}
	m.UI.OK(fmt.Sprintf("Updates now come from the %s channel", next))
	m.UI.Note(fmt.Sprintf("Get it with %s.", m.UI.Command("neocloud update "+next)))
	return nil
}
