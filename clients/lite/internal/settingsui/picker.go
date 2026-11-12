package settingsui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// commandRunner runs a program and returns what it printed. A program
// that ran but exited non-zero (a dialog the user cancelled) is reported
// as errPickerCancelled.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

var errPickerCancelled = errors.New("cancelled")

type pickerCommand struct {
	name string
	args []string
}

// Dialog titles: picking the folder to sync, or (?for=parent) where the
// cloud-first flow creates its folder.
const (
	pickerTitle       = "Choose a folder to sync"
	parentPickerTitle = "Choose where to create the NeoCloud folder"
)

// The Windows dialog is owned by an invisible topmost form so it opens in
// front of the app window instead of behind it.
const windowsPicker = `Add-Type -AssemblyName System.Windows.Forms
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$owner = New-Object System.Windows.Forms.Form -Property @{TopMost = $true}
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = '@TITLE@'
$dialog.ShowNewFolderButton = $true
if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { $dialog.SelectedPath }`

// pickerCommands are the native folder dialogs to try on goos, in order.
// title is one of the constants above, so it needs no quoting.
func pickerCommands(goos, title string, lookPath func(string) (string, error)) []pickerCommand {
	switch goos {
	case "darwin":
		return []pickerCommand{{"osascript", []string{"-e", `POSIX path of (choose folder with prompt "` + title + `")`}}}
	case "windows":
		return []pickerCommand{{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", strings.Replace(windowsPicker, "@TITLE@", title, 1)}}}
	}
	home, _ := os.UserHomeDir()
	linux := []pickerCommand{
		{"zenity", []string{"--file-selection", "--directory", "--title=" + title}},
		{"kdialog", []string{"--getexistingdirectory", home, "--title", title}},
		{"yad", []string{"--file", "--directory", "--title=" + title}},
	}
	var found []pickerCommand
	for _, command := range linux {
		if _, err := lookPath(command.name); err == nil {
			found = append(found, command)
		}
	}
	return found
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out, errPickerCancelled
	}
	return out, err
}

// pickFolder shows the OS folder dialog and answers { path }, or
// { cancelled: true }, or { unsupported: true } when there is no dialog
// to show (the page then asks for the path as text).
func (s *Server) pickFolder(r *http.Request) (any, error) {
	if !s.picking.TryLock() {
		return nil, errors.New("a folder dialog is already open")
	}
	defer s.picking.Unlock()
	title := pickerTitle
	if r.URL.Query().Get("for") == "parent" {
		title = parentPickerTitle
	}
	for _, command := range pickerCommands(runtime.GOOS, title, s.lookPath) {
		out, err := s.run(r.Context(), command.name, command.args...)
		if errors.Is(err, errPickerCancelled) {
			return map[string]bool{"cancelled": true}, nil
		}
		if err != nil {
			continue // not installed or not runnable here: try the next one
		}
		path := strings.TrimSpace(string(out))
		if path == "" {
			return map[string]bool{"cancelled": true}, nil
		}
		return map[string]string{"path": filepath.Clean(path)}, nil
	}
	return map[string]bool{"unsupported": true}, nil
}
