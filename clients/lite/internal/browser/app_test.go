package browser

import (
	"errors"
	"strings"
	"testing"
)

func TestFindChromium(t *testing.T) {
	env := map[string]string{"HOME": "/Users/me", "LOCALAPPDATA": `C:\Users\me\AppData\Local`, "ProgramFiles": `C:\Program Files`}
	getenv := func(key string) string { return env[key] }
	noPath := func(string) (string, error) { return "", errors.New("not found") }
	only := func(want string) func(string) bool { return func(path string) bool { return path == want } }

	mac := "/Users/me/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"
	if got := findChromium("darwin", getenv, noPath, only(mac)); got != mac {
		t.Errorf("darwin: %q", got)
	}
	edge := `C:\Program Files\Microsoft\Edge\Application\msedge.exe`
	if got := findChromium("windows", getenv, noPath, only(edge)); got != edge {
		t.Errorf("windows: %q", got)
	}
	linux := func(name string) (string, error) {
		if name == "chromium" {
			return "/usr/bin/chromium", nil
		}
		return "", errors.New("not found")
	}
	if got := findChromium("linux", getenv, linux, only("")); got != "/usr/bin/chromium" {
		t.Errorf("linux: %q", got)
	}
	if got := findChromium("linux", getenv, noPath, only("")); got != "" {
		t.Errorf("nothing installed: %q", got)
	}
}

func TestAppArgsUseTheirOwnProfile(t *testing.T) {
	args := strings.Join(AppArgs("http://127.0.0.1:1/launch?code=x", "/cfg/app-window"), " ")
	for _, want := range []string{"--app=http://127.0.0.1:1/launch?code=x", "--user-data-dir=/cfg/app-window", "--window-size=" + AppWindowSize} {
		if !strings.Contains(args, want) {
			t.Errorf("%s lacks %s", args, want)
		}
	}
}
