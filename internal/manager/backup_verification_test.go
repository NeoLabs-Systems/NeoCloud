package manager

import (
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/backups"
)

func TestBackupListShowsTheStoredVerification(t *testing.T) {
	state := t.TempDir()
	if got, err := lastVerification(state, 1); err != nil || got != "never verified" {
		t.Fatalf("before any check: %q %v", got, err)
	}
	if err := backups.RecordVerification(state, 1, backups.SnapshotVerification{OK: true, VerifiedAt: "2026-11-27T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := backups.RecordVerification(state, 2, backups.SnapshotVerification{Error: "missing", VerifiedAt: "2026-11-28T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]string{1: "verified ok · 2026-11-27T10:00:00Z", 2: "verified FAILED · 2026-11-28T10:00:00Z", 3: "never verified"} {
		if got, err := lastVerification(state, id); err != nil || got != want {
			t.Errorf("target %d = %q %v, want %q", id, got, err, want)
		}
	}
}
