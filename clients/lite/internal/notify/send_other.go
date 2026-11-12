//go:build !darwin && !windows

package notify

import "os/exec"

// send uses notify-send (libnotify) when it is installed.
func send(m Message) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return nil
	}
	return exec.Command(path, "--app-name=NeoCloud", "--", m.Title, m.Body).Run()
}
