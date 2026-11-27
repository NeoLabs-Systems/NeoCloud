//go:build windows

package documentconvert

import "syscall"

func processGroupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }
func platformKillGroup(int) error            { return nil }
