package photos

import (
	"encoding/json"
	"math"
	"math/bits"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

// Before camera upload sends a photo: is an almost identical one (at least
// 95 %, so 64-bit dHashes at most 3 bits apart) already among the account's
// own photos? The phone skips those. Only photos hashed already can match.
const (
	maxSimilarHashes = 200
	maxHashDistance  = 3
	maxSimilarBody   = 64 << 10
)

var requestedHash = regexp.MustCompile(`^[0-9a-f]{16}$`)

type similarMatch struct {
	Hash       string  `json:"hash"`
	NodeID     string  `json:"nodeId"`
	Similarity float64 `json:"similarity"`
}

func (h *Handler) similarUploads(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	var body struct {
		Hash   any `json:"hash"`
		Hashes any `json:"hashes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSimilarBody)).Decode(&body); err != nil {
		refuse(w, http.StatusBadRequest, "Invalid JSON request", "INVALID_JSON")
		return
	}
	list, isList := body.Hashes.([]any)
	if !isList && body.Hash != nil {
		list = []any{body.Hash}
	}
	if len(list) == 0 {
		refuse(w, http.StatusBadRequest, "Send a hash or a list of hashes.", "INVALID_HASH")
		return
	}
	if len(list) > maxSimilarHashes {
		refuse(w, http.StatusBadRequest, "At most 200 hashes at a time.", "TOO_MANY_HASHES")
		return
	}
	asked := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, value := range list {
		text, _ := value.(string)
		text = strings.ToLower(strings.TrimSpace(text))
		if !requestedHash.MatchString(text) {
			refuse(w, http.StatusBadRequest, "A hash is 16 hexadecimal characters.", "INVALID_HASH")
			return
		}
		if !seen[text] {
			seen[text] = true
			asked = append(asked, text)
		}
	}
	if err := h.CatchUp(r.Context(), uid); err != nil {
		internal(w)
		return
	}
	h.wakeWorker()
	matches, err := h.closest(r, uid, asked)
	if err != nil {
		internal(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

// closest finds, for every asked hash, the closest of the account's own live
// photos (the photo index leaves out vaults and the trash).
func (h *Handler) closest(r *http.Request, uid int64, asked []string) ([]similarMatch, error) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT n.id, h.dhash
		FROM photo_items p
		JOIN nodes n ON n.id=p.node_id AND n.blob_id=p.blob_id
		JOIN image_hashes h ON h.node_id=n.id AND h.etag=n.etag
		WHERE p.user_id=? AND p.kind='photo' AND h.dhash IS NOT NULL
		  AND n.user_id=p.user_id AND n.vault_id IS NULL AND n.trashed_at IS NULL
		ORDER BY n.created_at, n.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type known struct {
		id   string
		hash uint64
	}
	var photos []known
	for rows.Next() {
		var id, encoded string
		if err = rows.Scan(&id, &encoded); err != nil {
			return nil, err
		}
		if hash, parseErr := strconv.ParseUint(encoded, 16, 64); parseErr == nil {
			photos = append(photos, known{id, hash})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	matches := make([]similarMatch, 0)
	for _, text := range asked {
		hash, err := strconv.ParseUint(text, 16, 64)
		if err != nil {
			return nil, err
		}
		best, distance := "", maxHashDistance+1
		for _, photo := range photos {
			if found := bits.OnesCount64(hash ^ photo.hash); found < distance {
				best, distance = photo.id, found
				if found == 0 {
					break
				}
			}
		}
		if best != "" {
			matches = append(matches, similarMatch{Hash: text, NodeID: best, Similarity: math.Round(float64(64-distance)/64*1000) / 10})
		}
	}
	return matches, nil
}
