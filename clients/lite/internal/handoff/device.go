package handoff

import (
	"fmt"
	"strings"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
)

// ResolveDevice finds the device a person typed: its id, or its name
// without regard to case. An unclear answer says which devices matched.
func ResolveDevice(devices []api.HandoffDevice, query string) (api.HandoffDevice, error) {
	query = strings.TrimSpace(query)
	var matches []api.HandoffDevice
	for _, device := range devices {
		if device.ID == query {
			return device, nil
		}
		if strings.EqualFold(device.Name, query) {
			matches = append(matches, device)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return api.HandoffDevice{}, userErrorf("No device called %q is online right now.", query)
	}
	return api.HandoffDevice{}, userErrorf("%d devices are called %q. Use one of their ids instead.", len(matches), query)
}

// DeviceLabels name devices in a list: "Pixel 8 (phone)", made unique when
// two devices share a name.
func DeviceLabels(devices []api.HandoffDevice) []string {
	labels := make([]string, len(devices))
	used := map[string]int{}
	for i, device := range devices {
		label := safeLabel(device.Name)
		if kind := kindWord(device); kind != "" {
			label += " (" + kind + ")"
		}
		label = strings.ReplaceAll(label, "\n", " ")
		if used[label]++; used[label] > 1 {
			label = fmt.Sprintf("%s #%d", label, used[label])
		}
		labels[i] = label
	}
	return labels
}

func kindWord(device api.HandoffDevice) string {
	switch device.Kind {
	case "phone", "browser":
		return device.Kind
	case "desktop":
		return "computer"
	}
	return ""
}
