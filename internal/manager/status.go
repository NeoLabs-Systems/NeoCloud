package manager

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"golang.org/x/sys/unix"
)

// serviceKind names what keeps the server running.
func (m *Manager) serviceKind(cfg config.Config) string {
	sm, err := m.serviceManager(cfg)
	switch {
	case err != nil:
		return "not installed"
	case sm.Installed() && sm.Platform == "macos":
		return "launchd"
	case sm.Installed():
		return "systemd"
	case processAlive(readPID(cfg.Paths.PIDFile)):
		return "process"
	}
	return "not installed"
}

func (m *Manager) printStatus(ctx context.Context, cfg config.Config) bool {
	running := healthy(ctx, cfg.Port)
	server, tone := "not running", "warn"
	if running {
		server, tone = fmt.Sprintf("running on port %d", cfg.Port), "ok"
	}
	rows := [][2]string{
		{"Service", m.serviceKind(cfg)},
		{"Version", m.Version},
		{"Channel", m.channel()},
		{"Config", cfg.Paths.EnvFile},
		{"Data", cfg.Paths.DataDir},
		{"Storage", cfg.Paths.StorageDir},
	}
	if m.inContainer() {
		rows[0][1] = "container"
	}
	m.UI.ItemWidth(tone, "Server", server, 8)
	for _, row := range rows {
		m.UI.ItemWidth("info", row[0], row[1], 8)
	}
	return running
}

func (m *Manager) cmdStatus(ctx context.Context, _ []string) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	m.UI.Title("NeoCloud status", "")
	if !m.printStatus(ctx, cfg) {
		return cli.Errorf(codeReported, "NeoCloud is not running.")
	}
	return nil
}

// installed reports whether there is anything to manage here already.
func (m *Manager) installed(cfg config.Config) bool {
	if exists(cfg.Paths.EnvFile) || exists(cfg.Paths.DatabaseFile) {
		return true
	}
	sm, err := m.serviceManager(cfg)
	return err == nil && sm.Installed()
}

// cmdHome is `neocloud` alone: install when nothing is here yet, otherwise the
// state of the install and a menu.
func (m *Manager) cmdHome(ctx context.Context) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if !m.inContainer() && !m.installed(cfg) && m.UI.Interactive() {
		return m.cmdInstall(ctx, nil)
	}
	m.UI.Title("NeoCloud "+m.Version, "")
	running := m.printStatus(ctx, cfg)
	m.UI.Blank()
	background := context.Background()
	actions := cli.HomeActions{
		AppName: appName, Running: running, ShowUpdate: !m.inContainer(),
		Open:    func() error { return openInBrowser(address(cfg)) },
		Start:   func() error { return m.cmdStart(background, nil) },
		Status:  func() error { return m.cmdStatus(background, nil) },
		Restart: func() error { return m.cmdRestart(background, nil) },
		Update:  func() error { return m.cmdUpdate(background, nil) },
		Doctor:  func() error { return m.cmdDoctor(background, nil) },
		Help:    func() error { return m.cmdHelp(background, nil) },
	}
	return m.UI.HomeMenu(AppCommand, actions)
}

func openInBrowser(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}

// byteSize formats like the web client does.
func byteSize(size uint64) string {
	value := float64(size)
	for _, unit := range []string{"B", "KB", "MB", "GB"} {
		if value < 1024 {
			if unit == "B" {
				return fmt.Sprintf("%.0f B", value)
			}
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return fmt.Sprintf("%.1f TB", value)
}

func newestSnapshot(dir string) string {
	names, err := filepath.Glob(filepath.Join(dir, "*.db.enc"))
	if err != nil || len(names) == 0 {
		return "none yet"
	}
	newest := slices.MaxFunc(names, func(a, b string) int {
		return modified(a).Compare(modified(b))
	})
	return filepath.Base(newest)
}

func modified(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// cmdDoctor is read-only: the database is opened read-only and never migrated,
// so it is safe next to a running server. --full runs integrity_check.
func (m *Manager) cmdDoctor(ctx context.Context, args []string) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	m.UI.Title("NeoCloud doctor", "")
	problems := 0
	check := func(ok bool, good, bad string) {
		if ok {
			m.UI.OK(good)
			return
		}
		m.UI.Item("error", bad, "")
		problems++
	}
	check(true, fmt.Sprintf("NeoCloud %s (%s %s/%s)", m.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH), "")
	check(exists(cfg.Paths.Home), "Runtime directory "+cfg.Paths.Home, "Runtime directory missing: "+cfg.Paths.Home)
	check(len(cfg.MasterKeyHex) == 64, "Master key present", "Master key missing or malformed")
	check(unix.Access(cfg.Paths.StorageDir, unix.W_OK) == nil, "Storage writable: "+cfg.Paths.StorageDir, "Storage directory is not writable: "+cfg.Paths.StorageDir)
	var disk unix.Statfs_t
	if unix.Statfs(cfg.Paths.StorageDir, &disk) == nil {
		free := uint64(disk.Bavail) * uint64(disk.Bsize)
		minimum := uint64(max(cfg.MinFreeDiskBytes, 0))
		check(free >= minimum, "Free disk space: "+byteSize(free),
			fmt.Sprintf("Only %s free; uploads are refused below %s (NEOCLOUD_MIN_FREE_DISK).", byteSize(free), byteSize(minimum)))
	}
	check(exists(filepath.Join(cfg.Paths.WebDir, "index.html")), "Web app: "+cfg.Paths.WebDir, "The web app is missing from "+cfg.Paths.WebDir+". Run `neocloud repair`.")
	m.checkDatabase(ctx, cfg, cli.HasFlag(args, "--full"), check)
	m.UI.ItemWidth("info", "Local snapshot", newestSnapshot(cfg.Paths.BackupDir), 14)
	check(healthy(ctx, cfg.Port), fmt.Sprintf("Server answers on port %d", cfg.Port), fmt.Sprintf("Server not reachable on port %d", cfg.Port))
	if problems > 0 {
		return cli.Errorf(codeReported, "%d problem(s) found.", problems)
	}
	return nil
}

func (m *Manager) checkDatabase(ctx context.Context, cfg config.Config, full bool, check func(bool, string, string)) {
	if _, err := os.Stat(cfg.Paths.DatabaseFile); errors.Is(err, fs.ErrNotExist) {
		m.UI.Info("No database yet; it is created on the first start.")
		return
	}
	current, err := schemaVersion(ctx, cfg.Paths.DatabaseFile)
	if err != nil {
		check(false, "", "Database could not be opened: "+err.Error())
		return
	}
	switch {
	case current > schema.Version:
		check(false, "", fmt.Sprintf("Database schema %d is newer than this NeoCloud (%d). Update again or run `neocloud rollback`.", current, schema.Version))
	case current < schema.Version:
		m.UI.Warn(fmt.Sprintf("Database schema %d; %d migration(s) will run on the next start.", current, schema.Version-current))
	default:
		check(true, fmt.Sprintf("Database schema %d (current)", current), "")
	}
	pragma := "quick_check"
	if full {
		pragma = "integrity_check"
	}
	db, err := openReadOnly(cfg.Paths.DatabaseFile)
	if err != nil {
		check(false, "", "Database could not be opened: "+err.Error())
		return
	}
	defer db.Close()
	started := time.Now()
	rows, err := db.QueryContext(ctx, "PRAGMA "+pragma)
	if err != nil {
		check(false, "", "Database "+pragma+" failed: "+err.Error())
		return
	}
	defer rows.Close()
	var failures []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			failures = append(failures, err.Error())
		} else if line != "ok" {
			failures = append(failures, line)
		}
	}
	if err := rows.Err(); err != nil {
		failures = append(failures, err.Error())
	}
	seconds := time.Since(started).Seconds()
	check(len(failures) == 0, fmt.Sprintf("Database %s ok (%.1f s)", pragma, seconds),
		fmt.Sprintf("Database %s failed (%.1f s): %s", pragma, seconds, strings.Join(failures[:min(len(failures), 5)], "; ")))
	if !full {
		m.UI.Info(m.UI.Command("neocloud doctor --full") + " runs the slower integrity_check.")
	}
	var wal uint64
	if info, err := os.Stat(cfg.Paths.DatabaseFile + "-wal"); err == nil {
		wal = uint64(info.Size())
	}
	m.UI.ItemWidth("info", "WAL", byteSize(wal), 14)
}
