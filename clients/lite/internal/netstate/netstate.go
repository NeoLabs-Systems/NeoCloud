// Package netstate tells whether the computer is on a metered connection
// or in battery saver, so pairs can pause (pause_on_metered,
// pause_on_battery_saver). Every check is best effort: when the system
// cannot say, the answer is "no" and syncing goes on.
package netstate

import (
	"sync"
	"time"
)

// Conditions are what the pause rules look at.
type Conditions struct {
	Metered      bool
	BatterySaver bool
}

// cacheFor is how long an answer is reused; some checks start a process.
const cacheFor = time.Minute

var (
	mu      sync.Mutex
	last    Conditions
	checked time.Time
)

// Current returns the conditions, checked at most once a minute.
func Current() Conditions {
	mu.Lock()
	defer mu.Unlock()
	if time.Since(checked) > cacheFor {
		last = Conditions{Metered: metered(), BatterySaver: batterySaver()}
		checked = time.Now()
	}
	return last
}
