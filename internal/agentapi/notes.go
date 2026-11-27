package agentapi

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
)

// NotesFile is the owner-scoped file representation used by the Notes domain.
type NotesFile struct {
	ID, Name, Path, Type, Revision string
	Size                           int64
	ModifiedAt                     string
	Vault                          bool
	VaultID                        string
}

// NotesRoot returns the live /Notes folder id without creating it.
func (h *Handler) NotesRoot(ctx context.Context, uid int64) (string, error) {
	var root string
	err := h.db.QueryRowContext(ctx, `SELECT n.id FROM nodes n JOIN nodes r ON r.id=n.parent_id WHERE n.user_id=? AND r.user_id=? AND r.parent_id IS NULL AND n.name='Notes' COLLATE NOCASE AND n.type='folder' AND n.trashed_at IS NULL AND n.vault_id IS NULL`, uid, uid).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return root, err
}

// NotesTree returns the live /Notes subtree and marks vault descendants so the
// Notes API can hide their contents without bypassing the file service.
func (h *Handler) NotesTree(ctx context.Context, uid int64) ([]NotesFile, error) {
	root, err := h.NotesRoot(ctx, uid)
	if err != nil {
		return nil, err
	}
	if root == "" {
		return nil, nil
	}
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE tree(id,parent_id,type,name,size,modified_at,etag,vault_id,path,depth,hidden) AS (
	 SELECT id,parent_id,type,name,size,modified_at,etag,vault_id,name,0,CASE WHEN vault_id IS NULL THEN 0 ELSE 1 END FROM nodes WHERE id=? AND user_id=? AND trashed_at IS NULL
	 UNION ALL SELECT n.id,n.parent_id,n.type,n.name,n.size,n.modified_at,n.etag,n.vault_id,tree.path||'/'||n.name,tree.depth+1,CASE WHEN n.vault_id IS NULL AND tree.hidden=0 THEN 0 ELSE 1 END FROM nodes n JOIN tree ON n.parent_id=tree.id WHERE n.user_id=? AND n.trashed_at IS NULL
	) SELECT id,name,type,size,modified_at,etag,hidden,path,COALESCE((SELECT v.id FROM vaults v WHERE v.folder_id=tree.id AND v.user_id=?),'') FROM tree WHERE depth>0 ORDER BY depth,id`, root, uid, uid, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotesFile
	for rows.Next() {
		var n NotesFile
		var hidden int
		if err = rows.Scan(&n.ID, &n.Name, &n.Type, &n.Size, &n.ModifiedAt, &n.Revision, &hidden, &n.Path, &n.VaultID); err != nil {
			return nil, err
		}
		n.Vault = hidden != 0
		out = append(out, n)
	}
	return out, rows.Err()
}

// ReadNote reads a bounded owner note through the account's encrypted blob key.
func (h *Handler) ReadNote(ctx context.Context, uid int64, id string) ([]byte, NotesFile, error) {
	n, err := h.itemAt(ctx, uid, id)
	if err != nil {
		return nil, NotesFile{}, err
	}
	if n.Type != "file" || n.Size > 1<<20 || !strings.HasPrefix(strings.ToLower(n.Path), "/notes/") || !strings.HasSuffix(strings.ToLower(n.Name), ".md") {
		return nil, NotesFile{}, fileError{http.StatusNotFound, "Note not found", "NOT_FOUND"}
	}
	var blob string
	if err = h.db.QueryRowContext(ctx, `SELECT blob_id FROM nodes WHERE id=? AND user_id=?`, id, uid).Scan(&blob); err != nil {
		return nil, NotesFile{}, err
	}
	if len(blob) < 2 {
		return nil, NotesFile{}, errors.New("invalid note blob id")
	}
	dek, err := h.keys.GetUserDEK(ctx, h.db, uid)
	if err != nil {
		return nil, NotesFile{}, err
	}
	f, err := os.Open(filepath.Join(h.blobDir, strconv.FormatInt(uid, 10), blob[:2], blob))
	if err != nil {
		return nil, NotesFile{}, err
	}
	defer f.Close()
	var b bytes.Buffer
	if _, err = cryptox.DecryptBlobRange(f, n.Size, 0, maxInt64(n.Size-1, 0), dek, blob, &b); err != nil {
		return nil, NotesFile{}, err
	}
	return b.Bytes(), NotesFile{ID: n.ID, Name: n.Name, Path: n.Path, Type: n.Type, Revision: n.Revision, Size: n.Size, ModifiedAt: n.ModifiedAt}, nil
}

// WriteNote creates or replaces a Markdown file under /Notes using normal file
// encryption, version retention, quota, activity and sync-journal handling.
func (h *Handler) WriteNote(ctx context.Context, uid int64, path string, body []byte, overwrite bool, revision string) (NotesFile, error) {
	n, err := h.writePath(ctx, uid, path, body, overwrite, revision)
	if err != nil {
		return NotesFile{}, err
	}
	return NotesFile{ID: n.ID, Name: n.Name, Path: n.Path, Type: n.Type, Revision: n.Revision, Size: n.Size, ModifiedAt: n.ModifiedAt}, nil
}

// EnsureNotesFolder creates the Notes root or a notebook path idempotently.
func (h *Handler) EnsureNotesFolder(ctx context.Context, uid int64, path string) (string, bool, error) {
	parts, err := h.pathParts(path)
	if err != nil {
		return "", false, err
	}
	return h.ensureFolderPath(ctx, uid, parts)
}

// MoveNote relocates and/or renames a note while preserving file change rules.
func (h *Handler) MoveNote(ctx context.Context, uid int64, id, name, parentPath string) error {
	if _, err := h.itemAt(ctx, uid, id); err != nil {
		return err
	}
	parent, err := h.findPathNode(ctx, uid, parentPath)
	if err != nil {
		return err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = h.files.UpdateTx(tx, uid, id, &name, &parent, true); err != nil {
		return mapFileError(err)
	}
	return tx.Commit()
}

// TrashNote moves a note to the normal restorable trash.
func (h *Handler) TrashNote(ctx context.Context, uid int64, id string) error {
	if _, err := h.itemAt(ctx, uid, id); err != nil {
		return err
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = h.files.TrashTx(tx, uid, id); err != nil {
		return mapFileError(err)
	}
	return tx.Commit()
}
