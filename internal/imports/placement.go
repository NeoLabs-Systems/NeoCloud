package imports

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/NeoLabs-Systems/neocloud/internal/apijson"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
)

// Where remote items land in the account's tree, and the checks that decide
// whether a file is already there.

func (h *Handler) applyModifiedAt(ctx context.Context, userID int64, nodeID string, item importItem) error {
	if !item.mtime.Valid {
		return nil
	}
	return h.files.SetModifiedAt(ctx, userID, nodeID, item.mtime.String)
}

func (h *Handler) targetFolder(ctx context.Context, j job) (files.Node, error) {
	folder, err := h.files.OwnFolder(ctx, j.userID, j.targetID)
	if apijson.Public(err) != nil {
		err = apperr.New(http.StatusConflict, "The folder to import into is gone (deleted or in the trash).", "IMPORT_TARGET_MISSING")
	}
	return folder, err
}

// existingNode is the item at relative below the job's folder; nothing is
// created.
func (h *Handler) existingNode(ctx context.Context, j job, relative string) (files.Node, bool, error) {
	current := files.Node{ID: j.targetID, Type: "folder"}
	for _, part := range strings.Split(relative, "/") {
		name, err := files.ValidateName(part)
		if err != nil || current.Type != "folder" {
			return files.Node{}, false, nil
		}
		child, found, err := h.files.ChildByName(ctx, j.userID, current.ID, name)
		if err != nil || !found {
			return files.Node{}, false, err
		}
		current = child
	}
	return current, true, nil
}

// isUnchanged: a file counts as already imported when name, size and date all
// match.
func isUnchanged(existing files.Node, name string, item importItem) bool {
	return existing.Type == "file" && existing.Name == name && existing.Size == item.size && item.mtime.Valid && existing.ModifiedAt == item.mtime.String
}

func (h *Handler) folderFor(ctx context.Context, j job, relative string) (files.Node, error) {
	var folder files.Node
	var err error
	if relative == "" {
		folder, err = h.targetFolder(ctx, j)
	} else {
		folder, err = h.files.EnsureFolderPath(ctx, j.userID, j.targetID, relative)
	}
	if err == nil && folder.VaultID != nil {
		err = vaultRefused()
	}
	return folder, err
}

// assertRoomFor fails early when the files still to fetch cannot fit; per-file
// checks follow.
func (h *Handler) assertRoomFor(ctx context.Context, j job) error {
	pending, err := h.items(ctx, j.id, "file", "pending", "path")
	if err != nil {
		return err
	}
	var needed int64
	for _, item := range pending {
		existing, found, err := h.existingNode(ctx, j, item.path)
		if err != nil {
			return err
		}
		name, nameErr := files.ValidateName(path.Base(item.path))
		if !found || nameErr != nil || !isUnchanged(existing, name, item) {
			// A remote server may report absurd sizes; never wrap around.
			needed += min(item.size, 1<<62-needed)
		}
	}
	remaining, err := h.files.RemainingBytes(ctx, j.userID)
	if err != nil {
		return err
	}
	if needed > remaining {
		return apperr.New(http.StatusRequestEntityTooLarge, fmt.Sprintf("Not enough storage: the import needs %s, %s are left.", formatBytes(needed), formatBytes(remaining)), "QUOTA_EXCEEDED")
	}
	return h.files.CheckDiskSpace(needed)
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	scaled, unit := float64(value), "B"
	for _, next := range []string{"KB", "MB", "GB", "TB"} {
		if scaled < 1024 {
			break
		}
		scaled, unit = scaled/1024, next
	}
	precision := 1
	if scaled >= 10 {
		precision = 0
	}
	return fmt.Sprintf("%.*f %s", precision, scaled, unit)
}

// storeFile fetches one file unless it is already there, and returns the item's
// new state.
func (h *Handler) storeFile(ctx context.Context, j job, remote *client, item importItem) (string, error) {
	parentPath, base := path.Split(item.path)
	name, err := files.ValidateName(base)
	if err != nil {
		return "", err
	}
	parent, err := h.folderFor(ctx, j, strings.TrimSuffix(parentPath, "/"))
	if err != nil {
		return "", err
	}
	existing, found, err := h.files.ChildByName(ctx, j.userID, parent.ID, name)
	if err != nil {
		return "", err
	}
	if found && existing.Type == "folder" {
		return "", apperr.New(http.StatusConflict, fmt.Sprintf("A folder named “%s” is in the way.", existing.Name), "NAME_EXISTS")
	}
	nodeID, state := existing.ID, "skipped"
	if !found || !isUnchanged(existing, name, item) {
		if err = h.files.CheckDiskSpace(item.size); err != nil {
			return "", err
		}
		if err = h.files.CheckUploadFits(ctx, j.userID, item.size); err != nil {
			return "", err
		}
		stream, size, err := remote.download(ctx, item.path)
		if err != nil {
			return "", err
		}
		defer stream.Close()
		// A different spelling of the name ("Report.pdf" next to "report.pdf"
		// on a case-sensitive server) is kept beside it, never over it.
		conflict := "rename"
		if found && existing.Name == name {
			conflict = "replace"
		}
		stored, err := h.files.ImportCopy(ctx, j.userID, parent.ID, name, stream, size, conflict)
		if err != nil {
			return "", err
		}
		if err = h.applyModifiedAt(ctx, j.userID, stored.ID, item); err != nil {
			return "", err
		}
		if stored.Size != item.size {
			if _, err = h.db.ExecContext(ctx, `UPDATE import_items SET size=? WHERE job_id=? AND path=?`, stored.Size, j.id, item.path); err != nil {
				return "", err
			}
		}
		nodeID, state = stored.ID, done
	}
	if item.favorite && j.includeFavorites {
		err = h.files.MarkFavorite(ctx, j.userID, nodeID)
	}
	return state, err
}

// finishFolders sets folder dates and favorites last: adding files touches a
// folder's date.
func (h *Handler) finishFolders(ctx context.Context, j job) error {
	folders, err := h.items(ctx, j.id, "folder", done, "length(path) DESC")
	if err != nil {
		return err
	}
	for _, item := range folders {
		node, found, err := h.existingNode(ctx, j, item.path)
		if err != nil {
			return err
		}
		if !found || node.Type != "folder" {
			continue
		}
		if err = h.applyModifiedAt(ctx, j.userID, node.ID, item); err != nil {
			return err
		}
		if item.favorite && j.includeFavorites {
			if err = h.files.MarkFavorite(ctx, j.userID, node.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
