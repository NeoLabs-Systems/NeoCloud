package sync

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

// Renames and moves are recognised before the per-path pass. Without this
// a rename would be a delete plus a create: a full re-transfer on the
// other side, and the file's version history lost on the server.

// followRemoteMoves renames local files and folders whose node moved on
// the server (same node id, new path).
func (p *pass) followRemoteMoves() {
	for _, from := range sortedKeys(p.base) {
		entry, ok := p.base[from]
		if !ok {
			continue // re-keyed with a folder moved earlier in this loop
		}
		to, ok := p.remote.paths[entry.NodeID]
		if !ok || to == from || p.skipped(from) || p.skipped(to) {
			continue
		}
		item := p.local[from]
		if item == nil || item.dir != (entry.Type == "folder") {
			continue
		}
		if _, taken := p.local[to]; taken {
			continue
		}
		if _, taken := p.base[to]; taken {
			continue
		}
		fromDisk, toDisk := p.diskRel(from), p.diskRel(to)
		fromFull, err := p.full(from)
		if err != nil {
			p.fail(from, err)
			continue
		}
		toFull, err := p.full(to)
		if err != nil {
			p.fail(to, err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(toFull), 0o755); err != nil {
			p.fail(to, err)
			continue
		}
		if err := os.Rename(fromFull, toFull); err != nil {
			p.fail(from, err)
			continue
		}
		rekey(p.disk, from, to, func(disk string, _ string) string { return toDisk + strings.TrimPrefix(disk, fromDisk) })
		if err := p.db.Move(p.pair.ID, from, to); err != nil {
			p.fail(to, err)
		}
		rekey(p.base, from, to, func(e state.Entry, rel string) state.Entry { e.Path = rel; return e })
		rekey(p.local, from, to, func(item *localItem, _ string) *localItem { return item })
		p.actions++
		p.logf("moved %s to %s (renamed on the server)", from, to)
	}
}

// followLocalMoves finds files that vanished from their old path and turn
// up, byte for byte, at a new one, and moves the node on the server.
func (p *pass) followLocalMoves() {
	bySize := map[int64][]string{}
	for rel, item := range p.local {
		_, known := p.base[rel]
		_, remote := p.remote.nodes[rel]
		if !item.dir && !known && !remote && !p.skipped(rel) {
			bySize[item.size] = append(bySize[item.size], rel)
		}
	}
	if len(bySize) == 0 {
		return
	}
	for _, from := range sortedKeys(p.base) {
		entry := p.base[from]
		if entry.Type != "file" || p.local[from] != nil || p.skipped(from) {
			continue
		}
		node, ok := p.remote.nodes[from]
		if !ok || node.ID != entry.NodeID || node.Etag != entry.Etag {
			continue // changed remotely too: let the normal rules decide
		}
		candidates := bySize[entry.LocalSize]
		for i, to := range candidates {
			hash, err := p.localHash(to)
			if err != nil || hash != entry.Etag {
				continue
			}
			bySize[entry.LocalSize] = append(candidates[:i:i], candidates[i+1:]...)
			p.moveRemote(from, to, node)
			break
		}
	}
}

func (p *pass) moveRemote(from, to string, node api.Node) {
	parentID, err := p.ensureRemoteDir(parentDir(to))
	if err != nil {
		p.fail(to, err)
		return
	}
	newParent, newName := "", ""
	if parentDir(to) != parentDir(from) {
		newParent = parentID
	}
	if path.Base(to) != path.Base(from) {
		newName = path.Base(to)
	}
	moved, err := p.client.Move(p.ctx, node.ID, newParent, newName)
	if err != nil {
		p.fail(to, err)
		return
	}
	p.actions++
	p.logf("moved %s to %s on the server", from, to)
	p.remote.remove(from)
	p.remote.put(to, *moved)
	p.forget(from)
	p.record(to, *moved)
}

// rekey moves map entries at from (and below it) to the same place under to.
func rekey[T any](m map[string]T, from, to string, update func(T, string) T) {
	var matched []string
	for rel := range m {
		if rel == from || strings.HasPrefix(rel, from+"/") {
			matched = append(matched, rel)
		}
	}
	moved := make(map[string]T, len(matched))
	for _, rel := range matched {
		target := to + strings.TrimPrefix(rel, from)
		moved[target] = update(m[rel], target)
		delete(m, rel)
	}
	for rel, value := range moved {
		m[rel] = value
	}
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
