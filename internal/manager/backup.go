package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/backups"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/runtimeapp"
	"github.com/NeoLabs-Systems/neolabs-go/cli"
)

const backupUsage = "backup [list | run <id> | verify <id> | snapshots <id> | restore <id> <snapshot> [--bulk] | migrate-storage-class <id>]\n" +
	"       neocloud backup [snapshots | restore <snapshot> [--bulk]] --from <backup folder | target.json>"

type backupTarget struct {
	id        int64
	name      string
	kind      string
	schedule  string
	retention int
	config    map[string]any
	lastRun   string
}

func backupTargets(ctx context.Context, runtime *runtimeapp.Runtime) ([]backupTarget, error) {
	rows, err := runtime.DB.W.QueryContext(ctx, `SELECT t.id,t.name,t.type,t.schedule,t.retention,t.config_enc,
		COALESCE((SELECT r.status||' · '||r.started_at FROM backup_runs r WHERE r.target_id=t.id ORDER BY r.id DESC LIMIT 1),'never run')
		FROM backup_targets t ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []backupTarget
	for rows.Next() {
		var target backupTarget
		var sealed string
		if err := rows.Scan(&target.id, &target.name, &target.kind, &target.schedule, &target.retention, &sealed, &target.lastRun); err != nil {
			return nil, err
		}
		plain, err := runtime.Keys.DecryptValue(sealed)
		if err == nil {
			err = json.Unmarshal([]byte(plain), &target.config)
		}
		if err != nil {
			return nil, fmt.Errorf("The settings of backup target #%d could not be read: %w", target.id, err)
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func findTarget(ctx context.Context, runtime *runtimeapp.Runtime, id int64) (backupTarget, error) {
	targets, err := backupTargets(ctx, runtime)
	if err != nil {
		return backupTarget{}, err
	}
	if i := slices.IndexFunc(targets, func(t backupTarget) bool { return t.id == id }); i >= 0 {
		return targets[i], nil
	}
	return backupTarget{}, errors.New("Backup target not found.")
}

func (m *Manager) cmdBackup(ctx context.Context, args []string) error {
	from, hasFrom := cli.FlagValue(args, "--from")
	if hasFrom && from == "" {
		return inputError("Usage: --from <backup folder | target.json>")
	}
	tier := "Standard"
	if cli.HasFlag(args, "--bulk") {
		tier = "Bulk"
	}
	positional := cli.Positionals(args, "--from")
	action := "list"
	if len(positional) > 0 {
		action, positional = positional[0], positional[1:]
	}
	usage := inputError("Usage: neocloud " + backupUsage)
	if action == "list" {
		if hasFrom || len(positional) > 0 {
			return usage
		}
		return m.listBackupTargets(ctx)
	}
	if !slices.Contains([]string{"run", "verify", "snapshots", "restore", "migrate-storage-class"}, action) ||
		hasFrom && action != "snapshots" && action != "restore" {
		return usage
	}
	var id int64
	if !hasFrom {
		if len(positional) > 0 {
			id, _ = strconv.ParseInt(positional[0], 10, 64)
			positional = positional[1:]
		}
		if id <= 0 {
			suffix := ""
			if action == "restore" {
				suffix = " <snapshot>"
			}
			return inputError("Usage: neocloud backup " + action + " <target-id>" + suffix)
		}
	}
	snapshot := ""
	if action == "restore" {
		if len(positional) != 1 {
			return inputError("Usage: neocloud backup restore <target-id> <snapshot>  (see `neocloud backup snapshots <id>`)")
		}
		snapshot = positional[0]
	} else if len(positional) != 0 {
		return usage
	}
	switch action {
	case "restore":
		return m.restoreBackup(ctx, id, from, snapshot, tier)
	case "snapshots":
		return m.listSnapshots(ctx, id, from)
	}
	runtime, err := m.openRuntime(ctx)
	if err != nil {
		return err
	}
	defer runtime.Close()
	target, err := findTarget(ctx, runtime, id)
	if err != nil {
		return err
	}
	switch action {
	case "run":
		return m.runBackup(ctx, runtime, target)
	case "verify":
		return m.verifyBackup(ctx, runtime, target)
	}
	return m.migrateStorageClass(ctx, runtime, target)
}

func (m *Manager) listBackupTargets(ctx context.Context) error {
	runtime, err := m.openRuntime(ctx)
	if err != nil {
		return err
	}
	defer runtime.Close()
	targets, err := backupTargets(ctx, runtime)
	if err != nil {
		return err
	}
	m.UI.Title("Backup targets", "")
	if len(targets) == 0 {
		m.UI.Info("None yet. Add one in the app under Admin › Backups.")
	}
	for _, target := range targets {
		verified, err := lastVerification(runtime.Config.Paths.StateDir, target.id)
		if err != nil {
			return err
		}
		m.UI.Item("info", fmt.Sprintf("#%d %s", target.id, target.kind), target.name+" · "+target.schedule+" · "+target.lastRun+" · "+verified)
	}
	return nil
}

func lastVerification(stateDir string, targetID int64) (string, error) {
	stored, err := backups.LastVerification(stateDir, targetID)
	if err != nil || stored == nil {
		return "never verified", err
	}
	var result struct {
		OK         bool   `json:"ok"`
		VerifiedAt string `json:"verifiedAt"`
	}
	encoded, err := json.Marshal(stored)
	if err == nil {
		err = json.Unmarshal(encoded, &result)
	}
	if err != nil {
		return "", err
	}
	outcome := "FAILED"
	if result.OK {
		outcome = "ok"
	}
	return "verified " + outcome + " · " + result.VerifiedAt, nil
}

func newRunner(runtime *runtimeapp.Runtime, version string) (*backups.Runner, error) {
	paths := runtime.Config.Paths
	return backups.New(runtime.DB, runtime.Keys, paths.BlobDir, paths.UploadTmpDir, paths.BackupDir, version)
}

func (m *Manager) runBackup(ctx context.Context, runtime *runtimeapp.Runtime, target backupTarget) error {
	runner, err := newRunner(runtime, m.Version)
	if err != nil {
		return err
	}
	runner.StartManual(ctx)
	defer runner.Close()
	m.UI.Title(fmt.Sprintf("Backing up to target #%d", target.id), "")
	run, err := runner.RunTarget(target.id, target.kind, target.config, target.retention)
	if err != nil {
		return err
	}
	runner.Wait()
	var status, snapshot, message string
	var uploaded int64
	if err := runtime.DB.W.QueryRowContext(context.WithoutCancel(ctx),
		`SELECT status,COALESCE(snapshot,''),blobs_uploaded,COALESCE(error,'') FROM backup_runs WHERE id=?`, run).
		Scan(&status, &snapshot, &uploaded, &message); err != nil {
		return err
	}
	if status != "ok" {
		if message == "" {
			message = "Backup failed."
		}
		return errors.New(message)
	}
	m.UI.OK(fmt.Sprintf("Snapshot %s: %d new blob(s) uploaded.", snapshot, uploaded))
	return nil
}

func (m *Manager) verifyBackup(ctx context.Context, runtime *runtimeapp.Runtime, target backupTarget) error {
	runner, err := newRunner(runtime, m.Version)
	if err != nil {
		return err
	}
	store, err := backups.NewTarget(target.kind, target.config)
	if err != nil {
		return err
	}
	m.UI.Title(fmt.Sprintf("Verifying the newest snapshot on target #%d", target.id), "")
	result := runner.VerifyTargetSnapshot(ctx, store)
	if err := backups.RecordVerification(runtime.Config.Paths.StateDir, target.id, result); err != nil {
		return err
	}
	if !result.OK {
		if result.Error == "" {
			result.Error = "Verification failed."
		}
		return errors.New(result.Error)
	}
	if result.Mode == "metadata" {
		m.UI.OK(fmt.Sprintf("Snapshot %s: checked without reading (archived): database object present with the right size, all %d blob(s) present.", result.Snapshot, result.BlobsChecked))
		return nil
	}
	m.UI.OK(fmt.Sprintf("Snapshot %s: database %s, all %d blob(s) present.", result.Snapshot, result.Database, result.BlobsChecked))
	return nil
}

func (m *Manager) migrateStorageClass(ctx context.Context, runtime *runtimeapp.Runtime, target backupTarget) error {
	if target.kind != "s3" {
		return errors.New("Only S3 targets have a storage class.")
	}
	text := func(key string) string { value, _ := target.config[key].(string); return value }
	migrator, err := backups.NewStorageClassMigrator(runtime.Config.Paths.StateDir)
	if err != nil {
		return err
	}
	defer migrator.Close()
	m.UI.Title(fmt.Sprintf("Moving existing backups on target #%d to its storage class", target.id), "")
	if err := migrator.Start(target.id, backups.S3Config{
		Endpoint: text("endpoint"), Region: text("region"), Bucket: text("bucket"),
		AccessKeyID: text("accessKeyId"), SecretAccessKey: text("secretAccessKey"), Prefix: text("prefix"),
		ForcePathStyle: target.config["forcePathStyle"] == true, StorageClass: text("storageClass"),
	}); err != nil {
		return err
	}
	var done int64 = -1
	for {
		status, err := migrator.Status(target.id)
		if err != nil {
			return err
		}
		// The status arrives as the API's JSON shape.
		raw, err := json.Marshal(status)
		if err != nil {
			return err
		}
		var progress backups.StorageClassMigration
		if err := json.Unmarshal(raw, &progress); err != nil {
			return err
		}
		if progress.Status != "running" {
			if progress.Status != "ok" {
				if message, _ := progress.Error.(string); message != "" {
					return errors.New(message)
				}
				return errors.New("Moving the backups failed.")
			}
			m.UI.OK(fmt.Sprintf("%d object(s) moved to %s; %d were already there.", progress.ObjectsDone, progress.StorageClass, progress.ObjectsSkipped))
			if progress.ObjectsArchived > 0 {
				m.UI.Warn(fmt.Sprintf("%d object(s) are in GLACIER or DEEP_ARCHIVE already and were left as they are (moving them needs a restore first).", progress.ObjectsArchived))
			}
			return nil
		}
		if progress.ObjectsDone != done {
			done = progress.ObjectsDone
			total, bytes := "?", "?"
			if progress.ObjectsTotal != nil {
				total = strconv.FormatInt(*progress.ObjectsTotal, 10)
			}
			if progress.BytesTotal != nil {
				bytes = byteSize(uint64(*progress.BytesTotal))
			}
			m.UI.Info(fmt.Sprintf("%d/%s object(s), %s of %s", done, total, byteSize(uint64(progress.BytesDone)), bytes))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// backupSource opens what a restore or listing reads: a target of this
// install's database, or --from for a machine whose database doesn't know it.
func (m *Manager) backupSource(ctx context.Context, id int64, from string) (backups.Target, string, error) {
	if from != "" {
		absolute, err := filepath.Abs(from)
		if err != nil {
			return nil, "", err
		}
		target, err := backups.OpenTargetSpec(absolute)
		return target, absolute, err
	}
	runtime, err := m.openRuntime(ctx)
	if err != nil {
		return nil, "", err
	}
	defer runtime.Close()
	found, err := findTarget(ctx, runtime, id)
	if err != nil {
		return nil, "", err
	}
	target, err := backups.NewTarget(found.kind, found.config)
	return target, fmt.Sprintf("target #%d", id), err
}

func (m *Manager) listSnapshots(ctx context.Context, id int64, from string) error {
	target, label, err := m.backupSource(ctx, id, from)
	if err != nil {
		return err
	}
	names, err := backups.ListTargetSnapshots(ctx, target)
	if err != nil {
		return err
	}
	m.UI.Title("Snapshots on "+label, "")
	if len(names) == 0 {
		m.UI.Info("No finished snapshots yet.")
	}
	for _, name := range names {
		m.UI.Info(name)
	}
	return nil
}

func (m *Manager) restoreBackup(ctx context.Context, id int64, from, snapshot, tier string) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if healthy(ctx, cfg.Port) {
		return errors.New("Stop NeoCloud first (`neocloud stop`); a restore replaces the database.")
	}
	keys, err := cryptox.NewKeyring(cfg.MasterKeyHex)
	if err != nil {
		return fmt.Errorf("Put the original NEOCLOUD_MASTER_KEY into %s first: %w", cfg.Paths.EnvFile, err)
	}
	target, label, err := m.backupSource(ctx, id, from)
	if err != nil {
		return err
	}
	if err := cfg.Paths.Ensure(); err != nil {
		return err
	}
	if snapshot == "latest" {
		names, err := backups.ListTargetSnapshots(ctx, target)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return errors.New("No finished snapshots yet.")
		}
		snapshot = names[0]
		// Listing closes the target; open it again.
		if target, label, err = m.backupSource(ctx, id, from); err != nil {
			return err
		}
	}
	m.UI.Title("Restoring "+snapshot+" from "+label, "")
	result, err := backups.RestoreSnapshot(ctx, backups.RestoreOptions{
		DatabaseFile: cfg.Paths.DatabaseFile, BlobDir: cfg.Paths.BlobDir, PIDFile: cfg.Paths.PIDFile,
		Target: target, Snapshot: snapshot, Tier: tier, Port: cfg.Port, Keys: keys,
	})
	if err != nil {
		return err
	}
	if result.Waiting {
		hours := "3–5 hours (Standard)"
		if tier == "Bulk" {
			hours = "5–12 hours (Bulk)"
		}
		if result.Requested > 0 {
			m.UI.Warn(fmt.Sprintf("Restore requested for %d object(s); Glacier takes %s.", result.Requested, hours))
		}
		if result.Pending > 0 {
			m.UI.Info(fmt.Sprintf("%d object(s) are still being restored by Glacier.", result.Pending))
		}
		m.UI.Info("Nothing here has changed. Run the same command again later; it continues once every object is available (for 7 days).")
		return nil
	}
	if result.Previous != "" {
		m.UI.Info("Database restored. The previous one is at " + result.Previous + ".")
	}
	m.UI.OK("Restored. Start NeoCloud again with " + m.UI.Command("neocloud start") + ".")
	return nil
}
