package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

func addWriteGrant(t *testing.T, f *zipFixture, root string, permission string) {
	t.Helper()
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,?,2,?,'zip-member')`, root, permission); err != nil {
		t.Fatal(err)
	}
}

func TestSharedSingleAndResumableUploadsUseOwnerStorageAndMemberActor(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.defaultQuotaBytes = 1 << 20
	f.handler.uploadTmpDir = t.TempDir()
	f.handler.maxNodes = 100
	root := "40000000-0000-4000-8000-000000000001"
	share := "40000000-0000-4000-8000-000000000002"
	f.addRoot(t, root, 1)
	f.addRoot(t, "50000000-0000-4000-8000-000000000001", 2)
	f.addFolder(t, share, &root, 1, "Shared", nil)
	addWriteGrant(t, f, share, "write")
	memberServer := f.server(t, 2)

	content := []byte("member upload")
	sum := sha256.Sum256(content)
	uploadRequest, _ := http.NewRequest(http.MethodPut, memberServer.URL+"/api/upload?parentId="+share+"&name=member.txt", bytes.NewReader(content))
	uploadRequest.Header.Set("X-Content-SHA256", hex.EncodeToString(sum[:]))
	uploadResponse, err := memberServer.Client().Do(uploadRequest)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded node
	if uploadResponse.StatusCode != http.StatusCreated || json.NewDecoder(uploadResponse.Body).Decode(&uploaded) != nil || uploaded.ID == "" {
		body, _ := io.ReadAll(uploadResponse.Body)
		uploadResponse.Body.Close()
		t.Fatalf("shared single upload=%d: %s", uploadResponse.StatusCode, body)
	}
	uploadResponse.Body.Close()
	var owner, actor, used int64
	var parentID, name string
	if err = f.db.W.QueryRow(`SELECT user_id,parent_id,name FROM nodes WHERE id=?`, uploaded.ID).Scan(&owner, &parentID, &name); err != nil {
		t.Fatal(err)
	}
	if owner != 1 || parentID != share || name != "member.txt" {
		t.Fatalf("stored shared upload owner=%d parent=%s name=%s", owner, parentID, name)
	}
	if err = f.db.W.QueryRow(`SELECT owner_id,actor_id FROM activity WHERE node_id=? AND kind='upload'`, uploaded.ID).Scan(&owner, &actor); err != nil {
		t.Fatal(err)
	}
	if owner != 1 || actor != 2 {
		t.Fatalf("shared upload activity owner=%d actor=%d", owner, actor)
	}
	if err = f.db.W.QueryRow(`SELECT used_bytes FROM users WHERE id=1`).Scan(&used); err != nil || used != int64(len(content)) {
		t.Fatalf("owner quota used=%d err=%v", used, err)
	}

	resumableBody := `{"parentId":"` + share + `","name":"resumable.txt","size":14,"sha256":"` + hex.EncodeToString(sha256Bytes([]byte("resumable data"))) + `"}`
	createRequest, _ := http.NewRequest(http.MethodPost, memberServer.URL+"/api/uploads", strings.NewReader(resumableBody))
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse, err := memberServer.Client().Do(createRequest)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		ID string `json:"id"`
	}
	if createResponse.StatusCode != http.StatusCreated || json.NewDecoder(createResponse.Body).Decode(&session) != nil || session.ID == "" {
		body, _ := io.ReadAll(createResponse.Body)
		createResponse.Body.Close()
		t.Fatalf("shared create resumable=%d: %s", createResponse.StatusCode, body)
	}
	createResponse.Body.Close()
	var createdBy int64
	if err = f.db.W.QueryRow(`SELECT user_id,created_by FROM upload_sessions WHERE id=?`, session.ID).Scan(&owner, &createdBy); err != nil || owner != 1 || createdBy != 2 {
		t.Fatalf("resumable owner=%d creator=%d err=%v", owner, createdBy, err)
	}
	part := []byte("resumable data")
	partRequest, _ := http.NewRequest(http.MethodPut, memberServer.URL+"/api/uploads/"+session.ID+"/parts/0", bytes.NewReader(part))
	partResponse, err := memberServer.Client().Do(partRequest)
	if err != nil {
		t.Fatal(err)
	}
	partResponse.Body.Close()
	if partResponse.StatusCode != http.StatusOK {
		t.Fatalf("shared upload part=%d", partResponse.StatusCode)
	}
	completeRequest, _ := http.NewRequest(http.MethodPost, memberServer.URL+"/api/uploads/"+session.ID+"/complete", strings.NewReader(`{}`))
	completeRequest.Header.Set("Content-Type", "application/json")
	completeResponse, err := memberServer.Client().Do(completeRequest)
	if err != nil {
		t.Fatal(err)
	}
	var completed node
	if completeResponse.StatusCode != http.StatusCreated || json.NewDecoder(completeResponse.Body).Decode(&completed) != nil || completed.ID == "" {
		body, _ := io.ReadAll(completeResponse.Body)
		completeResponse.Body.Close()
		t.Fatalf("shared complete upload=%d: %s", completeResponse.StatusCode, body)
	}
	completeResponse.Body.Close()
	if err = f.db.W.QueryRow(`SELECT actor_id FROM activity WHERE node_id=? AND kind='upload'`, completed.ID).Scan(&actor); err != nil || actor != 2 {
		t.Fatalf("resumable upload actor=%d err=%v", actor, err)
	}
}

func sha256Bytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func TestSharedUploadRejectsReadOnlyAndOutOfGrantParents(t *testing.T) {
	f := newZIPFixture(t)
	ownerRoot := "60000000-0000-4000-8000-000000000001"
	shared := "60000000-0000-4000-8000-000000000002"
	outside := "60000000-0000-4000-8000-000000000003"
	f.addRoot(t, ownerRoot, 1)
	f.addRoot(t, "70000000-0000-4000-8000-000000000001", 2)
	f.addFolder(t, shared, &ownerRoot, 1, "Read only", nil)
	f.addFolder(t, outside, &ownerRoot, 1, "Private", nil)
	addWriteGrant(t, f, shared, "read")
	server := f.server(t, 2)
	for _, tc := range []struct {
		parent string
		status int
		code   string
	}{{shared, http.StatusForbidden, "SHARE_READ_ONLY"}, {outside, http.StatusNotFound, "NOT_FOUND"}} {
		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId="+tc.parent+"&name=denied.txt", strings.NewReader("no"))
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status || !strings.Contains(string(body), `"code":"`+tc.code+`"`) {
			t.Errorf("parent %s response=%d %s want %d/%s", tc.parent, resp.StatusCode, body, tc.status, tc.code)
		}
	}
	var count int
	if err := f.db.W.QueryRow(`SELECT COUNT(*) FROM nodes WHERE name='denied.txt'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("denied upload created %d nodes, err=%v", count, err)
	}
}

func TestSharedCopyRequiresWritableTargetAndAccountsToOwner(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.maxNodes = 100
	ownerRoot := "80000000-0000-4000-8000-000000000001"
	shared := "80000000-0000-4000-8000-000000000002"
	file := "80000000-0000-4000-8000-000000000003"
	f.addRoot(t, ownerRoot, 1)
	f.addRoot(t, "90000000-0000-4000-8000-000000000001", 2)
	f.addFolder(t, shared, &ownerRoot, 1, "Shared", nil)
	f.addFile(t, file, shared, 1, "source.txt", []byte("copy me"))
	addWriteGrant(t, f, shared, "write")
	server := f.server(t, 2)
	resp, err := server.Client().Post(server.URL+"/api/nodes/"+file+"/copy", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("copy shared file=%d: %s", resp.StatusCode, body)
	}
	var copied struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &copied); err != nil || copied.ID == "" {
		t.Fatalf("copy response id=%q err=%v body=%s", copied.ID, err, body)
	}
	var owner, actor int64
	var parent string
	if err = f.db.W.QueryRow(`SELECT user_id,parent_id FROM nodes WHERE id=?`, copied.ID).Scan(&owner, &parent); err != nil {
		t.Fatal(err)
	}
	if owner != 1 || parent != shared {
		t.Fatalf("copy stored under owner=%d parent=%s", owner, parent)
	}
	if err = f.db.W.QueryRow(`SELECT owner_id,actor_id FROM activity WHERE node_id=? AND kind='copy'`, copied.ID).Scan(&owner, &actor); err != nil || owner != 1 || actor != 2 {
		t.Fatalf("copy activity owner=%d actor=%d err=%v", owner, actor, err)
	}
	var refs int
	if err = f.db.W.QueryRow(`SELECT ref_count FROM blobs WHERE id=?`, file).Scan(&refs); err != nil || refs != 2 {
		t.Fatalf("shared copy blob ref_count=%d err=%v", refs, err)
	}
}

func TestSharedCopyRejectsReadOnlyAndOutOfGrantTargets(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.maxNodes = 100
	ownerRoot := "a0000000-0000-4000-8000-000000000001"
	shared := "a0000000-0000-4000-8000-000000000002"
	private := "a0000000-0000-4000-8000-000000000003"
	file := "a0000000-0000-4000-8000-000000000004"
	f.addRoot(t, ownerRoot, 1)
	f.addRoot(t, "b0000000-0000-4000-8000-000000000001", 2)
	f.addFolder(t, shared, &ownerRoot, 1, "Shared", nil)
	f.addFolder(t, private, &ownerRoot, 1, "Private", nil)
	f.addFile(t, file, shared, 1, "source.txt", []byte("copy me"))
	addWriteGrant(t, f, shared, "read")
	server := f.server(t, 2)
	for _, tc := range []struct {
		parent string
		status int
		code   string
	}{{shared, http.StatusForbidden, "SHARE_READ_ONLY"}, {private, http.StatusNotFound, "NOT_FOUND"}} {
		body := `{"parentId":"` + tc.parent + `"}`
		resp, err := server.Client().Post(server.URL+"/api/nodes/"+file+"/copy", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		responseBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status || !strings.Contains(string(responseBody), tc.code) {
			t.Errorf("copy target %s status=%d body=%s", tc.parent, resp.StatusCode, responseBody)
		}
	}
}

func TestCrossAccountCopyReencryptsOwnerAndSharedFolderTrees(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.defaultQuotaBytes = 1 << 20
	f.handler.uploadTmpDir = t.TempDir()
	f.handler.maxNodes = 100
	ownerRoot := "c0000000-0000-4000-8000-000000000001"
	memberRoot := "c0000000-0000-4000-8000-000000000002"
	sharedFromOwner := "c0000000-0000-4000-8000-000000000003"
	sourceFolder := "c0000000-0000-4000-8000-000000000004"
	sourceFile := "c0000000-0000-4000-8000-000000000005"
	sharedToOwner := "d0000000-0000-4000-8000-000000000001"
	destFolder := "d0000000-0000-4000-8000-000000000002"
	f.addRoot(t, ownerRoot, 1)
	f.addRoot(t, memberRoot, 2)
	f.addFolder(t, sharedFromOwner, &ownerRoot, 1, "Shared from owner", nil)
	f.addFolder(t, sourceFolder, &sharedFromOwner, 1, "Source", nil)
	f.addFile(t, sourceFile, sourceFolder, 1, "source.txt", []byte("re-encrypted bytes"))
	f.addFolder(t, sharedToOwner, &memberRoot, 2, "Shared to owner", nil)
	f.addFolder(t, destFolder, &sharedToOwner, 2, "Destination", nil)
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,?,2,'write','zip-member')`, sharedFromOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(2,?,1,'write','zip-owner')`, sharedToOwner); err != nil {
		t.Fatal(err)
	}

	ownerServer := f.server(t, 1)
	requestBody := `{"parentId":"` + destFolder + `"}`
	resp, err := ownerServer.Client().Post(ownerServer.URL+"/api/nodes/"+sourceFolder+"/copy", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	var copiedShared struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&copiedShared) != nil || copiedShared.ID == "" {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("owner to shared copy=%d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	var destOwner int64
	if err = f.db.W.QueryRow(`SELECT user_id FROM nodes WHERE id=?`, copiedShared.ID).Scan(&destOwner); err != nil || destOwner != 2 {
		t.Fatalf("owner to shared tree owner=%d err=%v", destOwner, err)
	}
	var blobID string
	if err = f.db.W.QueryRow(`SELECT blob_id FROM nodes WHERE user_id=2 AND parent_id=? AND name='source.txt'`, copiedShared.ID).Scan(&blobID); err != nil {
		t.Fatal(err)
	}
	if err = f.db.W.QueryRow(`SELECT user_id FROM blobs WHERE id=?`, blobID).Scan(&destOwner); err != nil || destOwner != 2 {
		t.Fatalf("owner to shared blob owner=%d err=%v", destOwner, err)
	}
	dek2, err := f.keys.GetUserDEK(context.Background(), f.db.W, 2)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.Open(blobPath(f.blobDir, 2, blobID))
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if _, err = cryptox.DecryptBlobRange(stored, int64(len("re-encrypted bytes")), 0, int64(len("re-encrypted bytes"))-1, dek2, blobID, &plain); err != nil || plain.String() != "re-encrypted bytes" {
		stored.Close()
		t.Fatalf("destination-encrypted content=%q err=%v", plain.String(), err)
	}
	stored.Close()
	var activityOwner, activityActor int64
	if err = f.db.W.QueryRow(`SELECT owner_id,actor_id FROM activity WHERE node_id=? AND kind='folder'`, copiedShared.ID).Scan(&activityOwner, &activityActor); err != nil || activityOwner != 2 || activityActor != 1 {
		t.Fatalf("owner to shared activity owner=%d actor=%d err=%v", activityOwner, activityActor, err)
	}

	memberServer := f.server(t, 2)
	requestBody = `{"parentId":"` + memberRoot + `"}`
	resp, err = memberServer.Client().Post(memberServer.URL+"/api/nodes/"+sourceFolder+"/copy", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	var copiedOwn struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&copiedOwn) != nil || copiedOwn.ID == "" {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("shared to owner copy=%d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()
	if err = f.db.W.QueryRow(`SELECT user_id FROM nodes WHERE id=?`, copiedOwn.ID).Scan(&destOwner); err != nil || destOwner != 2 {
		t.Fatalf("shared to owner tree owner=%d err=%v", destOwner, err)
	}
	if err = f.db.W.QueryRow(`SELECT owner_id,actor_id FROM activity WHERE node_id=? AND kind='folder'`, copiedOwn.ID).Scan(&activityOwner, &activityActor); err != nil || activityOwner != 2 || activityActor != 2 {
		t.Fatalf("shared to owner activity owner=%d actor=%d err=%v", activityOwner, activityActor, err)
	}
}

func TestCrossAccountCopyFailureLeavesNoPartialTreeOrBlobs(t *testing.T) {
	f := newZIPFixture(t)
	f.handler.defaultQuotaBytes = 1 << 20
	f.handler.uploadTmpDir = t.TempDir()
	f.handler.maxNodes = 100
	ownerRoot := "e0000000-0000-4000-8000-000000000001"
	memberRoot := "e0000000-0000-4000-8000-000000000002"
	shared := "e0000000-0000-4000-8000-000000000003"
	source := "e0000000-0000-4000-8000-000000000004"
	valid := "e0000000-0000-4000-8000-000000000005"
	corrupt := "e0000000-0000-4000-8000-000000000006"
	f.addRoot(t, ownerRoot, 1)
	f.addRoot(t, memberRoot, 2)
	f.addFolder(t, shared, &ownerRoot, 1, "Shared", nil)
	f.addFolder(t, source, &shared, 1, "Source", nil)
	f.addFile(t, valid, source, 1, "a.txt", []byte("valid first"))
	f.addFile(t, corrupt, source, 1, "b.txt", []byte("corrupt later"))
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,?,2,'read','zip-member')`, shared); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.W.Exec(`UPDATE nodes SET blob_id=? WHERE id=?`, corrupt, corrupt); err != nil {
		t.Fatal(err)
	}
	badPath := blobPath(f.blobDir, 1, corrupt)
	if err := os.WriteFile(badPath, []byte("not an encrypted blob"), 0600); err != nil {
		t.Fatal(err)
	}
	before := 0
	if err := f.db.W.QueryRow(`SELECT node_count FROM users WHERE id=2`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	server := f.server(t, 2)
	body := `{"parentId":"` + memberRoot + `"}`
	resp, err := server.Client().Post(server.URL+"/api/nodes/"+source+"/copy", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("corrupt copy unexpectedly succeeded: %d %s", resp.StatusCode, response)
	}
	var after, copied int
	if err = f.db.W.QueryRow(`SELECT node_count FROM users WHERE id=2`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err = f.db.W.QueryRow(`SELECT COUNT(*) FROM nodes WHERE user_id=2 AND name='Source'`).Scan(&copied); err != nil {
		t.Fatal(err)
	}
	if after != before || copied != 0 {
		t.Fatalf("failed copy changed tree: node_count %d->%d copies=%d", before, after, copied)
	}
	var blobs int
	if err = f.db.W.QueryRow(`SELECT COUNT(*) FROM blobs WHERE user_id=2`).Scan(&blobs); err != nil || blobs != 0 {
		t.Fatalf("failed copy destination blobs=%d err=%v", blobs, err)
	}
}
