package tray

import (
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

// The status line uses the app window's words.
func TestStatusLine(t *testing.T) {
	at := time.Date(2026, 1, 2, 14, 5, 0, 0, time.Local)
	for _, tc := range []struct {
		status sync.Status
		want   string
	}{
		{sync.Status{State: sync.StateIdle}, "Up to date"},
		{sync.Status{State: sync.StateIdle, LastSync: at}, "Up to date · 14:05"},
		{sync.Status{State: sync.StateSyncing}, "Syncing…"},
		{sync.Status{State: sync.StateSyncing, Changes: 1}, "Syncing 1 file"},
		{sync.Status{State: sync.StateSyncing, Changes: 3}, "Syncing 3 files"},
		{sync.Status{State: sync.StatePaused, Message: "Paused"}, "Paused"},
		{sync.Status{State: sync.StatePaused}, "Paused"},
		{sync.Status{State: sync.StateError, Message: "dial tcp: connection refused"}, "Needs attention"},
		{sync.Status{State: sync.StateConfirm}, "Needs attention"},
		{sync.Status{State: sync.StateOffline}, "Offline"},
		{sync.Status{State: sync.StateLocked}, "Locked"},
		{sync.Status{State: sync.StateLoggedOut}, "Not signed in"},
		{sync.Status{}, "Starting…"},
	} {
		if got := statusLine(tc.status); got != tc.want {
			t.Errorf("statusLine(%+v) = %q, want %q", tc.status, got, tc.want)
		}
	}
}
