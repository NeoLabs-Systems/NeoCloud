package runtimeapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ErrNoAccount and ErrLastAdmin are the refusals of an admin change.
var (
	ErrNoAccount = errors.New("No account named")
	ErrLastAdmin = errors.New("The last admin can’t be removed.")
)

// Admins lists the administrator usernames, oldest account first.
func Admins(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT username FROM users WHERE is_admin=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// SetAdmin grants or revokes the admin role by username, recording the change
// in the access audit log with its source. It returns the account's own
// spelling and whether anything changed.
func SetAdmin(ctx context.Context, db *sql.DB, username string, admin bool, source string) (string, bool, error) {
	name := strings.TrimSpace(username)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var id int64
	var canonical string
	var current bool
	err = tx.QueryRowContext(ctx, `SELECT id,username,is_admin=1 FROM users WHERE lower(username)=lower(?)`, name).Scan(&id, &canonical, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("%w %q.", ErrNoAccount, name)
	}
	if err != nil || current == admin {
		return canonical, false, err
	}
	action, value := "admin.grant", 1
	if !admin {
		action, value = "admin.revoke", 0
		var admins int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin=1`).Scan(&admins); err != nil {
			return canonical, false, err
		}
		if admins <= 1 {
			return canonical, false, ErrLastAdmin
		}
	}
	detail, err := json.Marshal(map[string]string{"source": source})
	if err != nil {
		return canonical, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_admin=? WHERE id=?`, value, id); err != nil {
		return canonical, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO access_audit_log(action,actor_user_id,subject_user_id,detail_json) VALUES(?,NULL,?,?)`, action, id, string(detail)); err != nil {
		return canonical, false, err
	}
	return canonical, true, tx.Commit()
}

// applyEnvironmentAdminGrants is the additive NEOCLOUD_ADMIN_USERS bootstrap:
// removing a name never revokes, and a missing account is reported, not created
// (auth keeps its name reserved).
func applyEnvironmentAdminGrants(ctx context.Context, db *sql.DB, usernames []string) error {
	for _, name := range usernames {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		canonical, changed, err := SetAdmin(ctx, db, name, true, "env")
		switch {
		case errors.Is(err, ErrNoAccount):
			slog.Warn("NEOCLOUD_ADMIN_USERS names a missing account", "username", name)
		case err != nil:
			return fmt.Errorf("grant admin to %q: %w", name, err)
		case changed:
			slog.Info("Granted admin from NEOCLOUD_ADMIN_USERS", "username", canonical)
		}
	}
	return nil
}
