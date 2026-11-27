// Command neocloud runs the NeoCloud server (`neocloud serve`) and manages the
// installation (`neocloud help`).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/analytics"
	"github.com/NeoLabs-Systems/neocloud/internal/application"
	"github.com/NeoLabs-Systems/neocloud/internal/buildinfo"
	"github.com/NeoLabs-Systems/neocloud/internal/config"
	"github.com/NeoLabs-Systems/neocloud/internal/httppolicy"
	"github.com/NeoLabs-Systems/neocloud/internal/loghistory"
	"github.com/NeoLabs-Systems/neocloud/internal/manager"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
)

func main() {
	args := os.Args[1:]
	// Files this process creates are private to its user.
	syscall.Umask(0o077)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := manager.Run(ctx, args, manager.Options{Serve: serve}); err != nil {
		manager.PrintError(os.Stderr, err, args)
		os.Exit(1)
	}
}

// serve runs the server until the context is cancelled, then lets running
// requests finish.
func serve(ctx context.Context) error {
	cfg, err := config.Load(os.Getenv, "")
	if err != nil {
		return err
	}
	if err := cfg.Paths.Ensure(); err != nil {
		return err
	}
	history, err := loghistory.Open(cfg.Paths.LogDir)
	if err != nil {
		return err
	}
	loghistory.SetDefault(history)
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(history.Writer(os.Stdout), &slog.HandlerOptions{Level: level})))

	releasePID, err := runtimeapp.AcquirePIDFile(cfg.Paths.PIDFile)
	if err != nil {
		return err
	}
	defer releasePID()
	// Listening first refuses a second server before it touches anything.
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Errorf("Port %d is already in use. Stop the running NeoCloud with `neocloud stop`, or set PORT in %s.", cfg.Port, cfg.Paths.EnvFile)
	}
	if err != nil {
		return err
	}
	defer listener.Close()
	app, err := application.Open(ctx, os.Getenv, "")
	if err != nil {
		return err
	}
	defer func() {
		if err := app.Close(); err != nil {
			slog.Error("close NeoCloud", "error", err)
		}
	}()
	slog.Info("Using env file: " + cfg.Paths.EnvFile)
	if cfg.TrustProxyUnset && cfg.TrustProxy != "false" {
		slog.Warn("TRUST_PROXY is unset, so proxies on any loopback, link-local or private address are trusted. " +
			"Set TRUST_PROXY to your reverse proxy's address (e.g. TRUST_PROXY=172.18.0.2) — see docs/operations.md.")
	}
	app.StartWorkers(ctx)

	drain := httppolicy.NewDrain()
	server := &http.Server{
		Handler:           drain.Handler(app.Handler()),
		ReadHeaderTimeout: time.Minute,
		// No read timeout: uploads and handoff payloads stream, and event
		// streams stay open. Route handlers bound their own bodies.
		IdleTimeout:    65 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	server.RegisterOnShutdown(drain.Start)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	address := cfg.PublicURL
	if address == "" {
		address = fmt.Sprintf("http://localhost:%d", cfg.Port)
	}
	slog.Info("NeoCloud running on "+address, "version", buildinfo.Version)
	if cfg.Analytics {
		go analytics.ReportHosts(ctx, buildinfo.Version)
	}

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	slog.Info(fmt.Sprintf("Finishing in-flight requests (up to %d s)...", int(manager.DrainTimeout/time.Second)))
	shutdown, cancel := context.WithTimeout(context.Background(), manager.DrainTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		slog.Warn("Grace period over; closing remaining connections.")
		_ = server.Close()
	}
	slog.Info("Stopped.")
	return nil
}
