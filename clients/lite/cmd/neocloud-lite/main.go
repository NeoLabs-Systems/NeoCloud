// Command neocloud-lite is the NeoCloud lightweight sync client: a tray
// icon plus a scriptable CLI around the same sync engine.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/keylock"
	"github.com/neolabs/neocloud/clients/lite/internal/proxy"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

const usage = `neocloud-lite — NeoCloud sync client

Usage:
  neocloud-lite                        the tray app (what a double click starts)
  neocloud-lite login --server URL [--no-browser]
  neocloud-lite logout
  neocloud-lite pair add <local> <remotePath> [--direction both|up|down] [--ignore GLOB]... [--create]
  neocloud-lite pair list
  neocloud-lite pair remove <id|local>
  neocloud-lite pair confirm-deletes <id>
  neocloud-lite sync --once
  neocloud-lite run [--headless]
  neocloud-lite status [--json]
  neocloud-lite check-ignore <path>...
  neocloud-lite autostart on|off
  neocloud-lite key enroll|status|remove

key enroll turns on the security key lock: the device token is stored
encrypted and NeoCloud Lite syncs only while that key is plugged in. Quit
the tray app first, or use its app window. macOS and Linux only.

check-ignore says whether each path is ignored and by which rule (the
folder's ignore patterns, the defaults or a .cloudignore on this computer).

Config and state live in $NEOCLOUD_LITE_HOME or the OS config folder.
`

// The tray's event loop must own the main OS thread on macOS.
func init() { runtime.LockOSThread() }

func main() {
	args := launchArgs(os.Args[1:])
	if len(args) == 0 {
		// Opened from Finder, Explorer, the Start menu or a desktop entry.
		args = []string{"run"}
	} else {
		attachConsole()
	}
	commands := map[string]func([]string) error{
		"login":        cmdLogin,
		"logout":       cmdLogout,
		"pair":         cmdPair,
		"sync":         cmdSync,
		"run":          cmdRun,
		"status":       cmdStatus,
		"check-ignore": cmdCheckIgnore,
		"autostart":    cmdAutostart,
		"key":          cmdKey,
	}
	command, ok := commands[args[0]]
	if !ok {
		if args[0] != "help" && args[0] != "-h" && args[0] != "--help" {
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		}
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := command(args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// launchArgs drops the -psn_… argument older macOS versions pass to an
// app opened from Finder.
func launchArgs(args []string) []string {
	var kept []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-psn_") {
			kept = append(kept, arg)
		}
	}
	return kept
}

// app is what every command needs: config, state and the engine.
type app struct {
	dir    string
	cfg    *config.Store
	db     *state.DB
	engine *sync.Engine
}

func openApp(logger *log.Logger) (*app, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Open(dir)
	if err != nil {
		return nil, err
	}
	if keylock.IgnoreIfUnsupported(cfg) {
		logger.Printf("ignoring the security key lock in %s: it is not available on this platform; sign in again", cfg.Path())
	}
	db, err := state.Open(dir)
	if err != nil {
		return nil, err
	}
	if aside := db.Recovered(); aside != "" {
		logger.Printf("the sync state was damaged and was moved to %s; every folder is merged again without deleting anything", aside)
	}
	api.SetProxy(proxy.For(cfg.Get().GlobalSettings))
	return &app{dir: dir, cfg: cfg, db: db, engine: sync.New(cfg, db, logger)}, nil
}

func (a *app) close() { a.db.Close() }

// parseInterspersed lets flags follow positional arguments
// (`pair add ~/Docs /Docs --direction up`), which Go's flag package
// otherwise stops at.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

// signalContext ends on Ctrl-C or SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// stderrLogger is for interactive commands; quiet drops the per-file lines.
func stderrLogger(quiet bool) *log.Logger {
	if quiet {
		return log.New(io.Discard, "", 0)
	}
	return log.New(os.Stderr, "", log.LstdFlags)
}

// logFileName is the tray app's log in the config folder.
const logFileName = "neocloud-lite.log"

// fileLogger writes to neocloud-lite.log in the config folder (the tray
// has no terminal), truncating it when it grows past a few MiB.
func fileLogger(dir string) *log.Logger {
	os.MkdirAll(dir, 0o700) // first run: the config folder may not exist yet
	path := filepath.Join(dir, logFileName)
	if info, err := os.Stat(path); err == nil && info.Size() > 4<<20 {
		os.Remove(path)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(file, "", log.LstdFlags)
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

var errUsage = errors.New("wrong arguments; see `neocloud-lite help`")
