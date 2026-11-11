package sync

import (
	"errors"
	"testing"

	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// Removing the security key mid-run stops the run, keeps the last real
// pair report and leaves the engine locked until the token is back.
func TestLockStopsTheRunningSync(t *testing.T) {
	srv := newFakeServer(t)
	srv.mkdir("Shared")
	h := newHarness(t, srv, config.Both)
	h.mustSync()
	before, _ := h.engine.PairReport(h.pair.ID)

	cfg := h.engine.cfg
	if err := cfg.Update(func(c *config.Config) error {
		c.KeyLock = &config.KeyLock{RPID: "neocloud-lite.local"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.write("a.txt", "a")
	h.write("b.txt", "b")
	uploads := 0
	srv.beforeUpload = func(string, string) {
		uploads++
		if uploads == 1 { // what keylock.Lock does when the key goes away
			cfg.SetUnlockedToken("")
			h.engine.Locked("Locked — security key removed")
		}
	}
	h.sync() // interrupted: its error does not matter
	if uploads != 1 {
		t.Errorf("%d uploads; the run went on after the lock", uploads)
	}
	if st := h.engine.Status(); st.State != StateLocked || st.Message != "Locked — security key removed" {
		t.Errorf("status %+v", st)
	}
	if after, _ := h.engine.PairReport(h.pair.ID); after.LastSync != before.LastSync || after.Error != "" {
		t.Errorf("the interrupted run overwrote the report: %+v", after)
	}
	if err := h.sync(); err != nil || uploads != 1 {
		t.Errorf("a locked engine synced: %v, %d uploads", err, uploads)
	}
	if _, err := h.engine.Client(); !errors.Is(err, ErrLocked) {
		t.Errorf("Client while locked: %v", err)
	}

	cfg.SetUnlockedToken("token")
	h.engine.Unlocked()
	h.mustSync()
	if files := srv.files("Shared"); files["a.txt"] != sha([]byte("a")) || files["b.txt"] != sha([]byte("b")) {
		t.Errorf("after unlocking: %v", files)
	}
}
