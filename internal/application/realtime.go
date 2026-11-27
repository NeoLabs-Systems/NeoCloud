package application

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/realtime"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	syncapi "github.com/NeoLabs-Systems/neocloud/internal/sync"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
)

// newRealtime serves the web app's socket: browser sessions only, since signed-in
// apps poll the change journal and hold their own event streams.
func newRealtime(runtime *runtimeapp.Runtime, origins httpx.Origins) (*realtime.Server, error) {
	return realtime.New(realtime.Options{
		DB: runtime.DB.W,
		Session: func(r *http.Request) (realtime.Session, bool) {
			principal, ok := auth.IdentityFromContext(r.Context())
			session := sessionx.FromContext(r.Context())
			if !ok || principal.IsDevice || session == nil {
				return realtime.Session{}, false
			}
			return realtime.Session{
				UserID: principal.UserID, Hash: sessionx.HashID(runtime.Config.SessionSecret, session.ID()), UserAgent: r.UserAgent(),
			}, true
		},
		AllowOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			parsed, err := url.Parse(origin)
			return origins.Allowed(origin, true) || err == nil && strings.EqualFold(parsed.Host, r.Host)
		},
		Follow: func(ctx context.Context, userID int64) (realtime.Feed, error) {
			return syncapi.Follow(ctx, runtime.DB.W, userID)
		},
	})
}

// serveRealtime sends the socket past the API middleware, which would buffer
// and compress what it has to stream.
func serveRealtime(socket, api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/socket.io/") {
			socket.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}
