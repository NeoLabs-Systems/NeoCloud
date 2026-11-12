package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func acquire(t *testing.T, dir string) *Instance {
	t.Helper()
	inst, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// A second launch finds the first copy, asks it to open its window and
// does not get the lock itself.
func TestSecondLaunchOpensTheRunningWindow(t *testing.T) {
	dir := t.TempDir()
	first := acquire(t, dir)
	defer first.Close()
	opened := make(chan struct{}, 1)
	go first.Serve(func() { opened <- struct{}{} })

	if _, err := Acquire(dir); !errors.Is(err, ErrRunning) {
		t.Fatalf("second Acquire: %v, want ErrRunning", err)
	}
	if err := Show(dir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the running copy was not asked to open its window")
	}
}

// Requests that arrive before Serve wait for it.
func TestShowWaitsForServe(t *testing.T) {
	dir := t.TempDir()
	first := acquire(t, dir)
	defer first.Close()
	opened := make(chan struct{}, 1)
	time.AfterFunc(300*time.Millisecond, func() { go first.Serve(func() { opened <- struct{}{} }) })
	if err := Show(dir); err != nil {
		t.Fatal(err)
	}
	<-opened
}

// Without the token from instance.json nothing happens.
func TestWrongTokenIsRefused(t *testing.T) {
	dir := t.TempDir()
	first := acquire(t, dir)
	defer first.Close()
	opened := make(chan struct{}, 1)
	go first.Serve(func() { opened <- struct{}{} })

	raw, err := os.ReadFile(filepath.Join(dir, infoName))
	if err != nil {
		t.Fatal(err)
	}
	var running info
	if err := json.Unmarshal(raw, &running); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"open wrong\n", "open \n", "quit " + running.Token + "\n"} {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", running.Port))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(conn, line)
		buf := make([]byte, 8)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if n, _ := conn.Read(buf); n != 0 {
			t.Errorf("%q got an answer: %q", line, buf[:n])
		}
		conn.Close()
	}
	select {
	case <-opened:
		t.Fatal("a request without the token opened the window")
	case <-time.After(200 * time.Millisecond):
	}
}

// instance.json carries the token, so only its owner may read it.
func TestInfoFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permissions")
	}
	dir := t.TempDir()
	first := acquire(t, dir)
	defer first.Close()
	stat, err := os.Stat(filepath.Join(dir, infoName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := stat.Mode().Perm(); mode != 0o600 {
		t.Errorf("instance.json mode %o", mode)
	}
}

// Quitting releases the lock: the next launch becomes the running copy.
func TestCloseReleasesTheLock(t *testing.T) {
	dir := t.TempDir()
	acquire(t, dir).Close()
	if _, err := os.Stat(filepath.Join(dir, infoName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("instance.json left behind: %v", err)
	}
	acquire(t, dir).Close()
}

// With nobody running, Show gives up instead of hanging.
func TestShowWithoutARunningCopy(t *testing.T) {
	if err := Show(t.TempDir()); err == nil {
		t.Fatal("Show succeeded with nothing running")
	}
}
