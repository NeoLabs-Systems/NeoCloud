package sync

import (
	"fmt"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

func TestActivityRecordsTransfersNewestFirst(t *testing.T) {
	srv := newFakeServer(t)
	srv.put("Shared/remote.txt", "from the server")
	h := newHarness(t, srv, config.Both)
	h.write("local.txt", "from here")
	h.mustSync()

	kinds := map[string]string{}
	for _, entry := range h.engine.Activity() {
		if entry.Pair != h.pair.ID {
			t.Errorf("entry for pair %q", entry.Pair)
		}
		kinds[entry.Path] = entry.Kind
	}
	if kinds["local.txt"] != ActivityUpload || kinds["remote.txt"] != ActivityDownload {
		t.Errorf("activity %v", kinds)
	}
	if got := h.engine.Status().Changes; got != 2 {
		t.Errorf("changes in the run: %d", got)
	}

	for i := 0; i < maxActivity+5; i++ {
		h.engine.recordActivity(h.pair.ID, ActivityUpload, fmt.Sprintf("f%d", i))
	}
	entries := h.engine.Activity()
	if len(entries) != maxActivity || entries[0].Path != fmt.Sprintf("f%d", maxActivity+4) {
		t.Errorf("kept %d, newest %q", len(entries), entries[0].Path)
	}
}
