package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
)

const maxSecurityKeyChallenges = 2000

var errSecurityKeyChallengeLimit = errors.New("too many security key sign-ins in progress")

// storeSecurityKeyChallenge persists the challenge so a later request can be
// handled by another process. Expired rows are pruned while holding SQLite's
// write lock, keeping the global cap meaningful across instances.
func (h *Handler) storeSecurityKeyChallenge(ctx context.Context, id string, challenge securityKeyChallenge) error {
	var pending any
	if challenge.Pending != nil {
		encoded, err := json.Marshal(challenge.Pending)
		if err != nil {
			return err
		}
		pending = string(encoded)
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := h.now()
	if _, err = tx.ExecContext(ctx, `DELETE FROM security_key_login_challenges WHERE expires_at<=?`, now.UnixMilli()); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_key_login_challenges`).Scan(&count); err != nil {
		return err
	}
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM security_key_login_challenges WHERE challenge_id=?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) && count >= maxSecurityKeyChallenges {
		return errSecurityKeyChallengeLimit
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO security_key_login_challenges
		(challenge_id,stage,pending_json,user_id,device_name,platform,expires_at,failures)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(challenge_id) DO UPDATE SET stage=excluded.stage,pending_json=excluded.pending_json,
		user_id=excluded.user_id,device_name=excluded.device_name,platform=excluded.platform,
		expires_at=excluded.expires_at,failures=excluded.failures`,
		id, challenge.Stage, pending, nullableUserID(challenge.UserID), challenge.Name,
		challenge.Platform, challenge.Expires.UnixMilli(), challenge.Failures)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullableUserID(id int64) any {
	if id < 1 {
		return nil
	}
	return id
}

// takeSecurityKeyChallenge consumes a matching, unexpired challenge in one
// DELETE ... RETURNING statement, so a replay or concurrent verifier can win
// at most once across all handlers sharing the database.
func (h *Handler) takeSecurityKeyChallenge(ctx context.Context, id, stage string) (securityKeyChallenge, bool, error) {
	var challenge securityKeyChallenge
	var pending sql.NullString
	var userID sql.NullInt64
	var expiry int64
	err := h.db.QueryRowContext(ctx, `DELETE FROM security_key_login_challenges
		WHERE challenge_id=? AND stage=? AND expires_at>?
		RETURNING stage,pending_json,user_id,device_name,platform,expires_at,failures`,
		id, stage, h.now().UnixMilli()).Scan(&challenge.Stage, &pending, &userID, &challenge.Name,
		&challenge.Platform, &expiry, &challenge.Failures)
	if errors.Is(err, sql.ErrNoRows) {
		// Remove an expired row without making it usable.
		_, cleanupErr := h.db.ExecContext(ctx, `DELETE FROM security_key_login_challenges WHERE challenge_id=? AND expires_at<=?`, id, h.now().UnixMilli())
		return securityKeyChallenge{}, false, cleanupErr
	}
	if err != nil {
		return securityKeyChallenge{}, false, err
	}
	challenge.Expires = time.UnixMilli(expiry)
	if userID.Valid {
		challenge.UserID = userID.Int64
	}
	if pending.Valid {
		var value webauthnx.Pending
		if err := json.Unmarshal([]byte(pending.String), &value); err != nil {
			return securityKeyChallenge{}, false, err
		}
		challenge.Pending = &value
	}
	return challenge, true, nil
}
