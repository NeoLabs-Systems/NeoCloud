// Package handoff receives files other devices of the account send to this
// computer ("Send to device", docs/handoff.md): it listens on the account's
// event stream, tells the person, asks where to save, downloads with resume
// and keeps every write inside the chosen folder.
package handoff

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNameBytes = 200

var windowsReserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)

// SafeName makes a name from the server (already sanitised there) safe to
// create as a plain file in any folder: no path parts, no control or
// bidirectional characters, nothing Windows refuses.
func SafeName(name string) string { return safeName(name, runtime.GOOS == "windows") }

func safeName(name string, windows bool) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\':
			b.WriteRune('_')
		case windows && strings.ContainsRune(`:*?"<>|`, r):
			b.WriteRune('_')
		case unicode.IsControl(r), r == 0x061c, r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e,
			r >= 0x2066 && r <= 0x2069, r == 0xfeff:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	clean := strings.TrimLeft(strings.TrimRight(strings.TrimSpace(b.String()), ". "), ".")
	if clean == "" {
		return "file"
	}
	stem, ext := clean, ""
	if i := strings.LastIndex(clean, "."); i > 0 {
		stem, ext = clean[:i], clean[i:]
	}
	if len(ext) > 20 {
		ext = truncate(ext, 20)
	}
	stem = truncate(stem, maxNameBytes-len(ext))
	if windowsReserved.MatchString(stem) {
		stem = "_" + stem
	}
	return stem + ext
}

// truncate cuts s to at most n bytes without splitting a character.
func truncate(s string, n int) string {
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// Reserve creates a new empty file for name directly inside folder and
// returns its path. It never touches an existing file: a taken name becomes
// "name (1).ext", "name (2).ext", … The file is created exclusively, so two
// sends racing for one name get different files. The path is verified to lie
// inside folder, whatever name says.
func Reserve(folder, name string) (string, error) {
	root, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	clean := SafeName(name)
	stem, ext := clean, ""
	if i := strings.LastIndex(clean, "."); i > 0 {
		stem, ext = clean[:i], clean[i:]
	}
	for n := 0; n < 1000; n++ {
		candidate := clean
		if n > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		path := filepath.Join(root, candidate)
		if filepath.Dir(path) != root {
			return "", errors.New("the file name points outside the folder")
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			file.Close()
			return path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("could not find a free name")
}
