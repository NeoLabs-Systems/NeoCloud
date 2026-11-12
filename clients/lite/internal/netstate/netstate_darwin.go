package netstate

import (
	"os/exec"
	"strings"
)

// metered is not detected on macOS yet (NWPathMonitor needs more cgo).
func metered() bool { return false }

// batterySaver reads Low Power Mode from pmset.
func batterySaver() bool {
	out, err := exec.Command("pmset", "-g").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "lowpowermode" {
			return fields[1] == "1"
		}
	}
	return false
}
