package imports

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

func TestCancelAccountStopsOnlyThatAccountsRunningImports(t *testing.T) {
	h := &Handler{active: map[string]*run{}, db: accountTestDB(t)}
	start := func(id string, userID int64) context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		current := &run{userID: userID, cancel: cancel, finished: make(chan struct{})}
		h.active[id] = current
		go func() {
			<-ctx.Done()
			time.Sleep(20 * time.Millisecond)
			close(current.finished)
		}()
		return ctx
	}
	mine, other := start("mine", 1), start("other", 2)
	if err := h.CancelAccount(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.active["mine"].finished:
	default:
		t.Fatal("returned before the running import had stopped")
	}
	if mine.Err() == nil || other.Err() != nil {
		t.Fatalf("cancelled: mine=%v other=%v", mine.Err(), other.Err())
	}
}

func accountTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := dbx.Open(context.Background(), filepath.Join(t.TempDir(), "imports.db"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.W.Exec(`CREATE TABLE import_jobs(user_id INTEGER, status TEXT, password_enc TEXT)`); err != nil {
		t.Fatal(err)
	}
	return db.W
}
