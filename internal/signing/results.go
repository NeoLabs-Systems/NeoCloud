package signing

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
)

// What signers produced: filled values, the final file and its variant
// without certificate pages.

// publicCacheControl is what the signer routes already set.
const publicCacheControl = "no-store, max-age=0"

const sandboxCSP = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'"

func contentDisposition(kind, name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	encoded := strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*").Replace(url.QueryEscape(name))
	return kind + `; filename="` + fallback + `"; filename*=UTF-8''` + encoded
}

type download struct {
	ownerID                           int64
	blobID, sha256, disposition, name string
	size                              int64
}

// sendPDF streams a stored PDF with Range support.
func (h *Handler) sendPDF(w http.ResponseWriter, r *http.Request, d download) {
	start, end, ranged, unsatisfiable := files.ParseByteRange(r.Header.Get("Range"), d.size)
	if !ranged {
		start, end = 0, d.size-1
	}
	var file *os.File
	var dek []byte
	if !unsatisfiable && r.Method != http.MethodHead {
		var err error
		file, dek, err = h.openBlob(r.Context(), d.ownerID, d.blobID)
		if err != nil {
			writeErr(w, httpError(500, "This file could not be read: its stored data is missing or damaged.", "FILE_UNREADABLE"))
			return
		}
		defer file.Close()
	}
	header := w.Header()
	header.Set("Content-Type", "application/pdf")
	header.Set("Content-Disposition", contentDisposition(d.disposition, d.name))
	header.Set("Accept-Ranges", "bytes")
	header.Set("ETag", `"`+d.sha256+`"`)
	if header.Get("Cache-Control") != publicCacheControl {
		header.Set("Cache-Control", "private, no-store")
	}
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", sandboxCSP)
	if unsatisfiable {
		header.Set("Content-Range", "bytes */"+strconv.FormatInt(d.size, 10))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	header.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	status := http.StatusOK
	if ranged {
		header.Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(d.size, 10))
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = cryptox.DecryptBlobRange(file, d.size, start, end, dek, d.blobID, w)
}

func snapshotDownload(d document) download {
	return download{ownerID: d.OwnerID, blobID: d.SnapshotBlobID, size: d.SnapshotSize, sha256: d.SnapshotSHA256, disposition: "inline", name: d.Title + ".pdf"}
}

func (h *Handler) ownerPDF(w http.ResponseWriter, r *http.Request, userID int64) {
	d, err := ownedDocument(r.Context(), h.db, userID, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendPDF(w, r, snapshotDownload(d))
}

func (h *Handler) publicPDF(w http.ResponseWriter, r *http.Request, c signerContext) {
	if c.state != "ready" && c.state != "completed" {
		writeErr(w, refusal(c.state))
		return
	}
	h.sendPDF(w, r, snapshotDownload(c.document))
}

// sendPNG sends a stored, re-encoded picture.
func (h *Handler) sendPNG(w http.ResponseWriter, r *http.Request, userID int64, blobID string, size int64) {
	data, err := h.readBlob(r.Context(), userID, blobID, size)
	if err != nil {
		writeErr(w, httpError(500, "This file could not be read: its stored data is missing or damaged.", "FILE_UNREADABLE"))
		return
	}
	header := w.Header()
	header.Set("Content-Type", "image/png")
	header.Set("Content-Length", strconv.Itoa(len(data)))
	if header.Get("Cache-Control") != publicCacheControl {
		header.Set("Cache-Control", "private, no-store")
	}
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", sandboxCSP)
	_, _ = w.Write(data)
}

// imageValue is the stored picture of a field of a document, if visible
// approves of the party that owns it.
func (h *Handler) imageValue(ctx context.Context, documentID, fieldID string, visible func(partyID, partyStatus string) bool) (string, int64, error) {
	notFound := httpError(404, "Not found", "NOT_FOUND")
	if !uuidPattern.MatchString(fieldID) {
		return "", 0, notFound
	}
	var blobID, partyID, status string
	var raw text
	var size int64
	err := h.db.QueryRowContext(ctx, `SELECT v.value_json,v.blob_ref,f.party_id,p.status,b.size FROM sign_values v
		JOIN sign_fields f ON f.id=v.field_id JOIN sign_parties p ON p.id=f.party_id JOIN blobs b ON b.id=v.blob_ref
		WHERE v.field_id=? AND v.document_id=?`, fieldID, documentID).Scan(&raw, &blobID, &partyID, &status, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, notFound
	}
	if err != nil {
		return "", 0, err
	}
	if !visible(partyID, status) || !(value{ValueJSON: raw, BlobRef: text{String: blobID, Valid: true}}).isImage() {
		return "", 0, notFound
	}
	return blobID, size, nil
}

func (h *Handler) ownerValueImage(w http.ResponseWriter, r *http.Request, userID int64) {
	d, err := ownedDocument(r.Context(), h.db, userID, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	blobID, size, err := h.imageValue(r.Context(), d.ID, r.PathValue("fieldId"), func(string, string) bool { return true })
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendPNG(w, r, d.OwnerID, blobID, size)
}

// publicValueImage shows the party its own pictures and those of parties that
// have completed.
func (h *Handler) publicValueImage(w http.ResponseWriter, r *http.Request, c signerContext) {
	if c.state != "ready" && c.state != "completed" {
		writeErr(w, refusal(c.state))
		return
	}
	blobID, size, err := h.imageValue(r.Context(), c.document.ID, r.PathValue("fieldId"), func(partyID, status string) bool {
		return partyID == c.party.ID || status == partyCompleted
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendPNG(w, r, c.document.OwnerID, blobID, size)
}

func (h *Handler) previewValues(r *http.Request, userID int64, _ body) (any, error) {
	d, err := ownedDocument(r.Context(), h.db, userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	values, err := listValues(r.Context(), h.db, d.ID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, v := range values {
		out[v.FieldID] = v.decoded()
	}
	return map[string]any{"values": out}, nil
}

type finalNode struct {
	id, name, blobID, etag string
	size                   int64
}

// liveFinalNode is the final file while it is still the one that was made:
// moved or renamed is fine, replaced, trashed or removed is not.
func liveFinalNode(ctx context.Context, q querier, d document) (finalNode, bool, error) {
	var n finalNode
	if d.Status != statusCompleted || !d.FinalNodeID.Valid {
		return n, false, nil
	}
	var blobID, trashed text
	err := q.QueryRowContext(ctx, `SELECT id,name,blob_id,etag,size,trashed_at FROM nodes WHERE id=?`, d.FinalNodeID.String).Scan(&n.id, &n.name, &blobID, &n.etag, &n.size, &trashed)
	if errors.Is(err, sql.ErrNoRows) {
		return n, false, nil
	}
	if err != nil {
		return n, false, err
	}
	n.blobID = blobID.String
	return n, !trashed.Valid && n.etag == d.FinalSHA256.String, nil
}

func finalAvailable(ctx context.Context, q querier, d document) (bool, error) {
	_, live, err := liveFinalNode(ctx, q, d)
	return live, err
}

func finalUnavailable() *apiError {
	return httpError(404, "The signed file is not available.", "FINAL_UNAVAILABLE")
}

// wantsCertificate reads ?certificate=0|1; 1 when absent.
func wantsCertificate(r *http.Request) (bool, error) {
	query := r.URL.Query()
	switch {
	case !query.Has("certificate") || query.Get("certificate") == "1":
		return true, nil
	case query.Get("certificate") == "0":
		return false, nil
	default:
		return false, badRequest("certificate must be 0 or 1.")
	}
}

// downloadEventGap: browsers fetch a file in ranges, and a link must not be
// able to fill the trail by reloading.
const downloadEventGap = time.Minute

func (h *Handler) recordDownload(r *http.Request, documentID, partyID, by string, certificate bool) error {
	if r.Method == http.MethodHead {
		return nil
	}
	ctx := r.Context()
	return h.tx(ctx, func(tx *sql.Tx) error {
		var last string
		err := tx.QueryRowContext(ctx, `SELECT at FROM sign_events WHERE document_id=? AND type='final_downloaded' AND party_id IS ?
			AND COALESCE(json_extract(data_json,'$.certificate'),1)=? ORDER BY id DESC LIMIT 1`, documentID, nullIfEmpty(partyID), flag(certificate)).Scan(&last)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if at, ok := parseStamp(last); ok && h.now().Sub(at) < downloadEventGap {
			return nil
		}
		data := map[string]any{"by": by}
		if !certificate {
			data["certificate"] = false
		}
		return appendEvent(ctx, tx, documentID, event{kind: "final_downloaded", partyID: partyID, at: h.stampNow(), client: clientOf(r), data: data})
	})
}

// ensureClean builds the variant without certificate pages on first use and
// keeps it as a blob of the document's owner.
func (h *Handler) ensureClean(ctx context.Context, documentID string) (document, error) {
	h.cleanMu.Lock()
	defer h.cleanMu.Unlock()
	d, err := documentByID(ctx, h.db, documentID)
	if err != nil || d.CleanBlobID.Valid {
		return d, err
	}
	pdf, sha, err := h.build(ctx, d, false)
	if err != nil {
		return d, err
	}
	blob, err := h.storeBlob(ctx, d.OwnerID, pdf)
	if err != nil {
		return d, err
	}
	kept := false
	err = h.tx(ctx, func(tx *sql.Tx) error {
		current, err := documentByID(ctx, tx, d.ID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && current.CleanBlobID.Valid {
			return nil
		}
		if err != nil {
			return err
		}
		if err := adoptBlob(ctx, tx, d.OwnerID, blob); err != nil {
			return err
		}
		kept = true
		_, err = tx.ExecContext(ctx, `UPDATE sign_documents SET clean_blob_id=?,clean_sha256=?,clean_size=?,updated_at=? WHERE id=?`, blob.id, sha, len(pdf), h.stampNow(), d.ID)
		return err
	})
	if err != nil || !kept {
		blob.discard()
	}
	if err != nil {
		return d, err
	}
	d, err = documentByID(ctx, h.db, documentID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !d.CleanBlobID.Valid {
		return d, finalUnavailable()
	}
	return d, err
}

// sendFinal serves the signed file of a document to its owner (partyID "") or
// one of its signers, with or without the certificate pages.
func (h *Handler) sendFinal(w http.ResponseWriter, r *http.Request, d document, partyID, by string, certificate bool) {
	ctx := r.Context()
	node, live, err := liveFinalNode(ctx, h.db, d)
	if err == nil && !live {
		err = finalUnavailable()
	}
	out := download{ownerID: d.OwnerID, blobID: node.blobID, size: node.size, sha256: node.etag, disposition: "attachment", name: node.name}
	if err == nil && !certificate {
		var built document
		if built, err = h.ensureClean(ctx, d.ID); err == nil {
			out.blobID, out.size, out.sha256, out.name = built.CleanBlobID.String, built.CleanSize.Int64, built.CleanSHA256.String, finalFileName(built, false)
		}
	}
	if err == nil {
		err = h.recordDownload(r, d.ID, partyID, by, certificate)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendPDF(w, r, out)
}

func (h *Handler) ownerFinal(w http.ResponseWriter, r *http.Request, userID int64) {
	certificate, err := wantsCertificate(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	d, err := ownedDocument(r.Context(), h.db, userID, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendFinal(w, r, d, "", "owner", certificate)
}

func (h *Handler) publicFinal(w http.ResponseWriter, r *http.Request, c signerContext) {
	certificate, err := wantsCertificate(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendFinal(w, r, c.document, c.party.ID, "signer", certificate)
}

// cleanForNode finds the clean variant through the signed file in the library.
func (h *Handler) cleanForNode(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx := r.Context()
	node, err := liveNode(ctx, h.db, userID, r.PathValue("nodeId"), "file")
	if err != nil {
		writeErr(w, err)
		return
	}
	d, err := scanDocument(h.db.QueryRowContext(ctx, `SELECT `+documentColumns+` FROM sign_documents WHERE final_node_id=? AND owner_id=?`, node.id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		err = fileNotFound()
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendFinal(w, r, d, "", "owner", false)
}
