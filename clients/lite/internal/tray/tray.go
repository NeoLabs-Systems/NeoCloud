// Package tray is the menu-bar / notification-area icon. systray needs
// the main thread on macOS, so Run blocks the caller until Quit.
package tray

import (
	_ "embed"
	"runtime"

	"fyne.io/systray"

	"github.com/neolabs/neocloud/clients/lite/internal/browser"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

//go:embed tray_icon.png
var iconPNG []byte

//go:embed tray_icon.ico
var iconICO []byte

// Options wires the tray to the rest of the app.
type Options struct {
	Engine      *sync.Engine
	Config      *config.Store
	SettingsURL func() string
	OnReady     func()
	OnQuit      func()
}

// Run shows the icon and blocks until the user quits.
func Run(opts Options) {
	systray.Run(func() { onReady(opts) }, opts.OnQuit)
}

// Quit ends Run from another goroutine.
func Quit() { systray.Quit() }

func onReady(opts Options) {
	if runtime.GOOS == "windows" {
		systray.SetIcon(iconICO)
	} else {
		systray.SetIcon(iconPNG)
	}
	systray.SetTooltip("NeoCloud Lite")

	status := systray.AddMenuItem("Starting…", "")
	status.Disable()
	systray.AddSeparator()
	openFolder := systray.AddMenuItem("Open folder", "Open the first synced folder")
	openWeb := systray.AddMenuItem("Open NeoCloud in the browser", "")
	systray.AddSeparator()
	pause := systray.AddMenuItem("Pause syncing", "")
	syncNow := systray.AddMenuItem("Sync now", "")
	settings := systray.AddMenuItem("Settings…", "")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "")

	refresh := func(st sync.Status) {
		status.SetTitle(statusLine(st))
		if opts.Config.Get().Paused {
			pause.SetTitle("Resume syncing")
		} else {
			pause.SetTitle("Pause syncing")
		}
	}
	opts.Engine.OnStatus(refresh)
	refresh(opts.Engine.Status())

	go func() {
		for {
			select {
			case <-openFolder.ClickedCh:
				if pairs := opts.Config.Get().Pairs; len(pairs) > 0 {
					browser.Open(pairs[0].Local)
				} else {
					browser.Open(opts.SettingsURL())
				}
			case <-openWeb.ClickedCh:
				if server := opts.Config.Get().Server; server != "" {
					browser.Open(server + "/app")
				}
			case <-pause.ClickedCh:
				opts.Engine.SetPaused(!opts.Config.Get().Paused)
				refresh(opts.Engine.Status())
			case <-syncNow.ClickedCh:
				opts.Engine.SyncNow()
			case <-settings.ClickedCh:
				browser.Open(opts.SettingsURL())
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
	if opts.OnReady != nil {
		opts.OnReady()
	}
}

func statusLine(st sync.Status) string {
	switch st.State {
	case sync.StateIdle:
		if st.LastSync.IsZero() {
			return "Up to date"
		}
		return "Up to date · " + st.LastSync.Format("15:04")
	case sync.StateLoggedOut:
		return "Not signed in"
	case "":
		return "Starting…"
	}
	if st.Message != "" {
		return st.Message
	}
	return st.State
}
