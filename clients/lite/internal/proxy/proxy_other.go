//go:build !darwin && !windows

package proxy

// readSystem: on Linux and elsewhere the system proxy is the environment
// (HTTPS_PROXY, HTTP_PROXY, NO_PROXY), which systemProxy already read.
func readSystem() systemSettings { return systemSettings{} }
