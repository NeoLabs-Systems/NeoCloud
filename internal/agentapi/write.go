package agentapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

type writeBody map[string]json.RawMessage

func decodeWriteBody(w http.ResponseWriter, r *http.Request) (writeBody, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*maxContentBytes+64*1024)
	decoder := json.NewDecoder(r.Body)
	var body writeBody
	if err := decoder.Decode(&body); err != nil || body == nil {
		errorJSON(w, http.StatusBadRequest, "Invalid request body.", "INVALID_REQUEST")
		return nil, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		errorJSON(w, http.StatusBadRequest, "Invalid request body.", "INVALID_REQUEST")
		return nil, false
	}
	return body, true
}

func bodyString(body writeBody, key string) (string, bool, error) {
	raw, ok := body[key]
	if !ok {
		return "", false, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, fileError{http.StatusBadRequest, key + " must be text.", "INVALID_REQUEST"}
	}
	return value, true, nil
}

func bodyBool(body writeBody, key string, fallback bool) (bool, error) {
	raw, ok := body[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fallback, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, fileError{http.StatusBadRequest, key + " must be true or false.", "INVALID_REQUEST"}
	}
	return value, nil
}

func (h *Handler) createFile(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeWriteBody(w, r)
	if !ok {
		return
	}
	path, _, err := bodyString(body, "path")
	if err != nil {
		writeFileError(w, err)
		return
	}
	content, err := requestContent(body)
	if err != nil {
		writeFileError(w, err)
		return
	}
	overwrite, err := bodyBool(body, "overwrite", false)
	if err != nil {
		writeFileError(w, err)
		return
	}
	item, err := h.writePath(r.Context(), principal(r).UserID, path, content, overwrite, "")
	if err != nil {
		writeFileError(w, err)
		return
	}
	item.Revision = ""
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) updateContent(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeWriteBody(w, r)
	if !ok {
		return
	}
	uid, id := principal(r).UserID, r.PathValue("id")
	current, err := h.itemAt(r.Context(), uid, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	if current.Type != "file" {
		writeFileError(w, fileError{http.StatusBadRequest, "That is not a file.", "NOT_A_FILE"})
		return
	}
	content, err := requestContent(body)
	if err != nil {
		writeFileError(w, err)
		return
	}
	revision, hasRevision, err := bodyString(body, "baseRevision")
	if err != nil || hasRevision && revision == "" && !bytes.Equal(bytes.TrimSpace(body["baseRevision"]), []byte("null")) {
		if err == nil {
			err = fileError{http.StatusBadRequest, "baseRevision must be text.", "INVALID_REQUEST"}
		}
		writeFileError(w, err)
		return
	}
	if revision != "" && revision != current.Revision {
		writeFileError(w, fileError{http.StatusConflict, "The file changed since that revision.", "CONFLICT"})
		return
	}
	var parent sql.NullString
	var name string
	if err = h.db.QueryRowContext(r.Context(), `SELECT parent_id,name FROM nodes WHERE id=? AND user_id=?`, id, uid).Scan(&parent, &name); err != nil || !parent.Valid {
		if err == nil {
			err = fileError{http.StatusBadRequest, "The root folder can’t be changed.", "ROOT_IMMUTABLE"}
		}
		writeFileError(w, err)
		return
	}
	item, err := h.storeFile(r.Context(), uid, parent.String, name, content, true, revision)
	if err != nil {
		writeFileError(w, err)
		return
	}
	current, err = h.itemAt(r.Context(), uid, item.ID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, current)
}

func (h *Handler) createFolder(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeWriteBody(w, r)
	if !ok {
		return
	}
	path, _, err := bodyString(body, "path")
	if err != nil {
		writeFileError(w, err)
		return
	}
	parts, err := h.pathParts(path)
	if err != nil {
		writeFileError(w, err)
		return
	}
	if len(parts) == 0 {
		writeFileError(w, fileError{http.StatusBadRequest, "path must name a folder.", "INVALID_PATH"})
		return
	}
	id, created, err := h.ensureFolderPath(r.Context(), principal(r).UserID, parts)
	if err != nil {
		writeFileError(w, err)
		return
	}
	item, err := h.itemAt(r.Context(), principal(r).UserID, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	item.Revision = ""
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, item)
}

func (h *Handler) moveOrRename(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeWriteBody(w, r)
	if !ok {
		return
	}
	uid, id := principal(r).UserID, r.PathValue("id")
	if _, err := h.itemAt(r.Context(), uid, id); err != nil {
		writeFileError(w, err)
		return
	}
	name, nameSet, err := bodyString(body, "name")
	if err != nil {
		writeFileError(w, err)
		return
	}
	parentPath, parentSet, err := bodyString(body, "parentPath")
	if err != nil {
		writeFileError(w, err)
		return
	}
	if nameSet && name == "" || !nameSet && !parentSet {
		writeFileError(w, fileError{http.StatusBadRequest, "Send name or parentPath.", "INVALID_REQUEST"})
		return
	}
	if nameSet {
		name, err = validateAgentName(name)
		if err != nil {
			writeFileError(w, err)
			return
		}
	}
	var targetID string
	if parentSet {
		targetID, err = h.findPathNode(r.Context(), uid, parentPath)
		if err == nil {
			var kind string
			err = h.db.QueryRowContext(r.Context(), `SELECT type FROM nodes WHERE id=? AND user_id=?`, targetID, uid).Scan(&kind)
			if err == nil && kind != "folder" {
				err = fileError{http.StatusBadRequest, "That is not a folder.", "NOT_A_FOLDER"}
			}
		}
		if err != nil {
			writeFileError(w, err)
			return
		}
	}
	if err = h.applyMoveRename(r.Context(), uid, id, name, nameSet, targetID, parentSet); err != nil {
		writeFileError(w, err)
		return
	}
	item, err := h.itemAt(r.Context(), uid, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	item.Revision = ""
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) trashItem(w http.ResponseWriter, r *http.Request) {
	uid, id := principal(r).UserID, r.PathValue("id")
	item, err := h.itemAt(r.Context(), uid, id)
	if err != nil {
		writeFileError(w, err)
		return
	}
	if item.Path == "/" {
		writeFileError(w, fileError{http.StatusBadRequest, "The root folder can’t be deleted.", "ROOT_IMMUTABLE"})
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = h.files.TrashTx(tx, uid, id); err != nil {
		writeFileError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		writeFileError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"trashed": true})
}

func requestContent(body writeBody) ([]byte, error) {
	contentRaw, hasContent := body["content"]
	base64Raw, hasBase64 := body["contentBase64"]
	if hasContent && bytes.Equal(bytes.TrimSpace(contentRaw), []byte("null")) {
		hasContent = false
	}
	if hasBase64 && bytes.Equal(bytes.TrimSpace(base64Raw), []byte("null")) {
		hasBase64 = false
	}
	if hasContent == hasBase64 {
		return nil, fileError{http.StatusBadRequest, "Send either content or contentBase64.", "INVALID_REQUEST"}
	}
	var content string
	if hasContent {
		if err := json.Unmarshal(contentRaw, &content); err != nil {
			return nil, fileError{http.StatusBadRequest, "content must be text.", "INVALID_REQUEST"}
		}
		if len(content) > maxContentBytes {
			return nil, fileError{http.StatusRequestEntityTooLarge, "The content is too large (5 MB at most).", "TOO_LARGE"}
		}
		return []byte(content), nil
	}
	var encoded string
	if err := json.Unmarshal(base64Raw, &encoded); err != nil || !base64AgentPattern.MatchString(strings.Join(strings.Fields(encoded), "")) {
		return nil, fileError{http.StatusBadRequest, "contentBase64 is not valid base64.", "INVALID_REQUEST"}
	}
	encoded = strings.Join(strings.Fields(encoded), "")
	encoded = strings.NewReplacer("-", "+", "_", "/").Replace(encoded)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(encoded, "="))
	}
	if err != nil {
		return nil, fileError{http.StatusBadRequest, "contentBase64 is not valid base64.", "INVALID_REQUEST"}
	}
	if len(decoded) > maxContentBytes {
		return nil, fileError{http.StatusRequestEntityTooLarge, "The content is too large (5 MB at most).", "TOO_LARGE"}
	}
	return decoded, nil
}

var base64AgentPattern = regexp.MustCompile(`^[A-Za-z0-9+/_-]*={0,2}$`)

func validateAgentName(raw string) (string, error) {
	name := strings.TrimSpace(norm.NFC.String(raw))
	if name == "" || name == "." || name == ".." {
		return "", fileError{http.StatusBadRequest, "Invalid file or folder name.", "INVALID_REQUEST"}
	}
	if len([]byte(name)) > 255 {
		return "", fileError{http.StatusBadRequest, "Name is too long.", "INVALID_REQUEST"}
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return "", fileError{http.StatusBadRequest, "Invalid file or folder name.", "INVALID_REQUEST"}
		}
	}
	return name, nil
}

func (h *Handler) ensureFolderPath(ctx context.Context, uid int64, parts []string) (string, bool, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	root, err := rootInTx(ctx, tx, uid)
	if err != nil {
		return "", false, err
	}
	current := root
	created := false
	for _, raw := range parts {
		name, nameErr := validateAgentName(raw)
		if nameErr != nil {
			return "", false, nameErr
		}
		var child, kind string
		var vault sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT id,type,vault_id FROM nodes WHERE user_id=? AND parent_id=? AND lower(name)=lower(?) AND trashed_at IS NULL`, uid, current, name).Scan(&child, &kind, &vault)
		if err == nil {
			if vault.Valid {
				return "", false, fileError{http.StatusNotFound, "File or folder not found", "NOT_FOUND"}
			}
			if kind != "folder" {
				return "", false, fileError{http.StatusConflict, fmt.Sprintf("%q is a file, not a folder.", name), "NOT_A_FOLDER"}
			}
			current = child
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
		next, made, ensureErr := h.files.EnsureFolderTx(tx, uid, current, name)
		if ensureErr != nil {
			return "", false, mapFileError(ensureErr)
		}
		current = next
		if made {
			created = true
		}
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	return current, created, nil
}

func rootInTx(ctx context.Context, tx *sql.Tx, uid int64) (string, error) {
	if _, err := tx.ExecContext(ctx, ensureRootSQL, uuid.NewString(), uid, uuid.NewString(), uid); err != nil {
		return "", err
	}
	var root string
	err := tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL AND trashed_at IS NULL`, uid).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fileError{http.StatusNotFound, "File or folder not found", "NOT_FOUND"}
	}
	return root, err
}

func (h *Handler) writePath(ctx context.Context, uid int64, rawPath string, data []byte, overwrite bool, revision string) (node, error) {
	parts, err := h.pathParts(rawPath)
	if err != nil {
		return node{}, err
	}
	if len(parts) == 0 {
		return node{}, fileError{http.StatusBadRequest, "path must name a file.", "INVALID_PATH"}
	}
	name, err := validateAgentName(parts[len(parts)-1])
	if err != nil {
		return node{}, err
	}
	parentID, _, err := h.ensureFolderPath(ctx, uid, parts[:len(parts)-1])
	if err != nil {
		return node{}, err
	}
	return h.storeFile(ctx, uid, parentID, name, data, overwrite, revision)
}

func (h *Handler) storeFile(ctx context.Context, uid int64, parentID, name string, data []byte, overwrite bool, revision string) (node, error) {
	if int64(len(data)) > maxContentBytes {
		return node{}, fileError{http.StatusRequestEntityTooLarge, "The content is too large (5 MB at most).", "TOO_LARGE"}
	}
	return h.storeBytes(ctx, uid, parentID, name, data, overwrite, revision)
}

// storeBytes is storeFile without the limit on what a request body may send:
// files the server renders itself are bounded by the upload limit only.
func (h *Handler) storeBytes(ctx context.Context, uid int64, parentID, name string, data []byte, overwrite bool, revision string) (node, error) {
	conflict := "fail"
	if overwrite {
		conflict = "replace"
	}
	stored, err := h.files.StoreFile(ctx, uid, parentID, name, bytes.NewReader(data), int64(len(data)), conflict, revision)
	if err != nil {
		return node{}, mapFileError(err)
	}
	return h.itemAt(ctx, uid, stored.ID)
}

func (h *Handler) applyMoveRename(ctx context.Context, uid int64, id, name string, nameSet bool, targetParent string, parentSet bool) error {
	if _, err := h.itemAt(ctx, uid, id); err != nil {
		return err
	}
	var newName, newParent *string
	if nameSet {
		newName = &name
	}
	if parentSet {
		if _, err := h.itemAt(ctx, uid, targetParent); err != nil {
			return err
		}
		newParent = &targetParent
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = h.files.UpdateTx(tx, uid, id, newName, newParent, false); err != nil {
		return mapFileError(err)
	}
	return tx.Commit()
}
