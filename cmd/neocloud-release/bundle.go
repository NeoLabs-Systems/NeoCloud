package main

// The signed runtime archives an install runs from: one zip per platform
// holding app/bin/neocloud, app/flutter_app/build/web and app/landing, plus a
// manifest of their digests signed with the release key.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/manager"
	"github.com/NeoLabs-Systems/neocloud/internal/updates"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

const (
	signingKeyEnv = "NEOCLOUD_RUNTIME_SIGNING_PRIVATE_KEY"
	publicKeyFile = "internal/manager/public_key.txt"
	webDir        = "flutter_app/build/web"
)

// bundleAssets travel with the binary, at the same place in the repository and
// the runtime directory.
var bundleAssets = []string{webDir, "landing"}

// cliBinary is the release asset name of one platform's command.
func cliBinary(platform, arch string) string {
	return manager.AppCommand + "-cli-" + platform + "-" + arch
}

func buildRuntimeBundles(root, dist, version, privateKey string) error {
	publicKey, err := manager.RuntimePublicKey()
	if err != nil {
		return fmt.Errorf("%s holds no release key; run `neocloud-release runtime-keygen`, commit the printed public key "+
			"and set the %s secret: %w", publicKeyFile, signingKeyEnv, err)
	}
	return signRuntimeBundles(root, dist, version, privateKey, publicKey)
}

func signRuntimeBundles(root, dist, version, privateKey, publicKey string) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if _, err := release.NormalizeVersion(version); err != nil {
		return err
	}
	if strings.TrimSpace(privateKey) == "" {
		return errors.New(signingKeyEnv + " is required to sign runtime manifests")
	}
	if err := release.CheckKeypair(privateKey, publicKey); err != nil {
		return fmt.Errorf("the signing key does not match the embedded runtime public key: %w", err)
	}
	var artifacts []release.Artifact
	for platform, architectures := range manager.SupportedTargets() {
		for _, arch := range architectures {
			artifact, err := bundleTarget(root, dist, version, platform, arch)
			if err != nil {
				return err
			}
			artifacts = append(artifacts, artifact)
		}
	}
	manifest, err := release.NewRuntimeManifest(1, version, artifacts, manager.SupportedTargets())
	if err != nil {
		return err
	}
	data, err := release.Serialize(manifest)
	if err != nil {
		return err
	}
	return release.WriteSigned(filepath.Join(dist, updates.ManifestPrefix+version+".json"), data, privateKey, publicKey)
}

// bundleTarget builds one platform's archive and returns its manifest entry.
func bundleTarget(root, dist, version, platform, arch string) (release.Artifact, error) {
	var none release.Artifact
	stage, err := os.MkdirTemp("", "neocloud-runtime-")
	if err != nil {
		return none, err
	}
	defer os.RemoveAll(stage)
	app := filepath.Join(stage, manager.RuntimeArchiveRoot)
	if err := os.MkdirAll(filepath.Join(app, "bin"), 0o755); err != nil {
		return none, err
	}
	binary, err := os.ReadFile(filepath.Join(dist, cliBinary(platform, arch)))
	if err != nil {
		return none, fmt.Errorf("read %s (was it built?): %w", cliBinary(platform, arch), err)
	}
	if err := os.WriteFile(filepath.Join(app, "bin", manager.AppCommand), binary, 0o755); err != nil {
		return none, err
	}
	for _, asset := range bundleAssets {
		source := filepath.Join(root, filepath.FromSlash(asset))
		if info, err := os.Stat(filepath.Join(source, "index.html")); err != nil || !info.Mode().IsRegular() {
			return none, fmt.Errorf("%s/index.html is missing", asset)
		}
		if err := os.CopyFS(filepath.Join(app, filepath.FromSlash(asset)), os.DirFS(source)); err != nil {
			return none, err
		}
	}
	name := manager.RuntimeAssetName(platform, arch, version)
	archive := filepath.Join(dist, name)
	if err := release.CreateZip(stage, archive); err != nil {
		return none, err
	}
	digest, size, err := release.FileDigest(archive)
	if err != nil {
		return none, err
	}
	fmt.Printf("built %s (%d bytes)\n", name, size)
	return release.Artifact{Platform: platform, Architecture: arch, AssetName: name, SHA256: digest, SizeBytes: size}, nil
}

// writeKeypair generates the Ed25519 release keypair: the private half goes to
// a file for the CI secret, the public half is printed for public_key.txt.
func writeKeypair(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	privatePath := filepath.Join(dir, "neocloud-runtime-signing.key")
	if err := os.WriteFile(privatePath, []byte(base64.StdEncoding.EncodeToString(der)+"\n"), 0o600); err != nil {
		return err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(publicDER))
	fmt.Fprintf(os.Stderr, "private key written to %s: store it as the %s secret, then delete the file\n", privatePath, signingKeyEnv)
	fmt.Fprintf(os.Stderr, "put the printed public key in %s\n", publicKeyFile)
	return nil
}
