package settingsui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/keylock"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

func startServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	cfg, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	engine := sync.New(cfg, db, nil)
	s, err := Start(engine, cfg, keylock.New(cfg, engine), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func browserClient(t *testing.T) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// status is the response's status code, or -1 when the request failed.
func status(res *http.Response, err error) int {
	if err != nil {
		return -1
	}
	res.Body.Close()
	return res.StatusCode
}

func apiGet(t *testing.T, client *http.Client, s *Server, header bool) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "http://"+s.host+"/api/state", nil)
	if header {
		req.Header.Set(apiHeader, "settings")
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestLaunchURLIsSpentOnceForASessionCookie(t *testing.T) {
	s := startServer(t)
	launch := s.LaunchURL(LaunchTTL)
	if !strings.Contains(launch, "/launch?code=") {
		t.Fatalf("launch URL %q", launch)
	}

	browser := browserClient(t)
	res, err := browser.Get(launch)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Request.URL.Path != "/" || res.Request.URL.RawQuery != "" {
		t.Fatalf("launch: %d at %s", res.StatusCode, res.Request.URL)
	}
	u, _ := url.Parse("http://" + s.host + "/")
	cookies := browser.Jar.Cookies(u)
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	if got := apiGet(t, browser, s, true); got != http.StatusOK {
		t.Fatalf("API with session: %d", got)
	}
	if got := apiGet(t, browser, s, false); got != http.StatusForbidden {
		t.Fatalf("API without the header: %d", got)
	}

	// The code is spent: a second browser (another user who read it) gets nothing.
	other := browserClient(t)
	res, err = other.Get(launch)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || len(other.Jar.Cookies(u)) != 0 {
		t.Fatalf("reused launch code: %d", res.StatusCode)
	}
	if got := apiGet(t, other, s, true); got != http.StatusForbidden {
		t.Fatalf("API without a session: %d", got)
	}
	// The page itself needs the session too.
	if got := status(other.Get("http://" + s.host + "/")); got != http.StatusForbidden {
		t.Fatalf("page without a session: %d", got)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	s := startServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(s.LaunchURL(LaunchTTL))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	header := res.Header.Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(header, want) {
			t.Errorf("Set-Cookie %q lacks %s", header, want)
		}
	}
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Errorf("launch answered %d → %q", res.StatusCode, res.Header.Get("Location"))
	}
}

func TestLaunchCodesExpireAndRejectBadRequests(t *testing.T) {
	s := startServer(t)
	expired := s.LaunchURL(time.Nanosecond)
	time.Sleep(time.Millisecond)
	for _, target := range []string{expired, "http://" + s.host + "/launch?code=", "http://" + s.host + "/launch?code=guess"} {
		res, err := browserClient(t).Get(target)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %d", target, res.StatusCode)
		}
	}

	// A session does not help from another site or another Host.
	browser := browserClient(t)
	res, err := browser.Get(s.LaunchURL(LaunchTTL))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	req, _ := http.NewRequest(http.MethodPost, "http://"+s.host+"/api/sync", nil)
	req.Header.Set(apiHeader, "x")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	if got := status(browser.Do(req)); got != http.StatusForbidden {
		t.Errorf("cross-site API call: %d", got)
	}
	req, _ = http.NewRequest(http.MethodGet, "http://"+s.host+"/api/state", nil)
	req.Host = "attacker.example:80"
	req.Header.Set(apiHeader, "x")
	if got := status(browser.Do(req)); got != http.StatusForbidden {
		t.Errorf("rebound Host: %d", got)
	}
}

func TestKeyLockEndpoints(t *testing.T) {
	s := startServer(t)
	call := func(client *http.Client, method, path, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(method, "http://"+s.host+path, strings.NewReader(body))
		req.Header.Set(apiHeader, "settings")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	if code, _ := call(browserClient(t), http.MethodPost, "/api/key-lock", `{}`); code != http.StatusForbidden {
		t.Errorf("enroll without a session: %d", code)
	}
	browser := browserClient(t)
	if got := status(browser.Get(s.LaunchURL(LaunchTTL))); got != http.StatusOK {
		t.Fatalf("launch: %d", got)
	}
	_, state := call(browser, http.MethodGet, "/api/state", "")
	lock, ok := state["keyLock"].(map[string]any)
	if !ok || lock["enrolled"] != false || lock["message"] == "" {
		t.Errorf("keyLock in state: %v", state["keyLock"])
	}
	if code, out := call(browser, http.MethodPost, "/api/key-lock", `{"pin":""}`); code != http.StatusBadRequest || out["error"] == "" {
		t.Errorf("enroll while signed out: %d %v", code, out)
	}
	if code, _ := call(browser, http.MethodDelete, "/api/key-lock", ""); code != http.StatusBadRequest {
		t.Errorf("remove while off: %d", code)
	}
}

func TestLaunchOpensSettingsWhenAsked(t *testing.T) {
	s := startServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for view, want := range map[string]string{"settings": "/#settings", "elsewhere": "/"} {
		res, err := client.Get(s.LaunchURL(LaunchTTL) + "&view=" + view)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Location"); got != want {
			t.Errorf("view=%s redirects to %q, want %q", view, got, want)
		}
	}
}

func TestPageServesItsFonts(t *testing.T) {
	s := startServer(t)
	browser := browserClient(t)
	page, err := browser.Get(s.LaunchURL(LaunchTTL))
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	if csp := page.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "font-src 'self'") {
		t.Errorf("CSP %q does not allow the page's fonts", csp)
	}
	for _, name := range []string{"geist.woff2", "geist-mono.woff2"} {
		res, err := browser.Get("http://" + s.host + "/assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "font/woff2" {
			t.Errorf("%s: %d %s", name, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
}

func TestReachErrorNamesTheServer(t *testing.T) {
	refused := &url.Error{Op: "Post", URL: "http://127.0.0.1:9/api/devices/authorize", Err: errors.New("connection refused")}
	got := reachError("http://127.0.0.1:9", fmt.Errorf("authorize: %w", refused))
	if got == nil || !strings.HasPrefix(got.Error(), "can't reach 127.0.0.1:9.") {
		t.Errorf("reachError = %v", got)
	}
	answer := errors.New("this server does not allow sign-ups")
	if got := reachError("http://127.0.0.1:9", answer); got != answer {
		t.Errorf("a server answer was rewritten: %v", got)
	}
}
