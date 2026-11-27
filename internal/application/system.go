package application

import (
	"net/http"
	"os"
	goruntime "runtime"

	"github.com/NeoLabs-Systems/neocloud/internal/analytics"
	"github.com/NeoLabs-Systems/neocloud/internal/auth"
	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// systemRoutes are the unauthenticated endpoints load balancers, Docker, the
// CLI and the status page poll. They say nothing beyond ok or not ok per part.
func systemRoutes(mux *http.ServeMux, runtime *runtimeapp.Runtime) {
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := runtime.DB.W.PingContext(r.Context()); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		health := map[string]string{"status": "ok", "db": "ok", "storage": "ok"}
		if err := runtime.DB.W.PingContext(r.Context()); err != nil {
			health["db"] = "error"
		}
		for _, dir := range []string{runtime.Config.Paths.DataDir, runtime.Config.Paths.StorageDir} {
			if !writable(dir) {
				health["storage"] = "error"
			}
		}
		status := http.StatusOK
		if health["db"] != "ok" || health["storage"] != "ok" {
			health["status"], status = "degraded", http.StatusServiceUnavailable
		}
		httpx.WriteJSON(w, status, health)
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{
			"name": "NeoCloud", "product": "neocloud", "version": buildinfo.Version, "go": goruntime.Version(),
		})
	})
}

func writable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".neocloud-health-*")
	if err != nil {
		return false
	}
	closeErr := probe.Close()
	return os.Remove(probe.Name()) == nil && closeErr == nil
}

func analyticsOrigins(enabled bool) []string {
	if !enabled {
		return nil
	}
	return []string{analytics.Origin}
}

// signedInUser names the account of a request for the server log.
func signedInUser(r *http.Request) int64 {
	principal, _ := auth.IdentityFromContext(r.Context())
	return principal.UserID
}
