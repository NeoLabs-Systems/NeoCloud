package manager

// The signed runtime bundles NeoCloud installs and updates itself with:
// cmd/neocloud-release builds one archive per platform and signs a manifest
// that lists them.

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

const (
	// AppCommand is the command, and the name release assets start with.
	AppCommand = "neocloud"
	appName    = "NeoCloud"
	// RuntimeArchiveRoot is the top-level directory of a runtime archive and
	// the folder under the NeoCloud home that holds installed runtimes.
	RuntimeArchiveRoot = "app"
	serviceLabel       = "com.neocloud"
	defaultPort        = config.DefaultPort
)

// public_key.txt is the release key (`neocloud-release runtime-keygen`); a
// build without one trusts nothing.
//
//go:embed public_key.txt
var embeddedPublicKey string

// RuntimePublicKey is the Ed25519 key a downloaded runtime manifest must be
// signed with. CI holds the private half in the
// NEOCLOUD_RUNTIME_SIGNING_PRIVATE_KEY secret.
func RuntimePublicKey() (string, error) {
	if strings.TrimSpace(embeddedPublicKey) == "" {
		return "", cli.Errorf("SETUP_TRUST_NOT_CONFIGURED", "This NeoCloud build has no release signing key, "+
			"so it cannot verify or install downloaded releases. The project owner has to publish one first.")
	}
	return release.NormalizePublicKey(strings.TrimSpace(embeddedPublicKey))
}

// SupportedTargets are the platforms every release carries.
func SupportedTargets() release.Targets {
	return release.Targets{"linux": {"x64", "arm64"}, "macos": {"x64", "arm64"}}
}

// RuntimeAssetName is one platform's archive.
func RuntimeAssetName(platform, arch, version string) string {
	return fmt.Sprintf("%s-runtime-%s-%s-%s.zip", AppCommand, platform, arch, strings.TrimPrefix(version, "v"))
}

// RuntimeStore is where downloaded runtimes live: <home>/app/versions/<version>,
// with current.json naming the active one.
func RuntimeStore(home string) release.RuntimeStore {
	return release.RuntimeStore{
		Root: filepath.Join(home, RuntimeArchiveRoot),
		RequiredFiles: []string{
			filepath.Join(RuntimeArchiveRoot, "bin", AppCommand),
			filepath.Join(RuntimeArchiveRoot, "flutter_app", "build", "web", "index.html"),
			filepath.Join(RuntimeArchiveRoot, "landing", "index.html"),
		},
		SchemaVersion: 1,
	}
}

func runtimeAppDir(versionDir string) string { return filepath.Join(versionDir, RuntimeArchiveRoot) }

func runtimeBinary(versionDir string) string {
	return filepath.Join(runtimeAppDir(versionDir), "bin", AppCommand)
}

// activeRuntime is the version directory and version this install runs from,
// or an error when it runs a binary installed some other way.
func activeRuntime(home string) (string, string, error) {
	store := RuntimeStore(home)
	marker, err := store.Current()
	if err != nil {
		return "", "", err
	}
	dir, err := store.RuntimeDir(marker.Version)
	return dir, marker.Version, err
}
