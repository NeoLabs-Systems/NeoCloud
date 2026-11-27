package backups

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// The newest verification of each target is kept between restarts and shown
// with the target.

const verificationStateFile = "backup-verify.json"

var verificationMu sync.Mutex

// MarshalJSON writes what a check did not reach as null.
func (v SnapshotVerification) MarshalJSON() ([]byte, error) {
	type plain SnapshotVerification
	orNull := func(value string) *string {
		if value == "" {
			return nil
		}
		return &value
	}
	return json.Marshal(struct {
		plain
		Snapshot *string `json:"snapshot"`
		Database *string `json:"database"`
		Error    *string `json:"error"`
	}{plain(v), orNull(v.Snapshot), orNull(v.Database), orNull(v.Error)})
}

func readVerifications(stateDir string) (map[string]json.RawMessage, error) {
	all := map[string]json.RawMessage{}
	data, err := os.ReadFile(filepath.Join(stateDir, verificationStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return all, nil
	}
	if err != nil {
		return nil, err
	}
	// An unreadable document starts over, as an absent one does.
	if json.Unmarshal(data, &all) != nil || all == nil {
		return map[string]json.RawMessage{}, nil
	}
	return all, nil
}

// RecordVerification stores result as the target's last verification.
func RecordVerification(stateDir string, targetID int64, result any) error {
	verificationMu.Lock()
	defer verificationMu.Unlock()
	all, err := readVerifications(stateDir)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	all[strconv.FormatInt(targetID, 10)] = encoded
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(stateDir, ".backup-verify-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(data); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary.Name(), filepath.Join(stateDir, verificationStateFile))
}

// LastVerification is the target's stored verification, or nil.
func LastVerification(stateDir string, targetID int64) (any, error) {
	verificationMu.Lock()
	defer verificationMu.Unlock()
	all, err := readVerifications(stateDir)
	if err != nil {
		return nil, err
	}
	if value, ok := all[strconv.FormatInt(targetID, 10)]; ok {
		return value, nil
	}
	return nil, nil
}
