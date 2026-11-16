// Package sendmenu adds "Send to my device" to the file manager's menus, so a
// file can be sent from where it is (docs/handoff.md). It is opt-in
// (config.GlobalSettings.SendToMenu) and per-user: nothing here needs
// administrator rights.
//
//   - Windows: a shortcut in the Send to menu (%APPDATA%\Microsoft\Windows\SendTo).
//   - Linux: a desktop entry (Open with, and the launcher), a Nautilus and a
//     Nemo script and a Dolphin service menu.
//   - macOS: nothing. A Finder Quick Action or extension would need a signed
//     app, and this one is ad-hoc signed.
//
// Every entry runs `neocloud-lite send <files>`.
package sendmenu

import (
	"log"

	"github.com/neolabs/neocloud/clients/lite/internal/autostart"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// Supported reports whether this system has a menu to add to.
func Supported() bool { return supported }

// Reconcile makes the menu entries match the setting: present and pointing
// at this executable when on, gone when off. Failures are logged.
func Reconcile(store *config.Store, logger *log.Logger) {
	if !supported {
		return
	}
	exe, err := autostart.Executable()
	if err != nil {
		logger.Printf("send menu: %v", err)
		return
	}
	on := store.Get().SendMenuEnabled()
	if on && !autostart.Installed(exe) {
		return // a build run from a temporary folder: the entries would break
	}
	if err := Apply(on, exe); err != nil {
		logger.Printf("send menu: %v", err)
	}
}
