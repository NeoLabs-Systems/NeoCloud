package runtimeapp

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPIDFileLockIsExclusiveAndCleanedUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "neocloud.pid")
	cleanup, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(pid)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("PID file=%q err=%v", pid, err)
	}
	if _, err := AcquirePIDFile(path); err == nil || !strings.Contains(err.Error(), "another NeoCloud server") {
		t.Fatalf("second lock error=%v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("PID file after cleanup stat error=%v", err)
	}
	cleanupAgain, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("reacquire after cleanup: %v", err)
	}
	cleanupAgain()
}

func TestPIDFileRefusesActivePIDWithoutLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "neocloud.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getppid())), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquirePIDFile(path); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("active process was not refused: %v", err)
	}
}

func TestPIDFileNamingThisProcessIsTakenOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "neocloud.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	cleanup, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}
