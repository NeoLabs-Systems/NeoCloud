package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalled(t *testing.T) {
	cases := map[string]bool{
		filepath.Join(os.TempDir(), "go-build123", "exe", "neocloud-lite"):                     false,
		"/private/var/folders/x/T/AppTranslocation/ABC/d/NeoCloud.app/Contents/MacOS/NeoCloud": false,
		"/Volumes/NeoCloud/NeoCloud.app/Contents/MacOS/NeoCloud":                               false,
		"/Applications/NeoCloud.app/Contents/MacOS/NeoCloud":                                   true,
		filepath.Join(string(filepath.Separator), "opt", "neocloud", "neocloud-lite"):          true,
	}
	for exe, want := range cases {
		if got := Installed(exe); got != want {
			t.Errorf("Installed(%q) = %v, want %v", exe, got, want)
		}
	}
}

// An AppImage runs from a fresh mount each time; the entry must start the
// image file instead.
func TestExecutablePrefersTheAppImage(t *testing.T) {
	t.Setenv("APPIMAGE", "/home/ana/Apps/NeoCloud.AppImage")
	if exe, err := Executable(); err != nil || exe != "/home/ana/Apps/NeoCloud.AppImage" {
		t.Errorf("Executable() = %q, %v", exe, err)
	}
	t.Setenv("APPIMAGE", "")
	if exe, err := Executable(); err != nil || exe == "" {
		t.Errorf("Executable() = %q, %v", exe, err)
	}
}
