package handoff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Choice is what the person answered a send with.
type Choice int

const (
	// ChoiceDownloads saves into the Downloads folder.
	ChoiceDownloads Choice = iota
	// ChoiceFolder asks for a folder first.
	ChoiceFolder
	// ChoiceDecline refuses the send.
	ChoiceDecline
	// ChoiceLater closes the dialog without deciding; the send stays waiting.
	ChoiceLater
	// ChoiceUnavailable means this system has no dialog to ask with.
	ChoiceUnavailable
)

// Prompter asks what to do with a send. message is plain text.
type Prompter interface {
	Ask(ctx context.Context, message string) (Choice, error)
}

// runner runs a program with extra environment and reports its output and
// exit code (-1 when it could not start).
type runner func(ctx context.Context, env []string, name string, args ...string) (out string, code int)

// nativePrompt uses what the system provides: AppleScript on macOS,
// PowerShell's message box on Windows, zenity or kdialog on Linux. The
// message is never part of a script: it goes in through argv or the
// environment.
type nativePrompt struct {
	goos     string
	lookPath func(string) (string, error)
	run      runner
}

// NewNativePrompt is the dialog of the system this runs on.
func NewNativePrompt() Prompter {
	return nativePrompt{goos: runtime.GOOS, lookPath: exec.LookPath, run: runProgram}
}

const (
	saveLabel   = "Save to Downloads"
	chooseLabel = "Choose folder…"
	declineText = "Decline"
)

const macScript = `on run argv
	set answer to display dialog (item 1 of argv) with title "NeoCloud" buttons {"Decline", "Choose folder…", "Save to Downloads"} default button 3
	return button returned of answer
end run`

// Windows has no four-button box: Yes saves, No chooses a folder, Cancel
// (also the close button) declines.
const windowsScript = `Add-Type -AssemblyName System.Windows.Forms
$text = $env:NEOCLOUD_PROMPT + "` + "`n`n" + `Yes: save to Downloads` + "`n" + `No: choose a folder` + "`n" + `Cancel: decline"
[System.Windows.Forms.MessageBox]::Show($text, 'NeoCloud', 'YesNoCancel', 'Question').ToString()`

func (p nativePrompt) Ask(ctx context.Context, message string) (Choice, error) {
	switch p.goos {
	case "darwin":
		out, code := p.run(ctx, nil, "osascript", "-e", macScript, message)
		switch {
		case code != 0:
			return ChoiceLater, nil // Esc
		case strings.Contains(out, declineText):
			return ChoiceDecline, nil
		case strings.Contains(out, chooseLabel):
			return ChoiceFolder, nil
		}
		return ChoiceDownloads, nil
	case "windows":
		out, code := p.run(ctx, []string{"NEOCLOUD_PROMPT=" + message}, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-STA", "-Command", windowsScript)
		if code != 0 {
			return ChoiceUnavailable, errors.New("the message box did not open")
		}
		switch strings.TrimSpace(out) {
		case "Yes":
			return ChoiceDownloads, nil
		case "No":
			return ChoiceFolder, nil
		}
		return ChoiceDecline, nil
	}
	return p.askLinux(ctx, message)
}

func (p nativePrompt) askLinux(ctx context.Context, message string) (Choice, error) {
	if _, err := p.lookPath("zenity"); err == nil {
		// OK saves, the extra button chooses a folder (printed, exit 1), Cancel declines.
		out, code := p.run(ctx, nil, "zenity", "--question", "--no-markup", "--title=NeoCloud", "--text="+message,
			"--ok-label="+saveLabel, "--cancel-label="+declineText, "--extra-button="+chooseLabel)
		switch {
		case code == 0:
			return ChoiceDownloads, nil
		case strings.Contains(out, chooseLabel):
			return ChoiceFolder, nil
		case code == 1:
			return ChoiceDecline, nil
		}
		return ChoiceLater, nil
	}
	if _, err := p.lookPath("kdialog"); err == nil {
		_, code := p.run(ctx, nil, "kdialog", "--title", "NeoCloud", "--yes-label", saveLabel, "--no-label", chooseLabel,
			"--cancel-label", declineText, "--yesnocancel", message)
		switch code {
		case 0:
			return ChoiceDownloads, nil
		case 1:
			return ChoiceFolder, nil
		case 2:
			return ChoiceDecline, nil
		}
		return ChoiceLater, nil
	}
	return ChoiceUnavailable, nil
}

func runProgram(ctx context.Context, env []string, name string, args ...string) (string, int) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	hideWindow(cmd)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode()
	}
	if err != nil {
		return "", -1
	}
	return string(out), 0
}
