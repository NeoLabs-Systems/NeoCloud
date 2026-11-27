package updates

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neolabs-go/cli/release"
	"github.com/NeoLabs-Systems/neolabs-go/runtimecfg"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

const checkTimeout = 20 * time.Second

var (
	errContainer = errors.New("NeoCloud runs from a container image. Pull the new image and recreate the container.")
	errRunning   = errors.New("An update is already running.")
	errChannel   = errors.New(`The release channel must be "stable" or "beta".`)
)

// Available is a newer release waiting on the channel.
type Available struct {
	Version string   `json:"version"`
	Branch  string   `json:"branch"`
	Changes []string `json:"changes"`
}

// Status is the body of every /api/admin/update response.
type Status struct {
	Version       string     `json:"version"`
	Channel       string     `json:"channel"`
	ChannelLabel  string     `json:"channelLabel"`
	SelfUpdate    bool       `json:"selfUpdate"`
	BlockedReason *string    `json:"blockedReason"`
	CheckedAt     *string    `json:"checkedAt"`
	CheckFailed   bool       `json:"checkFailed"`
	Available     *Available `json:"available"`
	Job           Job        `json:"job"`
}

// Options configure the update service of one install.
type Options struct {
	Version    string
	EnvFile    string
	StatusFile string
	LogDir     string
	// Getenv defaults to the process environment.
	Getenv func(string) string
	// Releases reads GitHub releases; nil uses the public API.
	Releases *release.Client
	// Runner is the detached update command; nil is this binary's `update`. The
	// channel is appended to it.
	Runner []string
}

// Service checks for newer releases and hands the update to a detached runner.
type Service struct {
	options Options

	// start serializes starting an update and changing the channel.
	start sync.Mutex
	mu    sync.Mutex
	last  struct {
		channel   string
		checkedAt *string
		failed    bool
		available *Available
	}
	channel string
}

// New returns the update service.
func New(options Options) *Service {
	if options.Getenv == nil {
		options.Getenv = os.Getenv
	}
	if options.Releases == nil {
		options.Releases = &release.Client{HTTP: ssrf.NewClient(ssrf.Options{Timeout: checkTimeout}), UserAgent: "NeoCloud " + options.Version}
	}
	return &Service{options: options}
}

func (s *Service) currentChannel() string {
	s.mu.Lock()
	chosen := s.channel
	s.mu.Unlock()
	if chosen != "" {
		return chosen
	}
	return Channel(s.options.Getenv, s.options.EnvFile)
}

func (s *Service) statusFile() StatusFile { return StatusFile{Path: s.options.StatusFile} }

// Status reports the update state.
func (s *Service) Status() Status {
	channel := s.currentChannel()
	status := Status{
		Version: s.options.Version, Channel: channel, ChannelLabel: release.ChannelLabel(channel),
		SelfUpdate: true, Job: s.statusFile().Read(),
	}
	if InContainer(s.options.Getenv) {
		reason := "container"
		status.SelfUpdate, status.BlockedReason = false, &reason
	}
	status.Job.RunnerPID = 0
	s.mu.Lock()
	defer s.mu.Unlock()
	// A check of another channel says nothing about this one.
	if s.last.channel == channel {
		status.CheckedAt, status.CheckFailed, status.Available = s.last.checkedAt, s.last.failed, s.last.available
	}
	return status
}

// Check asks the channel for a newer release. A check that fails keeps the last
// good answer.
func (s *Service) Check(ctx context.Context) Status {
	if InContainer(s.options.Getenv) {
		return s.Status()
	}
	channel := s.currentChannel()
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	latest, err := Latest(ctx, s.options.Releases, channel)
	// A channel with nothing published yet is an answer, not a failed check.
	failed := err != nil && cli.CodeOf(err, "") != "SETUP_RUNTIME_NOT_PUBLISHED"
	if failed {
		slog.Warn("The update check failed", "error", err)
	}
	var available *Available
	if err == nil && release.CompareVersions(latest, s.options.Version) > 0 {
		branch := "main"
		if channel == release.Beta {
			branch = "beta"
		}
		available = &Available{Version: latest, Branch: branch, Changes: []string{}}
	}
	now := timestamp()
	s.mu.Lock()
	if !failed || s.last.channel != channel || s.last.failed {
		s.last.channel, s.last.checkedAt, s.last.failed, s.last.available = channel, &now, failed, available
	}
	s.mu.Unlock()
	return s.Status()
}

func (s *Service) assertIdle() error {
	if InContainer(s.options.Getenv) {
		return errContainer
	}
	if s.statusFile().Read().State == StateRunning {
		return errRunning
	}
	return nil
}

// Start hands the update to a detached runner.
func (s *Service) Start() (Status, error) {
	s.start.Lock()
	defer s.start.Unlock()
	if err := s.assertIdle(); err != nil {
		return Status{}, err
	}
	channel := s.currentChannel()
	command := s.options.Runner
	if len(command) == 0 {
		executable, err := os.Executable()
		if err != nil {
			return Status{}, err
		}
		command = []string{executable, "update"}
	}
	command = append(append([]string{}, command...), channel)
	// A runner in the service's own cgroup would be stopped by the service stop
	// the update needs.
	if runtime.GOOS == "linux" && s.options.Getenv("INVOCATION_ID") != "" {
		command = append([]string{"systemd-run", "--user", "--scope", "--quiet", "--collect"}, command...)
	}
	if err := runtimecfg.EnsurePrivateDir(s.options.LogDir); err != nil {
		return Status{}, err
	}
	output, err := os.OpenFile(filepath.Join(s.options.LogDir, "update.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Status{}, fmt.Errorf("updates: open update log: %w", err)
	}
	defer output.Close()
	started, before := timestamp(), s.options.Version
	job := Job{
		State: StateRunning, Phase: "starting", Progress: 1, Message: "Starting the update",
		StartedAt: &started, VersionBefore: &before, RunnerPID: os.Getpid(),
	}
	s.mu.Lock()
	if s.last.channel == channel && s.last.available != nil {
		after := s.last.available.Version
		job.VersionAfter = &after
	}
	s.mu.Unlock()
	file := s.statusFile()
	if err := file.Write(job); err != nil {
		return Status{}, err
	}
	runner := exec.Command(command[0], command[1:]...)
	runner.Stdout, runner.Stderr = output, output
	// Its own session, so it outlives the server it restarts.
	runner.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := runner.Start(); err != nil {
		_ = file.Record(StateFailed, StateFailed, 100, "The update could not start: "+err.Error(), "", false)
		return Status{}, fmt.Errorf("updates: start update runner: %w", err)
	}
	// The runner records its own phases, so only claim the file while it still
	// shows the start.
	if current := file.Read(); current.State == StateRunning && current.Phase == "starting" {
		current.RunnerPID = runner.Process.Pid
		if err := file.Write(current); err != nil {
			return Status{}, err
		}
	}
	go func() { _ = runner.Wait() }()
	return s.Status(), nil
}

// SetChannel stores the channel, then checks it.
func (s *Service) SetChannel(ctx context.Context, requested string) (Status, error) {
	s.start.Lock()
	defer s.start.Unlock()
	if err := s.assertIdle(); err != nil {
		return Status{}, err
	}
	channel := release.ParseChannel(requested)
	if channel == "" {
		return Status{}, errChannel
	}
	if err := SetChannel(s.options.EnvFile, channel); err != nil {
		return Status{}, fmt.Errorf("updates: store release channel: %w", err)
	}
	s.mu.Lock()
	s.channel = channel
	s.mu.Unlock()
	return s.Check(ctx), nil
}
