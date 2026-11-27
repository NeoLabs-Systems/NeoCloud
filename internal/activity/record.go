package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
)

const (
	burstWindow = "-5 minutes"
	nameSample  = 3
)

// Kinds that fold into one entry with a count when one account repeats them in
// one folder within the burst window.
var burstKinds = map[string]bool{"upload": true, "folder": true, "copy": true, "trash": true, "restore": true, "move": true}

var kinds = map[string]bool{"upload": true, "folder": true, "copy": true, "version": true, "rename": true, "move": true, "trash": true, "restore": true, "share": true, "comment": true}

// Querier is the transaction of the change being logged.
type Querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Event is one thing that happened to a node in an account's tree.
type Event struct {
	OwnerID int64
	// ActorID is the account that did it; zero means the owner.
	ActorID int64
	Kind    string
	NodeID  string
	// Detail is extra context: the old name of a rename ("from"), the member of
	// a share ("granteeId").
	Detail map[string]any
}

// Record logs an event from the node's current row, so call it after the change
// inside the same transaction. Vault contents are never logged.
func Record(ctx context.Context, tx Querier, event Event) error {
	if !kinds[event.Kind] {
		return errors.New("activity: unknown kind " + event.Kind)
	}
	var parent, vault sql.NullString
	var nodeType, name string
	if err := tx.QueryRowContext(ctx, `SELECT parent_id,type,name,vault_id FROM nodes WHERE id=?`, event.NodeID).Scan(&parent, &nodeType, &name, &vault); err != nil {
		return err
	}
	if vault.Valid {
		return nil
	}
	actor := event.ActorID
	if actor == 0 {
		actor = event.OwnerID
	}
	count := int64(1)
	var detail any = event.Detail
	if burstKinds[event.Kind] {
		var lastID, lastCount int64
		var lastName string
		var lastDetail sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id,name,count,detail FROM activity WHERE owner_id=? AND actor_id=? AND kind=? AND parent_id IS ? AND at>=datetime('now',?) ORDER BY id DESC LIMIT 1`,
			event.OwnerID, actor, event.Kind, parent, burstWindow).Scan(&lastID, &lastName, &lastCount, &lastDetail)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			// The row is replaced so the feed stays in the order of the events.
			names := parseDetail(lastDetail).Names
			if len(names) == 0 {
				names = []string{lastName}
			}
			if !slices.Contains(names, name) && len(names) < nameSample {
				names = append(names, name)
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM activity WHERE id=?`, lastID); err != nil {
				return err
			}
			count, detail = lastCount+1, map[string]any{"names": names}
		}
	}
	var encoded any
	if values, ok := detail.(map[string]any); ok && values != nil {
		raw, err := json.Marshal(values)
		if err != nil {
			return err
		}
		encoded = string(raw)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO activity(owner_id,actor_id,kind,node_id,parent_id,node_type,name,detail,count) VALUES(?,?,?,?,?,?,?,?,?)`,
		event.OwnerID, actor, event.Kind, event.NodeID, parent, nodeType, name, encoded, count)
	return err
}
