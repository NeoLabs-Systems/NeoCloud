//go:build !darwin && !windows && !linux

package shellfolder

func setIcon(string, string) error { return nil }

func pin(string, string) error { return nil }

func unpin(string) error { return nil }
