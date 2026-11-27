package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

const versionFile = "internal/buildinfo/version.go"

// versionedFiles carry the same version: package.json and its lock are what the
// Node.js updater of older installations reads.
var versionedFiles = []struct {
	path    string
	pattern *regexp.Regexp
}{
	{versionFile, regexp.MustCompile(`(Version = )"[^"]*"`)},
	{"package.json", regexp.MustCompile(`("version": )"[^"]*"`)},
	{"package-lock.json", regexp.MustCompile(`("version": )"[^"]*"`)},
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-beta\.\d+)?$`)

func readVersion(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, versionFile))
	if err != nil {
		return "", err
	}
	match := regexp.MustCompile(`Version = "([^"]*)"`).FindSubmatch(raw)
	if match == nil {
		return "", fmt.Errorf("version constant not found in %s", versionFile)
	}
	return string(match[1]), nil
}

func setVersion(root, version string) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH or MAJOR.MINOR.PATCH-beta.N", version)
	}
	for _, file := range versionedFiles {
		path := filepath.Join(root, file.path)
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) && file.path != versionFile {
			return fmt.Errorf("%s is missing", file.path)
		}
		if err != nil {
			return err
		}
		if !file.pattern.Match(raw) {
			return fmt.Errorf("%s has no version", file.path)
		}
		if err := os.WriteFile(path, file.pattern.ReplaceAll(raw, []byte(`${1}"`+version+`"`)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

var tagPattern = regexp.MustCompile(`^v(\d+\.\d+\.\d+)(?:-beta\.(\d+))?$`)

// nextVersion is the version a channel releases next: the larger of the tree's
// version and a patch above the newest stable tag, with the next beta counter
// on beta.
func nextVersion(current string, tags []string, channel string) (string, error) {
	base, _, _ := strings.Cut(current, "-")
	if !versionPattern.MatchString(base) {
		return "", fmt.Errorf("invalid current version %q", current)
	}
	stable := ""
	for _, tag := range tags {
		if match := tagPattern.FindStringSubmatch(tag); match != nil && match[2] == "" && release.CompareVersions(match[1], stable) > 0 {
			stable = match[1]
		}
	}
	if stable != "" {
		parts := strings.Split(stable, ".")
		patch, err := strconv.Atoi(parts[2])
		if err != nil {
			return "", err
		}
		if after := fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1); release.CompareVersions(after, base) > 0 {
			base = after
		}
	}
	switch channel {
	case release.Stable:
		return base, nil
	case release.Beta:
	default:
		return "", fmt.Errorf("unknown channel %q: want beta or stable", channel)
	}
	next := 0
	for _, tag := range tags {
		if match := tagPattern.FindStringSubmatch(tag); match != nil && match[1] == base && match[2] != "" {
			counter, err := strconv.Atoi(match[2])
			if err != nil {
				return "", err
			}
			next = max(next, counter+1)
		}
	}
	return fmt.Sprintf("%s-beta.%d", base, next), nil
}
