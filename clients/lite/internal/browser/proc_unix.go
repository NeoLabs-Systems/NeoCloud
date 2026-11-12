//go:build !windows

package browser

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// detach starts cmd in its own process group, so a Ctrl-C in the
// terminal that runs the client does not also close the app window.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// CloseApp asks the app window's browser, the one running with profileDir,
// to quit. On macOS Chromium keeps running (and in the Dock) after its last
// window closes. The process is signalled only if its command line still
// names profileDir, so a stale lock whose pid was reused is left alone.
func CloseApp(profileDir string) error {
	pid := singletonPID(profileDir)
	if pid == 0 {
		return nil
	}
	args, err := exec.Command("ps", "-ww", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !strings.Contains(string(args), "--user-data-dir="+profileDir) {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}
