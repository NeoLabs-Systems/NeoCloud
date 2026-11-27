package handoff

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

var mimePattern = regexp.MustCompile(`^[\w.+-]+/[\w.+-]+$`)

type endpoint struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Platform *string `json:"platform,omitempty"`
}

type file struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int64  `json:"size"`
}

type offer struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	Status    string   `json:"status"`
	From      endpoint `json:"from"`
	To        endpoint `json:"to"`
	TotalSize int64    `json:"totalSize"`
	Files     []file   `json:"files"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
	ExpiresAt string   `json:"expiresAt"`
}

// offerRow is a send as stored; only its sender and its receiver may see it.
type offerRow struct {
	id, status, senderKey, senderName, senderPlatform, targetKey, targetName string
	totalSize                                                                int64
	createdAt, updatedAt, expiresAt                                          string
}

func (row offerRow) expired() bool {
	at, err := time.Parse("2006-01-02 15:04:05", row.expiresAt)
	return err == nil && !at.After(time.Now())
}

const offerColumns = `id,status,sender_key,sender_name,sender_platform,target_key,target_name,total_size,created_at,updated_at,expires_at`

func scanOffer(row interface{ Scan(...any) error }) (offerRow, error) {
	var o offerRow
	err := row.Scan(&o.id, &o.status, &o.senderKey, &o.senderName, &o.senderPlatform, &o.targetKey, &o.targetName, &o.totalSize, &o.createdAt, &o.updatedAt, &o.expiresAt)
	return o, err
}

var errSendNotFound = failure(http.StatusNotFound, "Send not found.", "NOT_FOUND")

func (h *Handler) row(ctx context.Context, userID int64, id string) (offerRow, error) {
	row, err := scanOffer(h.db.QueryRowContext(ctx, `SELECT `+offerColumns+` FROM handoff_offers WHERE id=? AND user_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return offerRow{}, errSendNotFound
	}
	return row, err
}

// receiverRow is an offer addressed to key; the receiver sees it once uploaded.
func (h *Handler) receiverRow(ctx context.Context, userID int64, key, id string) (offerRow, error) {
	row, err := h.row(ctx, userID, id)
	if err == nil && (row.targetKey != key || row.status == "uploading") {
		return offerRow{}, errSendNotFound
	}
	return row, err
}

func (h *Handler) senderRow(ctx context.Context, userID int64, key, id string) (offerRow, error) {
	row, err := h.row(ctx, userID, id)
	if err == nil && row.senderKey != key {
		return offerRow{}, errSendNotFound
	}
	return row, err
}

func (h *Handler) serialize(ctx context.Context, row offerRow, role string) (offer, error) {
	o := offer{
		ID: row.id, Role: role, Status: row.status,
		From:      endpoint{ID: row.senderKey, Name: row.senderName, Platform: &row.senderPlatform},
		To:        endpoint{ID: row.targetKey, Name: row.targetName},
		TotalSize: row.totalSize, Files: []file{},
		CreatedAt: row.createdAt, UpdatedAt: row.updatedAt, ExpiresAt: row.expiresAt,
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,name,mime,size FROM handoff_files WHERE offer_id=? ORDER BY position`, row.id)
	if err != nil {
		return offer{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.ID, &f.Name, &f.MIME, &f.Size); err != nil {
			return offer{}, err
		}
		o.Files = append(o.Files, f)
	}
	return o, rows.Err()
}

// offerFor is the send as its sender or receiver sees it.
func (h *Handler) offerFor(ctx context.Context, userID int64, key, id string) (offer, error) {
	row, err := h.row(ctx, userID, id)
	if err != nil {
		return offer{}, err
	}
	if row.senderKey == key {
		return h.serialize(ctx, row, "sender")
	}
	if row.targetKey == key && row.status != "uploading" {
		return h.serialize(ctx, row, "receiver")
	}
	return offer{}, errSendNotFound
}

func (h *Handler) offers(ctx context.Context, role, condition string, args ...any) ([]offer, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT `+offerColumns+` FROM handoff_offers WHERE `+condition, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []offerRow
	for rows.Next() {
		row, err := scanOffer(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, row)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	out := []offer{}
	for _, row := range found {
		o, err := h.serialize(ctx, row, role)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (h *Handler) sent(ctx context.Context, userID int64, key string) ([]offer, error) {
	return h.offers(ctx, "sender", `user_id=? AND sender_key=? ORDER BY created_at DESC,rowid DESC LIMIT 50`, userID, key)
}

func (h *Handler) inbox(ctx context.Context, userID int64, key string) ([]offer, error) {
	return h.offers(ctx, "receiver", `user_id=? AND target_key=? AND status IN ('pending','accepted') ORDER BY created_at,rowid`, userID, key)
}

type declared struct {
	name, mime string
	size       int64
	// nodeID and blobID are set for a file already in NeoCloud.
	nodeID, blobID string
}

func declaredFiles(value any) ([]declared, error) {
	list, ok := value.([]any)
	if !ok || len(list) == 0 {
		return nil, failure(http.StatusBadRequest, "Choose at least one file.", "NOTHING_SELECTED")
	}
	if len(list) > maxFiles {
		return nil, failure(http.StatusBadRequest, fmt.Sprintf("Send up to %d files at once.", maxFiles), "TOO_MANY_FILES")
	}
	out := make([]declared, 0, len(list))
	for _, item := range list {
		entry, _ := item.(map[string]any)
		size, ok := entry["size"].(float64)
		const maxSafe = 1<<53 - 1
		if !ok || size < 0 || size > maxSafe || size != float64(int64(size)) {
			return nil, failure(http.StatusBadRequest, "Every file needs its size.", "INVALID_SIZE")
		}
		name, _ := entry["name"].(string)
		mime, _ := entry["mime"].(string)
		if !mimePattern.MatchString(mime) {
			mime = "application/octet-stream"
		}
		out = append(out, declared{name: name, mime: mime, size: int64(size)})
	}
	return out, nil
}

// declaredNodes are files already in NeoCloud, sent as copies sharing their content.
func (h *Handler) declaredNodes(ctx context.Context, userID int64, value any) ([]declared, error) {
	list, _ := value.([]any)
	if len(list) > maxFiles {
		return nil, failure(http.StatusBadRequest, fmt.Sprintf("Send up to %d files at once.", maxFiles), "TOO_MANY_FILES")
	}
	var out []declared
	var seen []string
	for _, item := range list {
		id, _ := item.(string)
		if slices.Contains(seen, id) {
			continue
		}
		seen = append(seen, id)
		node, err := h.files.OwnNode(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if node.Type != "file" {
			return nil, failure(http.StatusBadRequest, "That is not a file.", "NOT_A_FILE")
		}
		if node.VaultID != nil {
			return nil, failure(http.StatusBadRequest, "Files in a vault can’t be sent.", "VAULT_FILE")
		}
		entry := declared{name: node.Name, size: node.Size, mime: "application/octet-stream", nodeID: node.ID}
		if err := h.db.QueryRowContext(ctx, `SELECT blob_id,COALESCE(NULLIF(mime,''),'application/octet-stream') FROM nodes WHERE id=? AND user_id=?`, node.ID, userID).Scan(&entry.blobID, &entry.mime); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, c caller) error {
	ctx := r.Context()
	body, err := apijson.Object(w, r)
	if err != nil {
		return err
	}
	targetID, _ := body["targetId"].(string)
	target, err := h.resolveTarget(ctx, c.UserID, targetID)
	if err != nil {
		return err
	}
	if target.ID == c.key {
		return failure(http.StatusBadRequest, "Choose another device.", "SAME_DEVICE")
	}
	list, err := h.declaredNodes(ctx, c.UserID, body["nodeIds"])
	if err != nil {
		return err
	}
	ready := true
	if value, given := body["files"]; given {
		uploads, err := declaredFiles(value)
		if err != nil {
			return err
		}
		list = append(list, uploads...)
		ready = false
	}
	if len(list) == 0 {
		return failure(http.StatusBadRequest, "Choose at least one file.", "NOTHING_SELECTED")
	}
	if len(list) > maxFiles {
		return failure(http.StatusBadRequest, fmt.Sprintf("Send up to %d files at once.", maxFiles), "TOO_MANY_FILES")
	}
	var total, uploadBytes int64
	names := make([]string, len(list))
	for i, entry := range list {
		// Checked one by one so that huge declared sizes cannot overflow the sum.
		if entry.size > maxTotalBytes || total+entry.size > maxTotalBytes {
			return failure(http.StatusRequestEntityTooLarge, "That is more than can be sent at once.", "SEND_TOO_LARGE")
		}
		total += entry.size
		if entry.nodeID == "" {
			uploadBytes += entry.size
		}
		names[i] = entry.name
	}
	if uploadBytes > 0 {
		if err := h.files.CheckUploadFits(ctx, c.UserID, uploadBytes); err != nil {
			return err
		}
	}
	senderName, senderPlatform, err := h.describeSender(ctx, c, r.UserAgent())
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	if err := h.insert(ctx, c, id, target, senderName, senderPlatform, list, uniqueFileNames(names), total, ready); err != nil {
		return err
	}
	if ready {
		h.announceReady(ctx, c.UserID, id)
	}
	o, err := h.offerFor(ctx, c.UserID, c.key, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, o)
	return nil
}

func (h *Handler) insert(ctx context.Context, c caller, id string, target device, senderName, senderPlatform string, list []declared, names []string, total int64, ready bool) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var open int
	var openBytes int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(total_size),0) FROM handoff_offers WHERE user_id=? AND status IN ('uploading','pending','accepted')`, c.UserID).Scan(&open, &openBytes); err != nil {
		return err
	}
	if open >= maxOpenOffers || openBytes+total > maxOpenBytes {
		return failure(http.StatusTooManyRequests, "Too many sends are waiting. Wait for them to arrive or cancel one.", "TOO_MANY_OPEN_SENDS")
	}
	status, expiry := "uploading", uploadWindow
	if ready {
		status, expiry = "pending", waitingTTL
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO handoff_offers(id,user_id,sender_key,sender_name,sender_platform,target_key,target_name,status,total_size,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,datetime('now',?))`, id, c.UserID, c.key, senderName, senderPlatform, target.ID, target.Name, status, total, expiry); err != nil {
		return err
	}
	for position, entry := range list {
		var nodeID, blobID any
		if entry.nodeID != "" {
			nodeID, blobID = entry.nodeID, entry.blobID
			result, err := tx.ExecContext(ctx, `UPDATE blobs SET ref_count=ref_count+1 WHERE id=? AND user_id=?`, entry.blobID, c.UserID)
			if err != nil {
				return err
			}
			if changed, err := result.RowsAffected(); err != nil || changed != 1 {
				return errors.Join(err, failure(http.StatusNotFound, "File not found.", "NOT_FOUND"))
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO handoff_files(offer_id,position,name,mime,size,source_node_id,blob_id) VALUES(?,?,?,?,?,?,?)`,
			id, position, names[position], entry.mime, entry.size, nodeID, blobID); err != nil {
			return err
		}
	}
	detail, err := json.Marshal(map[string]any{"files": len(list), "bytes": total, "to": target.Kind})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES('handoff.sent',?,?,?)`, c.UserID, c.UserID, string(detail)); err != nil {
		return err
	}
	return tx.Commit()
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
