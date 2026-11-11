package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorizeKeepsOnlySafeVerificationLinks(t *testing.T) {
	var link string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"deviceCode": "d", "userCode": "ABCD-EFGH", "verificationUrl": link, "expiresIn": 600, "interval": 3})
	}))
	defer srv.Close()
	fallback := srv.URL + "/app/device?code=ABCD-EFGH"
	for given, want := range map[string]string{
		srv.URL + "/app/device?code=ABCD-EFGH":              srv.URL + "/app/device?code=ABCD-EFGH",
		"file:///System/Applications/Calculator.app":        fallback,
		`\\attacker\share\x.exe`:                            fallback,
		"smb://attacker/share":                              fallback,
		"javascript:alert(1)":                               fallback,
		"custom-scheme://launch":                            fallback,
		"https://evil.example/app/device?code=ABCD-EFGH":    fallback,
		"http://user@" + srv.Listener.Addr().String() + "/": fallback,
		"": fallback,
	} {
		link = given
		auth, err := New(srv.URL, "").Authorize(context.Background(), "dev", "lite")
		if err != nil {
			t.Fatal(err)
		}
		if auth.VerificationURL != want {
			t.Errorf("%q → %q, want %q", given, auth.VerificationURL, want)
		}
	}
}

func TestCleanServer(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://cloud.example.com/": true, " http://127.0.0.1:3344 ": true,
		"file:///etc": false, "cloud.example.com": false, "https://": false, "javascript:alert(1)": false,
	} {
		if _, err := CleanServer(raw); (err == nil) != ok {
			t.Errorf("CleanServer(%q): %v", raw, err)
		}
	}
	if got, _ := CleanServer(" https://cloud.example.com/ "); got != "https://cloud.example.com" {
		t.Errorf("trimmed to %q", got)
	}
}
