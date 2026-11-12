// Package tray is the menu-bar / notification-area icon. systray needs
// the main thread on macOS, so Run blocks the caller until Quit.
package tray

import (
	"embed"
	"fmt"
	"path/filepath"
	"runtime"
	gosync "sync"

	"fyne.io/systray"

	"github.com/neolabs/neocloud/clients/lite/internal/browser"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

// The icons are the logo's glyph with a status badge, made from
// flutter_app/assets/branding/logo.svg: black templates for the macOS menu
// bar (which tints them), the colored tile elsewhere.
//
//go:embed icons
var icons embed.FS

// maxFolderItems is how many synced folders the menu lists.
const maxFolderItems = 5

// Options wires the tray to the rest of the app.
type Options struct {
	Engine *sync.Engine
	Config *config.Store
	// OpenWindow shows the NeoCloud app window; OpenSettings shows it on
	// its Settings page.
	OpenWindow   func()
	OpenSettings func()
	OnReady      func()
	OnQuit       func()
}

// Run shows the icon and blocks until the user quits.
func Run(opts Options) {
	systray.Run(func() { onReady(opts) }, opts.OnQuit)
}

// Quit ends Run from another goroutine.
func Quit() { systray.Quit() }

func onReady(opts Options) {
	systray.SetTooltip("NeoCloud")

	// The same names as the app window: "Open web app", "Sync now",
	// "Pause syncing", "Settings".
	status := systray.AddMenuItem("Starting…", "")
	status.Disable()
	systray.AddSeparator()
	open := systray.AddMenuItem("Open NeoCloud", "")
	web := systray.AddMenuItem("Open web app", "Open NeoCloud in your browser")
	systray.AddSeparator()
	syncNow := systray.AddMenuItem("Sync now", "")
	pause := systray.AddMenuItem("Pause syncing", "")
	systray.AddSeparator()
	folders := make([]*systray.MenuItem, maxFolderItems)
	for i := range folders {
		folders[i] = systray.AddMenuItem("", "Open this folder")
		folders[i].Hide()
	}
	systray.AddSeparator()
	settings := systray.AddMenuItem("Settings…", "")
	quit := systray.AddMenuItem("Quit NeoCloud", "")

	var mu gosync.Mutex
	shownIcon := ""
	var shownPairs []config.Pair
	refresh := func(st sync.Status) {
		cfg := opts.Config.Get()
		status.SetTitle(statusLine(st))
		if cfg.Paused {
			pause.SetTitle("Resume syncing")
		} else {
			pause.SetTitle("Pause syncing")
		}
		// Signed out there is nothing to sync, pause or open on the web.
		for _, item := range []*systray.MenuItem{web, syncNow, pause} {
			if cfg.LoggedIn() {
				item.Enable()
			} else {
				item.Disable()
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if name := iconFor(st.State); name != shownIcon {
			shownIcon = name
			setIcon(name)
		}
		shownPairs = cfg.Pairs
		for i, item := range folders {
			if i < len(cfg.Pairs) {
				item.SetTitle("Open " + filepath.Base(cfg.Pairs[i].Local))
				item.Show()
			} else {
				item.Hide()
			}
		}
	}
	onReopen(opts.OpenWindow)
	opts.Engine.OnStatus(refresh)
	refresh(opts.Engine.Status())

	for i, item := range folders {
		go func() {
			for range item.ClickedCh {
				mu.Lock()
				local := ""
				if i < len(shownPairs) {
					local = shownPairs[i].Local
				}
				mu.Unlock()
				if local != "" {
					browser.Open(local)
				}
			}
		}()
	}
	go func() {
		for {
			select {
			case <-open.ClickedCh:
				opts.OpenWindow()
			case <-web.ClickedCh:
				if server := opts.Config.Get().Server; server != "" {
					browser.Open(server + "/app")
				}
			case <-settings.ClickedCh:
				opts.OpenSettings()
			case <-pause.ClickedCh:
				opts.Engine.SetPaused(!opts.Config.Get().Paused)
				refresh(opts.Engine.Status())
			case <-syncNow.ClickedCh:
				opts.Engine.SyncNow()
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

// iconFor maps an engine state to an icon name.
func iconFor(state string) string {
	switch state {
	case sync.StateIdle, "":
		return "synced"
	case sync.StateSyncing:
		return "syncing"
	case sync.StatePaused:
		return "paused"
	case sync.StateLocked:
		return "locked"
	}
	return "error" // error, offline, needs-confirmation, logged-out
}

func setIcon(name string) {
	switch runtime.GOOS {
	case "darwin":
		template, _ := icons.ReadFile("icons/template_" + name + ".png")
		color, _ := icons.ReadFile("icons/color_" + name + ".png")
		systray.SetTemplateIcon(template, color)
	case "windows":
		ico, _ := icons.ReadFile("icons/color_" + name + ".ico")
		systray.SetIcon(ico)
	default:
		png, _ := icons.ReadFile("icons/color_" + name + ".png")
		systray.SetIcon(png)
	}
}

func statusLine(st sync.Status) string {
	switch st.State {
	case sync.StateIdle:
		if st.LastSync.IsZero() {
			return "Up to date"
		}
		return "Up to date · " + st.LastSync.Format("15:04")
	case sync.StateSyncing:
		switch {
		case st.Changes == 1:
			return "Syncing 1 file"
		case st.Changes > 1:
			return fmt.Sprintf("Syncing %d files", st.Changes)
		}
		return "Syncing…"
	case sync.StateLoggedOut:
		return "Not signed in"
	case sync.StateLocked:
		return "Locked"
	case sync.StateOffline:
		return "Offline"
	case sync.StatePaused:
		if st.Message != "" {
			return st.Message // "Paused", or why it waits (metered, battery saver)
		}
		return "Paused"
	case sync.StateConfirm, sync.StateError:
		return "Needs attention"
	case "":
		return "Starting…"
	}
	if st.Message != "" {
		return st.Message
	}
	return st.State
}
