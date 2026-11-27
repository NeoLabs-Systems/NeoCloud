package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

// fetch reads a JSON endpoint of the local server and reports whether it
// answered with success.
func fetch(ctx context.Context, port int, path string) (map[string]any, bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
	if err != nil {
		return nil, false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, false
	}
	defer response.Body.Close()
	body := map[string]any{}
	_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&body)
	return body, response.StatusCode >= 200 && response.StatusCode < 300
}

func healthy(ctx context.Context, port int) bool {
	_, ok := fetch(ctx, port, "/api/health")
	return ok
}

// waitForServer waits until /api/health answers and, when version is not empty,
// the server is that version.
func (m *Manager) waitForServer(ctx context.Context, port int, version string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if healthy(ctx, port) {
			if version == "" {
				return nil
			}
			if body, ok := fetch(ctx, port, "/api/version"); ok {
				if got, _ := body["version"].(string); release.CompareVersions(got, version) == 0 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return notReady(port)
}

// installService writes the service definition and starts it, and reports
// whether the system restarts it by itself.
func (m *Manager) installService(cfg config.Config) (bool, error) {
	if m.inContainer() {
		return false, errors.New("NeoCloud runs inside a container: let Docker manage its lifecycle.")
	}
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return false, err
	}
	sm.Logf = func(level, message string) {
		if level == "warn" {
			m.UI.Warn(message)
		}
	}
	if err := sm.Install(); err != nil {
		if !sm.Installed() {
			return false, err
		}
		// The definition is on disk but nothing runs it; a detached process
		// still keeps NeoCloud up.
		m.UI.Warn("Couldn't set up the system service, so NeoCloud won't restart by itself")
		m.UI.Paragraph(err.Error(), 4, m.UI.Muted)
		return false, sm.StartFallback()
	}
	return sm.Installed(), nil
}

func readPID(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return 0
	}
	return pid
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return pid > 0 && (err == nil || errors.Is(err, syscall.EPERM))
}

// stopAndWait asks the service manager and any detached process to stop and
// waits until nothing answers: running transfers finish first. It reports
// whether there was anything to stop.
func (m *Manager) stopAndWait(ctx context.Context, cfg config.Config) (bool, error) {
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return false, err
	}
	pid := readPID(cfg.Paths.PIDFile)
	running := cli.PortOpen(cfg.Port) || processAlive(pid)
	stopped := running || sm.Active()
	sm.Stop()
	if processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(stopTimeout)
	for running && (processAlive(pid) || cli.PortOpen(cfg.Port)) {
		if time.Now().After(deadline) {
			return stopped, fmt.Errorf("NeoCloud is still running %d s after being asked to stop.", int(stopTimeout/time.Second))
		}
		select {
		case <-ctx.Done():
			return stopped, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return stopped, nil
}

func (m *Manager) containerLifecycle(verb string) {
	m.UI.Blank()
	m.UI.Info(fmt.Sprintf("NeoCloud runs in a container: %s it with `docker compose %s neocloud`", verb, verb))
	m.UI.Blank()
}

func (m *Manager) cmdStart(ctx context.Context, args []string) error {
	if cli.HasFlag(args, "--foreground") || m.inContainer() {
		return m.serve(ctx)
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if m.legacyInstall(cfg).NeedsMigration {
		return errUpgradeFirst
	}
	m.UI.Title("Starting NeoCloud", "")
	if healthy(ctx, cfg.Port) {
		m.UI.OK(fmt.Sprintf("Already running on http://localhost:%d", cfg.Port))
		return nil
	}
	if err := m.start(ctx); err != nil {
		return err
	}
	if !m.hasAccount(ctx) {
		m.UI.Info("The first account you register becomes the admin.")
	}
	return nil
}

// start installs the service definition, starts it and waits for the server.
func (m *Manager) start(ctx context.Context) error {
	if err := m.ensureEnv(); err != nil {
		return err
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if _, err := m.installService(cfg); err != nil {
		return err
	}
	if err := m.waitForServer(ctx, cfg.Port, "", 30*time.Second); err != nil {
		return err
	}
	m.UI.OK("Running on " + address(cfg))
	return nil
}

func (m *Manager) cmdStop(ctx context.Context, _ []string) error {
	if m.inContainer() {
		m.containerLifecycle("stop")
		return nil
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	m.UI.Title("Stopping NeoCloud", "")
	m.UI.Info("Letting running uploads and downloads finish (up to 30 s)...")
	stopped, err := m.stopAndWait(ctx, cfg)
	if err != nil {
		return err
	}
	if stopped {
		m.UI.OK("Stopped")
	} else {
		m.UI.Warn("NeoCloud wasn't running")
	}
	return nil
}

func (m *Manager) cmdRestart(ctx context.Context, _ []string) error {
	if m.inContainer() {
		m.containerLifecycle("restart")
		return nil
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if m.legacyInstall(cfg).NeedsMigration {
		return errUpgradeFirst
	}
	if err := m.cmdStop(ctx, nil); err != nil {
		return err
	}
	m.UI.Title("Starting NeoCloud", "")
	return m.start(ctx)
}

func (m *Manager) cmdUninstall(ctx context.Context, _ []string) error {
	if m.inContainer() {
		m.containerLifecycle("stop")
		return nil
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	m.UI.Title("Removing the NeoCloud service", "")
	if _, err := m.stopAndWait(ctx, cfg); err != nil {
		return err
	}
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return err
	}
	sm.Uninstall()
	m.UI.OK("Service removed. Your files and settings stay in place:")
	m.UI.Info(cfg.Paths.Home)
	return nil
}

// cmdHealthcheck is what the container health check runs.
func (m *Manager) cmdHealthcheck(ctx context.Context, _ []string) error {
	port := m.port()
	if !healthy(ctx, port) {
		return fmt.Errorf("NeoCloud is not healthy on port %d.", port)
	}
	return nil
}

func (m *Manager) cmdVersion(context.Context, []string) error {
	fmt.Fprintln(m.Out, "neocloud "+m.Version)
	return nil
}
