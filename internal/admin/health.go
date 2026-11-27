package admin

import (
	"net/http"
	"runtime"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"golang.org/x/sys/unix"
)

// DiskSpace is the size and free room of the filesystem holding directory, or
// nil when it cannot be asked.
func DiskSpace(directory string) any {
	var stats unix.Statfs_t
	if unix.Statfs(directory, &stats) != nil {
		return nil
	}
	size := uint64(stats.Bsize)
	return map[string]uint64{"totalBytes": uint64(stats.Blocks) * size, "freeBytes": uint64(stats.Bavail) * size}
}

// adminHealth reports database totals and what the running server knows about
// itself; the database check and blob scrub run on the maintenance timer.
func (h *Handler) adminHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	var users, files, usedBytes, activeUploads int64
	err := h.db.QueryRowContext(r.Context(), `SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM nodes WHERE type='file'),
		(SELECT COALESCE(SUM(used_bytes),0) FROM users),
		(SELECT COUNT(*) FROM upload_sessions)`).Scan(&users, &files, &usedBytes, &activeUploads)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Could not read server health.", "HEALTH_UNAVAILABLE")
		return
	}
	payload := map[string]any{
		"database": "reachable", "databaseConnection": "reachable", "databaseCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"users": users, "files": files, "usedBytes": usedBytes, "activeUploads": activeUploads,
	}
	if h.onHealthMetadata != nil {
		for key, value := range h.onHealthMetadata() {
			payload[key] = value
		}
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) adminVersion(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"name": "NeoCloud", "product": "neocloud", "version": buildinfo.Version, "go": runtime.Version(),
	})
}
