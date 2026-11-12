package notify

import "os/exec"

// send uses AppleScript; the text is passed as arguments, never parsed.
func send(m Message) error {
	return exec.Command("osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		m.Title, m.Body).Run()
}
