package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
	"github.com/NeoLabs-Systems/neocloud/internal/manager"
	"github.com/NeoLabs-Systems/neocloud/internal/updates"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

// The release workflow and the Node.js updater both rely on these agreeing.
func TestVersionedFilesAgree(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"package.json", "package-lock.json"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(raw, &document); err != nil || document.Version != buildinfo.Version {
			t.Errorf("%s version = %q, buildinfo %q (%v)", name, document.Version, buildinfo.Version, err)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSetVersionStampsEveryFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, versionFile), "package buildinfo\n\nconst Version = \"0.1.0\"\n")
	write(t, filepath.Join(root, "package.json"), `{"name": "x", "version": "0.1.0"}`)
	write(t, filepath.Join(root, "package-lock.json"), `{"version": "0.1.0", "packages": {"": {"version": "0.1.0"}}}`)
	if err := setVersion(root, "v0.2.0-beta.3"); err != nil {
		t.Fatal(err)
	}
	if got, err := readVersion(root); err != nil || got != "0.2.0-beta.3" {
		t.Fatal(got, err)
	}
	lock, _ := os.ReadFile(filepath.Join(root, "package-lock.json"))
	if strings.Contains(string(lock), "0.1.0") {
		t.Fatalf("lock = %s", lock)
	}
	if setVersion(root, "banana") == nil {
		t.Fatal("accepted an invalid version")
	}
}

func TestNextVersion(t *testing.T) {
	for _, c := range []struct {
		current string
		tags    []string
		channel string
		want    string
	}{
		{"0.1.1-beta.35", []string{"v0.1.1-beta.35", "v0.1.1-beta.9"}, "beta", "0.1.1-beta.36"},
		{"0.1.1-beta.35", []string{"v0.1.1-beta.35"}, "stable", "0.1.1"},
		{"0.1.1", []string{"v0.1.1", "v0.1.1-beta.35"}, "beta", "0.1.2-beta.0"},
		{"0.1.1", []string{"v0.0.0"}, "stable", "0.1.1"},
		{"1.0.0", []string{"v0.9.0"}, "stable", "1.0.0"},
	} {
		if got, err := nextVersion(c.current, c.tags, c.channel); err != nil || got != c.want {
			t.Errorf("%+v: %q, %v", c, got, err)
		}
	}
}

func TestRuntimeBundlesAreSignedAndInstallable(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, publicKey := base64.StdEncoding.EncodeToString(der), base64.StdEncoding.EncodeToString(public)
	root, dist := t.TempDir(), t.TempDir()
	write(t, filepath.Join(root, webDir, "index.html"), "web")
	write(t, filepath.Join(root, "landing", "index.html"), "landing")
	for platform, architectures := range manager.SupportedTargets() {
		for _, arch := range architectures {
			write(t, filepath.Join(dist, cliBinary(platform, arch)), "binary "+platform+arch)
		}
	}
	if err := signRuntimeBundles(root, dist, "v1.2.3", privateKey, publicKey); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dist, updates.ManifestPrefix+"1.2.3.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(manifest + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := release.Verify(data, string(signature), publicKey); !ok || err != nil {
		t.Fatalf("signature: %v %v", ok, err)
	}
	store := manager.RuntimeStore(t.TempDir())
	if err := store.InstallArchive("1.2.3", filepath.Join(dist, manager.RuntimeAssetName("linux", "x64", "1.2.3"))); err != nil {
		t.Fatalf("the archive does not install: %v", err)
	}
	archive, err := zip.OpenReader(filepath.Join(dist, manager.RuntimeAssetName("macos", "arm64", "1.2.3")))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	for _, want := range []string{"app/bin/neocloud", "app/flutter_app/build/web/index.html", "app/landing/index.html"} {
		if !slices.Contains(names, want) {
			t.Errorf("archive lacks %s: %v", want, names)
		}
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	otherDER, _ := x509.MarshalPKCS8PrivateKey(other)
	if signRuntimeBundles(root, dist, "1.2.3", base64.StdEncoding.EncodeToString(otherDER), publicKey) == nil {
		t.Fatal("signed with a key that does not match")
	}
}

func TestDownloadNotes(t *testing.T) {
	section := downloadNotes("v1.2.3", "o/r", []string{"neocloud-cli-linux-x64", "neocloud-v1.2.3-android.apk", "neocloud-runtime-linux-x64-1.2.3.zip"})
	for _, want := range []string{"releases/download/v1.2.3/neocloud-cli-linux-x64", "releases/download/v1.2.3/neocloud-v1.2.3-android.apk", "building", "docker pull ghcr.io/neolabs-systems/neocloud:1.2.3"} {
		if !strings.Contains(section, want) {
			t.Errorf("notes lack %q:\n%s", want, section)
		}
	}
	merged := mergeDownloadNotes("## Changes\n", section)
	again := mergeDownloadNotes(merged, downloadNotes("v1.2.3", "o/r", nil))
	if strings.Count(again, notesStart) != 1 || !strings.Contains(again, "## Changes") || strings.Contains(again, "releases/download") {
		t.Fatalf("merged = %s", again)
	}
}
