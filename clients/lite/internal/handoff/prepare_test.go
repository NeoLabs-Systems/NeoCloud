package handoff

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrepareAcceptsFilesAndKeepsOrder(t *testing.T) {
	dir := t.TempDir()
	a, b := write(t, dir, "a.txt", "aa"), write(t, dir, "b.txt", "")
	items, err := Prepare([]string{a, b, a, "", "  "})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "a.txt" || items[0].Size != 2 || items[1].Name != "b.txt" || items[1].Size != 0 {
		t.Errorf("items: %+v", items)
	}
	if Describe(items) != "2 files" || TotalSize(items) != 2 || Describe(items[:1]) != "a.txt" {
		t.Error("describe/total")
	}
}

func TestPrepareRefusesFolders(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "a.txt", "x")
	sub := filepath.Join(dir, "Photos")
	os.Mkdir(sub, 0o755)
	_, err := Prepare([]string{file, sub})
	if err == nil || !strings.Contains(err.Error(), "Photos is a folder") || !strings.Contains(err.Error(), "zip") {
		t.Errorf("error: %v", err)
	}
	var user *UserError
	if !errors.As(err, &user) {
		t.Errorf("a person-facing error is expected, got %T", err)
	}
	if _, err := Prepare([]string{sub, filepath.Join(dir, "Other")}); err == nil {
		t.Error("two entries")
	}
	if _, err := Prepare([]string{sub, sub}); err == nil || !strings.Contains(err.Error(), "Photos is a folder") {
		t.Errorf("one folder named once: %v", err)
	}
}

func TestPrepareCaps(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for i := 0; i <= MaxFiles; i++ {
		paths = append(paths, write(t, dir, fmt.Sprintf("f%02d", i), "x"))
	}
	if _, err := Prepare(paths[:MaxFiles]); err != nil {
		t.Errorf("50 files are fine: %v", err)
	}
	if _, err := Prepare(paths); err == nil || !strings.Contains(err.Error(), "up to 50 files") {
		t.Errorf("51 files: %v", err)
	}
	big := filepath.Join(dir, "big.bin")
	f, _ := os.Create(big)
	if err := f.Truncate(MaxBytes); err != nil {
		t.Skip("no sparse files here")
	}
	f.Close()
	if _, err := Prepare([]string{big}); err != nil {
		t.Errorf("exactly 2 GiB is fine: %v", err)
	}
	if _, err := Prepare([]string{big, paths[0]}); err == nil || !strings.Contains(err.Error(), "2.0 GiB") {
		t.Errorf("over 2 GiB: %v", err)
	}
}

func TestPrepareMissingUnreadableAndEmptyChoice(t *testing.T) {
	dir := t.TempDir()
	if _, err := Prepare([]string{filepath.Join(dir, "gone.txt")}); err == nil || !strings.Contains(err.Error(), "gone.txt no longer exists") {
		t.Errorf("missing: %v", err)
	}
	if _, err := Prepare(nil); err == nil || !strings.Contains(err.Error(), "at least one file") {
		t.Errorf("nothing: %v", err)
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := write(t, dir, "secret.txt", "x")
		os.Chmod(locked, 0)
		t.Cleanup(func() { os.Chmod(locked, 0o600) })
		if _, err := Prepare([]string{locked}); err == nil || !strings.Contains(err.Error(), "secret.txt can't be read: you don't have permission") {
			t.Errorf("unreadable: %v", err)
		}
	}
}

func TestResolveDevice(t *testing.T) {
	devices := []api.HandoffDevice{{ID: "d:1", Name: "Pixel 8"}, {ID: "s:2", Name: "Firefox"}, {ID: "d:3", Name: "firefox"}}
	if d, err := ResolveDevice(devices, "pixel 8"); err != nil || d.ID != "d:1" {
		t.Errorf("by name: %v %v", d, err)
	}
	if d, err := ResolveDevice(devices, "d:3"); err != nil || d.ID != "d:3" {
		t.Errorf("by id: %v %v", d, err)
	}
	if _, err := ResolveDevice(devices, "Firefox"); err == nil || !strings.Contains(err.Error(), "2 devices") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err := ResolveDevice(devices, "Tablet"); err == nil || !strings.Contains(err.Error(), "No device called") {
		t.Errorf("unknown: %v", err)
	}
}

func TestDeviceLabelsAreUniqueAndPlain(t *testing.T) {
	labels := DeviceLabels([]api.HandoffDevice{
		{Name: "Pixel", Kind: "phone"}, {Name: "Pixel", Kind: "phone"}, {Name: "Work\nPC‮", Kind: "desktop"}, {Name: "x", Kind: "app"}})
	want := []string{"Pixel (phone)", "Pixel (phone) #2", "WorkPC (computer)", "x"}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("%d: %q want %q", i, labels[i], want[i])
		}
	}
}
