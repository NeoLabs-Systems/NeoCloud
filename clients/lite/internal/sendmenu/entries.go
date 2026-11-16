package sendmenu

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The Linux entries are plain files; they live here, free of build tags, so
// every system's tests can check what would be written.

const entryName = "Send to my device"

// linuxFiles are the entries under the XDG data folder.
func linuxFiles(data, exe string) []file {
	script := "#!/bin/sh\nexec " + quoteShell(exe) + " send \"$@\"\n"
	return []file{
		{filepath.Join(data, "applications", "neocloud-send.desktop"), "[Desktop Entry]\nType=Application\nName=" + entryName +
			"\nComment=Send files to another of your NeoCloud devices\nExec=" + quoteExec(exe) +
			" send %F\nTerminal=false\nCategories=Network;\nMimeType=application/octet-stream;\n", 0o644},
		{filepath.Join(data, "nautilus", "scripts", entryName), script, 0o755},
		{filepath.Join(data, "nemo", "scripts", entryName), script, 0o755},
		{filepath.Join(data, "kio", "servicemenus", "neocloud-send.desktop"), "[Desktop Entry]\nType=Service\nMimeType=all/allfiles;\n" +
			"Actions=send\nX-KDE-Submenu=NeoCloud\n\n[Desktop Action send]\nName=" + entryName + "\nExec=" + quoteExec(exe) + " send %F\n", 0o755},
	}
}

// file is one entry to write on Linux.
type file struct {
	path    string
	content string
	mode    os.FileMode
}

func write(files []file) error {
	var errs []error
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, os.WriteFile(f.path, []byte(f.content), f.mode), os.Chmod(f.path, f.mode))
	}
	return errors.Join(errs...)
}

func remove(files []file) error {
	var errs []error
	for _, f := range files {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// quoteExec quotes an argument for a desktop entry's Exec key.
func quoteExec(arg string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`, "%", "%%")
	return `"` + replacer.Replace(arg) + `"`
}

// quoteShell quotes an argument for a POSIX shell script.
func quoteShell(arg string) string { return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'" }
