package sync

import (
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/unicode/norm"
)

// Reasons a path is left out of syncing (docs/clients.md "Not synced").
// The desktop engine uses the same codes.
const (
	ReasonSymlink     = "symlink"
	ReasonSpecialFile = "specialFile"
	ReasonInvalidName = "invalidName"
	ReasonPathTooLong = "pathTooLong"
	ReasonNameClash   = "nameClash"
	ReasonIgnored     = "ignored"
	ReasonUnreadable  = "unreadable"
	// ReasonMovedIgnored is a synced file whose local copy was moved into
	// an ignored or excluded folder: it stops syncing and the cloud copy
	// stays where it was.
	ReasonMovedIgnored = "movedIgnored"
	// ReasonKeptInCloud is a folder deleted on this computer that stays in
	// the cloud because it holds files this computer does not sync.
	ReasonKeptInCloud = "keptInCloud"
)

// NotSynced is a path the engine deliberately leaves alone, with why.
type NotSynced struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// windowsNames turns on the Windows name rules. A variable so tests on
// other systems can exercise them.
var windowsNames = runtime.GOOS == "windows"

// windowsMaxPath is MAX_PATH minus the terminating NUL.
const windowsMaxPath = 259

// nfc is the key every path is compared under: macOS writes decomposed
// names (NFD), everything else composed ones, and the server keeps
// whatever it was sent.
func nfc(rel string) string {
	return norm.NFC.String(rel)
}

// foldKey is how the server compares names: without case.
func foldKey(rel string) string {
	return strings.ToLower(rel)
}

// windowsNameProblem explains why Windows cannot hold a file of this name,
// or returns "" when it can.
func windowsNameProblem(name string) string {
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "names ending in a dot or space are not allowed on Windows"
	}
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return "the name contains a character Windows does not allow"
		}
	}
	stem := strings.ToUpper(name)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.TrimRight(stem, " ")
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return "the name is reserved on Windows"
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
		return "the name is reserved on Windows"
	}
	return ""
}

// unsafePath is the detail for a server path that cannot name a file
// inside the pair.
const unsafePath = "not synced: the server sent a path that does not stay inside this folder"

// remotePathProblem explains why a relative path from the server cannot be
// used locally, or returns "" when it can. The server never sends such
// paths (its name rules reject them); the client still refuses to rely on
// that: a path that is absolute, has an empty, "." or ".." segment, or
// holds a backslash or NUL could reach outside the pair's folder.
func remotePathProblem(rel string) string {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsAny(rel, "\\\x00") {
		return unsafePath
	}
	for _, segment := range strings.Split(rel, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return unsafePath
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		// On Windows this also refuses reserved names in any segment.
		for _, segment := range strings.Split(rel, "/") {
			if problem := windowsNameProblem(segment); windowsNames && problem != "" {
				return "not synced: " + problem
			}
		}
		return unsafePath
	}
	return ""
}

// windowsPathTooLong reports whether full exceeds MAX_PATH (counted in
// UTF-16 units, as Windows does).
func windowsPathTooLong(full string) bool {
	return len(utf16.Encode([]rune(full))) > windowsMaxPath
}

// clashes finds names that cannot coexist: siblings that differ only by
// case (or by Unicode normalization) on one side, and a new local name
// that differs only by case from a different remote one. The server
// compares names without case, so only one of them can be synced; the one
// already tracked (or present on the other side, or else the first in
// sort order) keeps syncing and the others are reported.
func clashes(local, remote, base map[string]bool, rawLocal map[string][]string) []NotSynced {
	var out []NotSynced
	report := func(rel, other, where string) {
		detail := "not synced: name clashes with " + path.Base(other) + where
		out = append(out, NotSynced{Path: rel, Reason: ReasonNameClash, Detail: detail})
	}
	// Two names on disk that normalize to the same key.
	for _, spellings := range rawLocal {
		if len(spellings) > 1 {
			sort.Strings(spellings)
			for _, other := range spellings[1:] {
				report(other, spellings[0], "")
			}
		}
	}
	sideClashes := func(side, otherSide map[string]bool, where string) {
		groups := map[string][]string{}
		for rel := range side {
			groups[foldKey(rel)] = append(groups[foldKey(rel)], rel)
		}
		for _, group := range groups {
			if len(group) < 2 {
				continue
			}
			sort.Strings(group)
			keep := group[0]
			for _, rel := range group {
				if base[rel] || otherSide[rel] {
					keep = rel
					break
				}
			}
			for _, rel := range group {
				if rel != keep {
					report(rel, keep, where)
				}
			}
		}
	}
	sideClashes(local, remote, "")
	sideClashes(remote, local, " on the server")
	remoteByFold := map[string]string{}
	for rel := range remote {
		remoteByFold[foldKey(rel)] = rel
	}
	for rel := range local {
		other, ok := remoteByFold[foldKey(rel)]
		if !ok || other == rel || base[rel] || remote[rel] || local[other] {
			continue
		}
		report(rel, other, " on the server")
		out = append(out, NotSynced{Path: other, Reason: ReasonNameClash, Detail: "not synced: name clashes with " + path.Base(rel) + " on this computer"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
