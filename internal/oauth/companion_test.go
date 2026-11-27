package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompanionBootstrapCanonicalizesAndReusesClient(t *testing.T) {
	h, db, _ := testHandler(t)
	mux := http.NewServeMux()
	h.CompanionRoutes(mux, func(*http.Request) string { return "https://cloud.example/" })
	redirect := "HTTP://LOCALHOST:080/tmp/../path/api/integrations/oauth/callback"
	first := companionRequest(mux, `{"redirectUri":"`+redirect+`"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("bootstrap = %d: %s", first.Code, first.Body.String())
	}
	var response struct {
		Companion             string   `json:"companion"`
		Created               bool     `json:"created"`
		ClientID              string   `json:"clientId"`
		RedirectURI           string   `json:"redirectUri"`
		Scopes                []string `json:"scopes"`
		AuthorizationEndpoint string   `json:"authorizationEndpoint"`
		TokenEndpoint         string   `json:"tokenEndpoint"`
		UserinfoEndpoint      string   `json:"userinfoEndpoint"`
		RevocationEndpoint    string   `json:"revocationEndpoint"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Companion != "neoagent" || !response.Created || !strings.HasPrefix(response.ClientID, "ncoc_") || response.RedirectURI != "http://localhost/path/api/integrations/oauth/callback" {
		t.Fatalf("bootstrap response = %+v", response)
	}
	wantScopes := []string{"files:read", "files:write", "signing:read", "signing:write"}
	if len(response.Scopes) != len(wantScopes) {
		t.Fatalf("scopes = %v", response.Scopes)
	}
	for i := range wantScopes {
		if response.Scopes[i] != wantScopes[i] {
			t.Fatalf("scopes = %v", response.Scopes)
		}
	}
	if response.AuthorizationEndpoint != "https://cloud.example/oauth/authorize" || response.TokenEndpoint != "https://cloud.example/oauth/token" || response.UserinfoEndpoint != "https://cloud.example/oauth/userinfo" || response.RevocationEndpoint != "https://cloud.example/oauth/revoke" {
		t.Fatalf("endpoint URLs = %+v", response)
	}
	second := companionRequest(mux, `{"redirectUri":"http://localhost/path/api/integrations/oauth/callback","appName":"NeoAgent"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("re-bootstrap = %d: %s", second.Code, second.Body.String())
	}
	var repeated struct {
		Created  bool   `json:"created"`
		ClientID string `json:"clientId"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &repeated); err != nil || repeated.Created || repeated.ClientID != response.ClientID {
		t.Fatalf("re-bootstrap = %+v, error = %v", repeated, err)
	}
	var count int
	if err := db.W.QueryRow(`SELECT COUNT(*) FROM oauth_clients WHERE kind=? AND redirect_uri=? AND revoked_at IS NULL`, companionClientKind, response.RedirectURI).Scan(&count); err != nil || count != 1 {
		t.Fatalf("active client count = %d, error = %v", count, err)
	}
}

func TestCompanionBootstrapValidatesRedirectAndAppName(t *testing.T) {
	h, _, _ := testHandler(t)
	mux := http.NewServeMux()
	h.CompanionRoutes(mux, func(*http.Request) string { return "https://cloud.example" })
	valid := []string{
		"https://agent.example/api/integrations/oauth/callback",
		"http://127.0.0.1:3333/api/integrations/oauth/callback",
		"http://10.1.2.3/api/integrations/oauth/callback",
		"http://172.31.0.1/api/integrations/oauth/callback",
		"http://192.168.1.2/api/integrations/oauth/callback",
		"http://neo.localhost/api/integrations/oauth/callback",
		"http://[::1]:3333/api/integrations/oauth/callback",
		"http://[fd00::2]/api/integrations/oauth/callback",
		"http://localhost/a/%2e%2e/api/integrations/oauth/callback",
	}
	for _, redirect := range valid {
		if got := companionRequest(mux, `{"redirectUri":"`+redirect+`"}`); got.Code != http.StatusOK {
			t.Errorf("valid redirect %q rejected: %d %s", redirect, got.Code, got.Body.String())
		}
	}
	invalid := []string{
		"http://public.example/api/integrations/oauth/callback",
		"ftp://localhost/api/integrations/oauth/callback",
		"https://agent.example/api/integrations/oauth/callback?next=x",
		"https://agent.example/api/integrations/oauth/callback#fragment",
		"https://user:pass@agent.example/api/integrations/oauth/callback",
		"https://agent.example/api/integrations/oauth/other",
		"https://agent.example:abc/api/integrations/oauth/callback",
		"https://agent.example:65536/api/integrations/oauth/callback",
	}
	for _, redirect := range invalid {
		if got := companionRequest(mux, `{"redirectUri":"`+redirect+`"}`); got.Code != http.StatusBadRequest {
			t.Errorf("invalid redirect %q accepted: %d %s", redirect, got.Code, got.Body.String())
		}
	}
	for _, body := range []string{
		`{"redirectUri":"https://agent.example/api/integrations/oauth/callback","appName":"Other"}`,
		`{"redirectUri":"not-a-url"}`,
		`{`,
	} {
		if got := companionRequest(mux, body); got.Code != http.StatusBadRequest {
			t.Errorf("invalid body %q accepted: %d %s", body, got.Code, got.Body.String())
		}
	}
}

func companionRequest(mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/oauth/companion/neoagent/bootstrap", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}
