package sync

import (
	"path"
	"strconv"
	"strings"
	"time"
)

// ConflictName is the name a local file gets when both sides changed it:
// `name (conflict <device> YYYY-MM-DD HHmm).ext` (docs/clients.md). The
// extension stays last so the copy still opens in the right app.
func ConflictName(name, device string, at time.Time) string {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	// ".bashrc" has no stem; treat the whole name as the stem.
	if stem == "" {
		stem, ext = name, ""
	}
	return stem + " (conflict " + cleanDevice(device) + " " + at.Format("2006-01-02 1504") + ")" + ext
}

// cleanDevice keeps a device name usable inside a file name on every OS.
func cleanDevice(device string) string {
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 0x20 {
			return '-'
		}
		return r
	}, strings.TrimSpace(device))
	if cleaned == "" {
		return "device"
	}
	return cleaned
}

// uniqueName adds " 2", " 3", … before the extension until taken() is false.
func uniqueName(name string, taken func(string) bool) string {
	if !taken(name) {
		return name
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := stem + " " + strconv.Itoa(i) + ext
		if !taken(candidate) {
			return candidate
		}
	}
}
