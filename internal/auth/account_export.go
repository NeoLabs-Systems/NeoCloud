package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

type accountExportRequest struct {
	CurrentPassword string `json:"currentPassword"`
	Code            string `json:"code"`
}

// exportAccount returns the user's account metadata without exporting material
// that can authenticate as the user or decrypt private data.
func (h *Handler) exportAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := h.accountUser(w, r)
	if !ok {
		return
	}
	var request accountExportRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !h.confirmIdentity(w, r, u.ID, request.CurrentPassword, request.Code) {
		return
	}

	payload, err := h.payload(r.Context(), u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not export account data", "INTERNAL_ERROR")
		return
	}
	data, err := h.accountExportData(r, u.ID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not export account data", "INTERNAL_ERROR")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": fmt.Sprintf("neocloud-account-%s-%s.json", safeExportName(u.Username), h.now().UTC().Format("2006-01-02")),
	}))
	writeJSON(w, http.StatusOK, data)
}

func (h *Handler) accountExportData(r *http.Request, userID int64, profile userPayload) (map[string]any, error) {
	ctx := r.Context()
	preferences := map[string]any{}
	if h.readPreferences != nil {
		var err error
		if preferences, err = h.readPreferences(ctx, userID); err != nil {
			return nil, err
		}
	}

	twoFactorEnabled := false
	var enabled int
	var enabledAt sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT enabled,enabled_at FROM user_two_factor WHERE user_id=?`, userID).Scan(&enabled, &enabledAt)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	twoFactorEnabled = enabled == 1
	passkeys, err := exportRows(ctx, h.db, `SELECT id, label, device_type AS deviceType, backed_up AS backedUp, rp_id AS host, created_at AS createdAt, last_used_at AS lastUsedAt FROM user_webauthn_credentials WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}

	sections := map[string]string{
		"sessions":      `SELECT id, ip_address AS ipAddress, user_agent AS userAgent, created_at AS createdAt, last_seen_at AS lastSeenAt, expires_at AS expiresAt, revoked_at AS revokedAt FROM user_sessions WHERE user_id=? ORDER BY id`,
		"devices":       `SELECT id, name, platform, created_at AS createdAt, last_seen_at AS lastSeenAt, revoked_at AS revokedAt FROM device_tokens WHERE user_id=? ORDER BY id`,
		"files":         `SELECT id, parent_id AS parentId, type, name, size, mime, version, created_at AS createdAt, modified_at AS modifiedAt, trashed_at AS trashedAt, vault_id AS vaultId FROM nodes WHERE user_id=? ORDER BY parent_id IS NOT NULL, parent_id, name`,
		"favorites":     `SELECT node_id AS nodeId, created_at AS createdAt FROM favorites WHERE user_id=? ORDER BY created_at`,
		"vaults":        `SELECT id, folder_id AS folderId, created_at AS createdAt, updated_at AS updatedAt FROM vaults WHERE user_id=? ORDER BY created_at`,
		"links":         `SELECT s.id, s.node_id AS nodeId, n.name AS nodeName, s.mode, s.password_hash IS NOT NULL AS hasPassword, s.note, s.expires_at AS expiresAt, s.max_downloads AS maxDownloads, s.download_count AS downloadCount, s.max_upload_bytes AS maxUploadBytes, s.uploaded_bytes AS uploadedBytes, s.created_at AS createdAt, s.revoked_at AS revokedAt FROM shares s JOIN nodes n ON n.id=s.node_id WHERE s.user_id=? ORDER BY s.id`,
		"sharingByMe":   `SELECT s.id, s.node_id AS nodeId, n.name AS nodeName, s.permission, s.recipient, s.expires_at AS expiresAt, s.created_at AS createdAt, u.display_name AS displayName FROM user_shares s JOIN nodes n ON n.id=s.node_id JOIN users u ON u.id=s.grantee_id WHERE s.owner_id=? ORDER BY s.id`,
		"sharingWithMe": `SELECT s.id, s.node_id AS nodeId, n.name AS nodeName, s.permission, s.expires_at AS expiresAt, s.created_at AS createdAt, u.username AS owner FROM user_shares s JOIN nodes n ON n.id=s.node_id JOIN users u ON u.id=s.owner_id WHERE s.grantee_id=? ORDER BY s.id`,
		"imports":       `SELECT id, source, url, username, source_path AS sourcePath, target_folder_id AS targetFolderId, status, created_at AS createdAt, started_at AS startedAt, finished_at AS finishedAt FROM import_jobs WHERE user_id=? ORDER BY created_at`,
		"audit":         `SELECT id, action, actor_user_id AS actorUserId, subject_user_id AS subjectUserId, detail_json AS detailJSON, created_at AS createdAt FROM access_audit_log WHERE actor_user_id=? OR subject_user_id=? ORDER BY id`,
	}
	result := map[string]any{
		"format": 1, "kind": "neocloud-account", "exportedAt": h.now().UTC().Format(time.RFC3339Nano),
		"note": "File contents are not included. Download your files, or a folder as ZIP, to keep them.",
		"profile": map[string]any{
			"id": profile.ID, "username": profile.Username, "displayName": profile.DisplayName,
			"email": profile.Email, "isAdmin": profile.IsAdmin, "createdAt": profile.CreatedAt,
			"lastLogin": profile.LastLogin,
		},
		"storage": profile.Storage, "settings": preferences,
		"security": map[string]any{"twoFactor": map[string]any{"enabled": twoFactorEnabled, "enabledAt": nullableString(enabledAt)}, "passkeys": passkeys},
		"sharing":  map[string]any{},
	}
	activity, err := h.exportAccountActivity(ctx, userID)
	if err != nil {
		return nil, err
	}
	result["activity"] = activity
	externalStorage, err := h.exportExternalStorage(ctx, userID)
	if err != nil {
		return nil, err
	}
	result["externalStorage"] = externalStorage
	for name, query := range sections {
		if name == "files" {
			continue
		}
		var items []map[string]any
		if name == "audit" {
			items, err = exportRows(ctx, h.db, query, userID, userID)
		} else {
			items, err = exportRows(ctx, h.db, query, userID)
		}
		if err != nil {
			return nil, err
		}
		if name == "audit" {
			for _, item := range items {
				if raw, ok := item["detailJSON"].(string); ok {
					var detail any
					if json.Unmarshal([]byte(raw), &detail) == nil {
						item["detail"] = detail
					} else {
						item["detail"] = map[string]any{}
					}
					delete(item, "detailJSON")
				}
				item["byMe"] = item["actorUserId"] == userID
				item["aboutMe"] = item["subjectUserId"] == userID
				delete(item, "actorUserId")
				delete(item, "subjectUserId")
			}
		}
		if name == "sharingByMe" {
			result["sharing"].(map[string]any)["byMe"] = items
		} else if name == "sharingWithMe" {
			result["sharing"].(map[string]any)["withMe"] = items
		} else {
			result[name] = items
		}
	}
	versions, err := exportRows(ctx, h.db, `SELECT v.node_id AS nodeId, v.version, v.size, v.created_at AS createdAt FROM file_versions v JOIN nodes n ON n.id=v.node_id WHERE n.user_id=? ORDER BY v.node_id, v.version`, userID)
	if err != nil {
		return nil, err
	}
	versionMap := make(map[string][]map[string]any)
	for _, version := range versions {
		id, _ := version["nodeId"].(string)
		versionMap[id] = append(versionMap[id], map[string]any{"version": version["version"], "size": version["size"], "createdAt": version["createdAt"]})
	}
	files, err := exportRows(ctx, h.db, sections["files"], userID)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file["type"] == "file" {
			id, _ := file["id"].(string)
			if versionMap[id] == nil {
				versionMap[id] = make([]map[string]any, 0)
			}
			file["versions"] = versionMap[id]
		}
	}
	result["files"] = files
	signing, err := h.exportSigning(ctx, userID)
	if err != nil {
		return nil, err
	}
	result["signing"] = signing
	signingProfile, err := h.exportSigningProfile(ctx, userID, profile)
	if err != nil {
		return nil, err
	}
	result["signingProfile"] = signingProfile
	return result, nil
}

// exportExternalStorage mirrors Node's public mount serialization: connection
// settings remain useful in the export, while credentials and the SFTP host
// key are never disclosed.
func (h *Handler) exportExternalStorage(ctx context.Context, userID int64) ([]map[string]any, error) {
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,name,provider,config_enc,read_only,created_at FROM external_mounts WHERE user_id=? ORDER BY name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mounts := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, provider, encoded, createdAt string
		var readOnly int
		if err := rows.Scan(&id, &name, &provider, &encoded, &readOnly, &createdAt); err != nil {
			return nil, err
		}
		sealed, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("account export: decode external storage config: %w", err)
		}
		plain, err := cryptox.OpenBytes(dek, sealed, []byte(fmt.Sprintf("external-mount:%d:%s", userID, id)))
		if err != nil {
			return nil, fmt.Errorf("account export: decrypt external storage config: %w", err)
		}
		config := map[string]any{}
		if err := json.Unmarshal(plain, &config); err != nil {
			return nil, fmt.Errorf("account export: parse external storage config: %w", err)
		}
		publicConfig, err := publicExternalStorageConfig(provider, config)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, map[string]any{
			"id": id, "name": name, "provider": provider, "readOnly": readOnly != 0,
			"config": publicConfig, "createdAt": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return mounts, nil
}

func publicExternalStorageConfig(provider string, config map[string]any) (map[string]any, error) {
	fields := map[string][]string{
		"s3":     {"endpoint", "region", "bucket", "prefix", "accessKeyId", "secretAccessKey", "forcePathStyle"},
		"webdav": {"url", "username", "password"},
		"sftp":   {"host", "port", "username", "password", "privateKey", "passphrase", "path"},
	}
	providerFields, ok := fields[provider]
	if !ok {
		return nil, fmt.Errorf("account export: unsupported external storage provider %q", provider)
	}
	secret := map[string]bool{"secretAccessKey": true, "password": true, "privateKey": true, "passphrase": true}
	out := map[string]any{}
	for _, field := range providerFields {
		value, exists := config[field]
		if secret[field] {
			out[field+"Set"] = exists && value != ""
		} else if exists {
			out[field] = value
		}
	}
	return out, nil
}

// exportAccountActivity matches the portable Node export shape. Activity
// details stay structured, and comments authored on another account's file
// are included without revealing that file's current name.
func (h *Handler) exportAccountActivity(ctx context.Context, userID int64) (map[string]any, error) {
	entries, err := exportRows(ctx, h.db, `SELECT a.actor_id AS actorId,a.kind,a.node_id AS nodeId,a.node_type AS nodeType,a.name,a.detail,a.count,a.at,
		(a.actor_id=?) AS byMe,COALESCE(NULLIF(u.display_name,''),u.username) AS actor
		FROM activity a JOIN users u ON u.id=a.actor_id WHERE a.owner_id=? ORDER BY a.id`, userID, userID)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		detail := map[string]any{}
		if raw, ok := entry["detail"].(string); ok && raw != "" {
			if json.Unmarshal([]byte(raw), &detail) != nil {
				detail = map[string]any{}
			}
		}
		delete(entry, "detail")
		delete(entry, "actorId")
		for key, value := range detail {
			entry[key] = value
		}
	}
	comments, err := exportRows(ctx, h.db, `SELECT c.id,c.node_id AS nodeId,c.user_id AS userId,c.body,c.created_at AS createdAt,c.edited_at AS editedAt,
		n.name AS nodeName,n.user_id AS ownerId,COALESCE(NULLIF(u.display_name,''),u.username) AS author,
		(n.user_id=?) AS onMyFile,(c.user_id=?) AS byMe
		FROM node_comments c JOIN nodes n ON n.id=c.node_id JOIN users u ON u.id=c.user_id
		WHERE n.user_id=? OR c.user_id=? ORDER BY c.id`, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	for _, comment := range comments {
		body, ok := comment["body"].(string)
		if !ok {
			return nil, fmt.Errorf("account export: comment body has unexpected type")
		}
		plain, err := h.keys.DecryptValue(body)
		if err != nil {
			return nil, fmt.Errorf("account export: decrypt comment: %w", err)
		}
		comment["body"] = plain
		if comment["onMyFile"] == false {
			comment["nodeName"] = nil
		}
		delete(comment, "userId")
		delete(comment, "ownerId")
	}
	return map[string]any{"entries": entries, "comments": comments}, nil
}

func (h *Handler) exportSigning(ctx context.Context, userID int64) ([]map[string]any, error) {
	documents, err := exportRows(ctx, h.db, `SELECT id,title,status,signing_order AS signingOrder,page_count AS pageCount,source_node_id AS sourceNodeId,final_node_id AS finalNodeId,snapshot_sha256 AS snapshotSha256,final_sha256 AS finalSha256,expires_at AS expiresAt,created_at AS createdAt,sent_at AS sentAt,completed_at AS completedAt FROM sign_documents WHERE owner_id=? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	for _, document := range documents {
		id, ok := document["id"].(string)
		if !ok {
			return nil, fmt.Errorf("account export: signing document has invalid id")
		}
		parties, err := exportRows(ctx, h.db, `SELECT name,email,status,signer_name AS signerName,opened_at AS openedAt,consented_at AS consentedAt,completed_at AS completedAt,declined_at AS declinedAt,decline_reason AS declineReason,ip FROM sign_parties WHERE document_id=? ORDER BY position`, id)
		if err != nil {
			return nil, err
		}
		events, err := exportRows(ctx, h.db, `SELECT type,at,party_id AS partyId,ip,hash FROM sign_events WHERE document_id=? ORDER BY id`, id)
		if err != nil {
			return nil, err
		}
		document["parties"] = parties
		document["events"] = events
	}
	return documents, nil
}

func (h *Handler) exportSigningProfile(ctx context.Context, userID int64, profile userPayload) (map[string]any, error) {
	displayName := profile.Username
	if profile.DisplayName != nil && *profile.DisplayName != "" {
		displayName = *profile.DisplayName
	}
	email := ""
	if profile.Email != nil {
		email = *profile.Email
	}
	details := map[string]any{"displayName": displayName, "email": email, "phone": "", "presets": []any{}}
	var encrypted sql.NullString
	autoFill, autoSave := 1, 1
	var notify int
	var folder sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT data_enc,auto_fill,auto_save,save_folder_id,notify_each_signature FROM sign_profiles WHERE user_id=?`, userID).Scan(&encrypted, &autoFill, &autoSave, &folder, &notify)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if encrypted.Valid && encrypted.String != "" {
		plain, err := h.keys.DecryptValue(encrypted.String)
		if err != nil {
			return nil, fmt.Errorf("account export: decrypt signing profile: %w", err)
		}
		var saved map[string]any
		if json.Unmarshal([]byte(plain), &saved) != nil {
			return nil, fmt.Errorf("account export: signing profile is invalid")
		}
		for _, key := range []string{"displayName", "email", "phone", "presets"} {
			if value, ok := saved[key]; ok {
				details[key] = value
			}
		}
	}
	images, err := exportRows(ctx, h.db, `SELECT id,kind,method,label,is_default AS isDefault,created_at AS createdAt FROM sign_profile_images WHERE user_id=? ORDER BY kind DESC,created_at,id`, userID)
	if err != nil {
		return nil, err
	}
	for _, image := range images {
		if n, ok := image["isDefault"].(int64); ok {
			image["isDefault"] = n != 0
		}
	}
	var signed []map[string]any
	if signed, err = exportRows(ctx, h.db, `SELECT d.id AS documentId,d.title,p.id AS partyId,p.status,p.completed_at AS completedAt FROM sign_parties p JOIN sign_documents d ON d.id=p.document_id WHERE p.user_id=? ORDER BY d.created_at`, userID); err != nil {
		return nil, err
	}
	return map[string]any{
		"displayName": details["displayName"], "email": details["email"], "phone": details["phone"], "presets": details["presets"],
		"autoFill": autoFill != 0, "autoSave": autoSave != 0, "saveFolderId": nullableString(folder), "notifyOnEachSignature": notify != 0,
		"signatures": images, "signed": signed,
	}, nil
}

func exportRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(columns))
		for i, column := range columns {
			value := values[i]
			if bytes, ok := value.([]byte); ok {
				value = string(bytes)
			}
			if column == "hasPassword" || column == "backedUp" || column == "readOnly" || column == "byMe" || column == "onMyFile" {
				if n, ok := value.(int64); ok {
					value = n != 0
				}
			}
			item[column] = value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func safeExportName(username string) string {
	username = strings.TrimSpace(username)
	var b strings.Builder
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "account"
	}
	return b.String()
}
