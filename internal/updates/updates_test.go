package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
)

func releaseServer(t *testing.T, tags ...string) *release.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var releases []map[string]any
		for _, tag := range tags {
			name := ManifestPrefix + strings.TrimPrefix(tag, "v") + ".json"
			releases = append(releases, map[string]any{
				"tag_name": tag, "prerelease": strings.Contains(tag, "-"),
				"assets": []map[string]string{
					{"name": name, "browser_download_url": "http://example.test/m"},
					{"name": name + ".sig", "browser_download_url": "http://example.test/s"},
				},
			})
		}
		if strings.HasSuffix(r.URL.Path, "/releases") {
			_ = json.NewEncoder(w).Encode(releases)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return &release.Client{HTTP: server.Client(), APIBase: server.URL, AllowHTTP: true}
}

func testService(t *testing.T, env map[string]string, runner []string) (*Service, *http.ServeMux, *[]string) {
	t.Helper()
	dir := t.TempDir()
	service := New(Options{
		Version: "1.0.0", EnvFile: filepath.Join(dir, ".env"), StatusFile: filepath.Join(dir, "state", "update-status.json"),
		LogDir: filepath.Join(dir, "logs"), Getenv: func(key string) string { return env[key] },
		Releases: releaseServer(t, "v1.2.0-beta.0", "v1.1.0"), Runner: runner,
	})
	mux := http.NewServeMux()
	actions := []string{}
	service.Routes(mux, func(_ bool, next func(http.ResponseWriter, *http.Request, int64)) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Admin") == "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r, 7)
		})
	}, func(_ *http.Request, _ int64, action string, _ map[string]any) error {
		actions = append(actions, action)
		return nil
	})
	return service, mux, &actions
}

func call(t *testing.T, mux *http.ServeMux, method, path, body string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Admin", "1")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder.Code, decoded
}

func TestCheckAndChannelFollowTheSelectedReleases(t *testing.T) {
	_, mux, actions := testService(t, nil, nil)
	status, body := call(t, mux, http.MethodPost, "/api/admin/update/check", "")
	if status != 200 || body["channel"] != "stable" || body["available"].(map[string]any)["version"] != "1.1.0" {
		t.Fatalf("check: %d %v", status, body)
	}
	status, body = call(t, mux, http.MethodPut, "/api/admin/update/channel", `{"channel":"beta"}`)
	if status != 200 || body["channel"] != "beta" || body["available"].(map[string]any)["version"] != "1.2.0-beta.0" {
		t.Fatalf("channel: %d %v", status, body)
	}
	status, body = call(t, mux, http.MethodPut, "/api/admin/update/channel", `{"channel":"nightly"}`)
	if status != 400 || body["code"] != "INVALID_CHANNEL" {
		t.Fatalf("bad channel: %d %v", status, body)
	}
	if len(*actions) != 1 || (*actions)[0] != "server.update.channel" {
		t.Fatalf("audit = %v", *actions)
	}
}

func TestStartRunsTheRunnerOnceAndReportsADeadOne(t *testing.T) {
	out := filepath.Join(t.TempDir(), "args")
	service, mux, actions := testService(t, nil, []string{"/bin/sh", "-c", `echo "$0" > "` + out + `"; sleep 1`})
	status, body := call(t, mux, http.MethodPost, "/api/admin/update", "")
	if status != http.StatusAccepted || body["job"].(map[string]any)["state"] != "running" {
		t.Fatalf("start: %d %v", status, body)
	}
	status, body = call(t, mux, http.MethodPost, "/api/admin/update", "")
	if status != 409 || body["code"] != "UPDATE_RUNNING" {
		t.Fatalf("second start: %d %v", status, body)
	}
	if len(*actions) != 1 {
		t.Fatalf("audit = %v", *actions)
	}
	for deadline := time.Now().Add(10 * time.Second); service.Status().Job.State == StateRunning; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("runner never ended")
		}
	}
	if job := service.Status().Job; job.State != StateFailed || job.CompletedAt == nil {
		t.Fatalf("a silent runner = %+v", job)
	}
	if raw, err := os.ReadFile(out); err != nil || strings.TrimSpace(string(raw)) != "stable" {
		t.Fatalf("runner channel = %q, %v", raw, err)
	}
}

func TestContainerInstallsCannotUpdateThemselves(t *testing.T) {
	service, mux, _ := testService(t, map[string]string{ContainerEnvKey: "1"}, nil)
	status, body := call(t, mux, http.MethodPost, "/api/admin/update", "")
	if status != 409 || body["code"] != "UPDATE_CONTAINER" {
		t.Fatalf("start: %d %v", status, body)
	}
	if current := service.Check(context.Background()); current.SelfUpdate || current.BlockedReason == nil || *current.BlockedReason != "container" {
		t.Fatalf("status = %+v", current)
	}
}

func TestRecordOnlyFollowsAServerStartedUpdate(t *testing.T) {
	file := StatusFile{Path: filepath.Join(t.TempDir(), "status.json")}
	if err := file.Record(StateRunning, "checking", 10, "x", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file.Path); !os.IsNotExist(err) {
		t.Fatal("a terminal update wrote the status file")
	}
	if err := file.Write(Job{State: StateRunning, RunnerPID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if err := file.Record(StateCompleted, StateCompleted, 100, "done", "2.0.0", false); err != nil {
		t.Fatal(err)
	}
	if job := file.Read(); job.State != StateCompleted || *job.VersionAfter != "2.0.0" || len(job.Logs) != 1 {
		t.Fatalf("job = %+v", job)
	}
}
