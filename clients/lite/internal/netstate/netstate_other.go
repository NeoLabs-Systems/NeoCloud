//go:build !darwin && !windows && !linux

package netstate

func metered() bool { return false }

func batterySaver() bool { return false }
