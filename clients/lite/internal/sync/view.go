package sync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// localItem is one file or folder found under a pair's local root.
type localItem struct {
	dir   bool
	size  int64
	mtime int64 // UnixNano
	hash  string
}

// localScan is what scanLocal found under a pair's root.
type localScan struct {
	items map[string]*localItem // keyed by NFC path
	dirs  []string              // absolute folder paths, for the watcher
	// disk maps an NFC key to its spelling on disk where the two differ.
	disk map[string]string
	// spellings lists every on-disk spelling of a key; more than one is a clash.
	spellings map[string][]string
	skipped   []NotSynced
	// hidden are items Windows marks hidden or system, skipped when the
	// pair leaves hidden files out.
	hidden []string
	// ignored are the paths a .cloudignore kept out, for the report.
	ignored []NotSynced
	// ignoredRoots are the absolute paths the scan did not enter because
	// they are ignored or excluded (outside the metadata folder), where a
	// tracked file that vanished may have been moved to.
	ignoredRoots []string
}

// scanLocal walks the pair's folder. Symlinks and special files are
// skipped (and reported): following links could leave the pair or loop
// forever. Paths are keyed by their NFC form.
//
// A folder that cannot be read (a permission error, a network drive that
// drops out) is reported as unreadable and its subtree is left alone on
// both sides: what the scan could not see must never read as deleted.
// A root that is a symbolic link (or a Windows junction) is walked at its
// target; paths stay under root.
func scanLocal(root string, ignore *Matcher, excluded func(string) bool) (*localScan, error) {
	scan := &localScan{
		items:     map[string]*localItem{},
		dirs:      []string{root},
		disk:      map[string]string{},
		spellings: map[string][]string{},
	}
	walkRoot, err := walkableRoot(root)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(walkRoot, func(walked string, d fs.DirEntry, err error) error {
		if walked == walkRoot {
			return err
		}
		raw, relErr := filepath.Rel(walkRoot, walked)
		if relErr != nil {
			return relErr
		}
		full := filepath.Join(root, raw)
		raw = filepath.ToSlash(raw)
		key := nfc(raw)
		if err != nil {
			scan.unreadable(key, err)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		verdict := ignore.Explain(key, d.IsDir())
		if verdict.Ignored || excluded(key) {
			if item, ok := ignoredItem(key, verdict); ok {
				scan.ignored = append(scan.ignored, item)
			}
			if !strings.EqualFold(path.Base(key), metaDir) {
				scan.ignoredRoots = append(scan.ignoredRoots, full)
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if ignore.hidesAttr(key, d.IsDir()) && hasHiddenAttr(full) {
			scan.hidden = append(scan.hidden, key)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			scan.skipped = append(scan.skipped, NotSynced{Path: key, Reason: ReasonSymlink, Detail: "not synced: symbolic links are skipped"})
			return nil
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			scan.skipped = append(scan.skipped, NotSynced{Path: key, Reason: ReasonSpecialFile, Detail: "not synced: not a regular file"})
			return nil
		}
		scan.spellings[key] = append(scan.spellings[key], raw)
		if len(scan.spellings[key]) > 1 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			if !errors.Is(infoErr, fs.ErrNotExist) {
				scan.unreadable(key, infoErr) // gone meanwhile is simply gone
			}
			return nil
		}
		item := &localItem{dir: d.IsDir(), mtime: info.ModTime().UnixNano()}
		if !item.dir {
			item.size = info.Size()
		} else {
			scan.dirs = append(scan.dirs, full)
		}
		scan.items[key] = item
		if key != raw {
			scan.disk[key] = raw
		}
		return nil
	})
	return scan, err
}

// unreadable reports a path the scan could not look at.
func (scan *localScan) unreadable(key string, err error) {
	detail := "not synced: could not be read on this computer"
	if errors.Is(err, fs.ErrPermission) {
		detail += " (permission denied)"
	}
	scan.skipped = append(scan.skipped, NotSynced{Path: key, Reason: ReasonUnreadable, Detail: detail})
}

// walkableRoot is the folder to walk for root: root itself, or the target
// of a root that is a link (WalkDir would not enter one, and the whole
// pair would look empty).
func walkableRoot(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return root, nil
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return "", fmt.Errorf("local folder %s is not a folder", root)
	}
	return resolved, nil
}

// remoteView is the part of the account tree that belongs to one pair,
// keyed by path relative to the pair's remote folder.
type remoteView struct {
	rootID string
	nodes  map[string]api.Node
	paths  map[string]string // node id → relative path
	// vaults are relative paths of end-to-end encrypted folders. Nothing
	// inside them is synced, in either direction.
	vaults []string
	// skipped are remote paths left out: names that normalize to a path
	// already taken, and paths that could not name a file inside the pair.
	skipped []NotSynced
	// all holds every node below the pair's folder, by relative path,
	// including those the pass leaves out (vaults, invalid or clashing
	// names, ignored and excluded paths).
	all map[string]string
}

var errRemoteRootMissing = errors.New("the remote folder of this pair no longer exists; re-add the pair to start over")

// buildRemoteView filters the snapshot down to the pair (ignore rules
// apply afterwards, see dropIgnored). When the pair's
// folder is missing it is created, but only for a pair that never synced:
// for a synced pair a missing folder must not read as "delete everything".
func buildRemoteView(ctx context.Context, client *api.Client, tree *api.Tree, remote string, neverSynced bool) (*remoteView, error) {
	view := &remoteView{rootID: tree.RootID, nodes: map[string]api.Node{}, paths: map[string]string{}, all: map[string]string{}}
	prefix := ""
	if remote != "" {
		root, ok := findFolder(tree, remote)
		switch {
		case ok && root.VaultID != "":
			return nil, fmt.Errorf("%s is a vault folder; vaults are not synced by the lite client", remote)
		case ok:
			view.rootID = root.ID
			prefix = root.Path + "/"
		case neverSynced:
			created, err := client.EnsureFolderPath(ctx, api.RootAlias, remote)
			if err != nil {
				return nil, fmt.Errorf("creating %s on the server: %w", remote, err)
			}
			view.rootID = created.ID
			return view, nil
		default:
			return nil, errRemoteRootMissing
		}
	}
	for _, node := range tree.Nodes {
		if node.ID == view.rootID || !strings.HasPrefix(node.Path, prefix) {
			continue
		}
		raw := strings.TrimPrefix(node.Path, prefix)
		rel := nfc(raw)
		view.all[rel] = node.ID
		if problem := remotePathProblem(rel); problem != "" {
			view.skipped = append(view.skipped, NotSynced{Path: raw, Reason: ReasonInvalidName, Detail: problem})
			continue
		}
		if node.VaultID != "" || view.inVault(rel) {
			if node.IsFolder() && !view.inVault(rel) {
				view.vaults = append(view.vaults, rel)
			}
			continue
		}
		if taken, ok := view.nodes[rel]; ok {
			view.skipped = append(view.skipped, NotSynced{Path: raw, Reason: ReasonNameClash,
				Detail: "not synced: name clashes with " + taken.Name + " on the server"})
			continue
		}
		view.put(rel, node)
	}
	return view, nil
}

// dropIgnored takes out what the matcher ignores and returns the paths a
// .cloudignore kept out, for the report.
func (v *remoteView) dropIgnored(ignore *Matcher) []NotSynced {
	var ignored []NotSynced
	for _, rel := range sortedKeys(v.nodes) {
		node := v.nodes[rel]
		verdict := ignore.Explain(rel, node.IsFolder())
		if !verdict.Ignored {
			continue
		}
		delete(v.nodes, rel)
		delete(v.paths, node.ID)
		if item, ok := ignoredItem(rel, verdict); ok {
			ignored = append(ignored, item)
		}
	}
	return ignored
}

// findFolder looks a folder up by path. The server compares names without
// case, so the pair's path does too.
func findFolder(tree *api.Tree, remote string) (api.Node, bool) {
	for _, node := range tree.Nodes {
		if node.IsFolder() && strings.EqualFold(nfc(node.Path), nfc(remote)) {
			return node, true
		}
	}
	return api.Node{}, false
}

func (v *remoteView) put(rel string, node api.Node) {
	v.nodes[rel] = node
	v.paths[node.ID] = rel
}

func (v *remoteView) remove(rel string) {
	for p, node := range v.nodes {
		if p == rel || strings.HasPrefix(p, rel+"/") {
			delete(v.nodes, p)
			delete(v.paths, node.ID)
		}
	}
}

// keeps names a node below the folder rel that is not being deleted
// (its id is not in deleting), or returns "" when there is none.
func (v *remoteView) keeps(rel string, deleting map[string]bool) string {
	prefix := rel + "/"
	var found []string
	for p, id := range v.all {
		if strings.HasPrefix(p, prefix) && !deleting[id] {
			found = append(found, p)
		}
	}
	for p, node := range v.nodes {
		if strings.HasPrefix(p, prefix) && !deleting[node.ID] {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		return ""
	}
	sort.Strings(found)
	return found[0]
}

func (v *remoteView) inVault(rel string) bool {
	for _, vault := range v.vaults {
		if rel == vault || strings.HasPrefix(rel, vault+"/") {
			return true
		}
	}
	return false
}

// parentDir is path.Dir with "" for the pair root.
func parentDir(rel string) string {
	dir := path.Dir(rel)
	if dir == "." {
		return ""
	}
	return dir
}

// lstatItem re-reads one path, for last-moment checks before destructive steps.
func lstatItem(full string) (*localItem, error) {
	info, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	item := &localItem{dir: info.IsDir(), mtime: info.ModTime().UnixNano()}
	if !item.dir {
		item.size = info.Size()
	}
	return item, nil
}
