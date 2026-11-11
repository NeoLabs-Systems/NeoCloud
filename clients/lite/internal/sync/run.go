package sync

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// maxWatchedDirs bounds the watcher: kqueue (macOS/BSD) holds a file
// descriptor per watched folder and per file inside it. Beyond the cap the
// periodic rescan still catches every change, just later.
const maxWatchedDirs = 1000

// Run syncs continuously until ctx ends: on start, after local changes
// (fsnotify, debounced), when the journal reports remote changes, on every
// rescan interval and on SyncNow.
func (e *Engine) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err == nil {
		e.mu.Lock()
		e.watcher = watcher
		e.watched = map[string]bool{}
		e.mu.Unlock()
		defer watcher.Close()
		go e.watchLoop(ctx, watcher)
	} else {
		e.log.Printf("file watcher unavailable, relying on rescans: %v", err)
	}

	e.SyncNow()
	cfg := e.cfg.Get()
	poll := time.NewTicker(seconds(cfg.PollSeconds, defaultPoll))
	rescan := time.NewTicker(seconds(cfg.RescanSeconds, defaultRescan))
	defer poll.Stop()
	defer rescan.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-e.wake:
		case <-rescan.C:
			e.SyncNow()
			continue
		case <-poll.C:
			e.pollRemote(ctx)
		}
		if dirty := e.takeDirty(); len(dirty) > 0 {
			if err := e.syncPairs(ctx, dirty); err != nil && ctx.Err() == nil {
				e.log.Printf("sync: %v", err)
			}
		}
	}
}

func (e *Engine) pollRemote(ctx context.Context) {
	cfg := e.cfg.Get()
	if cfg.Paused || !cfg.LoggedIn() || cfg.Locked() {
		return
	}
	client, err := e.Client()
	if err != nil {
		return
	}
	for _, pair := range cfg.Pairs {
		changed, err := e.remoteChanged(ctx, client, pair.ID)
		if err != nil {
			// Offline or a revoked token: run the pair anyway so the
			// status reflects the error.
			e.markDirty(pair.ID)
			continue
		}
		if changed {
			e.markDirty(pair.ID)
		}
	}
}

// watchDirs adds the folders a pass found to the watcher.
func (e *Engine) watchDirs(pairID string, dirs []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.watcher == nil {
		return
	}
	for _, dir := range dirs {
		if e.watched[dir] || len(e.watched) >= maxWatchedDirs {
			continue
		}
		if err := e.watcher.Add(dir); err == nil {
			e.watched[dir] = true
		}
	}
}

// watchLoop turns file events into dirty pairs, waiting until events stop
// for settleDelay so a large copy or a save-via-rename is one run.
func (e *Engine) watchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if strings.HasSuffix(event.Name, api.PartSuffix) || inMetaDir(event.Name) {
				continue // our own downloads in progress, marker and trash
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				e.mu.Lock()
				delete(e.watched, event.Name)
				e.mu.Unlock()
			}
			if pair, ok := pairFor(e.cfg.Get(), event.Name); ok {
				e.markDirty(pair)
				timer.Reset(settleDelay)
			}
		case <-watcher.Errors:
		case <-timer.C:
			e.poke()
		}
	}
}

func inMetaDir(file string) bool {
	sep := string(filepath.Separator)
	return strings.HasSuffix(file, sep+metaDir) || strings.Contains(file, sep+metaDir+sep)
}

func pairFor(cfg config.Config, file string) (string, bool) {
	for _, pair := range cfg.Pairs {
		root := filepath.Clean(pair.Local)
		if file == root || strings.HasPrefix(file, root+string(filepath.Separator)) {
			return pair.ID, true
		}
	}
	return "", false
}

func seconds(value int, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Second
}
