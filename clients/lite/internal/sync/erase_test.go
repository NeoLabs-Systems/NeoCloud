package sync

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEraseKeepsTheFolderAndEmptiesIt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, ".hidden", "b"), "b")
	writeFile(t, filepath.Join(root, "x", "y", "z.txt"), "z")
	if err := eraseFolderContents(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("root should exist and be empty: %v %v", entries, err)
	}
}

func TestEraseNeverFollowsLinksOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root, outside := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "keep.txt"), "keep")
	writeFile(t, filepath.Join(outside, "dir", "keep2.txt"), "keep")
	if err := os.Symlink(outside, filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "keep.txt"), filepath.Join(root, "filelink")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "sub", "f"), "f")
	if err := os.Symlink(outside, filepath.Join(root, "sub", "inner")); err != nil {
		t.Fatal(err)
	}
	if err := eraseFolderContents(root); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"keep.txt", "dir/keep2.txt"} {
		if _, err := os.Stat(filepath.Join(outside, p)); err != nil {
			t.Fatalf("%s outside the folder was deleted: %v", p, err)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("links should be gone: %v", entries)
	}
}

func TestEraseRefusesRisky(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Sync")
	writeFile(t, filepath.Join(root, "f"), "f")
	writeFile(t, filepath.Join(base, "neighbour"), "n")

	if err := eraseFolderContents("relative/path"); err == nil {
		t.Error("a relative path must be refused")
	}
	if err := eraseFolderContents(filepath.VolumeName(base) + string(filepath.Separator)); err == nil {
		t.Error("a drive root must be refused")
	}
	if err := eraseFolderContents(base, root); err == nil {
		t.Error("a folder containing a protected path must be refused")
	}
	if err := eraseFolderContents(root, root); err == nil {
		t.Error("a protected folder itself must be refused")
	}
	if _, err := os.Stat(filepath.Join(base, "neighbour")); err != nil {
		t.Error("neighbour was touched")
	}
	if err := eraseFolderContents(filepath.Join(base, "missing")); err != nil {
		t.Errorf("a missing folder is nothing to erase: %v", err)
	}
	// A file in place of the folder.
	if err := eraseFolderContents(filepath.Join(base, "neighbour")); err == nil {
		t.Error("a plain file must be refused")
	}
}

func TestEraseRefusesALinkAsRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	target, base := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(target, "precious"), "p")
	link := filepath.Join(base, "Sync")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := eraseFolderContents(link); err == nil {
		t.Error("a linked sync folder must be refused")
	}
	if _, err := os.Stat(filepath.Join(target, "precious")); err != nil {
		t.Error("the link's target was erased")
	}
}
