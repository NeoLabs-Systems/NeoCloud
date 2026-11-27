package httppolicy

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const (
	jsonBodyLimit = 1 << 20
	// The agent API takes whole files as base64 JSON.
	agentBodyLimit = 8 << 20
	formBodyLimit  = 64 << 10
)

// Upload routes stream the raw body, whatever Content-Type the client sent.
var rawBodyPaths = []*regexp.Regexp{
	regexp.MustCompile(`^/api/upload$`),
	regexp.MustCompile(`^/api/uploads/[^/]+/parts/\d+$`),
	regexp.MustCompile(`^/api/handoff/offers/[^/]+/files/\d+$`),
	regexp.MustCompile(`^/api/public/shares/[^/]+/upload$`),
	regexp.MustCompile(`^/api/public/sign/[^/]+/fields/[^/]+/upload$`),
	regexp.MustCompile(`^/wopi/files/[^/]+/contents$`),
}

// BodyLimit caps the JSON and form bodies of every route that parses one.
func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := bodyLimit(r)
		if limit == 0 || r.Body == nil || r.ContentLength >= 0 && r.ContentLength <= limit {
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength < 0 {
			data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
			if err != nil {
				httpx.ErrorCode(w, http.StatusBadRequest, "The request body could not be read.", "INVALID_BODY")
				return
			}
			if int64(len(data)) <= limit {
				request := r.WithContext(r.Context())
				request.Body = io.NopCloser(bytes.NewReader(data))
				request.ContentLength = int64(len(data))
				next.ServeHTTP(w, request)
				return
			}
		}
		w.Header().Set("Connection", "close")
		httpx.ErrorCode(w, http.StatusRequestEntityTooLarge, "The request body is too large.", "BODY_TOO_LARGE")
	})
}

// bodyLimit is 0 for requests whose body is not buffered as JSON or a form.
func bodyLimit(r *http.Request) int64 {
	for _, pattern := range rawBodyPaths {
		if pattern.MatchString(r.URL.Path) {
			return 0
		}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return 0
	}
	switch {
	case mediaType == "application/x-www-form-urlencoded":
		return formBodyLimit
	case mediaType != "application/json":
		return 0
	case strings.HasPrefix(r.URL.Path, "/api/v1/"):
		return agentBodyLimit
	}
	return jsonBodyLimit
}
