package httppolicy

import (
	"context"
	"net/http"
	"regexp"
	"sync/atomic"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// Requests that never end on their own.
var streamPaths = map[string]bool{"/api/sync/watch": true, "/api/handoff/events": true, "/socket.io/": true}

var davWritePath = regexp.MustCompile(`^(?:/dav/files/|/remote\.php/dav/files/|/remote\.php/webdav(?:/|$))`)

// Drain lets a server shut down: running uploads and downloads finish, new
// uploads are turned away, and event streams end.
type Drain struct {
	draining atomic.Bool
	context  context.Context
	cancel   context.CancelFunc
}

// NewDrain returns a drain that has not started.
func NewDrain() *Drain {
	ctx, cancel := context.WithCancel(context.Background())
	return &Drain{context: ctx, cancel: cancel}
}

// Start begins shutting down.
func (d *Drain) Start() {
	d.draining.Store(true)
	d.cancel()
}

// isUpload reports a request that starts writing a file.
func isUpload(r *http.Request) bool {
	if r.Method == http.MethodPut && davWritePath.MatchString(r.URL.Path) {
		return true
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		return false
	}
	if r.URL.Path == "/api/uploads" {
		return true
	}
	for _, pattern := range rawBodyPaths {
		if pattern.MatchString(r.URL.Path) {
			return true
		}
	}
	return false
}

// Handler applies the drain in front of next.
func (d *Drain) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.draining.Load() {
			w.Header().Set("Connection", "close")
			if isUpload(r) {
				w.Header().Set("Retry-After", "30")
				httpx.ErrorCode(w, http.StatusServiceUnavailable, "The server is restarting. Try again in a moment.", "SERVER_STOPPING")
				return
			}
		}
		if streamPaths[r.URL.Path] {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			stop := context.AfterFunc(d.context, cancel)
			defer stop()
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}
