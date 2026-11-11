package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// desktopPath follows the XDG autostart spec: $XDG_CONFIG_HOME/autostart.
func desktopPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "autostart", "neocloud-lite.desktop"), nil
}

// quoteExec quotes an argument for the desktop entry Exec key.
func quoteExec(arg string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`)
	return `"` + replacer.Replace(arg) + `"`
}

func enable(exe string) error {
	path, err := desktopPath()
	if err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=NeoCloud Lite\n" +
		"Exec=" + quoteExec(exe) + " run\nX-GNOME-Autostart-enabled=true\nTerminal=false\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(entry), 0o644)
}

func disable() error {
	path, err := desktopPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func enabled() bool {
	path, err := desktopPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
