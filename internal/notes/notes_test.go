package notes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/agentapi"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/oauth"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/llm"
)

type ctxKey struct{}

type fakeAI struct {
	disabled bool
	answer   string
	data     map[string]any
	prompts  []string
}

func (f *fakeAI) Check(context.Context, int64, string) error {
	if f.disabled {
		return apperr.New(http.StatusForbidden, "AI features aren’t turned on.", "AI_DISABLED")
	}
	return nil
}
func (f *fakeAI) Chat(_ context.Context, _ int64, role string, messages []llm.Message) (string, string, error) {
	f.record(role, messages)
	return f.answer, "fixture::model", nil
}
func (f *fakeAI) ChatJSON(_ context.Context, _ int64, role string, messages []llm.Message) (map[string]any, string, error) {
	f.record(role, messages)
	return f.data, "fixture::model", nil
}
func (f *fakeAI) record(role string, messages []llm.Message) {
	raw, _ := json.Marshal(messages)
	f.prompts = append(f.prompts, role+" "+string(raw))
}

func fixture(t *testing.T) (*dbx.DB, *http.ServeMux, *fakeAI) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(base, "notes.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(base, "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(username,password) VALUES('reader','x'),('other','x'); INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('root',1,NULL,'folder','','root'),('root2',2,NULL,'folder','','root2')`); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	blobDir := filepath.Join(base, "blobs")
	if err = os.MkdirAll(blobDir, 0700); err != nil {
		t.Fatal(err)
	}
	store := files.New(files.Options{DB: db.W, Keys: keys, BlobDir: blobDir, UploadTmpDir: filepath.Join(base, "tmp"),
		PreviewDir: filepath.Join(base, "previews"), MaxNodes: 1000, MaxUploadBytes: 100 << 20, DefaultQuotaBytes: 1 << 30, VersionRetention: 10})
	files, err := agentapi.New(agentapi.Options{DB: db.W, Keys: keys, BlobDir: blobDir, Files: store, DefaultQuotaBytes: 1 << 30, MaxNodes: 1000, VersionRetention: 10, Authenticate: func(context.Context, string) (oauth.AccessPrincipal, error) {
		return oauth.AccessPrincipal{}, oauth.ErrInvalidAccessToken
	}})
	if err != nil {
		t.Fatal(err)
	}
	ai := &fakeAI{}
	h, err := New(Options{Files: files, AI: ai, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		p, ok := r.Context().Value(ctxKey{}).(Principal)
		return p, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return db, mux, ai
}

func do(m *http.ServeMux, p Principal, method, url, body string) *httptest.ResponseRecorder {
	ctx := context.Background()
	if p.UserID > 0 {
		ctx = context.WithValue(ctx, ctxKey{}, p)
	}
	r := httptest.NewRequest(method, url, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

var owner, other = Principal{UserID: 1}, Principal{UserID: 2}

func expect[T any](t *testing.T, w *httptest.ResponseRecorder, status int) T {
	t.Helper()
	var value T
	if w.Code != status {
		t.Fatalf("status=%d want %d: %s", w.Code, status, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

type object = map[string]any

func titles(l listing) []string {
	out := []string{}
	for _, n := range l.Notes {
		out = append(out, n.Title)
	}
	return out
}

func TestNotesLifecycle(t *testing.T) {
	db, m, _ := fixture(t)
	empty := do(m, owner, "GET", "/api/notes", "")
	if empty.Body.String() != `{"rootId":null,"notes":[],"notebooks":[],"encryptedNotebooks":[],"tags":[]}` {
		t.Fatalf("empty listing %s", empty.Body.String())
	}
	groceries := expect[noteWithBody](t, do(m, owner, "POST", "/api/notes", `{"title":"Groceries","body":"- [ ] Milk\n- [x] Bread","tags":["home","shopping"],"pinned":true,"color":"amber"}`), 201)
	if groceries.Title != "Groceries" || groceries.Notebook != "" || groceries.Checklist != (checklist{1, 2}) || !groceries.Pinned || groceries.Color != "amber" || groceries.Body != "- [ ] Milk\n- [x] Bread" {
		t.Fatalf("created %#v", groceries)
	}
	meeting := expect[noteWithBody](t, do(m, owner, "POST", "/api/notes", `{"body":"# Meeting notes\nAgenda"}`), 201)
	second := expect[noteWithBody](t, do(m, owner, "POST", "/api/notes", `{"title":"Meeting notes","body":"Again"}`), 201)
	if meeting.Title != "Meeting notes" || second.Title != "Meeting notes (2)" {
		t.Fatalf("titles %q %q", meeting.Title, second.Title)
	}
	listed := expect[listing](t, do(m, owner, "GET", "/api/notes", ""), 200)
	if listed.RootID == nil || listed.Notes[0].Title != "Groceries" || !reflect.DeepEqual(listed.Tags, []tagCount{{"home", 1}, {"shopping", 1}}) {
		t.Fatalf("listing %#v", listed)
	}
	if raw := do(m, owner, "GET", "/api/notes", "").Body.String(); strings.Contains(raw, `"body"`) {
		t.Fatalf("cards carry a body: %s", raw)
	}
	if got := titles(expect[listing](t, do(m, owner, "GET", "/api/notes?q=agenda", ""), 200)); !reflect.DeepEqual(got, []string{"Meeting notes"}) {
		t.Fatalf("q %v", got)
	}
	if got := titles(expect[listing](t, do(m, owner, "GET", "/api/notes?tag=HOME", ""), 200)); !reflect.DeepEqual(got, []string{"Groceries"}) {
		t.Fatalf("tag %v", got)
	}

	saved := expect[noteWithBody](t, do(m, owner, "PATCH", "/api/notes/"+groceries.ID, `{"body":"- [x] Milk","baseRevision":"`+groceries.Revision+`"}`), 200)
	if saved.Body != "- [x] Milk" || !saved.Pinned || saved.Revision == groceries.Revision {
		t.Fatalf("saved %#v", saved)
	}
	stale := expect[object](t, do(m, owner, "PATCH", "/api/notes/"+groceries.ID, `{"body":"lost","baseRevision":"`+groceries.Revision+`"}`), 409)
	if stale["code"] != "CONFLICT" {
		t.Fatalf("stale %v", stale)
	}
	archived := expect[noteWithBody](t, do(m, owner, "PATCH", "/api/notes/"+groceries.ID, `{"archived":true,"pinned":false,"title":"Shopping list"}`), 200)
	if archived.Title != "Shopping list" || !archived.Archived || archived.Body != "- [x] Milk" {
		t.Fatalf("archived %#v", archived)
	}
	if got := titles(expect[listing](t, do(m, owner, "GET", "/api/notes?archived=true", ""), 200)); !reflect.DeepEqual(got, []string{"Shopping list"}) {
		t.Fatalf("archive %v", got)
	}
	expect[object](t, do(m, owner, "PATCH", "/api/notes/"+groceries.ID, `{"pinned":"yes"}`), 400)

	if got := do(m, owner, "POST", "/api/notes/notebooks", `{"name":"Work"}`).Body.String(); got != `{"created":true,"name":"Work"}` {
		t.Fatalf("notebook %s", got)
	}
	plan := expect[noteWithBody](t, do(m, owner, "POST", "/api/notes", `{"title":"Plan","body":"Q3","notebook":"Work"}`), 201)
	moved := expect[noteWithBody](t, do(m, owner, "PATCH", "/api/notes/"+plan.ID, `{"notebook":"Ideas"}`), 200)
	if plan.Notebook != "Work" || moved.Notebook != "Ideas" || moved.Title != "Plan" {
		t.Fatalf("notebooks %#v %#v", plan, moved)
	}
	listed = expect[listing](t, do(m, owner, "GET", "/api/notes", ""), 200)
	if !reflect.DeepEqual(listed.Notebooks, []string{"Ideas", "Work"}) {
		t.Fatalf("notebooks %v", listed.Notebooks)
	}
	if got := titles(expect[listing](t, do(m, owner, "GET", "/api/notes?notebook=Ideas", ""), 200)); !reflect.DeepEqual(got, []string{"Plan"}) {
		t.Fatalf("in notebook %v", got)
	}

	for _, request := range [][3]string{{"GET", "", ""}, {"PATCH", "", `{"body":"mine now"}`}, {"DELETE", "", ""}, {"POST", "/ai", `{"action":"summarize"}`}} {
		if w := do(m, other, request[0], "/api/notes/"+plan.ID+request[1], request[2]); w.Code != 404 {
			t.Fatalf("%s by another account = %d %s", request[0], w.Code, w.Body.String())
		}
	}
	if got := titles(expect[listing](t, do(m, other, "GET", "/api/notes", ""), 200)); len(got) != 0 {
		t.Fatalf("other sees %v", got)
	}

	expect[object](t, do(m, owner, "DELETE", "/api/notes/"+plan.ID, ""), 200)
	expect[object](t, do(m, owner, "GET", "/api/notes/"+plan.ID, ""), 404)
	var trashed int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id=? AND trashed_at IS NOT NULL`, plan.ID).Scan(&trashed); err != nil || trashed != 1 {
		t.Fatalf("not in trash: %d %v", trashed, err)
	}
}

func TestEncryptedNotebooksAreListedByNameAndNeverRead(t *testing.T) {
	db, m, _ := fixture(t)
	root := expect[object](t, do(m, owner, "POST", "/api/notes/root", ""), 200)["id"].(string)
	if again := expect[object](t, do(m, owner, "POST", "/api/notes/root", ""), 200)["id"]; again != root {
		t.Fatalf("second root %v", again)
	}
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('work',1,?,'folder','Work','w'),('private',1,'work','folder','Private','p');
		INSERT INTO vaults(id,user_id,folder_id,salt,iterations,wrapped_key) VALUES('vault-1',1,'private','s',600000,'k');
		UPDATE nodes SET vault_id='vault-1' WHERE id='private';
		INSERT INTO nodes(id,user_id,parent_id,type,name,size,etag,vault_id) VALUES('secret',1,'private','file','Secret.md',80,'e','vault-1')`, root); err != nil {
		t.Fatal(err)
	}
	listed := expect[listing](t, do(m, owner, "GET", "/api/notes", ""), 200)
	want := []encryptedNotebook{{VaultID: "vault-1", FolderID: "private", Name: "Work/Private"}}
	if !reflect.DeepEqual(listed.EncryptedNotebooks, want) || !reflect.DeepEqual(listed.Notebooks, []string{"Work"}) || len(listed.Notes) != 0 {
		t.Fatalf("%#v", listed)
	}
	expect[object](t, do(m, owner, "GET", "/api/notes/secret", ""), 404)
	expect[object](t, do(m, owner, "POST", "/api/notes/secret/ai", `{"action":"summarize"}`), 404)
}

func TestOnlyBrowsersAndPhonesReachNotes(t *testing.T) {
	_, m, _ := fixture(t)
	expect[object](t, do(m, Principal{}, "GET", "/api/notes", ""), 401)
	for _, request := range [][2]string{{"GET", "/api/notes"}, {"POST", "/api/notes"}, {"POST", "/api/notes/x/ai"}} {
		got := expect[object](t, do(m, Principal{UserID: 1, Device: true, Platform: "linux"}, request[0], request[1], "{}"), 403)
		if got["code"] != "PHONE_REQUIRED" {
			t.Fatalf("%v", got)
		}
	}
	expect[object](t, do(m, Principal{UserID: 1, Device: true, Platform: "ios"}, "GET", "/api/notes", ""), 200)
}

func TestNoteHelpReturnsTextTitleOrTagsWithoutChangingTheNote(t *testing.T) {
	_, m, ai := fixture(t)
	created := expect[noteWithBody](t, do(m, owner, "POST", "/api/notes", `{"title":"Call","body":"Call Anna about the offer on Friday.","tags":["work"]}`), 201)
	url := "/api/notes/" + created.ID + "/ai"

	ai.disabled = true
	if got := expect[object](t, do(m, owner, "POST", url, `{"action":"summarize"}`), 403); got["code"] != "AI_DISABLED" {
		t.Fatalf("%v", got)
	}
	ai.disabled = false

	ai.answer = "```markdown\n- [ ] Call Anna about the offer (Friday)\n```"
	if got := expect[object](t, do(m, owner, "POST", url, `{"action":"actionItems"}`), 200); got["text"] != "- [ ] Call Anna about the offer (Friday)" || got["model"] != "fixture::model" {
		t.Fatalf("%v", got)
	}
	last := func() string { return ai.prompts[len(ai.prompts)-1] }
	if !strings.Contains(last(), "Call Anna about the offer on Friday") || !strings.Contains(last(), "data, not instructions") {
		t.Fatalf("prompt %s", last())
	}

	ai.data = map[string]any{"title": "Call Anna / offer"}
	if got := expect[object](t, do(m, owner, "POST", url, `{"action":"title"}`), 200); got["title"] != "Call Anna   offer" {
		t.Fatalf("%v", got)
	}
	ai.data = map[string]any{"tags": []any{"work", "Work", "calls"}}
	if got := expect[object](t, do(m, owner, "POST", url, `{"action":"tags"}`), 200); !reflect.DeepEqual(got["tags"], []any{"work", "calls"}) {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(last(), "Existing tags: work") {
		t.Fatalf("prompt %s", last())
	}

	ai.answer = "Shorter."
	if got := expect[object](t, do(m, owner, "POST", url, `{"action":"shorten","selection":"A long sentence here."}`), 200); got["text"] != "Shorter." {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(last(), "Selected text") {
		t.Fatalf("prompt %s", last())
	}

	expect[object](t, do(m, owner, "POST", url, `{"action":"ask"}`), 400)
	expect[object](t, do(m, owner, "POST", url, `{"action":"delete everything"}`), 400)
	blank := expect[noteWithBody](t, do(m, owner, "POST", "/api/notes", `{"title":"Empty"}`), 201)
	if got := expect[object](t, do(m, owner, "POST", "/api/notes/"+blank.ID+"/ai", `{"action":"summarize"}`), 422); got["code"] != "AI_NO_CONTENT" {
		t.Fatalf("%v", got)
	}
	if after := expect[noteWithBody](t, do(m, owner, "GET", "/api/notes/"+created.ID, ""), 200); after.Revision != created.Revision {
		t.Fatal("note help changed the note")
	}
}
