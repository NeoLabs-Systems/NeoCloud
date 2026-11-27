package updates

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/cli/envfile"
)

const maxLogLines = 220

// Job states.
const (
	StateIdle      = "idle"
	StateRunning   = "running"
	StateCompleted = "completed"
	StateFailed    = "failed"
)

// Job is the last update: what the runner writes to the status file and the
// admin page reads.
type Job struct {
	State         string   `json:"state"`
	Phase         string   `json:"phase"`
	Progress      int      `json:"progress"`
	Message       string   `json:"message"`
	VersionBefore *string  `json:"versionBefore"`
	VersionAfter  *string  `json:"versionAfter"`
	Changelog     []string `json:"changelog"`
	RolledBack    bool     `json:"rolledBack"`
	Logs          []string `json:"logs"`
	StartedAt     *string  `json:"startedAt"`
	CompletedAt   *string  `json:"completedAt"`
	RunnerPID     int      `json:"runnerPid,omitempty"`
}

// StatusFile is the file the server and the detached update runner talk
// through: the runner has to outlive the server it restarts.
type StatusFile struct{ Path string }

func timestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Read returns the job, with a runner that died without saying so read as
// failed so the admin page never waits forever.
func (f StatusFile) Read() Job {
	job := Job{State: StateIdle, Phase: StateIdle}
	if raw, err := os.ReadFile(f.Path); err == nil {
		var saved Job
		if json.Unmarshal(raw, &saved) == nil && saved.State != "" {
			job = saved
		}
	}
	if job.State == StateRunning && !alive(job.RunnerPID) {
		now := timestamp()
		job.State, job.Phase, job.Progress, job.CompletedAt, job.RunnerPID = StateFailed, StateFailed, 100, &now, 0
		job.Message = "The update stopped unexpectedly. Nothing was lost; you can try again."
	}
	if job.Changelog == nil {
		job.Changelog = []string{}
	}
	if job.Logs == nil {
		job.Logs = []string{}
	}
	return job
}

// Write saves the job.
func (f StatusFile) Write(job Job) error {
	body, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	return envfile.WriteAtomic(f.Path, body, 0o600)
}

// Record adds one phase of the running update. It does nothing unless the
// server started the update, so an `update` typed in a terminal stays out of
// the admin page.
func (f StatusFile) Record(state, phase string, progress int, message, versionAfter string, rolledBack bool) error {
	if _, err := os.Stat(f.Path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	job := f.Read()
	if job.State != StateRunning {
		return nil
	}
	job.State, job.Phase, job.Progress, job.Message, job.RolledBack = state, phase, progress, message, rolledBack
	job.RunnerPID = os.Getpid()
	if versionAfter != "" {
		job.VersionAfter = &versionAfter
	}
	if state != StateRunning {
		now := timestamp()
		job.CompletedAt, job.RunnerPID = &now, 0
	}
	job.Logs = append(job.Logs, timestamp()+" "+message)
	job.Logs = job.Logs[max(0, len(job.Logs)-maxLogLines):]
	return f.Write(job)
}
