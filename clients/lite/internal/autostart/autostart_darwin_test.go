package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The LaunchAgent starts the bundle's executable with `run` at login, is
// tied to NeoCloud.app and does not restart the app after Quit. HOME is
// a temporary folder, so the user's real login items are never touched.
func TestLaunchAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	exe := "/Applications/NeoCloud.app/Contents/MacOS/NeoCloud"
	if err := enable(exe); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("not enabled after enable")
	}
	raw, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", Label+".plist"))
	if err != nil {
		t.Fatal(err)
	}
	plist := string(raw)
	for _, want := range []string{
		"<array><string>" + exe + "</string><string>run</string></array>",
		"<key>RunAtLoad</key><true/>",
		"<key>AssociatedBundleIdentifiers</key><string>systems.neolabs.neocloud.lite</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("LaunchAgent lacks %s:\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "KeepAlive") {
		t.Error("KeepAlive would restart the app after Quit")
	}
	if err := Disable(); err != nil || Enabled() {
		t.Errorf("Disable: %v, still enabled: %v", err, Enabled())
	}
}
