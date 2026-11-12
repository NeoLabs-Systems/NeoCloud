//go:build e2e

// Package e2e runs the lite client against a real NeoCloud server spawned
// from this repository (node server/index.js) with a throwaway data folder.
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

const strongPassword = "Plum-Orbit-73!quartz" // same as test/helpers/app.js

// testUser is the throwaway account each test server is created with.
const testUser = "litetester"

// server is a running NeoCloud plus a signed-in browser-style session.
type server struct {
	url     string
	session *http.Client
	cmd     *exec.Cmd
	logs    *bytes.Buffer
	dir     string   // the repository root
	env     []string // the server's environment, kept for a restart
}

func repoRoot(t *testing.T) string {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "server", "index.js")); err != nil {
		t.Fatalf("NeoCloud repo not found at %s", root)
	}
	return root
}

func freePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func randomHex(n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// startServer spawns the server and registers the first (admin) account.
func startServer(t *testing.T) *server {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	home := t.TempDir()
	port := freePort(t)
	s := &server{
		url:  fmt.Sprintf("http://127.0.0.1:%d", port),
		logs: &bytes.Buffer{},
		dir:  repoRoot(t),
		env: append(os.Environ(),
			"NEOCLOUD_HOME="+home,
			fmt.Sprintf("PORT=%d", port),
			"NODE_ENV=development",
			"NEOCLOUD_MASTER_KEY="+randomHex(32),
			"SESSION_SECRET="+randomHex(32),
			"PUBLIC_URL=",
			"ALLOWED_ORIGINS=",
		),
	}
	t.Cleanup(func() {
		s.stop()
		if t.Failed() {
			t.Logf("server output:\n%s", s.logs.String())
		}
	})
	s.spawn(t)

	jar, _ := cookiejar.New(nil)
	s.session = &http.Client{Jar: jar, Timeout: 30 * time.Second}
	s.mustJSON(t, "POST", "/api/auth/register", map[string]string{
		"username": testUser, "email": testUser + "@example.com", "password": strongPassword,
	}, nil)
	return s
}

// spawn starts the server process and waits until it answers.
func (s *server) spawn(t *testing.T) {
	t.Helper()
	cmd := exec.Command("node", "server/index.js")
	cmd.Dir, cmd.Env = s.dir, s.env
	cmd.Stdout, cmd.Stderr = s.logs, s.logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s.cmd = cmd
	deadline := time.Now().Add(30 * time.Second)
	for {
		res, err := http.Get(s.url + "/api/auth/status")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v\n%s", err, s.logs.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// stop kills the server process (a crash: no shutdown handlers run).
func (s *server) stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
}

// sessionJSON calls the API with the browser session.
func (s *server) sessionJSON(method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		encoded, _ := json.Marshal(in)
		body = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, s.url+path, body)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.session.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	payload, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return res.StatusCode, fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, payload)
	}
	if out != nil {
		return res.StatusCode, json.Unmarshal(payload, out)
	}
	return res.StatusCode, nil
}

func (s *server) mustJSON(t *testing.T, method, path string, in, out any) {
	t.Helper()
	if _, err := s.sessionJSON(method, path, in, out); err != nil {
		t.Fatal(err)
	}
}

// approve is what the user does on /app/device in the web app.
func (s *server) approve(t *testing.T, userCode string) {
	t.Helper()
	var info map[string]any
	s.mustJSON(t, "GET", "/api/account/devices/authorize/"+userCode, nil, &info)
	if info["platform"] != "lite" {
		t.Errorf("approval page shows platform %v, want lite", info["platform"])
	}
	s.mustJSON(t, "POST", "/api/account/devices/authorize/"+userCode, map[string]bool{"approve": true}, nil)
}

// deviceLogin runs the full device-code flow and returns the token.
func (s *server) deviceLogin(t *testing.T, name string) *api.DeviceToken {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := api.New(s.url, "")
	auth, err := client.Authorize(ctx, name, "lite")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !strings.Contains(auth.VerificationURL, "/app/device?code="+auth.UserCode) {
		t.Errorf("verificationUrl = %q", auth.VerificationURL)
	}
	if pending, err := client.PollToken(ctx, auth.DeviceCode); pending != nil || err != nil {
		t.Fatalf("token before approval: %v %v", pending, err)
	}
	s.approve(t, auth.UserCode)
	token, err := client.WaitForToken(ctx, auth)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if !strings.HasPrefix(token.Token, "nc_") || token.User.Username != testUser {
		t.Fatalf("unexpected token answer: %+v", token)
	}
	// The code is spent once used.
	if _, err := client.PollToken(ctx, auth.DeviceCode); err == nil {
		t.Error("device code could be exchanged twice")
	}
	me, err := api.New(s.url, token.Token).Me(ctx)
	if err != nil || me.User.Username != testUser || me.Device.Name != name {
		t.Fatalf("devices/me: %+v %v", me, err)
	}
	return token
}

// mustDoErr calls the API with a device token outside the client package,
// for steps the client never does on its own (e.g. a half-finished upload).
func mustDoErr(client *api.Client, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, _ := json.Marshal(in)
		body = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, client.BaseURL+path, body)
	req.Header.Set("Authorization", "Bearer "+client.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	payload, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return &api.Error{Status: res.StatusCode, Message: string(payload)}
	}
	if out != nil {
		return json.Unmarshal(payload, out)
	}
	return nil
}

func mustDo(t *testing.T, client *api.Client, method, path string, in, out any) {
	t.Helper()
	if err := mustDoErr(client, method, path, in, out); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
}

func putPart(t *testing.T, client *api.Client, uploadID string, index int, data []byte) {
	t.Helper()
	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/uploads/%s/parts/%d", client.BaseURL, uploadID, index), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+client.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		t.Fatalf("part %d: %d", index, res.StatusCode)
	}
}
