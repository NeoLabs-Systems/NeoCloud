//go:build !windows

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func lockTo(t *testing.T, profile, target string) {
	t.Helper()
	os.Remove(filepath.Join(profile, "SingletonLock"))
	if err := os.Symlink(target, filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
}

func TestSingletonPID(t *testing.T) {
	profile := t.TempDir()
	if pid := singletonPID(profile); pid != 0 {
		t.Errorf("no lock: %d", pid)
	}
	for target, want := range map[string]int{"neo-mbp.local-4242": 4242, "host-": 0, "nohyphen": 0, "host-x": 0} {
		lockTo(t, profile, target)
		if pid := singletonPID(profile); pid != want {
			t.Errorf("%q: %d, want %d", target, pid, want)
		}
	}
}

// CloseApp ends the browser that runs with the profile, and nothing else.
func TestCloseApp(t *testing.T) {
	profile := t.TempDir()

	// A stale lock naming a process that is not the app window's browser
	// (here the test itself) is ignored.
	lockTo(t, profile, fmt.Sprintf("host-%d", os.Getpid()))
	if err := CloseApp(profile); err != nil {
		t.Fatal(err)
	}

	browser := exec.Command("sh", "-c", "sleep 30; :", "browser", "--user-data-dir="+profile)
	if err := browser.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- browser.Wait() }()
	lockTo(t, profile, fmt.Sprintf("host-%d", browser.Process.Pid))
	time.Sleep(100 * time.Millisecond) // let ps see the full command line
	if err := CloseApp(profile); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		browser.Process.Kill()
		t.Fatal("the app window's browser kept running")
	}
}
