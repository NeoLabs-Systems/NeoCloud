package files

import (
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

var contentHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type contentExistsRequest struct {
	ParentID string          `json:"parentId"`
	SHA256   string          `json:"sha256"`
	Size     json.RawMessage `json:"size"`
}

// contentExists checks whether an identical live file is already in the
// selected folder or any live descendant. Shared folders are checked in the
// owner's tree after resolving the caller's grant, matching member uploads.
func (h *Handler) contentExists(w http.ResponseWriter, r *http.Request) {
	viewerID, ok := h.auth(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	var request contentExistsRequest
	if err := decoder.Decode(&request); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return
	}
	if strings.TrimSpace(request.SHA256) == "" {
		writeErr(w, http.StatusBadRequest, "sha256 is required.", "INVALID_HASH")
		return
	}
	hash := strings.ToLower(strings.TrimSpace(request.SHA256))
	if !contentHashPattern.MatchString(hash) {
		writeErr(w, http.StatusBadRequest, "sha256 must be 64 hex characters.", "INVALID_HASH")
		return
	}
	bytes, validSize := parseContentSize(request.Size)
	if !validSize {
		writeErr(w, http.StatusBadRequest, "size must be a byte count.", "INVALID_SIZE")
		return
	}
	size := int64(bytes)
	parentID := strings.TrimSpace(request.ParentID)
	if parentID == "" {
		parentID = "root"
	}
	tx, err := h.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	folder, shared, err := h.resolveReadNode(tx, viewerID, parentID)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if folder.Type != "folder" {
		failTx(w, tx, apiError{Status: http.StatusBadRequest, Message: "That is not a folder.", Code: "NOT_A_FOLDER"})
		return
	}
	ownerID := viewerID
	sharedAccess := 0
	if shared != nil {
		ownerID = shared.ownerID
		sharedAccess = 1
	}
	var found int
	err = tx.QueryRowContext(r.Context(), `WITH RECURSIVE below(id) AS (
		SELECT ?
		UNION ALL
		SELECT n.id FROM nodes n JOIN below b ON n.parent_id=b.id
		WHERE n.user_id=? AND n.type='folder' AND n.trashed_at IS NULL AND (?=0 OR n.vault_id IS NULL)
	) SELECT EXISTS(
		SELECT 1 FROM nodes n JOIN below b ON n.parent_id=b.id
		WHERE n.user_id=? AND n.type='file' AND n.trashed_at IS NULL AND (?=0 OR n.vault_id IS NULL) AND n.etag=? AND n.size=?
	)`, folder.ID, ownerID, sharedAccess, ownerID, sharedAccess, hash, size).Scan(&found)
	if err != nil {
		failTx(w, tx, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"exists": found == 1})
}

// Node converts this input with Number() and accepts any non-negative safe
// integer representation, including decimal notation and numeric strings.
func parseContentSize(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	text := strings.TrimSpace(string(raw))
	if text == "null" {
		return 0, true
	}
	if len(text) > 1 && text[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return 0, false
		}
		text = strings.TrimSpace(value)
		if text == "" {
			return 0, true
		}
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 9007199254740991 || math.Trunc(value) != value {
		return 0, false
	}
	return value, true
}
