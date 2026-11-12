package sync

import "time"

// Kinds of Activity.
const (
	ActivityUpload       = "upload"
	ActivityDownload     = "download"
	ActivityDeleteRemote = "delete-remote"
	ActivityDeleteLocal  = "delete-local"
	ActivityConflict     = "conflict"
)

// maxActivity is how many recent changes the engine remembers (in memory
// only; the log file has the full history).
const maxActivity = 20

// Activity is one file a sync pass changed.
type Activity struct {
	At   time.Time `json:"at"`
	Pair string    `json:"pair"`
	Kind string    `json:"kind"`
	Path string    `json:"path"`
}

// Activity returns the most recent changes, newest first.
func (e *Engine) Activity() []Activity {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Activity, len(e.activity))
	for i, entry := range e.activity {
		out[len(out)-1-i] = entry
	}
	return out
}

func (e *Engine) recordActivity(pairID, kind, rel string) {
	e.mu.Lock()
	e.activity = append(e.activity, Activity{At: time.Now(), Pair: pairID, Kind: kind, Path: rel})
	if len(e.activity) > maxActivity {
		e.activity = e.activity[len(e.activity)-maxActivity:]
	}
	e.mu.Unlock()
	e.setStatus(func(s *Status) { s.Changes++ })
}
