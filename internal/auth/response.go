package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/lockout"
)

type authError struct {
	status                    int
	message                   string
	code                      string
	requiresEmailConfirmation bool
}

func (e authError) Error() string { return e.message }

func writeAuthError(w http.ResponseWriter, err error) {
	var authErr authError
	if errors.As(err, &authErr) {
		body := map[string]any{"error": authErr.message, "code": authErr.code}
		if authErr.requiresEmailConfirmation {
			body["requiresEmailConfirmation"] = true
		}
		writeJSON(w, authErr.status, body)
		return
	}
	writeError(w, 500, "Could not create account", "INTERNAL_ERROR")
}

func writeSignInError(w http.ResponseWriter, err error) {
	var authErr authError
	if errors.As(err, &authErr) {
		body := map[string]any{"error": authErr.message, "code": authErr.code}
		if authErr.requiresEmailConfirmation {
			body["requiresEmailConfirmation"] = true
		}
		writeJSON(w, authErr.status, body)
		return
	}
	writeLockout(w, err)
}

// writeLockout answers a throttled sign-in. The shared lockout words its
// refusal for confirmations; a sign-in names the failed attempts instead.
func writeLockout(w http.ResponseWriter, err error) {
	var locked *lockout.LockedError
	if errors.As(err, &locked) {
		writeLocked(w, locked, strings.Replace(locked.Error(), "wrong passwords or codes", "failed sign-in attempts", 1))
		return
	}
	writeError(w, 500, "Sign-in failed", "INTERNAL_ERROR")
}

func writeLocked(w http.ResponseWriter, locked *lockout.LockedError, message string) {
	w.Header().Set("Retry-After", strconv.Itoa(locked.RetryAfter))
	writeJSON(w, 429, map[string]any{"error": message, "code": locked.Code(), "retryAfter": locked.RetryAfter})
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, map[string]any{"error": message, "code": code})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	// A bodyless request is an empty object, as Express reads it.
	if err := dec.Decode(target); errors.Is(err, io.EOF) {
		return true
	} else if err != nil {
		writeError(w, 400, "Invalid JSON request", "INVALID_JSON")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, 400, "Invalid JSON request", "INVALID_JSON")
		return false
	}
	return true
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// ParseOrigin is shared by the HTTP composition layer when applying account
// route transport checks.
func ParseOrigin(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid origin")
	}
	return u.Scheme + "://" + u.Host, nil
}
