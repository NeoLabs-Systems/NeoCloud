package application

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/admin"
	"github.com/NeoLabs-Systems/neocloud/internal/backups"
	"github.com/google/uuid"
)

func openBackupTarget(kind string, config map[string]any) (backups.Target, error) {
	return backups.NewTarget(kind, config)
}

func probeBackupTarget(ctx context.Context, kind string, config map[string]any) (map[string]any, error) {
	var target backups.Target
	var err error
	if kind == "sftp" {
		port, portErr := sftpConfigPort(config["port"])
		if portErr != nil {
			return nil, portErr
		}
		target, err = backups.NewSFTPTargetForProbe(backups.SFTPConfig{
			Host: stringValue(config["host"]), Port: port, Username: stringValue(config["username"]),
			Password: stringValue(config["password"]), PrivateKey: stringValue(config["privateKey"]),
			Passphrase: stringValue(config["passphrase"]), Path: stringValue(config["path"]),
			HostKey: stringValue(config["hostKey"]),
		})
	} else {
		target, err = openBackupTarget(kind, config)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", admin.ErrBackupConnect, err)
	}
	if err = target.Open(ctx); err != nil {
		return nil, fmt.Errorf("%w: %w", admin.ErrBackupConnect, err)
	}
	defer target.Close()
	if err = writeProbeFile(ctx, target); err != nil {
		return nil, fmt.Errorf("%w: %w", admin.ErrBackupWrite, err)
	}
	updated := make(map[string]any, len(config)+1)
	for key, value := range config {
		updated[key] = value
	}
	if sftpTarget, ok := target.(*backups.SFTPTarget); ok {
		fingerprint := sftpTarget.HostKeyFingerprint()
		if fingerprint == "" {
			return nil, fmt.Errorf("SFTP host fingerprint was not captured")
		}
		updated["hostKey"] = fingerprint
	}
	return updated, nil
}

func sftpConfigPort(value any) (int, error) {
	if value == nil || value == "" {
		return 22, nil
	}
	switch port := value.(type) {
	case int:
		return port, nil
	case int64:
		return int(port), nil
	case float64:
		if port == float64(int(port)) {
			return int(port), nil
		}
	case string:
		return strconv.Atoi(port)
	}
	return 0, fmt.Errorf("SFTP port is invalid")
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func writeProbeFile(ctx context.Context, target backups.Target) error {
	tmp, err := os.MkdirTemp("", "neocloud-backup-target-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	content := []byte("NeoCloud backup target verification\n")
	source := filepath.Join(tmp, "probe")
	if err = os.WriteFile(source, content, 0600); err != nil {
		return err
	}
	key := ".neocloud-backup-test-" + uuid.NewString()
	defer target.Delete(context.Background(), key)
	if err = target.Put(ctx, key, source); err != nil {
		return err
	}
	destination := filepath.Join(tmp, "readback")
	if err = target.Get(ctx, key, destination); err != nil {
		return err
	}
	readback, err := os.ReadFile(destination)
	if err != nil {
		return err
	}
	if !bytes.Equal(readback, content) {
		return os.ErrInvalid
	}
	return nil
}

func listBackupTargetSnapshots(ctx context.Context, kind string, config map[string]any) ([]string, error) {
	target, err := openBackupTarget(kind, config)
	if err != nil {
		return nil, err
	}
	return backups.ListTargetSnapshots(ctx, target)
}

func verifyBackupTargetSnapshot(runner *backups.Runner, ctx context.Context, kind string, config map[string]any) any {
	target, err := openBackupTarget(kind, config)
	if err != nil {
		return backups.SnapshotVerification{Error: "Could not open backup target."}
	}
	return runner.VerifyTargetSnapshot(ctx, target)
}
