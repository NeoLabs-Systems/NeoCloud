package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSafeNameStripsEverythingDangerous(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":  "_.._etc_passwd",
		`..\..\win.ini`:     "_.._win.ini",
		"a/b/c.txt":         "a_b_c.txt",
		"inv‮oice.exe":      "invoice.exe",
		"bad\x00name\n.txt": "badname.txt",
		"report. . ":        "report",
		".hidden":           "hidden",
		"   ":               "file",
		"..":                "file",
		"":                  "file",
		"nul.txt":           "_nul.txt",
		"ok name (1).pdf":   "ok name (1).pdf",
		"zero​width.md":     "zerowidth.md",
	}
	for in, want := range cases {
		if got := safeName(in, false); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeNameWindowsCharacters(t *testing.T) {
	if got := safeName(`a:b*c?"d<e>f|g.txt`, true); got != "a_b_c__d_e_f_g.txt" {
		t.Errorf("got %q", got)
	}
	if got := safeName(`a:b.txt`, false); got != "a:b.txt" {
		t.Errorf("other systems keep the colon: %q", got)
	}
}

func TestSafeNameCutsLongNamesByBytes(t *testing.T) {
	name := safeName(strings.Repeat("ä", 300)+".pdf", false)
	if len(name) > maxNameBytes || !strings.HasSuffix(name, ".pdf") {
		t.Errorf("length %d, %q", len(name), name[len(name)-6:])
	}
	for _, r := range name {
		if r == 0xfffd {
			t.Fatal("a character was split")
		}
	}
}

func TestReserveNeverOverwritesAndStaysInside(t *testing.T) {
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Reserve(folder, "a.txt")
	if err != nil || filepath.Base(first) != "a (1).txt" {
		t.Fatalf("got %q, %v", first, err)
	}
	second, _ := Reserve(folder, "a.txt")
	if filepath.Base(second) != "a (2).txt" {
		t.Errorf("second: %q", second)
	}
	data, _ := os.ReadFile(filepath.Join(folder, "a.txt"))
	if string(data) != "mine" {
		t.Error("an existing file was changed")
	}
	for _, evil := range []string{"../escape.txt", "..\\escape.txt", "/etc/passwd", "sub/../../x", "a/../../../b"} {
		path, err := Reserve(folder, evil)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(path) != folder {
			t.Errorf("%q escaped to %q", evil, path)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(folder))
	for _, e := range entries {
		if e.Name() == "escape.txt" {
			t.Error("a file landed outside the folder")
		}
	}
}

func TestReserveSurvivesRacingSends(t *testing.T) {
	folder := t.TempDir()
	var wg sync.WaitGroup
	paths := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := Reserve(folder, "same.bin")
			if err != nil {
				t.Error(err)
			}
			paths <- path
		}()
	}
	wg.Wait()
	close(paths)
	seen := map[string]bool{}
	for path := range paths {
		if seen[path] {
			t.Fatalf("%s handed out twice", path)
		}
		seen[path] = true
	}
}

func TestReserveFailsWhenFolderIsMissing(t *testing.T) {
	if _, err := Reserve(filepath.Join(t.TempDir(), "gone"), "a.txt"); err == nil {
		t.Error("expected an error for a missing folder")
	}
}
