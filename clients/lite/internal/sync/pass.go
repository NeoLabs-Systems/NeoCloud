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
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

// pass is one reconciliation of a pair: the local scan, the remote view
// and the stored state are compared path by path and the differences are
// applied. A pass is idempotent; anything it cannot finish (a file that
// changed mid-transfer, a fresh conflict copy to upload) is picked up by
// the next one.
type pass struct {
	ctx    context.Context
	client *api.Client
	db     *state.DB
	pair   config.Pair
	device string
	logf   func(format string, args ...any)
	// note, when set, hears about every file the pass changed.
	note func(kind, rel string)

	local  map[string]*localItem // keyed by NFC path
	dirs   []string              // local folders, for the watcher
	disk   map[string]string     // NFC key → on-disk spelling where they differ
	remote *remoteView
	base   map[string]state.Entry
	ignore *Matcher

	// skip holds paths (and, implicitly, everything below them) that are
	// not synced this pass; notSynced says why.
	skip      map[string]bool
	notSynced []NotSynced
	// ignored are the paths a .cloudignore keeps out (for the report).
	ignored []NotSynced

	actions int  // changes made on either side
	again   bool // something is left for another pass
	errs    []error

	deleteRemote []string
	deleteLocal  []string
	// pending is set when the guard held this pass's deletions back.
	pending *DeleteConfirmation

	// Settings from the global config.
	guardLimits guardLimits
	bigFolder   int64 // bytes; 0 is off

	// excludes are the folders left out this pass: exclude_paths plus new
	// folders held back. newExcludes are new folders to exclude for good
	// (new_folders "skip"), asks the ones that wait for the user.
	excludes    []string
	newExcludes []string
	asks        []NewFolder
	// resolve holds the issues view's per-file choices; failed is what
	// this pass could not sync.
	resolve map[string]string
	failed  []Issue
}

// step is one planned action of a pass.
type step struct {
	rel    string
	action Action
}

var (
	errLocalChanged = errors.New("the local file changed during the transfer")
	errOutsideRoot  = errors.New("the path leads outside the pair's folder; not touched")
)

func (p *pass) noteActivity(kind, rel string) {
	if p.note != nil {
		p.note(kind, rel)
	}
}

func (p *pass) canUp() bool   { return p.pair.Direction != config.Down }
func (p *pass) canDown() bool { return p.pair.Direction != config.Up }

// full is the absolute on-disk path of an NFC key. Names that exist keep
// their spelling on disk; new ones are created in NFC. Keys are checked
// where they enter a pass (remotePathProblem, validBase); full checks
// again that the result stays inside the pair's folder before anything
// reads or writes it.
func (p *pass) full(rel string) (string, error) {
	full := filepath.Join(p.pair.Local, filepath.FromSlash(p.diskRel(rel)))
	if !insideRoot(p.pair.Local, full) {
		return "", errOutsideRoot
	}
	return full, nil
}

// insideRoot reports whether full lies below root (root itself excluded).
func insideRoot(root, full string) bool {
	rel, err := filepath.Rel(root, full)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

func (p *pass) diskRel(rel string) string {
	if disk, ok := p.disk[rel]; ok {
		return disk
	}
	parent := parentDir(rel)
	if parent == "" {
		return rel
	}
	return path.Join(p.diskRel(parent), path.Base(rel))
}

// skipped reports whether rel or a folder above it is not synced.
func (p *pass) skipped(rel string) bool {
	for {
		if p.skip[rel] {
			return true
		}
		if rel = parentDir(rel); rel == "" {
			return false
		}
	}
}

func (p *pass) fail(rel string, err error) {
	if errors.Is(err, context.Canceled) {
		p.errs = append(p.errs, err)
		return
	}
	p.logf("%s: %v", rel, err)
	p.errs = append(p.errs, fmt.Errorf("%s: %w", rel, err))
	both := false
	if p.remote != nil {
		_, remoteHas := p.remote.nodes[rel]
		both = remoteHas && p.local[rel] != nil
	}
	p.failed = append(p.failed, Issue{Path: rel, Detail: err.Error(), Both: both})
}

// run executes the pass against a tree snapshot. Deletions held back by
// the guard leave p.pending set; the returned error covers the rest.
func (p *pass) run(tree *api.Tree) error {
	if info, err := os.Stat(p.pair.Local); err != nil || !info.IsDir() {
		// An unmounted drive or deleted folder must not look like "the
		// user deleted every file".
		return fmt.Errorf("local folder %s is not available", p.pair.Local)
	}
	if err := p.checkMarker(); err != nil {
		return err
	}
	pruneTrash(p.pair.Local, time.Now(), p.pair.TrashDays())
	var err error
	if p.base, err = p.db.Entries(p.pair.ID); err != nil {
		return err
	}
	if err := p.takeResolutions(); err != nil {
		return err
	}
	p.dropUnsafeBase()
	if p.remote, err = buildRemoteView(p.ctx, p.client, tree, p.pair.Remote, len(p.base) == 0); err != nil {
		return err
	}
	if err := p.checkRoot(); err != nil {
		return err
	}
	p.ignore = MatcherFor(p.pair)
	p.ignore.load = p.cloudIgnoreRules
	remoteIgnored := p.remote.dropIgnored(p.ignore)
	if err := p.ignore.Err(); err != nil {
		return err // rules unknown: deciding anything now could be wrong
	}
	p.dropIgnoredBase()
	p.applyExcludes()
	if err := p.holdNewFolders(p.bigFolder); err != nil {
		return err
	}
	scan, err := scanLocal(p.pair.Local, p.ignore, func(rel string) bool { return p.remote.inVault(rel) || p.excluded(rel) })
	if err != nil {
		return err
	}
	if err := p.ignore.Err(); err != nil {
		return err
	}
	p.local, p.dirs, p.disk = scan.items, scan.dirs, scan.disk
	p.ignored = mergeIgnored(scan.ignored, remoteIgnored)
	p.findNotSynced(scan)

	if p.canDown() {
		p.followRemoteMoves()
	}
	if p.canUp() {
		p.followLocalMoves()
	}
	var plan []step
	for _, rel := range p.allPaths() {
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		if action, ok := p.decide(rel); ok {
			plan = append(plan, step{rel, action})
		}
	}
	held, err := p.guard(plan)
	if err != nil {
		return err
	}
	for _, s := range plan {
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		if held && (s.action == DeleteLocal || s.action == DeleteRemote) {
			continue
		}
		p.execute(s)
	}
	p.applyRemoteDeletes()
	p.applyLocalDeletes()
	return errors.Join(p.errs...)
}

// dropUnsafeBase forgets rows whose path could not name a file inside the
// pair (written by a version that trusted the server's paths). Only the
// rows go; nothing on disk is touched.
func (p *pass) dropUnsafeBase() {
	for rel := range p.base {
		if remotePathProblem(rel) != "" {
			p.logf("forgetting the unsafe path %q", rel)
			p.forget(rel)
		}
	}
}

// checkMarker verifies the root marker of a pair that wrote one, and
// writes it on the first sync.
func (p *pass) checkMarker() error {
	written, err := p.db.Meta(p.pair.ID, metaMarker)
	if err != nil {
		return err
	}
	wrote, err := checkMarker(p.pair.Local, p.pair.ID, written == "1")
	if err != nil || !wrote {
		return err
	}
	return p.db.SetMeta(p.pair.ID, metaMarker, "1")
}

// checkRoot notices a remote folder that was replaced by another one of
// the same name (deleted and created again, say). Its rows describe the
// old folder, so they are dropped: the pass then merges both sides like a
// first sync, which never deletes anything.
func (p *pass) checkRoot() error {
	stored, err := p.db.Meta(p.pair.ID, metaRootID)
	if err != nil {
		return err
	}
	if stored != "" && stored != p.remote.rootID && len(p.base) > 0 {
		p.logf("the server folder was replaced; merging both sides without deleting anything")
		if err := p.db.ClearEntries(p.pair.ID); err != nil {
			return err
		}
		p.base = map[string]state.Entry{}
	}
	return p.db.SetMeta(p.pair.ID, metaRootID, p.remote.rootID)
}

// findNotSynced collects what this pass leaves alone: links and special
// files, name clashes and, on Windows, names and paths Windows cannot hold.
func (p *pass) findNotSynced(scan *localScan) {
	list := append(append(append([]NotSynced(nil), p.notSynced...), scan.skipped...), p.remote.skipped...)
	list = append(list, p.tooLarge()...)
	local, remote, base := keySet(p.local), keySet(p.remote.nodes), keySet(p.base)
	list = append(list, clashes(local, remote, base, scan.spellings)...)
	if windowsNames {
		for _, rel := range sortedKeys(p.remote.nodes) {
			if p.local[rel] != nil {
				continue
			}
			full, _ := p.full(rel)
			if problem := windowsNameProblem(path.Base(rel)); problem != "" {
				list = append(list, NotSynced{Path: rel, Reason: ReasonInvalidName, Detail: "not synced: " + problem})
			} else if windowsPathTooLong(full) {
				list = append(list, NotSynced{Path: rel, Reason: ReasonPathTooLong, Detail: "not synced: the path is longer than Windows allows"})
			}
		}
	}
	p.skip = map[string]bool{}
	for _, item := range list {
		p.skip[item.Path] = true
	}
	for _, rel := range scan.hidden {
		p.skip[rel] = true // hidden files (include_hidden off): left alone on both sides, quietly
	}
	p.notSynced = list
}

func keySet[T any](m map[string]T) map[string]bool {
	set := make(map[string]bool, len(m))
	for key := range m {
		set[key] = true
	}
	return set
}

// guard counts the plan's deletions and holds them back when the pair
// needs a confirmation (docs/clients.md "Data safety"). A stored approval
// is spent by the first pass that gets here, whatever happens next: it
// lets through exactly the deletions it was given for, once.
func (p *pass) guard(plan []step) (bool, error) {
	remoteDeletes, localDeletes, tracked := 0, 0, 0
	for _, entry := range p.base {
		if entry.Type == "file" {
			tracked++
		}
	}
	for _, s := range plan {
		if entry, ok := p.base[s.rel]; !ok || entry.Type != "file" {
			continue
		}
		switch s.action {
		case DeleteRemote:
			remoteDeletes++
		case DeleteLocal:
			localDeletes++
		}
	}
	stored, err := p.db.Meta(p.pair.ID, metaConfirm)
	if err != nil {
		return false, err
	}
	if stored != "" {
		if err := p.db.SetMeta(p.pair.ID, metaConfirm, ""); err != nil {
			return false, err
		}
	}
	pending := p.guardLimits.evaluate(remoteDeletes, localDeletes, tracked, tracked > 0 && len(p.remote.nodes) == 0)
	if pending == nil {
		return false, nil
	}
	pending.Digest = deletionDigest(plan)
	if approved, ok := parseConfirm(stored); ok && approved.covers(*pending) {
		p.logf("deleting %d file(s) on the server and %d locally, as confirmed", remoteDeletes, localDeletes)
		return false, nil
	}
	p.logf("holding back %d deletion(s) on the server and %d locally until confirmed", remoteDeletes, localDeletes)
	p.pending = pending
	return true, nil
}

func (p *pass) allPaths() []string {
	seen := map[string]bool{}
	for rel := range p.local {
		seen[rel] = true
	}
	for rel := range p.remote.nodes {
		seen[rel] = true
	}
	for rel := range p.base {
		seen[rel] = true
	}
	paths := make([]string, 0, len(seen))
	for rel := range seen {
		if !p.skipped(rel) {
			paths = append(paths, rel)
		}
	}
	// Sorting puts every folder before what it contains.
	sort.Strings(paths)
	return paths
}

// localHash hashes a local file once per pass.
func (p *pass) localHash(rel string) (string, error) {
	item := p.local[rel]
	if item.hash == "" {
		full, err := p.full(rel)
		if err != nil {
			return "", err
		}
		hash, err := api.HashFile(full)
		if err != nil {
			return "", err
		}
		item.hash = hash
	}
	return item.hash, nil
}

func (p *pass) localSide(rel string) (Side, error) {
	item, ok := p.local[rel]
	entry, known := p.base[rel]
	switch {
	case !known && ok:
		return SideNew, nil
	case !known:
		return SideAbsent, nil
	case !ok:
		return SideDeleted, nil
	case item.dir != (entry.Type == "folder"):
		return SideChanged, nil
	case item.dir:
		return SideSame, nil
	case item.size == entry.LocalSize && item.mtime == entry.LocalMtime:
		return SideSame, nil
	}
	// Size or mtime moved; only a different hash makes it a change
	// (a `touch` or a copy-back of the same content is not one).
	hash, err := p.localHash(rel)
	if err != nil {
		return SideSame, err
	}
	if hash == entry.Etag {
		entry.LocalMtime, entry.LocalSize, entry.LocalHash = item.mtime, item.size, hash
		p.base[rel] = entry
		return SideSame, p.db.Put(p.pair.ID, entry)
	}
	return SideChanged, nil
}

func (p *pass) remoteSide(rel string) Side {
	node, ok := p.remote.nodes[rel]
	entry, known := p.base[rel]
	switch {
	case !known && ok:
		return SideNew
	case !known:
		return SideAbsent
	case !ok:
		return SideDeleted
	case node.Type != entry.Type:
		return SideChanged
	case node.IsFolder():
		return SideSame
	case node.ID != entry.NodeID || node.Etag != entry.Etag:
		return SideChanged
	}
	return SideSame
}

// decide settles one path's action. Bookkeeping (both sides already
// agree) happens right away; everything that touches a file is returned
// for the plan.
func (p *pass) decide(rel string) (Action, bool) {
	localSide, err := p.localSide(rel)
	if err != nil {
		p.fail(rel, err)
		return None, false
	}
	remoteSide := p.remoteSide(rel)
	item, node := p.local[rel], p.remote.nodes[rel]
	bothPresent := item != nil && localSide != SideDeleted && remoteSide != SideDeleted && remoteSide != SideAbsent

	if bothPresent && (localSide == SideNew || localSide == SideChanged) && (remoteSide == SideNew || remoteSide == SideChanged) {
		if same, err := p.sameContent(rel, item, node); err != nil {
			p.fail(rel, err)
			return None, false
		} else if same {
			p.record(rel, node)
			return None, false
		}
	}
	if bothPresent && localSide == SideSame && remoteSide == SideSame && node.ID != p.base[rel].NodeID {
		p.record(rel, node) // a folder replaced by one of the same name
	}

	bothFiles := bothPresent && !item.dir && !node.IsFolder()
	action := p.policy(rel, Decide(localSide, remoteSide, p.pair.Direction), item, bothFiles)
	if bothPresent && item.dir != node.IsFolder() && action == Upload {
		p.fail(rel, errors.New("a file and a folder share this name; rename one of them"))
		return None, false
	}
	return action, action != None
}

// execute runs one planned step. Deletions are queued and applied after
// all creations, so a folder is only removed once its content was handled.
func (p *pass) execute(s step) {
	switch s.action {
	case Upload:
		p.upload(s.rel)
	case Download:
		p.download(s.rel)
	case Conflict:
		p.conflict(s.rel)
	case DeleteLocal:
		p.deleteLocal = append(p.deleteLocal, s.rel)
	case DeleteRemote:
		p.deleteRemote = append(p.deleteRemote, s.rel)
	case Forget:
		p.forget(s.rel)
	case KeepLocal:
		p.keepLocal(s.rel)
	case KeepRemote:
		p.keepRemote(s.rel)
	}
}

// sameContent: two folders, or two files with the same hash, need no
// transfer even though both sides look new or changed.
func (p *pass) sameContent(rel string, item *localItem, node api.Node) (bool, error) {
	if item.dir || node.IsFolder() {
		return item.dir && node.IsFolder(), nil
	}
	if item.size != node.Size {
		return false, nil
	}
	hash, err := p.localHash(rel)
	return hash == node.Etag, err
}

// record stores "both sides agree" for rel.
func (p *pass) record(rel string, node api.Node) {
	entry := state.Entry{Path: rel, NodeID: node.ID, Type: node.Type, Etag: node.Etag, Size: node.Size}
	if item := p.local[rel]; item != nil {
		entry.LocalMtime, entry.LocalSize, entry.LocalHash = item.mtime, item.size, item.hash
		if !item.dir && entry.LocalHash == "" {
			entry.LocalHash = node.Etag
		}
	}
	p.base[rel] = entry
	if err := p.db.Put(p.pair.ID, entry); err != nil {
		p.fail(rel, err)
	}
}

func (p *pass) forget(rel string) {
	delete(p.base, rel)
	if err := p.db.Delete(p.pair.ID, rel); err != nil {
		p.fail(rel, err)
	}
}

// ensureRemoteDir returns the id of the remote folder at rel, creating it
// and any missing parents.
func (p *pass) ensureRemoteDir(rel string) (string, error) {
	if rel == "" {
		return p.remote.rootID, nil
	}
	if node, ok := p.remote.nodes[rel]; ok {
		if !node.IsFolder() {
			return "", fmt.Errorf("%s is a file on the server", rel)
		}
		return node.ID, nil
	}
	parentID, err := p.ensureRemoteDir(parentDir(rel))
	if err != nil {
		return "", err
	}
	node, err := p.client.CreateFolder(p.ctx, parentID, path.Base(rel))
	if err != nil {
		return "", err
	}
	p.actions++
	p.remote.put(rel, *node)
	if item := p.local[rel]; item != nil && item.dir {
		p.record(rel, *node)
	}
	return node.ID, nil
}

func (p *pass) upload(rel string) {
	item := p.local[rel]
	if item.dir {
		if _, err := p.ensureRemoteDir(rel); err != nil {
			p.fail(rel, err)
		}
		return
	}
	parentID, err := p.ensureRemoteDir(parentDir(rel))
	if err != nil {
		p.fail(rel, err)
		return
	}
	hash, err := p.localHash(rel)
	if err != nil {
		p.fail(rel, err)
		return
	}
	opts := api.UploadOptions{ParentID: parentID, Name: path.Base(rel), Conflict: "fail", SHA256: hash}
	if existing, ok := p.remote.nodes[rel]; ok && !existing.IsFolder() {
		opts.Name = existing.Name // the server's spelling, so replace finds it
		opts.Conflict = "replace"
		// In two-way mode only overwrite the version we compared against;
		// if someone changed it since, the server says 412 and the next
		// pass sees a conflict instead of silently losing their edit.
		if p.pair.Direction == config.Both {
			opts.BaseEtag = existing.Etag
		}
	}
	if pending, ok, _ := p.db.PendingUpload(p.pair.ID, rel); ok && pending.SHA256 == hash && pending.Size == item.size {
		opts.ResumeID = pending.ID
	}
	opts.OnSession = func(id string) {
		if err := p.db.SetPendingUpload(p.pair.ID, rel, state.Upload{ID: id, SHA256: hash, Size: item.size}); err != nil {
			p.logf("%s: remembering upload: %v", rel, err)
		}
	}
	full, err := p.full(rel)
	if err != nil {
		p.fail(rel, err)
		return
	}
	node, err := p.client.UploadFile(p.ctx, full, opts)
	if err != nil {
		if api.HasCode(err, "ETAG_MISMATCH") || api.HasCode(err, "NAME_EXISTS") || api.HasCode(err, "CHECKSUM_MISMATCH") {
			p.again = true // the other side moved meanwhile; the next pass re-decides
			p.logf("%s: %v (retrying)", rel, err)
			return
		}
		p.fail(rel, err)
		return
	}
	_ = p.db.ClearPendingUpload(p.pair.ID, rel)
	p.actions++
	p.logf("uploaded %s", rel)
	p.noteActivity(ActivityUpload, rel)
	p.remote.put(rel, *node)
	item.hash = node.Etag
	p.record(rel, *node)
}

func (p *pass) download(rel string) {
	node := p.remote.nodes[rel]
	full, err := p.full(rel)
	if err != nil {
		p.fail(rel, err)
		return
	}
	if node.IsFolder() {
		if err := os.MkdirAll(full, 0o755); err != nil {
			p.fail(rel, err)
			return
		}
		p.actions++
		p.refreshLocal(rel)
		p.record(rel, node)
		return
	}
	before := p.local[rel]
	err = p.client.DownloadFile(p.ctx, node.ID, full, node.Etag, func() error {
		// The user may have saved the file while it downloaded; their
		// version wins this round and the next pass sees a conflict.
		now, statErr := lstatItem(full)
		switch {
		case before == nil && statErr == nil:
			return errLocalChanged
		case before != nil && (statErr != nil || now.size != before.size || now.mtime != before.mtime):
			return errLocalChanged
		case before != nil:
			// The version being replaced goes to the local trash.
			_, err := moveToTrash(p.pair.Local, p.diskRel(rel), time.Now())
			return err
		}
		return nil
	})
	if errors.Is(err, errLocalChanged) {
		p.again = true
		return
	}
	if err != nil {
		p.fail(rel, err)
		return
	}
	if modified, ok := parseServerTime(node.ModifiedAt); ok {
		_ = os.Chtimes(full, modified, modified)
	}
	p.actions++
	p.logf("downloaded %s", rel)
	p.noteActivity(ActivityDownload, rel)
	p.refreshLocal(rel)
	if item := p.local[rel]; item != nil {
		item.hash = node.Etag
	}
	p.record(rel, node)
}

// conflict keeps both versions: the local file moves aside under a
// conflict name and the remote one takes the original name.
func (p *pass) conflict(rel string) {
	dir := parentDir(rel)
	taken := func(name string) bool {
		candidate := path.Join(dir, name)
		if _, remoteHas := p.remote.nodes[candidate]; remoteHas {
			return true
		}
		full, err := p.full(candidate)
		if err != nil {
			return true
		}
		_, err = os.Lstat(full)
		return err == nil
	}
	copyName := uniqueName(ConflictName(path.Base(rel), p.device, time.Now()), taken)
	copyRel := path.Join(dir, copyName)
	from, err := p.full(rel)
	if err != nil {
		p.fail(rel, err)
		return
	}
	to, err := p.full(copyRel)
	if err != nil {
		p.fail(rel, err)
		return
	}
	if err := os.Rename(from, to); err != nil {
		p.fail(rel, err)
		return
	}
	p.logf("conflict: kept the local %s as %s", rel, copyName)
	p.noteActivity(ActivityConflict, rel)
	p.actions++
	p.again = true // upload the copy (two-way mode) on the next pass
	p.dropLocal(rel)
	p.download(rel)
}

func (p *pass) refreshLocal(rel string) {
	full, err := p.full(rel)
	if err != nil {
		delete(p.local, rel)
		return
	}
	item, err := lstatItem(full)
	if err != nil {
		delete(p.local, rel)
		return
	}
	p.local[rel] = item
}

func (p *pass) dropLocal(rel string) {
	for other := range p.local {
		if other == rel || strings.HasPrefix(other, rel+"/") {
			delete(p.local, other)
		}
	}
}

// applyRemoteDeletes trashes nodes deleted locally. Shallowest first: a
// trashed folder takes its subtree with it, so its content needs no calls.
func (p *pass) applyRemoteDeletes() {
	sort.Strings(p.deleteRemote)
	var done []string
	for _, rel := range p.deleteRemote {
		if underAny(rel, done) {
			p.forget(rel)
			continue
		}
		// Something reappeared locally (a download into this folder, or
		// the user restored it): keep the remote side.
		full, err := p.full(rel)
		if err != nil {
			p.fail(rel, err)
			continue
		}
		if _, err := os.Lstat(full); err == nil {
			continue
		}
		node, ok := p.remote.nodes[rel]
		if !ok {
			p.forget(rel)
			continue
		}
		if err := p.client.Delete(p.ctx, node.ID); err != nil && !api.HasStatus(err, 404) {
			p.fail(rel, err)
			continue
		}
		p.actions++
		p.logf("deleted %s on the server", rel)
		p.noteActivity(ActivityDeleteRemote, rel)
		p.remote.remove(rel)
		p.forgetTree(rel)
		done = append(done, rel)
	}
}

// applyLocalDeletes moves what was deleted remotely into the local trash.
// Shallowest first: a folder whose content can all go moves as a whole,
// ignored files included, so the trash keeps it in one piece; a folder
// that still holds something to keep stays, and its tracked content is
// then handled one by one.
func (p *pass) applyLocalDeletes() {
	sort.Strings(p.deleteLocal)
	queued := make(map[string]bool, len(p.deleteLocal))
	for _, rel := range p.deleteLocal {
		queued[rel] = true
	}
	for _, rel := range p.deleteLocal {
		if _, back := p.remote.nodes[rel]; back {
			continue // recreated remotely this pass (e.g. for a re-uploaded child)
		}
		if _, tracked := p.base[rel]; !tracked {
			continue // went with a folder above it
		}
		full, err := p.full(rel)
		if err != nil {
			p.fail(rel, err)
			continue
		}
		now, err := lstatItem(full)
		if err != nil {
			p.forget(rel)
			continue
		}
		if now.dir {
			p.deleteLocalFolder(rel, full, queued)
			continue
		}
		// Never delete a file the user touched since the scan, and
		// never unlink one: it goes to the local trash.
		if seen := p.local[rel]; seen == nil || seen.dir || now.size != seen.size || now.mtime != seen.mtime {
			p.again = true
			continue
		}
		p.trashLocal(rel)
	}
}

// deleteLocalFolder removes a folder deleted remotely. Its content decides
// how: only empty folders and OS junk (.DS_Store, Thumbs.db, desktop.ini, Icon\r)
// are removed for good; anything else that may go (unchanged tracked
// files, ignored files) moves to the trash with the folder. Content to
// keep (a file changed since the scan, something new) keeps the folder;
// it is uploaded as new, or looked at again, on the next pass.
func (p *pass) deleteLocalFolder(rel, full string, queued map[string]bool) {
	var junk, dirs []string
	keep, worth := false, false
	walkErr := filepath.WalkDir(full, func(entryPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entryPath == full {
			return nil
		}
		inner, err := filepath.Rel(full, entryPath)
		if err != nil {
			return err
		}
		key := rel + "/" + nfc(filepath.ToSlash(inner))
		switch {
		case d.IsDir() && p.ignore.Match(key, true):
			worth = true
			return filepath.SkipDir
		case d.IsDir() && queued[key]:
			dirs = append(dirs, entryPath)
		case d.IsDir():
			keep = true
		case isOSJunk(d.Name()):
			junk = append(junk, entryPath)
		case p.ignore.Match(key, false):
			worth = true
		case queued[key] && p.unchangedFile(key, d):
			worth = true
		default:
			keep = true
		}
		if keep {
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		p.fail(rel, walkErr)
		return
	}
	if keep {
		return
	}
	if worth {
		p.trashLocal(rel)
		return
	}
	for _, file := range junk {
		os.Remove(file)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Remove(dirs[i])
	}
	if err := os.Remove(full); err != nil {
		p.again = true // something appeared meanwhile
		return
	}
	p.actions++
	p.logf("deleted the empty folder %s locally", rel)
	p.dropLocal(rel)
	p.forgetTree(rel)
}

// unchangedFile reports whether the tracked file at key is still what the
// scan saw.
func (p *pass) unchangedFile(key string, d fs.DirEntry) bool {
	seen := p.local[key]
	if seen == nil || seen.dir || !d.Type().IsRegular() {
		return false
	}
	info, err := d.Info()
	return err == nil && info.Size() == seen.size && info.ModTime().UnixNano() == seen.mtime
}

// trashLocal moves rel (a file, or a folder with everything in it) into
// the local trash and forgets it.
func (p *pass) trashLocal(rel string) {
	if _, err := moveToTrash(p.pair.Local, p.diskRel(rel), time.Now()); err != nil {
		p.fail(rel, err)
		return
	}
	p.actions++
	p.logf("deleted %s locally (kept in %s/%s)", rel, metaDir, trashDir)
	p.noteActivity(ActivityDeleteLocal, rel)
	p.dropLocal(rel)
	p.forgetTree(rel)
}

// osJunk are files the desktop itself drops into folders. They are the
// only files a sync pass ever removes without keeping them in the trash.
// A pass never removes the pair's own folder, so the icon files of its
// root stay.
var osJunk = []string{".DS_Store", "Thumbs.db", "desktop.ini", iconFile}

func isOSJunk(name string) bool {
	for _, junk := range osJunk {
		if strings.EqualFold(name, junk) {
			return true
		}
	}
	return false
}

func (p *pass) forgetTree(rel string) {
	for other := range p.base {
		if other == rel || strings.HasPrefix(other, rel+"/") {
			p.forget(other)
		}
	}
}

func underAny(rel string, roots []string) bool {
	for _, root := range roots {
		if strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

// parseServerTime reads SQLite's "YYYY-MM-DD HH:MM:SS" (UTC) or RFC 3339.
func parseServerTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
