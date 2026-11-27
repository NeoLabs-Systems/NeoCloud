package files_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/apptest"
)

func digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func put(t *testing.T, c *apptest.Client, status int, query url.Values, content string, headers ...string) map[string]any {
	t.Helper()
	response := c.Do(t, "PUT", "/api/upload?"+query.Encode(), []byte(content), append([]string{"Content-Type", "application/octet-stream"}, headers...)...)
	if response.Status != status {
		t.Fatalf("upload %s = %d, want %d: %s", query.Encode(), response.Status, status, response.Body)
	}
	return response.JSON(t)
}

func code(t *testing.T, body map[string]any, want string) {
	t.Helper()
	if body["code"] != want {
		t.Fatalf("code = %v, want %s (%v)", body["code"], want, body)
	}
}

func items(body map[string]any) []any { return body["items"].([]any) }

func TestConflictsVersionsAndRestore(t *testing.T) {
	server := apptest.Start(t, nil)
	owner := server.Register(t, "owner")
	first := owner.Upload(t, "root", "doc.md", "v1")
	clash := put(t, owner, 409, url.Values{"name": {"DOC.md"}}, "v2")
	code(t, clash, "NAME_EXISTS")
	if clash["error"] != "“doc.md” already exists here." {
		t.Fatalf("error = %v", clash["error"])
	}
	if renamed := put(t, owner, 201, url.Values{"name": {"doc.md"}, "conflict": {"rename"}}, "copy"); renamed["name"] != "doc (1).md" {
		t.Fatalf("name = %v", renamed["name"])
	}
	code(t, put(t, owner, 412, url.Values{"name": {"doc.md"}, "conflict": {"replace"}}, "v2", "If-Match", `"nope"`), "ETAG_MISMATCH")
	replaced := put(t, owner, 201, url.Values{"name": {"doc.md"}, "conflict": {"replace"}}, "v2")
	if replaced["id"] != first || replaced["version"] != float64(2) {
		t.Fatalf("replaced = %v", replaced)
	}
	restored := owner.Call(t, 200, "POST", "/api/files/"+first+"/versions/1/restore", nil)
	if restored["version"] != float64(3) || restored["etag"] != digest("v1") {
		t.Fatalf("restored = %v", restored)
	}
	code(t, owner.Call(t, 404, "POST", "/api/files/"+first+"/versions/abc/restore", nil), "NOT_FOUND")
	folder := owner.Folder(t, "root", "folder")
	code(t, owner.Call(t, 400, "GET", "/api/files/"+folder+"/versions", nil), "NOT_A_FILE")
	code(t, put(t, owner, 409, url.Values{"name": {"folder"}, "conflict": {"replace"}}, "x"), "NAME_EXISTS")
	code(t, owner.Call(t, 409, "POST", "/api/folders", map[string]any{"name": "doc.md", "conflict": "replace"}), "NAME_EXISTS")
	if dot := put(t, owner, 201, url.Values{"name": {".env"}}, "a"); dot["mime"] != "application/octet-stream" {
		t.Fatalf("dotfile mime = %v", dot["mime"])
	}
	if dot := put(t, owner, 201, url.Values{"name": {".env"}, "conflict": {"rename"}}, "b"); dot["name"] != ".env (1)" {
		t.Fatalf("dotfile name = %v", dot["name"])
	}
}

func TestTreeChangesVaultBoundaryAndJournal(t *testing.T) {
	server := apptest.Start(t, nil)
	owner := server.Register(t, "owner")
	deep := owner.Call(t, 201, "POST", "/api/folders", map[string]any{"path": "Photos/2026/May"})["id"].(string)
	if again := owner.Call(t, 201, "POST", "/api/folders", map[string]any{"path": "Photos/2026/May"}); again["id"] != deep {
		t.Fatalf("path not reused: %v", again)
	}
	code(t, owner.Call(t, 400, "POST", "/api/folders", map[string]any{"path": "//"}), "PATH_EMPTY")
	code(t, owner.Call(t, 400, "POST", "/api/folders", map[string]any{"name": "x", "conflict": "merge"}), "CONFLICT_MODE_INVALID")
	path := owner.Call(t, 200, "GET", "/api/nodes/"+deep, nil)["path"]
	if apptest.Names(path, "name") != ",Photos,2026,May" {
		t.Fatalf("path = %v", path)
	}
	photos := path.([]any)[1].(map[string]any)["id"].(string)
	code(t, owner.Call(t, 400, "PATCH", "/api/nodes/"+photos, map[string]any{"parentId": deep}), "MOVE_INTO_SELF")

	// A vault's own folder moves like any folder; its contents stay inside.
	vault := owner.Vault(t, "root", "Secret", "fail")["folderId"].(string)
	owner.Call(t, 200, "PATCH", "/api/nodes/"+vault, map[string]any{"parentId": photos})
	cipher := owner.Upload(t, vault, "cipher", "ciphertext")
	code(t, owner.Call(t, 409, "PATCH", "/api/nodes/"+cipher, map[string]any{"parentId": photos}), "VAULT_BOUNDARY")

	// Ciphertext cannot be restored into the root while its vault is gone.
	owner.Call(t, 200, "DELETE", "/api/nodes/"+cipher, nil)
	owner.Call(t, 200, "DELETE", "/api/nodes/"+vault, nil)
	refused := owner.Call(t, 409, "POST", "/api/trash/"+cipher+"/restore", nil)
	code(t, refused, "VAULT_BOUNDARY")
	owner.Call(t, 200, "POST", "/api/trash/"+vault+"/restore", nil)
	owner.Call(t, 200, "POST", "/api/trash/"+cipher+"/restore", nil)

	var unnamed int
	if err := server.DB.QueryRow(`SELECT COUNT(*) FROM changes WHERE parent_ids_json='[]'`).Scan(&unnamed); err != nil || unnamed != 0 {
		t.Fatalf("journal rows without folders = %d (%v)", unnamed, err)
	}
	var vaultRows int
	if err := server.DB.QueryRow(`SELECT COUNT(*) FROM activity a JOIN nodes n ON n.id=a.node_id WHERE n.vault_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM vaults v WHERE v.folder_id=n.id)`).Scan(&vaultRows); err != nil || vaultRows != 0 {
		t.Fatalf("vault contents in the feed = %d (%v)", vaultRows, err)
	}
}

func TestUploadBurstsFoldInTheFeed(t *testing.T) {
	server := apptest.Start(t, nil)
	owner := server.Register(t, "owner")
	folder := owner.Folder(t, "root", "Drop")
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		owner.Upload(t, folder, name, name)
	}
	var rows, count int
	if err := server.DB.QueryRow(`SELECT COUNT(*),MAX(count) FROM activity WHERE kind='upload'`).Scan(&rows, &count); err != nil || rows != 1 || count != 4 {
		t.Fatalf("upload rows=%d count=%d (%v)", rows, count, err)
	}
}

func TestResumableUploadDatesRetriesAndAbandonment(t *testing.T) {
	server := apptest.Start(t, nil)
	owner := server.Register(t, "owner")
	folder := owner.Folder(t, "root", "Camera")
	content := "IMG bytes"
	query := url.Values{"parentId": {folder}, "name": {"IMG.jpg"}, "conflict": {"rename"}}
	first := put(t, owner, 201, query, content, "X-Content-SHA256", digest(content))
	if again := put(t, owner, 201, query, content, "X-Content-SHA256", digest(content)); again["id"] != first["id"] {
		t.Fatalf("retry made another file: %v", again)
	}
	session := owner.Call(t, 201, "POST", "/api/uploads", map[string]any{"parentId": folder, "name": "IMG.jpg", "size": len(content), "conflict": "rename", "sha256": digest(content), "createdAt": 1700000000000})
	id := session["id"].(string)
	code(t, owner.Call(t, 409, "POST", "/api/uploads/"+id+"/complete", nil), "UPLOAD_INCOMPLETE")
	owner.Do(t, "PUT", "/api/uploads/"+id+"/parts/0", []byte(content), "Content-Type", "application/octet-stream")
	// A body-less completion is fine.
	if done := owner.Call(t, 201, "POST", "/api/uploads/"+id+"/complete", nil); done["id"] != first["id"] {
		t.Fatalf("resumable retry made another file: %v", done)
	}
	owner.Call(t, 404, "GET", "/api/uploads/"+id, nil)
	var blobs, used int
	if err := server.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM blobs),(SELECT used_bytes FROM users WHERE id=?)`, owner.ID).Scan(&blobs, &used); err != nil || blobs != 1 || used != len(content) {
		t.Fatalf("blobs=%d used=%d (%v)", blobs, used, err)
	}
	if kept := put(t, owner, 201, query, content); kept["name"] != "IMG (1).jpg" {
		t.Fatalf("explicit keep-both = %v", kept["name"])
	}

	dated := put(t, owner, 201, url.Values{"name": {"dated.jpg"}}, "d", "X-File-Created", "2026-03-12 10:10+01:00", "X-File-Modified", "0")
	var created, modified any
	if err := server.DB.QueryRow(`SELECT created_at,modified_at FROM file_client_dates WHERE node_id=?`, dated["id"]).Scan(&created, &modified); err != nil || created != "2026-03-12T10:10:00" || modified != nil {
		t.Fatalf("client dates = %v %v (%v)", created, modified, err)
	}
}

func TestDiskFloorRefusesUploads(t *testing.T) {
	server := apptest.Start(t, map[string]string{"NEOCLOUD_MIN_FREE_DISK": "1000000GB"})
	owner := server.Register(t, "owner")
	code(t, put(t, owner, 507, url.Values{"name": {"x"}}, "x"), "DISK_FULL")
}

func TestDownloadsAreNeverRewritten(t *testing.T) {
	server := apptest.Start(t, nil)
	server.Register(t, "admin")
	member := server.Register(t, "member")
	content := `{"error":"TypeError: boom at /var/log/app"}`
	id := member.Upload(t, "root", "trace.json", content)
	response := member.Expect(t, 200, "GET", "/api/files/"+id+"/content", nil)
	if string(response.Body) != content {
		t.Fatalf("download changed: %s", response.Body)
	}
	if partial := member.Do(t, "GET", "/api/files/"+id+"/content", nil, "Range", "bytes=2-6"); partial.Status != 206 || string(partial.Body) != content[2:7] {
		t.Fatalf("range = %d %q", partial.Status, partial.Body)
	}
}

func TestTreeChangesAreRateLimitedPerAccount(t *testing.T) {
	server := apptest.Start(t, map[string]string{"NEOCLOUD_MAX_NODES": "100000"})
	alice, bob := server.Register(t, "alice"), server.Register(t, "bob")
	for range 100 {
		alice.Call(t, 404, "POST", "/api/nodes/missing/copy", map[string]any{})
	}
	code(t, alice.Call(t, 429, "POST", "/api/nodes/missing/copy", map[string]any{}), "RATE_LIMITED")
	bob.Call(t, 404, "POST", "/api/nodes/missing/copy", map[string]any{})
}

func TestListingPagesAreStableForEverySort(t *testing.T) {
	server := apptest.Start(t, nil)
	owner := server.Register(t, "owner")
	box := owner.Folder(t, "root", "Paging")
	for i, name := range []string{"Äpfel.txt", "zebra.txt", "Zulu.txt", "ábc.txt", "beta.txt", "Alpha.txt", "ölig.txt"} {
		owner.Upload(t, box, name, strings.Repeat("x", i%3+1))
	}
	owner.Folder(t, box, "b-folder")
	for _, sort := range []string{"name", "size", "modified", "type"} {
		for _, order := range []string{"asc", "desc"} {
			base := fmt.Sprintf("/api/folders/%s/children?sort=%s&order=%s", box, sort, order)
			full := apptest.Names(owner.Call(t, 200, "GET", base, nil)["items"], "name")
			var paged []string
			cursor := ""
			for range 10 {
				page := owner.Call(t, 200, "GET", base+"&limit=2&cursor="+cursor, nil)
				paged = append(paged, apptest.Names(page["items"], "name"))
				next, more := page["nextCursor"].(string)
				if !more {
					break
				}
				cursor = next
			}
			if strings.Join(paged, ",") != full {
				t.Fatalf("%s %s: paged %v, want %s", sort, order, paged, full)
			}
		}
	}
	code(t, owner.Call(t, 400, "GET", "/api/folders/"+box+"/children?cursor=bm9wZQ", nil), "INVALID_CURSOR")
}

func TestListsAndStorageAccounting(t *testing.T) {
	server := apptest.Start(t, nil)
	owner := server.Register(t, "owner")
	docs := owner.Folder(t, "root", "Docs")
	report := owner.Upload(t, docs, "report.pdf", "report")
	owner.Upload(t, "root", "my_report.md", "other")
	if got := apptest.Names(items(owner.Call(t, 200, "GET", "/api/search?q=report&limit=nonsense", nil)), "name"); got != "report.pdf,my_report.md" {
		t.Fatalf("search = %s", got)
	}
	owner.Expect(t, 204, "POST", "/api/nodes/"+report+"/opened", nil)
	recent := items(owner.Call(t, 200, "GET", "/api/recent", nil))
	if never, present := recent[1].(map[string]any)["lastOpenedAt"]; !present || never != nil {
		t.Fatalf("recent = %v", recent[1])
	}
	// A trashed file inside a trashed folder is counted once.
	owner.Call(t, 200, "DELETE", "/api/nodes/"+report, nil)
	owner.Call(t, 200, "DELETE", "/api/nodes/"+docs, nil)
	if trash := owner.Call(t, 200, "GET", "/api/storage/breakdown", nil)["trash"]; trash != float64(len("report")) {
		t.Fatalf("trash bytes = %v", trash)
	}
}

func TestOtherAccountsIdsBehaveAsMissing(t *testing.T) {
	server := apptest.Start(t, nil)
	owner, other := server.Register(t, "owner"), server.Register(t, "other")
	folder := owner.Folder(t, "root", "Private")
	file := owner.Upload(t, folder, "secret.txt", "secret")
	owner.Upload(t, folder, "secret.txt.bin", "x")
	put(t, owner, 201, url.Values{"parentId": {folder}, "name": {"secret.txt"}, "conflict": {"replace"}}, "newer")
	trashed := owner.Upload(t, folder, "gone.txt", "gone")
	owner.Call(t, 200, "DELETE", "/api/nodes/"+trashed, nil)
	upload := owner.Call(t, 201, "POST", "/api/uploads", map[string]any{"parentId": folder, "name": "pending", "size": 1})["id"].(string)
	own := other.Upload(t, "root", "own.txt", "own")
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/folders/" + folder + "/children", nil},
		{"POST", "/api/folders", map[string]any{"parentId": folder, "name": "x"}},
		{"GET", "/api/nodes/" + file, nil},
		{"PATCH", "/api/nodes/" + file, map[string]any{"name": "x"}},
		{"PATCH", "/api/nodes/" + own, map[string]any{"parentId": folder}},
		{"POST", "/api/nodes/" + file + "/copy", map[string]any{}},
		{"POST", "/api/nodes/" + own + "/copy", map[string]any{"parentId": folder}},
		{"DELETE", "/api/nodes/" + file, nil},
		{"PUT", "/api/nodes/" + file + "/favorite", nil},
		{"DELETE", "/api/nodes/" + file + "/favorite", nil},
		{"POST", "/api/nodes/" + file + "/opened", nil},
		{"GET", "/api/files/" + file + "/content", nil},
		{"GET", "/api/files/" + file + "/content?version=1", nil},
		{"GET", "/api/files/" + file + "/versions", nil},
		{"POST", "/api/files/" + file + "/versions/1/restore", nil},
		{"GET", "/api/download/zip?ids=" + file, nil},
		{"GET", "/api/search?parentId=" + folder, nil},
		{"POST", "/api/trash/" + trashed + "/restore", nil},
		{"DELETE", "/api/trash/" + trashed, nil},
		{"POST", "/api/uploads", map[string]any{"parentId": folder, "name": "x", "size": 1}},
		{"POST", "/api/uploads/check", map[string]any{"parentId": folder, "sha256": digest("secret"), "size": 6}},
		{"GET", "/api/uploads/" + upload, nil},
		{"POST", "/api/uploads/" + upload + "/complete", nil},
		{"DELETE", "/api/uploads/" + upload, nil},
	} {
		other.Expect(t, 404, request.method, request.path, request.body)
	}
	put(t, other, 404, url.Values{"parentId": {folder}, "name": {"x"}}, "x")
	if response := other.Do(t, "PUT", "/api/uploads/"+upload+"/parts/0", []byte("x")); response.Status != 404 {
		t.Fatalf("part = %d", response.Status)
	}
	other.Call(t, 200, "DELETE", "/api/trash", nil)
	if got := len(items(owner.Call(t, 200, "GET", "/api/trash", nil))); got != 1 {
		t.Fatalf("owner trash = %d", got)
	}
	if got := len(items(other.Call(t, 200, "GET", "/api/favorites", nil))); got != 0 {
		t.Fatalf("favorites = %d", got)
	}
	server.Visitor(t).Expect(t, 401, "GET", "/api/nodes/"+file, nil)
	owner.Device(t, "webdav").Expect(t, 401, "GET", "/api/nodes/"+file, nil)
	owner.Device(t, "macos").Expect(t, 200, "GET", "/api/nodes/"+file, nil)
}

func TestMemberWritesStayInsideTheShare(t *testing.T) {
	server := apptest.Start(t, nil)
	owner, member := server.Register(t, "owner"), server.Register(t, "member")
	shared := owner.Folder(t, "root", "Team")
	reading := owner.Folder(t, "root", "Reading")
	owner.Call(t, 201, "POST", "/api/nodes/"+shared+"/members", map[string]any{"recipient": "member", "permission": "write"})
	owner.Call(t, 201, "POST", "/api/nodes/"+reading+"/members", map[string]any{"recipient": "member", "permission": "read"})
	own := member.Folder(t, "root", "Mine")

	folder := member.Folder(t, shared, "Plans")
	file := member.Upload(t, folder, "plan.txt", "plan")
	member.Call(t, 200, "PATCH", "/api/nodes/"+file, map[string]any{"name": "plan-2.txt"})
	code(t, member.Call(t, 403, "PATCH", "/api/nodes/"+shared, map[string]any{"name": "x"}), "SHARE_ROOT_IMMUTABLE")
	code(t, member.Call(t, 403, "DELETE", "/api/nodes/"+shared, nil), "SHARE_ROOT_IMMUTABLE")
	code(t, member.Call(t, 409, "PATCH", "/api/nodes/"+file, map[string]any{"parentId": own}), "SHARE_BOUNDARY")
	code(t, member.Call(t, 409, "PATCH", "/api/nodes/"+own, map[string]any{"parentId": shared}), "SHARE_BOUNDARY")
	code(t, member.Call(t, 403, "POST", "/api/folders", map[string]any{"parentId": reading, "name": "x"}), "SHARE_READ_ONLY")
	member.Call(t, 200, "DELETE", "/api/nodes/"+file, nil)
	if restored := member.Call(t, 200, "POST", "/api/trash/"+file+"/restore", nil); restored["parentId"] != folder {
		t.Fatalf("restored = %v", restored)
	}
	member.Call(t, 404, "GET", "/api/files/"+file+"/versions", nil)

	var foreign int
	if err := server.DB.QueryRow(`SELECT COUNT(*) FROM activity WHERE owner_id=? AND actor_id=? AND kind IN ('folder','upload','rename','trash','restore')`, owner.ID, member.ID).Scan(&foreign); err != nil || foreign != 5 {
		t.Fatalf("member activity = %d (%v)", foreign, err)
	}
}
