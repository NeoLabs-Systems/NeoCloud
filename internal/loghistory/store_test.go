package loghistory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorePersistsRedactedEntriesAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	writer := store.Writer(&stderr)
	line := `{"time":"2026-11-27T10:00:00Z","level":"ERROR","msg":"request failed Bearer abc.def","token":"sensitive","userId":7}` + "\n"
	if _, err = writer.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "sensitive") {
		t.Fatalf("stderr should retain the original diagnostic record: %s", stderr.String())
	}
	if got := store.Entries(); len(got) != 1 || got[0].Type != "error" || strings.Contains(got[0].Message, "sensitive") || strings.Contains(got[0].Message, "abc.def") || !strings.Contains(got[0].Message, `"token":"[redacted]"`) {
		t.Fatalf("log history was not redacted: %#v", got)
	}

	restarted, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.Entries(); len(got) != 1 || got[0] != store.Entries()[0] {
		t.Fatalf("persisted history = %#v; want %#v", got, store.Entries())
	}
	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("log mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestStoreBoundsHistoryToNewestThousandEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	writer := store.Writer(nil)
	for i := 0; i < 1150; i++ {
		line, marshalErr := json.Marshal(map[string]any{
			"time": "2026-11-27T10:00:00Z", "level": "INFO", "msg": fmt.Sprintf("entry-%04d", i),
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err = writer.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	entries := store.Entries()
	if len(entries) != maxEntries || entries[0].Message != "entry-0150" || entries[len(entries)-1].Message != "entry-1149" {
		t.Fatalf("bounded entries = count %d, first %q, last %q", len(entries), entries[0].Message, entries[len(entries)-1].Message)
	}
	restarted, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.Entries(); len(got) != maxEntries || got[0].Message != "entry-0150" || got[maxEntries-1].Message != "entry-1149" {
		t.Fatalf("restarted entries = count %d, first %q, last %q", len(got), got[0].Message, got[len(got)-1].Message)
	}
}
