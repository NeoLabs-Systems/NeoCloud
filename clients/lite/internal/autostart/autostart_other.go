//go:build !darwin && !linux && !windows

package autostart

import "errors"

var errUnsupported = errors.New("autostart is not supported on this system")

func enable(string) error { return errUnsupported }
func disable() error      { return errUnsupported }
func enabled() bool       { return false }
