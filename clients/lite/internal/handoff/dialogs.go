package handoff

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

var (
	// ErrCancelled means the person closed or cancelled the dialog.
	ErrCancelled = errors.New("cancelled")
	// ErrNoDialog means this system has nothing to show a dialog with.
	ErrNoDialog = errors.New("no dialog program is available")
)

// Dialogs are the questions the send flow asks. Messages are plain text.
type Dialogs interface {
	// Choose shows labels as a list and returns the index of the chosen one.
	Choose(ctx context.Context, message string, labels []string) (int, error)
	// Confirm asks to go ahead; ok is the confirm button's label.
	Confirm(ctx context.Context, message, ok string) (bool, error)
	// Inform shows a message with one button.
	Inform(ctx context.Context, message string) error
}

// nativeDialogs uses what the system provides, like nativePrompt: AppleScript
// on macOS, PowerShell's forms on Windows, zenity or kdialog on Linux. Text
// never becomes part of a script: it goes in through argv or the environment.
type nativeDialogs struct {
	goos     string
	lookPath func(string) (string, error)
	run      runner
}

// NewNativeDialogs is the set of dialogs of the system this runs on.
func NewNativeDialogs() Dialogs {
	return nativeDialogs{goos: runtime.GOOS, lookPath: exec.LookPath, run: runProgram}
}

const macChooseScript = `on run argv
	set picked to choose from list (items 3 thru -1 of argv) with title "NeoCloud" with prompt (item 1 of argv) default items {item 3 of argv} OK button name (item 2 of argv) cancel button name "Cancel"
	if picked is false then error number -128
	return item 1 of picked
end run`

const macConfirmScript = `on run argv
	display dialog (item 1 of argv) with title "NeoCloud" buttons {"Cancel", item 2 of argv} default button 2 cancel button 1 with icon note
end run`

const macInformScript = `on run argv
	display dialog (item 1 of argv) with title "NeoCloud" buttons {"OK"} default button 1 with icon note
end run`

// The list is a small form owned by nothing, kept on top; it prints the
// chosen index.
const windowsChooseScript = `Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form -Property @{Text = 'NeoCloud'; StartPosition = 'CenterScreen'; TopMost = $true; ClientSize = New-Object System.Drawing.Size(360, 300); FormBorderStyle = 'FixedDialog'; MaximizeBox = $false; MinimizeBox = $false}
$label = New-Object System.Windows.Forms.Label -Property @{Text = $env:NEOCLOUD_PROMPT; Left = 12; Top = 12; Width = 336; Height = 40}
$list = New-Object System.Windows.Forms.ListBox -Property @{Left = 12; Top = 56; Width = 336; Height = 190}
foreach ($item in ($env:NEOCLOUD_ITEMS -split "` + "`n" + `")) { [void]$list.Items.Add($item) }
$list.SelectedIndex = 0
$ok = New-Object System.Windows.Forms.Button -Property @{Text = $env:NEOCLOUD_OK; Left = 176; Top = 260; Width = 84; DialogResult = 'OK'}
$cancel = New-Object System.Windows.Forms.Button -Property @{Text = 'Cancel'; Left = 268; Top = 260; Width = 80; DialogResult = 'Cancel'}
$form.AcceptButton = $ok
$form.CancelButton = $cancel
$form.Controls.AddRange(@($label, $list, $ok, $cancel))
if ($form.ShowDialog() -eq 'OK' -and $list.SelectedIndex -ge 0) { $list.SelectedIndex }`

const windowsBoxScript = `Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.MessageBox]::Show($env:NEOCLOUD_PROMPT, 'NeoCloud', $env:NEOCLOUD_BUTTONS, 'Information').ToString()`

func (d nativeDialogs) Choose(ctx context.Context, message string, labels []string) (int, error) {
	const send = "Send"
	switch d.goos {
	case "darwin":
		args := append([]string{"-e", macChooseScript, message, send}, labels...)
		out, code := d.run(ctx, nil, "osascript", args...)
		if code != 0 {
			return 0, ErrCancelled
		}
		return indexOf(labels, strings.TrimSpace(out))
	case "windows":
		env := []string{"NEOCLOUD_PROMPT=" + message, "NEOCLOUD_OK=" + send, "NEOCLOUD_ITEMS=" + strings.Join(labels, "\n")}
		out, code := d.run(ctx, env, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", windowsChooseScript)
		if code != 0 {
			return 0, errors.New("the dialog did not open")
		}
		if strings.TrimSpace(out) == "" {
			return 0, ErrCancelled
		}
		index, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil || index < 0 || index >= len(labels) {
			return 0, ErrCancelled
		}
		return index, nil
	}
	if _, err := d.lookPath("zenity"); err == nil {
		args := []string{"--list", "--title=NeoCloud", "--text=" + message, "--column=Device", "--ok-label=" + send, "--no-markup"}
		out, code := d.run(ctx, nil, "zenity", append(args, labels...)...)
		if code != 0 {
			return 0, ErrCancelled
		}
		return indexOf(labels, strings.TrimSpace(out))
	}
	if _, err := d.lookPath("kdialog"); err == nil {
		args := []string{"--title", "NeoCloud", "--menu", message}
		for i, label := range labels {
			args = append(args, strconv.Itoa(i), label)
		}
		out, code := d.run(ctx, nil, "kdialog", args...)
		if code != 0 {
			return 0, ErrCancelled
		}
		index, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil || index < 0 || index >= len(labels) {
			return 0, ErrCancelled
		}
		return index, nil
	}
	return 0, ErrNoDialog
}

func indexOf(labels []string, chosen string) (int, error) {
	for i, label := range labels {
		if label == chosen {
			return i, nil
		}
	}
	return 0, ErrCancelled
}

func (d nativeDialogs) Confirm(ctx context.Context, message, ok string) (bool, error) {
	switch d.goos {
	case "darwin":
		_, code := d.run(ctx, nil, "osascript", "-e", macConfirmScript, message, ok)
		return code == 0, nil
	case "windows":
		out, code := d.run(ctx, []string{"NEOCLOUD_PROMPT=" + message, "NEOCLOUD_BUTTONS=OKCancel"}, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-STA", "-Command", windowsBoxScript)
		if code != 0 {
			return false, errors.New("the message box did not open")
		}
		return strings.TrimSpace(out) == "OK", nil
	}
	if _, err := d.lookPath("zenity"); err == nil {
		_, code := d.run(ctx, nil, "zenity", "--question", "--no-markup", "--title=NeoCloud", "--text="+message, "--ok-label="+ok, "--cancel-label=Cancel")
		return code == 0, nil
	}
	if _, err := d.lookPath("kdialog"); err == nil {
		_, code := d.run(ctx, nil, "kdialog", "--title", "NeoCloud", "--yes-label", ok, "--no-label", "Cancel", "--yesno", message)
		return code == 0, nil
	}
	return false, ErrNoDialog
}

func (d nativeDialogs) Inform(ctx context.Context, message string) error {
	switch d.goos {
	case "darwin":
		d.run(ctx, nil, "osascript", "-e", macInformScript, message)
		return nil
	case "windows":
		d.run(ctx, []string{"NEOCLOUD_PROMPT=" + message, "NEOCLOUD_BUTTONS=OK"}, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-STA", "-Command", windowsBoxScript)
		return nil
	}
	if _, err := d.lookPath("zenity"); err == nil {
		d.run(ctx, nil, "zenity", "--info", "--no-markup", "--title=NeoCloud", "--text="+message)
		return nil
	}
	if _, err := d.lookPath("kdialog"); err == nil {
		d.run(ctx, nil, "kdialog", "--title", "NeoCloud", "--msgbox", message)
		return nil
	}
	return ErrNoDialog
}
