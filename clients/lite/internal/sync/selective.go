package sync

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// Selective sync, new-folder prompts and the per-pair policies of
// docs/clients.md "Folder settings".

const (
	ReasonExcluded = "excluded"
	ReasonTooLarge = "tooLarge"

	// Per-pair state (state.DB meta keys).
	metaAllowed = "allowed" // new folders the user chose to sync, JSON list
	metaResolve = "resolve" // per-file choices from the issues view, JSON map
)

// Choices of the issues view for a file that failed to sync.
const (
	ResolveLocal  = "local"
	ResolveRemote = "remote"
	ResolveBoth   = "both"
)

// NewFolder is a new folder at the top of the pair's remote folder that
// waits for the user (new_folders "ask", or bigger than the big-folder
// limit).
type NewFolder struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Issue is a file the last pass could not sync. Both is set when it
// exists on both sides, so keeping one version is possible.
type Issue struct {
	Path   string `json:"path"`
	Detail string `json:"detail"`
	Both   bool   `json:"both"`
}

// excluded reports whether rel is in an excluded folder (or one held for
// a new-folder prompt). Names compare like the server's: without case.
func (p *pass) excluded(rel string) bool {
	key := foldKey(rel)
	for _, ex := range p.excludes {
		ex = foldKey(ex)
		if key == ex || strings.HasPrefix(key, ex+"/") {
			return true
		}
	}
	return false
}

// applyExcludes takes the pair's excluded folders out of the pass. Their
// remote side is left alone; a local copy this client synced goes to
// the local trash, unless it holds something not in NeoCloud (or the pair
// only uploads, where the local folder is the original).
func (p *pass) applyExcludes() {
	for _, raw := range p.pair.ExcludePaths {
		ex := nfc(raw)
		p.excludes = append(p.excludes, ex)
		p.remote.removeFold(ex)
		if p.canDown() {
			p.releaseExcluded(ex)
		}
		for rel := range p.base {
			if key := foldKey(rel); key == foldKey(ex) || strings.HasPrefix(key, foldKey(ex)+"/") {
				p.forget(rel)
			}
		}
	}
}

func (p *pass) releaseExcluded(ex string) {
	full, err := p.full(ex)
	if err != nil {
		return
	}
	if info, err := os.Lstat(full); err != nil || !info.IsDir() {
		return
	}
	clean, ignored := p.onlySyncedContent(ex, full)
	if !clean {
		p.notSynced = append(p.notSynced, NotSynced{Path: ex, Reason: ReasonExcluded,
			Detail: "not synced: excluded, and kept here because it holds files that are not in NeoCloud"})
		return
	}
	if _, err := moveToTrash(p.pair.Local, ex, time.Now(), ignored); err != nil {
		p.fail(ex, err)
		return
	}
	p.actions++
	p.logf("stopped syncing %s; the local copy is in %s/%s", ex, metaDir, trashDir)
}

// onlySyncedContent reports whether everything under the local folder ex
// is a synced, unchanged file, an ignored file or OS junk, and whether
// there are ignored files (which the cloud has no copy of).
func (p *pass) onlySyncedContent(ex, full string) (clean, ignored bool) {
	clean = true
	filepath.WalkDir(full, func(entryPath string, d fs.DirEntry, err error) error {
		if err != nil {
			clean = false
			return filepath.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		inner, _ := filepath.Rel(full, entryPath)
		key := ex + "/" + nfc(filepath.ToSlash(inner))
		entry, tracked := p.base[key]
		info, infoErr := d.Info()
		switch {
		case isOSJunk(d.Name()):
		case p.ignore.Match(key, false):
			ignored = true
		case tracked && infoErr == nil && d.Type().IsRegular() && entry.Type == "file" &&
			info.Size() == entry.LocalSize && info.ModTime().UnixNano() == entry.LocalMtime:
		default:
			clean = false
			return filepath.SkipAll
		}
		return nil
	})
	return clean, ignored
}

// holdNewFolders applies new_folders and the big-folder limit to folders
// that appeared at the top of the remote folder since the first sync:
// "skip" excludes them for good (the engine saves that), "ask" and big
// ones wait in p.asks. Either way they are not synced this pass.
func (p *pass) holdNewFolders(bigFolder int64) error {
	if len(p.base) == 0 || !p.canDown() {
		return nil // the first sync takes what the user picked
	}
	allowed, err := p.allowedFolders()
	if err != nil {
		return err
	}
	policy := p.pair.NewFolderPolicy()
	for rel, node := range p.remote.nodes {
		if strings.Contains(rel, "/") || !node.IsFolder() || allowed[rel] {
			continue
		}
		if _, known := p.base[rel]; known {
			continue
		}
		if full, err := p.full(rel); err != nil {
			continue
		} else if _, err := os.Lstat(full); err == nil {
			continue // it exists here too: this side made it
		}
		size := p.remote.sizeUnder(rel)
		switch {
		case policy == config.NewFoldersSkip:
			p.newExcludes = append(p.newExcludes, node.Name)
			p.logf("not syncing the new folder %s (new folders: skip)", rel)
		case policy == config.NewFoldersAsk || (bigFolder > 0 && size > bigFolder):
			p.asks = append(p.asks, NewFolder{Path: node.Name, Size: size})
		default:
			continue
		}
		p.excludes = append(p.excludes, rel)
		p.remote.remove(rel)
	}
	return nil
}

func (p *pass) allowedFolders() (map[string]bool, error) {
	value, err := p.db.Meta(p.pair.ID, metaAllowed)
	if err != nil || value == "" {
		return map[string]bool{}, err
	}
	var list []string
	json.Unmarshal([]byte(value), &list)
	allowed := map[string]bool{}
	var keep []string
	for _, rel := range list {
		if _, synced := p.base[rel]; synced {
			continue // synced now, so no longer new
		}
		allowed[rel] = true
		keep = append(keep, rel)
	}
	if len(keep) != len(list) {
		return allowed, p.db.SetMeta(p.pair.ID, metaAllowed, encodeJSON(keep))
	}
	return allowed, nil
}

// takeResolutions loads (and clears) the issues view's choices.
func (p *pass) takeResolutions() error {
	value, err := p.db.Meta(p.pair.ID, metaResolve)
	if err != nil || value == "" {
		return err
	}
	json.Unmarshal([]byte(value), &p.resolve)
	return p.db.SetMeta(p.pair.ID, metaResolve, "")
}

// tooLarge lists files over max_file_size_mb on either side.
func (p *pass) tooLarge() []NotSynced {
	limit := p.pair.MaxFileSize()
	if limit <= 0 {
		return nil
	}
	detail := fmt.Sprintf("not synced: larger than the %d MB limit of this folder", p.pair.MaxFileSizeMB)
	seen := map[string]bool{}
	var list []NotSynced
	for _, rel := range sortedKeys(p.local) {
		if item := p.local[rel]; !item.dir && item.size > limit {
			seen[rel] = true
			list = append(list, NotSynced{Path: rel, Reason: ReasonTooLarge, Detail: detail})
		}
	}
	for _, rel := range sortedKeys(p.remote.nodes) {
		if node := p.remote.nodes[rel]; !seen[rel] && !node.IsFolder() && node.Size > limit {
			list = append(list, NotSynced{Path: rel, Reason: ReasonTooLarge, Detail: detail})
		}
	}
	return list
}

// policy turns the plain merge decision into what the pair's settings
// and the user's per-file choices ask for.
func (p *pass) policy(rel string, action Action, item *localItem, bothFiles bool) Action {
	if choice, ok := p.resolve[rel]; ok && bothFiles {
		switch choice {
		case ResolveLocal:
			return KeepLocal
		case ResolveRemote:
			return KeepRemote
		case ResolveBoth:
			return Conflict
		}
	}
	if action == Conflict && bothFiles && p.pair.Direction == config.Both {
		switch p.pair.ConflictPolicy() {
		case config.ConflictPreferLocal:
			return KeepLocal
		case config.ConflictPreferRemote:
			return KeepRemote
		case config.ConflictNewer:
			if modified, ok := parseServerTime(p.remote.nodes[rel].ModifiedAt); ok && item.mtime > modified.UnixNano() {
				return KeepLocal
			}
			return KeepRemote
		}
	}
	if !p.pair.MirrorDeletes() {
		both := p.pair.Direction == config.Both
		switch {
		case action == DeleteRemote && both:
			return Download // deleted here: it comes back from the cloud
		case action == DeleteLocal && both:
			return Upload // deleted in the cloud: it goes back up
		case action == DeleteRemote || action == DeleteLocal:
			return Forget // the receiving side keeps its copy
		}
	}
	return action
}

// keepLocal settles a conflict for the local version: the cloud version
// is saved to the local trash, then replaced.
func (p *pass) keepLocal(rel string) {
	node := p.remote.nodes[rel]
	if err := p.client.DownloadFile(p.ctx, node.ID, p.trashTarget(rel), node.Etag, nil); err != nil {
		p.fail(rel, err)
		return
	}
	p.logf("conflict: kept the local %s; the cloud version is in %s/%s", rel, metaDir, trashDir)
	p.noteActivity(ActivityConflict, rel)
	p.upload(rel)
}

// keepRemote settles a conflict for the cloud version; download moves
// the local one to the trash.
func (p *pass) keepRemote(rel string) {
	p.logf("conflict: kept the cloud version of %s; the local one is in %s/%s", rel, metaDir, trashDir)
	p.noteActivity(ActivityConflict, rel)
	p.download(rel)
}

// trashTarget is a free path for rel in today's local trash.
func (p *pass) trashTarget(rel string) string {
	day := filepath.Join(p.pair.Local, metaDir, trashDir, time.Now().Format(trashDayFormat))
	dir := filepath.Join(day, filepath.FromSlash(parentDir(rel)))
	name := uniqueName(path.Base(rel), func(candidate string) bool {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		return err == nil
	})
	return filepath.Join(dir, name)
}

// removeFold drops rel and everything below it, comparing without case.
func (v *remoteView) removeFold(rel string) {
	key := foldKey(rel)
	for p, node := range v.nodes {
		if other := foldKey(p); other == key || strings.HasPrefix(other, key+"/") {
			delete(v.nodes, p)
			delete(v.paths, node.ID)
		}
	}
}

// sizeUnder adds up the files below rel.
func (v *remoteView) sizeUnder(rel string) int64 {
	var total int64
	for p, node := range v.nodes {
		if strings.HasPrefix(p, rel+"/") && !node.IsFolder() {
			total += node.Size
		}
	}
	return total
}
