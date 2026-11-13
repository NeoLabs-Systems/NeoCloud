// Package notify shows desktop notifications for what the engine does,
// as the notifications setting asks: "errors" (failed runs, deletions
// waiting for a confirmation, conflicts, new folders waiting), "all"
// (also a line after every run that changed files) or "off".
package notify

import (
	"fmt"
	"log"
	gosync "sync"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

// Message is one notification.
type Message struct{ Title, Body string }

// Watch notifies about the engine's status changes until the process
// ends. Notifications are sent in the background; failures are logged.
func Watch(engine *sync.Engine, cfg *config.Store, logger *log.Logger) {
	var mu gosync.Mutex
	prev := engine.Status()
	seenConflict := time.Now()
	gate := newGate()
	engine.OnStatus(func(next sync.Status) {
		mu.Lock()
		defer mu.Unlock()
		conflicts := 0
		for _, item := range engine.Activity() {
			if item.Kind == sync.ActivityConflict && item.At.After(seenConflict) {
				conflicts++
			}
		}
		if conflicts > 0 {
			seenConflict = time.Now()
		}
		if next.State == sync.StateIdle {
			gate.clearErrors() // a good run: a later failure is news again
		}
		for _, message := range messages(prev, next, conflicts, cfg.Get().NotifyLevel()) {
			if !gate.allow(message, time.Now()) {
				continue
			}
			go func() {
				if err := send(message); err != nil {
					logger.Printf("notification: %v", err)
				}
			}()
		}
		prev = next
	})
}

// gate keeps a notification from repeating. Status updates arrive many
// times a second while a pass runs, and some carry no folder list or flip
// between "error" and "syncing" on every retry, so comparing with the
// previous update alone sent the same notice hundreds of times.
//   - a new folder is announced once per path while the app runs;
//   - "could not sync" / "waiting for you" once until a run succeeds;
//   - anything else at most once per text every 30 minutes.
type gate struct {
	folders map[string]bool
	errors  map[string]bool
	recent  map[string]time.Time
}

func newGate() *gate {
	return &gate{folders: map[string]bool{}, errors: map[string]bool{}, recent: map[string]time.Time{}}
}

func (g *gate) clearErrors() { g.errors = map[string]bool{} }

func (g *gate) allow(m Message, now time.Time) bool {
	key := m.Title + "\x00" + m.Body
	switch m.Title {
	case titleNewFolder:
		if g.folders[m.Body] {
			return false
		}
		g.folders[m.Body] = true
		return true
	case titleError, titleConfirm:
		if g.errors[m.Title] {
			return false
		}
		g.errors[m.Title] = true
		return true
	}
	if at, ok := g.recent[key]; ok && now.Sub(at) < 30*time.Minute {
		return false
	}
	g.recent[key] = now
	return true
}

const (
	titleError     = "NeoCloud could not sync"
	titleConfirm   = "NeoCloud is waiting for you"
	titleNewFolder = "New folder in NeoCloud"
)

// messages decides what a status change is worth telling.
func messages(prev, next sync.Status, conflicts int, level string) []Message {
	if level == config.NotifyOff {
		return nil
	}
	var out []Message
	if next.State != prev.State {
		switch next.State {
		case sync.StateError:
			out = append(out, Message{titleError, next.Message})
		case sync.StateConfirm:
			out = append(out, Message{titleConfirm, next.Message})
		case sync.StateIdle:
			if level == config.NotifyAll && prev.State == sync.StateSyncing && next.Changes > 0 {
				out = append(out, Message{"NeoCloud is up to date", plural(next.Changes, "file") + " synced"})
			}
		}
	}
	if conflicts > 0 {
		out = append(out, Message{"NeoCloud kept two versions", plural(conflicts, "file") + " changed on both sides; open NeoCloud to see them."})
	}
	for id, ps := range next.PairState {
		known := map[string]bool{}
		for _, folder := range prev.PairState[id].NewFolders {
			known[folder.Path] = true
		}
		for _, folder := range ps.NewFolders {
			if !known[folder.Path] {
				out = append(out, Message{titleNewFolder, folder.Path + " is not synced until you choose to. Open NeoCloud to decide."})
			}
		}
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
