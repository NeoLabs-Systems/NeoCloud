package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	gosync "sync"
	"testing"
)

// fakeServer is an in-memory stand-in for the NeoCloud routes the engine
// uses (docs/clients.md §2): tree, changes, single uploads with If-Match,
// folders, PATCH, DELETE to the trash and content with Range. Names
// compare without case, like the real server. Replaced versions and
// trashed files keep their content hashes so tests can check that nothing
// was lost.
type fakeServer struct {
	t   *testing.T
	srv *httptest.Server

	mu       gosync.Mutex
	nodes    map[string]*fakeNode
	blobs    map[string][]byte // etag → content
	seq      int64
	journal  []fakeChange
	nextID   int
	trash    []string // etags of trashed files (and their subtrees)
	versions []string // etags replaced by newer content
	// hooks let tests interfere with requests.
	beforeUpload func(parentID, name string)
	failContent  string // content requests for files of this name fail
	// treeFail answers the tree with this status; treeDrop leaves the
	// paths it matches out of the tree (an outage, a partial answer).
	treeFail int
	treeDrop func(path string) bool
	// removeFail answers DELETE with this status.
	removeFail int
	// onContent may answer a content request itself (returns true).
	onContent func(w http.ResponseWriter, r *http.Request, data []byte) bool
}

type fakeNode struct {
	ID         string `json:"id"`
	ParentID   string `json:"parentId"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Etag       string `json:"etag"`
	ModifiedAt string `json:"modifiedAt"`
	Path       string `json:"path,omitempty"`
	TrashedAt  string `json:"trashedAt,omitempty"`
}

type fakeChange struct {
	seq    int64
	nodeID string
}

const fakeRoot = "root-0"

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{
		t:     t,
		nodes: map[string]*fakeNode{fakeRoot: {ID: fakeRoot, Type: "folder"}},
		blobs: map[string][]byte{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sync/tree", f.tree)
	mux.HandleFunc("GET /api/sync/changes", f.changes)
	mux.HandleFunc("POST /api/folders", f.createFolder)
	mux.HandleFunc("PUT /api/upload", f.upload)
	mux.HandleFunc("GET /api/files/{id}/content", f.content)
	mux.HandleFunc("PATCH /api/nodes/{id}", f.patch)
	mux.HandleFunc("DELETE /api/nodes/{id}", f.remove)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f *fakeServer) fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code, "code": code})
}

func (f *fakeServer) send(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func (f *fakeServer) note(id string) {
	f.seq++
	f.journal = append(f.journal, fakeChange{seq: f.seq, nodeID: id})
}

func (f *fakeServer) resolve(id string) string {
	if id == "root" {
		return fakeRoot
	}
	return id
}

func (f *fakeServer) child(parentID, name string) *fakeNode {
	for _, node := range f.nodes {
		if node.ParentID == parentID && node.ID != fakeRoot && strings.EqualFold(node.Name, name) {
			return node
		}
	}
	return nil
}

// pathOf is the node's path below the account root ("" for the root).
func (f *fakeServer) pathOf(node *fakeNode) string {
	var parts []string
	for node != nil && node.ID != fakeRoot {
		parts = append([]string{node.Name}, parts...)
		node = f.nodes[node.ParentID]
	}
	return strings.Join(parts, "/")
}

func (f *fakeServer) newNode(parentID, kind, name string, content []byte) *fakeNode {
	f.nextID++
	node := &fakeNode{ID: fmt.Sprintf("n%d", f.nextID), ParentID: parentID, Type: kind, Name: name, ModifiedAt: "2026-01-01 00:00:00"}
	if kind == "file" {
		node.Etag, node.Size = sha(content), int64(len(content))
		f.blobs[node.Etag] = content
	}
	f.nodes[node.ID] = node
	f.note(node.ID)
	return node
}

func (f *fakeServer) tree(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.treeFail != 0 {
		f.fail(w, f.treeFail, "INTERNAL")
		return
	}
	out := []fakeNode{}
	for _, node := range f.nodes {
		if node.ID == fakeRoot {
			continue
		}
		copied := *node
		copied.Path = f.pathOf(node)
		if f.treeDrop != nil && f.treeDrop(copied.Path) {
			continue
		}
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	f.send(w, map[string]any{"cursor": f.seq, "rootId": fakeRoot, "nodes": out})
}

func (f *fakeServer) changes(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	out := []map[string]any{}
	for _, change := range f.journal {
		if change.seq <= cursor {
			continue
		}
		entry := map[string]any{"seq": change.seq, "op": "update", "nodeId": change.nodeID, "node": nil}
		if node, ok := f.nodes[change.nodeID]; ok {
			entry["node"] = node
		}
		out = append(out, entry)
	}
	f.send(w, map[string]any{"changes": out, "cursor": f.seq, "hasMore": false})
}

func (f *fakeServer) createFolder(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body struct{ ParentID, Name, Path string }
	json.NewDecoder(r.Body).Decode(&body)
	parent := f.resolve(body.ParentID)
	names := []string{body.Name}
	if body.Path != "" {
		names = strings.Split(body.Path, "/")
	}
	var node *fakeNode
	for _, name := range names {
		if node = f.child(parent, name); node == nil {
			node = f.newNode(parent, "folder", name, nil)
		} else if node.Type != "folder" {
			f.fail(w, 409, "NAME_EXISTS")
			return
		}
		parent = node.ID
	}
	f.send(w, node)
}

func (f *fakeServer) upload(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	parent, name := f.resolve(query.Get("parentId")), query.Get("name")
	if f.beforeUpload != nil {
		f.beforeUpload(parent, name)
	}
	data, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if want := r.Header.Get("X-Content-SHA256"); want != "" && want != sha(data) {
		f.fail(w, 422, "CHECKSUM_MISMATCH")
		return
	}
	existing := f.child(parent, name)
	if existing != nil {
		if query.Get("conflict") != "replace" || existing.Type != "file" {
			f.fail(w, 409, "NAME_EXISTS")
			return
		}
		if base := strings.Trim(r.Header.Get("If-Match"), `"`); base != "" && base != existing.Etag {
			f.fail(w, 412, "ETAG_MISMATCH")
			return
		}
		f.versions = append(f.versions, existing.Etag)
		existing.Etag, existing.Size = sha(data), int64(len(data))
		f.blobs[existing.Etag] = data
		f.note(existing.ID)
		f.send(w, existing)
		return
	}
	f.send(w, f.newNode(parent, "file", name, data))
}

func (f *fakeServer) content(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	node, ok := f.nodes[r.PathValue("id")]
	var data []byte
	if ok {
		data = f.blobs[node.Etag]
	}
	f.mu.Unlock()
	if !ok || node.Type != "file" {
		f.fail(w, 404, "NOT_FOUND")
		return
	}
	if f.failContent != "" && node.Name == f.failContent {
		f.fail(w, 500, "INTERNAL")
		return
	}
	if f.onContent != nil && f.onContent(w, r, data) {
		return
	}
	w.Header().Set("ETag", `"`+node.Etag+`"`)
	if spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		start, _ := strconv.Atoi(strings.TrimSuffix(spec, "-"))
		if start >= len(data) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(data[start:])
		return
	}
	w.Write(data)
}

func (f *fakeServer) patch(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	node, ok := f.nodes[r.PathValue("id")]
	if !ok {
		f.fail(w, 404, "NOT_FOUND")
		return
	}
	var body struct{ Name, ParentID string }
	json.NewDecoder(r.Body).Decode(&body)
	parent, name := node.ParentID, node.Name
	if body.ParentID != "" {
		parent = f.resolve(body.ParentID)
	}
	if body.Name != "" {
		name = body.Name
	}
	if other := f.child(parent, name); other != nil && other != node {
		f.fail(w, 409, "NAME_EXISTS")
		return
	}
	node.ParentID, node.Name = parent, name
	f.note(node.ID)
	f.send(w, node)
}

func (f *fakeServer) remove(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	node, ok := f.nodes[r.PathValue("id")]
	if !ok {
		f.fail(w, 404, "NOT_FOUND")
		return
	}
	if f.removeFail != 0 {
		f.fail(w, f.removeFail, "INTERNAL")
		return
	}
	f.trashTree(node)
	f.send(w, map[string]bool{"success": true})
}

func (f *fakeServer) trashTree(node *fakeNode) {
	for _, other := range f.nodes {
		if other.ParentID == node.ID {
			f.trashTree(other)
		}
	}
	if node.Type == "file" {
		f.trash = append(f.trash, node.Etag)
	}
	delete(f.nodes, node.ID)
	f.note(node.ID)
}

// --- direct manipulation, standing in for another client ---

// put writes a file at rel (below the account root), creating folders.
func (f *fakeServer) put(rel, content string) *fakeNode {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(rel, "/")
	parent := fakeRoot
	for _, name := range parts[:len(parts)-1] {
		node := f.child(parent, name)
		if node == nil {
			node = f.newNode(parent, "folder", name, nil)
		}
		parent = node.ID
	}
	name := parts[len(parts)-1]
	if existing := f.child(parent, name); existing != nil {
		f.versions = append(f.versions, existing.Etag)
		existing.Etag, existing.Size = sha([]byte(content)), int64(len(content))
		f.blobs[existing.Etag] = []byte(content)
		f.note(existing.ID)
		return existing
	}
	return f.newNode(parent, "file", name, []byte(content))
}

// putRaw adds a node without the case-insensitive name check, for
// collisions a legacy server could hold.
func (f *fakeServer) putRaw(parentRel, name, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := f.lookup(parentRel)
	f.newNode(parent.ID, "file", name, []byte(content))
}

// mkdirRaw adds a folder without any name check, for paths a hostile
// server could send.
func (f *fakeServer) mkdirRaw(parentRel, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := f.lookup(parentRel)
	f.newNode(parent.ID, "folder", name, nil)
}

// setModified sets a node's modification time as the server reports it.
func (f *fakeServer) setModified(rel, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookup(rel).ModifiedAt = value
}

// journalSeq is the journal position: unchanged means nothing changed.
func (f *fakeServer) journalSeq() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seq
}

func (f *fakeServer) mkdir(rel string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := fakeRoot
	for _, name := range strings.Split(rel, "/") {
		node := f.child(parent, name)
		if node == nil {
			node = f.newNode(parent, "folder", name, nil)
		}
		parent = node.ID
	}
}

func (f *fakeServer) lookup(rel string) *fakeNode {
	if rel == "" {
		return f.nodes[fakeRoot]
	}
	for _, node := range f.nodes {
		if node.ID != fakeRoot && f.pathOf(node) == rel {
			return node
		}
	}
	return nil
}

func (f *fakeServer) del(rel string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if node := f.lookup(rel); node != nil {
		f.trashTree(node)
	}
}

func (f *fakeServer) rename(rel, newName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if node := f.lookup(rel); node != nil && f.child(node.ParentID, newName) == nil {
		node.Name = newName
		f.note(node.ID)
	}
}

// files maps every live file below folder to its etag, and folders to "/".
func (f *fakeServer) files(folder string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	prefix := folder + "/"
	for _, node := range f.nodes {
		p := f.pathOf(node)
		if node.ID == fakeRoot || !strings.HasPrefix(p, prefix) {
			continue
		}
		if node.Type == "folder" {
			out[strings.TrimPrefix(p, prefix)] = "/"
		} else {
			out[strings.TrimPrefix(p, prefix)] = node.Etag
		}
	}
	return out
}

// everyHash is every content hash the server still holds: live files,
// trash and replaced versions.
func (f *fakeServer) everyHash() map[string]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]bool{}
	for _, node := range f.nodes {
		if node.Type == "file" {
			out[node.Etag] = true
		}
	}
	for _, etag := range append(append([]string(nil), f.trash...), f.versions...) {
		out[etag] = true
	}
	return out
}
