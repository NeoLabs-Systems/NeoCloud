package httppolicy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	editors := []string(nil)
	handler := SecurityHeaders(HeaderOptions{SecureCookies: true, HSTSMaxAge: 600, AllowExternal: true,
		EditorOrigins: func() []string { return editors }})(http.NotFoundHandler())
	get := func(path string) http.Header {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Header()
	}
	header := get("/app/")
	csp := header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "'wasm-unsafe-eval'", "https://www.gstatic.com", "frame-ancestors 'self'", "object-src 'none'", "frame-src 'self' blob: 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP is missing %q: %s", want, csp)
		}
	}
	if header.Get("Strict-Transport-Security") != "max-age=600" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", header)
	}
	editors = []string{"https://office.example.test"}
	if csp = get("/app/").Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://office.example.test") {
		t.Errorf("editor origin not allowed: %s", csp)
	}
	if got := get("/api/public/sign/token").Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("signing referrer policy = %q", got)
	}
}

func TestBodyLimit(t *testing.T) {
	handler := BodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		if n == 0 {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	serve := func(path, contentType string, size int, chunked bool) int {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Repeat("a", size)))
		request.Header.Set("Content-Type", contentType)
		if chunked {
			request.ContentLength = -1
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		return w.Code
	}
	cases := []struct {
		path, contentType string
		size              int
		chunked           bool
		want              int
	}{
		{"/api/auth/login", "application/json", 100, false, 200},
		{"/api/auth/login", "application/json", jsonBodyLimit + 1, false, 413},
		{"/api/auth/login", "application/json; charset=utf-8", jsonBodyLimit + 1, true, 413},
		{"/api/auth/login", "application/json", 100, true, 200},
		{"/api/v1/files", "application/json", jsonBodyLimit + 1, false, 200},
		{"/api/auth/password/reset", "application/x-www-form-urlencoded", formBodyLimit + 1, false, 413},
		{"/api/upload", "application/json", jsonBodyLimit + 1, false, 200},
		{"/api/uploads/u1/parts/2", "application/json", jsonBodyLimit + 1, true, 200},
		{"/dav/files/a", "application/octet-stream", jsonBodyLimit + 1, false, 200},
	}
	for _, tc := range cases {
		if got := serve(tc.path, tc.contentType, tc.size, tc.chunked); got != tc.want {
			t.Errorf("%s %s %d chunked=%v = %d, want %d", tc.path, tc.contentType, tc.size, tc.chunked, got, tc.want)
		}
	}
}
