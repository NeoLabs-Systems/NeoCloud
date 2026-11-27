package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"index.html":                `<!doctype html><html><head><base href="/"></head><body>NeoCloud</body></html>`,
		"flutter_bootstrap.js":      "boot();",
		"flutter_service_worker.js": "worker();",
		"version.json":              `{"version":"test"}`,
		"assets/app.hash.js":        "asset();",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	landing := t.TempDir()
	for name, body := range map[string]string{
		"index.html":  "<html><head></head><body>landing</body></html>",
		"share.html":  "<html>share</html>",
		"status.html": "<html>status</html>",
		"404.html":    "<html>nothing here</html>",
		"css/a.css":   "body{}",
		".env":        "secret",
	} {
		path := filepath.Join(landing, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h, err := New(Options{WebDir: dir, LandingDir: landing})
	if err != nil {
		t.Fatal(err)
	}
	return h, dir
}

func serve(h *Handler, method, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.Routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestSPAFallbackAndCachePolicy(t *testing.T) {
	h, _ := testHandler(t)
	for _, path := range []string{"/app/", "/app/index.html", "/app/missing/route"} {
		w := serve(h, http.MethodGet, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<base href="/app/">`) {
			t.Fatalf("%s response=%d %q", path, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != noStore {
			t.Fatalf("%s Cache-Control=%q", path, got)
		}
	}
}

func TestStaticAssetsSetNoStoreOnlyForMutableFlutterFiles(t *testing.T) {
	h, _ := testHandler(t)
	for _, name := range []string{"flutter_bootstrap.js", "flutter_service_worker.js", "version.json"} {
		w := serve(h, http.MethodGet, "/app/"+name)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != noStore {
			t.Fatalf("%s response=%d headers=%v", name, w.Code, w.Header())
		}
	}
	w := serve(h, http.MethodGet, "/app/assets/app.hash.js")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") == noStore || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("hashed asset response=%d headers=%v", w.Code, w.Header())
	}
}

func TestSigningShellHasCredentialSafeHeadersAndLocalOnlyCSP(t *testing.T) {
	h, _ := testHandler(t)
	w := serve(h, http.MethodGet, "/app/sign/abc")
	if w.Code != http.StatusOK || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("signing response=%d headers=%v", w.Code, w.Header())
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, required := range []string{"default-src 'self'", "connect-src 'self' data: blob:", "font-src 'self' data:", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, required) {
			t.Errorf("signing CSP missing %q: %s", required, csp)
		}
	}
	if strings.Contains(csp, "gstatic") || strings.Contains(csp, "fonts.googleapis") {
		t.Fatalf("external asset origin in signing CSP: %s", csp)
	}
}

func TestAssetPathCannotEscapeRootAndMissingBuildIsUnavailable(t *testing.T) {
	h, dir := testHandler(t)
	outside := filepath.Join(filepath.Dir(dir), "outside-secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)
	w := serve(h, http.MethodGet, "/app/%2e%2e/outside-secret")
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("path escaped asset root: %q", w.Body.String())
	}
	missing, err := New(Options{WebDir: filepath.Join(t.TempDir(), "missing"), LandingDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	w = serve(missing, http.MethodGet, "/app/")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing build status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestHeadAppShellDoesNotReturnBody(t *testing.T) {
	h, _ := testHandler(t)
	w := serve(h, http.MethodHead, "/app/")
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestLandingShareStatusAndNotFoundPages(t *testing.T) {
	h, _ := testHandler(t)
	for path, want := range map[string]string{"/": "landing", "/status": "status", "/css/a.css": "body{}", "/s/token": "share", "/s/token/": "share"} {
		w := serve(h, http.MethodGet, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%s response=%d %q", path, w.Code, w.Body.String())
		}
	}
	share := serve(h, http.MethodGet, "/s/token")
	if share.Header().Get("Referrer-Policy") != "no-referrer" || share.Header().Get("Cache-Control") != noStore {
		t.Fatalf("share headers=%v", share.Header())
	}
	for path, accept := range map[string]string{"/nope": "text/html,*/*", "/.env": "text/html", "/../etc/passwd": "text/html"} {
		mux := http.NewServeMux()
		h.Routes(mux)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.URL.Path = path
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		h.site(w, r)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "nothing here") {
			t.Fatalf("%s response=%d %q", path, w.Code, w.Body.String())
		}
	}
	api := serve(h, http.MethodDelete, "/api/unknown")
	if api.Code != http.StatusNotFound || !strings.Contains(api.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("api response=%d %q", api.Code, api.Body.String())
	}
	if w := serve(h, http.MethodGet, "/_neolabs/analytics.js"); w.Code != http.StatusNotFound {
		t.Fatalf("analytics served while off: %d", w.Code)
	}
}

func TestAnalyticsOnLandingAndAppButNeverOnSigning(t *testing.T) {
	h, _ := testHandler(t)
	h.options.Analytics = true
	for path, want := range map[string]bool{"/": true, "/app/": true, "/app/sign/abc": false, "/s/token": false} {
		if got := strings.Contains(serve(h, http.MethodGet, path).Body.String(), "/_neolabs/analytics.js"); got != want {
			t.Errorf("%s analytics = %v", path, got)
		}
	}
}
