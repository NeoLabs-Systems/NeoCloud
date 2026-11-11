package settingsui

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps PowerShell from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
