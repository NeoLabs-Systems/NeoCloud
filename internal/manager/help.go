package manager

import (
	"context"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
)

var helpGroups = []struct {
	name    string
	entries [][2]string
}{
	{"Setup", [][2]string{
		{"install [--channel stable|beta]", "Download the app and start the background service"},
		{"setup [--port N] [--public-url URL]", "Create configuration and secrets"},
	}},
	{"Running", [][2]string{
		{"start [--foreground] · stop · restart", "Control the background service"},
		{"serve", "Run the server in the foreground"},
		{"status", "Show whether it runs and where data lives"},
		{"logs [-f]", "Show recent logs (-f to follow)"},
		{"doctor [--full]", "Check the installation, read-only (--full: integrity_check)"},
		{"repair", "Download what is missing, reinstall the service and restart"},
		{"uninstall", "Remove the background service (keeps data)"},
	}},
	{"Updates", [][2]string{
		{"update [stable|beta] [--check]", "Snapshot, update, restart; rolls back on failure"},
		{"update --tag <version>", "Move to one release and stay on it"},
		{"rollback", "Undo the last update (and the database if its schema changed)"},
		{"channel [stable|beta]", "Show or switch the release channel"},
		{"version", "Print the version"},
	}},
	{"Data & accounts", [][2]string{
		{"env list|get|set|unset", "Read or change settings (secrets masked)"},
		{"admin list|grant|revoke <user>", "Manage administrators"},
		{"keys export", "Print the master key for safekeeping"},
		{"backup list|run <id>|verify <id>", "Backup targets set up in the app"},
		{"backup snapshots <id>", "Finished snapshots on a target"},
		{"backup restore <id> <snapshot> [--bulk]", "Restore with the server stopped"},
		{"backup migrate-storage-class <id>", "Move S3 backups to the target's storage class"},
		{"backup snapshots|restore … --from <path>", "From a backup folder or target.json (new machine)"},
	}},
}

func (m *Manager) cmdHelp(context.Context, []string) error {
	m.UI.Title("NeoCloud "+m.Version, "")
	for _, group := range helpGroups {
		m.UI.Section(m.UI.Accent(group.name))
		for _, entry := range group.entries {
			m.UI.Print("  " + cli.PadEnd(AppCommand+" "+entry[0], 50) + "  " + m.UI.Muted(entry[1]))
		}
	}
	m.UI.Blank()
	return nil
}
