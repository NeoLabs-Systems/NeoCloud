package cryptox

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestGetUserDEKCreatesOnceAcrossConcurrentCalls(t *testing.T) {
	db, err := sql.Open("sqlite", "file:user-key-race?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, wrapped_dek TEXT); INSERT INTO users (id) VALUES (42)`); err != nil {
		t.Fatal(err)
	}
	keyring, err := NewKeyring("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	const count = 12
	keys := make([][]byte, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys[i], errs[i] = keyring.GetUserDEK(context.Background(), db, 42)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if string(keys[i]) != string(keys[0]) {
			t.Fatalf("call %d returned a different data key", i)
		}
	}
	var wrapped string
	if err := db.QueryRow(`SELECT wrapped_dek FROM users WHERE id = 42`).Scan(&wrapped); err != nil {
		t.Fatal(err)
	}
	if opened, err := keyring.UnwrapDEK(42, wrapped); err != nil || string(opened) != string(keys[0]) {
		t.Fatalf("stored key does not match returned key: %v", err)
	}
}

func TestVerifyMasterKeyDoesNotMutateAndRejectsWrongKey(t *testing.T) {
	db, err := sql.Open("sqlite", "file:verify-master-key?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, wrapped_dek TEXT); INSERT INTO users (id) VALUES (42)`); err != nil {
		t.Fatal(err)
	}
	keyring, _ := NewKeyring("0000000000000000000000000000000000000000000000000000000000000000")
	if _, err := keyring.GetUserDEK(context.Background(), db, 42); err != nil {
		t.Fatal(err)
	}
	wrongKey, _ := NewKeyring("1111111111111111111111111111111111111111111111111111111111111111")
	if err := wrongKey.VerifyMasterKey(context.Background(), db); err == nil {
		t.Fatal("accepted a different master key")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE wrapped_dek IS NOT NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("master-key verification changed data: count=%d err=%v", count, err)
	}
	if err := keyring.VerifyMasterKey(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.GetUserDEK(context.Background(), db, 999); err == nil {
		t.Fatal("created a data key for a missing account")
	}
}
