package netstate

import "github.com/godbus/dbus/v5"

// metered asks NetworkManager (NMMetered: 1 yes, 3 guessed yes).
func metered() bool {
	conn, err := dbus.SystemBus()
	if err != nil {
		return false
	}
	value, err := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager").
		GetProperty("org.freedesktop.NetworkManager.Metered")
	if err != nil {
		return false
	}
	state, ok := value.Value().(uint32)
	return ok && (state == 1 || state == 3)
}

// batterySaver asks power-profiles-daemon for the power-saver profile.
func batterySaver() bool {
	conn, err := dbus.SystemBus()
	if err != nil {
		return false
	}
	for _, name := range []string{"org.freedesktop.UPower.PowerProfiles", "net.hadess.PowerProfiles"} {
		path := dbus.ObjectPath("/" + replaceDots(name))
		value, err := conn.Object(name, path).GetProperty(name + ".ActiveProfile")
		if err == nil {
			profile, _ := value.Value().(string)
			return profile == "power-saver"
		}
	}
	return false
}

func replaceDots(name string) string {
	out := []byte(name)
	for i := range out {
		if out[i] == '.' {
			out[i] = '/'
		}
	}
	return string(out)
}
