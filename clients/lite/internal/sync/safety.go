package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Safety rails of a pass (docs/clients.md "Data safety"): the mass-delete
// guard, the root marker and the local trash. The desktop engine
// implements the same rules with the same numbers.

const (
	// A pass that would delete more than delete_guard_percent of the
	// tracked files (50), or more than delete_guard_files (500), on either
	// side waits for a confirmation, but only from massDeleteMin deletions
	// up, so ordinary clean-ups never ask.
	massDeleteMin = 20

	metaDir        = ".neocloud"
	markerName     = "pair-id"
	trashDir       = "trash"
	trashDayFormat = "2006-01-02"

	// Per-pair state (state.DB meta keys).
	metaMarker  = "marker"  // "1" once the root marker was written
	metaRootID  = "root_id" // the remote folder the rows describe
	metaConfirm = "confirm" // a DeleteConfirmation the user approved
	metaReport  = "report"  // the PairState of the last pass, for `status`
)

// Guard reasons.
const (
	GuardMassDelete  = "massDelete"
	GuardRemoteEmpty = "remoteEmpty"
)

// DeleteConfirmation describes deletions held back until the user agrees.
// Digest identifies exactly which paths were held, so an approval never
// covers a different set of deletions.
type DeleteConfirmation struct {
	RemoteDeletes int    `json:"remoteDeletes"`
	LocalDeletes  int    `json:"localDeletes"`
	Reason        string `json:"reason"`
	Digest        string `json:"digest"`
}

// NeedsConfirmationError is what a pass returns when the guard held its
// deletions back. Everything else in the pass ran.
type NeedsConfirmationError struct {
	Pair    string
	Pending DeleteConfirmation
}

func (e *NeedsConfirmationError) Error() string {
	what := "would delete"
	if e.Pending.Reason == GuardRemoteEmpty {
		what = "the server folder is suddenly empty; syncing would delete"
	}
	return fmt.Sprintf("%s %d file(s) on the server and %d on this computer; nothing was deleted. Confirm with `neocloud-lite pair confirm-deletes %s` or in the settings page",
		what, e.Pending.RemoteDeletes, e.Pending.LocalDeletes, e.Pair)
}

// errMarkerMissing protects a pair whose folder was replaced by an empty
// one (an unmounted drive's mount point, a restored backup elsewhere).
var errMarkerMissing = errors.New("folder looks unmounted or replaced: its .neocloud/pair-id marker is missing; not syncing. Remove and re-add the pair if this is intended")

// guardLimits are a pair's delete_guard_files and delete_guard_percent.
type guardLimits struct{ files, percent int }

// guardTrips decides whether deletions of n tracked files (of tracked)
// need a confirmation.
func (g guardLimits) trips(n, tracked int) bool {
	return n >= massDeleteMin && (n*100 > g.percent*tracked || n > g.files)
}

// evaluateGuard returns the confirmation a plan needs, or nil.
// remoteEmpty is true when the server folder has nothing left although
// files were tracked.
func (g guardLimits) evaluate(remoteDeletes, localDeletes, tracked int, remoteEmpty bool) *DeleteConfirmation {
	pending := DeleteConfirmation{RemoteDeletes: remoteDeletes, LocalDeletes: localDeletes}
	switch {
	case remoteEmpty && localDeletes > 0:
		pending.Reason = GuardRemoteEmpty
	case g.trips(remoteDeletes, tracked) || g.trips(localDeletes, tracked):
		pending.Reason = GuardMassDelete
	default:
		return nil
	}
	return &pending
}

// covers reports whether an approval allows a plan: the same reason and
// exactly the deletions the user saw.
func (c DeleteConfirmation) covers(plan DeleteConfirmation) bool {
	return c.Digest != "" && c.Reason == plan.Reason && c.Digest == plan.Digest &&
		plan.RemoteDeletes == c.RemoteDeletes && plan.LocalDeletes == c.LocalDeletes
}

// deletionDigest is a SHA-256 over a plan's deletions (side and path,
// sorted), binding an approval to the paths it was given for.
func deletionDigest(plan []step) string {
	var lines []string
	for _, s := range plan {
		switch s.action {
		case DeleteRemote:
			lines = append(lines, "remote\x00"+s.rel)
		case DeleteLocal:
			lines = append(lines, "local\x00"+s.rel)
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func encodeJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// checkMarker verifies (or, for a pair that never wrote one, writes) the
// root marker.
func checkMarker(root, pairID string, written bool) (bool, error) {
	marker := filepath.Join(root, metaDir, markerName)
	data, err := os.ReadFile(marker)
	if written {
		if err != nil || strings.TrimSpace(string(data)) != pairID {
			return false, errMarkerMissing
		}
		return false, nil
	}
	if err == nil && strings.TrimSpace(string(data)) == pairID {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return false, err
	}
	if err := writeFileSync(marker, []byte(pairID+"\n")); err != nil {
		return false, err
	}
	return true, nil
}

func writeFileSync(name string, data []byte) error {
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// moveToTrash moves a local file or folder (diskRel below root) into
// <root>/.neocloud/trash/<YYYY-MM-DD>/<diskRel>, adding " 2", " 3", … when
// that name is taken. User data is never unlinked directly.
func moveToTrash(root, diskRel string, now time.Time) (string, error) {
	source := filepath.Join(root, filepath.FromSlash(diskRel))
	if !insideRoot(root, source) {
		return "", errOutsideRoot
	}
	day := filepath.Join(root, metaDir, trashDir, now.Format(trashDayFormat))
	target := filepath.Join(day, filepath.FromSlash(diskRel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	dir, name := filepath.Dir(target), filepath.Base(target)
	free := uniqueName(name, func(candidate string) bool {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		return err == nil
	})
	target = filepath.Join(dir, free)
	if err := os.Rename(source, target); err != nil {
		return "", err
	}
	return target, nil
}

// pruneTrash removes trash days older than days; 0 keeps them all.
func pruneTrash(root string, now time.Time, days int) {
	if days <= 0 {
		return
	}
	dir := filepath.Join(root, metaDir, trashDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -days)
	for _, entry := range entries {
		day, err := time.ParseInLocation(trashDayFormat, entry.Name(), now.Location())
		if err != nil || !entry.IsDir() || !day.Before(cutoff) {
			continue
		}
		os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}

// parseConfirm reads a stored approval.
func parseConfirm(value string) (DeleteConfirmation, bool) {
	var c DeleteConfirmation
	if value == "" || json.Unmarshal([]byte(value), &c) != nil {
		return DeleteConfirmation{}, false
	}
	return c, true
}
