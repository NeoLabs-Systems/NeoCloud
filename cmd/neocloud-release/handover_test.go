package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// handover is a checkout of this tree as the Node.js updater leaves it after
// `git pull`, with a fake install.sh recording how it was run.
type handover struct {
	t                *testing.T
	tree, home, node string
	ran, launcher    string
	environment      []string
}

func newHandover(t *testing.T, installStatus int) handover {
	t.Helper()
	node, errNode := exec.LookPath("node")
	if _, err := exec.LookPath("npm"); err != nil || errNode != nil || runtime.GOOS == "windows" {
		t.Skip("node, npm and a POSIX shell are needed to replay the old updater")
	}
	h := handover{t: t, tree: t.TempDir(), home: t.TempDir(), node: node}
	for _, rel := range []string{"package.json", "package-lock.json", ".npmrc", "scripts/node-handover.sh"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		h.write(filepath.Join(h.tree, rel), string(raw))
	}
	h.write(filepath.Join(h.tree, "node_modules", "kept", "package.json"), `{"name":"kept","version":"1.0.0"}`)
	// bin/neocloud.js runs `npm ci` the way lib/cli/upgrade.js did.
	h.write(filepath.Join(h.tree, "bin", "neocloud.js"), `#!/usr/bin/env node
const r = require('child_process').spawnSync('npm', ['ci', '--omit=dev', '--no-audit', '--no-fund'], { cwd: process.argv[process.argv.length - 1], encoding: 'utf8' });
process.stdout.write(String(r.status) + '\n' + r.stderr);
`)
	h.ran = filepath.Join(h.home, "installed")
	h.launcher = filepath.Join(h.home, ".local", "bin", "neocloud")
	// install.sh replaces the command the way the shared installer does.
	h.write(filepath.Join(h.tree, "install.sh"), "#!/bin/bash\n"+
		"rm -f '"+h.launcher+"'; echo go-binary > '"+h.launcher+"'\n"+
		`echo "channel=$NEOCLOUD_RELEASE_CHANNEL version=$NEOCLOUD_VERSION app=${NEOCLOUD_APP_DIR:-} data=${NEOCLOUD_DATA_DIR:-}" > '`+h.ran+".tmp'\n"+
		"mv '"+h.ran+".tmp' '"+h.ran+"'\n"+
		"exit "+strconv.Itoa(installStatus)+"\n")
	h.environment = append(os.Environ(), "HOME="+h.home, "NEOCLOUD_HOME="+filepath.Join(h.home, ".neocloud"),
		"NEOCLOUD_APP_DIR="+h.tree, "NEOCLOUD_BIN_DIR=", "NEOCLOUD_ENV_FILE=", "NEOCLOUD_DATA_DIR=",
		"npm_config_cache="+filepath.Join(h.home, ".npm"), "TMPDIR="+t.TempDir(),
		"PATH="+filepath.Dir(h.launcher)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return h
}

func (h handover) write(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h handover) git(args ...string) {
	command := exec.Command("git", append([]string{"-C", h.tree, "-c", "user.name=t", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	command.Env = h.environment
	if out, err := command.CombinedOutput(); err != nil {
		h.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// update runs the old updater and waits for install.sh.
func (h handover) update(updater []string) (install, log string) {
	h.t.Helper()
	command := exec.Command(updater[0], append(updater[1:], h.tree)...)
	command.Env = h.environment
	out, err := command.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "1\n") || !strings.Contains(string(out), "switch then finishes by itself") {
		h.t.Fatalf("updater run: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(h.tree, "node_modules", "kept", "package.json")); err != nil {
		h.t.Fatal("npm removed the old node_modules")
	}
	logFile := filepath.Join(h.home, ".neocloud", "data", "logs", "handover.log")
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(200 * time.Millisecond) {
		ran, _ := os.ReadFile(h.ran)
		logged, _ := os.ReadFile(logFile)
		work, _ := filepath.Glob(filepath.Join(h.env("TMPDIR"), "neocloud-handover.*"))
		if len(ran) > 0 && len(work) == 0 {
			return strings.TrimSpace(string(ran)), string(logged)
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("the handover did not finish; log: %s", logged)
		}
	}
}

func (h handover) env(key string) string {
	for i := len(h.environment) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(h.environment[i], key+"="); ok {
			return value
		}
	}
	return ""
}

func TestNodeUpdaterHandsOver(t *testing.T) {
	t.Run("beta branch", func(t *testing.T) {
		h := newHandover(t, 0)
		h.git("init", "-q", "-b", "beta")
		install, log := h.update([]string{h.node, filepath.Join(h.tree, "bin", "neocloud.js"), "update"})
		if install != "channel=beta version= app= data=" || !strings.Contains(log, "handing over to the Go binary") {
			t.Fatalf("install.sh ran with %q; log %s", install, log)
		}
	})
	t.Run("main branch with its own data folder", func(t *testing.T) {
		h := newHandover(t, 0)
		h.git("init", "-q", "-b", "main")
		data := filepath.Join(h.home, "elsewhere")
		h.write(filepath.Join(h.home, ".neocloud", ".env"), "NEOCLOUD_DATA_DIR="+data+"\n")
		// The Node.js CLI exported .env into what it started, but its launchd
		// service never read the directories from the file; systemd did.
		h.write(filepath.Join(h.home, "Library", "LaunchAgents", "com.neocloud.plist"), "<plist/>")
		h.environment = append(h.environment, "NEOCLOUD_DATA_DIR="+data)
		command := exec.Command(h.node, filepath.Join(h.tree, "bin", "neocloud.js"), "update", h.tree)
		command.Env = h.environment
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		want := "channel=stable version= app= data="
		if runtime.GOOS != "darwin" {
			want += data
		}
		for deadline := time.Now().Add(time.Minute); ; time.Sleep(200 * time.Millisecond) {
			if ran, _ := os.ReadFile(h.ran); len(ran) > 0 {
				if got := strings.TrimSpace(string(ran)); got != want {
					t.Fatalf("install.sh ran with %q, want %q", got, want)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the handover did not finish")
			}
		}
	})
	t.Run("pinned tag through the npm link, and a switch that fails", func(t *testing.T) {
		h := newHandover(t, 1)
		h.git("init", "-q", "-b", "main")
		h.git("add", "package.json")
		h.git("commit", "-q", "-m", "release")
		h.git("tag", "v9.9.9")
		h.git("checkout", "-q", "--detach", "v9.9.9")
		// npm link with ~/.local as its prefix puts the command where the Go
		// binary goes.
		if err := os.MkdirAll(filepath.Dir(h.launcher), 0o755); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(h.tree, "bin", "neocloud.js")
		if err := os.Symlink(script, h.launcher); err != nil {
			t.Fatal(err)
		}
		install, log := h.update([]string{h.node, h.launcher, "update", "--tag", "v9.9.9"})
		if install != "channel=stable version=v9.9.9 app= data=" || !strings.Contains(log, "keeps running on Node.js") {
			t.Fatalf("install.sh ran with %q; log %s", install, log)
		}
		if target, err := os.Readlink(h.launcher); err != nil || target != script {
			t.Fatalf("npm command after a failed switch = %q, %v", target, err)
		}
		if raw, err := os.ReadFile(script); err != nil || !strings.Contains(string(raw), "spawnSync") {
			t.Fatal("the failed switch wrote through the npm command into the checkout")
		}
	})
}

func TestDirectNpmInstallIsRefused(t *testing.T) {
	h := newHandover(t, 0)
	command := exec.Command("npm", "ci", "--no-audit", "--no-fund")
	command.Dir, command.Env = h.tree, h.environment
	if out, err := command.CombinedOutput(); err == nil || !strings.Contains(string(out), "no longer uses npm") {
		t.Fatalf("direct npm ci: %v\n%s", err, out)
	}
}
