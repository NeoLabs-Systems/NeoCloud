package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

const installedExe = "/Applications/NeoCloud.app/Contents/MacOS/NeoCloud"

// The first launch of the installed app turns on start at login once;
// after that a user who turned it off keeps it off.
func TestFirstLaunchTurnsOnAutostartOnce(t *testing.T) {
	cfg, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	enable := func() error { enabled++; return nil }
	if err := defaultAutostart(cfg, installedExe, enable); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || !cfg.Get().AutostartDefaulted {
		t.Fatalf("first launch: enabled %d times, defaulted %v", enabled, cfg.Get().AutostartDefaulted)
	}
	// The user turns it off in Settings; later launches leave it off.
	if err := defaultAutostart(cfg, installedExe, enable); err != nil || enabled != 1 {
		t.Fatalf("second launch enabled it again (%d, %v)", enabled, err)
	}
	reopened, err := config.Open(filepath.Dir(cfg.Path()))
	if err != nil || !reopened.Get().AutostartDefaulted {
		t.Fatalf("the default was not saved: %v", err)
	}
}

// A copy run from the disk image, a quarantine location or a `go run`
// build is not registered, and the default waits for the installed copy.
func TestAutostartWaitsForTheInstalledCopy(t *testing.T) {
	cfg, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enable := func() error { t.Error("enabled from a temporary location"); return nil }
	for _, exe := range []string{
		"/Volumes/NeoCloud/NeoCloud.app/Contents/MacOS/NeoCloud",
		"/private/var/folders/x/T/AppTranslocation/1/d/NeoCloud.app/Contents/MacOS/NeoCloud",
		filepath.Join(os.TempDir(), "go-build1", "b001", "exe", "neocloud-lite"),
	} {
		if err := defaultAutostart(cfg, exe, enable); err != nil {
			t.Fatal(err)
		}
	}
	if cfg.Get().AutostartDefaulted {
		t.Error("marked as done without enabling")
	}
}

// A failure is reported and retried on the next launch.
func TestAutostartFailureIsRetried(t *testing.T) {
	cfg, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	failing := errors.New("no LaunchAgents folder")
	if err := defaultAutostart(cfg, installedExe, func() error { return failing }); !errors.Is(err, failing) {
		t.Fatalf("error %v", err)
	}
	if cfg.Get().AutostartDefaulted {
		t.Error("marked as done after a failure")
	}
}

func TestLaunchArgs(t *testing.T) {
	if got := launchArgs([]string{"-psn_0_12345"}); len(got) != 0 {
		t.Errorf("Finder argument kept: %v", got)
	}
	if got := launchArgs([]string{"status", "--json"}); len(got) != 2 {
		t.Errorf("CLI arguments: %v", got)
	}
}
