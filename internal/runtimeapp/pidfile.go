package runtimeapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// AcquirePIDFile makes this process the only server of an install: it holds the
// lock a restore also takes and records the PID until the returned func runs.
func AcquirePIDFile(path string) (func(), error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("server PID file path is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create server state directory: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open server startup lock: %w", err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("another NeoCloud server or restore operation is already running")
	}
	cleanupOnError := func(err error) (func(), error) {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
		return nil, err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return cleanupOnError(errors.New("server PID path is not a regular file"))
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return cleanupOnError(fmt.Errorf("read existing server PID file: %w", readErr))
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if parseErr != nil || pid < 1 {
			return cleanupOnError(errors.New("existing server PID file is invalid; remove it only after confirming NeoCloud is stopped"))
		}
		// A service manager's fallback records the PID of the server it starts,
		// and a restarted container can get the same one again.
		if pid != os.Getpid() && processIsAlive(pid) {
			return cleanupOnError(fmt.Errorf("NeoCloud process %d is still running", pid))
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return cleanupOnError(fmt.Errorf("remove stale server PID file: %w", err))
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return cleanupOnError(statErr)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return cleanupOnError(fmt.Errorf("create server PID file: %w", err))
	}
	pid := os.Getpid()
	if _, err = fmt.Fprintf(file, "%d\n", pid); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return cleanupOnError(fmt.Errorf("write server PID file: %w", err))
	}
	cleanup := func() {
		if raw, readErr := os.ReadFile(path); readErr == nil && strings.TrimSpace(string(raw)) == strconv.Itoa(pid) {
			_ = os.Remove(path)
		}
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}
	return cleanup, nil
}

func processIsAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
