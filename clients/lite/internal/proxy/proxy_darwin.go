package proxy

import (
	"os/exec"
	"strings"
)

// readSystem parses `scutil --proxy` (System Settings › Network › Proxies).
func readSystem() systemSettings {
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return systemSettings{}
	}
	return parseScutil(string(out))
}

func parseScutil(text string) systemSettings {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, " : ")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	var settings systemSettings
	if values["HTTPEnable"] == "1" {
		settings.http = hostPort(values["HTTPProxy"], values["HTTPPort"])
	}
	if values["HTTPSEnable"] == "1" {
		settings.https = hostPort(values["HTTPSProxy"], values["HTTPSPort"])
	}
	return settings
}
