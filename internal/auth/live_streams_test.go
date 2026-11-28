package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
)

func TestRevokingACredentialEndsItsEventStreams(t *testing.T) {
	previous := streamCheckInterval
	streamCheckInterval = 20 * time.Millisecond
	t.Cleanup(func() { streamCheckInterval = previous })
	runtime, handler := testAuthServer(t)
	login := serveJSON(t, handler, http.MethodPost, "/api/auth/login", map[string]string{"username": "cloud-user", "password": "G7!mQv#2xL9rT4pZ"}, nil)
	cookie := login.Result().Cookies()[0]
	if _, err := runtime.DB.W.Exec(`INSERT INTO device_tokens(user_id,name,platform,token_hash) VALUES(1,'Laptop','linux',?)`, passhash.DigestToken("nc_stream")); err != nil {
		t.Fatal(err)
	}
	// A stream that only ends when its request is cancelled.
	stream := runtime.SessionMiddleware(IdentityMiddleware(runtime.DB.W, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})))
	open := func(path string, authorize func(*http.Request)) chan struct{} {
		request := httptest.NewRequest(http.MethodGet, path, strings.NewReader(""))
		authorize(request)
		ended := make(chan struct{})
		go func() { stream.ServeHTTP(httptest.NewRecorder(), request); close(ended) }()
		return ended
	}
	browser := open("/api/sync/watch", func(r *http.Request) { r.AddCookie(cookie) })
	device := open("/api/handoff/events", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nc_stream") })
	stillOpen := func(name string, ended chan struct{}) {
		select {
		case <-ended:
			t.Fatalf("%s stream ended while signed in", name)
		case <-time.After(150 * time.Millisecond):
		}
	}
	mustEnd := func(name string, ended chan struct{}) {
		select {
		case <-ended:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s stream stayed open after revocation", name)
		}
	}
	stillOpen("browser", browser)
	stillOpen("device", device)
	if _, err := runtime.DB.W.Exec(`UPDATE device_tokens SET revoked_at=datetime('now')`); err != nil {
		t.Fatal(err)
	}
	mustEnd("device", device)
	stillOpen("browser", browser)
	if response := serveJSON(t, handler, http.MethodPost, "/api/auth/logout", nil, cookie); response.Code != http.StatusOK {
		t.Fatalf("logout = %d", response.Code)
	}
	mustEnd("browser", browser)
}
