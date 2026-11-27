// Package updates owns NeoCloud's self-update as the server sees it: where
// releases are published, the release channel, the check for a newer signed
// release, the status file the admin page and the update runner share, and the
// /api/admin/update endpoints.
package updates

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

const (
	// Repo is the GitHub repository releases are published in.
	Repo = "NeoLabs-Systems/NeoCloud"
	// ManifestPrefix names a release's signed runtime manifest; the signature
	// is the same name plus ".sig".
	ManifestPrefix = "neocloud-runtime-manifest-"
	// ChannelEnvKey is the .env key that stores the release channel.
	ChannelEnvKey = "NEOCLOUD_RELEASE_CHANNEL"
	// ContainerEnvKey marks the container image, which updates by being
	// replaced.
	ContainerEnvKey = "NEOCLOUD_CONTAINER"
)

// Channel is the release channel an install follows: the environment wins over
// .env, and stable is the default.
func Channel(getenv func(string) string, envFile string) string {
	if getenv != nil {
		if value := getenv(ChannelEnvKey); value != "" {
			return release.NormalizeChannel(value)
		}
	}
	values, err := envfile.Load(envFile)
	if err != nil {
		return release.Stable
	}
	return release.NormalizeChannel(values[ChannelEnvKey])
}

// SetChannel records the release channel in an install's .env.
func SetChannel(envFile, channel string) error {
	return envfile.Upsert(envFile, ChannelEnvKey, release.NormalizeChannel(channel))
}

// Latest is the version of the newest release of the channel that carries a
// signed runtime manifest. It reads release metadata only.
func Latest(ctx context.Context, client *release.Client, channel string) (string, error) {
	found, err := client.ResolveRelease(ctx, Repo, channel, ManifestPrefix)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(found.Tag, "v"), nil
}

var containerCgroup = regexp.MustCompile(`(?i)docker|podman|kubepods|lxc`)

// InContainer reports whether this process runs inside a container, where there
// is nothing to install and the image is the deployment unit.
func InContainer(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(getenv(ContainerEnvKey)) {
	case "1", "true", "yes", "on":
		return true
	}
	if getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	if value := getenv("container"); value != "" {
		return value != "host"
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	raw, err := os.ReadFile("/proc/1/cgroup")
	return err == nil && containerCgroup.Match(raw)
}
