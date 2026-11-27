package application

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/ai"
	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/documentconvert"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestComposedRuntimeServesHealthAndFirstAccountRegistration(t *testing.T) {
	home := t.TempDir()
	// A stand-in web client: a checkout has none until a release builds it.
	appDir := t.TempDir()
	webDir := config.WebClientDir(appDir)
	if err := os.MkdirAll(webDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<base href="/app/">`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := Open(context.Background(), func(key string) string {
		switch key {
		case "NEOCLOUD_HOME":
			return home
		case "NEOCLOUD_APP_DIR":
			return appDir
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	app.StartWorkers(context.Background())
	app.StartWorkers(context.Background())
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	for _, endpoint := range []string{"/health/live", "/health/ready", "/api/health", "/api/version"} {
		response, err := server.Client().Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", endpoint, response.StatusCode)
		}
	}
	oauthRequest, err := http.NewRequest(http.MethodPost, server.URL+"/oauth/token", strings.NewReader(`{"grant_type":"unsupported"}`))
	if err != nil {
		t.Fatal(err)
	}
	oauthRequest.Header.Set("Content-Type", "application/json")
	oauthResponse, err := server.Client().Do(oauthRequest)
	if err != nil {
		t.Fatal(err)
	}
	oauthBody, err := io.ReadAll(oauthResponse.Body)
	oauthResponse.Body.Close()
	if err != nil || oauthResponse.StatusCode != http.StatusBadRequest || !strings.Contains(string(oauthBody), "unsupported_grant_type") {
		t.Fatalf("composed OAuth token endpoint = %d: %s (%v)", oauthResponse.StatusCode, oauthBody, err)
	}
	bootstrapRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/oauth/companion/neoagent/bootstrap", strings.NewReader(`{"redirectUri":"http://localhost:3333/api/integrations/oauth/callback","appName":"NeoAgent"}`))
	bootstrapRequest.Header.Set("Content-Type", "application/json")
	bootstrapResponse, err := server.Client().Do(bootstrapRequest)
	if err != nil {
		t.Fatal(err)
	}
	var bootstrap struct {
		Companion             string `json:"companion"`
		ClientID              string `json:"clientId"`
		AuthorizationEndpoint string `json:"authorizationEndpoint"`
	}
	if bootstrapResponse.StatusCode != http.StatusOK || json.NewDecoder(bootstrapResponse.Body).Decode(&bootstrap) != nil || bootstrap.Companion != "neoagent" || bootstrap.ClientID == "" || bootstrap.AuthorizationEndpoint != server.URL+"/oauth/authorize" {
		body, _ := io.ReadAll(bootstrapResponse.Body)
		bootstrapResponse.Body.Close()
		t.Fatalf("OAuth companion bootstrap = %d: %s", bootstrapResponse.StatusCode, body)
	}
	bootstrapResponse.Body.Close()
	versionResponse, err := server.Client().Get(server.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	var version struct {
		Name, Product, Version, Go string
	}
	if err = json.NewDecoder(versionResponse.Body).Decode(&version); err != nil {
		versionResponse.Body.Close()
		t.Fatal(err)
	}
	versionResponse.Body.Close()
	if versionResponse.StatusCode != http.StatusOK || version.Name != "NeoCloud" || version.Product != "neocloud" || version.Version != buildinfo.Version || version.Go == "" {
		t.Fatalf("version response = %#v status=%d", version, versionResponse.StatusCode)
	}
	webResponse, err := server.Client().Get(server.URL + "/app/")
	if err != nil {
		t.Fatal(err)
	}
	webHTML, err := io.ReadAll(webResponse.Body)
	webResponse.Body.Close()
	if err != nil || webResponse.StatusCode != http.StatusOK || !bytes.Contains(webHTML, []byte(`<base href="/app/">`)) {
		t.Fatalf("web app = status %d, error %v", webResponse.StatusCode, err)
	}
	status, err := server.Client().Get(server.URL + "/api/auth/status")
	if err != nil {
		t.Fatal(err)
	}
	status.Body.Close()
	if status.StatusCode != http.StatusOK {
		t.Fatalf("auth status = %d", status.StatusCode)
	}
	agentFiles, err := server.Client().Get(server.URL + "/api/v1/files")
	if err != nil {
		t.Fatal(err)
	}
	agentFiles.Body.Close()
	if agentFiles.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated agent files = %d", agentFiles.StatusCode)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"first-cloud-user","email":"first@example.test","password":"G7!mQv#2xL9rT4pZ"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("register = %d", response.StatusCode)
	}
	if len(response.Cookies()) != 1 || response.Cookies()[0].Name != "neocloud.sid" {
		t.Fatalf("registration session cookie = %#v", response.Cookies())
	}
	var registeredUserID int64
	var accountRootID string
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='first-cloud-user'`).Scan(&registeredUserID); err != nil {
		t.Fatal(err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL`, registeredUserID).Scan(&accountRootID); errors.Is(err, sql.ErrNoRows) {
		accountRootID = "root"
		if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('root',?,NULL,'folder','','root-etag')`, registeredUserID); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	// The on-demand AI summary endpoint must be reachable in the composed
	// application and return a cache hit without requiring provider access.
	app.aiHandler.SetPolicy(true, 0)
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO user_settings(user_id,key,value) VALUES(?,'ai_opt_in','true') ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value`, registeredUserID); err != nil {
		t.Fatal(err)
	}
	const summaryFileID = "32345678-1234-1234-1234-123456789abc"
	const summaryBlobID = "42345678-1234-1234-1234-123456789abc"
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,?,1,?,1,1)`, summaryBlobID, registeredUserID, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag) VALUES(?,?,?,'file','summary.txt',1,'text/plain',?,'summary-etag')`, summaryFileID, registeredUserID, accountRootID, summaryBlobID); err != nil {
		t.Fatal(err)
	}
	dek, err := app.Runtime.Keys.GetUserDEK(context.Background(), app.Runtime.DB.W, registeredUserID)
	if err != nil {
		t.Fatal(err)
	}
	sealedSummary, err := cryptox.SealBytes(dek, []byte("Cached summary."), []byte("ai-summary:"+summaryFileID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO ai_summaries(node_id,user_id,etag,summary_enc,model) VALUES(?,?,?,?,'fixture::model')`, summaryFileID, registeredUserID, "summary-etag", sealedSummary); err != nil {
		t.Fatal(err)
	}
	summaryRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/ai/files/"+summaryFileID+"/summary", strings.NewReader(`{}`))
	summaryRequest.AddCookie(response.Cookies()[0])
	summaryResponse, err := server.Client().Do(summaryRequest)
	if err != nil {
		t.Fatal(err)
	}
	var summaryPayload ai.Summary
	if summaryResponse.StatusCode != http.StatusOK || json.NewDecoder(summaryResponse.Body).Decode(&summaryPayload) != nil {
		body, _ := io.ReadAll(summaryResponse.Body)
		summaryResponse.Body.Close()
		t.Fatalf("composed AI summary = %d: %s", summaryResponse.StatusCode, body)
	}
	summaryResponse.Body.Close()
	if summaryPayload.Summary != "Cached summary." || !summaryPayload.Cached || summaryPayload.ETag != "summary-etag" {
		t.Fatalf("composed AI summary payload = %#v", summaryPayload)
	}
	duplicatesRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/ai/duplicates", nil)
	duplicatesRequest.AddCookie(response.Cookies()[0])
	duplicatesResponse, err := server.Client().Do(duplicatesRequest)
	if err != nil {
		t.Fatal(err)
	}
	var duplicatePayload struct {
		Groups  []any `json:"groups"`
		Pending int   `json:"pending"`
		Hashed  int   `json:"hashed"`
		Total   int   `json:"total"`
	}
	if duplicatesResponse.StatusCode != http.StatusOK || json.NewDecoder(duplicatesResponse.Body).Decode(&duplicatePayload) != nil {
		body, _ := io.ReadAll(duplicatesResponse.Body)
		duplicatesResponse.Body.Close()
		t.Fatalf("composed AI duplicates = %d: %s", duplicatesResponse.StatusCode, body)
	}
	duplicatesResponse.Body.Close()
	if len(duplicatePayload.Groups) != 0 {
		t.Fatalf("unexpected composed duplicate groups: %#v", duplicatePayload.Groups)
	}
	const suggestionFileID = "12345678-1234-1234-1234-123456789abc"
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,?,'file','draft.txt','before-etag')`, suggestionFileID, registeredUserID, accountRootID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO ai_suggestions(id,user_id,node_id,kind,from_json,to_json,reason,confidence,tier,status,source,basis_name,basis_parent_id,basis_etag)
		VALUES('composed-rename',?,?,'rename','"draft.txt"','"final.txt"','test proposal',0.95,'high','pending','ai','draft.txt',?,'before-etag')`, registeredUserID, suggestionFileID, accountRootID); err != nil {
		t.Fatal(err)
	}
	acceptRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/ai/suggestions/composed-rename/accept", strings.NewReader(`{}`))
	acceptRequest.AddCookie(response.Cookies()[0])
	acceptResponse, err := server.Client().Do(acceptRequest)
	if err != nil {
		t.Fatal(err)
	}
	var acceptedPayload map[string]any
	if acceptResponse.StatusCode != http.StatusOK || json.NewDecoder(acceptResponse.Body).Decode(&acceptedPayload) != nil {
		body, _ := io.ReadAll(acceptResponse.Body)
		acceptResponse.Body.Close()
		t.Fatalf("composed AI accept = %d: %s", acceptResponse.StatusCode, body)
	}
	acceptResponse.Body.Close()
	var acceptedName, acceptedStatus string
	if err = app.Runtime.DB.W.QueryRow(`SELECT name FROM nodes WHERE id=?`, suggestionFileID).Scan(&acceptedName); err != nil {
		t.Fatal(err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT status FROM ai_suggestions WHERE id='composed-rename'`).Scan(&acceptedStatus); err != nil || acceptedName != "final.txt" || acceptedStatus != "accepted" {
		t.Fatalf("composed AI acceptance name=%q status=%q err=%v", acceptedName, acceptedStatus, err)
	}
	undoRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/ai/suggestions/composed-rename/undo", nil)
	undoRequest.AddCookie(response.Cookies()[0])
	undoResponse, err := server.Client().Do(undoRequest)
	if err != nil {
		t.Fatal(err)
	}
	var undoPayload map[string]any
	if undoResponse.StatusCode != http.StatusOK || json.NewDecoder(undoResponse.Body).Decode(&undoPayload) != nil {
		body, _ := io.ReadAll(undoResponse.Body)
		undoResponse.Body.Close()
		t.Fatalf("composed AI undo = %d: %s", undoResponse.StatusCode, body)
	}
	undoResponse.Body.Close()
	if err = app.Runtime.DB.W.QueryRow(`SELECT name FROM nodes WHERE id=?`, suggestionFileID).Scan(&acceptedName); err != nil {
		t.Fatal(err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT status FROM ai_suggestions WHERE id='composed-rename'`).Scan(&acceptedStatus); err != nil || acceptedName != "draft.txt" || acceptedStatus != "undone" {
		t.Fatalf("composed AI undo name=%q status=%q err=%v", acceptedName, acceptedStatus, err)
	}
	healthRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/admin/health", nil)
	healthRequest.AddCookie(response.Cookies()[0])
	healthResponse, err := server.Client().Do(healthRequest)
	if err != nil {
		t.Fatal(err)
	}
	var adminHealth map[string]any
	if err = json.NewDecoder(healthResponse.Body).Decode(&adminHealth); err != nil {
		healthResponse.Body.Close()
		t.Fatal(err)
	}
	healthResponse.Body.Close()
	if healthResponse.StatusCode != http.StatusOK || adminHealth["database"] != "pending" || adminHealth["databaseConnection"] != "reachable" || adminHealth["users"] != float64(1) || adminHealth["version"] == nil || adminHealth["runtime"] == nil {
		t.Fatalf("admin health=%d payload=%#v", healthResponse.StatusCode, adminHealth)
	}
	oauthQuery := url.Values{
		"response_type": {"code"}, "client_id": {bootstrap.ClientID},
		"redirect_uri": {"http://localhost:3333/api/integrations/oauth/callback"},
		"state":        {"cloud-e2e-state"}, "scope": {"files:read"},
		"code_challenge": {strings.Repeat("A", 43)}, "code_challenge_method": {"S256"},
	}
	noRedirectClient := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	oauthConsentRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/oauth/authorize?"+oauthQuery.Encode(), nil)
	oauthConsentRequest.AddCookie(response.Cookies()[0])
	oauthConsentResponse, err := noRedirectClient.Do(oauthConsentRequest)
	if err != nil {
		t.Fatal(err)
	}
	consentPage, _ := io.ReadAll(oauthConsentResponse.Body)
	oauthConsentResponse.Body.Close()
	consentToken := regexp.MustCompile(`name="consent" value="([^"]+)"`).FindSubmatch(consentPage)
	if oauthConsentResponse.StatusCode != http.StatusOK || len(consentToken) != 2 {
		t.Fatalf("composed OAuth consent = %d: %s", oauthConsentResponse.StatusCode, consentPage)
	}
	oauthDecision := url.Values{"consent": {string(consentToken[1])}, "decision": {"approve"}}
	oauthDecisionRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/oauth/authorize", strings.NewReader(oauthDecision.Encode()))
	oauthDecisionRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	oauthDecisionRequest.AddCookie(response.Cookies()[0])
	oauthDecisionResponse, err := noRedirectClient.Do(oauthDecisionRequest)
	if err != nil {
		t.Fatal(err)
	}
	location, parseErr := url.Parse(oauthDecisionResponse.Header.Get("Location"))
	oauthDecisionResponse.Body.Close()
	if oauthDecisionResponse.StatusCode != http.StatusFound || parseErr != nil || location.Query().Get("state") != "cloud-e2e-state" || location.Query().Get("code") == "" {
		t.Fatalf("composed OAuth consent decision = %d: %q (%v)", oauthDecisionResponse.StatusCode, oauthDecisionResponse.Header.Get("Location"), parseErr)
	}
	capabilityRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/capabilities", nil)
	if err != nil {
		t.Fatal(err)
	}
	capabilityRequest.AddCookie(response.Cookies()[0])
	capabilityResponse, err := server.Client().Do(capabilityRequest)
	if err != nil {
		t.Fatal(err)
	}
	var capabilities map[string]bool
	if capabilityResponse.StatusCode != http.StatusOK || json.NewDecoder(capabilityResponse.Body).Decode(&capabilities) != nil || len(capabilities) != 3 || !capabilities["imageThumbnails"] || capabilities["officePreview"] != documentconvert.Available() || capabilities["fullTextSearch"] {
		body, _ := io.ReadAll(capabilityResponse.Body)
		capabilityResponse.Body.Close()
		t.Fatalf("composed capabilities = %d: %s (%#v)", capabilityResponse.StatusCode, body, capabilities)
	}
	capabilityResponse.Body.Close()
	response.Body.Close()
	createFolderRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/folders", strings.NewReader(`{"name":"Managed share E2E"}`))
	createFolderRequest.Header.Set("Content-Type", "application/json")
	createFolderRequest.AddCookie(response.Cookies()[0])
	createFolderResponse, err := server.Client().Do(createFolderRequest)
	if err != nil {
		t.Fatal(err)
	}
	var shareFolder struct {
		ID string `json:"id"`
	}
	if createFolderResponse.StatusCode != http.StatusCreated || json.NewDecoder(createFolderResponse.Body).Decode(&shareFolder) != nil || shareFolder.ID == "" {
		body, _ := io.ReadAll(createFolderResponse.Body)
		createFolderResponse.Body.Close()
		t.Fatalf("create share folder = %d: %s (%#v)", createFolderResponse.StatusCode, body, shareFolder)
	}
	createFolderResponse.Body.Close()
	createShareRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/shares", strings.NewReader(`{"nodeId":"`+shareFolder.ID+`","mode":"view"}`))
	createShareRequest.Header.Set("Content-Type", "application/json")
	createShareRequest.AddCookie(response.Cookies()[0])
	createShareResponse, err := server.Client().Do(createShareRequest)
	if err != nil {
		t.Fatal(err)
	}
	var managedShare struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if createShareResponse.StatusCode != http.StatusCreated || json.NewDecoder(createShareResponse.Body).Decode(&managedShare) != nil || managedShare.ID < 1 || !strings.HasPrefix(managedShare.URL, server.URL+"/s/") {
		body, _ := io.ReadAll(createShareResponse.Body)
		createShareResponse.Body.Close()
		t.Fatalf("create managed share = %d: %s (%#v)", createShareResponse.StatusCode, body, managedShare)
	}
	createShareResponse.Body.Close()
	shareListRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/shares?nodeId="+url.QueryEscape(shareFolder.ID), nil)
	shareListRequest.AddCookie(response.Cookies()[0])
	shareListResponse, err := server.Client().Do(shareListRequest)
	if err != nil {
		t.Fatal(err)
	}
	shareListBody, _ := io.ReadAll(shareListResponse.Body)
	shareListResponse.Body.Close()
	if shareListResponse.StatusCode != http.StatusOK || !bytes.Contains(shareListBody, []byte(`"url":"`+managedShare.URL+`"`)) {
		t.Fatalf("list managed shares = %d: %s", shareListResponse.StatusCode, shareListBody)
	}
	shareUpdateRequest, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/shares/"+strconv.FormatInt(managedShare.ID, 10), strings.NewReader(`{"note":"Updated from app E2E"}`))
	shareUpdateRequest.Header.Set("Content-Type", "application/json")
	shareUpdateRequest.AddCookie(response.Cookies()[0])
	shareUpdateResponse, err := server.Client().Do(shareUpdateRequest)
	if err != nil {
		t.Fatal(err)
	}
	shareUpdateBody, _ := io.ReadAll(shareUpdateResponse.Body)
	shareUpdateResponse.Body.Close()
	if shareUpdateResponse.StatusCode != http.StatusOK || !bytes.Contains(shareUpdateBody, []byte(`"note":"Updated from app E2E"`)) {
		t.Fatalf("update managed share = %d: %s", shareUpdateResponse.StatusCode, shareUpdateBody)
	}
	shareDeleteRequest, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/shares/"+strconv.FormatInt(managedShare.ID, 10), nil)
	shareDeleteRequest.AddCookie(response.Cookies()[0])
	shareDeleteResponse, err := server.Client().Do(shareDeleteRequest)
	if err != nil {
		t.Fatal(err)
	}
	shareDeleteResponse.Body.Close()
	if shareDeleteResponse.StatusCode != http.StatusOK {
		t.Fatalf("revoke managed share = %d", shareDeleteResponse.StatusCode)
	}
	var firstUserID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='first-cloud-user'`).Scan(&firstUserID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, firstUserID); err != nil {
		t.Fatal(err)
	}
	routerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/device/code" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"device_code":"router-device-code","user_code":"ABCD-EFGH","verification_uri":"https://router.example/device","interval":60,"expires_in":600}`)
	}))
	defer routerServer.Close()
	connectRouter, _ := http.NewRequest(http.MethodPost, server.URL+"/api/neorouter/connect", strings.NewReader(`{"scope":"server","address":"`+routerServer.URL+`/admin"}`))
	connectRouter.Header.Set("Content-Type", "application/json")
	connectRouter.AddCookie(response.Cookies()[0])
	connectResponse, err := server.Client().Do(connectRouter)
	if err != nil {
		t.Fatal(err)
	}
	var connectResult struct {
		ID       string `json:"id"`
		URL      string `json:"url"`
		UserCode string `json:"userCode"`
	}
	if connectResponse.StatusCode != http.StatusOK || json.NewDecoder(connectResponse.Body).Decode(&connectResult) != nil || connectResult.ID == "" || connectResult.URL != "https://router.example/device" || connectResult.UserCode != "ABCD-EFGH" {
		body, _ := io.ReadAll(connectResponse.Body)
		connectResponse.Body.Close()
		t.Fatalf("composed NeoRouter connect = %d: %s (%#v)", connectResponse.StatusCode, body, connectResult)
	}
	connectResponse.Body.Close()
	adminListRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/users?q=first-cloud", nil)
	if err != nil {
		t.Fatal(err)
	}
	adminListRequest.AddCookie(response.Cookies()[0])
	adminListResponse, err := server.Client().Do(adminListRequest)
	if err != nil {
		t.Fatal(err)
	}
	adminListBody, _ := io.ReadAll(adminListResponse.Body)
	adminListResponse.Body.Close()
	if adminListResponse.StatusCode != http.StatusOK || !bytes.Contains(adminListBody, []byte(`"first-cloud-user"`)) {
		t.Fatalf("admin user listing = %d: %s", adminListResponse.StatusCode, adminListBody)
	}
	quotaRequest, err := http.NewRequest(http.MethodPut, server.URL+"/api/admin/users/"+strconv.FormatInt(firstUserID, 10)+"/quota", strings.NewReader(`{"mode":"custom","bytes":"2 GB"}`))
	if err != nil {
		t.Fatal(err)
	}
	quotaRequest.Header.Set("Content-Type", "application/json")
	quotaRequest.AddCookie(response.Cookies()[0])
	quotaResponse, err := server.Client().Do(quotaRequest)
	if err != nil {
		t.Fatal(err)
	}
	quotaBody, _ := io.ReadAll(quotaResponse.Body)
	quotaResponse.Body.Close()
	if quotaResponse.StatusCode != http.StatusOK || !bytes.Contains(quotaBody, []byte(`"quotaBytes":2147483648`)) {
		t.Fatalf("admin quota update = %d: %s", quotaResponse.StatusCode, quotaBody)
	}
	auditRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/admin/audit", nil)
	auditRequest.AddCookie(response.Cookies()[0])
	auditResponse, err := server.Client().Do(auditRequest)
	if err != nil {
		t.Fatal(err)
	}
	auditBody, _ := io.ReadAll(auditResponse.Body)
	auditResponse.Body.Close()
	if auditResponse.StatusCode != http.StatusOK || !bytes.Contains(auditBody, []byte(`"action":"account.quota"`)) {
		t.Fatalf("admin audit = %d: %s", auditResponse.StatusCode, auditBody)
	}
	for _, endpoint := range []string{"/api/neorouter", "/api/account/security-keys"} {
		r, requestErr := http.NewRequest(http.MethodGet, server.URL+endpoint, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		r.AddCookie(response.Cookies()[0])
		routeResponse, requestErr := server.Client().Do(r)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, _ := io.ReadAll(routeResponse.Body)
		routeResponse.Body.Close()
		if routeResponse.StatusCode != http.StatusOK {
			t.Fatalf("composed %s = %d: %s", endpoint, routeResponse.StatusCode, body)
		}
	}
	for _, action := range []struct {
		path string
		body string
	}{{"admin", `{"isAdmin":false}`}, {"disabled", `{"disabled":true}`}} {
		actionRequest, _ := http.NewRequest(http.MethodPut, server.URL+"/api/admin/users/"+strconv.FormatInt(firstUserID, 10)+"/"+action.path, strings.NewReader(action.body))
		actionRequest.Header.Set("Content-Type", "application/json")
		actionRequest.AddCookie(response.Cookies()[0])
		actionResponse, requestErr := server.Client().Do(actionRequest)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		actionResponse.Body.Close()
		if actionResponse.StatusCode != http.StatusBadRequest {
			t.Fatalf("self %s action = %d", action.path, actionResponse.StatusCode)
		}
	}
	if _, err = app.Runtime.DB.W.Exec(`UPDATE users SET email_verified_at=datetime('now') WHERE id=?`, firstUserID); err != nil {
		t.Fatal(err)
	}
	pairingRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/account/devices/pairing", strings.NewReader(`{"name":"Paired E2E Phone","platform":"ios"}`))
	pairingRequest.Header.Set("Content-Type", "application/json")
	pairingRequest.AddCookie(response.Cookies()[0])
	pairingResponse, err := server.Client().Do(pairingRequest)
	if err != nil {
		t.Fatal(err)
	}
	var pairing struct {
		Code      string `json:"code"`
		Server    string `json:"server"`
		QRPayload string `json:"qrPayload"`
	}
	if pairingResponse.StatusCode != http.StatusOK || json.NewDecoder(pairingResponse.Body).Decode(&pairing) != nil || pairing.Code == "" || pairing.Server != server.URL || !strings.Contains(pairing.QRPayload, pairing.Code) {
		body, _ := io.ReadAll(pairingResponse.Body)
		pairingResponse.Body.Close()
		t.Fatalf("start device pairing = %d: %s (%#v)", pairingResponse.StatusCode, body, pairing)
	}
	pairingResponse.Body.Close()
	pairRedeemRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/devices/pair", strings.NewReader(`{"code":"`+pairing.Code+`"}`))
	pairRedeemRequest.Header.Set("Content-Type", "application/json")
	pairRedeemResponse, err := server.Client().Do(pairRedeemRequest)
	if err != nil {
		t.Fatal(err)
	}
	var paired struct {
		Token  string `json:"token"`
		Device struct {
			Name string `json:"name"`
		} `json:"device"`
	}
	if pairRedeemResponse.StatusCode != http.StatusOK || json.NewDecoder(pairRedeemResponse.Body).Decode(&paired) != nil || !strings.HasPrefix(paired.Token, "nc_") || paired.Device.Name != "Paired E2E Phone" {
		body, _ := io.ReadAll(pairRedeemResponse.Body)
		pairRedeemResponse.Body.Close()
		t.Fatalf("redeem device pairing = %d: %s (%#v)", pairRedeemResponse.StatusCode, body, paired)
	}
	pairRedeemResponse.Body.Close()
	pairStatusRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/account/devices/pairing/"+pairing.Code, nil)
	pairStatusRequest.AddCookie(response.Cookies()[0])
	pairStatusResponse, err := server.Client().Do(pairStatusRequest)
	if err != nil {
		t.Fatal(err)
	}
	var pairingStatus struct {
		Status string `json:"status"`
	}
	if pairStatusResponse.StatusCode != http.StatusOK || json.NewDecoder(pairStatusResponse.Body).Decode(&pairingStatus) != nil || pairingStatus.Status != "used" {
		body, _ := io.ReadAll(pairStatusResponse.Body)
		pairStatusResponse.Body.Close()
		t.Fatalf("device pairing status = %d: %s (%#v)", pairStatusResponse.StatusCode, body, pairingStatus)
	}
	pairStatusResponse.Body.Close()
	deviceStart, _ := http.NewRequest(http.MethodPost, server.URL+"/api/devices/authorize", strings.NewReader(`{"name":"App E2E Device","platform":"linux"}`))
	deviceStart.Header.Set("Content-Type", "application/json")
	deviceStartResponse, err := server.Client().Do(deviceStart)
	if err != nil {
		t.Fatal(err)
	}
	var deviceAuthorization struct {
		DeviceCode string `json:"deviceCode"`
		UserCode   string `json:"userCode"`
	}
	if deviceStartResponse.StatusCode != http.StatusOK || json.NewDecoder(deviceStartResponse.Body).Decode(&deviceAuthorization) != nil || deviceAuthorization.DeviceCode == "" || deviceAuthorization.UserCode == "" {
		body, _ := io.ReadAll(deviceStartResponse.Body)
		deviceStartResponse.Body.Close()
		t.Fatalf("start device authorization = %d: %s", deviceStartResponse.StatusCode, body)
	}
	deviceStartResponse.Body.Close()
	deviceDecision, _ := http.NewRequest(http.MethodPost, server.URL+"/api/account/devices/authorize/"+deviceAuthorization.UserCode, strings.NewReader(`{"approve":true}`))
	deviceDecision.Header.Set("Content-Type", "application/json")
	deviceDecision.AddCookie(response.Cookies()[0])
	deviceDecisionResponse, err := server.Client().Do(deviceDecision)
	if err != nil {
		t.Fatal(err)
	}
	deviceDecisionResponse.Body.Close()
	if deviceDecisionResponse.StatusCode != http.StatusOK {
		t.Fatalf("approve device authorization = %d", deviceDecisionResponse.StatusCode)
	}
	deviceExchange, _ := http.NewRequest(http.MethodPost, server.URL+"/api/devices/token", strings.NewReader(`{"deviceCode":"`+deviceAuthorization.DeviceCode+`"}`))
	deviceExchange.Header.Set("Content-Type", "application/json")
	deviceExchangeResponse, err := server.Client().Do(deviceExchange)
	if err != nil {
		t.Fatal(err)
	}
	var deviceToken struct {
		Token string `json:"token"`
	}
	if deviceExchangeResponse.StatusCode != http.StatusOK || json.NewDecoder(deviceExchangeResponse.Body).Decode(&deviceToken) != nil || !strings.HasPrefix(deviceToken.Token, "nc_") {
		body, _ := io.ReadAll(deviceExchangeResponse.Body)
		deviceExchangeResponse.Body.Close()
		t.Fatalf("exchange device authorization = %d: %s", deviceExchangeResponse.StatusCode, body)
	}
	deviceExchangeResponse.Body.Close()
	connectedRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/account/connected-apps", nil)
	if err != nil {
		t.Fatal(err)
	}
	connectedRequest.AddCookie(response.Cookies()[0])
	connectedResponse, err := server.Client().Do(connectedRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer connectedResponse.Body.Close()
	if connectedResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(connectedResponse.Body)
		t.Fatalf("connected apps = %d: %s", connectedResponse.StatusCode, body)
	}
	connectedResponse.Body.Close()
	for _, path := range []string{"/api/photos", "/api/photos/summary"} {
		photoRequest, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		photoRequest.AddCookie(response.Cookies()[0])
		photoResponse, requestErr := server.Client().Do(photoRequest)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		photoResponse.Body.Close()
		if photoResponse.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d", path, photoResponse.StatusCode)
		}
	}
	vaultRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/vaults/missing-vault", nil)
	vaultRequest.AddCookie(response.Cookies()[0])
	vaultResponse, err := server.Client().Do(vaultRequest)
	if err != nil {
		t.Fatal(err)
	}
	vaultResponse.Body.Close()
	if vaultResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("vault metadata = %d", vaultResponse.StatusCode)
	}
	vaultCreateBody := `{"name":"Vault E2E","salt":"AAAAAAAAAAAAAAAAAAAAAA==","iterations":600000,"wrappedKey":"` + strings.Repeat("A", 80) + `"}`
	vaultCreateRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/vaults", strings.NewReader(vaultCreateBody))
	vaultCreateRequest.Header.Set("Content-Type", "application/json")
	vaultCreateRequest.AddCookie(response.Cookies()[0])
	vaultCreateResponse, err := server.Client().Do(vaultCreateRequest)
	if err != nil {
		t.Fatal(err)
	}
	var createdVault struct {
		ID       string `json:"id"`
		FolderID string `json:"folderId"`
	}
	if vaultCreateResponse.StatusCode != http.StatusCreated || json.NewDecoder(vaultCreateResponse.Body).Decode(&createdVault) != nil || createdVault.ID == "" || createdVault.FolderID == "" {
		body, _ := io.ReadAll(vaultCreateResponse.Body)
		vaultCreateResponse.Body.Close()
		t.Fatalf("create vault = %d: %s", vaultCreateResponse.StatusCode, body)
	}
	vaultCreateResponse.Body.Close()
	vaultMetadataRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/vaults/"+createdVault.ID, nil)
	vaultMetadataRequest.AddCookie(response.Cookies()[0])
	vaultMetadataResponse, err := server.Client().Do(vaultMetadataRequest)
	if err != nil {
		t.Fatal(err)
	}
	vaultMetadataResponse.Body.Close()
	if vaultMetadataResponse.StatusCode != http.StatusOK {
		t.Fatalf("vault metadata = %d", vaultMetadataResponse.StatusCode)
	}
	plainFolderRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/folders", strings.NewReader(`{"name":"Plain source"}`))
	plainFolderRequest.Header.Set("Content-Type", "application/json")
	plainFolderRequest.AddCookie(response.Cookies()[0])
	plainFolderResponse, err := server.Client().Do(plainFolderRequest)
	if err != nil {
		t.Fatal(err)
	}
	var plainFolder struct {
		ID string `json:"id"`
	}
	if plainFolderResponse.StatusCode != http.StatusCreated || json.NewDecoder(plainFolderResponse.Body).Decode(&plainFolder) != nil || plainFolder.ID == "" {
		body, _ := io.ReadAll(plainFolderResponse.Body)
		plainFolderResponse.Body.Close()
		t.Fatalf("create conversion source = %d: %s", plainFolderResponse.StatusCode, body)
	}
	plainFolderResponse.Body.Close()
	conversionCheckRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/vaults/conversions/"+plainFolder.ID, nil)
	conversionCheckRequest.AddCookie(response.Cookies()[0])
	conversionCheckResponse, err := server.Client().Do(conversionCheckRequest)
	if err != nil {
		t.Fatal(err)
	}
	var conversionCheck struct {
		Cursor int64 `json:"cursor"`
	}
	if conversionCheckResponse.StatusCode != http.StatusOK || json.NewDecoder(conversionCheckResponse.Body).Decode(&conversionCheck) != nil {
		body, _ := io.ReadAll(conversionCheckResponse.Body)
		conversionCheckResponse.Body.Close()
		t.Fatalf("check vault conversion = %d: %s", conversionCheckResponse.StatusCode, body)
	}
	conversionCheckResponse.Body.Close()
	replaceBody := `{"folderId":"` + plainFolder.ID + `","cursor":` + strconv.FormatInt(conversionCheck.Cursor, 10) + `}`
	replaceRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/vaults/"+createdVault.ID+"/replace", strings.NewReader(replaceBody))
	replaceRequest.Header.Set("Content-Type", "application/json")
	replaceRequest.AddCookie(response.Cookies()[0])
	replaceResponse, err := server.Client().Do(replaceRequest)
	if err != nil {
		t.Fatal(err)
	}
	var replaced struct {
		TrashedID string `json:"trashedId"`
	}
	if replaceResponse.StatusCode != http.StatusOK || json.NewDecoder(replaceResponse.Body).Decode(&replaced) != nil || replaced.TrashedID != plainFolder.ID {
		body, _ := io.ReadAll(replaceResponse.Body)
		replaceResponse.Body.Close()
		t.Fatalf("replace folder with vault = %d: %s", replaceResponse.StatusCode, body)
	}
	replaceResponse.Body.Close()
	settingsRequest, err := http.NewRequest(http.MethodPut, server.URL+"/api/settings", strings.NewReader(`{"theme":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	settingsRequest.Header.Set("Content-Type", "application/json")
	settingsRequest.AddCookie(response.Cookies()[0])
	settingsResponse, err := server.Client().Do(settingsRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer settingsResponse.Body.Close()
	if settingsResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(settingsResponse.Body)
		t.Fatalf("settings update = %d: %s", settingsResponse.StatusCode, body)
	}
	byokRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/settings/byok", nil)
	if err != nil {
		t.Fatal(err)
	}
	byokRequest.AddCookie(response.Cookies()[0])
	byokResponse, err := server.Client().Do(byokRequest)
	if err != nil {
		t.Fatal(err)
	}
	if byokResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(byokResponse.Body)
		byokResponse.Body.Close()
		t.Fatalf("BYOK settings = %d: %s", byokResponse.StatusCode, body)
	}
	byokResponse.Body.Close()
	folderRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/folders", strings.NewReader(`{"name":"Comment target"}`))
	if err != nil {
		t.Fatal(err)
	}
	folderRequest.Header.Set("Content-Type", "application/json")
	folderRequest.AddCookie(response.Cookies()[0])
	folderResponse, err := server.Client().Do(folderRequest)
	if err != nil {
		t.Fatal(err)
	}
	var commentTarget struct {
		ID string `json:"id"`
	}
	if folderResponse.StatusCode != http.StatusCreated || json.NewDecoder(folderResponse.Body).Decode(&commentTarget) != nil || commentTarget.ID == "" {
		body, _ := io.ReadAll(folderResponse.Body)
		folderResponse.Body.Close()
		t.Fatalf("comment target = %d: %s", folderResponse.StatusCode, body)
	}
	folderResponse.Body.Close()
	live := dialLive(t, server, http.Header{"Cookie": {response.Cookies()[0].Name + "=" + response.Cookies()[0].Value}})
	live.await("40")
	commentRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/files/"+commentTarget.ID+"/comments", strings.NewReader(`{"body":"composed comment"}`))
	if err != nil {
		t.Fatal(err)
	}
	commentRequest.Header.Set("Content-Type", "application/json")
	commentRequest.AddCookie(response.Cookies()[0])
	commentResponse, err := server.Client().Do(commentRequest)
	if err != nil {
		t.Fatal(err)
	}
	commentBody, _ := io.ReadAll(commentResponse.Body)
	commentResponse.Body.Close()
	if commentResponse.StatusCode != http.StatusCreated || !strings.Contains(string(commentBody), `"body":"composed comment"`) {
		t.Fatalf("composed comment = %d: %s", commentResponse.StatusCode, commentBody)
	}
	if frame := live.await(`42["comments:changed"`); !strings.Contains(frame, commentTarget.ID) {
		t.Fatalf("comment event names the wrong file: %s", frame)
	}
	activityRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/activity", nil)
	if err != nil {
		t.Fatal(err)
	}
	activityRequest.AddCookie(response.Cookies()[0])
	activityResponse, err := server.Client().Do(activityRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer activityResponse.Body.Close()
	if activityResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(activityResponse.Body)
		t.Fatalf("activity feed = %d: %s", activityResponse.StatusCode, body)
	}
	syncRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/sync/tree", nil)
	if err != nil {
		t.Fatal(err)
	}
	syncRequest.AddCookie(response.Cookies()[0])
	syncResponse, err := server.Client().Do(syncRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer syncResponse.Body.Close()
	if syncResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(syncResponse.Body)
		t.Fatalf("sync tree = %d: %s", syncResponse.StatusCode, body)
	}
	sharedRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/shared-with-me", nil)
	if err != nil {
		t.Fatal(err)
	}
	sharedRequest.AddCookie(response.Cookies()[0])
	sharedResponse, err := server.Client().Do(sharedRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer sharedResponse.Body.Close()
	if sharedResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(sharedResponse.Body)
		t.Fatalf("shared with me = %d: %s", sharedResponse.StatusCode, body)
	}
}

func TestComposedPublicShareExportAndAccountDeletionLifecycle(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	password := "G7!mQv#2xL9rT4pZ"
	request := func(method, path, body string, cookie *http.Cookie) (*http.Response, []byte) {
		t.Helper()
		var bodyReader io.Reader
		if body != "" {
			bodyReader = strings.NewReader(body)
		}
		req, reqErr := http.NewRequest(method, server.URL+path, bodyReader)
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, reqErr := client.Do(req)
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		return resp, data
	}
	if response, body := request(http.MethodPost, "/api/auth/register", `{"username":"share-lifecycle-admin","email":"share-lifecycle-admin@example.test","password":"`+password+`"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("register admin=%d: %s", response.StatusCode, body)
	}
	registered, body := request(http.MethodPost, "/api/auth/register", `{"username":"share-lifecycle-owner","email":"share-lifecycle-owner@example.test","password":"`+password+`"}`, nil)
	if registered.StatusCode != http.StatusOK || len(registered.Cookies()) == 0 {
		t.Fatalf("register share owner=%d: %s", registered.StatusCode, body)
	}
	cookie := registered.Cookies()[0]
	folderResponse, body := request(http.MethodPost, "/api/folders", `{"name":"Share lifecycle"}`, cookie)
	var folder struct {
		ID string `json:"id"`
	}
	if folderResponse.StatusCode != http.StatusCreated || json.Unmarshal(body, &folder) != nil || folder.ID == "" {
		t.Fatalf("create shared folder=%d: %s", folderResponse.StatusCode, body)
	}
	created, body := request(http.MethodPost, "/api/shares", `{"nodeId":"`+folder.ID+`","mode":"view_upload","password":"share-password-42","note":"lifecycle export"}`, cookie)
	var link struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if created.StatusCode != http.StatusCreated || json.Unmarshal(body, &link) != nil || link.ID <= 0 || link.Token == "" {
		t.Fatalf("create share=%d: %s", created.StatusCode, body)
	}
	var ownerID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='share-lifecycle-owner'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	var passwordHash string
	if err = app.Runtime.DB.W.QueryRow(`SELECT password_hash FROM shares WHERE id=?`, link.ID).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	exported, body := request(http.MethodPost, "/api/account/export", `{"currentPassword":"`+password+`"}`, cookie)
	if exported.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"links":[{`)) || !bytes.Contains(body, []byte(`"nodeName":"Share lifecycle"`)) || !bytes.Contains(body, []byte(`"hasPassword":true`)) {
		t.Fatalf("share metadata missing from account export=%d: %s", exported.StatusCode, body)
	}
	if bytes.Contains(body, []byte(link.Token)) || bytes.Contains(body, []byte(passwordHash)) || bytes.Contains(body, []byte("share-password-42")) {
		t.Fatalf("account export leaked share token/password material: %s", body)
	}
	deleted, body := request(http.MethodPost, "/api/account/delete", `{"currentPassword":"`+password+`","confirmUsername":"share-lifecycle-owner"}`, cookie)
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("delete share owner=%d: %s", deleted.StatusCode, body)
	}
	var remaining int
	if err = app.Runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM shares WHERE user_id=?`, ownerID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("owner's public shares remain=%d err=%v", remaining, err)
	}
	public, body := request(http.MethodGet, "/api/public/shares/"+url.PathEscape(link.Token), "", nil)
	if public.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted owner's public link still resolves=%d: %s", public.StatusCode, body)
	}
}

func TestComposedSigningCompletionCreatesEncryptedFinalFile(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	app.StartWorkers(context.Background())
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"signing-finalizer","email":"signing-finalizer@example.test","password":"Q8!zLp#3vT7mK5rX"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, err := client.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	registeredBody, _ := io.ReadAll(registered.Body)
	registered.Body.Close()
	if registered.StatusCode != http.StatusOK || len(registered.Cookies()) == 0 {
		t.Fatalf("register=%d %s", registered.StatusCode, registeredBody)
	}
	cookie := registered.Cookies()[0]
	var sourcePDF bytes.Buffer
	if err = api.Create(context.Background(), nil, strings.NewReader(`{"pages":{"1":{"content":{"textfield":[]}}}}`), &sourcePDF, model.NewStatelessConfiguration()); err != nil {
		t.Fatal(err)
	}
	upload, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId=root&name=contract.pdf", bytes.NewReader(sourcePDF.Bytes()))
	upload.AddCookie(cookie)
	uploadResponse, err := client.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	uploadBody, _ := io.ReadAll(uploadResponse.Body)
	uploadResponse.Body.Close()
	var uploaded struct {
		ID string `json:"id"`
	}
	if uploadResponse.StatusCode != http.StatusCreated || json.Unmarshal(uploadBody, &uploaded) != nil || uploaded.ID == "" {
		t.Fatalf("upload source=%d %s", uploadResponse.StatusCode, uploadBody)
	}
	create, _ := http.NewRequest(http.MethodPost, server.URL+"/api/signing/documents", strings.NewReader(`{"nodeId":"`+uploaded.ID+`"}`))
	create.Header.Set("Content-Type", "application/json")
	create.AddCookie(cookie)
	created, err := client.Do(create)
	if err != nil {
		t.Fatal(err)
	}
	createdBody, _ := io.ReadAll(created.Body)
	created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create signing document=%d %s", created.StatusCode, createdBody)
	}
	var documentID, partyID string
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM sign_documents WHERE owner_id=(SELECT id FROM users WHERE username='signing-finalizer') ORDER BY created_at DESC LIMIT 1`).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM sign_parties WHERE document_id=?`, documentID).Scan(&partyID); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 43)
	tokenSum := sha256.Sum256([]byte(token))
	if _, err = app.Runtime.DB.W.Exec(`UPDATE sign_documents SET status='sent' WHERE id=?`, documentID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`UPDATE sign_parties SET status='pending',token_hash=? WHERE id=?`, hex.EncodeToString(tokenSum[:]), partyID); err != nil {
		t.Fatal(err)
	}
	var tokenHash, documentStatus, ownerDisabled sql.NullString
	if err = app.Runtime.DB.W.QueryRow(`SELECT p.token_hash,d.status,u.disabled_at FROM sign_parties p JOIN sign_documents d ON d.id=p.document_id JOIN users u ON u.id=d.owner_id WHERE p.id=?`, partyID).Scan(&tokenHash, &documentStatus, &ownerDisabled); err != nil || !tokenHash.Valid || tokenHash.String != hex.EncodeToString(tokenSum[:]) || documentStatus.String != "sent" || ownerDisabled.Valid {
		t.Fatalf("prepared public signer: token=%v status=%v disabled=%v err=%v", tokenHash, documentStatus, ownerDisabled, err)
	}
	publicPost := func(suffix, body string) (*http.Response, []byte) {
		req, requestErr := http.NewRequest(http.MethodPost, server.URL+"/api/public/sign/"+token+suffix, strings.NewReader(body))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		req.Header.Set("Content-Type", "application/json")
		res, requestErr := client.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		data, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		return res, data
	}
	consentResponse, consentBody := publicPost("/consent", `{"accepted":true,"signerName":"Ada Lovelace","timezone":"UTC"}`)
	if consentResponse.StatusCode != http.StatusOK {
		t.Fatalf("public consent=%d %s", consentResponse.StatusCode, consentBody)
	}
	completeResponse, completeBody := publicPost("/complete", `{}`)
	if completeResponse.StatusCode != http.StatusOK {
		t.Fatalf("public completion=%d %s", completeResponse.StatusCode, completeBody)
	}
	deadline := time.Now().Add(8 * time.Second)
	var status, finalNodeID string
	for time.Now().Before(deadline) {
		err = app.Runtime.DB.W.QueryRow(`SELECT status,COALESCE(final_node_id,'') FROM sign_documents WHERE id=?`, documentID).Scan(&status, &finalNodeID)
		if err == nil && status == "completed" && finalNodeID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status != "completed" || finalNodeID == "" {
		var failure string
		_ = app.Runtime.DB.W.QueryRow(`SELECT COALESCE(finalize_error,'') FROM sign_documents WHERE id=?`, documentID).Scan(&failure)
		t.Fatalf("finalizer status=%q node=%q error=%q", status, finalNodeID, failure)
	}
	var finalBlobID string
	var finalSize, usedBytes int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT blob_id,size FROM nodes WHERE id=? AND user_id=(SELECT id FROM users WHERE username='signing-finalizer') AND type='file'`, finalNodeID).Scan(&finalBlobID, &finalSize); err != nil {
		t.Fatal(err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT used_bytes FROM users WHERE username='signing-finalizer'`).Scan(&usedBytes); err != nil || usedBytes < finalSize {
		t.Fatalf("quota usage=%d final size=%d err=%v", usedBytes, finalSize, err)
	}
	var journalCount int
	if err = app.Runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE node_id=? AND op='create'`, finalNodeID).Scan(&journalCount); err != nil || journalCount != 1 {
		t.Fatalf("generated file journal inserts=%d err=%v", journalCount, err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT size FROM blobs WHERE id=? AND user_id=(SELECT id FROM users WHERE username='signing-finalizer')`, finalBlobID).Scan(&finalSize); err != nil {
		t.Fatal(err)
	}
	content, _ := http.NewRequest(http.MethodGet, server.URL+"/api/files/"+finalNodeID+"/content", nil)
	content.AddCookie(cookie)
	contentResponse, err := client.Do(content)
	if err != nil {
		t.Fatal(err)
	}
	finalPDF, _ := io.ReadAll(contentResponse.Body)
	contentResponse.Body.Close()
	if contentResponse.StatusCode != http.StatusOK || len(finalPDF) != int(finalSize) || !bytes.HasPrefix(finalPDF, []byte("%PDF-")) {
		t.Fatalf("download generated file=%d bytes=%d expected=%d", contentResponse.StatusCode, len(finalPDF), finalSize)
	}
}

func TestAdminSignupPolicyChangesApplyImmediatelyToAuthHandler(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	register := func(username string, cookie *http.Cookie) (int, string, []*http.Cookie) {
		body := `{"username":"` + username + `","email":"` + username + `@example.test","password":"Q8!zLp#3vT7mK5rX"}`
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, requestErr := client.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		data, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		return res.StatusCode, string(data), res.Cookies()
	}
	status, body, cookies := register("first-live-signup", nil)
	if status != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("first registration=%d %s", status, body)
	}
	cookie := cookies[0]
	storageConfig, _ := http.NewRequest(http.MethodPut, server.URL+"/api/admin/config/storage", strings.NewReader(`{"defaultQuotaBytes":"100MB","maxUploadBytes":"1B","trashRetentionDays":0,"versionRetention":1}`))
	storageConfig.Header.Set("Content-Type", "application/json")
	storageConfig.AddCookie(cookie)
	storageResponse, requestErr := client.Do(storageConfig)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	storageBody, _ := io.ReadAll(storageResponse.Body)
	storageResponse.Body.Close()
	if storageResponse.StatusCode != http.StatusOK {
		t.Fatalf("update storage policy=%d %s", storageResponse.StatusCode, storageBody)
	}
	accountRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/account", nil)
	accountRequest.AddCookie(cookie)
	accountResponse, requestErr := client.Do(accountRequest)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	accountBody, _ := io.ReadAll(accountResponse.Body)
	accountResponse.Body.Close()
	if accountResponse.StatusCode != http.StatusOK || !strings.Contains(string(accountBody), `"quotaBytes":104857600`) {
		t.Fatalf("account quota after live storage policy=%d %s", accountResponse.StatusCode, accountBody)
	}
	importsRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/imports", nil)
	importsRequest.AddCookie(cookie)
	importsResponse, requestErr := client.Do(importsRequest)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	importsBody, _ := io.ReadAll(importsResponse.Body)
	importsResponse.Body.Close()
	if importsResponse.StatusCode != http.StatusOK || !strings.Contains(string(importsBody), `"items":[]`) {
		t.Fatalf("composed import history=%d %s", importsResponse.StatusCode, importsBody)
	}
	signingRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/signing/documents", nil)
	signingRequest.AddCookie(cookie)
	signingResponse, requestErr := client.Do(signingRequest)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	signingBody, _ := io.ReadAll(signingResponse.Body)
	signingResponse.Body.Close()
	if signingResponse.StatusCode != http.StatusOK || !strings.Contains(string(signingBody), `"items":[]`) {
		t.Fatalf("composed signing list=%d %s", signingResponse.StatusCode, signingBody)
	}
	officeRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/office/config", nil)
	officeRequest.AddCookie(cookie)
	officeResponse, requestErr := client.Do(officeRequest)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	officeBody, _ := io.ReadAll(officeResponse.Body)
	officeResponse.Body.Close()
	if officeResponse.StatusCode != http.StatusOK || !strings.Contains(string(officeBody), `"enabled":false`) {
		t.Fatalf("composed office config=%d %s", officeResponse.StatusCode, officeBody)
	}
	upload, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId=root&name=too-large.txt", strings.NewReader("12"))
	upload.AddCookie(cookie)
	uploadResponse, requestErr := client.Do(upload)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	uploadBody, _ := io.ReadAll(uploadResponse.Body)
	uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(uploadBody), "FILE_TOO_LARGE") {
		t.Fatalf("upload after live storage policy=%d %s", uploadResponse.StatusCode, uploadBody)
	}
	setSignup := func(enabled bool) {
		raw := `{"allowSignup":false}`
		if enabled {
			raw = `{"allowSignup":true}`
		}
		req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/admin/access/signup", strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("set signup=%t status=%d body=%s", enabled, res.StatusCode, data)
		}
	}
	setSignup(false)
	status, body, _ = register("closed-live-signup", nil)
	if status != http.StatusForbidden || !strings.Contains(body, "SIGNUP_DISABLED") {
		t.Fatalf("registration after close=%d %s", status, body)
	}
	setSignup(true)
	status, body, _ = register("reopened-live-signup", nil)
	if status != http.StatusOK {
		t.Fatalf("registration after reopen=%d %s", status, body)
	}
	statusResponse, e := client.Get(server.URL + "/api/auth/status")
	if e != nil {
		t.Fatal(e)
	}
	defer statusResponse.Body.Close()
	var authStatus map[string]any
	if e = json.NewDecoder(statusResponse.Body).Decode(&authStatus); e != nil {
		t.Fatal(e)
	}
	if authStatus["registrationOpen"] != true {
		t.Fatalf("auth status did not reflect live config: %v", authStatus)
	}
}

func TestComposedEncryptedContentSearchIndexesExistingBlobFormat(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"search-owner","email":"search@example.test","password":"Q8!zLp#3vT7mK5rX"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, err := client.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	registerBody, _ := io.ReadAll(registered.Body)
	registered.Body.Close()
	if registered.StatusCode != http.StatusOK || len(registered.Cookies()) == 0 {
		t.Fatalf("register=%d %s", registered.StatusCode, registerBody)
	}
	cookie := registered.Cookies()[0]
	settings, _ := http.NewRequest(http.MethodPut, server.URL+"/api/admin/config", strings.NewReader(`{"fullTextSearch":true}`))
	settings.Header.Set("Content-Type", "application/json")
	settings.AddCookie(cookie)
	settingsResponse, err := client.Do(settings)
	if err != nil {
		t.Fatal(err)
	}
	settingsBody, _ := io.ReadAll(settingsResponse.Body)
	settingsResponse.Body.Close()
	if settingsResponse.StatusCode != http.StatusOK {
		t.Fatalf("enable full-text search=%d %s", settingsResponse.StatusCode, settingsBody)
	}
	content := "The uncommon word telescope-quartz appears only in this encrypted file."
	upload, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId=root&name=private-notes.txt", strings.NewReader(content))
	upload.AddCookie(cookie)
	uploadResponse, err := client.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	uploadBody, _ := io.ReadAll(uploadResponse.Body)
	uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusCreated {
		t.Fatalf("upload searchable file=%d %s", uploadResponse.StatusCode, uploadBody)
	}
	if _, err = app.searchIndexer.RunPass(context.Background(), 10); err != nil {
		t.Fatalf("index encrypted source: %v", err)
	}
	search, _ := http.NewRequest(http.MethodGet, server.URL+"/api/search?q=telescope-quartz&content=1", nil)
	search.AddCookie(cookie)
	searchResponse, err := client.Do(search)
	if err != nil {
		t.Fatal(err)
	}
	searchBody, _ := io.ReadAll(searchResponse.Body)
	searchResponse.Body.Close()
	if searchResponse.StatusCode != http.StatusOK || !strings.Contains(string(searchBody), `"match":"content"`) || !strings.Contains(string(searchBody), "telescope-quartz") {
		t.Fatalf("encrypted content search=%d %s", searchResponse.StatusCode, searchBody)
	}
}

func TestComposedWebDAVAppPasswordReadWriteAndMethods(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"dav-user","email":"dav@example.test","password":"G7!mQv#2xL9rT4pZ"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, err := server.Client().Do(register)
	if err != nil {
		t.Fatal(err)
	}
	registered.Body.Close()
	if registered.StatusCode != http.StatusOK {
		t.Fatalf("register = %d", registered.StatusCode)
	}
	var uid int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='dav-user'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	password := "nc_dav_application_test_token"
	hash := sha256.Sum256([]byte(password))
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(?,?,?,?)`, uid, "WebDAV test", "webdav", hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("dav-user:"+password))
	request := func(method, path string, body io.Reader, headers map[string]string) *http.Response {
		t.Helper()
		req, e := http.NewRequest(method, server.URL+path, body)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", authHeader)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	response := request("MKCOL", "/dav/files/dav-user/Docs", nil, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("MKCOL = %d", response.StatusCode)
	}
	response = request("PUT", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader("hello webdav"), map[string]string{"X-OC-Mtime": "1795600000"})
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("PUT = %d", response.StatusCode)
	}
	var davNodeID string
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM nodes WHERE user_id=? AND name='hello.txt' AND trashed_at IS NULL`, uid).Scan(&davNodeID); err != nil {
		t.Fatal(err)
	}
	var updateCount int
	if err = app.Runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE user_id=? AND node_id=? AND op='update'`, uid, davNodeID).Scan(&updateCount); err != nil || updateCount != 1 {
		t.Fatalf("X-OC-Mtime change journal updates=%d err=%v", updateCount, err)
	}
	response = request("GET", "/dav/files/dav-user/Docs/hello.txt", nil, nil)
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(data) != "hello webdav" {
		t.Fatalf("GET = %d %q", response.StatusCode, data)
	}
	response = request("GET", "/dav/files/dav-user/Docs/hello.txt", nil, map[string]string{"Range": "bytes=0-4"})
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusPartialContent || string(data) != "hello" || response.Header.Get("Content-Range") != "bytes 0-4/12" {
		t.Fatalf("ranged GET = %d %q (%q)", response.StatusCode, data, response.Header.Get("Content-Range"))
	}
	response = request("PROPFIND", "/dav/files/dav-user/Docs", nil, map[string]string{"Depth": "1"})
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 207 || !bytes.Contains(data, []byte("hello.txt")) || !bytes.Contains(data, []byte("multistatus")) {
		t.Fatalf("PROPFIND = %d %s", response.StatusCode, data)
	}
	patch := `<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:getlastmodified>2026-11-25T10:20:30Z</d:getlastmodified></d:prop></d:set></d:propertyupdate>`
	response = request("PROPPATCH", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader(patch), nil)
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 207 || !bytes.Contains(data, []byte("getlastmodified")) || !bytes.Contains(data, []byte("200 OK")) {
		t.Fatalf("PROPPATCH = %d %s", response.StatusCode, data)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM changes WHERE user_id=? AND node_id=? AND op='update'`, uid, davNodeID).Scan(&updateCount); err != nil || updateCount != 2 {
		t.Fatalf("PROPPATCH change journal updates=%d err=%v", updateCount, err)
	}
	response = request("PUT", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader("hello webdav"), map[string]string{"If-Match": "*"})
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("If-Match wildcard PUT = %d", response.StatusCode)
	}
	var currentETag string
	if err = app.Runtime.DB.W.QueryRow(`SELECT etag FROM nodes WHERE id=? AND user_id=?`, davNodeID, uid).Scan(&currentETag); err != nil {
		t.Fatal(err)
	}
	response = request("PUT", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader("hello webdav"), map[string]string{"If-Match": `W/"` + currentETag + `"`})
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("weak If-Match PUT = %d", response.StatusCode)
	}
	response = request("PUT", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader("stale"), map[string]string{"If-Match": `"stale-etag"`})
	response.Body.Close()
	if response.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("stale If-Match PUT = %d", response.StatusCode)
	}
	response = request("COPY", "/dav/files/dav-user/Docs/hello.txt", nil, map[string]string{"Destination": server.URL + "/dav/files/dav-user/Docs/copy.txt", "Overwrite": "F"})
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("COPY = %d", response.StatusCode)
	}
	response = request("LOCK", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader(`<d:lockinfo xmlns:d="DAV:"><d:lockscope><d:exclusive/></d:lockscope><d:locktype><d:write/></d:locktype><d:owner><d:href>Neo &amp; Co</d:href></d:owner></d:lockinfo>`), map[string]string{"Depth": "0"})
	lockToken := response.Header.Get("Lock-Token")
	response.Body.Close()
	if response.StatusCode != http.StatusOK || lockToken == "" {
		t.Fatalf("LOCK = %d token=%q", response.StatusCode, lockToken)
	}
	response = request("PROPFIND", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader(`<d:propfind xmlns:d="DAV:"><d:prop><d:lockdiscovery/></d:prop></d:propfind>`), map[string]string{"Depth": "0"})
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 207 || !bytes.Contains(data, []byte(`<d:lockdiscovery><d:activelock>`)) || !bytes.Contains(data, []byte(strings.Trim(lockToken, "<>"))) || !bytes.Contains(data, []byte(`<d:owner><d:href>Neo &amp; Co</d:href></d:owner>`)) {
		t.Fatalf("PROPFIND lockdiscovery = %d %s", response.StatusCode, data)
	}
	response = request("PUT", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader("blocked"), nil)
	response.Body.Close()
	if response.StatusCode != http.StatusLocked {
		t.Fatalf("locked PUT = %d", response.StatusCode)
	}
	response = request("PUT", "/dav/files/dav-user/Docs/hello.txt", strings.NewReader("hello webdav"), map[string]string{"If": "(<" + strings.Trim(lockToken, "<>") + ">)"})
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("token authorized PUT = %d", response.StatusCode)
	}
	response = request("UNLOCK", "/dav/files/dav-user/Docs/hello.txt", nil, map[string]string{"Lock-Token": lockToken})
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("UNLOCK = %d", response.StatusCode)
	}
	response = request("MOVE", "/dav/files/dav-user/Docs/copy.txt", nil, map[string]string{"Destination": server.URL + "/dav/files/dav-user/Docs/moved.txt"})
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("MOVE = %d", response.StatusCode)
	}
	response = request("DELETE", "/dav/files/dav-user/Docs/moved.txt", nil, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d", response.StatusCode)
	}
	response = request("LOCK", "/dav/files/dav-user/Docs/created-by-lock.txt", strings.NewReader(`<d:lockinfo xmlns:d="DAV:"><d:lockscope><d:exclusive/></d:lockscope><d:locktype><d:write/></d:locktype></d:lockinfo>`), map[string]string{"Depth": "0"})
	lockToken = response.Header.Get("Lock-Token")
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || lockToken == "" {
		t.Fatalf("LOCK new resource = %d token=%q", response.StatusCode, lockToken)
	}
	response = request("GET", "/dav/files/dav-user/Docs/created-by-lock.txt", nil, nil)
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(data) != 0 {
		t.Fatalf("new locked resource GET = %d %q", response.StatusCode, data)
	}
	response = request("GET", "/dav/files/other-user/Docs/hello.txt", nil, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("different username path = %d", response.StatusCode)
	}
}

func TestTrashPurgeRetainsSharedBlobUntilItsFinalReference(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"purge-owner","email":"purge@example.test","password":"P4!rQm#6zW8pL2cN"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(response.Cookies()) == 0 {
		t.Fatalf("register = %d", response.StatusCode)
	}
	cookie := response.Cookies()[0]
	var uid int64
	var root string
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='purge-owner'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	root = `42345678-1234-1234-1234-123456789abc`
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,NULL,'folder','',?)`, root, uid, root); err != nil {
		t.Fatal(err)
	}
	blobID := `12345678-1234-1234-1234-123456789abc`
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,?,41,?,1,2)`, blobID, uid, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`UPDATE users SET used_bytes=41 WHERE id=?`, uid); err != nil {
		t.Fatal(err)
	}
	ids := []string{`22345678-1234-1234-1234-123456789abc`, `32345678-1234-1234-1234-123456789abc`}
	for i, id := range ids {
		if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,blob_id,etag) VALUES(?,?,?,'file',?,41,'text/plain',?,?)`, id, uid, root, []string{"a.txt", "b.txt"}[i], blobID, "etag-"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	blobPath := filepath.Join(app.Runtime.Config.Paths.BlobDir, strconv.FormatInt(uid, 10), blobID[:2], blobID)
	if err = os.MkdirAll(filepath.Dir(blobPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(blobPath, []byte("encrypted fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	do := func(method, path string) int {
		req, _ := http.NewRequest(method, server.URL+path, nil)
		req.AddCookie(cookie)
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode >= 400 {
			t.Logf("%s %s: %s", method, path, body)
		}
		return res.StatusCode
	}
	for i, id := range ids {
		if code := do(http.MethodDelete, "/api/nodes/"+id); code != http.StatusOK {
			t.Fatalf("trash %d = %d", i, code)
		}
		if code := do(http.MethodDelete, "/api/trash/"+id); code != http.StatusOK {
			t.Fatalf("purge %d = %d", i, code)
		}
		var refs int
		err = app.Runtime.DB.W.QueryRow(`SELECT ref_count FROM blobs WHERE id=?`, blobID).Scan(&refs)
		if i == 0 {
			if err != nil || refs != 1 {
				t.Fatalf("first purge blob refs = %d, %v", refs, err)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("last purge blob row remains: refs=%d err=%v", refs, err)
		}
	}
	var used int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT used_bytes FROM users WHERE id=?`, uid).Scan(&used); err != nil || used != 0 {
		t.Fatalf("used bytes after purge = %d, %v", used, err)
	}
	if _, err = os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatalf("orphan blob still exists: %v", err)
	}
}

func TestFileTreeLifecycleUsesAccountScopedJournal(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	client.Timeout = 15 * time.Second
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"tree-owner","email":"tree@example.test","password":"T8!rQm#4zW6pL2cN"}`))
	register.Header.Set("Content-Type", "application/json")
	response, err := client.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(response.Cookies()) == 0 {
		t.Fatalf("registration = %d", response.StatusCode)
	}
	client.Jar = nil
	cookie := response.Cookies()[0]
	if _, err = app.Runtime.DB.W.Exec(`UPDATE users SET email_verified_at=datetime('now') WHERE username='tree-owner'`); err != nil {
		t.Fatal(err)
	}
	do := func(method, path, body string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res, b
	}
	payload := "NeoCloud encrypted transfer fixture"
	upload, err := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId=root&name=readme.txt", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	upload.AddCookie(cookie)
	uploadResponse, err := client.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	uploadBody, _ := io.ReadAll(uploadResponse.Body)
	uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusCreated {
		t.Fatalf("upload = %d: %s", uploadResponse.StatusCode, uploadBody)
	}
	var uploaded struct {
		ID   string `json:"id"`
		Etag string `json:"etag"`
	}
	if err = json.Unmarshal(uploadBody, &uploaded); err != nil || uploaded.ID == "" || uploaded.Etag == "" {
		t.Fatalf("upload payload: %s (%v)", uploadBody, err)
	}
	var storedBlob string
	if err = app.Runtime.DB.W.QueryRow(`SELECT blob_id FROM nodes WHERE id=?`, uploaded.ID).Scan(&storedBlob); err != nil {
		t.Fatal(err)
	}
	var ownerID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT user_id FROM nodes WHERE id=?`, uploaded.ID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	storedPath := filepath.Join(app.Runtime.Config.Paths.BlobDir, strconv.FormatInt(ownerID, 10), storedBlob[:2], storedBlob)
	ciphertext, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(payload)) {
		t.Fatal("uploaded content was stored in plaintext")
	}
	var usedBeforeCopy int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT used_bytes FROM users WHERE id=?`, ownerID).Scan(&usedBeforeCopy); err != nil {
		t.Fatal(err)
	}
	res, body := do(http.MethodPost, "/api/nodes/"+uploaded.ID+"/copy", `{"name":"readme clone.md"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("copy file = %d: %s", res.StatusCode, body)
	}
	var fileCopy struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &fileCopy); err != nil || fileCopy.ID == "" {
		t.Fatalf("copied file payload: %s (%v)", body, err)
	}
	var refs int
	if err = app.Runtime.DB.W.QueryRow(`SELECT ref_count FROM blobs WHERE id=?`, storedBlob).Scan(&refs); err != nil || refs != 2 {
		t.Fatalf("copied blob references = %d, %v", refs, err)
	}
	var usedAfterCopy int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT used_bytes FROM users WHERE id=?`, ownerID).Scan(&usedAfterCopy); err != nil || usedAfterCopy != usedBeforeCopy {
		t.Fatalf("storage charged for shared copy: before=%d after=%d err=%v", usedBeforeCopy, usedAfterCopy, err)
	}
	res, body = do(http.MethodGet, "/api/files/"+fileCopy.ID+"/content", "")
	if res.StatusCode != http.StatusOK || string(body) != payload || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/markdown") {
		t.Fatalf("copied file download = %d type=%q: %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	res, body = do(http.MethodPost, "/api/account/devices", `{"name":"Copy test Mac","platform":"macos"}`)
	var provisioned struct {
		Device struct {
			ID int64 `json:"id"`
		} `json:"device"`
		Token string `json:"token"`
	}
	if res.StatusCode != http.StatusCreated || json.Unmarshal(body, &provisioned) != nil || provisioned.Device.ID == 0 || !strings.HasPrefix(provisioned.Token, "nc_") {
		t.Fatalf("create device token = %d: %s", res.StatusCode, body)
	}
	deviceRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/nodes/"+uploaded.ID, nil)
	deviceRequest.Header.Set("Authorization", "Bearer "+provisioned.Token)
	deviceResponse, err := client.Do(deviceRequest)
	if err != nil {
		t.Fatal(err)
	}
	deviceBody, _ := io.ReadAll(deviceResponse.Body)
	deviceResponse.Body.Close()
	if deviceResponse.StatusCode != http.StatusOK || !bytes.Contains(deviceBody, []byte(uploaded.ID)) {
		t.Fatalf("device token file request = %d: %s", deviceResponse.StatusCode, deviceBody)
	}
	deviceMeRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/devices/me", nil)
	deviceMeRequest.Header.Set("Authorization", "Bearer "+provisioned.Token)
	deviceMeResponse, err := client.Do(deviceMeRequest)
	if err != nil {
		t.Fatal(err)
	}
	deviceMeBody, _ := io.ReadAll(deviceMeResponse.Body)
	deviceMeResponse.Body.Close()
	if deviceMeResponse.StatusCode != http.StatusOK || !bytes.Contains(deviceMeBody, []byte(`"isAdmin":true`)) {
		t.Fatalf("device self info = %d: %s", deviceMeResponse.StatusCode, deviceMeBody)
	}
	deviceRenameRequest, _ := http.NewRequest(http.MethodPatch, server.URL+"/api/devices/me", strings.NewReader(`{"name":"Self renamed Mac"}`))
	deviceRenameRequest.Header.Set("Authorization", "Bearer "+provisioned.Token)
	deviceRenameRequest.Header.Set("Content-Type", "application/json")
	deviceRenameResponse, err := client.Do(deviceRenameRequest)
	if err != nil {
		t.Fatal(err)
	}
	deviceRenameBody, _ := io.ReadAll(deviceRenameResponse.Body)
	deviceRenameResponse.Body.Close()
	if deviceRenameResponse.StatusCode != http.StatusOK || !bytes.Contains(deviceRenameBody, []byte("Self renamed Mac")) {
		t.Fatalf("device self rename = %d: %s", deviceRenameResponse.StatusCode, deviceRenameBody)
	}
	deviceAccountRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/account/devices", nil)
	deviceAccountRequest.Header.Set("Authorization", "Bearer "+provisioned.Token)
	deviceAccountResponse, err := client.Do(deviceAccountRequest)
	if err != nil {
		t.Fatal(err)
	}
	deviceAccountResponse.Body.Close()
	if deviceAccountResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("desktop token account request = %d", deviceAccountResponse.StatusCode)
	}
	res, body = do(http.MethodPatch, "/api/account/devices/"+strconv.FormatInt(provisioned.Device.ID, 10), `{"name":"Copy test Mac Renamed"}`)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Copy test Mac Renamed")) {
		t.Fatalf("rename device token = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/account/devices/"+strconv.FormatInt(provisioned.Device.ID, 10)+"/wipe", `{"eraseFiles":true}`)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"eraseFiles":true`)) {
		t.Fatalf("request device wipe = %d: %s", res.StatusCode, body)
	}
	deviceRequest, _ = http.NewRequest(http.MethodGet, server.URL+"/api/nodes/"+uploaded.ID, nil)
	deviceRequest.Header.Set("Authorization", "Bearer "+provisioned.Token)
	deviceResponse, err = client.Do(deviceRequest)
	if err != nil {
		t.Fatal(err)
	}
	wipeBody, _ := io.ReadAll(deviceResponse.Body)
	deviceResponse.Body.Close()
	if deviceResponse.StatusCode != http.StatusUnauthorized || !bytes.Contains(wipeBody, []byte(`"code":"DEVICE_WIPE_REQUESTED"`)) || !bytes.Contains(wipeBody, []byte(`"eraseFiles":true`)) {
		t.Fatalf("pending wipe response = %d: %s", deviceResponse.StatusCode, wipeBody)
	}
	deviceRequest, _ = http.NewRequest(http.MethodGet, server.URL+"/api/nodes/"+uploaded.ID, nil)
	deviceRequest.Header.Set("Authorization", "Bearer "+provisioned.Token)
	deviceResponse, err = client.Do(deviceRequest)
	if err != nil {
		t.Fatal(err)
	}
	wipeBody, _ = io.ReadAll(deviceResponse.Body)
	deviceResponse.Body.Close()
	if deviceResponse.StatusCode != http.StatusUnauthorized || !bytes.Contains(wipeBody, []byte(`"code":"UNAUTHORIZED"`)) {
		t.Fatalf("reused wiped token response = %d: %s", deviceResponse.StatusCode, wipeBody)
	}
	res, body = do(http.MethodGet, "/api/search?q=readme&limit=10", "")
	var searchResults struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"items"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &searchResults) != nil || len(searchResults.Items) != 2 {
		t.Fatalf("name search = %d: %s", res.StatusCode, body)
	}
	for _, item := range searchResults.Items {
		if !strings.Contains(strings.ToLower(item.Name), "readme") || item.Path != "/" {
			t.Fatalf("search item: %#v", item)
		}
	}
	res, body = do(http.MethodGet, "/api/search", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"items":[]`)) {
		t.Fatalf("empty search = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/search?q=needle&content=1", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"items":[]`)) {
		t.Fatalf("content search status = %d: %s", res.StatusCode, body)
	}
	duplicateUpload, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId=root&name=readme-duplicate.txt", strings.NewReader(payload))
	duplicateUpload.AddCookie(cookie)
	duplicateResponse, err := client.Do(duplicateUpload)
	if err != nil {
		t.Fatal(err)
	}
	duplicateResponse.Body.Close()
	if duplicateResponse.StatusCode != http.StatusCreated {
		t.Fatalf("upload duplicate content = %d", duplicateResponse.StatusCode)
	}
	res, body = do(http.MethodGet, "/api/storage/duplicates", "")
	var duplicates struct {
		Groups []struct {
			SHA256      string `json:"sha256"`
			Size        int64  `json:"size"`
			Reclaimable int64  `json:"reclaimable"`
			Nodes       []struct {
				ID   string `json:"id"`
				Path string `json:"path"`
			} `json:"nodes"`
		} `json:"groups"`
		Reclaimable int64 `json:"reclaimable"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &duplicates) != nil || len(duplicates.Groups) != 1 || len(duplicates.Groups[0].Nodes) != 3 || duplicates.Reclaimable != int64(len(payload)) || duplicates.Groups[0].Reclaimable != int64(len(payload)) {
		t.Fatalf("duplicate report = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/storage/breakdown", "")
	var breakdown struct {
		Total    int64 `json:"total"`
		Files    int64 `json:"files"`
		Versions int64 `json:"versions"`
		ByType   []struct {
			Type  string `json:"type"`
			Count int    `json:"count"`
		} `json:"byType"`
		ByFolder []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"byFolder"`
		Largest []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"largest"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &breakdown) != nil || breakdown.Total != usedBeforeCopy+int64(len(payload)) || breakdown.Files != int64(3*len(payload)) || breakdown.Versions != 0 || len(breakdown.ByType) == 0 || len(breakdown.Largest) != 3 {
		t.Fatalf("storage breakdown = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/files/"+uploaded.ID+"/content", "")
	if res.StatusCode != http.StatusOK || string(body) != payload {
		t.Fatalf("download = %d: %q", res.StatusCode, body)
	}
	rangeRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/files/"+uploaded.ID+"/content", nil)
	rangeRequest.AddCookie(cookie)
	rangeRequest.Header.Set("Range", "bytes=3-10")
	rangeResponse, err := client.Do(rangeRequest)
	if err != nil {
		t.Fatal(err)
	}
	rangeBody, _ := io.ReadAll(rangeResponse.Body)
	rangeResponse.Body.Close()
	if rangeResponse.StatusCode != http.StatusPartialContent || string(rangeBody) != payload[3:11] {
		t.Fatalf("range download = %d: %q", rangeResponse.StatusCode, rangeBody)
	}
	badRange, _ := http.NewRequest(http.MethodGet, server.URL+"/api/files/"+uploaded.ID+"/content", nil)
	badRange.AddCookie(cookie)
	badRange.Header.Set("Range", "bytes=999-")
	badRangeResponse, err := client.Do(badRange)
	if err != nil {
		t.Fatal(err)
	}
	badRangeResponse.Body.Close()
	if badRangeResponse.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfiable range = %d", badRangeResponse.StatusCode)
	}
	cached, _ := http.NewRequest(http.MethodGet, server.URL+"/api/files/"+uploaded.ID+"/content", nil)
	cached.AddCookie(cookie)
	cached.Header.Set("If-None-Match", `"`+uploaded.Etag+`"`)
	cachedResponse, err := client.Do(cached)
	if err != nil {
		t.Fatal(err)
	}
	cachedResponse.Body.Close()
	if cachedResponse.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional download = %d", cachedResponse.StatusCode)
	}
	updatedPayload := "updated NeoCloud file"
	staleRequest, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId=root&name=readme.txt&conflict=replace", strings.NewReader(updatedPayload))
	staleRequest.AddCookie(cookie)
	staleRequest.Header.Set("If-Match", `"stale-etag"`)
	staleResponse, err := client.Do(staleRequest)
	if err != nil {
		t.Fatal(err)
	}
	staleResponse.Body.Close()
	if staleResponse.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("stale replace = %d", staleResponse.StatusCode)
	}
	updateRequest, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId=root&name=readme.txt&conflict=replace", strings.NewReader(updatedPayload))
	updateRequest.AddCookie(cookie)
	updateRequest.Header.Set("If-Match", `"`+uploaded.Etag+`"`)
	updateResponse, err := client.Do(updateRequest)
	if err != nil {
		t.Fatal(err)
	}
	updateBody, _ := io.ReadAll(updateResponse.Body)
	updateResponse.Body.Close()
	if updateResponse.StatusCode != http.StatusCreated {
		t.Fatalf("replace upload = %d: %s", updateResponse.StatusCode, updateBody)
	}
	res, body = do(http.MethodGet, "/api/files/"+uploaded.ID+"/content?version=1", "")
	if res.StatusCode != http.StatusOK || string(body) != payload {
		t.Fatalf("version download = %d: %q", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/files/"+uploaded.ID+"/versions", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"version":1`)) || !bytes.Contains(body, []byte(`"current"`)) {
		t.Fatalf("list versions = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/files/"+uploaded.ID+"/versions/1/restore", `{}`)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"version":3`)) {
		t.Fatalf("restore version = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/files/"+uploaded.ID+"/content", "")
	if res.StatusCode != http.StatusOK || string(body) != payload {
		t.Fatalf("restored version download = %d: %q", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/uploads", `{"parentId":"root","name":"resumable.txt","size":18,"sha256":""}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create resumable upload = %d: %s", res.StatusCode, body)
	}
	var resumable struct {
		ID        string `json:"id"`
		PartCount int    `json:"partCount"`
	}
	if err = json.Unmarshal(body, &resumable); err != nil || resumable.ID == "" || resumable.PartCount != 1 {
		t.Fatalf("resumable upload payload: %s (%v)", body, err)
	}
	res, body = do(http.MethodPost, "/api/uploads/"+resumable.ID+"/complete", `{}`)
	if res.StatusCode != http.StatusConflict || !bytes.Contains(body, []byte("UPLOAD_INCOMPLETE")) {
		t.Fatalf("incomplete resumable upload = %d: %s", res.StatusCode, body)
	}
	part := "chunked-content-17"
	partRequest, _ := http.NewRequest(http.MethodPut, server.URL+"/api/uploads/"+resumable.ID+"/parts/0", strings.NewReader(part))
	partRequest.AddCookie(cookie)
	partResponse, err := client.Do(partRequest)
	if err != nil {
		t.Fatal(err)
	}
	partBody, _ := io.ReadAll(partResponse.Body)
	partResponse.Body.Close()
	if partResponse.StatusCode != http.StatusOK {
		t.Fatalf("upload part = %d: %s", partResponse.StatusCode, partBody)
	}
	res, body = do(http.MethodPost, "/api/uploads/"+resumable.ID+"/complete", `{}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("complete resumable upload = %d: %s", res.StatusCode, body)
	}
	var resumableFile struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &resumableFile); err != nil || resumableFile.ID == "" {
		t.Fatalf("completed upload payload: %s (%v)", body, err)
	}
	res, body = do(http.MethodGet, "/api/files/"+resumableFile.ID+"/content", "")
	if res.StatusCode != http.StatusOK || string(body) != part {
		t.Fatalf("resumable download = %d: %q", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/folders", `{"name":"Copy source"}`)
	var sourceFolder struct {
		ID string `json:"id"`
	}
	if res.StatusCode != http.StatusCreated || json.Unmarshal(body, &sourceFolder) != nil {
		t.Fatalf("create copy source folder = %d: %s", res.StatusCode, body)
	}
	childUpload, _ := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId="+sourceFolder.ID+"&name=child.txt", strings.NewReader("nested"))
	childUpload.AddCookie(cookie)
	childResponse, err := client.Do(childUpload)
	if err != nil {
		t.Fatal(err)
	}
	childResponse.Body.Close()
	if childResponse.StatusCode != http.StatusCreated {
		t.Fatalf("upload nested copy fixture = %d", childResponse.StatusCode)
	}
	res, body = do(http.MethodGet, "/api/search?parentId="+sourceFolder.ID+"&type=document", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"name":"child.txt"`)) || !bytes.Contains(body, []byte(`"path":"/Copy source"`)) {
		t.Fatalf("scoped type search = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/nodes/"+sourceFolder.ID+"/copy", `{"name":"Copied source"}`)
	var copiedFolder struct {
		ID string `json:"id"`
	}
	if res.StatusCode != http.StatusCreated || json.Unmarshal(body, &copiedFolder) != nil {
		t.Fatalf("copy folder = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/folders/"+copiedFolder.ID+"/children", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"name":"child.txt"`)) {
		t.Fatalf("copied folder children = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/folders", `{"name":"Photos"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create folder = %d: %s", res.StatusCode, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		t.Fatalf("created folder payload: %s (%v)", body, err)
	}
	res, body = do(http.MethodPut, "/api/nodes/"+created.ID+"/favorite", `{}`)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"favorite":true`)) {
		t.Fatalf("favorite = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/favorites", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"path":"/"`)) {
		t.Fatalf("favorites = %d: %s", res.StatusCode, body)
	}
	res, _ = do(http.MethodPost, "/api/nodes/"+created.ID+"/opened", `{}`)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("record open = %d", res.StatusCode)
	}
	res, body = do(http.MethodGet, "/api/recent?by=unknown", "")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid recent mode = %d: %s", res.StatusCode, body)
	}
	res, _ = do(http.MethodDelete, "/api/nodes/"+created.ID+"/favorite", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear favorite = %d", res.StatusCode)
	}
	res, body = do(http.MethodPost, "/api/folders", `{"name":"Archive"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create second folder = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/folders/root/children", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(created.ID)) {
		t.Fatalf("list folder = %d: %s", res.StatusCode, body)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	res, body = do(http.MethodGet, "/api/folders/root/children?limit=1", "")
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &page) != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first cursor page = %d: %s", res.StatusCode, body)
	}
	firstID := page.Items[0].ID
	res, body = do(http.MethodGet, "/api/folders/root/children?limit=1&cursor="+page.NextCursor, "")
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &page) != nil || len(page.Items) != 1 || page.Items[0].ID == firstID {
		t.Fatalf("second cursor page = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodPatch, "/api/nodes/"+created.ID, `{"name":"Pictures"}`)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"name":"Pictures"`)) {
		t.Fatalf("rename = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodDelete, "/api/nodes/"+created.ID, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("trash = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/nodes/"+created.ID, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("trashed node lookup = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/trash", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(created.ID)) || !bytes.Contains(body, []byte(`"originalPath":"/"`)) {
		t.Fatalf("trash listing = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/trash/"+created.ID+"/restore", `{}`)
	if res.StatusCode != http.StatusOK || bytes.Contains(body, []byte(`"trashedAt":`)) {
		t.Fatalf("restore = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodDelete, "/api/nodes/"+created.ID, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("trash before purge = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodDelete, "/api/trash/"+created.ID, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("purge = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodGet, "/api/nodes/"+created.ID, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("purged node lookup = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodPost, "/api/folders", `{"name":"Temporary"}`)
	var temporary struct {
		ID string `json:"id"`
	}
	if res.StatusCode != http.StatusCreated || json.Unmarshal(body, &temporary) != nil {
		t.Fatalf("create temporary folder = %d: %s", res.StatusCode, body)
	}
	if res, body = do(http.MethodDelete, "/api/nodes/"+temporary.ID, ""); res.StatusCode != http.StatusOK {
		t.Fatalf("trash temporary folder = %d: %s", res.StatusCode, body)
	}
	res, body = do(http.MethodDelete, "/api/trash", "")
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"removed":1`)) {
		t.Fatalf("empty trash = %d: %s", res.StatusCode, body)
	}
}

func TestSharedReadIsGrantBoundedAndHidesOwnerMetadata(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	register := func(username, email, password string) *http.Cookie {
		t.Helper()
		body := fmt.Sprintf(`{"username":%q,"email":%q,"password":%q}`, username, email, password)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK || len(res.Cookies()) == 0 {
			payload, _ := io.ReadAll(res.Body)
			t.Fatalf("register %s = %d: %s", username, res.StatusCode, payload)
		}
		return res.Cookies()[0]
	}
	ownerCookie := register("shared-owner", "shared-owner@example.test", "S8!hQm#4zW6pL2cN")
	memberCookie := register("shared-member", "shared-member@example.test", "S9!hQm#4zW6pL2cN")
	call := func(cookie *http.Cookie, method, path, body string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res, payload
	}
	_, body := call(ownerCookie, http.MethodPost, "/api/folders", `{"name":"Private"}`)
	var private struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &private); err != nil || private.ID == "" {
		t.Fatalf("private folder: %s %v", body, err)
	}
	_, body = call(ownerCookie, http.MethodPost, "/api/folders", `{"name":"Shared root"}`)
	var shared struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &shared); err != nil || shared.ID == "" {
		t.Fatalf("shared folder: %s %v", body, err)
	}
	_, body = call(ownerCookie, http.MethodPost, "/api/folders", fmt.Sprintf(`{"parentId":%q,"name":"Visible child"}`, shared.ID))
	var child struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &child); err != nil || child.ID == "" {
		t.Fatalf("child folder: %s %v", body, err)
	}
	upload, err := http.NewRequest(http.MethodPut, server.URL+"/api/upload?parentId="+child.ID+"&name=shared.txt", strings.NewReader("member-readable content"))
	if err != nil {
		t.Fatal(err)
	}
	upload.AddCookie(ownerCookie)
	uploadResponse, err := client.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	var sharedFile struct {
		ID string `json:"id"`
	}
	if uploadResponse.StatusCode != http.StatusCreated || json.NewDecoder(uploadResponse.Body).Decode(&sharedFile) != nil || sharedFile.ID == "" {
		payload, _ := io.ReadAll(uploadResponse.Body)
		uploadResponse.Body.Close()
		t.Fatalf("shared file upload status=%d body=%s", uploadResponse.StatusCode, payload)
	}
	uploadResponse.Body.Close()
	contentHash := sha256.Sum256([]byte("member-readable content"))
	checkBody := fmt.Sprintf(`{"parentId":%q,"sha256":%q,"size":%d}`, child.ID, hex.EncodeToString(contentHash[:]), len("member-readable content"))
	checkResponse, checkResult := call(ownerCookie, http.MethodPost, "/api/uploads/check", checkBody)
	if checkResponse.StatusCode != http.StatusOK || string(checkResult) != `{"exists":true}` {
		t.Fatalf("owner content check = %d: %s", checkResponse.StatusCode, checkResult)
	}
	_, body = call(ownerCookie, http.MethodPost, "/api/nodes/"+shared.ID+"/members", `{"recipient":"shared-member","permission":"read"}`)
	if !strings.Contains(string(body), `"permission":"read"`) {
		t.Fatalf("add member: %s", body)
	}
	var ownerID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='shared-owner'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag,vault_id) VALUES('hidden-vault',?,?,'folder','Vault','vault-etag','vault-id')`, ownerID, shared.ID); err != nil {
		t.Fatal(err)
	}
	vaultContent := []byte("content hidden in vault")
	vaultHash := sha256.Sum256(vaultContent)
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,size,etag,vault_id) VALUES('vault-secret',?,'hidden-vault','file','secret.txt',?,?, 'vault-id')`, ownerID, len(vaultContent), hex.EncodeToString(vaultHash[:])); err != nil {
		t.Fatal(err)
	}
	vaultCheckBody := fmt.Sprintf(`{"parentId":%q,"sha256":%q,"size":%d}`, shared.ID, hex.EncodeToString(vaultHash[:]), len(vaultContent))
	checkResponse, checkResult = call(ownerCookie, http.MethodPost, "/api/uploads/check", vaultCheckBody)
	if checkResponse.StatusCode != http.StatusOK || string(checkResult) != `{"exists":true}` {
		t.Fatalf("owner vault content check = %d: %s", checkResponse.StatusCode, checkResult)
	}
	checkResponse, checkResult = call(memberCookie, http.MethodPost, "/api/uploads/check", vaultCheckBody)
	if checkResponse.StatusCode != http.StatusOK || string(checkResult) != `{"exists":false}` {
		t.Fatalf("member vault content check = %d: %s", checkResponse.StatusCode, checkResult)
	}
	res, body := call(memberCookie, http.MethodGet, "/api/folders/"+shared.ID+"/children?limit=1", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("shared children = %d: %s", res.StatusCode, body)
	}
	var listing struct {
		Folder map[string]any      `json:"folder"`
		Path   []map[string]string `json:"path"`
		Items  []map[string]any    `json:"items"`
		Total  int                 `json:"total"`
	}
	if err = json.Unmarshal(body, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Items) != 1 || listing.Total != 1 || listing.Items[0]["name"] != "Visible child" {
		t.Fatalf("shared listing: %s", body)
	}
	if listing.Folder["parentId"] != nil || listing.Folder["favorite"] != false || listing.Folder["shared"] != nil || len(listing.Path) != 1 || listing.Path[0]["id"] != shared.ID {
		t.Fatalf("member view leaks boundary or metadata: %s", body)
	}
	res, body = call(memberCookie, http.MethodGet, "/api/nodes/"+child.ID, "")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"path":[{"id":"`+shared.ID+`"`) {
		t.Fatalf("shared node path = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodGet, "/api/files/"+sharedFile.ID+"/content", "")
	if res.StatusCode != http.StatusOK || string(body) != "member-readable content" {
		t.Fatalf("shared file content = %d: %q", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPost, "/api/uploads/check", checkBody)
	if res.StatusCode != http.StatusOK || string(body) != `{"exists":true}` {
		t.Fatalf("member content check = %d: %s", res.StatusCode, body)
	}
	res, _ = call(memberCookie, http.MethodGet, "/api/files/"+sharedFile.ID+"/content?version=1", "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("shared historical content status = %d", res.StatusCode)
	}
	res, body = call(memberCookie, http.MethodGet, "/api/nodes/"+private.ID, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("private node status=%d body=%s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPost, "/api/folders", fmt.Sprintf(`{"parentId":%q,"name":"Denied folder"}`, shared.ID))
	if res.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "SHARE_READ_ONLY") {
		t.Fatalf("read-only shared folder write = %d: %s", res.StatusCode, body)
	}
	var memberID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='shared-member'`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Runtime.DB.W.Exec(`UPDATE user_shares SET permission='write' WHERE owner_id=? AND grantee_id=? AND node_id=?`, ownerID, memberID, shared.ID); err != nil {
		t.Fatal(err)
	}
	res, body = call(memberCookie, http.MethodPost, "/api/folders", fmt.Sprintf(`{"parentId":%q,"name":"Member folder"}`, shared.ID))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create in write share = %d: %s", res.StatusCode, body)
	}
	var memberFolder struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &memberFolder); err != nil || memberFolder.ID == "" {
		t.Fatalf("member folder: %s %v", body, err)
	}
	res, body = call(memberCookie, http.MethodPatch, "/api/nodes/"+shared.ID, `{"name":"Changed root"}`)
	if res.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "SHARE_ROOT_IMMUTABLE") {
		t.Fatalf("write-share root mutation = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPatch, "/api/nodes/"+memberFolder.ID, fmt.Sprintf(`{"name":"Renamed folder","parentId":%q}`, child.ID))
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"Renamed folder"`) {
		t.Fatalf("rename/move within share = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPatch, "/api/nodes/"+memberFolder.ID, `{"conflict":"invalid"}`)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "CONFLICT_MODE_INVALID") {
		t.Fatalf("invalid shared conflict mode = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPatch, "/api/nodes/"+memberFolder.ID, `{"parentId":""}`)
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "SHARE_BOUNDARY") {
		t.Fatalf("empty move target = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPost, "/api/folders", `{"name":"Member private folder"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("member private folder = %d: %s", res.StatusCode, body)
	}
	var memberPrivate struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &memberPrivate); err != nil || memberPrivate.ID == "" {
		t.Fatalf("member private folder: %s %v", body, err)
	}
	res, body = call(memberCookie, http.MethodPost, "/api/uploads/check", fmt.Sprintf(`{"parentId":%q,"sha256":%q,"size":%d}`, memberPrivate.ID, hex.EncodeToString(contentHash[:]), len("member-readable content")))
	if res.StatusCode != http.StatusOK || string(body) != `{"exists":false}` {
		t.Fatalf("private content check = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPatch, "/api/nodes/"+memberFolder.ID, fmt.Sprintf(`{"parentId":%q}`, memberPrivate.ID))
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "SHARE_BOUNDARY") {
		t.Fatalf("move out of share = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodDelete, "/api/nodes/"+memberFolder.ID, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("trash shared child = %d: %s", res.StatusCode, body)
	}
	res, body = call(memberCookie, http.MethodPost, "/api/trash/"+memberFolder.ID+"/restore", "")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), memberFolder.ID) {
		t.Fatalf("restore shared child = %d: %s", res.StatusCode, body)
	}
	if _, err = app.Runtime.DB.W.Exec(`INSERT INTO shares(user_id,node_id,token,mode,note,max_downloads) VALUES(?,?,'anonymous-link-token','view','Public description',1)`, ownerID, shared.ID); err != nil {
		t.Fatal(err)
	}
	publicResponse, err := client.Get(server.URL + "/api/public/shares/anonymous-link-token")
	if err != nil {
		t.Fatal(err)
	}
	publicBody, err := io.ReadAll(publicResponse.Body)
	publicResponse.Body.Close()
	if err != nil || publicResponse.StatusCode != http.StatusOK || !strings.Contains(string(publicBody), `"sharedBy":"shared-owner"`) || strings.Contains(string(publicBody), `"id":"hidden-vault"`) {
		t.Fatalf("anonymous share browse = %d: %s (%v)", publicResponse.StatusCode, publicBody, err)
	}
	contentURL := server.URL + "/api/public/shares/anonymous-link-token/files/" + sharedFile.ID + "/content?download=1"
	firstDownload, err := client.Get(contentURL)
	if err != nil {
		t.Fatal(err)
	}
	firstContent, err := io.ReadAll(firstDownload.Body)
	var continuation *http.Cookie
	for _, cookie := range firstDownload.Cookies() {
		if cookie.Name == "nc_share_continue" {
			continuation = cookie
		}
	}
	firstDownload.Body.Close()
	if err != nil || firstDownload.StatusCode != http.StatusOK || string(firstContent) != "member-readable content" || continuation == nil {
		t.Fatalf("public content download = %d: %q (%v), continuation=%v", firstDownload.StatusCode, firstContent, err, continuation != nil)
	}
	rangeRequest, err := http.NewRequest(http.MethodGet, contentURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	rangeRequest.Header.Set("Range", "bytes=7-10")
	rangeRequest.AddCookie(continuation)
	rangeResponse, err := client.Do(rangeRequest)
	if err != nil {
		t.Fatal(err)
	}
	rangeContent, err := io.ReadAll(rangeResponse.Body)
	rangeResponse.Body.Close()
	if err != nil || rangeResponse.StatusCode != http.StatusPartialContent || string(rangeContent) != "read" {
		t.Fatalf("resumed public range = %d: %q (%v)", rangeResponse.StatusCode, rangeContent, err)
	}
	var downloadCount int
	if err = app.Runtime.DB.W.QueryRow(`SELECT download_count FROM shares WHERE token='anonymous-link-token'`).Scan(&downloadCount); err != nil || downloadCount != 1 {
		t.Fatalf("public download count=%d err=%v", downloadCount, err)
	}
	if _, err = app.Runtime.DB.W.Exec(`UPDATE shares SET max_downloads=NULL WHERE token='anonymous-link-token'`); err != nil {
		t.Fatal(err)
	}
	zipResponse, err := client.Get(server.URL + "/api/public/shares/anonymous-link-token/zip?folderId=" + child.ID)
	if err != nil {
		t.Fatal(err)
	}
	zipBody, err := io.ReadAll(zipResponse.Body)
	zipResponse.Body.Close()
	if err != nil || zipResponse.StatusCode != http.StatusOK || zipResponse.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("public ZIP = %d (%v): %s", zipResponse.StatusCode, err, zipBody)
	}
	archive, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	if err != nil {
		t.Fatalf("open public ZIP: %v", err)
	}
	foundFile := false
	for _, entry := range archive.File {
		if entry.Name != "Visible child/shared.txt" {
			continue
		}
		file, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		contents, readErr := io.ReadAll(file)
		file.Close()
		if readErr != nil || string(contents) != "member-readable content" {
			t.Fatalf("public ZIP content=%q err=%v", contents, readErr)
		}
		foundFile = true
	}
	if !foundFile {
		t.Fatalf("public ZIP omitted selected subtree file; entries: %#v", archive.File)
	}
	if _, err = app.Runtime.DB.W.Exec(`UPDATE shares SET mode='view_upload',max_upload_bytes=1024,uploaded_bytes=0 WHERE token='anonymous-link-token'`); err != nil {
		t.Fatal(err)
	}
	publicUpload, err := http.NewRequest(http.MethodPut, server.URL+"/api/public/shares/anonymous-link-token/upload?folderId="+child.ID+"&name=public.txt", strings.NewReader("anonymous upload contents"))
	if err != nil {
		t.Fatal(err)
	}
	publicUploadResponse, err := client.Do(publicUpload)
	if err != nil {
		t.Fatal(err)
	}
	var publicFile struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if publicUploadResponse.StatusCode != http.StatusCreated || json.NewDecoder(publicUploadResponse.Body).Decode(&publicFile) != nil || publicFile.ID == "" || publicFile.Name != "public.txt" {
		body, _ := io.ReadAll(publicUploadResponse.Body)
		publicUploadResponse.Body.Close()
		t.Fatalf("public upload = %d: %s", publicUploadResponse.StatusCode, body)
	}
	publicUploadResponse.Body.Close()
	ownerRead, err := http.NewRequest(http.MethodGet, server.URL+"/api/files/"+publicFile.ID+"/content", nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerRead.AddCookie(ownerCookie)
	ownerReadResponse, err := client.Do(ownerRead)
	if err != nil {
		t.Fatal(err)
	}
	ownerReadBody, err := io.ReadAll(ownerReadResponse.Body)
	ownerReadResponse.Body.Close()
	if err != nil || ownerReadResponse.StatusCode != http.StatusOK || string(ownerReadBody) != "anonymous upload contents" {
		t.Fatalf("read public upload as owner = %d: %q (%v)", ownerReadResponse.StatusCode, ownerReadBody, err)
	}
	var uploadedBytes int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT uploaded_bytes FROM shares WHERE token='anonymous-link-token'`).Scan(&uploadedBytes); err != nil || uploadedBytes != int64(len("anonymous upload contents")) {
		t.Fatalf("public share uploaded bytes = %d err=%v", uploadedBytes, err)
	}
	res, body = call(ownerCookie, http.MethodPatch, "/api/nodes/"+sharedFile.ID, fmt.Sprintf(`{"parentId":%q}`, private.ID))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("move shared file out = %d: %s", res.StatusCode, body)
	}
	var affectedParents string
	if err = app.Runtime.DB.W.QueryRow(`SELECT parent_ids_json FROM changes WHERE user_id=? AND node_id=? ORDER BY seq DESC LIMIT 1`, ownerID, sharedFile.ID).Scan(&affectedParents); err != nil {
		t.Fatal(err)
	}
	var parentIDs []string
	if err = json.Unmarshal([]byte(affectedParents), &parentIDs); err != nil || len(parentIDs) != 2 || parentIDs[0] != child.ID || parentIDs[1] != private.ID {
		t.Fatalf("move journal parents=%s err=%v", affectedParents, err)
	}
	var memberShareID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM user_shares WHERE owner_id=? AND node_id=? AND grantee_id=(SELECT id FROM users WHERE username='shared-member')`, ownerID, shared.ID).Scan(&memberShareID); err != nil {
		t.Fatal(err)
	}
	openWatch := func(cookie *http.Cookie) (<-chan string, func()) {
		t.Helper()
		watchRequest, requestErr := http.NewRequest(http.MethodGet, server.URL+"/api/sync/watch", nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		watchRequest.AddCookie(cookie)
		watchResponse, requestErr := client.Do(watchRequest)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		if watchResponse.StatusCode != http.StatusOK {
			payload, _ := io.ReadAll(watchResponse.Body)
			watchResponse.Body.Close()
			t.Fatalf("sync watch = %d: %s", watchResponse.StatusCode, payload)
		}
		reader := bufio.NewReader(watchResponse.Body)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				watchResponse.Body.Close()
				t.Fatalf("read sync ready event: %v", readErr)
			}
			if line == "\r\n" || line == "\n" {
				break
			}
		}
		result := make(chan string, 1)
		go func() {
			var frame strings.Builder
			for {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					return
				}
				frame.WriteString(line)
				if line == "\r\n" || line == "\n" {
					if strings.Contains(frame.String(), "event: shares:changed") {
						result <- frame.String()
					}
					frame.Reset()
				}
			}
		}()
		return result, func() { _ = watchResponse.Body.Close() }
	}
	ownerShareEvents, closeOwnerWatch := openWatch(ownerCookie)
	defer closeOwnerWatch()
	memberShareEvents, closeMemberWatch := openWatch(memberCookie)
	defer closeMemberWatch()
	leaveResponse, leaveBody := call(memberCookie, http.MethodDelete, "/api/shared-with-me/"+strconv.FormatInt(memberShareID, 10), "")
	if leaveResponse.StatusCode != http.StatusOK {
		t.Fatalf("member leaves share = %d: %s", leaveResponse.StatusCode, leaveBody)
	}
	for name, events := range map[string]<-chan string{"owner": ownerShareEvents, "member": memberShareEvents} {
		select {
		case frame := <-events:
			lines := strings.Split(strings.TrimSuffix(frame, "\n\n"), "\n")
			invalid := len(lines) != 3 || strings.Contains(frame, "shared-member") || strings.Contains(frame, "shared-member@example.test")
			if !invalid {
				cursor, cursorErr := strconv.ParseInt(strings.TrimPrefix(lines[0], "id: "), 10, 64)
				invalid = !strings.HasPrefix(lines[0], "id: ") || cursorErr != nil || cursor < 0 || lines[1] != "event: shares:changed" || lines[2] != "data: {}"
			}
			if invalid {
				t.Fatalf("%s shares invalidation disclosed data or changed cursor/framing: %q", name, frame)
			}
		case <-time.After(8 * time.Second):
			t.Fatalf("%s sync stream did not invalidate shares after member left", name)
		}
	}
	select {
	case duplicate := <-ownerShareEvents:
		t.Fatalf("owner received a duplicate shares invalidation: %q", duplicate)
	case <-time.After(5200 * time.Millisecond):
	}
}

func TestComposedHandoffBrowserToPhoneAndPhoneReceivePreference(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"handoff-browser","email":"handoff-browser@example.test","password":"H4!nQm#6zW8pL2cN"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, err := client.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	registered.Body.Close()
	if registered.StatusCode != http.StatusOK || len(registered.Cookies()) == 0 {
		t.Fatalf("register=%d", registered.StatusCode)
	}
	cookie := registered.Cookies()[0]
	var userID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM users WHERE username='handoff-browser'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	token := "nc_handoff_receiver_test_token"
	tokenHash := sha256.Sum256([]byte(token))
	result, err := app.Runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash,last_seen_at) VALUES(?,?,?,?,datetime('now'))`, userID, "Phone", "ios", hex.EncodeToString(tokenHash[:]))
	if err != nil {
		t.Fatal(err)
	}
	deviceID, _ := result.LastInsertId()
	do := func(method, path, body string, auth string) (int, []byte) {
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		} else {
			req.AddCookie(cookie)
		}
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		payload, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		return res.StatusCode, payload
	}
	// A browser is a device while it holds its live connection.
	browser := dialLive(t, server, http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}})
	browser.await(`42["handoff:devices"`)
	status, payload := do("GET", "/api/handoff/devices", "", "")
	var deviceList struct {
		Devices []struct {
			ID         string `json:"id"`
			Kind       string `json:"kind"`
			Online     bool   `json:"online"`
			CanReceive bool   `json:"canReceive"`
		} `json:"devices"`
	}
	if status != http.StatusOK || json.Unmarshal(payload, &deviceList) != nil {
		t.Fatalf("browser devices=%d %s", status, payload)
	}
	browserFound := false
	for _, d := range deviceList.Devices {
		if strings.HasPrefix(d.ID, "s:") && d.Kind == "browser" && d.Online && d.CanReceive {
			browserFound = true
		}
	}
	if !browserFound {
		t.Fatalf("current browser missing from devices: %s", payload)
	}
	status, payload = do("POST", "/api/handoff/offers", fmt.Sprintf(`{"targetId":"d:%d","files":[{"name":"hello.txt","size":5,"mime":"text/plain"}]}`, deviceID), "")
	var created struct {
		ID    string `json:"id"`
		Files []struct {
			ID int64 `json:"id"`
		} `json:"files"`
	}
	if status != http.StatusCreated || json.Unmarshal(payload, &created) != nil || created.ID == "" || len(created.Files) != 1 {
		t.Fatalf("create offer=%d %s", status, payload)
	}
	upload, _ := http.NewRequest(http.MethodPut, server.URL+fmt.Sprintf("/api/handoff/offers/%s/files/%d", created.ID, created.Files[0].ID), strings.NewReader("hello"))
	upload.AddCookie(cookie)
	uploaded, err := client.Do(upload)
	if err != nil {
		t.Fatal(err)
	}
	uploadedPayload, _ := io.ReadAll(uploaded.Body)
	uploaded.Body.Close()
	if uploaded.StatusCode != http.StatusOK {
		t.Fatalf("upload=%d %s", uploaded.StatusCode, uploadedPayload)
	}
	status, payload = do("GET", "/api/handoff/offers", "", token)
	var lists struct {
		Inbox []struct {
			ID string `json:"id"`
		} `json:"inbox"`
	}
	if status != http.StatusOK || json.Unmarshal(payload, &lists) != nil || len(lists.Inbox) != 1 || lists.Inbox[0].ID != created.ID {
		t.Fatalf("phone inbox=%d %s", status, payload)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/handoff/events?receive=0", nil)
	events.Header.Set("Authorization", "Bearer "+token)
	stream, err := client.Do(events)
	if err != nil {
		t.Fatal(err)
	}
	if stream.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(stream.Body)
		stream.Body.Close()
		cancel()
		t.Fatalf("events=%d %s", stream.StatusCode, data)
	}
	status, payload = do("GET", "/api/handoff/devices", "", "")
	if status != http.StatusOK || json.Unmarshal(payload, &deviceList) != nil {
		stream.Body.Close()
		cancel()
		t.Fatalf("phone preference devices=%d %s", status, payload)
	}
	phoneFound := false
	for _, d := range deviceList.Devices {
		if d.ID == fmt.Sprintf("d:%d", deviceID) {
			phoneFound = d.Kind == "phone" && d.Online && !d.CanReceive
		}
	}
	stream.Body.Close()
	cancel()
	if !phoneFound {
		t.Fatalf("receive=0 phone presence was not respected: %s", payload)
	}
	status, payload = do(http.MethodDelete, fmt.Sprintf("/api/account/devices/%d", deviceID), "", "")
	if status != http.StatusOK {
		t.Fatalf("revoke handoff recipient device=%d %s", status, payload)
	}
	var offerStatus string
	var stagedBlobs, remainingBytes int
	if err = app.Runtime.DB.W.QueryRow(`SELECT status FROM handoff_offers WHERE id=?`, created.ID).Scan(&offerStatus); err != nil {
		t.Fatal(err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT COUNT(*) FROM handoff_files WHERE offer_id=? AND blob_id IS NOT NULL`, created.ID).Scan(&stagedBlobs); err != nil {
		t.Fatal(err)
	}
	if err = app.Runtime.DB.W.QueryRow(`SELECT used_bytes FROM users WHERE id=?`, userID).Scan(&remainingBytes); err != nil {
		t.Fatal(err)
	}
	if offerStatus != "cancelled" || stagedBlobs != 0 || remainingBytes != 0 {
		t.Fatalf("device revocation did not cancel and release handoff: status=%q staged=%d used=%d", offerStatus, stagedBlobs, remainingBytes)
	}
}

func TestComposedExternalStorageCopiesUseEncryptedFileService(t *testing.T) {
	remoteFiles := map[string][]byte{"/dav/incoming.txt": []byte("remote file bytes")}
	var remoteMu sync.Mutex
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			remoteMu.Lock()
			_, exists := remoteFiles[r.URL.Path]
			remoteMu.Unlock()
			if r.URL.Path != "/dav" && r.URL.Path != "/dav/" && !exists {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(207)
			if exists {
				_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>%d</d:getcontentlength></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, r.URL.Path, len(remoteFiles[r.URL.Path]))
				return
			}
			_, _ = fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			remoteMu.Lock()
			remoteFiles[r.URL.Path] = data
			remoteMu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			remoteMu.Lock()
			data, ok := remoteFiles[r.URL.Path]
			remoteMu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer remote.Close()
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		switch key {
		case "NEOCLOUD_HOME":
			return home
		case "NEOCLOUD_EXTERNAL_ALLOW_PRIVATE":
			return "true"
		case "NEOCLOUD_EXTERNAL_MAX_UPLOAD_BYTES":
			return "5"
		default:
			return ""
		}
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"storage-copy","email":"storage-copy@example.test","password":"G7!mQv#2xL9rT4pZ"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, err := client.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	registered.Body.Close()
	if registered.StatusCode != http.StatusOK || len(registered.Cookies()) == 0 {
		t.Fatalf("register=%d", registered.StatusCode)
	}
	cookie := registered.Cookies()[0]
	call := func(method, path, body string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		response, requestErr := client.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return response.StatusCode, data
	}
	status, uploadBody := call(http.MethodPut, "/api/upload?parentId=root&name=source.txt", "local file bytes")
	var uploaded struct {
		ID string `json:"id"`
	}
	if status != http.StatusCreated || json.Unmarshal(uploadBody, &uploaded) != nil || uploaded.ID == "" {
		t.Fatalf("upload=%d %s", status, uploadBody)
	}
	mountBody := fmt.Sprintf(`{"name":"Test DAV","provider":"webdav","config":{"url":%q,"username":"test","password":"secret"}}`, remote.URL+"/dav")
	status, mountPayload := call(http.MethodPost, "/api/external-storage", mountBody)
	var mount struct {
		ID string `json:"id"`
	}
	if status != http.StatusCreated || json.Unmarshal(mountPayload, &mount) != nil || mount.ID == "" {
		t.Fatalf("mount=%d %s", status, mountPayload)
	}
	status, oversized := call(http.MethodPut, "/api/external-storage/"+mount.ID+"/file?path=too-large.txt", "123456")
	if status != http.StatusRequestEntityTooLarge || !strings.Contains(string(oversized), "FILE_TOO_LARGE") {
		t.Fatalf("external upload over configured byte cap=%d %s", status, oversized)
	}
	unknownLength, err := http.NewRequest(http.MethodPut, server.URL+"/api/external-storage/"+mount.ID+"/file?path=unknown-length.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	unknownLength.ContentLength = -1
	unknownLength.Body = io.NopCloser(strings.NewReader("123456"))
	unknownLength.AddCookie(cookie)
	unknownResponse, err := client.Do(unknownLength)
	if err != nil {
		t.Fatal(err)
	}
	unknownBody, err := io.ReadAll(unknownResponse.Body)
	unknownResponse.Body.Close()
	if err != nil || unknownResponse.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(unknownBody), "FILE_TOO_LARGE") {
		t.Fatalf("unknown-length external upload over configured byte cap=%d %s (%v)", unknownResponse.StatusCode, unknownBody, err)
	}
	status, copiedIn := call(http.MethodPost, "/api/external-storage/"+mount.ID+"/copy-in", fmt.Sprintf(`{"nodeId":%q}`, uploaded.ID))
	remoteMu.Lock()
	copyInBytes := append([]byte(nil), remoteFiles["/dav/source.txt"]...)
	remoteMu.Unlock()
	if status != http.StatusCreated || string(copyInBytes) != "local file bytes" {
		t.Fatalf("copy-in=%d %s remote=%q", status, copiedIn, copyInBytes)
	}
	status, copiedOut := call(http.MethodPost, "/api/external-storage/"+mount.ID+"/copy-out", `{"path":"incoming.txt","parentId":"root"}`)
	var imported struct {
		ID string `json:"id"`
	}
	if status != http.StatusCreated || json.Unmarshal(copiedOut, &imported) != nil || imported.ID == "" {
		t.Fatalf("copy-out=%d %s", status, copiedOut)
	}
	status, importedBytes := call(http.MethodGet, "/api/files/"+imported.ID+"/content", "")
	if status != http.StatusOK || string(importedBytes) != "remote file bytes" {
		t.Fatalf("imported content=%d %q", status, importedBytes)
	}
}

func TestComposedImportWorkerTransfersWebDAVIntoEncryptedFiles(t *testing.T) {
	const remoteContent = "imported through WebDAV"
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(207)
			_, _ = fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.org/ns"><d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype><d:quota-used-bytes>23</d:quota-used-bytes></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response><d:response><d:href>/dav/source.txt</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>23</d:getcontentlength><d:getlastmodified>Thu, 26 Nov 2026 10:00:00 GMT</d:getlastmodified><oc:favorite>1</oc:favorite></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
		case http.MethodGet:
			if r.URL.Path != "/dav/source.txt" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, remoteContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer remote.Close()
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		switch key {
		case "NEOCLOUD_HOME":
			return home
		case "NEOCLOUD_IMPORT_ALLOW_PRIVATE":
			return "true"
		default:
			return ""
		}
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"import-owner","email":"import-owner@example.test","password":"G7!mQv#2xL9rT4pZ"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, err := client.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	registered.Body.Close()
	if registered.StatusCode != http.StatusOK || len(registered.Cookies()) == 0 {
		t.Fatalf("register=%d", registered.StatusCode)
	}
	cookie := registered.Cookies()[0]
	call := func(method, path, body string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		response, requestErr := client.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return response.StatusCode, data
	}
	testBody := fmt.Sprintf(`{"source":"webdav","url":%q,"username":"reader","password":"app-password"}`, remote.URL+"/dav/")
	status, tested := call(http.MethodPost, "/api/imports/test", testBody)
	if status != http.StatusOK || !bytes.Contains(tested, []byte(`"ok":true`)) || !bytes.Contains(tested, []byte(`"files":1`)) {
		t.Fatalf("import test=%d %s", status, tested)
	}
	status, created := call(http.MethodPost, "/api/imports", testBody)
	var job struct {
		ID             string `json:"id"`
		TargetFolderID string `json:"targetFolderId"`
	}
	if status != http.StatusCreated || json.Unmarshal(created, &job) != nil || job.ID == "" || job.TargetFolderID == "" {
		t.Fatalf("create import=%d %s", status, created)
	}
	app.StartWorkers(context.Background())
	deadline := time.Now().Add(15 * time.Second)
	var jobPayload []byte
	for time.Now().Before(deadline) {
		status, jobPayload = call(http.MethodGet, "/api/imports/"+job.ID, "")
		if status != http.StatusOK {
			t.Fatalf("import status=%d %s", status, jobPayload)
		}
		var state struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(jobPayload, &state) != nil {
			t.Fatalf("invalid import status response: %s", jobPayload)
		}
		if state.Status == "done" {
			break
		}
		if state.Status == "failed" {
			t.Fatalf("import failed: %s", jobPayload)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var finalState struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(jobPayload, &finalState) != nil || finalState.Status != "done" {
		t.Fatalf("import did not finish: %s", jobPayload)
	}
	var importedID string
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM nodes WHERE user_id=(SELECT id FROM users WHERE username='import-owner') AND parent_id=? AND name='source.txt'`, job.TargetFolderID).Scan(&importedID); err != nil {
		t.Fatal(err)
	}
	status, imported := call(http.MethodGet, "/api/files/"+importedID+"/content", "")
	if status != http.StatusOK || string(imported) != remoteContent {
		t.Fatalf("imported file=%d %q", status, imported)
	}
}

func TestComposedAdminCanRunEncryptedLocalBackup(t *testing.T) {
	home := t.TempDir()
	app, err := Open(context.Background(), func(key string) string {
		if key == "NEOCLOUD_HOME" {
			return home
		}
		return ""
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	app.StartWorkers(context.Background())
	defer app.Close()
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	register, _ := http.NewRequest(http.MethodPost, server.URL+"/api/auth/register", strings.NewReader(`{"username":"backup-owner","email":"backup-owner@example.test","password":"B7!mQv#2xL9rT4pZ"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, err := client.Do(register)
	if err != nil {
		t.Fatal(err)
	}
	registered.Body.Close()
	if registered.StatusCode != http.StatusOK || len(registered.Cookies()) == 0 {
		t.Fatalf("register=%d", registered.StatusCode)
	}
	cookie := registered.Cookies()[0]
	call := func(method, path, body string) (*http.Response, []byte) {
		t.Helper()
		request, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		return response, data
	}
	destination := filepath.Join(t.TempDir(), "local-archive")
	body := fmt.Sprintf(`{"name":"local archive","type":"local","config":{"path":%q},"retention":2}`, destination)
	created, createdBody := call(http.MethodPost, "/api/admin/backups/targets", body)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create target=%d %s", created.StatusCode, createdBody)
	}
	var targetID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM backup_targets WHERE name='local archive'`).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	started, startedBody := call(http.MethodPost, fmt.Sprintf("/api/admin/backups/targets/%d/run", targetID), `{}`)
	if started.StatusCode != http.StatusAccepted || !bytes.Contains(startedBody, []byte(`"started":true`)) {
		t.Fatalf("run backup=%d %s", started.StatusCode, startedBody)
	}
	var runID int64
	if err = app.Runtime.DB.W.QueryRow(`SELECT id FROM backup_runs WHERE target_id=? ORDER BY id DESC LIMIT 1`, targetID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err = app.Runtime.DB.W.QueryRow(`SELECT status FROM backup_runs WHERE id=?`, runID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "ok" {
			break
		}
		if status == "failed" {
			var message sql.NullString
			_ = app.Runtime.DB.W.QueryRow(`SELECT error FROM backup_runs WHERE id=?`, runID).Scan(&message)
			t.Fatalf("backup failed: %s", message.String)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var snapshot, status string
	if err = app.Runtime.DB.W.QueryRow(`SELECT snapshot,status FROM backup_runs WHERE id=?`, runID).Scan(&snapshot, &status); err != nil || status != "ok" {
		t.Fatalf("backup status=%q snapshot=%q err=%v", status, snapshot, err)
	}
	if _, err = os.Stat(filepath.Join(destination, "snapshots", snapshot, "neocloud.db.enc")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(destination, "snapshots", snapshot, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	listed, listedBody := call(http.MethodGet, "/api/admin/backups", "")
	if listed.StatusCode != http.StatusOK || !bytes.Contains(listedBody, []byte(`"status":"ok"`)) || !bytes.Contains(listedBody, []byte(`"bytesTotal":0`)) {
		t.Fatalf("backup list=%d %s", listed.StatusCode, listedBody)
	}
}
