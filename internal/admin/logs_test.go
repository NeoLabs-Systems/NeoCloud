package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/loghistory"
)

func TestAdminLogsRequireAdmin(t *testing.T) {
	db, mux := testHandler(t)
	history, err := loghistory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = history.Writer(nil).Write([]byte(`{"time":"2026-11-27T10:00:00Z","level":"INFO","msg":"visible"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{DB: db.W, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		principal, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
		return Principal{UserID: principal.ID, Device: principal.Device, Platform: principal.Platform}, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	h.LogsRoutes(mux, history)

	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/logs", "")
	if response.Code != http.StatusOK {
		t.Fatalf("admin logs=%d %s", response.Code, response.Body.String())
	}
	var body struct {
		Logs []loghistory.Entry `json:"logs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Logs) != 1 || body.Logs[0].Message != "visible" {
		t.Fatalf("unexpected log history response %s (decode error %v)", response.Body.String(), err)
	}
	denied := adminRequest(mux, testPrincipal{ID: 2}, http.MethodGet, "/api/admin/logs", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin logs=%d %s", denied.Code, denied.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/admin/logs", nil)
	unauthenticated := httptest.NewRecorder()
	mux.ServeHTTP(unauthenticated, request)
	if unauthenticated.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated logs=%d, want forbidden", unauthenticated.Code)
	}
}
