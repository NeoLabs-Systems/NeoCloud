package settingsui

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/handoff"
	"github.com/neolabs/neocloud/clients/lite/internal/keylock"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

// fakeHandoff is just enough of the send protocol for the page's calls.
func fakeHandoff(t *testing.T, uploaded *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/handoff/devices", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"devices": []map[string]any{
			{"id": "d:1", "name": "Pixel 8", "kind": "phone", "platform": "android", "canReceive": true, "online": true},
			{"id": "d:2", "name": "This computer", "kind": "desktop", "canReceive": true, "online": true, "current": true},
			{"id": "d:3", "name": "Asleep", "kind": "phone", "canReceive": true, "online": false},
		}})
	})
	mux.HandleFunc("POST /api/handoff/offers", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": "o1", "status": "uploading", "files": []map[string]any{{"id": 7, "name": "a.txt", "size": 5}}})
	})
	mux.HandleFunc("PUT /api/handoff/offers/o1/files/7", func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		uploaded.Add(n)
		json.NewEncoder(w).Encode(map[string]any{"id": "o1", "status": "pending"})
	})
	mux.HandleFunc("GET /api/handoff/offers/o1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "o1", "status": "delivered"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func startSendServer(t *testing.T) (*Server, *atomic.Int64) {
	t.Helper()
	uploaded := &atomic.Int64{}
	remote := fakeHandoff(t, uploaded)
	home := t.TempDir()
	cfg, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Update(func(c *config.Config) error { c.Server, c.Token = remote.URL, "tok"; return nil })
	db, err := state.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	engine := sync.New(cfg, db, nil)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	sender := handoff.NewSender(ctx, cfg, log.New(io.Discard, "", 0))
	s, err := Start(engine, cfg, keylock.New(cfg, engine), Options{Sender: sender})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, uploaded
}

func TestSendDevicesListsOnlyOtherOnlineDevices(t *testing.T) {
	s, _ := startSendServer(t)
	browser := signedIn(t, s)
	code, out := callAPI(t, browser, s, "GET", "/api/send/devices", "")
	devices, _ := out["devices"].([]any)
	if code != 200 || len(devices) != 1 || devices[0].(map[string]any)["name"] != "Pixel 8" {
		t.Errorf("%d %v", code, out)
	}
}

func TestStateCarriesTheSendCardsData(t *testing.T) {
	s, _ := startSendServer(t)
	_, out := callAPI(t, signedIn(t, s), s, "GET", "/api/state", "")
	card, _ := out["send"].(map[string]any)
	if card["enabled"] != true || card["maxFiles"] != float64(handoff.MaxFiles) {
		t.Errorf("send: %v", card)
	}
	plain := startServer(t)
	_, out = callAPI(t, signedIn(t, plain), plain, "GET", "/api/state", "")
	if card, _ := out["send"].(map[string]any); card["enabled"] != false {
		t.Errorf("without a sender: %v", card)
	}
}

func TestInspectRefusesFoldersWithAClearMessage(t *testing.T) {
	s, _ := startSendServer(t)
	browser := signedIn(t, s)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	os.WriteFile(file, []byte("hello"), 0o600)
	body, _ := json.Marshal(map[string]any{"paths": []string{file}})
	code, out := callAPI(t, browser, s, "POST", "/api/send/inspect", string(body))
	files, _ := out["files"].([]any)
	if code != 200 || len(files) != 1 || files[0].(map[string]any)["size"] != float64(5) {
		t.Errorf("%d %v", code, out)
	}
	body, _ = json.Marshal(map[string]any{"paths": []string{dir}})
	code, out = callAPI(t, browser, s, "POST", "/api/send/inspect", string(body))
	if code != 400 || !strings.Contains(out["error"].(string), "is a folder") {
		t.Errorf("folder: %d %v", code, out)
	}
}

func TestDropIsStagedSentAndRemoved(t *testing.T) {
	s, uploaded := startSendServer(t)
	browser := signedIn(t, s)
	req, _ := http.NewRequest("PUT", "http://"+s.host+"/api/send/stage?name="+"..%2F..%2Fa.txt", strings.NewReader("hello"))
	req.Header.Set(apiHeader, "neocloud")
	res, err := browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var staged sendFile
	json.NewDecoder(res.Body).Decode(&staged)
	res.Body.Close()
	if staged.Name != "a.txt" || staged.Size != 5 || !s.isStaged(staged.Path) {
		t.Fatalf("staged: %+v", staged)
	}
	body, _ := json.Marshal(map[string]any{"deviceId": "d:1", "paths": []string{staged.Path}})
	if code, out := callAPI(t, browser, s, "POST", "/api/send", string(body)); code != 200 || out["id"] == "" {
		t.Fatalf("send: %d %v", code, out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(staged.Path); os.IsNotExist(err) && uploaded.Load() == 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if uploaded.Load() != 5 {
		t.Errorf("uploaded %d bytes", uploaded.Load())
	}
	if _, err := os.Stat(staged.Path); err == nil {
		t.Error("the staged copy stays after the send")
	}
}

func TestSendRejectsADeviceThatIsNotOnline(t *testing.T) {
	s, _ := startSendServer(t)
	file := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(file, []byte("x"), 0o600)
	for _, id := range []string{"d:3", "d:2", "nope"} {
		body, _ := json.Marshal(map[string]any{"deviceId": id, "paths": []string{file}})
		if code, out := callAPI(t, signedIn(t, s), s, "POST", "/api/send", string(body)); code != 400 || !strings.Contains(out["error"].(string), "not online") {
			t.Errorf("%s: %d %v", id, code, out)
		}
	}
}

func TestDiscardOnlyDeletesStagedFiles(t *testing.T) {
	s, _ := startSendServer(t)
	browser := signedIn(t, s)
	own := filepath.Join(t.TempDir(), "mine.txt")
	os.WriteFile(own, []byte("keep"), 0o600)
	body, _ := json.Marshal(map[string]any{"paths": []string{own, "/etc/hosts", filepath.Join(os.TempDir(), "..", "etc")}})
	callAPI(t, browser, s, "POST", "/api/send/discard", string(body))
	if _, err := os.Stat(own); err != nil {
		t.Error("a file that was not dropped was deleted")
	}
	if s.isStaged(own) || s.isStaged("/etc/hosts") {
		t.Error("only files under the staging folder count")
	}
}

func TestStageKeepsEveryNameInsideItsFolderAndCleansUpOnClose(t *testing.T) {
	s, _ := startSendServer(t)
	browser := signedIn(t, s)
	for _, name := range []string{"..", "", "a/../../b", `..\..\c`} {
		req, _ := http.NewRequest("PUT", "http://"+s.host+"/api/send/stage?name="+url.QueryEscape(name), strings.NewReader("x"))
		req.Header.Set(apiHeader, "neocloud")
		res, err := browser.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var staged sendFile
		json.NewDecoder(res.Body).Decode(&staged)
		res.Body.Close()
		if res.StatusCode != 200 || !s.isStaged(staged.Path) || strings.ContainsAny(staged.Name, `/\`) {
			t.Errorf("%q: %d %+v", name, res.StatusCode, staged)
		}
	}
	root, _ := s.stagingRoot()
	s.Close()
	if _, err := os.Stat(root); err == nil {
		t.Error("the staging folder outlived the server")
	}
}

func TestFilePickerCommands(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, n := range names {
				if n == name {
					return "/bin/" + n, nil
				}
			}
			return "", os.ErrNotExist
		}
	}
	mac := filePickerCommands("darwin", filesPickerTitle, has())
	if len(mac) != 1 || !strings.Contains(mac[0].args[1], "with multiple selections allowed") || !strings.Contains(mac[0].args[1], filesPickerTitle) {
		t.Errorf("mac: %+v", mac)
	}
	win := filePickerCommands("windows", filesPickerTitle, has())
	if len(win) != 1 || !strings.Contains(win[0].args[len(win[0].args)-1], "Multiselect = $true") {
		t.Errorf("windows: %+v", win)
	}
	linux := filePickerCommands("linux", filesPickerTitle, has("zenity", "kdialog"))
	if len(linux) != 2 || !strings.Contains(strings.Join(linux[0].args, " "), "--multiple") || !strings.Contains(strings.Join(linux[1].args, " "), "--separate-output") {
		t.Errorf("linux: %+v", linux)
	}
	if got := filePickerCommands("linux", filesPickerTitle, has()); len(got) != 0 {
		t.Errorf("none installed: %+v", got)
	}
}

func TestPickFilesSplitsLinesAndHandlesCancel(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b b.txt")
	os.WriteFile(a, []byte("1"), 0o600)
	os.WriteFile(b, []byte("22"), 0o600)
	s, _ := startSendServer(t)
	s.lookPath = func(string) (string, error) { return "/bin/x", nil }
	s.run = func(context.Context, string, ...string) ([]byte, error) { return []byte(a + "\r\n" + b + "\n\n"), nil }
	code, out := callAPI(t, signedIn(t, s), s, "POST", "/api/send/pick", "")
	files, _ := out["files"].([]any)
	if code != 200 || len(files) != 2 || files[1].(map[string]any)["name"] != "b b.txt" {
		t.Errorf("%d %v", code, out)
	}
	s.run = func(context.Context, string, ...string) ([]byte, error) { return nil, errPickerCancelled }
	if _, out := callAPI(t, signedIn(t, s), s, "POST", "/api/send/pick", ""); out["cancelled"] != true {
		t.Errorf("cancel: %v", out)
	}
}
