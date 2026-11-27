package members

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type key struct{}

func setup(t *testing.T) (*dbx.DB, *http.ServeMux) {
	t.Helper()
	ctx := context.Background()
	db, e := dbx.Open(ctx, filepath.Join(t.TempDir(), "members.sqlite"), dbx.Options{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	if e = schema.Migrate(ctx, db, filepath.Join(t.TempDir(), "backups")); e != nil {
		t.Fatal(e)
	}
	if _, e = db.W.Exec(`INSERT INTO users(username,display_name,password) VALUES('viewer','Viewer','x'),('owner','Owner','x'),('inactive','Inactive','x')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('r1',2,NULL,'folder','','r1'),('shared',2,'r1','folder','Shared','s'),('child',2,'shared','file','secret.txt','c'),('trashed',2,'r1','folder','Trash','t'),('vault',2,'r1','folder','Vault','v'),('r3',3,NULL,'folder','','r3'),('shared3',3,'r3','folder','Shared inactive','s3')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.W.Exec(`UPDATE nodes SET trashed_at=datetime('now') WHERE id='trashed'; UPDATE nodes SET vault_id='vault' WHERE id='vault'`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.W.Exec(`UPDATE users SET disabled_at=datetime('now') WHERE id=3`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.W.Exec(`INSERT INTO user_shares(owner_id,node_id,grantee_id,permission,recipient) VALUES(2,'shared',1,'write','viewer'),(2,'trashed',1,'read','viewer'),(2,'vault',1,'read','viewer'),(3,'shared3',1,'read','viewer')`); e != nil {
		t.Fatal(e)
	}
	h, e := New(Options{DB: db.W, NodeViews: files.New(files.Options{DB: db.W}).NodeViews, ResolvePrincipal: func(r *http.Request) (Principal, bool) {
		id, ok := r.Context().Value(key{}).(int64)
		return Principal{UserID: id}, ok
	}})
	if e != nil {
		t.Fatal(e)
	}
	m := http.NewServeMux()
	h.Routes(m)
	return db, m
}

func TestLeavingShareRemovesGrant(t *testing.T) {
	db, mux := setup(t)
	var shareID int64
	if err := db.W.QueryRow(`SELECT id FROM user_shares WHERE owner_id=2 AND node_id='shared' AND grantee_id=1`).Scan(&shareID); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodDelete, "/api/shared-with-me/"+strconv.FormatInt(shareID, 10), nil)
	r = r.WithContext(context.WithValue(context.Background(), key{}, int64(1)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("leave status=%d body=%s", w.Code, w.Body.String())
	}
	var remaining int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM user_shares WHERE id=?`, shareID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("left share remains count=%d err=%v", remaining, err)
	}
}

func TestSharedWithMeFiltersInvalidGrantsAndHidesOwnerParent(t *testing.T) {
	_, m := setup(t)
	r := httptest.NewRequest("GET", "/api/shared-with-me", nil).WithContext(context.WithValue(context.Background(), key{}, int64(1)))
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Shares []share `json:"shares"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	if len(body.Shares) != 1 {
		t.Fatalf("shares: %s", w.Body.String())
	}
	s := body.Shares[0]
	if s.Node["id"] != "shared" || s.Node["parentId"] != nil || s.Node["aiTags"] != nil || s.Node["favorite"] != false || s.OwnerName != "Owner" || s.Permission != "write" {
		t.Fatalf("share: %#v", s)
	}
}
func TestSharedWithMeRequiresIdentity(t *testing.T) {
	_, m := setup(t)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/api/shared-with-me", nil))
	if w.Code != 401 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGrantExpiryAcceptsNodeDateParseISOFormsAndStoresUTC(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "2035-01-02", want: "2035-01-02 00:00:00"},
		{input: "2035-01-02T03:04:05+02:00", want: "2035-01-02 01:04:05"},
	} {
		got, err := parseGrantExpiry(test.input)
		if err != nil || got != test.want {
			t.Errorf("parseGrantExpiry(%q) = %#v, %v; want %q", test.input, got, err, test.want)
		}
	}
	if _, err := parseGrantExpiry("not a date"); err == nil {
		t.Fatal("invalid date was accepted")
	}
}

func TestUpdateMemberDistinguishesOmittedAndNullExpiry(t *testing.T) {
	db, mux := setup(t)
	var shareID int64
	if err := db.W.QueryRow(`SELECT id FROM user_shares WHERE owner_id=2 AND node_id='shared' AND grantee_id=1`).Scan(&shareID); err != nil {
		t.Fatal(err)
	}
	expiry := "2099-01-01 00:00:00"
	if _, err := db.W.Exec(`UPDATE user_shares SET expires_at=? WHERE id=?`, expiry, shareID); err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPatch, "/api/nodes/shared/members/"+strconv.FormatInt(shareID, 10), strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), key{}, int64(2)))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := call(`{"permission":"read"}`); w.Code != 200 {
		t.Fatalf("omitted expiry patch status=%d body=%s", w.Code, w.Body.String())
	}
	var got sql.NullString
	if err := db.W.QueryRow(`SELECT expires_at FROM user_shares WHERE id=?`, shareID).Scan(&got); err != nil || !got.Valid || got.String != expiry {
		t.Fatalf("omitted expiry changed: %v err=%v", got, err)
	}
	if w := call(`{"expiresAt":null}`); w.Code != 200 {
		t.Fatalf("null expiry patch status=%d body=%s", w.Code, w.Body.String())
	}
	if err := db.W.QueryRow(`SELECT expires_at FROM user_shares WHERE id=?`, shareID).Scan(&got); err != nil || got.Valid {
		t.Fatalf("null expiry did not clear: %v err=%v", got, err)
	}
}

func TestUpdateMemberPermissionMatchesNodeNullAndEmptySemantics(t *testing.T) {
	db, mux := setup(t)
	var shareID int64
	if err := db.W.QueryRow(`SELECT id FROM user_shares WHERE owner_id=2 AND node_id='shared' AND grantee_id=1`).Scan(&shareID); err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPatch, "/api/nodes/shared/members/"+strconv.FormatInt(shareID, 10), strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), key{}, int64(2)))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{"permission":"write"}`, `{"permission":null}`} {
		if w := call(body); w.Code != http.StatusOK {
			t.Fatalf("permission patch %s status=%d body=%s", body, w.Code, w.Body.String())
		}
	}
	var permission string
	if err := db.W.QueryRow(`SELECT permission FROM user_shares WHERE id=?`, shareID).Scan(&permission); err != nil || permission != "read" {
		t.Fatalf("null should reset permission to read: got %q err=%v", permission, err)
	}
	if w := call(`{"permission":"write"}`); w.Code != http.StatusOK {
		t.Fatalf("write patch status=%d body=%s", w.Code, w.Body.String())
	}
	if w := call(`{"permission":""}`); w.Code != http.StatusOK {
		t.Fatalf("empty permission patch status=%d body=%s", w.Code, w.Body.String())
	}
	if err := db.W.QueryRow(`SELECT permission FROM user_shares WHERE id=?`, shareID).Scan(&permission); err != nil || permission != "read" {
		t.Fatalf("empty should reset permission to read: got %q err=%v", permission, err)
	}
	if w := call(`{}`); w.Code != http.StatusOK {
		t.Fatalf("omitted permission patch status=%d body=%s", w.Code, w.Body.String())
	}
	if err := db.W.QueryRow(`SELECT permission FROM user_shares WHERE id=?`, shareID).Scan(&permission); err != nil || permission != "read" {
		t.Fatalf("omitted permission should be retained: got %q err=%v", permission, err)
	}
}

func TestAddMemberIsAccountScopedAndRecordsActivityTransactionally(t *testing.T) {
	db, mux := setup(t)
	body := `{"recipient":"viewer","permission":"read","expiresAt":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/api/nodes/child/members", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), key{}, int64(2)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("add status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM activity WHERE owner_id=2 AND actor_id=2 AND kind='share' AND node_id='child'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("share activity count=%d err=%v", count, err)
	}
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/nodes/child/members", nil)
	ownerRequest = ownerRequest.WithContext(context.WithValue(context.Background(), key{}, int64(2)))
	listed := httptest.NewRecorder()
	mux.ServeHTTP(listed, ownerRequest)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"recipient":"viewer"`) {
		t.Fatalf("owner list status=%d body=%s", listed.Code, listed.Body.String())
	}
	other := httptest.NewRequest(http.MethodGet, "/api/nodes/child/members", nil)
	other = other.WithContext(context.WithValue(context.Background(), key{}, int64(1)))
	denied := httptest.NewRecorder()
	mux.ServeHTTP(denied, other)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("cross-account list status=%d body=%s", denied.Code, denied.Body.String())
	}
}
