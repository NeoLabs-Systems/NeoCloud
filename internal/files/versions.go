package files

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

type fileVersion struct {
	Version    int64   `json:"version"`
	Size       int64   `json:"size"`
	ETag       string  `json:"etag"`
	CreatedAt  string  `json:"createdAt"`
	ModifiedBy *string `json:"modifiedBy"`
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := h.live(tx, uid, r.PathValue("id"))
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if n.Type != "file" {
		failTx(w, tx, notAFile())
		return
	}
	rows, err := tx.Query(`SELECT v.version,v.size,v.etag,v.created_at,u.username FROM file_versions v LEFT JOIN users u ON u.id=v.modified_by WHERE v.node_id=? ORDER BY v.version DESC`, n.ID)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	versions := []fileVersion{}
	for rows.Next() {
		var v fileVersion
		var author sql.NullString
		if err = rows.Scan(&v.Version, &v.Size, &v.ETag, &v.CreatedAt, &author); err != nil {
			rows.Close()
			failTx(w, tx, err)
			return
		}
		if author.Valid {
			v.ModifiedBy = &author.String
		}
		versions = append(versions, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		failTx(w, tx, err)
		return
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"current": n, "versions": versions})
}

// restoreVersion makes a kept version the current content again and keeps what
// was current as a version.
func (h *Handler) restoreVersion(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.auth(w, r)
	if !ok {
		return
	}
	var updated node
	var orphans []string
	err := h.inTx(r.Context(), func(tx *sql.Tx) error {
		n, err := h.live(tx, uid, r.PathValue("id"))
		if err != nil {
			return err
		}
		if n.Type != "file" {
			return notAFile()
		}
		missing := apiError{404, "Version not found", "NOT_FOUND"}
		version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
		if err != nil {
			return missing
		}
		var blobID, etag string
		var size int64
		err = tx.QueryRow(`SELECT blob_id,size,etag FROM file_versions WHERE node_id=? AND version=?`, n.ID, version).Scan(&blobID, &size, &etag)
		if errors.Is(err, sql.ErrNoRows) {
			return missing
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE blobs SET ref_count=ref_count+1 WHERE id=? AND user_id=?`, blobID, uid); err != nil {
			return err
		}
		if orphans, err = h.replaceContentTx(tx, uid, uid, n, blobID, size, etag, nil); err != nil {
			return err
		}
		updated, err = nodeByID(tx, uid, n.ID)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	h.RemoveBlobFiles(uid, orphans)
	httpx.WriteJSON(w, 200, updated)
}

func notAFile() error { return apiError{400, "That is not a file.", "NOT_A_FILE"} }
