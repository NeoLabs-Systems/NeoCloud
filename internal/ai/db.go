package ai

import (
	"context"
	"database/sql"
	"encoding/json"
)

// querier is the database or a transaction. The database has one writer
// connection, so rows are always read to the end before the next query.
type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type scanner interface{ Scan(...any) error }

func queryAll[T any](ctx context.Context, q querier, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanString(row scanner) (string, error) {
	var value string
	err := row.Scan(&value)
	return value, err
}

func queryStrings(ctx context.Context, q querier, query string, args ...any) ([]string, error) {
	return queryAll(ctx, q, scanString, query, args...)
}

func (h *Handler) inTx(ctx context.Context, run func(*sql.Tx) error) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = run(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func jsonText(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
