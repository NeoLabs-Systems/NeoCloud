package sync

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"sort"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// How a pass applies .cloudignore files (docs/clients.md "Ignoring files").
//
// Both sides must use the same rules, or a file ignored on one side would
// look deleted on the other. So the rules of each .cloudignore are the
// version the pass is about to leave on both sides: the local file when
// it wins or both agree, the cloud's when it wins (fetched into memory
// before anything else is decided, so a new rule from another device
// stops the files it ignores from being downloaded in the same pass), and
// none when the file is about to be deleted. The file itself then syncs
// like any other.
//
// A path that becomes ignored stops syncing and both copies stay: its
// state row is forgotten, so nothing reads as deleted. When it is
// un-ignored again, both sides merge like a first sync: equal files are
// recorded, one-sided ones copied, and only real differences become
// conflict copies. Nothing is deleted either way.

// maxIgnoredReport caps the ignored paths a pass reports.
const maxIgnoredReport = 200

// cloudIgnoreRules returns the content of dir's .cloudignore that this
// pass applies (the matcher's loader).
func (p *pass) cloudIgnoreRules(dir string) ([]byte, error) {
	rel := path.Join(dir, CloudIgnoreName)
	full := filepath.Join(p.pair.Local, filepath.FromSlash(rel))
	data, item, err := readCloudIgnore(full)
	if err != nil && dir != "" && errors.Is(err, fs.ErrPermission) {
		// The folder's rules can't be known, so the folder is left alone
		// this pass (reported as unreadable); the rest of the pair syncs.
		p.unreadableDirs = append(p.unreadableDirs, dir)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	node, remoteHas := p.remote.nodes[rel]
	remoteHas = remoteHas && !node.IsFolder()
	entry, known := p.base[rel]
	hash := ""
	if item != nil && (known || remoteHas) {
		if hash, err = api.HashFile(full); err != nil {
			return nil, err
		}
	}
	if item != nil && remoteHas && hash == node.Etag {
		return data, nil // both sides agree
	}
	localSide := SideAbsent
	switch {
	case known && item == nil:
		localSide = SideDeleted
	case known && (entry.Type != "file" || hash != entry.Etag):
		localSide = SideChanged
	case known:
		localSide = SideSame
	case item != nil:
		localSide = SideNew
	}
	action := p.policy(rel, Decide(localSide, p.remoteSide(rel), p.pair.Direction), item, item != nil && remoteHas)
	switch action {
	case Download, Conflict, KeepRemote:
		return p.fetchCloudIgnore(node, remoteHas)
	case DeleteLocal, DeleteRemote:
		return nil, nil // about to go on both sides
	}
	if item != nil {
		return data, nil
	}
	// Only in the cloud, and staying there (an upload-only pair): its
	// rules still hold for the pair.
	return p.fetchCloudIgnore(node, remoteHas)
}

func (p *pass) fetchCloudIgnore(node api.Node, exists bool) ([]byte, error) {
	if !exists {
		return nil, nil
	}
	return p.client.ReadSmallFile(p.ctx, node.ID, node.Etag, maxCloudIgnoreSize)
}

// dropIgnoredBase forgets the rows of paths that are now ignored. Only
// the rows go: both copies stay where they are, untouched.
func (p *pass) dropIgnoredBase() {
	dropped := 0
	for _, rel := range sortedKeys(p.base) {
		entry, ok := p.base[rel]
		if ok && p.ignore.Match(rel, entry.Type == "folder") {
			p.forget(rel)
			dropped++
		}
	}
	if dropped > 0 {
		p.logf("stopped syncing %d ignored item(s); both copies stay", dropped)
	}
}

// ignoredItem is the report line for a path a .cloudignore keeps out;
// paths inside an ignored folder, and the fixed and settings rules,
// are not reported.
func ignoredItem(rel string, v Verdict) (NotSynced, bool) {
	if !v.Ignored || v.Rule == nil || !v.Rule.FromFile() || v.At != rel {
		return NotSynced{}, false
	}
	return NotSynced{Path: rel, Reason: ReasonIgnored, Detail: v.Reason()}, true
}

// mergeIgnored joins the ignored paths seen on both sides, one line per
// path, sorted and capped.
func mergeIgnored(lists ...[]NotSynced) []NotSynced {
	seen := map[string]bool{}
	var out []NotSynced
	for _, list := range lists {
		for _, item := range list {
			if !seen[item.Path] {
				seen[item.Path] = true
				out = append(out, item)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) > maxIgnoredReport {
		out = out[:maxIgnoredReport]
	}
	return out
}
