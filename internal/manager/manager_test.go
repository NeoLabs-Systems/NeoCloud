package manager

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neocloud/internal/updates"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/cli/service"
	"github.com/NeoLabs-Systems/neolabs-go/cli/update"
)

// releases is a fake GitHub serving signed runtime archives.
type releases struct {
	t       *testing.T
	server  *httptest.Server
	private string
	public  string
	mu      sync.Mutex
	files   map[string][]byte
	tags    []string
}

func newReleases(t *testing.T) *releases {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	r := &releases{t: t, private: base64.StdEncoding.EncodeToString(der), public: base64.StdEncoding.EncodeToString(public), files: map[string][]byte{}}
	r.server = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.server.Close)
	return r
}

func (r *releases) publish(version string) {
	r.t.Helper()
	stage, dist := r.t.TempDir(), r.t.TempDir()
	for name, body := range map[string]string{
		"app/bin/neocloud":                     "#!/bin/sh\necho neocloud " + version + "\n",
		"app/flutter_app/build/web/index.html": "web",
		"app/landing/index.html":               "landing",
	} {
		path := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			r.t.Fatal(err)
		}
	}
	platform, arch := update.Host()
	name := RuntimeAssetName(platform, arch, version)
	archive := filepath.Join(dist, name)
	if err := release.CreateZip(stage, archive); err != nil {
		r.t.Fatal(err)
	}
	digest, size, err := release.FileDigest(archive)
	if err != nil {
		r.t.Fatal(err)
	}
	manifest, err := release.NewRuntimeManifest(1, version, []release.Artifact{{Platform: platform, Architecture: arch, AssetName: name, SHA256: digest, SizeBytes: size}}, SupportedTargets())
	if err != nil {
		r.t.Fatal(err)
	}
	data, err := release.Serialize(manifest)
	if err != nil {
		r.t.Fatal(err)
	}
	signature, err := release.Sign(data, r.private)
	if err != nil {
		r.t.Fatal(err)
	}
	content, err := os.ReadFile(archive)
	if err != nil {
		r.t.Fatal(err)
	}
	manifestName := updates.ManifestPrefix + version + ".json"
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[name], r.files[manifestName], r.files[manifestName+".sig"] = content, data, []byte(signature)
	r.tags = append([]string{"v" + version}, r.tags...)
}

func (r *releases) describe(tag string) map[string]any {
	version := strings.TrimPrefix(tag, "v")
	var assets []map[string]string
	for name := range r.files {
		if strings.Contains(name, "-"+version+".") {
			assets = append(assets, map[string]string{"name": name, "browser_download_url": r.server.URL + "/files/" + name})
		}
	}
	return map[string]any{"tag_name": tag, "prerelease": strings.Contains(tag, "-"), "assets": assets}
}

func (r *releases) serve(w http.ResponseWriter, request *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name, ok := strings.CutPrefix(request.URL.Path, "/files/"); ok {
		_, _ = w.Write(r.files[name])
		return
	}
	if strings.HasSuffix(request.URL.Path, "/releases") {
		var all []any
		for _, tag := range r.tags {
			all = append(all, r.describe(tag))
		}
		_ = json.NewEncoder(w).Encode(all)
		return
	}
	if _, tag, ok := strings.Cut(request.URL.Path, "/releases/tags/"); ok {
		_ = json.NewEncoder(w).Encode(r.describe(tag))
		return
	}
	http.NotFound(w, request)
}

var versionInUnit = regexp.MustCompile(`versions/([^/"]+)/app/bin/neocloud`)

// machine is a fake systemd running whatever the unit file on disk says: the
// "server" answers health and reports the version of the program started.
type machine struct {
	t      *testing.T
	unit   string
	port   int
	mu     sync.Mutex
	server *http.Server
	// broken versions start but never answer; node is the Node.js server.
	broken map[string]bool
	starts []string
}

func (m *machine) Exists(name string) bool { return name == "systemctl" }

func (m *machine) Run(name string, args ...string) (string, error) {
	command := strings.Join(args, " ")
	switch {
	case strings.Contains(command, "is-active"):
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.server == nil && len(m.starts) == 0 {
			return "inactive", fmt.Errorf("inactive")
		}
		return "active\n", nil
	case strings.Contains(command, " stop"), strings.Contains(command, " disable"):
		m.stop()
	case strings.Contains(command, " restart"), strings.Contains(command, " start"):
		m.start()
	}
	return "", nil
}

func (m *machine) start() {
	m.stop()
	raw, err := os.ReadFile(m.unit)
	if err != nil {
		m.t.Fatal(err)
	}
	version := "node"
	if match := versionInUnit.FindStringSubmatch(string(raw)); match != nil {
		version = match[1]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts = append(m.starts, version)
	if m.broken[version] {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(m.port))
	if err != nil {
		m.t.Fatal(err)
	}
	m.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": version})
	})}
	go func(server *http.Server) { _ = server.Serve(listener) }(m.server)
}

func (m *machine) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
}

func (m *machine) running() string {
	body, ok := fetch(context.Background(), m.port, "/api/version")
	if !ok {
		return ""
	}
	version, _ := body["version"].(string)
	return version
}

type install struct {
	manager  *Manager
	machine  *machine
	releases *releases
	home     string
	env      map[string]string
	out      *bytes.Buffer
}

func newInstall(t *testing.T) *install {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	userHome := t.TempDir()
	home := filepath.Join(userHome, ".neocloud")
	env := map[string]string{"PORT": strconv.Itoa(port), "PATH": filepath.Join(userHome, "npm-bin"), "NEOCLOUD_BIN_DIR": filepath.Join(userHome, ".local", "bin")}
	releases := newReleases(t)
	fake := &machine{t: t, unit: filepath.Join(userHome, "neocloud.service"), port: port, broken: map[string]bool{}}
	t.Cleanup(fake.stop)
	out := &bytes.Buffer{}
	ui := cli.NewUI(false)
	ui.In, ui.Out, ui.Err = strings.NewReader(""), out, out
	m := &Manager{
		Getenv: func(key string) string { return env[key] }, UserHome: userHome, Version: "1.0.0", built: "1.0.0", Out: out, UI: ui,
		releases:  &release.Client{HTTP: releases.server.Client(), APIBase: releases.server.URL, AllowHTTP: true},
		publicKey: releases.public, startupTimeout: 3 * time.Second,
		newService: func(spec service.Spec) (*service.Manager, error) {
			return &service.Manager{Spec: spec, Platform: "linux", Runner: fake, UnitPath: fake.unit, UID: -1}, nil
		},
	}
	t.Setenv(updates.ChannelEnvKey, "")
	return &install{manager: m, machine: fake, releases: releases, home: home, env: env, out: out}
}

func (i *install) run(t *testing.T, args ...string) error {
	t.Helper()
	i.out.Reset()
	return i.manager.Run(context.Background(), args)
}

func (i *install) active(t *testing.T) string {
	t.Helper()
	_, version, err := activeRuntime(i.home)
	if err != nil {
		return ""
	}
	return version
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const nodeUnit = "[Service]\nExecStart=\"/usr/bin/node\" \"/home/u/NeoCloud/server/index.js\"\n"

// nodeInstall lays out what a running Node.js installation leaves: data under
// the master key, its service and the npm command link.
func (i *install) nodeInstall(t *testing.T) (envBefore, link, target string) {
	t.Helper()
	envBefore = "PORT=" + i.env["PORT"] + "\nNODE_ENV=production\nSESSION_SECRET=" + strings.Repeat("a", 64) +
		"\nNEOCLOUD_MASTER_KEY=" + strings.Repeat("b", 64) + "\n"
	if err := os.MkdirAll(i.home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(i.home, ".env"), []byte(envBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := runtimeapp.Open(context.Background(), i.manager.environment(), i.manager.UserHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO users(username,password) VALUES('ann','x')`); err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	envBefore = read(t, filepath.Join(i.home, ".env"))
	if err := os.WriteFile(i.machine.unit, []byte(nodeUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(i.manager.UserHome, "NeoCloud", "bin", "neocloud.js")
	link = filepath.Join(i.env["PATH"], "neocloud")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	i.machine.start()
	return envBefore, link, target
}

func TestNodeInstallIsUpgradedOrPutBack(t *testing.T) {
	i := newInstall(t)
	envBefore, link, target := i.nodeInstall(t)
	i.releases.publish("1.0.0")
	if i.machine.running() != "node" {
		t.Fatal("the Node.js server is not running")
	}
	for _, command := range [][]string{{"start"}, {"admin", "list"}, {"setup"}} {
		if err := i.run(t, command...); err != errUpgradeFirst {
			t.Fatalf("%v beside Node.js = %v", command, err)
		}
	}

	i.machine.broken["1.0.0"] = true
	if err := i.run(t, "install", "--channel", "stable"); err == nil {
		t.Fatalf("a server that never answers was accepted:\n%s", i.out)
	}
	if got := i.machine.running(); got != "node" {
		t.Fatalf("running after a failed upgrade = %q\n%s", got, i.out)
	}
	if read(t, i.machine.unit) != nodeUnit || read(t, filepath.Join(i.home, ".env")) != envBefore || i.active(t) != "" {
		t.Fatalf("the install was not put back: unit %q", read(t, i.machine.unit))
	}
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatalf("npm command = %q, %v", got, err)
	}

	i.machine.broken["1.0.0"] = false
	if err := i.run(t, "install", "--channel", "stable"); err != nil {
		t.Fatalf("%v\n%s", err, i.out)
	}
	if got := i.machine.running(); got != "1.0.0" {
		t.Fatalf("running = %q", got)
	}
	unit := read(t, i.machine.unit)
	for _, want := range []string{"NEOCLOUD_APP_DIR=", `"serve"`, "TimeoutStopSec=40", "NEOCLOUD_HOME="} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %s:\n%s", want, unit)
		}
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("the Node.js command survived")
	}
	env := read(t, filepath.Join(i.home, ".env"))
	for _, want := range []string{"NEOCLOUD_MASTER_KEY=" + strings.Repeat("b", 64), "NEOCLOUD_RELEASE_CHANNEL=stable"} {
		if !strings.Contains(env, want) {
			t.Errorf(".env lacks %s", want)
		}
	}
	if snapshots, _ := filepath.Glob(filepath.Join(i.home, "data", "backups", "pre-update-*.db.enc")); len(snapshots) == 0 {
		t.Error("no database snapshot was saved")
	}
	if err := i.run(t, "admin", "grant", "ann"); err != nil || !strings.Contains(i.out.String(), "already was an admin") {
		t.Fatalf("admin grant: %v\n%s", err, i.out)
	}
}

func TestUpdateRollsBackAndForward(t *testing.T) {
	i := newInstall(t)
	i.releases.publish("1.0.0")
	if err := i.run(t, "install", "--channel", "stable"); err != nil {
		t.Fatalf("%v\n%s", err, i.out)
	}
	launcher := filepath.Join(i.env["NEOCLOUD_BIN_DIR"], "neocloud")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := i.run(t, "rollback"); err == nil {
		t.Fatal("rolled back without an update")
	}

	i.releases.publish("1.1.0")
	i.machine.broken["1.1.0"] = true
	if err := i.run(t, "update"); !update.RolledBack(err) {
		t.Fatalf("broken update = %v\n%s", err, i.out)
	}
	if i.machine.running() != "1.0.0" || i.active(t) != "1.0.0" {
		t.Fatalf("after a broken update: running %q, active %q", i.machine.running(), i.active(t))
	}

	i.machine.broken["1.1.0"] = false
	cfg, err := i.manager.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	// The server asked for this update, so its progress is recorded.
	status := updates.StatusFile{Path: cfg.Paths.UpdateStatusFile}
	if err := status.Write(updates.Job{State: updates.StateRunning, RunnerPID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if err := i.run(t, "update"); err != nil {
		t.Fatalf("%v\n%s", err, i.out)
	}
	if i.machine.running() != "1.1.0" || !strings.Contains(read(t, launcher), "neocloud 1.1.0") {
		t.Fatalf("after update: running %q, command %q", i.machine.running(), read(t, launcher))
	}
	if job := status.Read(); job.State != updates.StateCompleted || *job.VersionAfter != "1.1.0" {
		t.Fatalf("job = %+v", job)
	}

	if err := i.run(t, "rollback"); err != nil {
		t.Fatalf("%v\n%s", err, i.out)
	}
	if i.machine.running() != "1.0.0" || !strings.Contains(i.out.String(), "schema unchanged") && i.active(t) != "1.0.0" {
		t.Fatalf("after rollback: %q\n%s", i.machine.running(), i.out)
	}
	if err := i.run(t, "rollback"); err == nil || !strings.Contains(err.Error(), "already rolled back") {
		t.Fatalf("second rollback = %v", err)
	}

	// A tag pins the install until a channel is named.
	if err := i.run(t, "update", "--tag", "v1.1.0"); err != nil {
		t.Fatalf("%v\n%s", err, i.out)
	}
	if err := i.run(t, "update"); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("update of a pinned install = %v", err)
	}
	if err := i.run(t, "update", "stable"); err != nil {
		t.Fatalf("%v\n%s", err, i.out)
	}
	if err := i.run(t, "update"); err != nil {
		t.Fatalf("update after leaving the pin: %v", err)
	}
}

func TestCommandsRefuseUnknownArgumentsAndProtectTheMasterKey(t *testing.T) {
	i := newInstall(t)
	for _, args := range [][]string{{"nope"}, {"stop", "now"}, {"update", "--force"}, {"channel", "nightly"}} {
		if err := i.run(t, args...); cli.CodeOf(err, "") != codeInput {
			t.Errorf("%v = %v", args, err)
		}
	}
	if err := i.run(t, "env", "set", "PUBLIC_URL", "https://cloud.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := i.run(t, "env", "set", masterKeyEnv, strings.Repeat("c", 64)); err == nil {
		t.Fatal("the master key was changed")
	}
	if err := i.run(t, "env"); err != nil {
		t.Fatal(err)
	}
	output := i.out.String()
	if !strings.Contains(output, "PUBLIC_URL=https://cloud.example.com") {
		t.Fatalf("env list:\n%s", output)
	}
	secret := regexp.MustCompile(`NEOCLOUD_MASTER_KEY=([0-9a-f]{64})`)
	if secret.MatchString(output) {
		t.Fatal("env list printed the master key")
	}
	if err := i.run(t, "keys", "export"); err != nil || !secret.MatchString(i.out.String()) {
		t.Fatalf("keys export: %v\n%s", err, i.out)
	}
	if err := i.run(t, "version"); err != nil || i.out.String() != "neocloud 1.0.0\n" {
		t.Fatalf("version = %q, %v", i.out, err)
	}
	if err := i.run(t, "help"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(i.out); err != nil {
		t.Fatal(err)
	}
}

func TestLocalBackupTargetFromTheCommandLine(t *testing.T) {
	i := newInstall(t)
	if err := i.run(t, "env", "list"); err != nil {
		t.Fatal(err)
	}
	runtime, err := runtimeapp.Open(context.Background(), i.manager.environment(), i.manager.UserHome)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	raw, _ := json.Marshal(map[string]string{"path": target})
	sealed, err := runtime.Keys.EncryptValue(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.W.Exec(`INSERT INTO backup_targets(name,type,config_enc) VALUES('disk','local',?)`, sealed); err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	for _, step := range []struct{ args, want string }{
		{"backup", "disk · manual · never run"},
		{"backup run 1", "new blob(s) uploaded"},
		{"backup verify 1", "database ok"},
		{"backup snapshots 1", "Snapshots on target #1"},
		{"backup snapshots --from " + target, "Snapshots on"},
		{"backup restore 1 latest", "Restored. Start NeoCloud again"},
		{"doctor", "Database quick_check ok"},
	} {
		err := i.run(t, strings.Fields(step.args)...)
		if err != nil && cli.CodeOf(err, "") != codeReported {
			t.Fatalf("%s: %v\n%s", step.args, err, i.out)
		}
		if !strings.Contains(i.out.String(), step.want) {
			t.Fatalf("%s lacks %q:\n%s", step.args, step.want, i.out)
		}
	}
}
