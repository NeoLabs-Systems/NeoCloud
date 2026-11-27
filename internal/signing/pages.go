package signing

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"slices"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
)

// Turning pages of a document that is still being prepared. The frozen PDF
// is written again with the new /Rotate of each turned page: nothing on the
// page is redrawn, every later step (the pages clients show, the final file,
// its seal) works on the turned file, and the boxes on a turned page move
// with it. The audit trail records the turn with the hashes before and after,
// so the original hash in the "created" entry still leads to the file signed.

// parseTurns reads {"angle": 90|180|270|-90, "pages": [0, 2]}; no pages means
// all of them.
func parseTurns(input body, pageCount int) (map[int]int, []any, int, error) {
	angle, ok := input["angle"].(float64)
	if !ok || angle != math.Trunc(angle) || int(angle)%90 != 0 || int(angle)%360 == 0 {
		return nil, nil, 0, badRequest("angle is a multiple of 90 degrees, like 90, 180, 270 or -90.")
	}
	turns := ((int(angle)/90)%4 + 4) % 4
	var pages []int
	switch raw := input["pages"].(type) {
	case nil:
		for i := range pageCount {
			pages = append(pages, i)
		}
	case []any:
		for _, item := range raw {
			n, ok := item.(float64)
			if !ok || n != math.Trunc(n) || n < 0 || int(n) >= pageCount {
				return nil, nil, 0, badRequest("pages lists page numbers of this document, counted from 0.")
			}
			if !slices.Contains(pages, int(n)) {
				pages = append(pages, int(n))
			}
		}
		if len(pages) == 0 {
			return nil, nil, 0, badRequest("pages lists at least one page.")
		}
	default:
		return nil, nil, 0, badRequest("pages lists page numbers of this document, counted from 0.")
	}
	slices.Sort(pages)
	out := make(map[int]int, len(pages))
	listed := make([]any, len(pages))
	for i, p := range pages {
		out[p] = turns
		listed[i] = p
	}
	return out, listed, turns * 90, nil
}

func (h *Handler) rotatePages(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	d, err := ownedDocument(ctx, h.db, userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if err := editable(ctx, h.db, d); err != nil {
		return nil, err
	}
	turns, pages, angle, err := parseTurns(input, d.PageCount)
	if err != nil {
		return nil, err
	}
	source, err := h.readBlob(ctx, userID, d.SnapshotBlobID, d.SnapshotSize)
	if err != nil {
		return nil, err
	}
	turned, info, err := signpdf.Rotate(ctx, source, turns)
	if err != nil {
		return nil, pdfRefusal(err)
	}
	if err := h.checkRoom(ctx, h.db, userID, int64(len(turned)), "Not enough storage left to turn the pages."); err != nil {
		return nil, err
	}
	blob, err := h.storeBlob(ctx, userID, turned)
	if err != nil {
		return nil, err
	}
	var orphaned []string
	_, err = h.change(ctx, userID, d.ID, func(tx *sql.Tx, current document) error {
		if err := editable(ctx, tx, current); err != nil {
			return err
		}
		if current.SnapshotBlobID != d.SnapshotBlobID {
			return httpError(409, "The pages changed meanwhile. Reload the document and try again.", "SNAPSHOT_CHANGED")
		}
		var sizes []signpdf.Page
		if err := json.Unmarshal([]byte(current.PagesJSON), &sizes); err != nil || len(sizes) != current.PageCount {
			sizes = info.Pages
			for i, p := range sizes {
				if turns[i]%2 == 1 {
					sizes[i].Width, sizes[i].Height = p.Height, p.Width
				}
			}
		}
		if err := adoptBlob(ctx, tx, userID, blob); err != nil {
			return err
		}
		at := h.stampNow()
		pagesJSON, _ := json.Marshal(info.Pages)
		if _, err := tx.ExecContext(ctx, `UPDATE sign_documents SET snapshot_blob_id=?,snapshot_sha256=?,snapshot_size=?,pages_json=?,updated_at=? WHERE id=?`,
			blob.id, blob.sha256, blob.size, string(pagesJSON), at, current.ID); err != nil {
			return err
		}
		if err := rotateFields(ctx, tx, current.ID, turns, sizes); err != nil {
			return err
		}
		if orphaned, err = releaseBlobs(ctx, tx, current.SnapshotBlobID); err != nil {
			return err
		}
		return appendEvent(ctx, tx, current.ID, event{kind: "pages_rotated", at: at, client: clientOf(r),
			data: map[string]any{"pages": pages, "angle": angle, "previousSha256": current.SnapshotSHA256, "sha256": blob.sha256}})
	})
	if err != nil {
		blob.discard()
		return nil, err
	}
	h.removeBlobs(userID, orphaned)
	h.notifyOwner(ctx, userID, d.ID, d.Status)
	return h.describe(ctx, userID, d.ID)
}
