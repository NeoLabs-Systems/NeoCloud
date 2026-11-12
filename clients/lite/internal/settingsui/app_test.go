package settingsui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// signedIn returns a browser with a session on s.
func signedIn(t *testing.T, s *Server) *http.Client {
	t.Helper()
	browser := browserClient(t)
	if got := status(browser.Get(s.LaunchURL(LaunchTTL))); got != http.StatusOK {
		t.Fatalf("launch: %d", got)
	}
	return browser
}

// callAPI sends an API request and decodes the JSON answer.
func callAPI(t *testing.T, client *http.Client, s *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+s.host+path, strings.NewReader(body))
	req.Header.Set(apiHeader, "neocloud")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// newEndpoints are the routes added for the app window; every one needs a
// session and the API header.
var newEndpoints = []struct{ method, path string }{
	{http.MethodPost, "/api/pick-folder"},
	{http.MethodGet, "/api/local/suggest"},
	{http.MethodPost, "/api/pairs/x/new-folders"},
	{http.MethodPost, "/api/pairs/x/resolve"},
	{http.MethodGet, "/api/remote/folders"},
	{http.MethodPost, "/api/remote/folders"},
	{http.MethodGet, "/api/account"},
	{http.MethodPost, "/api/open/logs"},
	{http.MethodPost, "/api/open/web"},
	{http.MethodPost, "/api/open/approval"},
	{http.MethodPost, "/api/pairs/x/open"},
	{http.MethodPost, "/api/pairs/x/cloudignore"},
	{http.MethodPost, "/api/pairs/x/sync"},
	{http.MethodDelete, "/api/login"},
	{http.MethodPost, "/api/login/key"},
	{http.MethodPost, "/api/login/key/code"},
	{http.MethodGet, "/api/window"},
}

func TestNewEndpointsNeedASessionAndTheHeader(t *testing.T) {
	s := startServer(t)
	s.run = func(context.Context, string, ...string) ([]byte, error) {
		t.Error("a dialog ran for an unauthenticated request")
		return nil, nil
	}
	s.open = func(string) error { t.Error("something was opened"); return nil }
	s.openText = s.open
	stranger := browserClient(t)
	browser := signedIn(t, s)
	for _, endpoint := range newEndpoints {
		req, _ := http.NewRequest(endpoint.method, "http://"+s.host+endpoint.path, strings.NewReader("{}"))
		req.Header.Set(apiHeader, "x")
		if got := status(stranger.Do(req)); got != http.StatusForbidden {
			t.Errorf("%s %s without a session: %d", endpoint.method, endpoint.path, got)
		}
		req, _ = http.NewRequest(endpoint.method, "http://"+s.host+endpoint.path, strings.NewReader("{}"))
		if got := status(browser.Do(req)); got != http.StatusForbidden {
			t.Errorf("%s %s without the header: %d", endpoint.method, endpoint.path, got)
		}
	}
	for _, asset := range []string{"/", "/assets/app.js", "/assets/app.css", "/assets/logo.svg"} {
		if got := status(stranger.Get("http://" + s.host + asset)); got != http.StatusForbidden {
			t.Errorf("%s without a session: %d", asset, got)
		}
	}
}

func TestPageRendersWithAStrictPolicy(t *testing.T) {
	s := startServer(t)
	browser := signedIn(t, s)
	res, err := browser.Get("http://" + s.host + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	csp := res.Header.Get("Content-Security-Policy")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(page), `src="/assets/app.js"`) || !strings.Contains(string(page), `id="app"`) {
		t.Fatalf("page: %d\n%s", res.StatusCode, page)
	}
	if strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP %q", csp)
	}
	for asset, want := range map[string]string{"app.js": "text/javascript", "app.css": "text/css", "logo.svg": "image/svg+xml"} {
		res, err := browser.Get("http://" + s.host + "/assets/" + asset)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), want) || len(body) == 0 {
			t.Errorf("%s: %d %q", asset, res.StatusCode, res.Header.Get("Content-Type"))
		}
		if res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s lacks nosniff", asset)
		}
	}
	if got := status(browser.Get("http://" + s.host + "/assets/index.html")); got != http.StatusNotFound {
		t.Errorf("unlisted asset: %d", got)
	}
	_, state := callAPI(t, browser, s, http.MethodGet, "/api/state", "")
	for _, key := range []string{"activity", "version", "home", "os", "superseded"} {
		if _, ok := state[key]; !ok {
			t.Errorf("state lacks %s", key)
		}
	}
}

func TestPickerCommandsPerOS(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, n := range names {
				if n == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	if got := pickerCommands("darwin", pickerTitle, has()); len(got) != 1 || got[0].name != "osascript" || !strings.Contains(strings.Join(got[0].args, " "), "POSIX path of (choose folder") {
		t.Errorf("darwin: %+v", got)
	}
	if got := pickerCommands("windows", pickerTitle, has()); len(got) != 1 || got[0].name != "powershell.exe" || !strings.Contains(got[0].args[len(got[0].args)-1], "FolderBrowserDialog") {
		t.Errorf("windows: %+v", got)
	}
	if got := pickerCommands("linux", pickerTitle, has("kdialog", "yad")); len(got) != 2 || got[0].name != "kdialog" || got[1].name != "yad" {
		t.Errorf("linux: %+v", got)
	}
	if got := pickerCommands("linux", pickerTitle, has()); len(got) != 0 {
		t.Errorf("linux without dialogs: %+v", got)
	}
}

func TestPickFolder(t *testing.T) {
	s := startServer(t)
	s.lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	browser := signedIn(t, s)
	want := pickerCommands(runtime.GOOS, pickerTitle, s.lookPath)[0].name

	var ran []string
	s.run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		ran = append(ran, name)
		return []byte("/Users/me/Documents/Work/\n"), nil
	}
	code, out := callAPI(t, browser, s, http.MethodPost, "/api/pick-folder", "")
	if code != http.StatusOK || out["path"] != filepath.Clean("/Users/me/Documents/Work") || len(ran) != 1 || ran[0] != want {
		t.Errorf("picked: %d %v ran %v", code, out, ran)
	}

	s.run = func(context.Context, string, ...string) ([]byte, error) { return nil, errPickerCancelled }
	if code, out := callAPI(t, browser, s, http.MethodPost, "/api/pick-folder", ""); code != http.StatusOK || out["cancelled"] != true {
		t.Errorf("cancelled: %d %v", code, out)
	}

	s.run = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("exec: not found") }
	if code, out := callAPI(t, browser, s, http.MethodPost, "/api/pick-folder", ""); code != http.StatusOK || out["unsupported"] != true {
		t.Errorf("no dialog: %d %v", code, out)
	}

	// One dialog at a time.
	release := make(chan struct{})
	started := make(chan struct{})
	s.run = func(context.Context, string, ...string) ([]byte, error) {
		close(started)
		<-release
		return []byte("/tmp"), nil
	}
	var wg gosync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		callAPI(t, browser, s, http.MethodPost, "/api/pick-folder", "")
	}()
	<-started
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pick-folder", ""); code != http.StatusBadRequest {
		t.Errorf("second dialog: %d", code)
	}
	close(release)
	wg.Wait()
}

// fakeNeoCloud answers the folder calls the browser needs.
func fakeNeoCloud(t *testing.T) (*httptest.Server, *[]string) {
	var created []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/folders/{id}/children", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer nc_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		root := map[string]string{"id": "r00t", "name": ""}
		docs := map[string]string{"id": "d0c5", "name": "Documents"}
		switch r.PathValue("id") {
		case "root", "r00t":
			json.NewEncoder(w).Encode(map[string]any{
				"folder": map[string]any{"id": "r00t", "type": "folder"},
				"path":   []any{root},
				"items": []any{
					map[string]any{"id": "d0c5", "name": "Documents", "type": "folder"},
					map[string]any{"id": "v4u1", "name": "Secrets", "type": "folder", "vaultId": "v1"},
					map[string]any{"id": "f1", "name": "notes.txt", "type": "file"},
				},
				"nextCursor": nil,
			})
		case "d0c5":
			json.NewEncoder(w).Encode(map[string]any{
				"folder": map[string]any{"id": "d0c5", "name": "Documents", "type": "folder"},
				"path":   []any{root, docs},
				"items":  []any{map[string]any{"id": "w0rk", "name": "Work", "type": "folder"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Not found", "code": "NOT_FOUND"})
		}
	})
	mux.HandleFunc("POST /api/folders", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		created = append(created, body["parentId"]+"/"+body["name"]+"/"+body["conflict"])
		json.NewEncoder(w).Encode(map[string]any{"id": "n3w", "name": body["name"], "type": "folder"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, &created
}

func signInTo(t *testing.T, s *Server, server string) {
	t.Helper()
	err := s.cfg.Update(func(c *config.Config) error {
		c.Server, c.Token, c.UserID, c.Username = server, "nc_test", 1, "neo"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoteFolders(t *testing.T) {
	s := startServer(t)
	browser := signedIn(t, s)
	if code, out := callAPI(t, browser, s, http.MethodGet, "/api/remote/folders", ""); code != http.StatusBadRequest || out["error"] == "" {
		t.Errorf("signed out: %d %v", code, out)
	}
	cloud, created := fakeNeoCloud(t)
	signInTo(t, s, cloud.URL)

	code, out := callAPI(t, browser, s, http.MethodGet, "/api/remote/folders", "")
	folders, _ := out["folders"].([]any)
	if code != http.StatusOK || len(folders) != 1 {
		t.Fatalf("root: %d %v", code, out)
	}
	if docs := folders[0].(map[string]any); docs["name"] != "Documents" || docs["path"] != "Documents" {
		t.Errorf("root folders: %v (vaults and files are left out)", folders)
	}
	if folder := out["folder"].(map[string]any); folder["path"] != "" {
		t.Errorf("root path: %v", folder)
	}

	_, out = callAPI(t, browser, s, http.MethodGet, "/api/remote/folders?id=d0c5", "")
	folder := out["folder"].(map[string]any)
	crumbs := out["crumbs"].([]any)
	work := out["folders"].([]any)[0].(map[string]any)
	if folder["path"] != "Documents" || len(crumbs) != 2 || work["path"] != "Documents/Work" {
		t.Errorf("Documents: %v", out)
	}
	if code, _ := callAPI(t, browser, s, http.MethodGet, "/api/remote/folders?id=nope", ""); code != http.StatusBadRequest {
		t.Errorf("unknown folder: %d", code)
	}

	code, out = callAPI(t, browser, s, http.MethodPost, "/api/remote/folders", `{"parentId":"d0c5","name":" Photos "}`)
	if code != http.StatusOK || out["id"] != "n3w" || len(*created) != 1 || (*created)[0] != "d0c5/Photos/replace" {
		t.Errorf("create: %d %v %v", code, out, *created)
	}
	for _, bad := range []string{`{"name":""}`, `{"name":"a/b"}`, `{"name":".."}`, `{"name":"a\\b"}`} {
		if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/remote/folders", bad); code != http.StatusBadRequest {
			t.Errorf("create %s: %d", bad, code)
		}
	}
	if len(*created) != 1 {
		t.Errorf("bad names reached the server: %v", *created)
	}
}

func TestOpenTargetsAndPairActions(t *testing.T) {
	s := startServer(t)
	var opened []string
	s.open = func(target string) error { opened = append(opened, target); return nil }
	browser := signedIn(t, s)

	for _, target := range []string{"approval", "web", "logs", "elsewhere"} {
		if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/open/"+target, ""); code != http.StatusBadRequest {
			t.Errorf("open %s with nothing to open: %d", target, code)
		}
	}
	logs := filepath.Join(t.TempDir(), "neocloud-lite.log")
	os.WriteFile(logs, nil, 0o600)
	s.opts.LogPath = logs
	signInTo(t, s, "https://cloud.example.com")
	callAPI(t, browser, s, http.MethodPost, "/api/open/logs", "")
	callAPI(t, browser, s, http.MethodPost, "/api/open/web", "")

	local := t.TempDir()
	pair, err := config.NewPair(local, "Documents", "both", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.Update(func(c *config.Config) error { return c.AddPair(pair) })
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs/"+pair.ID+"/open", ""); code != http.StatusOK {
		t.Errorf("open pair: %d", code)
	}
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs/"+pair.ID+"/sync", ""); code != http.StatusOK {
		t.Errorf("sync pair: %d", code)
	}
	for _, path := range []string{"/api/pairs/nope/open", "/api/pairs/nope/sync"} {
		if code, _ := callAPI(t, browser, s, http.MethodPost, path, ""); code != http.StatusBadRequest {
			t.Errorf("%s: %d", path, code)
		}
	}
	want := []string{logs, "https://cloud.example.com/app", pair.Local}
	if strings.Join(opened, "|") != strings.Join(want, "|") {
		t.Errorf("opened %v, want %v", opened, want)
	}
}

func TestOpenWindowSupersedesTheOlderOne(t *testing.T) {
	s := startServer(t)
	s.opts.WindowProfile = "/cfg/app-window"
	var urls []string
	s.openApp = func(url, profile string) (bool, error) {
		if profile != "/cfg/app-window" {
			t.Errorf("profile %q", profile)
		}
		urls = append(urls, url)
		return true, nil
	}
	if err := s.OpenWindow(); err != nil {
		t.Fatal(err)
	}
	s.OpenWindow() // a double click: swallowed
	if len(urls) != 1 || !strings.Contains(urls[0], "/launch?code=") {
		t.Fatalf("opened %v", urls)
	}

	// The launch code works on the first open.
	first := browserClient(t)
	if got := status(first.Get(urls[0])); got != http.StatusOK {
		t.Fatalf("first open: %d", got)
	}
	if _, state := callAPI(t, first, s, http.MethodGet, "/api/state", ""); state["superseded"] != false {
		t.Errorf("only window superseded: %v", state["superseded"])
	}
	second := signedIn(t, s)
	if _, state := callAPI(t, first, s, http.MethodGet, "/api/state", ""); state["superseded"] != true {
		t.Errorf("older window not superseded: %v", state["superseded"])
	}
	if _, state := callAPI(t, second, s, http.MethodGet, "/api/state", ""); state["superseded"] != false {
		t.Errorf("newest window superseded: %v", state["superseded"])
	}
}

// holdOpen starts a page's /api/window request; cancel is the window going away.
func holdOpen(t *testing.T, client *http.Client, s *Server) (cancel func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.host+"/api/window", nil)
	req.Header.Set(apiHeader, "neocloud")
	go client.Do(req)
	waitFor(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.windows > 0 })
	return cancel
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Closing the app window quits its browser (macOS keeps it in the Dock
// otherwise); reloading it does not.
func TestClosingTheWindowQuitsItsBrowser(t *testing.T) {
	defer func(old time.Duration) { closedGrace = old }(closedGrace)
	closedGrace = 200 * time.Millisecond
	s := startServer(t)
	s.opts.WindowProfile = "/cfg/app-window"
	var mu gosync.Mutex
	var closed []string
	s.closeApp = func(profile string) error {
		mu.Lock()
		defer mu.Unlock()
		closed = append(closed, profile)
		return nil
	}
	closedCount := func() int { mu.Lock(); defer mu.Unlock(); return len(closed) }
	page := signedIn(t, s)

	// A reload lets go and holds again within the grace period.
	cancel := holdOpen(t, page, s)
	cancel()
	cancel = holdOpen(t, page, s)
	time.Sleep(2 * closedGrace)
	if n := closedCount(); n != 0 {
		t.Fatalf("a reload quit the browser %d times", n)
	}

	cancel()
	waitFor(t, func() bool { return closedCount() == 1 })
	if closed[0] != "/cfg/app-window" {
		t.Errorf("closed profile %q", closed[0])
	}
}

func TestCancelLogin(t *testing.T) {
	s := startServer(t)
	browser := signedIn(t, s)
	s.mu.Lock()
	cancelled := false
	s.login = loginState{Pending: true, UserCode: "ABCD-EFGH", cancel: func() { cancelled = true }}
	s.mu.Unlock()
	if code, _ := callAPI(t, browser, s, http.MethodDelete, "/api/login", ""); code != http.StatusOK || !cancelled {
		t.Fatalf("cancel: %d %v", code, cancelled)
	}
	if _, state := callAPI(t, browser, s, http.MethodGet, "/api/state", ""); state["login"].(map[string]any)["pending"] != false {
		t.Errorf("still pending: %v", state["login"])
	}
}

// The cloud-first flow: the server proposes ~/NeoCloud, creates it with
// the pair, and refuses to create over an existing folder.
func TestCloudFirstPair(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	s := startServer(t)
	// Keep the test's folder out of this computer's real sidebar.
	s.cfg.Update(func(c *config.Config) error { c.FileManagerSidebar = new(false); return nil })
	browser := signedIn(t, s)

	code, out := callAPI(t, browser, s, http.MethodGet, "/api/local/suggest?remote=", "")
	want := filepath.Join(home, "NeoCloud")
	if code != http.StatusOK || out["path"] != want {
		t.Fatalf("suggest: %d %v", code, out)
	}
	body, _ := json.Marshal(map[string]any{"local": want, "remote": "", "create": true})
	if code, out := callAPI(t, browser, s, http.MethodPost, "/api/pairs", string(body)); code != http.StatusOK || out["created"] != true {
		t.Fatalf("add: %d %v", code, out)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs", string(body)); code != http.StatusBadRequest {
		t.Errorf("created over an existing folder: %d", code)
	}
	if _, out := callAPI(t, browser, s, http.MethodGet, "/api/local/suggest?remote=Photos", ""); out["path"] != filepath.Join(home, "NeoCloud Photos") {
		t.Errorf("suggest next to the synced NeoCloud folder: %v", out)
	}
	missing, _ := json.Marshal(map[string]any{"local": filepath.Join(home, "Elsewhere"), "remote": "x"})
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs", string(missing)); code != http.StatusBadRequest {
		t.Errorf("a missing folder was created without create: %d", code)
	}
	if _, err := os.Stat(filepath.Join(home, "Elsewhere")); err == nil {
		t.Error("Elsewhere was created")
	}
}

// Folder and app settings go through validation; the state shows the
// effective app settings.
func TestSettingsEndpoints(t *testing.T) {
	s := startServer(t)
	browser := signedIn(t, s)
	local := t.TempDir()
	pair, _ := config.NewPair(local, "Docs", "both", nil, false)
	s.cfg.Update(func(c *config.Config) error { return c.AddPair(pair) })

	good := `{"settings":{"excludePaths":["/Photos/"],"conflict":"newer","maxFileSizeMB":5,"propagateDeletes":false}}`
	if code, out := callAPI(t, browser, s, http.MethodPut, "/api/pairs/"+pair.ID, good); code != http.StatusOK {
		t.Fatalf("good settings: %d %v", code, out)
	}
	saved, _ := s.cfg.Get().Pair(pair.ID)
	if len(saved.ExcludePaths) != 1 || saved.ExcludePaths[0] != "Photos" || saved.Conflict != "newer" || saved.MirrorDeletes() {
		t.Errorf("saved %+v", saved.PairSettings)
	}
	for _, bad := range []string{`{"settings":{"mode":"on_demand"}}`, `{"settings":{"conflict":"coin"}}`, `{"settings":{"excludePaths":["../x"]}}`} {
		if code, _ := callAPI(t, browser, s, http.MethodPut, "/api/pairs/"+pair.ID, bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
	}
	if code, _ := callAPI(t, browser, s, http.MethodPut, "/api/settings", `{"global":{"proxy":"manual","proxyUrl":"nope"}}`); code != http.StatusBadRequest {
		t.Errorf("bad proxy accepted: %d", code)
	}
	if code, _ := callAPI(t, browser, s, http.MethodPut, "/api/settings", `{"global":{"notifications":"off","fileManagerSidebar":false}}`); code != http.StatusOK {
		t.Errorf("global settings: %d", code)
	}
	_, state := callAPI(t, browser, s, http.MethodGet, "/api/state", "")
	global := state["global"].(map[string]any)
	if global["notifications"] != "off" || global["deleteGuardFiles"] != float64(500) || global["proxy"] != "system" {
		t.Errorf("state global: %v", global)
	}
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs/"+pair.ID+"/resolve", `{"path":"a.txt","choice":"coin"}`); code != http.StatusBadRequest {
		t.Errorf("bad choice: %d", code)
	}
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs/"+pair.ID+"/new-folders", `{"path":"Videos","sync":false}`); code != http.StatusOK {
		t.Errorf("new folder: %d", code)
	}
	if saved, _ := s.cfg.Get().Pair(pair.ID); len(saved.ExcludePaths) != 2 {
		t.Errorf("declined folder not excluded: %v", saved.ExcludePaths)
	}
}

func TestOpenCloudIgnore(t *testing.T) {
	s := startServer(t)
	var opened []string
	s.openText = func(target string) error { opened = append(opened, target); return nil }
	browser := signedIn(t, s)
	local := t.TempDir()
	pair, err := config.NewPair(local, "Documents", "both", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.Update(func(c *config.Config) error { return c.AddPair(pair) })
	file := filepath.Join(local, ".cloudignore")

	_, state := callAPI(t, browser, s, http.MethodGet, "/api/state", "")
	if state["cloudIgnore"].(map[string]any)[pair.ID] != false {
		t.Errorf("cloudIgnore before = %v", state["cloudIgnore"])
	}
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs/"+pair.ID+"/cloudignore", ""); code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	data, err := os.ReadFile(file)
	if err != nil || !strings.HasPrefix(string(data), "# Files and folders NeoCloud does not sync") {
		t.Fatalf("template: %q %v", data, err)
	}
	_, state = callAPI(t, browser, s, http.MethodGet, "/api/state", "")
	if state["cloudIgnore"].(map[string]any)[pair.ID] != true {
		t.Errorf("cloudIgnore after = %v", state["cloudIgnore"])
	}

	// An existing file is opened as it is.
	os.WriteFile(file, []byte("*.log\n"), 0o644)
	callAPI(t, browser, s, http.MethodPost, "/api/pairs/"+pair.ID+"/cloudignore", "")
	if data, _ := os.ReadFile(file); string(data) != "*.log\n" {
		t.Errorf("existing file changed: %q", data)
	}
	if code, _ := callAPI(t, browser, s, http.MethodPost, "/api/pairs/nope/cloudignore", ""); code != http.StatusBadRequest {
		t.Errorf("unknown pair: %d", code)
	}
	if len(opened) != 2 || opened[0] != file || opened[1] != file {
		t.Errorf("opened %v", opened)
	}
}
