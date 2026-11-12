package shellfolder

import (
	"bufio"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// setIcon sets GNOME's (Nautilus, Nemo, Caja) custom icon metadata. KDE's
// .directory file is left out: it would sit in the pair and be synced.
func setIcon(dir, assets string) error {
	icon, err := writeAsset(assets, "neocloud.png", logoPNG)
	if err != nil {
		return err
	}
	gio, err := exec.LookPath("gio")
	if err != nil {
		return nil // no GIO: nothing to set
	}
	return exec.Command(gio, "set", dir, "metadata::custom-icon", fileURL(icon)).Run()
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// bookmarksFile is GTK's sidebar list (Nautilus, Nemo, Thunar…).
func bookmarksFile() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gtk-3.0", "bookmarks"), nil
}

// pin adds a "file:///… NeoCloud" line to the GTK bookmarks.
func pin(dir, _ string) error {
	return editBookmarks(func(lines []string) []string {
		want := fileURL(dir)
		for _, line := range lines {
			if bookmarkURL(line) == want {
				return lines
			}
		}
		return append(lines, want+" "+filepath.Base(dir))
	})
}

// unpin drops the bookmark lines for dir.
func unpin(dir string) error {
	return editBookmarks(func(lines []string) []string {
		want := fileURL(dir)
		kept := lines[:0]
		for _, line := range lines {
			if bookmarkURL(line) != want {
				kept = append(kept, line)
			}
		}
		return kept
	})
}

func bookmarkURL(line string) string {
	target, _, _ := strings.Cut(line, " ")
	return target
}

func editBookmarks(edit func([]string) []string) error {
	file, err := bookmarksFile()
	if err != nil {
		return err
	}
	var lines []string
	if data, err := os.ReadFile(file); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			if line := scanner.Text(); line != "" {
				lines = append(lines, line)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	before := strings.Join(lines, "\n")
	lines = edit(lines)
	after := strings.Join(lines, "\n")
	if after == before {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	if after != "" {
		after += "\n"
	}
	return os.WriteFile(file, []byte(after), 0o644)
}
