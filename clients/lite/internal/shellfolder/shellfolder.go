// Package shellfolder makes the local folders the app creates look like
// NeoCloud's in the desktop's file manager: the NeoCloud logo as the
// folder icon, and the NeoCloud folder in the Finder sidebar, the
// Explorer navigation pane or the GTK bookmarks. All of it is cosmetic:
// failures are logged and never stop a pair.
//
// Only folders the app created are touched (config.Pair.Created); a
// folder the user picked keeps its own look.
package shellfolder

import (
	_ "embed"
	"log"
	"os"
	"path/filepath"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// The logo, rendered by clients/lite/packaging/icons/render.sh.
var (
	//go:embed neocloud.png
	logoPNG []byte
	//go:embed neocloud.ico
	logoICO []byte
)

// Created brands the folders config.CreateLocal made for a pair: its own
// folder and a NeoCloud folder made to hold it. assets is a folder the
// app keeps (the config folder), where icon files can live.
func Created(local string, made []string, assets string, logger *log.Logger) {
	for _, dir := range made {
		if filepath.Clean(dir) != filepath.Clean(local) && filepath.Base(dir) != config.FolderName {
			continue
		}
		if err := setIcon(dir, assets); err != nil {
			logger.Printf("setting the NeoCloud icon on %s: %v", dir, err)
		}
	}
}

// Reconcile brings the sidebar entry in line with the config: the
// folder config.SidebarFolder names is shown, the one shown before (if
// another) is taken out. It only acts when that changed, so an entry the
// user removed by hand is not added back on every start.
func Reconcile(store *config.Store, logger *log.Logger) {
	cfg := store.Get()
	want := cfg.SidebarFolder()
	if want == cfg.Pinned {
		return
	}
	if cfg.Pinned != "" {
		if err := unpin(cfg.Pinned); err != nil {
			logger.Printf("removing %s from the sidebar: %v", cfg.Pinned, err)
		}
	}
	pinned := ""
	if want != "" {
		if err := pin(want, filepath.Dir(store.Path())); err != nil {
			logger.Printf("adding %s to the sidebar: %v", want, err)
		} else {
			pinned = want
		}
	}
	if err := store.Update(func(c *config.Config) error { c.Pinned = pinned; return nil }); err != nil {
		logger.Printf("saving the sidebar entry: %v", err)
	}
}

// writeAsset writes an icon file into assets unless it is there already
// with the same size.
func writeAsset(assets, name string, data []byte) (string, error) {
	file := filepath.Join(assets, name)
	if info, err := os.Stat(file); err == nil && info.Size() == int64(len(data)) {
		return file, nil
	}
	if err := os.MkdirAll(assets, 0o700); err != nil {
		return "", err
	}
	return file, os.WriteFile(file, data, 0o644)
}
