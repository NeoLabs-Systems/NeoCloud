package imports

import "context"

// CancelAccount ends the account's imports before it is deleted: their stored
// passwords go, and a running job has stopped by the time this returns.
func (h *Handler) CancelAccount(ctx context.Context, userID int64) error {
	if _, err := h.db.ExecContext(ctx, `UPDATE import_jobs SET status=?,password_enc=NULL WHERE user_id=?`, cancelled, userID); err != nil {
		return err
	}
	h.mu.Lock()
	var running []*run
	for _, current := range h.active {
		if current.userID == userID {
			current.cancel()
			running = append(running, current)
		}
	}
	h.mu.Unlock()
	for _, current := range running {
		select {
		case <-current.finished:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
