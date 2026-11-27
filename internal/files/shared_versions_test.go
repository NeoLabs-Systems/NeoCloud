package files

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	requestauth "github.com/NeoLabs-Systems/neocloud/internal/auth"
)

func TestSharedMemberCannotDownloadOwnersRetainedVersion(t *testing.T) {
	f := newZIPFixture(t)
	root := "50000000-0000-4000-8000-000000000001"
	shared := "50000000-0000-4000-8000-000000000002"
	fileID := "50000000-0000-4000-8000-000000000003"
	f.addRoot(t, root, 1)
	f.addFolder(t, shared, &root, 1, "Shared", nil)
	f.addFile(t, fileID, shared, 1, "report.txt", []byte("historical content"))
	if _, err := f.db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(1,?,2,'read','member')`, shared); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.W.Exec(`INSERT INTO file_versions(node_id,blob_id,version,size,etag,created_at,modified_by) VALUES(?,?,1,18,'old-etag','2026-01-01 00:00:00',1)`, fileID, fileID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.handler.Routes(mux)
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID+"/content?version=1", nil)
	ownerRequest = ownerRequest.WithContext(requestauth.WithIdentity(context.Background(), requestauth.RequestIdentity{UserID: 1}))
	ownerResponse := httptest.NewRecorder()
	mux.ServeHTTP(ownerResponse, ownerRequest)
	if ownerResponse.Code != http.StatusOK || ownerResponse.Body.String() != "historical content" {
		t.Fatalf("owner historical download status=%d body=%q", ownerResponse.Code, ownerResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID+"/content?version=1", nil)
	request = request.WithContext(requestauth.WithIdentity(context.Background(), requestauth.RequestIdentity{UserID: 2}))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("shared historical download status=%d body=%s", response.Code, response.Body.String())
	}
	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/files/" + fileID + "/versions"},
		{method: http.MethodPost, path: "/api/files/" + fileID + "/versions/1/restore"},
	} {
		request := httptest.NewRequest(tc.method, tc.path, nil)
		request = request.WithContext(requestauth.WithIdentity(context.Background(), requestauth.RequestIdentity{UserID: 2}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, request)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":"NOT_FOUND"`) {
			t.Fatalf("shared historical %s %s status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
