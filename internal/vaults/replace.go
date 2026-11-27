package vaults

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/jsvalue"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

func parseCursor(body map[string]any) (int64, error) {
	invalid := apperr.New(http.StatusBadRequest, "cursor must be a whole number.", "INVALID_CURSOR")
	value, present := body["cursor"]
	if !present {
		return 0, invalid
	}
	if value == nil {
		return 0, nil
	}
	cursor, ok := jsvalue.Integer(value)
	if !ok || cursor < 0 {
		return 0, invalid
	}
	return cursor, nil
}

// replace moves a converted folder to the trash and gives the vault's folder
// its name and place, in one transaction. It is refused when anything below the
// folder changed after cursor, so a file added or edited during the conversion
// is never trashed unconverted.
func (h *Handler) replace(w http.ResponseWriter, r *http.Request, uid int64) error {
	body, err := apijson.Object(w, r)
	if err != nil {
		return err
	}
	cursor, err := parseCursor(body)
	if err != nil {
		return err
	}
	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := find(ctx, tx, uid, r.PathValue("id"))
	if err != nil {
		return err
	}
	target, err := liveFolder(ctx, tx, uid, v.FolderID)
	if err != nil {
		return err
	}
	source, err := liveFolder(ctx, tx, uid, jsvalue.String(body["folderId"]))
	if err != nil {
		return err
	}
	inside, err := contains(ctx, tx, uid, source.id, target.id)
	if err != nil {
		return err
	}
	if inside {
		return vaultBoundary("The vault is inside the folder it replaces.")
	}
	problems, err := problemsOf(ctx, tx, uid, source)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return apperr.New(http.StatusConflict, "This folder can’t be turned into a vault.", "VAULT_CONVERT_REFUSED")
	}
	changed, err := changedBelow(ctx, tx, uid, source.id, cursor)
	if err != nil {
		return err
	}
	if changed {
		return apperr.New(http.StatusConflict, "The folder changed during the conversion.", "VAULT_SOURCE_CHANGED")
	}
	parent, previous := source.parent.String, target.parent.String
	op, parents := "rename", []any{parent}
	if previous != parent {
		op, parents = "move", []any{previous, parent}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET trashed_at=datetime('now') WHERE id=?`, source.id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO changes(user_id,node_id,op,parent_ids_json) VALUES(?,?,'trash',json_array(?))`, uid, source.id, parent); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET parent_id=?,name=? WHERE id=?`, parent, source.name, target.id); err != nil {
		return err
	}
	list := `json_array(?)`
	if len(parents) == 2 {
		list = `json_array(?,?)`
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO changes(user_id,node_id,op,parent_ids_json) VALUES(?,?,?,`+list+`)`, append([]any{uid, target.id, op}, parents...)...); err != nil {
		return err
	}
	for _, id := range parents {
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET modified_at=datetime('now') WHERE id=?`, id); err != nil {
			return err
		}
	}
	node, err := serializeNode(ctx, tx, uid, target.id)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"folder": node, "trashedId": source.id})
	return nil
}

func changedBelow(ctx context.Context, tx *sql.Tx, uid int64, folderID string, cursor int64) (bool, error) {
	var changed bool
	err := tx.QueryRowContext(ctx, `WITH RECURSIVE down(id,depth) AS (
		SELECT id,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,d.depth+1 FROM nodes n JOIN down d ON n.parent_id=d.id WHERE n.user_id=? AND n.trashed_at IS NULL
	) SELECT EXISTS(SELECT 1 FROM changes c JOIN down d ON d.id=c.node_id WHERE c.user_id=? AND c.seq>? AND d.depth>0)`, folderID, uid, uid, uid, cursor).Scan(&changed)
	return changed, err
}
