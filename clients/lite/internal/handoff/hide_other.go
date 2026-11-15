//go:build !windows

package handoff

import "os/exec"

func hideWindow(*exec.Cmd) {}
