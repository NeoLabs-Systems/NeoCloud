package sync

import (
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

func TestDecide(t *testing.T) {
	type row struct {
		local, remote  Side
		both, up, down Action
	}
	// Every combination that can occur: without a state row each side is
	// Absent or New, with one it is Same, Changed or Deleted.
	table := []row{
		{SideNew, SideAbsent, Upload, Upload, None},
		{SideAbsent, SideNew, Download, None, Download},
		{SideNew, SideNew, Conflict, Upload, Conflict},
		{SideSame, SideSame, None, None, None},
		{SideChanged, SideSame, Upload, Upload, None},
		{SideSame, SideChanged, Download, None, Download},
		{SideChanged, SideChanged, Conflict, Upload, Conflict},
		{SideDeleted, SideSame, DeleteRemote, DeleteRemote, Download},
		{SideSame, SideDeleted, DeleteLocal, Upload, DeleteLocal},
		// An edit beats a delete (docs/clients.md).
		{SideChanged, SideDeleted, Upload, Upload, Forget},
		{SideDeleted, SideChanged, Download, DeleteRemote, Download},
		{SideDeleted, SideDeleted, Forget, Forget, Forget},
	}
	for _, r := range table {
		for direction, want := range map[string]Action{config.Both: r.both, config.Up: r.up, config.Down: r.down} {
			if got := Decide(r.local, r.remote, direction); got != want {
				t.Errorf("Decide(%d, %d, %s) = %s, want %s", r.local, r.remote, direction, got, want)
			}
		}
	}
}

// Upload-only must never write locally and download-only never remotely.
func TestDirectionsNeverWriteTheProtectedSide(t *testing.T) {
	sides := []Side{SideAbsent, SideNew, SideSame, SideChanged, SideDeleted}
	for _, l := range sides {
		for _, r := range sides {
			switch Decide(l, r, config.Up) {
			case Download, DeleteLocal, Conflict:
				t.Errorf("up mode writes locally for (%d, %d)", l, r)
			}
			switch Decide(l, r, config.Down) {
			case Upload, DeleteRemote:
				t.Errorf("down mode writes remotely for (%d, %d)", l, r)
			}
		}
	}
}
