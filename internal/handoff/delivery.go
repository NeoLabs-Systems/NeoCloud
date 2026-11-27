package handoff

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The life of a send after it exists is uploading, pending, accepted,
// delivered; from any open state it may be declined (receiver), cancelled
// (sender, or a revoked device) or expired. Every end releases the staged files.

var (
	openStates = []string{"uploading", "pending", "accepted"}
	errClosed  = failure(http.StatusConflict, "This send is no longer available.", "OFFER_CLOSED")
	errExpired = failure(http.StatusGone, "This send has expired.", "OFFER_EXPIRED")
)

// offerArrived tells the receiver of a ready send; offerChanged tells both
// devices to refresh. The contents reach the receiver only once it is ready.
func (h *Handler) offerArrived(ctx context.Context, userID int64, row offerRow) {
	h.push(ctx, userID, row.targetKey, "handoff:offer", row, "receiver", false)
}

func (h *Handler) offerChanged(ctx context.Context, userID int64, row offerRow) {
	h.push(ctx, userID, row.senderKey, "handoff:updated", row, "sender", true)
	if row.status != "uploading" {
		h.push(ctx, userID, row.targetKey, "handoff:updated", row, "receiver", true)
	}
}

func (h *Handler) push(ctx context.Context, userID int64, key, event string, row offerRow, role string, wrapped bool) {
	o, err := h.serialize(context.WithoutCancel(ctx), row, role)
	if err != nil {
		slog.Error("announce a send", "error", err)
		return
	}
	var payload any = o
	if wrapped {
		payload = map[string]any{"offer": o}
	}
	h.presence.pushToDevice(userID, key, event, payload)
}

// announceReady tells both devices that a send is waiting for its receiver.
func (h *Handler) announceReady(ctx context.Context, userID int64, id string) {
	row, err := h.row(context.WithoutCancel(ctx), userID, id)
	if err != nil {
		slog.Error("announce a send", "error", err)
		return
	}
	h.offerArrived(ctx, userID, row)
	h.offerChanged(ctx, userID, row)
}

// markReadyWhenComplete makes a send ready once its last file has arrived.
func (h *Handler) markReadyWhenComplete(ctx context.Context, userID int64, id string) error {
	result, err := h.db.ExecContext(ctx, `UPDATE handoff_offers SET status='pending',updated_at=datetime('now'),expires_at=datetime('now',?)
		WHERE id=? AND user_id=? AND status='uploading' AND NOT EXISTS(SELECT 1 FROM handoff_files WHERE offer_id=handoff_offers.id AND blob_id IS NULL)`, waitingTTL, id, userID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return err
	}
	h.announceReady(ctx, userID, id)
	return nil
}

// finish ends a send that is in one of from and releases its staged files. It
// reports false when someone else ended it first.
func (h *Handler) finish(ctx context.Context, userID int64, id, status string, from []string) (bool, error) {
	ctx = context.WithoutCancel(ctx)
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	args := []any{status, id, userID}
	for _, state := range from {
		args = append(args, state)
	}
	result, err := tx.ExecContext(ctx, `UPDATE handoff_offers SET status=?,updated_at=datetime('now') WHERE id=? AND user_id=? AND status IN (`+
		strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")+`)`, args...)
	if err != nil {
		return false, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return false, err
	}
	orphaned, err := releaseFiles(ctx, tx, userID, id)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	h.files.RemoveBlobFiles(userID, orphaned)
	row, err := h.row(ctx, userID, id)
	if err != nil {
		return true, err
	}
	h.offerChanged(ctx, userID, row)
	return true, nil
}

// releaseFiles drops every staged file of a send inside the caller's
// transaction and returns the blobs to delete from disk after it commits.
func releaseFiles(ctx context.Context, tx *sql.Tx, userID int64, offerID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT blob_id FROM handoff_files WHERE offer_id=? AND blob_id IS NOT NULL`, offerID)
	if err != nil {
		return nil, err
	}
	var blobs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		blobs = append(blobs, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE handoff_files SET blob_id=NULL WHERE offer_id=?`, offerID); err != nil {
		return nil, err
	}
	var orphaned []string
	for _, id := range blobs {
		removed, err := releaseBlob(ctx, tx, userID, id)
		if err != nil {
			return nil, err
		}
		if removed {
			orphaned = append(orphaned, id)
		}
	}
	return orphaned, nil
}

// releaseBlob gives back one reference, reporting true when it was the last.
func releaseBlob(ctx context.Context, tx *sql.Tx, userID int64, id string) (bool, error) {
	var size, references int64
	err := tx.QueryRowContext(ctx, `SELECT size,ref_count FROM blobs WHERE id=? AND user_id=?`, id, userID).Scan(&size, &references)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if references > 1 {
		_, err = tx.ExecContext(ctx, `UPDATE blobs SET ref_count=ref_count-1 WHERE id=?`, id)
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM blobs WHERE id=?`, id); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE users SET used_bytes=MAX(0,used_bytes-?) WHERE id=?`, size, userID)
	return true, err
}

func (h *Handler) blobPath(userID int64, id string) (string, error) {
	if len(id) < 2 || filepath.Base(id) != id {
		return "", fmt.Errorf("handoff: invalid blob id %q", id)
	}
	return filepath.Join(h.blobDir, strconv.FormatInt(userID, 10), id[:2], id), nil
}

// accept lets the receiver download; asking again is a resumed download.
func (h *Handler) accept(ctx context.Context, c caller, id string) (offer, error) {
	row, err := h.receiverRow(ctx, c.UserID, c.key, id)
	if err != nil {
		return offer{}, err
	}
	if row.status == "accepted" {
		return h.serialize(ctx, row, "receiver")
	}
	if row.status != "pending" {
		return offer{}, errClosed
	}
	if row.expired() {
		return offer{}, errExpired
	}
	result, err := h.db.ExecContext(ctx, `UPDATE handoff_offers SET status='accepted',updated_at=datetime('now') WHERE id=? AND status='pending'`, row.id)
	if err != nil {
		return offer{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return offer{}, err
	}
	if row, err = h.row(ctx, c.UserID, row.id); err != nil {
		return offer{}, err
	}
	if changed == 0 && row.status != "accepted" {
		return offer{}, errClosed
	}
	h.offerChanged(ctx, c.UserID, row)
	return h.serialize(ctx, row, "receiver")
}

// end finishes an offer and answers with it as role sees it.
func (h *Handler) end(ctx context.Context, userID int64, id, status, role string, from []string, refused error) (offer, error) {
	ended, err := h.finish(ctx, userID, id, status, from)
	if err != nil {
		return offer{}, err
	}
	if !ended && refused != nil {
		return offer{}, refused
	}
	row, err := h.row(ctx, userID, id)
	if err != nil {
		return offer{}, err
	}
	return h.serialize(ctx, row, role)
}

func (h *Handler) decline(ctx context.Context, c caller, id string) (offer, error) {
	row, err := h.receiverRow(ctx, c.UserID, c.key, id)
	if err != nil {
		return offer{}, err
	}
	return h.end(ctx, c.UserID, row.id, "declined", "receiver", []string{"pending", "accepted"}, errClosed)
}

// complete is the receiver saying it has saved everything.
func (h *Handler) complete(ctx context.Context, c caller, id string) (offer, error) {
	row, err := h.receiverRow(ctx, c.UserID, c.key, id)
	if err != nil {
		return offer{}, err
	}
	if row.status == "delivered" {
		return h.serialize(ctx, row, "receiver")
	}
	return h.end(ctx, c.UserID, row.id, "delivered", "receiver", []string{"accepted"},
		failure(http.StatusConflict, "Accept the send first.", "NOT_ACCEPTED"))
}

func (h *Handler) cancel(ctx context.Context, c caller, id string) (offer, error) {
	row, err := h.senderRow(ctx, c.UserID, c.key, id)
	if err != nil {
		return offer{}, err
	}
	if !slices.Contains(openStates, row.status) {
		return h.serialize(ctx, row, "sender")
	}
	return h.end(ctx, c.UserID, row.id, "cancelled", "sender", openStates, nil)
}

// fetched notes files streamed to their end. A browser saves a file with its
// own download and never says it is done, so once every file went out the
// send counts as delivered.
func (h *Handler) fetched(ctx context.Context, userID int64, offerID string, fileIDs ...int64) {
	ctx = context.WithoutCancel(ctx)
	for _, id := range fileIDs {
		if _, err := h.db.ExecContext(ctx, `UPDATE handoff_files SET fetched_at=datetime('now') WHERE id=? AND offer_id=?`, id, offerID); err != nil {
			slog.Error("note a delivered file", "error", err)
			return
		}
	}
	var remaining int
	err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM handoff_files WHERE offer_id=? AND fetched_at IS NULL`, offerID).Scan(&remaining)
	if err == nil && remaining == 0 {
		_, err = h.finish(ctx, userID, offerID, "delivered", []string{"accepted"})
	}
	if err != nil {
		slog.Error("finish a delivered send", "error", err)
	}
}

type offerRef struct {
	id     string
	userID int64
}

func (h *Handler) refs(ctx context.Context, condition string, args ...any) ([]offerRef, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id,user_id FROM handoff_offers WHERE status IN ('uploading','pending','accepted') AND `+condition, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []offerRef
	for rows.Next() {
		var ref offerRef
		if err := rows.Scan(&ref.id, &ref.userID); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// RunMaintenance expires sends past their time and forgets old finished ones.
func (h *Handler) RunMaintenance(ctx context.Context) (int, int64, error) {
	due, err := h.refs(ctx, `datetime(expires_at)<=datetime('now')`)
	if err != nil {
		return 0, 0, err
	}
	expired := 0
	for _, ref := range due {
		if err := ctx.Err(); err != nil {
			return expired, 0, err
		}
		changed, err := h.finish(ctx, ref.userID, ref.id, "expired", openStates)
		if err != nil {
			return expired, 0, err
		}
		if changed {
			expired++
		}
	}
	result, err := h.db.ExecContext(ctx, `DELETE FROM handoff_offers WHERE status NOT IN ('uploading','pending','accepted') AND datetime(updated_at)<datetime('now',?)`, keepFinished)
	if err != nil {
		return expired, 0, err
	}
	forgotten, err := result.RowsAffected()
	if err == nil && (expired > 0 || forgotten > 0) {
		slog.Info(fmt.Sprintf("Send to device: %d expired, %d old record(s) removed.", expired, forgotten))
	}
	return expired, forgotten, err
}

// CancelRevokedDeviceOffers closes the connections of a signed-out device and
// cancels every open send to or from it. A zero deviceID means every revoked
// token of the account except keepDeviceID.
func (h *Handler) CancelRevokedDeviceOffers(ctx context.Context, userID, deviceID, keepDeviceID int64) error {
	if userID < 1 || deviceID < 0 || keepDeviceID < 0 {
		return errors.New("handoff: invalid device revocation identity")
	}
	var keys []string
	if deviceID > 0 {
		keys = []string{deviceKey(deviceID)}
	} else {
		rows, err := h.db.QueryContext(ctx, `SELECT id FROM device_tokens WHERE user_id=? AND revoked_at IS NOT NULL AND id!=?`, userID, keepDeviceID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			keys = append(keys, deviceKey(id))
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
	}
	for _, key := range keys {
		h.presence.dropDevice(userID, key)
		open, err := h.refs(ctx, `user_id=? AND (sender_key=? OR target_key=?)`, userID, key, key)
		if err != nil {
			return err
		}
		for _, ref := range open {
			if _, err := h.finish(ctx, userID, ref.id, "cancelled", openStates); err != nil {
				return fmt.Errorf("handoff: cancel offer %s for revoked device: %w", ref.id, err)
			}
		}
	}
	return nil
}
