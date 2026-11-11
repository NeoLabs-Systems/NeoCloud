package sync

import (
	"testing"
	"time"
)

func TestConflictName(t *testing.T) {
	at := time.Date(2026, 3, 7, 9, 5, 0, 0, time.UTC)
	cases := map[string]string{
		"report.docx":    "report (conflict MacBook 2026-03-07 0905).docx",
		"archive.tar.gz": "archive.tar (conflict MacBook 2026-03-07 0905).gz",
		"README":         "README (conflict MacBook 2026-03-07 0905)",
		".bashrc":        ".bashrc (conflict MacBook 2026-03-07 0905)",
	}
	for name, want := range cases {
		if got := ConflictName(name, "MacBook", at); got != want {
			t.Errorf("ConflictName(%q) = %q, want %q", name, got, want)
		}
	}
	if got := ConflictName("a.txt", `Neo's PC: \/*`, at); got != "a (conflict Neo's PC- --- 2026-03-07 0905).txt" {
		t.Errorf("device name not cleaned: %q", got)
	}
	if got := ConflictName("a.txt", "  ", at); got != "a (conflict device 2026-03-07 0905).txt" {
		t.Errorf("empty device: %q", got)
	}
}

func TestUniqueName(t *testing.T) {
	taken := map[string]bool{"a.txt": true, "a 2.txt": true}
	if got := uniqueName("a.txt", func(n string) bool { return taken[n] }); got != "a 3.txt" {
		t.Errorf("uniqueName = %q", got)
	}
	if got := uniqueName("b.txt", func(n string) bool { return taken[n] }); got != "b.txt" {
		t.Errorf("uniqueName = %q", got)
	}
}
