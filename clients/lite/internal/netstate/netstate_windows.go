package netstate

import (
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// costScript prints the NetworkCostType of the internet connection
// (Unrestricted, Fixed, Variable or Unknown) through the WinRT API.
const costScript = `$p = [Windows.Networking.Connectivity.NetworkInformation, Windows.Networking.Connectivity, ContentType = WindowsRuntime]::GetInternetConnectionProfile()
if ($p) { $c = $p.GetConnectionCost(); if ($c.Roaming -or $c.OverDataLimit) { 'Fixed' } else { $c.NetworkCostType } }`

// metered reports a Fixed or Variable cost connection, as Windows'
// "metered connection" switch sets it.
func metered() bool {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", costScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	cost := strings.TrimSpace(string(out))
	return cost == "Fixed" || cost == "Variable"
}

var getSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// systemPowerStatus is SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte // 1 while battery saver is on
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func batterySaver() bool {
	var status systemPowerStatus
	ok, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	return ok != 0 && status.SystemStatusFlag == 1
}
