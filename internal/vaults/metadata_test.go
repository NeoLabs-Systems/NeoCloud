package vaults

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type testIdentityKey struct{}

func setupVaults(t *testing.T) (*dbx.DB, *http.ServeMux) {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "vaults.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO users(username,display_name,password) VALUES('alice','Alice','x'),('bob','Bob','x'); INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('root-a',1,NULL,'folder','','a'),('root-b',2,NULL,'folder','','b'),('vault-folder',1,'root-a','folder','Secret','c'); INSERT INTO vaults(id,user_id,folder_id,salt,iterations,wrapped_key) VALUES('vault-a',1,'vault-folder',?,600000,?); UPDATE nodes SET vault_id='vault-a' WHERE id='vault-folder'`, base64.StdEncoding.EncodeToString(make([]byte, saltBytes)), base64.StdEncoding.EncodeToString(make([]byte, wrappedKeyBytes))); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{DB: db.W, DefaultQuotaBytes: 5 * 1024 * 1024 * 1024, MaxNodes: 1000000, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		id, ok := r.Context().Value(testIdentityKey{}).(int64)
		return Principal{UserID: id}, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return db, mux
}

func vaultRequest(mux *http.ServeMux, method, path, body string, user int64) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), testIdentityKey{}, user))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestVaultMetadataIsOwnerScopedAndKeyCanRotate(t *testing.T) {
	db, mux := setupVaults(t)
	if w := vaultRequest(mux, "GET", "/api/vaults/vault-a", "", 2); w.Code != 404 {
		t.Fatalf("other account get status=%d body=%s", w.Code, w.Body.String())
	}
	salt := base64.StdEncoding.EncodeToString(make([]byte, saltBytes))
	wrapped := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", wrappedKeyBytes)))
	w := vaultRequest(mux, "PUT", "/api/vaults/vault-a/key", `{"salt":"`+salt+`","iterations":"600001","wrappedKey":"`+wrapped+`","clientMetadata":true}`, 1)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"iterations":600001`) {
		t.Fatalf("rotate key status=%d body=%s", w.Code, w.Body.String())
	}
	var stored string
	var rounds int
	if err := db.W.QueryRow(`SELECT wrapped_key,iterations FROM vaults WHERE id='vault-a'`).Scan(&stored, &rounds); err != nil || stored != wrapped || rounds != 600001 {
		t.Fatalf("stored key metadata=%q %d err=%v", stored, rounds, err)
	}
}

func TestVaultKeyRejectsInvalidParams(t *testing.T) {
	_, mux := setupVaults(t)
	for _, body := range []string{`{}`, `{"salt":"AA==","iterations":600000,"wrappedKey":"AA=="}`, `{"salt":"` + base64.StdEncoding.EncodeToString(make([]byte, saltBytes)) + `","iterations":5,"wrappedKey":"` + base64.StdEncoding.EncodeToString(make([]byte, wrappedKeyBytes)) + `"}`} {
		w := vaultRequest(mux, "PUT", "/api/vaults/vault-a/key", body, 1)
		if w.Code != 400 {
			t.Errorf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

func TestVaultRoutesRequireIdentity(t *testing.T) {
	_, mux := setupVaults(t)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/vaults/vault-a", nil))
	if w.Code != 401 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func validVaultBody(name, parent, conflict string) string {
	return `{"parentId":"` + parent + `","name":"` + name + `","salt":"` + base64.StdEncoding.EncodeToString(make([]byte, saltBytes)) + `","iterations":600000,"wrappedKey":"` + base64.StdEncoding.EncodeToString(make([]byte, wrappedKeyBytes)) + `","conflict":"` + conflict + `"}`
}

func TestCreateVaultCreatesFolderAndWrappedMetadataAtomically(t *testing.T) {
	db, mux := setupVaults(t)
	w := vaultRequest(mux, "POST", "/api/vaults", validVaultBody("Private", "root", "fail"), 1)
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result struct {
		ID       string `json:"id"`
		FolderID string `json:"folderId"`
		Folder   struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			VaultID string `json:"vaultId"`
		} `json:"folder"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID == "" || result.FolderID == "" || result.Folder.ID != result.FolderID || result.Folder.Name != "Private" || result.Folder.VaultID != result.ID {
		t.Fatalf("result=%s", w.Body.String())
	}
	var changed, activities int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id=? AND op='create'`, result.FolderID).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM activity WHERE node_id=? AND kind='folder'`, result.FolderID).Scan(&activities); err != nil {
		t.Fatal(err)
	}
	if changed != 1 || activities != 1 {
		t.Fatalf("change rows=%d activity rows=%d", changed, activities)
	}
}

func TestCreateVaultConflictRenameAndRejectsReplaceOrVaultParent(t *testing.T) {
	_, mux := setupVaults(t)
	first := vaultRequest(mux, "POST", "/api/vaults", validVaultBody("Private", "root", "fail"), 1)
	if first.Code != 201 {
		t.Fatalf("first create status=%d body=%s", first.Code, first.Body.String())
	}
	if w := vaultRequest(mux, "POST", "/api/vaults", validVaultBody("Private", "root", "fail"), 1); w.Code != 409 {
		t.Fatalf("conflict status=%d body=%s", w.Code, w.Body.String())
	}
	w := vaultRequest(mux, "POST", "/api/vaults", validVaultBody("Private", "root", "rename"), 1)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"name":"Private (1)"`) {
		t.Fatalf("rename status=%d body=%s", w.Code, w.Body.String())
	}
	if w := vaultRequest(mux, "POST", "/api/vaults", validVaultBody("Other", "root", "replace"), 1); w.Code != 400 {
		t.Fatalf("replace status=%d body=%s", w.Code, w.Body.String())
	}
	if w := vaultRequest(mux, "POST", "/api/vaults", validVaultBody("Nested", "vault-folder", "fail"), 1); w.Code != 409 {
		t.Fatalf("vault parent status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConversionCheckCountsEncodedCopyAndReportsBoundaries(t *testing.T) {
	db, mux := setupVaults(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('convert',1,'root-a','folder','Convert','c1'),('convert-child',1,'convert','file','file.bin','c2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`UPDATE nodes SET size=65537 WHERE id='convert-child'`); err != nil {
		t.Fatal(err)
	}
	w := vaultRequest(mux, "GET", "/api/vaults/conversions/convert", "", 1)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Files, Folders    int64
		Bytes, VaultBytes int64
		Problems          []string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Files != 1 || body.Folders != 0 || body.Bytes != 65537 || body.VaultBytes != 65581 || len(body.Problems) != 0 {
		t.Fatalf("result=%s decoded=%+v", w.Body.String(), body)
	}
	if _, err := db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,'root-a',2,'read','bob')`); err != nil {
		t.Fatal(err)
	}
	w = vaultRequest(mux, "GET", "/api/vaults/conversions/convert", "", 1)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"SHARED"`) {
		t.Fatalf("shared eligibility status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConversionCheckReportsVaultPublicLinkAndQuotaBarriers(t *testing.T) {
	db, mux := setupVaults(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('candidate',1,'root-a','folder','Candidate','candidate-etag'),('nested-vault',1,'candidate','folder','Nested','nested-etag'); UPDATE nodes SET vault_id='nested-vault' WHERE id='nested-vault'; INSERT INTO nodes(id,user_id,parent_id,type,name,size,etag) VALUES('too-large',1,'candidate','file','large.bin',50,'large-etag'); INSERT INTO shares(user_id,node_id,token) VALUES(1,'candidate','public-token'); UPDATE users SET quota_bytes=50 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	w := vaultRequest(mux, "GET", "/api/vaults/conversions/candidate", "", 1)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"CONTAINS_VAULT"`) || !strings.Contains(w.Body.String(), `"PUBLIC_LINK"`) || !strings.Contains(w.Body.String(), `"QUOTA"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for _, id := range []string{"root-a", "vault-folder"} {
		w = vaultRequest(mux, "GET", "/api/vaults/conversions/"+id, "", 1)
		want := `"ROOT"`
		if id == "vault-folder" {
			want = `"IN_VAULT"`
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("id=%s status=%d body=%s", id, w.Code, w.Body.String())
		}
	}
}

func TestReplaceVaultAtomicallyTrashesSourceAndMovesVaultIntoPlace(t *testing.T) {
	db, mux := setupVaults(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('other-parent',1,'root-a','folder','Other','other-etag'),('source',1,'root-a','folder','Documents','source-etag'),('source-child',1,'source','file','note.txt','file-etag'); UPDATE nodes SET parent_id='other-parent' WHERE id='vault-folder'`); err != nil {
		t.Fatal(err)
	}
	response := vaultRequest(mux, "POST", "/api/vaults/vault-a/replace", `{"folderId":"source","cursor":0}`, 1)
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"trashedId":"source"`) || !strings.Contains(response.Body.String(), `"name":"Documents"`) {
		t.Fatalf("response=%s", response.Body.String())
	}
	var trashed, parent, name string
	if err := db.W.QueryRow(`SELECT trashed_at FROM nodes WHERE id='source'`).Scan(&trashed); err != nil || trashed == "" {
		t.Fatalf("source trash=%q err=%v", trashed, err)
	}
	if err := db.W.QueryRow(`SELECT parent_id,name FROM nodes WHERE id='vault-folder'`).Scan(&parent, &name); err != nil || parent != "root-a" || name != "Documents" {
		t.Fatalf("vault position=%q/%q err=%v", parent, name, err)
	}
	var trashOps, renameOps, activity int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id='source' AND op='trash'`).Scan(&trashOps); err != nil {
		t.Fatal(err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id='vault-folder' AND op='rename'`).Scan(&renameOps); err != nil {
		t.Fatal(err)
	}
	var moveOps int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id='vault-folder' AND op='move'`).Scan(&moveOps); err != nil {
		t.Fatal(err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM activity WHERE node_id='vault-folder'`).Scan(&activity); err != nil {
		t.Fatal(err)
	}
	if trashOps != 1 || renameOps != 0 || moveOps != 1 || activity != 0 {
		t.Fatalf("journal trash=%d rename=%d move=%d activity=%d", trashOps, renameOps, moveOps, activity)
	}
}

func TestReplaceRejectsConcurrentDescendantChangeWithoutMutation(t *testing.T) {
	db, mux := setupVaults(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('source',1,'root-a','folder','Documents','source-etag'),('source-child',1,'source','file','note.txt','file-etag'); INSERT INTO changes(user_id,node_id,op) VALUES(1,'source-child','update')`); err != nil {
		t.Fatal(err)
	}
	w := vaultRequest(mux, "POST", "/api/vaults/vault-a/replace", `{"folderId":"source","cursor":0}`, 1)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "VAULT_SOURCE_CHANGED") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var trashed sql.NullString
	var name string
	if err := db.W.QueryRow(`SELECT trashed_at FROM nodes WHERE id='source'`).Scan(&trashed); err != nil {
		t.Fatal(err)
	}
	if err := db.W.QueryRow(`SELECT name FROM nodes WHERE id='vault-folder'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if trashed.Valid || name != "Secret" {
		t.Fatalf("source trashed=%v vault name=%q", trashed.Valid, name)
	}
}

func TestReplaceRollsBackTrashWhenLaterJournalWriteFails(t *testing.T) {
	db, mux := setupVaults(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('source',1,'root-a','folder','Documents','source-etag'); CREATE TRIGGER fail_vault_replace_journal BEFORE INSERT ON changes WHEN NEW.op='rename' BEGIN SELECT RAISE(ABORT,'test journal rejection'); END`); err != nil {
		t.Fatal(err)
	}
	w := vaultRequest(mux, "POST", "/api/vaults/vault-a/replace", `{"folderId":"source","cursor":0}`, 1)
	if w.Code != 500 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var trashed sql.NullString
	var name string
	var sourceTrashChanges int
	if err := db.W.QueryRow(`SELECT trashed_at FROM nodes WHERE id='source'`).Scan(&trashed); err != nil {
		t.Fatal(err)
	}
	if err := db.W.QueryRow(`SELECT name FROM nodes WHERE id='vault-folder'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id='source' AND op='trash'`).Scan(&sourceTrashChanges); err != nil {
		t.Fatal(err)
	}
	if trashed.Valid || name != "Secret" || sourceTrashChanges != 0 {
		t.Fatalf("rollback failed: trash=%v vault=%q journal=%d", trashed.Valid, name, sourceTrashChanges)
	}
}

func TestConcurrentReplaceRequestsCommitOnlyOneTransition(t *testing.T) {
	db, mux := setupVaults(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('source',1,'root-a','folder','Documents','source-etag')`); err != nil {
		t.Fatal(err)
	}
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := vaultRequest(mux, "POST", "/api/vaults/vault-a/replace", `{"folderId":"source","cursor":0}`, 1)
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	successes, notFound := 0, 0
	for status := range statuses {
		if status == 200 {
			successes++
		} else if status == 404 {
			notFound++
		} else {
			t.Errorf("concurrent request status=%d", status)
		}
	}
	var trashChanges, vaultChanges int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id='source' AND op='trash'`).Scan(&trashChanges); err != nil {
		t.Fatal(err)
	}
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id='vault-folder' AND op='rename'`).Scan(&vaultChanges); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || notFound != 1 || trashChanges != 1 || vaultChanges != 1 {
		t.Fatalf("success=%d notFound=%d source-trash=%d vault-rename=%d", successes, notFound, trashChanges, vaultChanges)
	}
}

func TestReplaceRejectsVaultInsideSourceAndInvalidCursor(t *testing.T) {
	db, mux := setupVaults(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('source',1,'root-a','folder','Documents','source-etag'); UPDATE nodes SET parent_id='source' WHERE id='vault-folder'`); err != nil {
		t.Fatal(err)
	}
	w := vaultRequest(mux, "POST", "/api/vaults/vault-a/replace", `{"folderId":"source","cursor":0}`, 1)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "VAULT_BOUNDARY") {
		t.Fatalf("nested vault status=%d body=%s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		body string
		want string
	}{{`{"folderId":"root-a","cursor":0}`, "VAULT_BOUNDARY"}, {`{"folderId":"root-a/nonsense","cursor":0}`, "NOT_FOUND"}, {`{"folderId":"root-a","cursor":-1}`, "INVALID_CURSOR"}, {`{"folderId":"root-a","cursor":"not-a-number"}`, "INVALID_CURSOR"}, {`{"folderId":"root-a","cursor":null}`, "VAULT_BOUNDARY"}} {
		w := vaultRequest(mux, "POST", "/api/vaults/vault-a/replace", tc.body, 1)
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("body=%s status=%d want=%s", w.Body.String(), w.Code, tc.want)
		}
	}
}
