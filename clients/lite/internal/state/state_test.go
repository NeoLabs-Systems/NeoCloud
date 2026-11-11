package state

import "testing"

func TestEntriesAndMove(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, e := range []Entry{
		{Path: "docs", NodeID: "1", Type: "folder"},
		{Path: "docs/a.txt", NodeID: "2", Type: "file", Etag: "aa", Size: 1, LocalMtime: 5, LocalSize: 1, LocalHash: "aa"},
		{Path: "docs/sub/b.txt", NodeID: "3", Type: "file", Etag: "bb"},
		{Path: "docsx/c.txt", NodeID: "4", Type: "file"}, // shares a prefix, not a parent
		{Path: "Über/d.txt", NodeID: "5", Type: "file"},
	} {
		if err := db.Put("p1", e); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Put("p2", Entry{Path: "docs", NodeID: "9", Type: "folder"}); err != nil {
		t.Fatal(err)
	}

	if err := db.Move("p1", "docs", "papers"); err != nil {
		t.Fatal(err)
	}
	if err := db.Move("p1", "Über", "Öl"); err != nil {
		t.Fatal(err)
	}
	got, err := db.Entries("p1")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"papers", "papers/a.txt", "papers/sub/b.txt", "docsx/c.txt", "Öl/d.txt"} {
		if _, ok := got[path]; !ok {
			t.Errorf("missing %s after move: %v", path, got)
		}
	}
	if len(got) != 5 {
		t.Errorf("got %d rows, want 5", len(got))
	}
	if a := got["papers/a.txt"]; a.Etag != "aa" || a.LocalMtime != 5 || a.State != Synced {
		t.Errorf("row content lost in move: %+v", a)
	}
	other, _ := db.Entries("p2")
	if _, ok := other["docs"]; !ok {
		t.Error("move leaked into another pair")
	}
}

func TestCursorUploadsAndReset(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if c, _ := db.Cursor("p"); c != 0 {
		t.Errorf("fresh cursor = %d", c)
	}
	db.SetCursor("p", 42)
	if c, _ := db.Cursor("p"); c != 42 {
		t.Errorf("cursor = %d", c)
	}

	db.SetPendingUpload("p", "big.bin", Upload{ID: "u1", SHA256: "h", Size: 10})
	if u, ok, _ := db.PendingUpload("p", "big.bin"); !ok || u.ID != "u1" {
		t.Errorf("pending upload = %+v %v", u, ok)
	}
	db.ClearPendingUpload("p", "big.bin")
	if _, ok, _ := db.PendingUpload("p", "big.bin"); ok {
		t.Error("upload not cleared")
	}

	db.Put("p", Entry{Path: "x", NodeID: "1", Type: "file"})
	if err := db.Reset(); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.Entries("p")
	c, _ := db.Cursor("p")
	if len(rows) != 0 || c != 0 {
		t.Errorf("reset left %d rows, cursor %d", len(rows), c)
	}
}
