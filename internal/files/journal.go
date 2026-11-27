package files

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/NeoLabs-Systems/neocloud/internal/activity"
)

// recordChange journals one tree mutation for sync clients. folders are the
// folders it touched; without any it names the node's current parent, which is
// what members of a shared folder are told about.
func recordChange(tx *sql.Tx, ownerID int64, nodeID, op string, folders ...string) error {
	if len(folders) == 0 {
		var parent sql.NullString
		if err := tx.QueryRow(`SELECT parent_id FROM nodes WHERE id=? AND user_id=?`, nodeID, ownerID).Scan(&parent); err != nil {
			return err
		}
		folders = []string{parent.String}
	}
	affected := []string{}
	for _, folder := range folders {
		if folder != "" && !slices.Contains(affected, folder) {
			affected = append(affected, folder)
		}
	}
	encoded, err := json.Marshal(affected)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO changes(user_id,node_id,op,parent_ids_json) VALUES(?,?,?,?)`, ownerID, nodeID, op, string(encoded))
	return err
}

// recordActivity adds the node, as it is now, to the activity feed.
func recordActivity(tx *sql.Tx, ownerID, actorID int64, kind, nodeID string, detail map[string]any) error {
	return activity.Record(context.Background(), tx, activity.Event{OwnerID: ownerID, ActorID: actorID, Kind: kind, NodeID: nodeID, Detail: detail})
}

func touchFolder(tx *sql.Tx, folderID string) error {
	_, err := tx.Exec(`UPDATE nodes SET modified_at=datetime('now') WHERE id=?`, folderID)
	return err
}
