package sendmenu

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

const supported = true

// The shortcut's arguments are "send"; Explorer appends the chosen files.
const shortcutScript = `$link = (New-Object -ComObject WScript.Shell).CreateShortcut($env:NEOCLOUD_LINK)
$link.TargetPath = $env:NEOCLOUD_EXE
$link.Arguments = 'send'
$link.IconLocation = $env:NEOCLOUD_EXE + ',0'
$link.Description = 'Send files to another of your NeoCloud devices'
$link.Save()`

func linkPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", errors.New("the Send to folder was not found (%APPDATA% is not set)")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "SendTo", "Send to my NeoCloud device.lnk"), nil
}

// Apply adds or removes the shortcut; exe is the program it starts.
func Apply(on bool, exe string) error {
	link, err := linkPath()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", shortcutScript)
	cmd.Env = append(os.Environ(), "NEOCLOUD_LINK="+link, "NEOCLOUD_EXE="+exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("creating the Send to shortcut: " + err.Error() + ": " + string(out))
	}
	return nil
}
