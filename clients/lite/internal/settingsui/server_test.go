package settingsui

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
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
	s, err := Start(sync.New(cfg, db, nil), cfg)
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
