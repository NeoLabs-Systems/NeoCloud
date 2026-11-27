package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/publicerr"
)

var backupFields = map[string][]string{
	"local":  {"path"},
	"sftp":   {"host", "port", "username", "password", "privateKey", "passphrase", "path", "hostKey"},
	"s3":     {"endpoint", "region", "bucket", "accessKeyId", "secretAccessKey", "prefix", "forcePathStyle", "storageClass"},
	"webdav": {"url", "username", "password"},
}

var backupSecretFields = map[string]bool{"password": true, "privateKey": true, "passphrase": true, "secretAccessKey": true}

func nullableInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

type backupTarget struct {
	ID               int64          `json:"id"`
	Name             string         `json:"name"`
	Type             string         `json:"type"`
	Config           map[string]any `json:"config"`
	Schedule         string         `json:"schedule"`
	Hour             int            `json:"hour"`
	Weekday          int            `json:"weekday"`
	Retention        int            `json:"retention"`
	Enabled          bool           `json:"enabled"`
	LastRun          any            `json:"lastRun"`
	LastVerification any            `json:"lastVerification"`
	StorageMigration any            `json:"storageMigration"`
}

// BackupRoutes provides target configuration, execution and read-only snapshot inspection.
func (h *Handler) BackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/backups", h.listBackups)
	mux.Handle("POST /api/admin/backups/targets", h.settingsLimiter.Middleware(http.HandlerFunc(h.createBackupTarget)))
	mux.Handle("POST /api/admin/backups/targets/{id}/test", h.settingsLimiter.Middleware(http.HandlerFunc(h.testBackupTarget)))
	mux.Handle("POST /api/admin/backups/targets/{id}/run", h.settingsLimiter.Middleware(http.HandlerFunc(h.startBackupRun)))
	mux.Handle("POST /api/admin/backups/targets/{id}/verify", h.settingsLimiter.Middleware(http.HandlerFunc(h.verifyBackupTarget)))
	mux.Handle("POST /api/admin/backups/targets/{id}/storage-class/migrate", h.settingsLimiter.Middleware(http.HandlerFunc(h.startStorageClassMigration)))
	mux.HandleFunc("GET /api/admin/backups/targets/{id}/snapshots", h.listBackupSnapshots)
	mux.Handle("PUT /api/admin/backups/targets/{id}", h.settingsLimiter.Middleware(http.HandlerFunc(h.updateBackupTarget)))
	mux.HandleFunc("DELETE /api/admin/backups/targets/{id}", h.deleteBackupTarget)
}

func (h *Handler) backupTargetConfig(w http.ResponseWriter, r *http.Request) (int64, string, map[string]any, bool) {
	if _, ok := h.authorize(w, r); !ok {
		return 0, "", nil, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 400, "Invalid target id", "INVALID_ID")
		return 0, "", nil, false
	}
	var typ, encrypted string
	err = h.db.QueryRowContext(r.Context(), `SELECT type,config_enc FROM backup_targets WHERE id=?`, id).Scan(&typ, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "Backup target not found", "NOT_FOUND")
		return 0, "", nil, false
	}
	if err != nil {
		writeError(w, 500, "Could not read backup target.", "INTERNAL_ERROR")
		return 0, "", nil, false
	}
	config, err := h.decryptBackupConfig(encrypted)
	if err != nil {
		writeError(w, 500, "Could not read backup target.", "INTERNAL_ERROR")
		return 0, "", nil, false
	}
	return id, typ, config, true
}

// ErrBackupConnect and ErrBackupWrite mark why a target test failed.
var (
	ErrBackupConnect = errors.New("could not connect to the backup target")
	ErrBackupWrite   = errors.New("could not write to the backup target")
)

const backupWorkerMissing = "Backup worker is not ready."

func (h *Handler) listBackupSnapshots(w http.ResponseWriter, r *http.Request) {
	_, typ, config, ok := h.backupTargetConfig(w, r)
	if !ok {
		return
	}
	if h.onBackupTargetSnapshots == nil {
		writeError(w, 503, backupWorkerMissing, "SERVICE_UNAVAILABLE")
		return
	}
	snapshots, err := h.onBackupTargetSnapshots(r.Context(), typ, config)
	if err != nil {
		writeError(w, 400, "Could not connect: "+h.backupFailure(err), "BACKUP_CONNECT_FAILED")
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"snapshots": snapshots})
}

// backupFailure is the cause as an administrator may read it: one line without
// local paths.
func (h *Handler) backupFailure(err error) string {
	home, _ := os.UserHomeDir()
	text := strings.Join(strings.Fields(err.Error()), " ")
	for _, mark := range []error{ErrBackupConnect, ErrBackupWrite} {
		text = strings.TrimPrefix(text, mark.Error()+": ")
	}
	return publicerr.Sanitize(text, home, "")
}

// verifyBackupTarget checks the newest snapshot; a failed check is reported in
// the result and kept with the target either way.
func (h *Handler) verifyBackupTarget(w http.ResponseWriter, r *http.Request) {
	id, typ, config, ok := h.backupTargetConfig(w, r)
	if !ok {
		return
	}
	if h.onBackupTargetVerify == nil {
		writeError(w, 503, backupWorkerMissing, "SERVICE_UNAVAILABLE")
		return
	}
	result := h.onBackupTargetVerify(r.Context(), typ, config)
	if h.onBackupVerified != nil {
		if err := h.onBackupVerified(id, result); err != nil {
			slog.Error("Could not keep the backup verification", "targetId", id, "error", err)
		}
	}
	httpx.WriteJSON(w, 200, result)
}

func (h *Handler) startStorageClassMigration(w http.ResponseWriter, r *http.Request) {
	id, typ, config, ok := h.backupTargetConfig(w, r)
	if !ok {
		return
	}
	if typ != "s3" {
		writeError(w, 400, "Only S3 targets have a storage class.", "BACKUP_NOT_S3")
		return
	}
	if h.onStorageClassMigrationStart == nil {
		writeError(w, 503, backupWorkerMissing, "SERVICE_UNAVAILABLE")
		return
	}
	err := h.onStorageClassMigrationStart(id, config)
	if errors.Is(err, ErrStorageMigrationRunning) {
		writeError(w, 409, "Existing backups on this target are already being moved.", "MIGRATION_RUNNING")
		return
	}
	if err != nil {
		writeError(w, 400, "Could not connect: "+h.backupFailure(err), "BACKUP_CONNECT_FAILED")
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

func (h *Handler) startBackupRun(w http.ResponseWriter, r *http.Request) {
	id, typ, config, ok := h.backupTargetConfig(w, r)
	if !ok {
		return
	}
	var retention int
	if err := h.db.QueryRowContext(r.Context(), `SELECT retention FROM backup_targets WHERE id=?`, id).Scan(&retention); err != nil {
		writeError(w, 500, "Could not read backup target.", "INTERNAL_ERROR")
		return
	}
	if h.onBackupTargetRun == nil {
		writeError(w, 503, backupWorkerMissing, "SERVICE_UNAVAILABLE")
		return
	}
	runID, err := h.onBackupTargetRun(id, typ, config, retention)
	switch {
	case errors.Is(err, ErrBackupRunning):
		writeError(w, 409, "A backup to this target is already running.", "BACKUP_RUNNING")
	case errors.Is(err, ErrBackupWorkerUnavailable):
		writeError(w, 503, backupWorkerMissing, "SERVICE_UNAVAILABLE")
	case err != nil:
		writeError(w, 500, "Could not start backup.", "INTERNAL_ERROR")
	default:
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"started": true, "runId": runID})
	}
}

// testBackupTarget writes and removes a probe on the target, and pins the SFTP
// host key it saw.
func (h *Handler) testBackupTarget(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.authorize(w, r)
	if !ok {
		return
	}
	id, typ, config, ok := h.backupTargetConfig(w, r)
	if !ok {
		return
	}
	var encrypted string
	if err := h.db.QueryRowContext(r.Context(), `SELECT config_enc FROM backup_targets WHERE id=?`, id).Scan(&encrypted); err != nil {
		writeError(w, 500, "Could not read backup target.", "INTERNAL_ERROR")
		return
	}
	if h.onBackupTargetTest == nil {
		writeError(w, 503, backupWorkerMissing, "SERVICE_UNAVAILABLE")
		return
	}
	verified, err := h.onBackupTargetTest(r.Context(), typ, config)
	if errors.Is(err, ErrBackupWrite) {
		writeError(w, 400, "Connected, but writing failed: "+h.backupFailure(err), "BACKUP_WRITE_FAILED")
		return
	}
	if err != nil {
		writeError(w, 400, "Could not connect: "+h.backupFailure(err), "BACKUP_CONNECT_FAILED")
		return
	}
	fingerprint, err := h.persistBackupHostKey(r, actor, id, typ, encrypted, config, verified)
	if errors.Is(err, ErrBackupTargetChanged) {
		writeError(w, 409, "The backup target changed during verification. Run the test again.", "BACKUP_TARGET_CHANGED")
		return
	}
	if err != nil {
		writeError(w, 500, "Could not save backup target verification.", "INTERNAL_ERROR")
		return
	}
	result := map[string]any{"ok": true}
	if fingerprint != "" {
		result["hostKeyFingerprint"] = fingerprint
	}
	httpx.WriteJSON(w, 200, result)
}

var ErrBackupTargetChanged = errors.New("backup target changed during test")
var ErrStorageMigrationRunning = errors.New("backup storage class migration is already running")

func (h *Handler) persistBackupHostKey(r *http.Request, actorID, targetID int64, typ, previousEncrypted string, previous, verified map[string]any) (string, error) {
	if typ != "sftp" || verified == nil {
		return "", nil
	}
	fingerprint, _ := verified["hostKey"].(string)
	oldFingerprint, _ := previous["hostKey"].(string)
	if fingerprint == "" || fingerprint == oldFingerprint {
		return "", nil
	}
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		return "", errors.New("invalid SFTP host fingerprint")
	}
	previousCopy, verifiedCopy := cloneBackupConfig(previous), cloneBackupConfig(verified)
	delete(previousCopy, "hostKey")
	delete(verifiedCopy, "hostKey")
	if !reflect.DeepEqual(previousCopy, verifiedCopy) {
		return "", errors.New("backup verification changed unrelated configuration")
	}
	verifiedJSON, err := json.Marshal(verified)
	if err != nil {
		return "", err
	}
	sealed, err := h.keys.EncryptValue(string(verifiedJSON))
	if err != nil {
		return "", err
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE backup_targets SET config_enc=? WHERE id=? AND config_enc=?`, sealed, targetID, previousEncrypted)
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", ErrBackupTargetChanged
	}
	if err = h.backupAudit(r, tx, actorID, "backup.target.host_key_pinned", targetID, map[string]any{"hostKeyFingerprint": fingerprint}); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return fingerprint, nil
}

func cloneBackupConfig(config map[string]any) map[string]any {
	clone := make(map[string]any, len(config))
	for key, value := range config {
		clone[key] = value
	}
	return clone
}

func (h *Handler) listBackups(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r); !ok {
		return
	}
	targets, err := h.db.QueryContext(r.Context(), `SELECT id,name,type,config_enc,schedule,hour,weekday,retention,enabled FROM backup_targets ORDER BY id`)
	if err != nil {
		writeError(w, 500, "Could not list backup targets", "INTERNAL_ERROR")
		return
	}
	items := make([]backupTarget, 0)
	for targets.Next() {
		var item backupTarget
		var encrypted string
		var enabled int
		if err := targets.Scan(&item.ID, &item.Name, &item.Type, &encrypted, &item.Schedule, &item.Hour, &item.Weekday, &item.Retention, &enabled); err != nil {
			targets.Close()
			writeError(w, 500, "Could not list backup targets", "INTERNAL_ERROR")
			return
		}
		item.Enabled = enabled == 1
		item.Config, err = h.publicBackupConfig(item.Type, encrypted)
		if err != nil {
			targets.Close()
			writeError(w, 500, "Could not read backup target", "INTERNAL_ERROR")
			return
		}
		items = append(items, item)
	}
	if err := targets.Err(); err != nil {
		targets.Close()
		writeError(w, 500, "Could not list backup targets", "INTERNAL_ERROR")
		return
	}
	if err := targets.Close(); err != nil {
		writeError(w, 500, "Could not list backup targets", "INTERNAL_ERROR")
		return
	}
	for i := range items {
		if err := h.completeBackupTarget(r, &items[i]); err != nil {
			writeError(w, 500, "Could not list backup runs", "INTERNAL_ERROR")
			return
		}
	}
	runs := make([]map[string]any, 0)
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,target_id,status,snapshot,started_at,finished_at,bytes_uploaded,blobs_uploaded,bytes_total,blobs_total,error FROM backup_runs ORDER BY id DESC LIMIT 50`)
	if err != nil {
		writeError(w, 500, "Could not list backup runs", "INTERNAL_ERROR")
		return
	}
	for rows.Next() {
		var id, targetID, bytes, blobs int64
		var bytesTotal, blobsTotal sql.NullInt64
		var status string
		var snapshot, started, finished, runError sql.NullString
		if err = rows.Scan(&id, &targetID, &status, &snapshot, &started, &finished, &bytes, &blobs, &bytesTotal, &blobsTotal, &runError); err != nil {
			rows.Close()
			writeError(w, 500, "Could not list backup runs", "INTERNAL_ERROR")
			return
		}
		runs = append(runs, map[string]any{"id": id, "targetId": targetID, "status": status, "snapshot": nullableString(snapshot), "startedAt": started.String, "finishedAt": nullableString(finished), "bytesUploaded": bytes, "blobsUploaded": blobs, "bytesTotal": nullableInt64(bytesTotal), "blobsTotal": nullableInt64(blobsTotal), "error": nullableString(runError)})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, 500, "Could not list backup runs", "INTERNAL_ERROR")
		return
	}
	if err = rows.Close(); err != nil {
		writeError(w, 500, "Could not list backup runs", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"targets": items, "runs": runs})
}

func (h *Handler) createBackupTarget(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.authorize(w, r)
	if !ok {
		return
	}
	if h.keys == nil {
		writeError(w, 503, "Backup target encryption is unavailable", "UNAVAILABLE")
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	name, _ := body["name"].(string)
	typ, _ := body["type"].(string)
	config, err := h.cleanBackupConfig(typ, body["config"], nil)
	if err != nil {
		writeError(w, 400, err.Error(), "BACKUP_CONFIG_INVALID")
		return
	}
	plan, err := cleanBackupPlan(body, nil)
	if err != nil {
		writeError(w, 400, err.Error(), "INVALID_VALUE")
		return
	}
	name = cleanBackupName(name, "")
	if name == "" {
		writeError(w, 400, "Give the target a name.", "NAME_REQUIRED")
		return
	}
	enabled := body["enabled"] != false
	id, err := h.saveBackupTarget(r, actor, 0, name, typ, config, plan, enabled)
	if err != nil {
		writeError(w, 500, "Could not save backup target", "INTERNAL_ERROR")
		return
	}
	h.writeBackupTarget(w, r, id, 201)
}

func (h *Handler) updateBackupTarget(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.authorize(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 400, "Invalid target id", "INVALID_ID")
		return
	}
	if h.keys == nil {
		writeError(w, 503, "Backup target encryption is unavailable", "UNAVAILABLE")
		return
	}
	body, ok := decodeObject(w, r)
	if !ok {
		return
	}
	var typ, encrypted, oldName, oldSchedule string
	var oldHour, oldWeekday, oldRetention, oldEnabled int
	err = h.db.QueryRowContext(r.Context(), `SELECT name,type,config_enc,schedule,hour,weekday,retention,enabled FROM backup_targets WHERE id=?`, id).Scan(&oldName, &typ, &encrypted, &oldSchedule, &oldHour, &oldWeekday, &oldRetention, &oldEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "Backup target not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, 500, "Could not read backup target", "INTERNAL_ERROR")
		return
	}
	previous, err := h.decryptBackupConfig(encrypted)
	if err != nil {
		writeError(w, 500, "Could not read backup target", "INTERNAL_ERROR")
		return
	}
	config := any(previous)
	if value, exists := body["config"]; exists {
		config = value
	}
	cleaned, err := h.cleanBackupConfig(typ, config, previous)
	if err != nil {
		writeError(w, 400, err.Error(), "BACKUP_CONFIG_INVALID")
		return
	}
	name := oldName
	if value, exists := body["name"]; exists {
		var valid bool
		name, valid = value.(string)
		if !valid {
			writeError(w, 400, "Give the target a name.", "NAME_REQUIRED")
			return
		}
	}
	name = cleanBackupName(name, "")
	if name == "" {
		writeError(w, 400, "Give the target a name.", "NAME_REQUIRED")
		return
	}
	current := map[string]any{"schedule": oldSchedule, "hour": oldHour, "weekday": oldWeekday, "retention": oldRetention}
	plan, err := cleanBackupPlan(body, current)
	if err != nil {
		writeError(w, 400, err.Error(), "INVALID_VALUE")
		return
	}
	enabled := oldEnabled == 1
	if value, exists := body["enabled"]; exists {
		var valid bool
		enabled, valid = value.(bool)
		if !valid {
			writeError(w, 400, "enabled must be true or false.", "INVALID_VALUE")
			return
		}
	}
	if _, err := h.saveBackupTarget(r, actor, id, name, typ, cleaned, plan, enabled); err != nil {
		writeError(w, 500, "Could not save backup target", "INTERNAL_ERROR")
		return
	}
	h.writeBackupTarget(w, r, id, 200)
}

func (h *Handler) deleteBackupTarget(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.authorize(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 400, "Invalid target id", "INVALID_ID")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "Could not delete backup target", "INTERNAL_ERROR")
		return
	}
	result, err := tx.ExecContext(r.Context(), `DELETE FROM backup_targets WHERE id=?`, id)
	if err != nil {
		tx.Rollback()
		writeError(w, 500, "Could not delete backup target", "INTERNAL_ERROR")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		tx.Rollback()
		writeError(w, 404, "Backup target not found", "NOT_FOUND")
		return
	}
	if err = h.backupAudit(r, tx, actor, "backup.target.deleted", id, nil); err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		writeError(w, 500, "Could not delete backup target", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, 200, map[string]bool{"success": true})
}

type backupPlan struct {
	schedule                 string
	hour, weekday, retention int
}

func cleanBackupPlan(body, current map[string]any) (backupPlan, error) {
	get := func(key string, fallback any) any {
		if value, ok := body[key]; ok {
			return value
		}
		if value, ok := current[key]; ok {
			return value
		}
		return fallback
	}
	p := backupPlan{schedule: "manual", hour: 3, weekday: 0, retention: 7}
	if value := get("schedule", p.schedule); value != nil {
		s, ok := value.(string)
		if !ok {
			return p, errors.New("schedule must be manual, daily or weekly.")
		}
		p.schedule = s
	}
	if p.schedule != "manual" && p.schedule != "daily" && p.schedule != "weekly" {
		return p, errors.New("schedule must be manual, daily or weekly.")
	}
	for key, dest := range map[string]*int{"hour": &p.hour, "weekday": &p.weekday, "retention": &p.retention} {
		value := get(key, *dest)
		n, ok := value.(int)
		if !ok {
			if f, yes := value.(float64); yes && f == float64(int(f)) {
				n, ok = int(f), true
			}
		}
		if !ok {
			if j, yes := value.(json.Number); yes {
				parsed, err := j.Int64()
				if err == nil {
					n, ok = int(parsed), true
				}
			}
		}
		if !ok {
			return p, fmt.Errorf("%s must be a whole number.", key)
		}
		*dest = n
	}
	if p.hour < 0 || p.hour > 23 {
		return p, errors.New("hour must be 0–23.")
	}
	if p.weekday < 0 || p.weekday > 6 {
		return p, errors.New("weekday must be 0 (Sunday) to 6.")
	}
	if p.retention < 1 || p.retention > 365 {
		return p, errors.New("Keep between 1 and 365 snapshots.")
	}
	return p, nil
}

func cleanBackupName(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		name = fallback
	}
	name = strings.TrimSpace(name)
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

func (h *Handler) cleanBackupConfig(typ string, raw any, previous map[string]any) (map[string]any, error) {
	fields, ok := backupFields[typ]
	if !ok {
		return nil, errors.New("type must be local, sftp, s3 or webdav.")
	}
	incoming, ok := raw.(map[string]any)
	if !ok || incoming == nil {
		return nil, errors.New("config must be an object.")
	}
	out := make(map[string]any)
	for _, field := range fields {
		if typ == "sftp" && field == "hostKey" {
			previousKey := ""
			if previous != nil {
				previousKey, _ = previous[field].(string)
			}
			if previousKey != "" {
				out[field] = previousKey
			}
			continue
		}
		value, provided := incoming[field]
		if secret := backupSecretFields[field]; secret {
			if text, valid := value.(string); valid {
				value = strings.TrimSpace(text)
			}
			if !provided || value == "" {
				value = previous[field]
			}
		}
		if value == nil || value == "" {
			continue
		}
		switch field {
		case "port":
			n, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil || n < 1 || n > 65535 {
				return nil, errors.New("port must be between 1 and 65535.")
			}
			value = n
		case "forcePathStyle":
			b, valid := value.(bool)
			if !valid {
				return nil, errors.New("forcePathStyle must be true or false.")
			}
			value = b
		case "url", "endpoint":
			s, valid := value.(string)
			if !valid {
				return nil, fmt.Errorf("%s must be a string.", field)
			}
			u, err := url.Parse(strings.TrimSpace(s))
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
				return nil, fmt.Errorf("%s must be an absolute HTTP or HTTPS URL.", field)
			}
			value = u.String()
		default:
			if _, valid := value.(string); !valid {
				return nil, fmt.Errorf("%s must be a string.", field)
			}
			value = strings.TrimSpace(value.(string))
		}
		out[field] = value
	}
	if typ == "sftp" && previous != nil && (fmt.Sprint(out["host"]) != fmt.Sprint(previous["host"]) || fmt.Sprint(out["port"]) != fmt.Sprint(previous["port"])) {
		delete(out, "hostKey")
	}
	for _, required := range map[string][]string{"local": {"path"}, "sftp": {"host", "username", "path"}, "s3": {"bucket", "accessKeyId", "secretAccessKey"}, "webdav": {"url"}}[typ] {
		if out[required] == nil {
			return nil, fmt.Errorf("%s is required.", required)
		}
	}
	if typ == "local" && !filepath.IsAbs(out["path"].(string)) {
		return nil, errors.New("Enter an absolute folder path.")
	}
	if typ == "sftp" && out["password"] == nil && out["privateKey"] == nil {
		return nil, errors.New("Enter a password or a private key.")
	}
	if typ == "s3" {
		if region, exists := out["region"]; !exists || region == "" {
			out["region"] = "us-east-1"
		}
		storageClass, exists := out["storageClass"]
		if exists {
			value := strings.ToUpper(storageClass.(string))
			allowed := map[string]bool{"STANDARD": true, "GLACIER": true, "DEEP_ARCHIVE": true, "GLACIER_IR": true, "STANDARD_IA": true}
			if !allowed[value] {
				return nil, errors.New("Storage class is not supported.")
			}
			out["storageClass"] = value
		}
	}
	return out, nil
}

func (h *Handler) decryptBackupConfig(encrypted string) (map[string]any, error) {
	plain, err := h.keys.DecryptValue(encrypted)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err = json.Unmarshal([]byte(plain), &config); err != nil || config == nil {
		return nil, errors.New("invalid encrypted backup configuration")
	}
	return config, nil
}

func (h *Handler) publicBackupConfig(typ, encrypted string) (map[string]any, error) {
	config, err := h.decryptBackupConfig(encrypted)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, field := range backupFields[typ] {
		if field == "hostKey" {
			continue
		}
		if backupSecretFields[field] {
			value, _ := config[field].(string)
			out[field+"Set"] = value != ""
		} else if value, exists := config[field]; exists {
			out[field] = value
		}
	}
	return out, nil
}

func (h *Handler) saveBackupTarget(r *http.Request, actor, id int64, name, typ string, config map[string]any, plan backupPlan, enabled bool) (int64, error) {
	if h.keys == nil {
		return 0, errors.New("backup encryption unavailable")
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return 0, err
	}
	sealed, err := h.keys.EncryptValue(string(encoded))
	if err != nil {
		return 0, err
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var targetID = id
	if id == 0 {
		result, execErr := tx.ExecContext(r.Context(), `INSERT INTO backup_targets(name,type,config_enc,schedule,hour,weekday,retention,enabled) VALUES(?,?,?,?,?,?,?,?)`, name, typ, sealed, plan.schedule, plan.hour, plan.weekday, plan.retention, enabledInt(enabled))
		if execErr != nil {
			return 0, execErr
		}
		targetID, err = result.LastInsertId()
	} else {
		_, err = tx.ExecContext(r.Context(), `UPDATE backup_targets SET name=?,config_enc=?,schedule=?,hour=?,weekday=?,retention=?,enabled=? WHERE id=?`, name, sealed, plan.schedule, plan.hour, plan.weekday, plan.retention, enabledInt(enabled), id)
	}
	if err != nil {
		return 0, err
	}
	action := "backup.target.created"
	if id != 0 {
		action = "backup.target.updated"
	}
	if err = h.backupAudit(r, tx, actor, action, targetID, map[string]any{"name": name, "type": typ}); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return targetID, nil
}

func enabledInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func (h *Handler) backupAudit(r *http.Request, tx *sql.Tx, actor int64, action string, targetID int64, detail any) error {
	data := map[string]any{"targetId": targetID}
	if values, ok := detail.(map[string]any); ok {
		for key, value := range values {
			data[key] = value
		}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO access_audit_log(action,actor_user_id,detail_json) VALUES(?,?,?)`, action, actor, string(encoded))
	return err
}

func (h *Handler) writeBackupTarget(w http.ResponseWriter, r *http.Request, id int64, status int) {
	var target backupTarget
	var encrypted string
	var enabled int
	err := h.db.QueryRowContext(r.Context(), `SELECT id,name,type,config_enc,schedule,hour,weekday,retention,enabled FROM backup_targets WHERE id=?`, id).Scan(&target.ID, &target.Name, &target.Type, &encrypted, &target.Schedule, &target.Hour, &target.Weekday, &target.Retention, &enabled)
	if err != nil {
		writeError(w, 500, "Could not read backup target", "INTERNAL_ERROR")
		return
	}
	target.Enabled = enabled == 1
	target.Config, err = h.publicBackupConfig(target.Type, encrypted)
	if err != nil {
		writeError(w, 500, "Could not read backup target", "INTERNAL_ERROR")
		return
	}
	if err := h.completeBackupTarget(r, &target); err != nil {
		writeError(w, 500, "Could not read backup target", "INTERNAL_ERROR")
		return
	}
	httpx.WriteJSON(w, status, target)
}

// completeBackupTarget adds what happened to the target: its last run,
// verification and storage class migration.
func (h *Handler) completeBackupTarget(r *http.Request, target *backupTarget) error {
	if h.onStorageClassMigrationStatus != nil {
		target.StorageMigration = h.onStorageClassMigrationStatus(target.ID)
	}
	if h.onBackupVerification != nil {
		verification, err := h.onBackupVerification(target.ID)
		if err != nil {
			return err
		}
		target.LastVerification = verification
	}
	var id, targetID, bytes, blobs int64
	var bytesTotal, blobsTotal sql.NullInt64
	var status string
	var snapshot, started, finished, runError sql.NullString
	err := h.db.QueryRowContext(r.Context(), `SELECT id,target_id,status,snapshot,started_at,finished_at,bytes_uploaded,blobs_uploaded,bytes_total,blobs_total,error FROM backup_runs WHERE target_id=? ORDER BY id DESC LIMIT 1`, target.ID).Scan(&id, &targetID, &status, &snapshot, &started, &finished, &bytes, &blobs, &bytesTotal, &blobsTotal, &runError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	target.LastRun = map[string]any{"id": id, "targetId": targetID, "status": status, "snapshot": nullableString(snapshot), "startedAt": started.String, "finishedAt": nullableString(finished), "bytesUploaded": bytes, "blobsUploaded": blobs, "bytesTotal": nullableInt64(bytesTotal), "blobsTotal": nullableInt64(blobsTotal), "error": nullableString(runError)}
	return nil
}
