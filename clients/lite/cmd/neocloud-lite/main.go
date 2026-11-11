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

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

const usage = `neocloud-lite — NeoCloud sync client

Usage:
  neocloud-lite login --server URL [--no-browser]
  neocloud-lite logout
  neocloud-lite pair add <local> <remotePath> [--direction both|up|down] [--ignore GLOB]...
  neocloud-lite pair list
  neocloud-lite pair remove <id|local>
  neocloud-lite pair confirm-deletes <id>
  neocloud-lite sync --once
  neocloud-lite run [--headless]
  neocloud-lite status [--json]
  neocloud-lite autostart on|off

Config and state live in $NEOCLOUD_LITE_HOME or the OS config folder.
`

// The tray's event loop must own the main OS thread on macOS.
func init() { runtime.LockOSThread() }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	commands := map[string]func([]string) error{
		"login":     cmdLogin,
		"logout":    cmdLogout,
		"pair":      cmdPair,
		"sync":      cmdSync,
		"run":       cmdRun,
		"status":    cmdStatus,
		"autostart": cmdAutostart,
	}
	command, ok := commands[os.Args[1]]
	if !ok {
		if os.Args[1] != "help" && os.Args[1] != "-h" && os.Args[1] != "--help" {
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		}
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := command(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
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
	db, err := state.Open(dir)
	if err != nil {
		return nil, err
	}
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

// fileLogger writes to neocloud-lite.log in the config folder (the tray
// has no terminal), truncating it when it grows past a few MiB.
func fileLogger(dir string) *log.Logger {
	path := filepath.Join(dir, "neocloud-lite.log")
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
