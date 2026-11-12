package notify

import (
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

func TestMessages(t *testing.T) {
	syncing := sync.Status{State: sync.StateSyncing}
	done := sync.Status{State: sync.StateIdle, Changes: 3}
	failed := sync.Status{State: sync.StateError, Message: "server unreachable"}
	asked := sync.Status{State: sync.StateIdle, PairState: map[string]sync.PairState{"p": {NewFolders: []sync.NewFolder{{Path: "Videos"}}}}}

	if got := messages(syncing, done, 0, config.NotifyErrors); len(got) != 0 {
		t.Errorf("errors level told about a normal run: %v", got)
	}
	if got := messages(syncing, done, 0, config.NotifyAll); len(got) != 1 || got[0].Body != "3 files synced" {
		t.Errorf("all level: %v", got)
	}
	if got := messages(syncing, failed, 0, config.NotifyErrors); len(got) != 1 || got[0].Body != "server unreachable" {
		t.Errorf("error: %v", got)
	}
	if got := messages(failed, failed, 0, config.NotifyErrors); len(got) != 0 {
		t.Errorf("the same error twice: %v", got)
	}
	if got := messages(syncing, done, 2, config.NotifyErrors); len(got) != 1 {
		t.Errorf("conflicts: %v", got)
	}
	if got := messages(done, asked, 0, config.NotifyErrors); len(got) != 1 || got[0].Title != "New folder in NeoCloud" {
		t.Errorf("new folder: %v", got)
	}
	if got := messages(asked, asked, 0, config.NotifyErrors); len(got) != 0 {
		t.Errorf("the same new folder twice: %v", got)
	}
	if got := messages(syncing, failed, 5, config.NotifyOff); len(got) != 0 {
		t.Errorf("off: %v", got)
	}
}
