package sendmenu

import (
	"os"
	"path/filepath"
)

const supported = true

// Apply adds or removes the entries; exe is the program they start.
func Apply(on bool, exe string) error {
	files := linuxFiles(dataHome(), exe)
	if !on {
		return remove(files)
	}
	return write(files)
}

func dataHome() string {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}
