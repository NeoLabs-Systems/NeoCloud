package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type principalKey struct{}

func fixture(t *testing.T) (*dbx.DB, *Handler, *http.ServeMux) {
	t.Helper()
	return fixtureAt(t, filepath.Join(t.TempDir(), "sync.sqlite"))
}

func fixtureAt(t *testing.T, path string) (*dbx.DB, *Handler, *http.ServeMux) {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, path, dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(username,password) VALUES('sync-one','hash'),('sync-two','hash')`); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{DB: db.W, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		id, ok := r.Context().Value(principalKey{}).(int64)
		return Principal{UserID: id}, ok
	}, WatchPollInterval: 10 * time.Millisecond, WatchShareInterval: 10 * time.Millisecond, WatchMaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return db, h, mux
}

func TestWatchInvalidatesOwnerSharedByMeAcrossDatabasePools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.sqlite")
	db, _, mux := fixtureAt(t, path)
	otherPool, err := dbx.Open(context.Background(), path, dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer otherPool.Close()
	if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('owner-root',1,NULL,'folder','','root-etag'),('owner-shared',1,'owner-root','folder','Shared','shared-etag')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/watch", nil).WithContext(context.WithValue(ctx, principalKey{}, int64(1)))
	w := &flushSignalWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 8)}
	done := make(chan struct{})
	go func() { mux.ServeHTTP(w, req); close(done) }()
	awaitFlush := func(want int) {
		t.Helper()
		select {
		case count := <-w.flushed:
			if count != want {
				t.Fatalf("flush count=%d, want %d; stream: %s", count, want, w.Body.String())
			}
		case <-time.After(time.Second):
			t.Fatalf("watch did not flush event %d; stream: %s", want, w.Body.String())
		}
	}
	awaitFlush(1) // ready
	assertShareInvalidation := func(flushNumber int) {
		t.Helper()
		awaitFlush(flushNumber)
		if !strings.Contains(w.Body.String(), "event: shares:changed\ndata: {}") {
			t.Fatalf("owner share invalidation missing after mutation: %s", w.Body.String())
		}
	}
	if _, err = otherPool.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,'owner-shared',2,'read','sync-two')`); err != nil {
		t.Fatal(err)
	}
	assertShareInvalidation(2)
	if _, err = otherPool.W.Exec(`UPDATE user_shares SET permission='write' WHERE owner_id=1 AND node_id='owner-shared' AND grantee_id=2`); err != nil {
		t.Fatal(err)
	}
	assertShareInvalidation(3)
	if _, err = otherPool.W.Exec(`UPDATE user_shares SET expires_at=datetime('now','-1 second') WHERE owner_id=1 AND node_id='owner-shared' AND grantee_id=2`); err != nil {
		t.Fatal(err)
	}
	assertShareInvalidation(4)
	if _, err = otherPool.W.Exec(`DELETE FROM user_shares WHERE owner_id=1 AND node_id='owner-shared' AND grantee_id=2`); err != nil {
		t.Fatal(err)
	}
	assertShareInvalidation(5)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop after request cancellation")
	}
}

type flushSignalWriter struct {
	*httptest.ResponseRecorder
	mu      sync.Mutex
	flushed chan int
	count   int
}

func (w *flushSignalWriter) Flush() {
	w.ResponseRecorder.Flush()
	w.mu.Lock()
	w.count++
	count := w.count
	w.mu.Unlock()
	w.flushed <- count
}

func TestWatchStreamsDurableCursorHintsAndUsesLastEventID(t *testing.T) {
	db, _, mux := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/watch", nil).WithContext(context.WithValue(ctx, principalKey{}, int64(1)))
	req.Header.Set("Last-Event-ID", "0")
	w := &flushSignalWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 4)}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(w, req)
		close(done)
	}()
	select {
	case <-w.flushed: // ready cursor event
	case <-time.After(time.Second):
		t.Fatal("watch did not send ready event")
	}
	if _, err := db.W.Exec(`INSERT INTO changes(user_id,node_id,op) VALUES(1,'node-one','create')`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.flushed: // durable change cursor event
	case <-time.After(time.Second):
		t.Fatal("watch did not publish a committed journal change")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop after request cancellation")
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: ready\ndata: {\"cursor\":0}") || !strings.Contains(body, "id: 1\nevent: sync:changed\ndata: {\"cursor\":1}") {
		t.Fatalf("unexpected SSE events: %s", body)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type=%q", got)
	}
}

func TestWatchWithoutCursorStartsAtCurrentJournalHead(t *testing.T) {
	db, _, mux := fixture(t)
	if _, err := db.W.Exec(`INSERT INTO changes(user_id,node_id,op) VALUES(1,'old-node','create')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/watch", nil).WithContext(context.WithValue(ctx, principalKey{}, int64(1)))
	w := &flushSignalWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 4)}
	done := make(chan struct{})
	go func() { mux.ServeHTTP(w, req); close(done) }()
	select {
	case <-w.flushed:
	case <-time.After(time.Second):
		t.Fatal("watch did not send ready event")
	}
	if !strings.Contains(w.Body.String(), "event: ready\ndata: {\"cursor\":1}") {
		t.Fatalf("watch did not start at current cursor: %s", w.Body.String())
	}
	select {
	case <-w.flushed:
		t.Fatal("watch replayed a change from before it connected")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop after request cancellation")
	}
}

func TestWatchSignalsOnlyTheGranteesShareInvalidation(t *testing.T) {
	db, _, mux := fixture(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('shared-root',1,NULL,'folder','Shared','one')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/watch", nil).WithContext(context.WithValue(ctx, principalKey{}, int64(2)))
	w := &flushSignalWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 4)}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(w, req)
		close(done)
	}()
	select {
	case <-w.flushed:
	case <-time.After(time.Second):
		t.Fatal("watch did not send ready event")
	}
	if _, err := db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,'shared-root',2,'read','sync-two')`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.flushed:
	case <-time.After(time.Second):
		t.Fatal("watch did not publish share invalidation")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop after request cancellation")
	}
	if !strings.Contains(w.Body.String(), "event: shares:changed\ndata: {}") {
		t.Fatalf("share contents should be an invalidation only: %s", w.Body.String())
	}
}

func TestWatchSignalsShareInvalidationWhenGrantExpires(t *testing.T) {
	db, h, mux := fixture(t)
	h.watchMaxDuration = 4 * time.Second
	if _, err := db.W.Exec(`
		INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('expiring-share',1,NULL,'folder','Shared','one');
		INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient,expires_at)
		VALUES(1,'expiring-share',2,'read','sync-two',datetime('now','+2 seconds'));`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/watch", nil).WithContext(context.WithValue(ctx, principalKey{}, int64(2)))
	w := &flushSignalWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 4)}
	done := make(chan struct{})
	go func() { mux.ServeHTTP(w, req); close(done) }()
	select {
	case <-w.flushed: // ready event
	case <-time.After(time.Second):
		t.Fatal("watch did not send ready event")
	}
	select {
	case <-w.flushed: // expiry-triggered share invalidation
	case <-time.After(3500 * time.Millisecond):
		t.Fatal("watch did not publish share invalidation after grant expiry")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop after request cancellation")
	}
	if !strings.Contains(w.Body.String(), "event: shares:changed\ndata: {}") {
		t.Fatalf("grant expiry should trigger a generic share invalidation: %s", w.Body.String())
	}
}

func TestWatchEmitsSharedFolderChangesOnlyWithinCurrentGrant(t *testing.T) {
	db, _, mux := fixture(t)
	if _, err := db.W.Exec(`
		INSERT INTO users(username,password) VALUES('sync-three','hash');
		INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES
			('owner-root',1,NULL,'folder','','r1'),('shared-folder',1,'owner-root','folder','Shared','s1'),
			('outside-folder',1,'owner-root','folder','Outside','o1'),
			('inside-file',1,'shared-folder','file','inside.txt','i1'),
			('outside-file',1,'outside-folder','file','outside.txt','o2'),
			('other-root',3,NULL,'folder','','r3'),('other-folder',3,'other-root','folder','Other','o3'),
			('other-file',3,'other-folder','file','other.txt','o4');
		INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,'shared-folder',2,'read','sync-two');`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/sync/watch", nil).WithContext(context.WithValue(ctx, principalKey{}, int64(2)))
	w := &flushSignalWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan int, 4)}
	done := make(chan struct{})
	go func() { mux.ServeHTTP(w, req); close(done) }()
	select {
	case <-w.flushed:
	case <-time.After(time.Second):
		t.Fatal("watch did not send ready event")
	}
	if _, err := db.W.Exec(`UPDATE nodes SET parent_id='outside-folder' WHERE id='inside-file';
		INSERT INTO changes(user_id,node_id,op,parent_ids_json) VALUES
			(1,'outside-file','update','[]'),(1,'inside-file','move','["shared-folder","outside-folder"]'),(3,'other-file','update','[]')`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.flushed:
	case <-time.After(time.Second):
		t.Fatal("watch did not publish grant-scoped folder changes")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch did not stop after request cancellation")
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: files:changed\ndata: {\"folderIds\":[\"shared-folder\"]}") {
		t.Fatalf("unexpected shared-folder event: %s", body)
	}
	if strings.Contains(body, "outside-folder") || strings.Contains(body, "other-folder") {
		t.Fatalf("watch exposed a folder outside the grant: %s", body)
	}
}

func serve(mux *http.ServeMux, method, path string, userID *int64) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if userID != nil {
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, *userID))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func addNode(t *testing.T, db *dbx.DB, id string, owner int64, parent any, kind, name string) {
	t.Helper()
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,etag) VALUES(?,?,?,?,?,5,'text/plain',?)`, id, owner, parent, kind, name, "etag-"+id); err != nil {
		t.Fatal(err)
	}
}

func TestTreeSnapshotHasConsistentUserScopedCursorAndPaths(t *testing.T) {
	db, _, mux := fixture(t)
	var one, two int64 = 1, 2
	addNode(t, db, "root-one", one, nil, "folder", "")
	addNode(t, db, "folder-one", one, "root-one", "folder", "Docs")
	addNode(t, db, "file-one", one, "folder-one", "file", "Readme.txt")
	addNode(t, db, "trashed-one", one, "root-one", "file", "Hidden.txt")
	if _, err := db.W.Exec(`UPDATE nodes SET trashed_at=datetime('now') WHERE id='trashed-one'`); err != nil {
		t.Fatal(err)
	}
	addNode(t, db, "root-two", two, nil, "folder", "")
	if _, err := db.W.Exec(`INSERT INTO changes(user_id,node_id,op) VALUES(1,'file-one','create'),(2,'root-two','create')`); err != nil {
		t.Fatal(err)
	}
	w := serve(mux, http.MethodGet, "/api/sync/tree", &one)
	if w.Code != http.StatusOK {
		t.Fatalf("tree status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Cursor int64      `json:"cursor"`
		RootID string     `json:"rootId"`
		Nodes  []treeNode `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Cursor != 1 || body.RootID != "root-one" || len(body.Nodes) != 3 {
		t.Fatalf("snapshot=%s", w.Body.String())
	}
	if body.Nodes[0].ID != "root-one" || body.Nodes[1].Path != "Docs" || body.Nodes[2].Path != "Docs/Readme.txt" {
		t.Fatalf("tree nodes=%+v", body.Nodes)
	}
	var serialized map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &serialized); err != nil {
		t.Fatal(err)
	}
	first := serialized["nodes"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "parentId", "type", "name", "size", "mime", "etag", "version", "createdAt", "modifiedAt", "vaultId", "path"} {
		if _, ok := first[key]; !ok {
			t.Errorf("serialized node missing %q: %v", key, first)
		}
	}
}

func TestChangesPaginationLatestAndDeletedNodeSerialization(t *testing.T) {
	db, _, mux := fixture(t)
	var one, two int64 = 1, 2
	addNode(t, db, "root-one", one, nil, "folder", "")
	addNode(t, db, "file-one", one, "root-one", "file", "Gone.txt")
	addNode(t, db, "root-two", two, nil, "folder", "")
	if _, err := db.W.Exec(`INSERT INTO changes(user_id,node_id,op,at) VALUES
		(1,'root-one','create','2026-11-01 10:00:00'),
		(1,'file-one','delete','2026-11-01 10:01:00'),
		(2,'root-two','create','2026-11-01 10:02:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`DELETE FROM nodes WHERE id='file-one'`); err != nil {
		t.Fatal(err)
	}
	w := serve(mux, http.MethodGet, "/api/sync/changes?cursor=0&limit=1", &one)
	if w.Code != http.StatusOK {
		t.Fatalf("changes status=%d body=%s", w.Code, w.Body.String())
	}
	var first struct {
		Changes []change `json:"changes"`
		Cursor  int64    `json:"cursor"`
		HasMore bool     `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Changes) != 1 || first.Cursor != 1 || !first.HasMore || first.Changes[0].Node == nil {
		t.Fatalf("first page=%s", w.Body.String())
	}
	w = serve(mux, http.MethodGet, "/api/sync/changes?cursor=1&limit=1", &one)
	var second struct {
		Changes []change `json:"changes"`
		Cursor  int64    `json:"cursor"`
		HasMore bool     `json:"hasMore"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Changes) != 1 || second.Cursor != 2 || second.HasMore || second.Changes[0].Node != nil || second.Changes[0].NodeID != "file-one" {
		t.Fatalf("deleted node page=%s", w.Body.String())
	}
	w = serve(mux, http.MethodGet, "/api/sync/changes?cursor=latest", &one)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cursor":2`) || !strings.Contains(w.Body.String(), `"changes":[]`) || !strings.Contains(w.Body.String(), `"hasMore":false`) {
		t.Fatalf("latest=%d %s", w.Code, w.Body.String())
	}
}

func TestSyncRequiresAuthenticatedPrincipal(t *testing.T) {
	_, _, mux := fixture(t)
	for _, path := range []string{"/api/sync/tree", "/api/sync/changes"} {
		w := serve(mux, http.MethodGet, path, nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestPageLimitMatchesNumericQuerySemantics(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 1000},
		{"n/a", 1000},
		{"0", 1000},
		{"1e2", 100},
		{"1.9", 1},
		{"-4", 1},
		{"5001", 5000},
		{"Infinity", 5000},
	} {
		t.Run(test.value, func(t *testing.T) {
			if got := pageLimit(test.value); got != test.want {
				t.Fatalf("pageLimit(%q)=%d, want %d", test.value, got, test.want)
			}
		})
	}
}
