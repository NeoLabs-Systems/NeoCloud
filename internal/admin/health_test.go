package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
)

func TestAdminHealthReportsLiveMetricsAndOmitsUnavailableChecks(t *testing.T) {
	db, mux := testHandler(t)
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,type,name,etag) VALUES('health-root',1,'folder','','root-etag')`); err != nil {
		t.Fatal(err)
	}
	var root string
	if err := db.W.QueryRow(`SELECT id FROM nodes WHERE user_id=1 AND parent_id IS NULL`).Scan(&root); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('health-file',1,?,'file','health.txt','etag')`, root); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec(`INSERT INTO upload_sessions(id,user_id,parent_id,name,conflict,total_size,chunk_size,expires_at) VALUES('health-upload',1,'root','upload.bin','rename',10,10,'2099-01-01')`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("health=%d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["database"] != "reachable" || body["users"] != float64(2) || body["files"] != float64(1) || body["activeUploads"] != float64(1) {
		t.Fatalf("unexpected health response: %s", response.Body.String())
	}
	checkedAt, ok := body["databaseCheckedAt"].(string)
	if !ok {
		t.Fatalf("missing database check time: %s", response.Body.String())
	}
	if _, err := time.Parse(time.RFC3339Nano, checkedAt); err != nil {
		t.Fatalf("invalid database check time %q: %v", checkedAt, err)
	}
	for _, unavailable := range []string{"damagedBlobs", "storageDisk", "dataDisk", "minFreeDiskBytes"} {
		if _, exists := body[unavailable]; exists {
			t.Fatalf("reported unavailable metric %q: %s", unavailable, response.Body.String())
		}
	}
	if denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodGet, "/api/admin/health", ""); denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "ADMIN_REQUIRED") {
		t.Fatalf("non-admin health=%d %s", denied.Code, denied.Body.String())
	}
}

func TestAdminVersionMatchesGoRuntimeAndRequiresAdmin(t *testing.T) {
	_, mux := testHandler(t)
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/version", "")
	if response.Code != http.StatusOK {
		t.Fatalf("version=%d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "NeoCloud" || body["product"] != "neocloud" || body["go"] == "" || body["version"] != buildinfo.Version {
		t.Fatalf("unexpected version response: %s", response.Body.String())
	}
	if denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodGet, "/api/admin/version", ""); denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "ADMIN_REQUIRED") {
		t.Fatalf("non-admin version=%d %s", denied.Code, denied.Body.String())
	}
}
