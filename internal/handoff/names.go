package handoff

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Names of a send end up on another device's disk, so they are made safe here
// for every receiver: no path parts, no control or bidi characters, nothing
// Windows refuses, unique within the send.

const (
	maxNameBytes      = 200
	maxExtensionBytes = 20
)

var (
	windowsReserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)
	numberSuffix    = regexp.MustCompile(` \(\d+\)$`)
)

// hidden reports control characters and the bidirectional overrides and
// isolates that make "report‮fdp.exe" read as a PDF.
func hidden(r rune) bool {
	return unicode.IsControl(r) || r == 0x061c || r >= 0x200b && r <= 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0xfeff
}

func splitExtension(name string) (stem, extension string) {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 {
		return name, ""
	}
	return name[:index], name[index:]
}

func truncateBytes(text string, limit int) string {
	for len(text) > limit {
		_, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
	}
	return text
}

func sanitiseFileName(raw string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case hidden(r):
			return -1
		case r == '/' || r == '\\':
			return '_'
		}
		return r
	}, norm.NFC.String(strings.ToValidUTF8(raw, "")))
	// A trailing dot or space is dropped by Windows; leading dots would hide the file.
	name = strings.TrimLeft(strings.TrimRight(strings.TrimSpace(name), ". "), ".")
	if name == "" {
		return "file"
	}
	stem, extension := splitExtension(name)
	extension = truncateBytes(extension, maxExtensionBytes)
	stem = truncateBytes(stem, maxNameBytes-len(extension))
	if windowsReserved.MatchString(stem) {
		stem = "_" + stem
	}
	return stem + extension
}

// uniqueFileNames sanitises names and makes them differ, ignoring case, with
// "name (1).ext".
func uniqueFileNames(raw []string) []string {
	used := map[string]bool{}
	out := make([]string, len(raw))
	for i, value := range raw {
		clean := sanitiseFileName(value)
		name := clean
		for n := 1; used[strings.ToLower(name)]; n++ {
			stem, extension := splitExtension(clean)
			name = fmt.Sprintf("%s (%d)%s", numberSuffix.ReplaceAllString(stem, ""), n, extension)
		}
		used[strings.ToLower(name)] = true
		out[i] = name
	}
	return out
}
