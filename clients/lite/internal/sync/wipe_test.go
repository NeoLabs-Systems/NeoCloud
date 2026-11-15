package sync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// wipeServer answers every request like a server whose owner signed this
// device out remotely: DEVICE_WIPE_REQUESTED once, then a plain 401.
func wipeServer(t *testing.T, eraseFiles string) *httptest.Server {
	t.Helper()
	first := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if first {
			first = false
			w.Write([]byte(`{"error":"signed out","code":"DEVICE_WIPE_REQUESTED","eraseFiles":` + eraseFiles + `}`))
			return
		}
		w.Write([]byte(`{"error":"Unauthorized","code":"UNAUTHORIZED"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitSignedOut(t *testing.T, h *harness) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !h.engine.cfg.Get().LoggedIn() && !h.engine.wiping.Load() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the wipe did not finish")
}

func armWipe(h *harness, eraseFiles string) {
	srv := wipeServer(h.t, eraseFiles)
	h.engine.cfg.Update(func(c *config.Config) error { c.Server = srv.URL; return nil })
}

func TestRemoteWipeSignOutOnlyKeepsFiles(t *testing.T) {
	h := newHarness(t, newFakeServer(t), "both")
	h.write("keep.txt", "mine")
	armWipe(h, "false")
	h.sync()
	waitSignedOut(t, h)

	if h.read("keep.txt") != "mine" {
		t.Fatal("files were deleted although only a sign-out was asked for")
	}
	cfg := h.engine.cfg.Get()
	if cfg.Token != "" || cfg.Username != "" || len(cfg.Pairs) != 1 {
		t.Fatalf("token cleared, pairs kept expected, got %+v", cfg)
	}
	if st := h.engine.Status(); st.State != StateLoggedOut {
		t.Fatalf("status %+v", st)
	}
}

func TestRemoteWipeErasesOnlyTheSyncFolder(t *testing.T) {
	h := newHarness(t, newFakeServer(t), "both")
	h.write("a.txt", "a")
	h.write("deep/b.txt", "b")
	outside := filepath.Join(filepath.Dir(h.root), "outside-"+filepath.Base(h.root))
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	os.WriteFile(filepath.Join(outside, "precious.txt"), []byte("x"), 0o644)
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, h.path("link")); err != nil {
			t.Fatal(err)
		}
	}
	armWipe(h, "true")
	h.sync()
	waitSignedOut(t, h)

	entries, _ := os.ReadDir(h.root)
	if len(entries) != 0 {
		t.Fatalf("sync folder not emptied: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(outside, "precious.txt")); err != nil {
		t.Fatalf("a file outside the sync folder was touched: %v", err)
	}
	if cfg := h.engine.cfg.Get(); cfg.Token != "" || len(cfg.Pairs) != 0 {
		t.Fatalf("token and pairs should be gone: %+v", cfg)
	}
}

func TestRemoteWipeRunsOnce(t *testing.T) {
	h := newHarness(t, newFakeServer(t), "both")
	h.write("a.txt", "a")
	armWipe(h, "false")
	h.sync()
	waitSignedOut(t, h)
	if err := h.engine.WipeLocal(true); err != nil { // a repeat is harmless
		t.Fatal(err)
	}
}
