package cryptox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
)

// GetUserDEK reads or creates an account's wrapped data key. The conditional
// update makes first access safe when concurrent requests race to create it.
func (k *Keyring) GetUserDEK(ctx context.Context, db *sql.DB, userID int64) ([]byte, error) {
	for attempt := 0; attempt < 5; attempt++ {
		var wrapped sql.NullString
		err := db.QueryRowContext(ctx, `SELECT wrapped_dek FROM users WHERE id = ?`, userID).Scan(&wrapped)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("Account %d does not exist.", userID)
		}
		if err != nil {
			return nil, fmt.Errorf("cryptox: load account data key: %w", err)
		}
		if wrapped.Valid && wrapped.String != "" {
			return k.UnwrapDEK(userID, wrapped.String)
		}

		dek := make([]byte, keyLength)
		if _, err := rand.Read(dek); err != nil {
			return nil, fmt.Errorf("cryptox: generate account data key: %w", err)
		}
		sealed, err := k.WrapDEK(userID, dek)
		if err != nil {
			return nil, err
		}
		result, err := db.ExecContext(ctx, `UPDATE users SET wrapped_dek = ? WHERE id = ? AND wrapped_dek IS NULL`, sealed, userID)
		if err != nil {
			return nil, fmt.Errorf("cryptox: store account data key: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("cryptox: confirm account data key: %w", err)
		}
		if changed == 1 {
			return dek, nil
		}
	}
	return nil, errors.New("cryptox: account data key changed repeatedly while being initialized")
}

// VerifyMasterKey refuses to start when existing account data keys cannot be
// unwrapped by this install's master key. It does not change the database.
func (k *Keyring) VerifyMasterKey(ctx context.Context, db *sql.DB) error {
	var userID int64
	var wrapped string
	err := db.QueryRowContext(ctx, `SELECT id, wrapped_dek FROM users WHERE wrapped_dek IS NOT NULL ORDER BY id LIMIT 1`).Scan(&userID, &wrapped)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cryptox: check install master key: %w", err)
	}
	if _, err := k.UnwrapDEK(userID, wrapped); err != nil {
		return errors.New("NEOCLOUD_MASTER_KEY does not match the key this data was encrypted with. Put the original key back; nothing was changed.")
	}
	return nil
}
