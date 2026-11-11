// Package browser opens URLs and folders with the desktop's default app.
package browser

import (
	"os/exec"
	"runtime"
)

// Open hands target (a URL or a local path) to the OS.
func Open(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap; the opener exits on its own
	return nil
}
