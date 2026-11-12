//go:build !windows

package settingsui

import "os/exec"

func hideWindow(*exec.Cmd) {}
