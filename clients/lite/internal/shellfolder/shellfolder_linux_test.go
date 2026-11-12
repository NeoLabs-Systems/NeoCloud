package shellfolder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBookmarks(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	file := filepath.Join(config, "gtk-3.0", "bookmarks")
	os.MkdirAll(filepath.Dir(file), 0o700)
	os.WriteFile(file, []byte("file:///home/u/Music\n"), 0o644)

	dir := "/home/u/My NeoCloud"
	for range 2 { // adding twice keeps one line
		if err := pin(dir, ""); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(file)
	if want := "file:///home/u/Music\nfile:///home/u/My%20NeoCloud My NeoCloud\n"; string(data) != want {
		t.Errorf("after pin:\n%q", data)
	}
	if err := unpin(dir); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(file)
	if string(data) != "file:///home/u/Music\n" {
		t.Errorf("after unpin:\n%q", data)
	}
}
