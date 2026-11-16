package settingsui

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Dialog title of the file picker; a plain constant, as it goes into scripts.
const filesPickerTitle = "Choose files to send"

const macFilesScript = `set picked to choose file with prompt "@TITLE@" with multiple selections allowed
set out to ""
repeat with item_ in picked
	set out to out & POSIX path of item_ & linefeed
end repeat
return out`

const windowsFilesPicker = `Add-Type -AssemblyName System.Windows.Forms
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$owner = New-Object System.Windows.Forms.Form -Property @{TopMost = $true}
$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Title = '@TITLE@'
$dialog.Multiselect = $true
if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { $dialog.FileNames -join "` + "`n" + `" }`

// filePickerCommands are the native multi-file dialogs to try on goos, in
// order. They print one path per line.
func filePickerCommands(goos, title string, lookPath func(string) (string, error)) []pickerCommand {
	switch goos {
	case "darwin":
		return []pickerCommand{{"osascript", []string{"-e", strings.Replace(macFilesScript, "@TITLE@", title, 1)}}}
	case "windows":
		return []pickerCommand{{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", strings.Replace(windowsFilesPicker, "@TITLE@", title, 1)}}}
	}
	home, _ := os.UserHomeDir()
	linux := []pickerCommand{
		{"zenity", []string{"--file-selection", "--multiple", "--separator=\n", "--title=" + title}},
		{"kdialog", []string{"--getopenfilename", home, "--multiple", "--separate-output", "--title", title}},
		{"yad", []string{"--file", "--multiple", "--separator=\n", "--title=" + title}},
	}
	var found []pickerCommand
	for _, command := range linux {
		if _, err := lookPath(command.name); err == nil {
			found = append(found, command)
		}
	}
	return found
}

// pickFilesWith tries the native file dialogs in order and returns the paths.
func pickFilesWith(ctx context.Context, run commandRunner, lookPath func(string) (string, error), title string) ([]string, pickOutcome) {
	out, outcome := firstAnswer(ctx, run, filePickerCommands(runtime.GOOS, title, lookPath))
	if outcome != pickedFolder {
		return nil, outcome
	}
	var paths []string
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, filepath.Clean(line))
		}
	}
	return paths, pickedFolder
}

// ChooseFiles shows the OS dialog for choosing several files. ok is false
// when the person cancelled or this system has no dialog.
func ChooseFiles(ctx context.Context) (paths []string, ok bool) {
	paths, outcome := pickFilesWith(ctx, runCommand, exec.LookPath, filesPickerTitle)
	return paths, outcome == pickedFolder && len(paths) > 0
}

// pickFiles is the page's "Choose files": { paths }, { cancelled } or
// { unsupported }.
func (s *Server) pickFiles(r *http.Request) (any, error) {
	if !s.picking.TryLock() {
		return nil, errAlreadyPicking
	}
	defer s.picking.Unlock()
	paths, outcome := pickFilesWith(r.Context(), s.run, s.lookPath, filesPickerTitle)
	switch outcome {
	case pickedFolder:
		return s.inspectPaths(paths)
	case pickCancelled:
		return map[string]bool{"cancelled": true}, nil
	}
	return map[string]bool{"unsupported": true}, nil
}
