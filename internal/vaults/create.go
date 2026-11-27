package vaults

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/activity"
	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/jsvalue"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

var forbiddenName = regexp.MustCompile(`[\x00-\x1f\x7f/\\]`)
var numberedSuffix = regexp.MustCompile(` \(\d+\)$`)

func cleanName(raw string) (string, error) {
	invalid := func(message, code string) (string, error) {
		return "", apperr.New(http.StatusBadRequest, message, code)
	}
	name := strings.TrimSpace(norm.NFC.String(raw))
	switch {
	case name == "":
		return invalid("Name can’t be empty.", "NAME_EMPTY")
	case name == "." || name == "..":
		return invalid("That name is reserved.", "NAME_RESERVED")
	case forbiddenName.MatchString(name):
		return invalid("Names can’t contain / or \\ or control characters.", "NAME_INVALID")
	case len(name) > 255:
		return invalid("Name is too long.", "NAME_TOO_LONG")
	}
	return name, nil
}

func conflictMode(value any) (string, error) {
	mode := jsvalue.String(value)
	switch mode {
	case "", "fail":
		return "fail", nil
	case "rename":
		return mode, nil
	case "replace":
		return "", apperr.New(http.StatusBadRequest, "conflict must be fail or rename.", "CONFLICT_MODE_INVALID")
	}
	return "", apperr.New(http.StatusBadRequest, "conflict must be fail, rename or replace.", "CONFLICT_MODE_INVALID")
}

// create makes a new vault folder. Vaults don't nest.
func (h *Handler) create(w http.ResponseWriter, r *http.Request, uid int64) error {
	body, err := apijson.Object(w, r)
	if err != nil {
		return err
	}
	key, err := parseKey(body)
	if err != nil {
		return err
	}
	mode, err := conflictMode(body["conflict"])
	if err != nil {
		return err
	}
	parentID := rootAlias
	if body["parentId"] != nil {
		parentID = jsvalue.String(body["parentId"])
	}
	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parent, err := liveFolder(ctx, tx, uid, parentID)
	if err != nil {
		return err
	}
	if parent.vault.Valid {
		return vaultBoundary("Items can’t be moved or copied into or out of a vault.")
	}
	name, err := cleanName(jsvalue.String(body["name"]))
	if err != nil {
		return err
	}
	if name, err = freeName(ctx, tx, parent.id, name, mode); err != nil {
		return err
	}
	var nodes int
	if err = tx.QueryRowContext(ctx, `SELECT node_count FROM users WHERE id=?`, uid).Scan(&nodes); err != nil {
		return err
	}
	if nodes+1 > h.maxNodes {
		return apperr.New(http.StatusInsufficientStorage, fmt.Sprintf("This account has reached its limit of %d files and folders (trash included).", h.maxNodes), "NODE_LIMIT")
	}
	folderID, vaultID := uuid.NewString(), uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,?,'folder',?,?)`, folderID, uid, parent.id, name, uuid.NewString()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO changes(user_id,node_id,op,parent_ids_json) VALUES(?,?,'create',json_array(?))`, uid, folderID, parent.id); err != nil {
		return err
	}
	// Logged while the folder is still plain: its own name is not secret.
	if err = activity.Record(ctx, tx, activity.Event{OwnerID: uid, Kind: "folder", NodeID: folderID}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO vaults(id,user_id,folder_id,salt,iterations,wrapped_key) VALUES(?,?,?,?,?,?)`, vaultID, uid, folderID, key.salt, key.iterations, key.wrappedKey); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET vault_id=? WHERE id=?`, vaultID, folderID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET modified_at=datetime('now') WHERE id=?`, parent.id); err != nil {
		return err
	}
	v, err := find(ctx, tx, uid, vaultID)
	if err != nil {
		return err
	}
	node, err := serializeNode(ctx, tx, uid, folderID)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, struct {
		record
		Folder map[string]any `json:"folder"`
	}{v, node})
	return nil
}

// freeName returns name, or with rename the first "name (n)" nobody has.
func freeName(ctx context.Context, tx *sql.Tx, parentID, name, mode string) (string, error) {
	ext := filepath.Ext(name)
	if ext == name {
		ext = ""
	}
	stem := numberedSuffix.ReplaceAllString(strings.TrimSuffix(name, ext), "")
	candidate := name
	for number := 1; number <= 10000; number++ {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT name FROM nodes WHERE parent_id=? AND lower(name)=lower(?) AND trashed_at IS NULL`, parentID, candidate).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		if mode != "rename" {
			return "", apperr.New(http.StatusConflict, "“"+existing+"” already exists here.", "NAME_EXISTS")
		}
		candidate = fmt.Sprintf("%s (%d)%s", stem, number, ext)
	}
	return "", apperr.New(http.StatusConflict, "Too many items with that name.", "NAME_EXISTS")
}
