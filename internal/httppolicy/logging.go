package httppolicy

import (
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

var (
	// Share, signing and device links are credentials in themselves.
	secretPath = regexp.MustCompile(`(/(?:api/public/shares|api/public/sign|app/sign|s|devices/authorize)/)[^/]+`)
	// WebDAV URLs name files by path; only the mount is kept.
	davPath = regexp.MustCompile(`^((?:/remote\.php)?/dav/files/[^/]+|/remote\.php/webdav)/.+$`)
)

// LoggedPath is a request path as the log shows it: no query string (search
// terms and file names travel there) and no secrets or file names.
func LoggedPath(path string) string {
	return secretPath.ReplaceAllString(davPath.ReplaceAllString(path, "$1/[path]"), "${1}[redacted]")
}

// LogFailures records every refused or failed request. userID names the
// signed-in account, 0 for none.
func LogFailures(userID func(*http.Request) int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)
			if recorder.status < http.StatusBadRequest {
				return
			}
			level := slog.LevelWarn
			if recorder.status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			attrs := []any{"ip", r.RemoteAddr}
			if id := userID(r); id > 0 {
				attrs = append(attrs, "userId", id)
			}
			slog.Log(r.Context(), level, "[HTTP] "+r.Method+" "+LoggedPath(r.URL.Path)+" completed "+
				strconv.Itoa(recorder.status)+" in "+strconv.FormatInt(time.Since(started).Milliseconds(), 10)+"ms", attrs...)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 && status >= http.StatusOK {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Flush() { _ = http.NewResponseController(w.ResponseWriter).Flush() }

// Unwrap lets http.ResponseController reach deadlines and hijacking.
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
