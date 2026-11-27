// Package apptest runs the composed server on a temporary home for tests that
// span several parts of the API.
package apptest

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/application"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

const Password = "G7!mQv#2xL9rT4pZ"

type Server struct {
	App  *application.Application
	HTTP *httptest.Server
	DB   *sql.DB
}

// Start serves a fresh installation; env adds settings to its environment.
func Start(t testing.TB, env map[string]string) *Server {
	t.Helper()
	home := t.TempDir()
	values := map[string]string{"NEOCLOUD_HOME": home, "NEOCLOUD_MIN_FREE_DISK": "0"}
	for key, value := range env {
		values[key] = value
	}
	app, err := application.Open(context.Background(), func(key string) string { return values[key] }, home)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		server.Close()
		if err := app.Close(); err != nil {
			t.Error(err)
		}
	})
	return &Server{App: app, HTTP: server, DB: app.Runtime.DB.W}
}

// Client is a browser (its own cookies) or a device (a bearer token).
type Client struct {
	Server *Server
	Name   string
	ID     int64
	Token  string
	http   *http.Client
}

// Visitor is a browser nobody is signed in to.
func (s *Server) Visitor(t testing.TB) *Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{Server: s, http: &http.Client{Jar: jar}}
}

// Register signs a new account up; the first one is the admin.
func (s *Server) Register(t testing.TB, name string) *Client {
	t.Helper()
	c := s.Visitor(t)
	c.Expect(t, 200, "POST", "/api/auth/register", map[string]any{"username": name, "email": name + "@example.test", "password": Password})
	c.Name = name
	if err := s.DB.QueryRow(`SELECT id FROM users WHERE username=?`, name).Scan(&c.ID); err != nil {
		t.Fatal(err)
	}
	return c
}

// Device is a client holding a device token of this account.
func (c *Client) Device(t testing.TB, platform string) *Client {
	t.Helper()
	token := "nc_" + base64.RawURLEncoding.EncodeToString(Random(32))
	if _, err := c.Server.DB.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(?,?,?,?)`, c.ID, platform, platform, passhash.DigestToken(token)); err != nil {
		t.Fatal(err)
	}
	return &Client{Server: c.Server, Name: c.Name, ID: c.ID, Token: token, http: &http.Client{}}
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r Response) JSON(t testing.TB) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(r.Body, &value); err != nil {
		t.Fatalf("not JSON (%d): %q", r.Status, r.Body)
	}
	return value
}

// Do sends one request; headers are name, value pairs.
func (c *Client) Do(t testing.TB, method, path string, body []byte, headers ...string) Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, c.Server.HTTP.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response, err := c.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return Response{response.StatusCode, response.Header, data}
}

// Expect sends a request with an optional JSON body and checks the status.
func (c *Client) Expect(t testing.TB, status int, method, path string, body any) Response {
	t.Helper()
	var data []byte
	headers := []string{}
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
		headers = []string{"Content-Type", "application/json"}
	}
	response := c.Do(t, method, path, data, headers...)
	if response.Status != status {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, response.Status, status, response.Body)
	}
	return response
}

// Call is Expect for endpoints answering a JSON object.
func (c *Client) Call(t testing.TB, status int, method, path string, body any) map[string]any {
	t.Helper()
	return c.Expect(t, status, method, path, body).JSON(t)
}

// Folder makes a folder and returns its id.
func (c *Client) Folder(t testing.TB, parentID, name string) string {
	t.Helper()
	return c.Call(t, 201, "POST", "/api/folders", map[string]any{"parentId": parentID, "name": name})["id"].(string)
}

// Upload sends a whole file and returns its id.
func (c *Client) Upload(t testing.TB, parentID, name, content string) string {
	t.Helper()
	query := url.Values{"parentId": {parentID}, "name": {name}}
	response := c.Do(t, "PUT", "/api/upload?"+query.Encode(), []byte(content), "Content-Type", "application/octet-stream")
	if response.Status != 201 {
		t.Fatalf("upload %s = %d: %s", name, response.Status, response.Body)
	}
	return response.JSON(t)["id"].(string)
}

// Vault makes a vault folder and returns the answer.
func (c *Client) Vault(t testing.TB, parentID, name, conflict string) map[string]any {
	t.Helper()
	body := KeyParams()
	body["parentId"], body["name"], body["conflict"] = parentID, name, conflict
	return c.Call(t, 201, "POST", "/api/vaults", body)
}

// KeyParams is well-formed vault key material.
func KeyParams() map[string]any {
	return map[string]any{"salt": base64.StdEncoding.EncodeToString(Random(16)), "iterations": 600000, "wrappedKey": base64.StdEncoding.EncodeToString(Random(60))}
}

func Random(n int) []byte {
	data := make([]byte, n)
	_, _ = rand.Read(data)
	return data
}

// Names lists a field of each object in a decoded JSON array.
func Names(list any, field string) string {
	var names []string
	for _, item := range list.([]any) {
		names = append(names, item.(map[string]any)[field].(string))
	}
	return strings.Join(names, ",")
}
