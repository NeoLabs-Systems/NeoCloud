package handoff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunMaintenanceExpiresOffersReleasesBlobReferencesAndPrunesHistory(t *testing.T) {
	db, h, _, blobDir := setup(t)
	const (
		uniqueBlob = "11111111-1111-4111-8111-111111111111"
		sharedBlob = "22222222-2222-4222-8222-222222222222"
		multiBlob  = "33333333-3333-4333-8333-333333333333"
	)
	for _, blob := range []struct {
		id   string
		size int64
		refs int
	}{
		{uniqueBlob, 5, 1},
		{sharedBlob, 7, 2},
		{multiBlob, 11, 2},
	} {
		if _, err := db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,1,?,'hash',1,?)`, blob.id, blob.size, blob.refs); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.W.Exec(`UPDATE users SET used_bytes=23 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for _, offer := range []struct {
		id, status, expires, updated string
	}{
		{"expired-unique", "pending", "datetime('now','-1 hour')", "datetime('now')"},
		{"expired-shared", "accepted", "datetime('now','-1 hour')", "datetime('now')"},
		{"expired-multiple", "uploading", "datetime('now','-1 hour')", "datetime('now')"},
		{"still-open", "pending", "datetime('now','+1 hour')", "datetime('now')"},
		{"old-terminal", "delivered", "datetime('now','-5 days')", "datetime('now','-4 days')"},
	} {
		query := `INSERT INTO handoff_offers(id,user_id,sender_key,sender_name,target_key,target_name,status,total_size,expires_at,updated_at)
			VALUES(?,1,'d:1','Laptop','d:2','Phone',?,0,` + offer.expires + `,` + offer.updated + `)`
		if _, err := db.W.Exec(query, offer.id, offer.status); err != nil {
			t.Fatal(err)
		}
	}
	for i, file := range []struct {
		offerID, blobID string
	}{
		{"expired-unique", uniqueBlob},
		{"expired-shared", sharedBlob},
		{"expired-multiple", multiBlob},
		{"expired-multiple", multiBlob},
	} {
		if _, err := db.W.Exec(`INSERT INTO handoff_files(offer_id,position,name,size,blob_id) VALUES(?,?,?,1,?)`, file.offerID, i, "file", file.blobID); err != nil {
			t.Fatal(err)
		}
	}
	for _, blobID := range []string{uniqueBlob, sharedBlob, multiBlob} {
		path := filepath.Join(blobDir, "1", blobID[:2], blobID)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("encrypted"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	expired, forgotten, err := h.RunMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if expired != 3 || forgotten != 1 {
		t.Fatalf("maintenance expired=%d forgotten=%d", expired, forgotten)
	}
	for id, want := range map[string]string{
		"expired-unique": "expired", "expired-shared": "expired", "expired-multiple": "expired", "still-open": "pending",
	} {
		var got string
		if err = db.W.QueryRow(`SELECT status FROM handoff_offers WHERE id=?`, id).Scan(&got); err != nil || got != want {
			t.Fatalf("offer %s status=%q err=%v, want %q", id, got, err, want)
		}
	}
	var count int
	if err = db.W.QueryRow(`SELECT COUNT(*) FROM handoff_offers WHERE id='old-terminal'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old terminal count=%d err=%v", count, err)
	}
	for _, check := range []struct {
		id       string
		wantRefs int
		wantFile bool
	}{
		{uniqueBlob, 0, false},
		{sharedBlob, 1, true},
		{multiBlob, 0, false},
	} {
		var refs int
		err = db.W.QueryRow(`SELECT ref_count FROM blobs WHERE id=?`, check.id).Scan(&refs)
		if check.wantRefs == 0 {
			if err == nil {
				t.Fatalf("blob %s still exists with refs=%d", check.id, refs)
			}
		} else if err != nil || refs != check.wantRefs {
			t.Fatalf("blob %s refs=%d err=%v, want %d", check.id, refs, err, check.wantRefs)
		}
		_, statErr := os.Stat(filepath.Join(blobDir, "1", check.id[:2], check.id))
		if check.wantFile && statErr != nil || !check.wantFile && !os.IsNotExist(statErr) {
			t.Fatalf("blob %s file stat err=%v, want exists=%v", check.id, statErr, check.wantFile)
		}
	}
	var used int64
	if err = db.W.QueryRow(`SELECT used_bytes FROM users WHERE id=1`).Scan(&used); err != nil || used != 7 {
		t.Fatalf("used_bytes=%d err=%v, want 7", used, err)
	}
}
