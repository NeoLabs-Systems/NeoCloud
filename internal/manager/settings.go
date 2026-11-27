package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
)

const masterKeyEnv = "NEOCLOUD_MASTER_KEY"

func (m *Manager) cmdEnv(_ context.Context, args []string) error {
	if err := m.ensureEnv(); err != nil {
		return err
	}
	envFile := m.paths().EnvFile
	action, key := "list", ""
	if len(args) > 0 {
		action = args[0]
	}
	if len(args) > 1 {
		key = args[1]
	}
	usage := inputError("Usage: neocloud env [list | get KEY | set KEY VALUE | unset KEY]")
	values, err := envfile.Load(envFile)
	if err != nil {
		return err
	}
	switch {
	case action == "list" && key == "":
		m.UI.Title("Settings in "+envFile, "")
		for _, name := range envfile.SortedKeys(values) {
			fmt.Fprintf(m.Out, "%s=%s\n", name, envfile.Mask(name, values[name]))
		}
		return nil
	case action == "get" && key != "":
		fmt.Fprintln(m.Out, values[key])
		return nil
	case (action == "set" || action == "unset") && key != "":
		if key == masterKeyEnv {
			return errors.New("The master key can’t be changed: stored files are encrypted under it.")
		}
		if !envfile.ValidKey(key) {
			return inputError("Invalid environment variable name: " + key)
		}
		if action == "unset" {
			if _, err := envfile.Remove(envFile, key); err != nil {
				return err
			}
		} else if err := envfile.Upsert(envFile, key, strings.Join(args[2:], " ")); err != nil {
			return err
		}
		m.UI.OK(fmt.Sprintf("%s saved. Restart with %s to apply.", key, m.UI.Command("neocloud restart")))
		return nil
	}
	return usage
}

func (m *Manager) cmdAdmin(ctx context.Context, args []string) error {
	action, username := "list", ""
	if len(args) > 0 {
		action = args[0]
	}
	if len(args) > 1 {
		username = args[1]
	}
	if action != "list" && action != "grant" && action != "revoke" {
		return inputError("Usage: neocloud admin [list | grant <username> | revoke <username>]")
	}
	if action != "list" && username == "" {
		return inputError("Usage: neocloud admin " + action + " <username>")
	}
	runtime, err := m.openRuntime(ctx)
	if err != nil {
		return err
	}
	defer runtime.Close()
	if action == "list" {
		names, err := runtimeapp.Admins(ctx, runtime.DB.W)
		if err != nil {
			return err
		}
		m.UI.Title("Admins", "")
		if len(names) == 0 {
			m.UI.Info("No admin yet. The first account you register becomes one.")
		}
		for _, name := range names {
			m.UI.OK(name)
		}
		return nil
	}
	name, changed, err := runtimeapp.SetAdmin(ctx, runtime.DB.W, username, action == "grant", "cli")
	if err != nil {
		return err
	}
	switch {
	case action == "grant" && changed:
		m.UI.OK(name + " is now an admin.")
	case action == "grant":
		m.UI.OK(name + " already was an admin.")
	case changed:
		m.UI.OK(name + " is no longer an admin.")
	default:
		m.UI.OK(name + " was not an admin.")
	}
	return nil
}

func (m *Manager) cmdKeys(_ context.Context, args []string) error {
	if len(args) != 1 || args[0] != "export" {
		return inputError("Usage: neocloud keys export")
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if cfg.MasterKeyHex == "" {
		return errors.New("There is no master key yet: run `neocloud setup` first.")
	}
	m.UI.Title("Master key", "")
	m.UI.Warn("Anyone with this key and a copy of your storage can read every file. Keep it offline.")
	fmt.Fprintf(m.Out, "\n  %s=%s\n\n", masterKeyEnv, cfg.MasterKeyHex)
	return nil
}

func (m *Manager) cmdLogs(ctx context.Context, args []string) error {
	logDir := m.paths().LogDir
	var files []string
	for _, name := range []string{AppCommand + ".log", AppCommand + ".error.log"} {
		if file := filepath.Join(logDir, name); exists(file) {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		m.UI.Info("No log files in " + logDir + " yet.")
		return nil
	}
	tail := []string{"-n", "100"}
	if cli.HasFlag(args, "-f") || cli.HasFlag(args, "--follow") {
		tail = append(tail, "-f")
	}
	command := exec.CommandContext(ctx, "tail", append(tail, files...)...)
	command.Stdout, command.Stderr = m.Out, os.Stderr
	if err := command.Run(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
