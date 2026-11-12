//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// TestCloudFirstAgainstRealServer: `pair add --create` makes the local
// folder, the first sync downloads what the server holds, and from then on
// the pair syncs both ways. On macOS the folder gets the NeoCloud icon,
// which stays local.
func TestCloudFirstAgainstRealServer(t *testing.T) {
	s := startServer(t)
	rest := api.New(s.url, s.deviceLogin(t, "rest").Token)
	if _, err := rest.EnsureFolderPath(context.Background(), api.RootAlias, remoteFolder+"/docs"); err != nil {
		t.Fatal(err)
	}
	uploadREST(t, rest, "", "a.txt", "from the cloud")
	uploadREST(t, rest, "docs", "b.txt", "nested")

	bin, home := buildCLI(t), t.TempDir()
	cliLogin(t, s, bin, home)
	// Keep the test's folder out of this computer's real sidebar.
	cfg, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Update(func(c *config.Config) error { c.FileManagerSidebar = new(false); return nil })

	local := filepath.Join(t.TempDir(), "NeoCloud")
	runCLI(t, bin, home, "pair", "add", local, "/"+remoteFolder, "--create")
	runCLI(t, bin, home, "sync", "--once")
	for rel, want := range map[string]string{"a.txt": "from the cloud", "docs/b.txt": "nested"} {
		data, err := os.ReadFile(filepath.Join(local, filepath.FromSlash(rel)))
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", rel, data, err)
		}
	}
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(filepath.Join(local, "Icon\r")); err != nil {
			t.Errorf("no custom folder icon: %v", err)
		}
		if out, _ := exec.Command("xattr", local).Output(); !strings.Contains(string(out), "com.apple.FinderInfo") {
			t.Errorf("no FinderInfo on the folder: %q", out)
		}
	}

	os.WriteFile(filepath.Join(local, "a.txt"), []byte("edited here"), 0o644)
	os.WriteFile(filepath.Join(local, "docs", "new.txt"), []byte("new here"), 0o644)
	runCLI(t, bin, home, "sync", "--once")
	remote := s.remoteSnapshot(t, rest)
	if remote["a.txt"] != sha([]byte("edited here")) || remote["docs/new.txt"] != sha([]byte("new here")) {
		t.Errorf("local changes not uploaded: %v", remote)
	}
	for rel := range remote {
		if strings.HasPrefix(rel, "Icon") || strings.EqualFold(rel, "desktop.ini") {
			t.Errorf("%q reached the server", rel)
		}
	}
	if len(remote) != 4 {
		t.Errorf("server holds %v", remote)
	}

	// Selective sync: unticking docs moves the local copy to the trash
	// and leaves the cloud alone.
	reopened, _ := config.Open(home)
	reopened.Update(func(c *config.Config) error {
		c.Pairs[0].ExcludePaths = []string{"docs"}
		return c.Pairs[0].Validate()
	})
	runCLI(t, bin, home, "sync", "--once")
	if _, err := os.Stat(filepath.Join(local, "docs")); !os.IsNotExist(err) {
		t.Errorf("the excluded folder is still here: %v", err)
	}
	if trashed, _ := filepath.Glob(filepath.Join(local, ".neocloud", "trash", "*", "docs", "b.txt")); len(trashed) != 1 {
		t.Error("the excluded folder is not in the local trash")
	}
	if after := s.remoteSnapshot(t, rest); len(after) != 4 {
		t.Errorf("excluding touched the server: %v", after)
	}
}
