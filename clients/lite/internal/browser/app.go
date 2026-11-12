package browser

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// AppWindowSize is the app window's size in CSS pixels.
const AppWindowSize = "440,680"

// OpenApp opens url as a small app window: a Chromium-based browser
// (Chrome, Chromium, Edge, Brave) started with --app and its own profile
// in profileDir, so it never mixes with the user's browsing profile. With
// no such browser it falls back to the default browser. It reports
// whether an app window was used.
func OpenApp(url, profileDir string) (bool, error) {
	exe := findChromium(runtime.GOOS, os.Getenv, exec.LookPath, fileExists)
	if exe == "" {
		return false, Open(url)
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return false, Open(url)
	}
	cmd := exec.Command(exe, AppArgs(url, profileDir)...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return false, Open(url)
	}
	go cmd.Wait()
	return true, nil
}

// AppArgs are the browser arguments for an app window.
func AppArgs(url, profileDir string) []string {
	return []string{
		"--app=" + url,
		"--window-size=" + AppWindowSize,
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		// The window is the app, not a web page: no translate bar (Chrome's
		// Translate, Edge's msTranslate) and no autofill server lookups.
		"--disable-features=Translate,msTranslate,AutofillServerCommunication",
		"--disable-sync",
	}
}

// findChromium returns the first Chromium-based browser it finds, or "".
func findChromium(goos string, getenv func(string) string, lookPath func(string) (string, error), exists func(string) bool) string {
	var candidates []string
	switch goos {
	case "darwin":
		apps := []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Chromium.app/Contents/MacOS/Chromium",
			"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"Brave Browser.app/Contents/MacOS/Brave Browser",
		}
		roots := []string{"/Applications"}
		if home := getenv("HOME"); home != "" {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
		for _, app := range apps {
			for _, root := range roots {
				candidates = append(candidates, filepath.Join(root, app))
			}
		}
	case "windows":
		exes := []string{
			`Google\Chrome\Application\chrome.exe`,
			`Microsoft\Edge\Application\msedge.exe`,
			`BraveSoftware\Brave-Browser\Application\brave.exe`,
			`Chromium\Application\chrome.exe`,
		}
		for _, exe := range exes {
			for _, root := range []string{"LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)"} {
				if dir := getenv(root); dir != "" {
					candidates = append(candidates, dir+`\`+exe)
				}
			}
		}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable", "brave-browser", "brave"} {
			if path, err := lookPath(name); err == nil {
				return path
			}
		}
	}
	for _, candidate := range candidates {
		if exists(candidate) {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// singletonPID reads the browser's process id from the target of the
// profile's SingletonLock symlink ("<hostname>-<pid>"; Chromium on macOS
// and Linux). It returns 0 when there is none.
func singletonPID(profileDir string) int {
	target, err := os.Readlink(filepath.Join(profileDir, "SingletonLock"))
	if err != nil {
		return 0
	}
	cut := strings.LastIndexByte(target, '-')
	if cut < 0 {
		return 0
	}
	pid, err := strconv.Atoi(target[cut+1:])
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
