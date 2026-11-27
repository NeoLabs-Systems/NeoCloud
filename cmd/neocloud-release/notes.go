package main

// The download buttons at the top of a GitHub release: one shields.io badge per
// app and server build, linking straight to its file once attached and grey
// "building" until then, plus the Docker image. Kept between two markers so a
// refresh replaces only this part of the notes.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/updates"
)

const (
	notesStart  = "<!-- neocloud-download:start -->"
	notesEnd    = "<!-- neocloud-download:end -->"
	dockerImage = "ghcr.io/neolabs-systems/neocloud"
)

type slot struct {
	label, detail, badge, logo, color string
	match                             *regexp.Regexp
}

func downloadSlot(label, detail, badge, logo, color, pattern string) slot {
	return slot{label, detail, badge, logo, color, regexp.MustCompile(pattern)}
}

var slots = []slot{
	downloadSlot("macOS", "Universal", "Universal", "apple", "1c2117", `^NeoCloud-.+-macos\.dmg$`),
	downloadSlot("Windows", "x64 installer", "x64", "windows", "0078d4", `^NeoCloud-Setup-.+-windows-x64\.exe$`),
	downloadSlot("Windows", "ARM64 installer", "ARM64", "windows", "0078d4", `^NeoCloud-Setup-.+-windows-arm64\.exe$`),
	downloadSlot("Linux", "AppImage", "AppImage", "linux", "2f7d6e", `^NeoCloud-.+-linux-x86_64\.AppImage$`),
	downloadSlot("Android", "APK", "APK", "android", "3ddc84", `^neocloud-.+-android\.apk$`),
	downloadSlot("Server", "Linux x64", "Linux x64", "linux", "f0a126", `^neocloud-cli-linux-x64$`),
	downloadSlot("Server", "Linux ARM64", "Linux ARM64", "linux", "f0a126", `^neocloud-cli-linux-arm64$`),
	downloadSlot("Server", "macOS Apple silicon", "macOS ARM64", "apple", "f0a126", `^neocloud-cli-macos-arm64$`),
	downloadSlot("Server", "macOS Intel", "macOS x64", "apple", "f0a126", `^neocloud-cli-macos-x64$`),
}

func escape(value string) string { return strings.ReplaceAll(url.QueryEscape(value), "+", "%20") }

func badge(label, message, color, logo string) string {
	return "https://img.shields.io/badge/" + escape(label) + "-" + escape(message) + "-" + color +
		"?style=for-the-badge&logo=" + escape(logo) + "&logoColor=white"
}

// downloadNotes renders the download section of a release's notes.
func downloadNotes(tag, repository string, assets []string) string {
	releaseURL := "https://github.com/" + repository + "/releases/tag/" + escape(tag)
	var badges []string
	ready := 0
	for _, slot := range slots {
		href, color, message := releaseURL, "6b7280", "building"
		for _, asset := range assets {
			if slot.match.MatchString(asset) {
				// The tag's own address, which also holds while a release is a draft.
				href = "https://github.com/" + repository + "/releases/download/" + escape(tag) + "/" + escape(asset)
				color, message = slot.color, slot.badge
				ready++
				break
			}
		}
		badges = append(badges, fmt.Sprintf(`<a href="%s"><img alt="%s %s" src="%s"></a>`, href, slot.label, slot.detail, badge(slot.label, message, color, slot.logo)))
	}
	badges = append(badges, fmt.Sprintf(`<a href="https://github.com/%s/pkgs/container/neocloud"><img alt="Docker image" src="%s"></a>`,
		repository, badge("Docker", "ghcr.io", "2496ed", "docker")))
	status := "Every download for this release is ready."
	switch {
	case ready == 0:
		status = "The downloads are building now. Each one appears here as soon as its build finishes."
	case ready < len(slots):
		status = "Ready downloads are available in one click. The remaining builds attach as they finish."
	}
	return strings.Join([]string{
		notesStart, "", `<p align="center">`, "  <strong>Download NeoCloud " + tag + "</strong>", "</p>", "",
		`<p align="center">`, "  " + strings.Join(badges, "\n  "), "</p>", "",
		`<p align="center"><sub>` + status + ` Server: <code>docker pull ` + dockerImage + ":" + strings.TrimPrefix(tag, "v") + `</code></sub></p>`,
		"", "---", "", notesEnd,
	}, "\n")
}

// mergeDownloadNotes replaces the download section of body, or prepends it.
func mergeDownloadNotes(body, section string) string {
	current := strings.TrimPrefix(body, "\uFEFF")
	start, end := strings.Index(current, notesStart), strings.Index(current, notesEnd)
	if start >= 0 && end > start {
		parts := []string{}
		for _, part := range []string{strings.TrimSpace(current[:start]), section, strings.TrimSpace(current[end+len(notesEnd):])} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, "\n\n") + "\n"
	}
	if strings.TrimSpace(current) == "" {
		return section + "\n"
	}
	return section + "\n\n" + strings.TrimLeft(current, " \t\r\n")
}

func updateDownloadNotes(ctx context.Context, tag, repository string, dryRun bool) error {
	if repository == "" {
		repository = os.Getenv("GITHUB_REPOSITORY")
	}
	if repository == "" {
		repository = updates.Repo
	}
	// `gh release view` also finds a draft, which the tags API does not.
	out, err := exec.CommandContext(ctx, "gh", "release", "view", tag, "--repo", repository, "--json", "body,assets").Output()
	if err != nil {
		return fmt.Errorf("gh release view: %w", err)
	}
	var found struct {
		Body   string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(out, &found); err != nil {
		return err
	}
	var names []string
	for _, asset := range found.Assets {
		names = append(names, asset.Name)
	}
	body := mergeDownloadNotes(found.Body, downloadNotes(tag, repository, names))
	if dryRun {
		fmt.Print(body)
		return nil
	}
	directory, err := os.MkdirTemp("", "neocloud-release-notes-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	notes := filepath.Join(directory, "notes.md")
	if err := os.WriteFile(notes, []byte(body), 0o600); err != nil {
		return err
	}
	edit := exec.CommandContext(ctx, "gh", "release", "edit", tag, "--repo", repository, "--notes-file", notes)
	edit.Stdout, edit.Stderr = os.Stdout, os.Stderr
	return edit.Run()
}
