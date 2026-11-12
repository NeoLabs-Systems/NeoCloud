package sync

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

// A synced file that the user moves into an ignored (or excluded) folder
// disappears from the scan, and a plain three-way merge would delete it
// in the cloud, the way git drops a file moved into an ignored folder.
// For people who don't think in git terms that reads as "NeoCloud lost
// my file". So the move is recognised (the same content turns up in an
// ignored place) and the file stops syncing instead: its row is parked,
// the cloud copy stays where it was, and the path is reported as
// movedIgnored. A parked row comes back when the file reappears at its
// old path (an editor's save-via-rename) or changes in the cloud; it is
// dropped when the cloud copy goes away.

// metaDetached holds the parked rows, a JSON map path → detachedEntry.
const metaDetached = "detached"

// maxIgnoredWalk bounds the entries looked at in ignored folders when
// searching for moved files (build output, node_modules…).
const maxIgnoredWalk = 100_000

// detachedEntry is a parked row and where its local copy went.
type detachedEntry struct {
	Entry state.Entry `json:"entry"`
	To    string      `json:"to"`
}

func (p *pass) loadDetached() (map[string]detachedEntry, error) {
	value, err := p.db.Meta(p.pair.ID, metaDetached)
	if err != nil || value == "" {
		return map[string]detachedEntry{}, err
	}
	parked := map[string]detachedEntry{}
	if err := json.Unmarshal([]byte(value), &parked); err != nil {
		return map[string]detachedEntry{}, nil // unreadable: forget them, nothing is deleted for it
	}
	return parked, nil
}

func (p *pass) saveDetached() error {
	if len(p.detached) == 0 {
		return p.db.SetMeta(p.pair.ID, metaDetached, "")
	}
	return p.db.SetMeta(p.pair.ID, metaDetached, encodeJSON(p.detached))
}

// applyDetached checks the parked rows against this pass: one whose
// cloud copy is unchanged and whose old local path is still empty stays
// parked (and is left alone on both sides); one whose file is back, or
// changed in the cloud, becomes a normal row again; one whose cloud copy
// is gone is dropped. Folders are checked after the files in them.
func (p *pass) applyDetached() error {
	parked, err := p.loadDetached()
	if err != nil {
		return err
	}
	p.detached = parked
	if len(parked) == 0 {
		return nil
	}
	paths := sortedKeys(parked)
	sort.Sort(sort.Reverse(sort.StringSlice(paths))) // files and subfolders before their folder
	for _, rel := range paths {
		item := parked[rel]
		node, remoteHas := p.remote.nodes[rel]
		switch {
		case !remoteHas || node.ID != item.Entry.NodeID || node.Type != item.Entry.Type:
			delete(p.detached, rel) // the cloud copy went or was replaced
		case p.local[rel] != nil || node.Etag != item.Entry.Etag || !p.onlyParkedBelow(rel):
			delete(p.detached, rel)
			p.base[rel] = item.Entry
			if err := p.db.Put(p.pair.ID, item.Entry); err != nil {
				return err
			}
		default:
			if _, known := p.base[rel]; known {
				p.forget(rel) // parked before a crash could drop the row
			}
			p.skip[rel] = true
			p.notSynced = append(p.notSynced, movedIgnoredNotice(rel, item.To))
		}
	}
	return p.saveDetached()
}

// onlyParkedBelow reports whether everything the cloud holds below the
// folder rel is parked too (true for a file).
func (p *pass) onlyParkedBelow(rel string) bool {
	prefix := rel + "/"
	for other := range p.remote.nodes {
		if strings.HasPrefix(other, prefix) {
			if _, ok := p.detached[other]; !ok {
				return false
			}
		}
	}
	return true
}

func movedIgnoredNotice(rel, to string) NotSynced {
	detail := "not synced: moved into an ignored folder on this computer; the cloud copy stays"
	if to != "" {
		detail = "not synced: moved to " + to + " (ignored) on this computer; the cloud copy stays"
	}
	return NotSynced{Path: rel, Reason: ReasonMovedIgnored, Detail: detail}
}

// detachMovedIntoIgnored parks the rows of synced files that vanished
// from their path and turn up, byte for byte, in an ignored or excluded
// place, instead of deleting them in the cloud. A synced folder that is
// gone locally and only held parked files is parked with them.
func (p *pass) detachMovedIntoIgnored() error {
	candidates := map[int64][]string{}
	var order []string
	for _, rel := range sortedKeys(p.base) {
		entry := p.base[rel]
		if entry.Type != "file" || p.local[rel] != nil || p.skipped(rel) {
			continue
		}
		node, ok := p.remote.nodes[rel]
		if !ok || node.ID != entry.NodeID || node.Etag != entry.Etag {
			continue
		}
		if p.policy(rel, Decide(SideDeleted, SideSame, p.pair.Direction), nil, false) != DeleteRemote {
			continue
		}
		candidates[entry.LocalSize] = append(candidates[entry.LocalSize], rel)
		order = append(order, rel)
	}
	if len(candidates) == 0 || len(p.ignoredRoots) == 0 {
		return nil
	}
	found := p.ignoredFilesBySize(candidates)
	parked := false
	hashes := map[string]string{}
	for _, rel := range order {
		entry := p.base[rel]
		files := found[entry.LocalSize]
		for i, full := range files {
			hash, ok := hashes[full]
			if !ok {
				hash, _ = api.HashFile(full)
				hashes[full] = hash
			}
			if hash != entry.Etag {
				continue
			}
			found[entry.LocalSize] = append(files[:i:i], files[i+1:]...)
			p.park(rel, entry, full)
			parked = true
			break
		}
	}
	if !parked {
		return nil
	}
	p.parkEmptiedFolders()
	return p.saveDetached()
}

// park stops syncing rel, whose local copy is now at full (ignored).
func (p *pass) park(rel string, entry state.Entry, full string) {
	to := ""
	if inner, err := filepath.Rel(p.pair.Local, full); err == nil {
		to = filepath.ToSlash(inner)
	}
	p.detached[rel] = detachedEntry{Entry: entry, To: to}
	p.forget(rel)
	p.skip[rel] = true
	p.notSynced = append(p.notSynced, movedIgnoredNotice(rel, to))
	p.logf("%s was moved into an ignored folder (%s); it stops syncing and the cloud copy stays", rel, to)
}

// parkEmptiedFolders parks synced folders that are gone locally when all
// the cloud holds below them is parked, so they are not deleted (or
// recreated empty) either.
func (p *pass) parkEmptiedFolders() {
	folders := sortedKeys(p.base)
	sort.Sort(sort.Reverse(sort.StringSlice(folders)))
	for _, rel := range folders {
		entry := p.base[rel]
		if entry.Type != "folder" || p.local[rel] != nil || p.skipped(rel) {
			continue
		}
		node, ok := p.remote.nodes[rel]
		if !ok || node.ID != entry.NodeID || !p.onlyParkedBelow(rel) || !p.hasParkedBelow(rel) {
			continue
		}
		p.detached[rel] = detachedEntry{Entry: entry}
		p.forget(rel)
		p.skip[rel] = true
	}
}

func (p *pass) hasParkedBelow(rel string) bool {
	for other := range p.detached {
		if strings.HasPrefix(other, rel+"/") {
			return true
		}
	}
	return false
}

// ignoredFilesBySize walks the ignored and excluded places of the scan
// and returns their regular files whose size one of the candidates has.
func (p *pass) ignoredFilesBySize(candidates map[int64][]string) map[int64][]string {
	found := map[int64][]string{}
	seen := 0
	stop := errors.New("enough")
	for _, root := range p.ignoredRoots {
		err := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
			if seen++; seen > maxIgnoredWalk {
				return stop
			}
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if strings.EqualFold(d.Name(), metaDir) {
					return filepath.SkipDir // the local trash holds copies of synced files
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if _, wanted := candidates[info.Size()]; wanted {
				found[info.Size()] = append(found[info.Size()], full)
			}
			return nil
		})
		if errors.Is(err, stop) {
			break
		}
	}
	return found
}
