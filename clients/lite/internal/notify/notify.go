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
		for _, message := range messages(prev, next, conflicts, cfg.Get().NotifyLevel()) {
			go func() {
				if err := send(message); err != nil {
					logger.Printf("notification: %v", err)
				}
			}()
		}
		prev = next
	})
}

// messages decides what a status change is worth telling.
func messages(prev, next sync.Status, conflicts int, level string) []Message {
	if level == config.NotifyOff {
		return nil
	}
	var out []Message
	if next.State != prev.State {
		switch next.State {
		case sync.StateError:
			out = append(out, Message{"NeoCloud could not sync", next.Message})
		case sync.StateConfirm:
			out = append(out, Message{"NeoCloud is waiting for you", next.Message})
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
				out = append(out, Message{"New folder in NeoCloud", folder.Path + " is not synced until you choose to. Open NeoCloud to decide."})
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
