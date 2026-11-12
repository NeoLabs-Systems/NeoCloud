package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/neolabs/neocloud/clients/lite/internal/autostart"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/instance"
	"github.com/neolabs/neocloud/clients/lite/internal/keylock"
	"github.com/neolabs/neocloud/clients/lite/internal/notify"
	"github.com/neolabs/neocloud/clients/lite/internal/settingsui"
	"github.com/neolabs/neocloud/clients/lite/internal/tray"
)

// cmdRun keeps syncing in the background: with the tray icon by default,
// or headless (servers, CI, a terminal) with the settings URL printed.
// The tray app runs once per config folder; starting it again opens the
// running copy's window.
func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	headless := fs.Bool("headless", false, "no tray icon; log to stderr")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	logger := stderrLogger(false)
	var single *instance.Instance
	if !*headless {
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		single, err = instance.Acquire(dir)
		if errors.Is(err, instance.ErrRunning) {
			return instance.Show(dir)
		}
		if err != nil {
			return err
		}
		defer single.Close()
		logger = fileLogger(dir)
	}
	a, err := openApp(logger)
	if err != nil {
		return err
	}
	defer a.close()
	if !*headless {
		exe, err := autostart.Executable()
		if err == nil {
			err = defaultAutostart(a.cfg, exe, autostart.Enable)
		}
		if err != nil {
			logger.Printf("turning on start at login: %v", err)
		}
	}

	// The security key lock pauses the engine while its key is away.
	lock := keylock.New(a.cfg, a.engine)
	lock.Logf = logger.Printf
	if *headless {
		lock.Prompt = func(message string) { fmt.Fprintln(os.Stderr, message) }
	}
	opts := settingsui.Options{WindowProfile: filepath.Join(a.dir, "app-window"), Logger: logger}
	if !*headless {
		opts.LogPath = filepath.Join(a.dir, logFileName)
	}
	settings, err := settingsui.Start(a.engine, a.cfg, lock, opts)
	if err != nil {
		return err
	}
	defer settings.Close()

	ctx, cancel := signalContext()
	defer cancel()
	go lock.Run(ctx)
	done := make(chan error, 1)
	go func() { done <- a.engine.Run(ctx) }()

	if *headless {
		// Printed once to the owner's terminal, not passed to another
		// process, so it stays valid until it is used.
		fmt.Fprintf(os.Stderr, "Settings (one-time link): %s\n", settings.LaunchURL(0))
		<-ctx.Done()
		return <-done
	}
	notify.Watch(a.engine, a.cfg, logger)
	go func() {
		<-ctx.Done() // Ctrl-C in a terminal also ends the tray
		tray.Quit()
	}()
	openWindow := func() {
		if err := settings.OpenWindow(); err != nil {
			logger.Printf("opening the app window: %v", err)
		}
	}
	openSettings := func() {
		if err := settings.OpenSettings(); err != nil {
			logger.Printf("opening the app window: %v", err)
		}
	}
	go single.Serve(openWindow)
	tray.Run(tray.Options{
		Engine:       a.engine,
		Config:       a.cfg,
		OpenWindow:   openWindow,
		OpenSettings: openSettings,
		OnReady: func() {
			// First run: nothing to sync until the user signs in.
			if !a.cfg.Get().LoggedIn() {
				openWindow()
			}
		},
		OnQuit: cancel,
	})
	cancel()
	settings.CloseWindow() // Quit leaves no window behind
	return <-done
}

// defaultAutostart turns on start at login the first time the tray app
// runs from where it is installed (not from a disk image or a `go run`
// build). From then on the setting is the user's, in Settings or with
// `autostart on|off`.
func defaultAutostart(cfg *config.Store, exe string, enable func() error) error {
	if cfg.Get().AutostartDefaulted || !autostart.Installed(exe) {
		return nil
	}
	if err := enable(); err != nil {
		return err
	}
	return cfg.Update(func(c *config.Config) error {
		c.AutostartDefaulted = true
		return nil
	})
}
