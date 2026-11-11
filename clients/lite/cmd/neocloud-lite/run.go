package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/settingsui"
	"github.com/neolabs/neocloud/clients/lite/internal/tray"
)

// cmdRun keeps syncing in the background: with the tray icon by default,
// or headless (servers, CI, a terminal) with the settings URL printed.
func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	headless := fs.Bool("headless", false, "no tray icon; log to stderr")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	logger := stderrLogger(false)
	if !*headless {
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		logger = fileLogger(dir)
	}
	a, err := openApp(logger)
	if err != nil {
		return err
	}
	defer a.close()

	settings, err := settingsui.Start(a.engine, a.cfg)
	if err != nil {
		return err
	}
	defer settings.Close()

	ctx, cancel := signalContext()
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.engine.Run(ctx) }()

	if *headless {
		// Printed once to the owner's terminal, not passed to another
		// process, so it stays valid until it is used.
		fmt.Fprintf(os.Stderr, "Settings (one-time link): %s\n", settings.LaunchURL(0))
		<-ctx.Done()
		return <-done
	}
	go func() {
		<-ctx.Done() // Ctrl-C in a terminal also ends the tray
		tray.Quit()
	}()
	tray.Run(tray.Options{
		Engine:      a.engine,
		Config:      a.cfg,
		SettingsURL: func() string { return settings.LaunchURL(settingsui.LaunchTTL) },
		OnQuit:      cancel,
	})
	cancel()
	return <-done
}
