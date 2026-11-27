package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestAuditRouteListsLatestEventsAndReferencedUsernames(t *testing.T) {
	db, mux := testHandler(t)
	h, err := New(Options{
		DB: db.W,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID, Device: p.Device, Platform: p.Platform}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.AuditRoutes(mux)
	if _, err = db.W.Exec(`INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES
		('account.updated',1,2,'{"relatedUserId":2,"unrelated":3}')`); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/audit", "")
	if response.Code != http.StatusOK {
		t.Fatalf("audit=%d %s", response.Code, response.Body.String())
	}
	var result struct {
		Entries   []auditEntry      `json:"entries"`
		Usernames map[string]string `json:"usernames"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries=%s", response.Body.String())
	}
	entry := result.Entries[0]
	if entry.Action != "account.updated" || entry.Actor == nil || entry.Actor.ID != 1 || entry.Actor.Username != "administrator" || entry.Subject == nil || entry.Subject.ID != 2 || entry.Subject.Username != "account" {
		t.Fatalf("entry=%+v", entry)
	}
	if result.Usernames["2"] != "account" || len(result.Usernames) != 1 {
		t.Fatalf("usernames=%v", result.Usernames)
	}
	detail, ok := entry.Detail.(map[string]any)
	if !ok || detail["relatedUserId"] != float64(2) {
		t.Fatalf("detail=%#v", entry.Detail)
	}
	for _, p := range []testPrincipal{{ID: 2}, {}} {
		denied := adminRequest(mux, p, http.MethodGet, "/api/admin/audit", "")
		if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "ADMIN_REQUIRED") {
			t.Fatalf("non-admin audit access=%d %s", denied.Code, denied.Body.String())
		}
	}
}

func TestAuditRouteReturnsOnlyNewestTwoHundredEvents(t *testing.T) {
	db, mux := testHandler(t)
	h, err := New(Options{
		DB: db.W,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			p, ok := r.Context().Value(principalContextKey{}).(testPrincipal)
			return Principal{UserID: p.ID, Device: p.Device, Platform: p.Platform}, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.AuditRoutes(mux)
	tx, err := db.W.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		if _, err = tx.Exec(`INSERT INTO access_audit_log(action,detail_json) VALUES(?, '{}')`, "event."+strconv.Itoa(i)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	response := adminRequest(mux, testPrincipal{ID: 1}, http.MethodGet, "/api/admin/audit", "")
	if response.Code != http.StatusOK {
		t.Fatalf("audit=%d %s", response.Code, response.Body.String())
	}
	var result struct {
		Entries []auditEntry `json:"entries"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != auditEventLimit || result.Entries[0].Action != "event.204" || result.Entries[len(result.Entries)-1].Action != "event.5" {
		t.Fatalf("audit limit/order: first=%+v last=%+v count=%d", result.Entries[0], result.Entries[len(result.Entries)-1], len(result.Entries))
	}
}
