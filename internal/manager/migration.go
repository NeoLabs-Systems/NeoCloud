package manager

// Upgrading a Node.js install to this binary: detect it, and while it is
// switched over keep everything needed to put it back — the database, the
// settings, the service definition, the `neocloud` command — until the new
// server answers.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/backups"
	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
)

// errUpgradeFirst is what a command says that cannot run beside the Node.js
// server.
var errUpgradeFirst = inputError("This computer still has the Node.js NeoCloud. Run `neocloud install` to upgrade it; your data is backed up first.")

// legacyState is what a Node.js installation left on this computer.
type legacyState struct {
	// NPMLink is the `neocloud` symlink npm put on the PATH.
	NPMLink string
	// Detached is the command line of a Node.js server running without a
	// service definition.
	Detached       string
	NeedsMigration bool
}

func runsNodeServer(definition string) bool { return strings.Contains(definition, "server/index.js") }

func findNPMLink(path string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, AppCommand)
		if target, err := os.Readlink(candidate); err == nil && strings.HasSuffix(filepath.ToSlash(target), "bin/neocloud.js") {
			return candidate
		}
	}
	return ""
}

func processCommand(pid int) string {
	if !processAlive(pid) {
		return ""
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// unitPath is the service definition file of this platform.
func unitPath(sm *service.Manager) string {
	switch sm.Platform {
	case "macos":
		return sm.PlistPath
	case "linux":
		return sm.UnitPath
	}
	return ""
}

// legacyInstall looks for a Node.js installation without changing anything. A
// container has no service or command to upgrade.
func (m *Manager) legacyInstall(cfg config.Config) legacyState {
	var state legacyState
	if m.inContainer() {
		return state
	}
	nodeUnit := false
	if sm, err := m.serviceManager(cfg); err == nil {
		if raw, err := os.ReadFile(unitPath(sm)); err == nil {
			nodeUnit = runsNodeServer(string(raw))
		}
	}
	state.NPMLink = findNPMLink(m.Getenv("PATH"))
	if command := processCommand(readPID(cfg.Paths.PIDFile)); runsNodeServer(command) {
		state.Detached = command
	}
	state.NeedsMigration = nodeUnit || state.Detached != "" || state.NPMLink != "" && exists(cfg.Paths.DatabaseFile)
	return state
}

// installSnapshot is an install as it was before it was switched over.
type installSnapshot struct {
	cfg     config.Config
	service *service.Manager
	// unit, env and marker are nil when the file did not exist.
	unit, env, marker, launcher []byte
	npmLink, npmTarget          string
	wasRunning                  bool
	detached                    string
	// database is the copy taken before anything could change its schema.
	database *backups.LocalSnapshot
}

func readOptional(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err == nil && raw == nil {
		raw = []byte{}
	}
	return raw, err
}

func (m *Manager) launcherPath() string {
	dir := m.Getenv("NEOCLOUD_BIN_DIR")
	if dir == "" {
		dir = filepath.Join(m.UserHome, ".local", "bin")
	}
	return filepath.Join(dir, AppCommand)
}

func (m *Manager) markerPath(cfg config.Config) string {
	return filepath.Join(RuntimeStore(cfg.Paths.Home).Root, "current.json")
}

func (m *Manager) snapshotInstall(cfg config.Config, legacy legacyState) (installSnapshot, error) {
	snap := installSnapshot{cfg: cfg, npmLink: legacy.NPMLink, detached: legacy.Detached}
	var err error
	if snap.service, err = m.serviceManager(cfg); err != nil {
		return snap, err
	}
	for _, file := range []struct {
		into *[]byte
		path string
	}{
		{&snap.unit, unitPath(snap.service)},
		{&snap.env, cfg.Paths.EnvFile},
		{&snap.marker, m.markerPath(cfg)},
	} {
		if *file.into, err = readOptional(file.path); err != nil {
			return snap, err
		}
	}
	// A symlink here is the Node.js command, which npmLink restores.
	if info, err := os.Lstat(m.launcherPath()); err == nil && info.Mode().IsRegular() {
		if snap.launcher, err = readOptional(m.launcherPath()); err != nil {
			return snap, err
		}
	}
	if snap.npmLink != "" {
		if snap.npmTarget, err = os.Readlink(snap.npmLink); err != nil {
			return snap, err
		}
	}
	snap.wasRunning = healthy(context.Background(), cfg.Port) || snap.service.Active()
	if snap.detached == "" && snap.unit == nil && snap.wasRunning {
		snap.detached = processCommand(readPID(cfg.Paths.PIDFile))
	}
	return snap, nil
}

// restoreInstall puts the install back the way the snapshot found it and starts
// what ran before.
func (m *Manager) restoreInstall(snap installSnapshot) error {
	// The context of the failed command may be what failed it.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg := snap.cfg
	var failed []error
	note := func(err error) {
		if err != nil {
			failed = append(failed, err)
		}
	}
	_, err := m.stopAndWait(ctx, cfg)
	note(err)
	restoreFile := func(path string, raw []byte, mode os.FileMode) {
		switch {
		case path == "":
		case raw != nil:
			note(envfile.WriteAtomic(path, raw, mode))
		default:
			if err := os.Remove(path); !errors.Is(err, fs.ErrNotExist) {
				note(err)
			}
		}
	}
	// A settings file this run created stays: it holds the only copy of a new
	// master key.
	if snap.env != nil {
		restoreFile(cfg.Paths.EnvFile, snap.env, 0o600)
	}
	if snap.database != nil {
		note(m.restoreDatabaseIfChanged(ctx, cfg, *snap.database))
	}
	restoreFile(m.markerPath(cfg), snap.marker, 0o600)
	if snap.unit != nil {
		note(envfile.WriteAtomic(unitPath(snap.service), snap.unit, 0o644))
	} else if exists(unitPath(snap.service)) {
		snap.service.Uninstall()
	}
	if snap.npmTarget != "" {
		if _, err := os.Lstat(snap.npmLink); errors.Is(err, fs.ErrNotExist) {
			note(os.Symlink(snap.npmTarget, snap.npmLink))
		}
	}
	if snap.launcher != nil {
		note(envfile.WriteAtomic(m.launcherPath(), snap.launcher, 0o755))
	}
	if snap.wasRunning {
		if err := m.startPrevious(snap); err != nil {
			note(err)
		} else {
			note(m.waitForServer(ctx, cfg.Port, "", m.startupTimeout))
		}
	}
	return errors.Join(failed...)
}

func (m *Manager) startPrevious(snap installSnapshot) error {
	if snap.unit != nil {
		return snap.service.Launch()
	}
	spec := snap.service.Spec
	// `node /path/server/index.js`, started by the Node.js CLI as a detached
	// process: the interpreter has no spaces, the script's path may.
	if program, script, ok := strings.Cut(snap.detached, " "); ok && runsNodeServer(script) {
		spec.Program, spec.Args = program, []string{script}
		spec.WorkDir = filepath.Dir(filepath.Dir(script))
		spec.Env = []service.EnvVar{{Key: "NEOCLOUD_HOME", Value: snap.cfg.Paths.Home}, {Key: "NODE_ENV", Value: "production"}}
	}
	sm, err := m.newService(spec)
	if err != nil {
		return err
	}
	return sm.StartFallback()
}

// installCommand retires the Node.js command and points the installed one at
// the binary the service runs. Failing here never fails an install.
func (m *Manager) installCommand(npmLink string) {
	if npmLink != "" {
		if err := os.Remove(npmLink); err != nil && !errors.Is(err, fs.ErrNotExist) {
			m.UI.Warn("Couldn't remove the Node.js neocloud command at " + npmLink + ": " + err.Error())
		}
	}
	// A command that was just removed from there still has to work.
	if dir, _, err := activeRuntime(m.paths().Home); err == nil && npmLink == m.launcherPath() {
		if _, err := release.RefreshCommandLauncher(npmLink, npmLink, runtimeBinary(dir)); err != nil {
			m.UI.Warn("Could not add the neocloud command: " + err.Error())
		}
		return
	}
	m.refreshLauncher()
}

// refreshLauncher updates the standalone command install.sh put in
// ~/.local/bin to the installed runtime's binary.
func (m *Manager) refreshLauncher() {
	dir, _, err := activeRuntime(m.paths().Home)
	if err != nil {
		return
	}
	launcher := m.launcherPath()
	if info, err := os.Lstat(launcher); err != nil || !info.Mode().IsRegular() {
		return
	}
	if err := update.RefreshLauncher(launcher, filepath.Dir(launcher), AppCommand, runtimeBinary(dir)); err != nil {
		m.UI.Warn("The neocloud command could not be refreshed: " + err.Error())
	}
}
