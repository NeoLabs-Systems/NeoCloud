package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// The server-sent event streams stay open for as long as the client likes, so
// the credential that opened one is checked while it runs: signing out,
// revoking a session or device, or disabling the account ends it, whichever
// process did it.

var eventStreamPaths = map[string]bool{"/api/sync/watch": true, "/api/handoff/events": true}

var streamCheckInterval = time.Second

// whileSignedIn cancels the request of an event stream once stillValid, a
// query returning one row for a live credential, finds none.
func whileSignedIn(db *sql.DB, r *http.Request, next func(*http.Request), stillValid string, args ...any) {
	if !eventStreamPaths[r.URL.Path] {
		next(r)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(streamCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			var one int
			err := db.QueryRowContext(ctx, stillValid, args...).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				cancel()
				return
			}
			if err != nil && ctx.Err() == nil {
				slog.Warn("could not check an open event stream's credential", "error", err)
			}
		}
	}()
	next(r.WithContext(ctx))
	cancel()
	<-done
}

const (
	liveSessionQuery = `SELECT 1 FROM sessions s JOIN users u ON u.id=? WHERE s.sid=? AND u.disabled_at IS NULL`
	liveDeviceQuery  = `SELECT 1 FROM device_tokens d JOIN users u ON u.id=d.user_id WHERE d.id=? AND d.revoked_at IS NULL AND u.disabled_at IS NULL`
)
