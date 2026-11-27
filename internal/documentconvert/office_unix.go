//go:build !windows

package documentconvert

import "syscall"

func processGroupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }
func platformKillGroup(pid int) error        { return syscall.Kill(-pid, syscall.SIGKILL) }
